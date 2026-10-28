package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	researchdomain "github.com/guide-flow/guide-flow/backend/domain/research"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

const SolutionPromptVersion = "solution-draft-v1"

const solutionSystemPrompt = `你是数学解题草稿生成器。只输出一个 JSON 对象，不输出 Markdown 或解释文字，也不要使用 content、draft、solutionDraft、finalAnswersDraft 等包装层。JSON 必须严格使用以下固定内部协议：{"schemaVersion":"solution-draft-v1","method":"...","assumptions":[],"variables":{},"steps":[],"expressions":[],"finalAnswer":"...","uncertainties":[]}。字段名称、类型和 schemaVersion 不得改变；无法确定的内容写入 uncertainties，不得猜测。输入包含用户确认后的正式题面、原题图片以及已经核对的外部参考解答。先判断参考解答是否与当前题面一致；冲突时记录 uncertainties。只能基于当前题面和可靠参考步骤生成可校验草稿，必须保留不确定项。`

type HTTPLLMConfig struct {
	Provider          string
	Endpoint          string
	APIKey            string
	Model             string
	Timeout           time.Duration
	MaxRetries        int
	RetryBaseDelay    time.Duration
	MaxTokens         int
	StreamIdleTimeout time.Duration
	// Normalizer 允许为新供应商注入自己的响应归一化器；未设置时 DeepSeek 使用内置适配器。
	Normalizer DraftNormalizer
}

type HTTPLLMSolver struct {
	cfg        HTTPLLMConfig
	client     *http.Client
	normalizer DraftNormalizer
}

type llmRequest struct {
	Model           string                    `json:"model"`
	SystemPrompt    string                    `json:"systemPrompt"`
	Question        domain.StructuredQuestion `json:"question"`
	Candidate       *domain.SolutionCandidate `json:"candidate,omitempty"`
	ResearchContext *domain.ResearchContext   `json:"researchContext,omitempty"`
	OriginalImage   string                    `json:"originalImage,omitempty"`
}

type llmResponse struct {
	Draft domain.SolutionDraft `json:"draft"`
}

type chatCompletionResponse struct {
	Choices []chatCompletionChoice `json:"choices"`
}

type chatCompletionChoice struct {
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Content string `json:"content"`
	} `json:"message"`
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
}

func NewHTTPLLMSolver(cfg HTTPLLMConfig) *HTTPLLMSolver {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 90 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	if cfg.StreamIdleTimeout <= 0 {
		cfg.StreamIdleTimeout = 30 * time.Second
	}
	// 流式请求不能使用 http.Client 的总读取超时，否则即使持续收到 keep-alive 也会被误杀。
	normalizer := cfg.Normalizer
	if normalizer == nil && strings.EqualFold(cfg.Provider, "deepseek") {
		normalizer = DeepSeekDraftNormalizer{}
	}
	return &HTTPLLMSolver{cfg: cfg, client: &http.Client{}, normalizer: normalizer}
}

func (a *HTTPLLMSolver) GenerateDraft(ctx context.Context, question domain.StructuredQuestion, candidate *domain.SolutionCandidate, researchContext *domain.ResearchContext) (domain.SolutionDraft, domain.ModelTrace, error) {
	return a.generateDraft(ctx, question, candidate, researchContext, nil, "")
}

// GenerateDraftWithImage 将原图作为解题事实来源传给视觉模型；Question 仅用于检索和结构化索引。
func (a *HTTPLLMSolver) GenerateDraftWithImage(ctx context.Context, question domain.StructuredQuestion, candidate *domain.SolutionCandidate, researchContext *domain.ResearchContext, image io.Reader, imageName string) (domain.SolutionDraft, domain.ModelTrace, error) {
	return a.generateDraft(ctx, question, candidate, researchContext, image, imageName)
}

func (a *HTTPLLMSolver) generateDraft(ctx context.Context, question domain.StructuredQuestion, candidate *domain.SolutionCandidate, researchContext *domain.ResearchContext, image io.Reader, imageName string) (domain.SolutionDraft, domain.ModelTrace, error) {
	if strings.TrimSpace(a.cfg.Endpoint) == "" {
		return domain.SolutionDraft{}, domain.ModelTrace{}, fmt.Errorf("llm endpoint is empty")
	}
	var imageData string
	if image != nil {
		data, readErr := io.ReadAll(io.LimitReader(image, 10<<20+1))
		if readErr != nil {
			return domain.SolutionDraft{}, domain.ModelTrace{}, readErr
		}
		if len(data) > 10<<20 {
			return domain.SolutionDraft{}, domain.ModelTrace{}, fmt.Errorf("original image exceeds 10 MiB")
		}
		imageData = "data:" + imageContentType(imageName) + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	payload, err := a.requestPayload(question, candidate, researchContext, imageData)
	if err != nil {
		return domain.SolutionDraft{}, domain.ModelTrace{}, err
	}
	requestID := fmt.Sprintf("llm-%d", time.Now().UnixNano())
	var lastErr error
	var lastRaw []byte
	var lastReport NormalizationReport
	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		draft, raw, report, retry, requestErr := a.doRequest(ctx, payload, requestID)
		if requestErr == nil {
			provider := a.cfg.Provider
			if provider == "" {
				provider = "http"
			}
			return draft, domain.ModelTrace{Provider: provider, Model: a.cfg.Model, PromptVersion: SolutionPromptVersion, RequestID: requestID, RawResponse: string(raw), OutputSchemaVersion: report.SchemaVersion, OutputShape: report.Shape, NormalizationWarnings: report.Warnings, NormalizationUnknownPaths: report.UnknownPaths, CreatedAt: time.Now()}, nil
		}
		// 解析失败时仍保留供应商原文，便于区分 JSON 格式漂移、截断和供应商错误。
		if len(raw) > 0 {
			lastRaw = raw
		}
		lastReport = report
		lastErr = requestErr
		if !retry || attempt == a.cfg.MaxRetries {
			break
		}
		select {
		case <-ctx.Done():
			return domain.SolutionDraft{}, domain.ModelTrace{}, ctx.Err()
		case <-time.After(a.cfg.RetryBaseDelay * time.Duration(1<<attempt)):
		}
	}
	provider := a.cfg.Provider
	if provider == "" {
		provider = "http"
	}
	return domain.SolutionDraft{}, domain.ModelTrace{Provider: provider, Model: a.cfg.Model, PromptVersion: SolutionPromptVersion, RequestID: requestID, RawResponse: string(lastRaw), OutputSchemaVersion: lastReport.SchemaVersion, OutputShape: lastReport.Shape, NormalizationWarnings: lastReport.Warnings, NormalizationUnknownPaths: lastReport.UnknownPaths, CreatedAt: time.Now()}, lastErr
}

func (a *HTTPLLMSolver) requestPayload(question domain.StructuredQuestion, candidate *domain.SolutionCandidate, researchContext *domain.ResearchContext, imageData string) ([]byte, error) {
	if strings.EqualFold(a.cfg.Provider, "deepseek") {
		var compactResearch *researchdomain.Context
		if researchContext != nil {
			compact := researchdomain.CompactForTutorial(*researchContext)
			compactResearch = &compact
		}
		textContent, err := json.Marshal(struct {
			Question        domain.StructuredQuestion `json:"question"`
			Candidate       *domain.SolutionCandidate `json:"candidate,omitempty"`
			ResearchContext *researchdomain.Context   `json:"researchContext,omitempty"`
		}{Question: question, Candidate: candidate, ResearchContext: compactResearch})
		if err != nil {
			return nil, err
		}
		content := []map[string]any{{"type": "text", "text": string(textContent)}}
		if imageData != "" {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": imageData}})
		}
		return json.Marshal(struct {
			Model          string            `json:"model"`
			Messages       []map[string]any  `json:"messages"`
			ResponseFormat map[string]string `json:"response_format"`
			Stream         bool              `json:"stream"`
			MaxTokens      int               `json:"max_tokens,omitempty"`
			Thinking       map[string]string `json:"thinking"`
		}{
			Model: a.cfg.Model,
			Messages: []map[string]any{
				{"role": "system", "content": solutionSystemPrompt},
				{"role": "user", "content": content},
			},
			ResponseFormat: map[string]string{"type": "json_object"},
			Stream:         true,
			MaxTokens:      a.cfg.MaxTokens,
			Thinking:       map[string]string{"type": "disabled"},
		})
	}
	return json.Marshal(llmRequest{Model: a.cfg.Model, SystemPrompt: solutionSystemPrompt, Question: question, Candidate: candidate, ResearchContext: researchContext, OriginalImage: imageData})
}

func imageContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "image/png"
	}
}

func (a *HTTPLLMSolver) doRequest(ctx context.Context, payload []byte, requestID string) (domain.SolutionDraft, []byte, NormalizationReport, bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, a.cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return domain.SolutionDraft{}, nil, NormalizationReport{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.EqualFold(a.cfg.Provider, "deepseek") {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("X-Request-ID", requestID)
	if a.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return domain.SolutionDraft{}, nil, NormalizationReport{}, true, err
	}
	if strings.EqualFold(a.cfg.Provider, "deepseek") && resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return a.parseDeepSeekStream(resp.Body)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return domain.SolutionDraft{}, nil, NormalizationReport{}, resp.StatusCode >= 500, err
	}
	if len(raw) > 1<<20 {
		return domain.SolutionDraft{}, nil, NormalizationReport{}, false, fmt.Errorf("llm response exceeds 1 MiB")
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return domain.SolutionDraft{}, raw, NormalizationReport{}, true, fmt.Errorf("llm provider status %s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.SolutionDraft{}, raw, NormalizationReport{}, false, fmt.Errorf("llm provider status %s", resp.Status)
	}
	draft, report, err := a.parseDraft(raw)
	if err != nil {
		return domain.SolutionDraft{}, raw, report, false, err
	}
	if err := validateDraft(draft); err != nil {
		return domain.SolutionDraft{}, raw, report, false, err
	}
	return draft, raw, report, false, nil
}

// parseDeepSeekStream 将 SSE 增量内容拼成完整 JSON 后再进入统一草稿校验。
// 中间片段只用于诊断和判断活跃度，绝不提前把不完整 JSON 写入领域对象。
func (a *HTTPLLMSolver) parseDeepSeekStream(body io.ReadCloser) (domain.SolutionDraft, []byte, NormalizationReport, bool, error) {
	defer body.Close()
	type lineResult struct {
		line string
		err  error
	}
	lines := make(chan lineResult, 1)
	go func() {
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 32*1024), 2<<20)
		for scanner.Scan() {
			lines <- lineResult{line: scanner.Text()}
		}
		streamErr := scanner.Err()
		if streamErr == nil {
			streamErr = io.EOF
		}
		lines <- lineResult{err: streamErr}
	}()

	var raw strings.Builder
	var content strings.Builder
	finishReason := ""
	timer := time.NewTimer(a.cfg.StreamIdleTimeout)
	defer timer.Stop()
	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(a.cfg.StreamIdleTimeout)
	}
	for {
		select {
		case result := <-lines:
			if result.err != nil {
				if result.err == io.EOF {
					return domain.SolutionDraft{}, []byte(raw.String()), NormalizationReport{}, false, fmt.Errorf("DeepSeek stream ended before [DONE]")
				}
				return domain.SolutionDraft{}, []byte(raw.String()), NormalizationReport{}, true, fmt.Errorf("read DeepSeek stream: %w", result.err)
			}
			resetTimer()
			raw.WriteString(result.line)
			raw.WriteByte('\n')
			data := strings.TrimSpace(strings.TrimPrefix(result.line, "data:"))
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return a.parseStreamedDraft(content.String(), finishReason, []byte(raw.String()))
				}
				continue
			}
			var chunk chatCompletionResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return domain.SolutionDraft{}, []byte(raw.String()), NormalizationReport{}, false, fmt.Errorf("decode DeepSeek stream event: %w", err)
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			contentPart := chunk.Choices[0].Delta.Content
			if contentPart == "" {
				// 兼容代理把最后一段放在 message.content 而不是 delta.content 的情况。
				contentPart = chunk.Choices[0].Message.Content
			}
			content.WriteString(contentPart)
			if chunk.Choices[0].FinishReason != "" {
				finishReason = chunk.Choices[0].FinishReason
			}
		case <-timer.C:
			return domain.SolutionDraft{}, []byte(raw.String()), NormalizationReport{}, true, fmt.Errorf("DeepSeek stream idle timeout")
		}
	}
}

func (a *HTTPLLMSolver) parseStreamedDraft(content, finishReason string, raw []byte) (domain.SolutionDraft, []byte, NormalizationReport, bool, error) {
	if finishReason == "length" {
		return domain.SolutionDraft{}, raw, NormalizationReport{Warnings: []string{"finish_reason=length"}}, false, fmt.Errorf("DeepSeek output truncated (finish_reason=length)")
	}
	normalizer := a.normalizer
	if normalizer == nil {
		normalizer = DeepSeekDraftNormalizer{}
	}
	draft, report, err := normalizer.Normalize(content)
	if err != nil {
		return domain.SolutionDraft{}, raw, report, false, fmt.Errorf("decode DeepSeek draft: %w", err)
	}
	if err := validateDraft(draft); err != nil {
		return domain.SolutionDraft{}, raw, report, false, err
	}
	return draft, raw, report, false, nil
}

func (a *HTTPLLMSolver) parseDraft(raw []byte) (domain.SolutionDraft, NormalizationReport, error) {
	if strings.EqualFold(a.cfg.Provider, "deepseek") {
		var response chatCompletionResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			return domain.SolutionDraft{}, NormalizationReport{}, fmt.Errorf("decode DeepSeek response: %w", err)
		}
		if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
			return domain.SolutionDraft{}, NormalizationReport{}, fmt.Errorf("DeepSeek response has no assistant content")
		}
		content := strings.TrimSpace(response.Choices[0].Message.Content)
		normalizer := a.normalizer
		if normalizer == nil {
			normalizer = DeepSeekDraftNormalizer{}
		}
		draft, report, err := normalizer.Normalize(content)
		if err != nil {
			return domain.SolutionDraft{}, report, fmt.Errorf("decode DeepSeek draft: %w", err)
		}
		return draft, report, nil
	}
	var response llmResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return domain.SolutionDraft{}, NormalizationReport{}, fmt.Errorf("decode llm response: %w", err)
	}
	if response.Draft.SchemaVersion == "" {
		response.Draft.SchemaVersion = solutiondomain.DraftSchemaVersion
	}
	return response.Draft, NormalizationReport{SchemaVersion: response.Draft.SchemaVersion, Shape: "generic:draft"}, nil
}

func validateDraft(draft domain.SolutionDraft) error {
	if draft.SchemaVersion != solutiondomain.DraftSchemaVersion {
		return fmt.Errorf("llm draft schemaVersion must be %q", solutiondomain.DraftSchemaVersion)
	}
	missing := make([]string, 0, 4)
	if strings.TrimSpace(draft.Method) == "" {
		missing = append(missing, "method")
	}
	if strings.TrimSpace(draft.FinalAnswer) == "" {
		missing = append(missing, "finalAnswer")
	}
	if len(missing) > 0 {
		return fmt.Errorf("llm draft must include %s", strings.Join(missing, ", "))
	}
	if len(draft.Steps) == 0 || len(draft.Expressions) == 0 {
		return fmt.Errorf("llm draft must include steps and expressions")
	}
	return nil
}
