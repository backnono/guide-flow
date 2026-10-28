package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	application "github.com/guide-flow/guide-flow/backend/application/research"
	domain "github.com/guide-flow/guide-flow/backend/domain/research"
)

const deepSeekResearchPromptVersion = "deepseek-web-research-v1"

// DeepSeekWebSearchConfig 同时描述模型端点和实际搜索端点；DeepSeek 只负责决定何时调用工具，搜索供应商负责访问公网。
type DeepSeekWebSearchConfig struct {
	DeepSeekEndpoint string
	DeepSeekAPIKey   string
	DeepSeekModel    string
	SearchEndpoint   string
	SearchAPIKey     string
	Timeout          time.Duration
	MaxRetries       int
	RetryBaseDelay   time.Duration
	MaxToolCalls     int
}

type DeepSeekWebSearchAdapter struct {
	cfg    DeepSeekWebSearchConfig
	client *http.Client
}

type deepSeekMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolCalls  []deepSeekToolCall `json:"tool_calls,omitempty"`
}

type deepSeekToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type deepSeekResponse struct {
	Choices []struct {
		FinishReason string          `json:"finish_reason"`
		Message      deepSeekMessage `json:"message"`
	} `json:"choices"`
}

type deepSeekSearchResponse struct {
	Results []struct {
		RerankScore float64 `json:"rerank_score"`
		Title       string  `json:"title"`
		URL         string  `json:"url"`
		Snippet     string  `json:"snippet"`
		Summary     string  `json:"summary"`
		Content     string  `json:"content"`
		Authority   int     `json:"authority"`
	} `json:"results"`
}

type researchAnswer struct {
	Status            string                     `json:"status"`
	MatchedQuestion   *researchMatchedQuestion   `json:"matchedQuestion,omitempty"`
	ReferenceSolution *researchReferenceSolution `json:"referenceSolution,omitempty"`
}

type researchMatchedQuestion struct {
	Title                   string   `json:"title"`
	MatchScore              float64  `json:"matchScore"`
	MatchedSubquestionCount int      `json:"matchedSubquestionCount"`
	Checks                  []string `json:"checks"`
}

type researchReferenceSolution struct {
	Status        string   `json:"status"`
	Steps         []string `json:"steps"`
	FinalAnswer   string   `json:"finalAnswer"`
	Uncertainties []string `json:"uncertainties"`
}

type webSearchArguments struct {
	Query string `json:"query"`
}

func NewDeepSeekWebSearchAdapter(cfg DeepSeekWebSearchConfig) *DeepSeekWebSearchAdapter {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.MaxToolCalls <= 0 {
		cfg.MaxToolCalls = 3
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	return &DeepSeekWebSearchAdapter{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

// Search 执行有限的 tool-calling 循环，只有 web_search 工具可以被模型调用，避免把任意函数暴露给外部模型。
func (a *DeepSeekWebSearchAdapter) Search(ctx context.Context, query string) (domain.Context, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return unavailableContext(query, "empty_query"), fmt.Errorf("research query is empty")
	}
	if strings.TrimSpace(a.cfg.DeepSeekEndpoint) == "" || strings.TrimSpace(a.cfg.DeepSeekAPIKey) == "" {
		return unavailableContext(query, "deepseek_not_configured"), fmt.Errorf("DeepSeek endpoint or API key is not configured")
	}
	if strings.TrimSpace(a.cfg.SearchEndpoint) == "" || strings.TrimSpace(a.cfg.SearchAPIKey) == "" {
		return unavailableContext(query, "web_search_not_configured"), fmt.Errorf("web search endpoint or API key is not configured")
	}

	messages := []deepSeekMessage{
		{Role: "system", Content: `你是题目检索 Agent。必须先调用 web_search 访问公网，不能凭记忆回答。
请核对搜索结果是否与当前题目一致，重点比较题干、数值、点线关系和小问数量。
最后只输出 JSON，不要输出 Markdown：{"status":"verified|found|conflict|research_unavailable","matchedQuestion":...,"referenceSolution":...}。
只有明确匹配当前题目时才使用 verified；相似题只能使用 found，并在 checks 或 uncertainties 中说明。`},
		{Role: "user", Content: query},
	}

	var sources []domain.Source
	for call := 0; call < a.cfg.MaxToolCalls; call++ {
		message, retry, err := a.callDeepSeek(ctx, messages, call == 0)
		if err != nil {
			if retry && call < a.cfg.MaxRetries {
				if waitErr := waitForRetry(ctx, a.cfg.RetryBaseDelay, call); waitErr != nil {
					return unavailableContext(query, "research_timeout"), waitErr
				}
				call--
				continue
			}
			return unavailableContext(query, "deepseek_agent_error"), err
		}

		if len(message.ToolCalls) == 0 {
			return a.normalizeAnswer(query, sources, message.Content)
		}

		messages = append(messages, message)
		for _, toolCall := range message.ToolCalls {
			if toolCall.Type != "function" || toolCall.Function.Name != "web_search" {
				return unavailableContext(query, "unsupported_tool_call"), fmt.Errorf("unsupported research tool %q", toolCall.Function.Name)
			}
			args, err := parseWebSearchArguments(toolCall.Function.Arguments)
			if err != nil {
				return unavailableContext(query, "invalid_tool_arguments"), err
			}
			result, resultSources, searchErr := a.webSearch(ctx, args.Query, query)
			if searchErr != nil {
				return unavailableContext(query, "web_search_error"), searchErr
			}
			sources = append(sources, resultSources...)
			messages = append(messages, deepSeekMessage{Role: "tool", ToolCallID: toolCall.ID, Content: result})
			// 每轮搜索后先做本地核对；已经找到高匹配且可读的来源时，不再浪费后续搜索轮次。
			if match := application.MatchSources(query, sources); match.ShouldStop {
				return contextFromLocalMatch(query, sources, match), nil
			}
		}
	}
	if match := application.MatchSources(query, sources); match.Matched {
		return contextFromLocalMatch(query, sources, match), nil
	}
	if len(sources) > 0 {
		result := unavailableContext(query, "research_match_failed")
		result.Sources = append([]domain.Source(nil), sources...)
		return result, fmt.Errorf("research sources did not pass local question matching")
	}
	return unavailableContext(query, "tool_call_limit"), fmt.Errorf("research agent exceeded max tool calls")
}

func (a *DeepSeekWebSearchAdapter) callDeepSeek(ctx context.Context, messages []deepSeekMessage, requireTool bool) (deepSeekMessage, bool, error) {
	toolChoice := "auto"
	if requireTool {
		toolChoice = "required"
	}
	payload := map[string]any{
		"model":    a.cfg.DeepSeekModel,
		"messages": messages,
		// DeepSeek 仅在非 thinking 模式下支持 tool_choice=required；否则会被 API 以 400 拒绝。
		"thinking": map[string]string{"type": "disabled"},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "web_search",
				"description": "Search the public web for the current question and return source pages.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"query": map[string]string{"type": "string"}},
					"required":   []string{"query"},
				},
			},
		}},
		"tool_choice": toolChoice,
		"temperature": 0,
	}
	if !requireTool {
		// 工具结果回传后的最终回答必须是内部协议 JSON，避免把 Markdown 或解释文本交给领域层解析。
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return deepSeekMessage{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.DeepSeekEndpoint, bytes.NewReader(body))
	if err != nil {
		return deepSeekMessage{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.DeepSeekAPIKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return deepSeekMessage{}, true, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if readErr != nil {
		return deepSeekMessage{}, resp.StatusCode >= 500, readErr
	}
	if len(raw) > 2<<20 {
		return deepSeekMessage{}, false, fmt.Errorf("DeepSeek research response exceeds 2 MiB")
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return deepSeekMessage{}, true, fmt.Errorf("DeepSeek research status %s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deepSeekMessage{}, false, fmt.Errorf("DeepSeek research status %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var response deepSeekResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return deepSeekMessage{}, false, fmt.Errorf("decode DeepSeek research response: %w", err)
	}
	if len(response.Choices) == 0 {
		return deepSeekMessage{}, false, fmt.Errorf("DeepSeek research response has no choices")
	}
	return response.Choices[0].Message, false, nil
}

func (a *DeepSeekWebSearchAdapter) webSearch(ctx context.Context, query, originalQuestion string) (string, []domain.Source, error) {
	// DeepSeek Search API 限制 query 和 ori_question 为 128 个 Unicode 字符；截断只发生在供应商边界，不改变内部正式题面。
	query = truncateRunes(strings.TrimSpace(query), 128)
	if query == "" {
		return "", nil, fmt.Errorf("web_search query is empty")
	}
	payload, err := json.Marshal(map[string]any{
		"query":        query,
		"count":        5,
		"ori_question": truncateRunes(originalQuestion, 128),
		"need_content": true,
	})
	if err != nil {
		return "", nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.SearchEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.SearchAPIKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil {
		return "", nil, err
	}
	if len(raw) > 2<<20 {
		return "", nil, fmt.Errorf("web search response exceeds 2 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("web search status %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var response deepSeekSearchResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", nil, fmt.Errorf("decode web search response: %w", err)
	}
	if len(response.Results) == 0 {
		return `{"results":[]}`, nil, nil
	}
	results := make([]map[string]any, 0, len(response.Results))
	sources := make([]domain.Source, 0, len(response.Results))
	for _, item := range response.Results {
		summary := strings.TrimSpace(item.Summary)
		if summary == "" {
			summary = strings.TrimSpace(item.Snippet)
		}
		if summary == "" {
			summary = strings.TrimSpace(item.Content)
		}
		if strings.TrimSpace(item.URL) == "" || summary == "" {
			continue
		}
		results = append(results, map[string]any{"title": item.Title, "url": item.URL, "snippet": item.Snippet, "summary": item.Summary, "content": item.Content, "rerankScore": item.RerankScore, "authority": item.Authority})
		sources = append(sources, domain.Source{Title: item.Title, URL: item.URL, Summary: summary, TrustScore: deepSeekTrustScore(item.RerankScore, item.Authority)})
	}
	toolResult, err := json.Marshal(map[string]any{"results": results})
	return string(toolResult), sources, err
}

func (a *DeepSeekWebSearchAdapter) normalizeAnswer(query string, sources []domain.Source, content string) (domain.Context, error) {
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```"))
	var answer researchAnswer
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		if match := application.MatchSources(query, sources); match.Matched {
			result := contextFromLocalMatch(query, sources, match)
			result.ErrorCode = "invalid_agent_output"
			result.ReferenceSolution = &domain.ReferenceSolution{Status: "unavailable", Uncertainties: []string{"invalid_agent_output"}}
			return result, nil
		}
		result := unavailableContext(query, "research_match_failed")
		result.Sources = append([]domain.Source(nil), sources...)
		return result, fmt.Errorf("decode DeepSeek research answer: %w", err)
	}
	result := domain.Context{Query: query, Sources: sources, Status: domain.Status(answer.Status), RetrievedAt: time.Now()}
	if answer.MatchedQuestion != nil {
		result.MatchedQuestion = &domain.MatchedQuestion{Title: answer.MatchedQuestion.Title, MatchScore: answer.MatchedQuestion.MatchScore, MatchedSubquestionCount: answer.MatchedQuestion.MatchedSubquestionCount, Checks: append([]string(nil), answer.MatchedQuestion.Checks...)}
	}
	if answer.ReferenceSolution != nil {
		result.ReferenceSolution = &domain.ReferenceSolution{Status: answer.ReferenceSolution.Status, Steps: append([]string(nil), answer.ReferenceSolution.Steps...), FinalAnswer: answer.ReferenceSolution.FinalAnswer, Uncertainties: append([]string(nil), answer.ReferenceSolution.Uncertainties...)}
	}
	if result.Status == "" {
		result.Status = domain.StatusFound
	}
	match := application.MatchSources(query, sources)
	if !match.Matched {
		result.Status = domain.StatusUnavailable
		result.ErrorCode = "research_match_failed"
		return result, fmt.Errorf("agent answer did not pass local question matching")
	}
	result.Status = domain.StatusFound
	if match.ShouldStop {
		result.Status = domain.StatusVerified
	}
	if result.MatchedQuestion == nil {
		result.MatchedQuestion = &domain.MatchedQuestion{Title: match.Title, MatchScore: match.Score, Checks: append([]string(nil), match.Checks...)}
	}
	return application.ValidateContext(result), nil
}

func contextFromLocalMatch(query string, sources []domain.Source, match application.SourceMatch) domain.Context {
	status := domain.StatusFound
	if match.ShouldStop {
		status = domain.StatusVerified
	}
	return application.ValidateContext(domain.Context{
		Query:           query,
		Sources:         append([]domain.Source(nil), sources...),
		Status:          status,
		MatchedQuestion: &domain.MatchedQuestion{Title: match.Title, MatchScore: match.Score, Checks: append([]string(nil), match.Checks...)},
		RetrievedAt:     time.Now(),
	})
}

func parseWebSearchArguments(raw string) (webSearchArguments, error) {
	var args webSearchArguments
	if err := json.Unmarshal([]byte(raw), &args); err != nil || strings.TrimSpace(args.Query) == "" {
		return webSearchArguments{}, fmt.Errorf("invalid web_search arguments")
	}
	return args, nil
}

func unavailableContext(query, code string) domain.Context {
	return domain.Context{Query: query, Status: domain.StatusUnavailable, ErrorCode: code, RetrievedAt: time.Now()}
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func deepSeekTrustScore(rerankScore float64, authority int) float64 {
	// DeepSeek 的 rerank_score 为 0～3，领域协议统一使用 0～1；权威性只作为补充信号。
	score := rerankScore / 3
	if authority >= 3 && score < 1 {
		score += 0.05
	}
	return clampScore(score)
}

func truncateRunes(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}

func waitForRetry(ctx context.Context, delay time.Duration, attempt int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay * time.Duration(1<<attempt)):
		return nil
	}
}

var _ application.SearchAdapter = (*DeepSeekWebSearchAdapter)(nil)
