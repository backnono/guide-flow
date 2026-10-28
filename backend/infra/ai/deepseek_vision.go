package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
)

const VisionPromptVersion = "question-vision-v7"
const VisionSchemaVersion = "question-vision-v1"

const legacyVisionGeometryPrompt = `你是数学题目视觉解析器。请仔细阅读用户上传的原题图片，输出严格 JSON，不要输出 Markdown。
目标不是简单 OCR，而是还原成可以正式讲解的题面：
1. text 必须保留题号、完整题干、所有小问；含图形的小问必须追加“图形说明”，说明点、线段、交点、连接关系和题目要求。
2. structuredQuestion 要保留题型、已知条件、关系、问题目标和图形实体；请按原题分别输出 diagram.figures（图1、图2……），每张图列出 points、segments、intersections、collinear、shape、constraints、layoutHints。shape 只描述明确识别出的形状，例如 rectangle、triangle；constraints 记录平行、垂直、直角点、中点、共线、延长线、长度比较等数学不变量，并把参与关系的 points、lines 或 segments 填入对象。layoutHints 只记录原图的示意比例或方向，不能当作数学坐标。不要根据图形视觉比例臆造 coordinates，题目没有给出的坐标 coordinates 必须留空。
3. analysis 记录识别不确定项。看不清的字符放入 uncertainItems，不要猜测。
JSON 结构：{"text":"正式题面","confidence":0.0,"fieldConfidence":{"text":0.0,"numbers":0.0,"diagram":0.0,"asks":0.0},"analysis":{...},"structuredQuestion":{"schemaVersion":"question-vision-v1","grade":0,"topic":"","normalizedText":"","knowns":[],"relations":[],"asks":[],"expressions":[],"payload":{"kind":"","data":{}},"diagram":{"figures":[]}}}

严格约束：只使用 topic、knowns、relations、asks、expressions、payload、diagram.figures 等约定字段；不要使用 questionType、knownConditions、figureNo。knowns 必须是 {"name":string,"value":string,"unit":string}[]，禁止使用 id/text/type/points 等另一套对象协议。非图形题可以省略 diagram 或返回空对象。points 必须是 string[]，segments 必须是 string[]，intersections 必须是 {"point":string,"of":string[]}[]，collinear 必须是 string[][]，constraints 必须是对象数组。shape 必须是一个字符串，禁止返回数组；优先使用 rectangle、square、triangle、right_triangle、quadrilateral。layoutHints 必须是单个对象，只允许 aspectRatio、orientation、anchors，禁止返回数组；anchors 必须是点名到归一化坐标对象的映射，例如 {"A":{"x":0.2,"y":0.8}}，x、y 必须在 0 到 1 之间，禁止使用 top、bottom_left、right_angle_vertex 等语义字符串。constraint.type 使用 snake_case，例如 right_angle、point_on_segment、midpoint、perpendicular；统一使用 points、lines、segments，禁止用 of、on 代替。没有内容返回空数组或空对象，不省略适用字段，不臆造数学坐标；每张图片单独保留为一个 figure。`

const visionCommonPrompt = `公共题型约束：先判断 questionKind，只能是 geometry、function、word_problem、arithmetic、other 之一，并写入 analysis.questionKind；以下题型扩展只执行与 questionKind 对应的一套，其余扩展全部忽略。所有题型都必须保留题号、完整题干和所有小问，提取已知条件、关系、问题目标和不确定项。输出必须是合法 JSON，不要输出 Markdown 或 JSON 以外的内容。knowns 必须是 {"name":string,"value":string,"unit":string}[]。看不清的字符放入 uncertainItems，不要猜测；每张图片及每一张图单独保留，不能串图。函数、应用题、算术题和 other 不得生成 diagram.figures。`

const visionEnvelopePrompt = `外层响应协议是强制的，不能改成扁平对象，也不能使用 stem、questionNumber 或 questionKind 替代约定字段。顶层必须同时包含 text、confidence、fieldConfidence、analysis、structuredQuestion 五个字段。text 必须是完整正式题面字符串；confidence 必须是数字；fieldConfidence 必须是对象；analysis 必须是对象；structuredQuestion 必须是对象，且必须包含 schemaVersion、grade、topic、normalizedText、knowns、relations、asks、expressions、payload、diagram。structuredQuestion.schemaVersion 必须严格等于 question-vision-v1。即使某个字段为空，也必须返回约定的空数组或空对象。`

const visionGeometryPrompt = `仅当 questionKind=geometry 时执行几何题扩展。diagram.figures 必须使用唯一线协议：points、segments、curves、intersections、collinear、shape、constraints、layoutHints。points 和 segments 必须是 string[]；segments 统一写成 "AB"，禁止 {"from":"A","to":"B"}。intersections 必须是 {"point":string,"of":string[]}[]；collinear 必须是 string[][]，禁止字符串或对象。constraints 必须是对象数组，禁止字符串；每个对象至少有 type:string，参与实体只能使用 points、lines、segments:string[]，angle/value 使用 string。圆弧必须使用 curves，例如 {"type":"arc","endpoints":["A","B"],"center":"O","sweep":"upper"}；半圆不能只写在文字中。shape 必须是单个字符串，例如 circle、semicircle、triangle；不要根据视觉比例臆造 coordinates。layoutHints 必须是对象，anchors 必须是点名到归一化坐标对象的映射，例如 {"A":{"x":0.2,"y":0.8}}，禁止使用 top、bottom_left 等语义字符串。`

const visionFunctionPrompt = `函数题扩展：重点提取函数表达式、变量、定义域、值域、单调性、零点、交点、表格和图像坐标；公式进入 expressions，题型专属结构进入 payload.kind=function。不要把函数曲线猜成几何线段。`

const visionWordProblemPrompt = `应用题扩展：重点提取对象、数量、单位、时间、速度、价格、比例等业务量及其数量关系；方程、不等式或计算表达式进入 expressions，题型专属结构进入 payload.kind=word_problem。`

// 公共 Prompt 与题型扩展分开维护，避免几何规则继续污染函数题和应用题。
const visionPromptExtensions = visionCommonPrompt + "\n" + visionEnvelopePrompt + "\n" + visionGeometryPrompt + "\n" + visionFunctionPrompt + "\n" + visionWordProblemPrompt

const visionSystemPrompt = visionPromptExtensions

// DeepSeekVisionAnalyzer 将原图作为 image_url 内容块发送，避免把多模态模型降级成纯文本模型。
// Base64 内联图片不依赖公网 URL，适合当前图片只存于对象存储内部的部署方式。
type DeepSeekVisionAnalyzer struct {
	cfg    HTTPLLMConfig
	client *http.Client
}

type visionResponse struct {
	Text               string             `json:"text"`
	Confidence         float64            `json:"confidence"`
	FieldConfidence    map[string]float64 `json:"fieldConfidence"`
	Analysis           json.RawMessage    `json:"analysis"`
	StructuredQuestion json.RawMessage    `json:"structuredQuestion"`
}

func NewDeepSeekVisionAnalyzer(cfg HTTPLLMConfig) *DeepSeekVisionAnalyzer {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	if cfg.MaxTokens <= 0 {
		// 视觉题需要同时输出题干、公式和多张图的结构；关闭 thinking 后将预算留给最终 JSON。
		cfg.MaxTokens = 8192
	}
	return &DeepSeekVisionAnalyzer{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

func (a *DeepSeekVisionAnalyzer) Analyze(ctx context.Context, image io.Reader, imageName string) (domain.OCRResult, error) {
	data, err := io.ReadAll(io.LimitReader(image, 32<<20+1))
	if err != nil {
		return domain.OCRResult{}, fmt.Errorf("read vision image: %w", err)
	}
	if len(data) == 0 || len(data) > 32<<20 {
		return domain.OCRResult{}, fmt.Errorf("vision image must be between 1 byte and 32 MiB")
	}
	mimeType := http.DetectContentType(data)
	if ext := strings.ToLower(filepath.Ext(imageName)); mimeType == "application/octet-stream" {
		switch ext {
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".png":
			mimeType = "image/png"
		case ".gif":
			mimeType = "image/gif"
		case ".webp":
			mimeType = "image/webp"
		}
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return domain.OCRResult{}, fmt.Errorf("unsupported vision image type %q", mimeType)
	}

	imageURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
	payload, err := json.Marshal(map[string]any{
		"model":      a.cfg.Model,
		"max_tokens": a.cfg.MaxTokens,
		// Vision 解析只需要稳定的结构化结果，不需要把输出预算消耗在 reasoning_content 上。
		"thinking": map[string]string{"type": "disabled"},
		"messages": []any{
			map[string]any{"role": "system", "content": visionSystemPrompt},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "请解析这张数学题图片，输出正式题面和结构化图形关系。"},
				map[string]any{"type": "image_url", "image_url": map[string]string{"url": imageURL}},
			}},
		},
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return domain.OCRResult{}, err
	}

	requestID := fmt.Sprintf("vision-%d", time.Now().UnixNano())
	var lastErr error
	var lastResult domain.OCRResult
	corrected := false
	networkRetries := 0
	requestCount := 0
	for {
		requestCount++
		result, retry, correctionNeeded, requestErr := a.doRequest(ctx, payload, requestID)
		if requestErr == nil {
			result.RequestCount = requestCount
			return result, nil
		}
		result.RequestCount = requestCount
		lastResult = result
		lastErr = requestErr
		if correctionNeeded && !corrected {
			// 结构校验失败只允许一次完整纠错，避免模型字段漂移触发无限调用。
			payload = visionCorrectionPayload(payload, requestErr)
			corrected = true
			continue
		}
		if !retry || networkRetries >= a.cfg.MaxRetries {
			return lastResult, lastErr
		}
		networkRetries++
		select {
		case <-ctx.Done():
			return domain.OCRResult{}, ctx.Err()
		case <-time.After(a.cfg.RetryBaseDelay * time.Duration(1<<(networkRetries-1))):
		}
	}
}

func (a *DeepSeekVisionAnalyzer) doRequest(ctx context.Context, payload []byte, requestID string) (domain.OCRResult, bool, bool, error) {
	startedAt := time.Now()
	trace := func(result domain.OCRResult) domain.OCRResult {
		result.Provider = "deepseek-vision"
		result.RequestID = requestID
		result.PromptVersion = VisionPromptVersion
		result.SchemaVersion = VisionSchemaVersion
		result.DurationMs = time.Since(startedAt).Milliseconds()
		if result.CreatedAt.IsZero() {
			result.CreatedAt = domain.Now()
		}
		return result
	}
	if strings.TrimSpace(a.cfg.Endpoint) == "" || strings.TrimSpace(a.cfg.Model) == "" {
		return trace(domain.OCRResult{}), false, false, fmt.Errorf("deepseek vision endpoint and model are required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return trace(domain.OCRResult{}), false, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	req.Header.Set("X-Request-ID", requestID)
	resp, err := a.client.Do(req)
	if err != nil {
		return trace(domain.OCRResult{}), true, false, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil {
		return trace(domain.OCRResult{}), resp.StatusCode >= 500, false, err
	}
	if len(raw) > 2<<20 {
		return trace(domain.OCRResult{RawResponse: raw}), false, false, fmt.Errorf("deepseek vision response exceeds 2 MiB")
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return trace(domain.OCRResult{RawResponse: raw}), true, false, fmt.Errorf("deepseek vision status %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return trace(domain.OCRResult{RawResponse: raw}), false, false, fmt.Errorf("deepseek vision status %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	result, correctionNeeded, decodeErr := decodeVisionProviderResponse(raw)
	return trace(result), false, correctionNeeded, decodeErr
}

// DecodeVisionProviderResponse 是离线回放与线上请求共用的纯响应入口；它不包含 HTTP 或密钥逻辑。
func DecodeVisionProviderResponse(raw []byte) (domain.OCRResult, error) {
	result, _, err := decodeVisionProviderResponse(raw)
	return result, err
}

func decodeVisionProviderResponse(raw []byte) (domain.OCRResult, bool, error) {
	var completion chatCompletionResponse
	if err := json.Unmarshal(raw, &completion); err != nil || len(completion.Choices) == 0 {
		if err == nil {
			err = fmt.Errorf("choices is empty")
		}
		return domain.OCRResult{RawResponse: raw, ParseError: err.Error(), ParseErrorPath: "choices"}, true, fmt.Errorf("decode DeepSeek vision response: %w", err)
	}
	finishReason := completion.Choices[0].FinishReason
	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	if finishReason == "length" {
		return domain.OCRResult{RawResponse: raw, FinishReason: finishReason, ParseError: "模型输出因 max_tokens 截断", ParseErrorPath: "choices[0].finish_reason"}, true, fmt.Errorf("DeepSeek vision response was truncated (finish_reason=length)")
	}
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	content = strings.TrimSpace(content)
	content = repairCommonVisionJSON(content)
	if repaired, ok := repairSingleMissingObjectBraceBeforeArrayClose(content); ok {
		content = repaired
	}
	if repaired, ok := repairSingleMissingRootBrace(content); ok {
		content = repaired
	}
	var parsed visionResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return domain.OCRResult{RawResponse: raw, FinishReason: finishReason, ParseError: err.Error(), ParseErrorPath: "choices[0].message.content"}, true, fmt.Errorf("decode DeepSeek vision JSON: %w", err)
	}
	if strings.TrimSpace(parsed.Text) == "" {
		return domain.OCRResult{RawResponse: raw, FinishReason: finishReason, ParseError: "empty question text", ParseErrorPath: "text"}, true, fmt.Errorf("DeepSeek vision returned empty question text")
	}
	analysis := decodeVisionAnalysis(parsed.Analysis)
	if parsed.Confidence <= 0 {
		parsed.Confidence = 0.9
	}
	structuredQuestion, err := decodeVisionQuestion(parsed.StructuredQuestion, parsed.Text)
	if err != nil {
		result := domain.OCRResult{RawResponse: raw, FinishReason: finishReason, ParseError: err.Error(), ParseErrorPath: validationErrorPath(err)}
		result.Draft = domain.QuestionDraft{Text: strings.TrimSpace(parsed.Text), Confidence: parsed.Confidence, FieldConfidence: parsed.FieldConfidence}
		result.Analysis = analysis
		return result, true, fmt.Errorf("decode DeepSeek structured question: %w", err)
	}
	result := domain.OCRResult{Draft: domain.QuestionDraft{Text: strings.TrimSpace(parsed.Text), Confidence: parsed.Confidence, FieldConfidence: parsed.FieldConfidence}, Analysis: analysis, StructuredQuestion: structuredQuestion, RawResponse: raw, FinishReason: finishReason}
	if err := validateVisionQuestion(structuredQuestion); err != nil {
		result.ParseError, result.ParseErrorPath = err.Error(), validationErrorPath(err)
		return result, true, err
	}
	return result, false, nil
}

// repairCommonVisionJSON 只处理真实响应中可判定的 JSON 外壳问题：多余根闭合符、未转义省略号和数组内误转义引号。
// 任何修复都必须以 json.Valid 通过为前提，不能用宽泛替换猜测数学内容。
func repairCommonVisionJSON(content string) string {
	if json.Valid([]byte(content)) {
		return content
	}
	for strings.HasSuffix(content, "}") {
		candidate := strings.TrimSuffix(content, "}")
		if json.Valid([]byte(candidate)) {
			return candidate
		}
		content = candidate + "}"
		break
	}
	candidate := strings.ReplaceAll(content, `"..."`, `...`)
	candidate = strings.ReplaceAll(candidate, `[\"`, `["`)
	candidate = strings.ReplaceAll(candidate, `\"]`, `"]`)
	if json.Valid([]byte(candidate)) {
		return candidate
	}
	return content
}

// repairSingleMissingObjectBraceBeforeArrayClose 仅处理扫描确认的“对象闭合符缺失、却遇到数组闭合符”这一种确定性损坏。
// 修复后必须再次通过 JSON 解析，且全程只允许插入一个 }，避免凭空改写题目内容。
func repairSingleMissingObjectBraceBeforeArrayClose(content string) (string, bool) {
	content = strings.TrimSpace(content)
	stack := make([]byte, 0, 16)
	inString, escaped := false, false
	for index := 0; index < len(content); index++ {
		char := content[index]
		if escaped {
			escaped = false
			continue
		}
		if inString && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch char {
		case '{', '[':
			stack = append(stack, char)
		case '}', ']':
			want := byte('{')
			if char == ']' {
				want = '['
			}
			if len(stack) == 0 || stack[len(stack)-1] == want {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}
			if char != ']' || stack[len(stack)-1] != '{' {
				return "", false
			}
			candidate := content[:index] + "}" + content[index:]
			var decoded any
			if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
				return candidate, true
			}
			// 同一响应可能同时缺少图形对象闭合符和根对象闭合符；先确认只需再补一个根 }，再交给外层修复。
			if repairedRoot, rootOK := repairSingleMissingRootBrace(candidate); rootOK {
				if err := json.Unmarshal([]byte(repairedRoot), &decoded); err == nil {
					return candidate, true
				}
			}
			return "", false
		}
	}
	return "", false
}

// repairSingleMissingRootBrace 只修复括号扫描确认的单个根对象闭合缺失。
// 不修复字符串、数组或多个括号缺失，避免把未知数学内容“猜”成合法 JSON。
func repairSingleMissingRootBrace(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "{") || strings.HasSuffix(content, "}") == false {
		return "", false
	}
	objectDepth, arrayDepth := 0, 0
	inString, escaped := false, false
	for _, char := range content {
		if escaped {
			escaped = false
			continue
		}
		if inString && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch char {
		case '{':
			objectDepth++
		case '}':
			objectDepth--
		case '[':
			arrayDepth++
		case ']':
			arrayDepth--
		}
	}
	if inString || objectDepth != 1 || arrayDepth != 0 {
		return "", false
	}
	return content + "}", true
}

func decodeVisionAnalysis(raw json.RawMessage) *domain.QuestionAnalysis {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var analysis domain.QuestionAnalysis
	if err := json.Unmarshal(raw, &analysis); err != nil {
		return nil
	}
	var metadata struct {
		UncertainItems []string `json:"uncertainItems"`
		Notes          []string `json:"notes"`
	}
	if err := json.Unmarshal(raw, &metadata); err == nil {
		if len(analysis.UncertainItems) == 0 {
			analysis.UncertainItems = metadata.UncertainItems
		}
		if len(analysis.Notes) == 0 {
			analysis.Notes = metadata.Notes
		}
	}
	return &analysis
}

// visionCorrectionPayload 保留原始图片请求，只追加明确的失败路径；纠错必须重新输出完整 JSON，不能让模型只返回补丁。
func visionCorrectionPayload(payload []byte, parseErr error) []byte {
	var request map[string]any
	if err := json.Unmarshal(payload, &request); err != nil {
		return payload
	}
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) == 0 {
		return payload
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		return payload
	}
	last["content"] = appendVisionCorrection(last["content"], parseErr)
	updated, err := json.Marshal(request)
	if err != nil {
		return payload
	}
	return updated
}

func appendVisionCorrection(content any, parseErr error) any {
	message := "上一次输出未通过结构校验：" + parseErr.Error() + "。请根据原图重新输出完整 JSON，不要只输出修改片段；严格使用 schemaVersion、topic、knowns、relations、asks、diagram.figures 及其约定类型。"
	if strings.Contains(parseErr.Error(), "references points that do not exist in this figure") {
		message += " 约束只能引用同一 figure.points、segments 或 lines 中已经列出的实体；无法确认的点线不要补造，移入 uncertainItems。"
	}
	if blocks, ok := content.([]any); ok {
		blocks = append(blocks, map[string]any{"type": "text", "text": message})
		return blocks
	}
	return message
}

type visionValidationError struct{ path, message string }

func (e *visionValidationError) Error() string { return e.path + ": " + e.message }

// validateVisionQuestion 在适配层阻断不完整的结构，避免空问题进入 structuring/solving/planning。
func validateVisionQuestion(question *questiondomain.StructuredQuestion) error {
	if question == nil {
		return &visionValidationError{"structuredQuestion", "must be an object"}
	}
	if question.SchemaVersion != VisionSchemaVersion {
		return &visionValidationError{"schemaVersion", "must be " + VisionSchemaVersion}
	}
	if strings.TrimSpace(question.Topic) == "" {
		return &visionValidationError{"topic", "must not be empty"}
	}
	if strings.TrimSpace(question.NormalizedText) == "" {
		return &visionValidationError{"normalizedText", "must not be empty"}
	}
	if len(question.Asks) == 0 {
		return &visionValidationError{"asks", "must contain at least one question target"}
	}
	if strings.Contains(strings.ToLower(question.Topic), "geometry") && (question.Diagram == nil || len(question.Diagram.Figures) == 0) {
		return &visionValidationError{"diagram.figures", "geometry question must retain at least one figure"}
	}
	if question.Diagram == nil {
		return nil
	}
	for i, figure := range question.Diagram.Figures {
		if len(figure.Points) == 0 && len(figure.Segments) == 0 {
			return &visionValidationError{fmt.Sprintf("diagram.figures[%d]", i), "must contain points or segments"}
		}
		pointSet := make(map[string]bool, len(figure.Points))
		for _, point := range figure.Points {
			pointSet[strings.TrimSpace(point)] = true
		}
		for j, intersection := range figure.Intersections {
			if strings.TrimSpace(intersection.Point) == "" {
				return &visionValidationError{fmt.Sprintf("diagram.figures[%d].intersections[%d].point", i, j), "must not be empty"}
			}
			if len(intersection.Of) == 0 {
				return &visionValidationError{fmt.Sprintf("diagram.figures[%d].intersections[%d].of", i, j), "must not be empty"}
			}
			if !pointSet[intersection.Point] {
				return &visionValidationError{fmt.Sprintf("diagram.figures[%d].intersections[%d].point", i, j), "must exist in points"}
			}
		}
		for j, group := range figure.Collinear {
			if len(group) < 2 {
				return &visionValidationError{fmt.Sprintf("diagram.figures[%d].collinear[%d]", i, j), "must contain at least two points"}
			}
		}
		for j, curve := range figure.Curves {
			path := fmt.Sprintf("diagram.figures[%d].curves[%d]", i, j)
			if strings.ToLower(strings.TrimSpace(curve.Type)) != "arc" {
				return &visionValidationError{path + ".type", "must be arc"}
			}
			if len(curve.Endpoints) != 2 {
				return &visionValidationError{path + ".endpoints", "must contain exactly two endpoints"}
			}
			for _, point := range curve.Endpoints {
				if !pointSet[strings.TrimSpace(point)] {
					return &visionValidationError{path + ".endpoints", "must reference points in this figure"}
				}
			}
			if curve.Center != "" && !pointSet[strings.TrimSpace(curve.Center)] {
				return &visionValidationError{path + ".center", "must reference a point in this figure"}
			}
			sweep := strings.ToLower(strings.TrimSpace(curve.Sweep))
			if sweep != "" && sweep != "upper" && sweep != "lower" {
				return &visionValidationError{path + ".sweep", "must be upper or lower"}
			}
		}
		if figure.LayoutHints != nil {
			if figure.LayoutHints.AspectRatio < 0 {
				return &visionValidationError{fmt.Sprintf("diagram.figures[%d].layoutHints.aspectRatio", i), "must be greater than zero"}
			}
			orientation := strings.ToLower(strings.TrimSpace(figure.LayoutHints.Orientation))
			if orientation != "" && orientation != "portrait" && orientation != "landscape" {
				return &visionValidationError{fmt.Sprintf("diagram.figures[%d].layoutHints.orientation", i), "must be portrait or landscape"}
			}
			for point, anchor := range figure.LayoutHints.Anchors {
				if math.IsNaN(anchor.X) || math.IsNaN(anchor.Y) || math.IsInf(anchor.X, 0) || math.IsInf(anchor.Y, 0) || anchor.X < 0 || anchor.X > 1 || anchor.Y < 0 || anchor.Y > 1 {
					return &visionValidationError{fmt.Sprintf("diagram.figures[%d].layoutHints.anchors.%s", i, point), "x and y must be finite numbers between zero and one"}
				}
			}
		}
		for j, constraint := range figure.Constraints {
			path := fmt.Sprintf("diagram.figures[%d].constraints[%d]", i, j)
			constraint = canonicalDiagramConstraint(constraint)
			if constraint.Type == "" {
				return &visionValidationError{path + ".type", "must not be empty"}
			}
			// 约束引用不属于本图的点时必须阻断，避免模型字段合法但图形语义已经串图。
			if !constraintBelongsToFigure(figure, constraint) {
				return &visionValidationError{path, "references points that do not exist in this figure"}
			}
		}
	}
	return nil
}

func validationErrorPath(err error) string {
	var validationErr *visionValidationError
	if errors.As(err, &validationErr) {
		return validationErr.path
	}
	return "structuredQuestion"
}

func visionTraceResult(raw []byte, requestID string) domain.OCRResult {
	return domain.OCRResult{Provider: "deepseek-vision", RequestID: requestID, RawResponse: raw, CreatedAt: domain.Now()}
}

// decodeVisionQuestion 兼容视觉模型常见的自然字段名，并在关键字段缺失时立即报错。
// 不能让 json.Unmarshal 静默忽略 type/known/points，随后带着空 topic 进入解题阶段。
func decodeVisionQuestion(raw json.RawMessage, text string) (*questiondomain.StructuredQuestion, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("structuredQuestion is required")
	}
	// 先在供应商适配层完成字段归一化，再反序列化到稳定领域协议；业务层永远看不到 shape 数组等漂移形态。
	normalizedRaw, err := normalizeVisionQuestionPayload(raw)
	if err != nil {
		return nil, fmt.Errorf("normalize structuredQuestion: %w", err)
	}
	var value struct {
		Grade           int                             `json:"grade"`
		Topic           string                          `json:"topic"`
		Type            string                          `json:"type"`
		QuestionType    string                          `json:"questionType"`
		NormalizedText  string                          `json:"normalizedText"`
		KnowledgePoints []string                        `json:"knowledgePoints"`
		Tags            []string                        `json:"tags"`
		Formula         string                          `json:"formula"`
		ParameterSchema map[string]string               `json:"parameterSchema"`
		UncertainItems  []string                        `json:"uncertainItems"`
		Expressions     []questiondomain.MathExpression `json:"expressions"`
		Payload         *questiondomain.QuestionPayload `json:"payload"`
		Knowns          json.RawMessage                 `json:"knowns"`
		Known           json.RawMessage                 `json:"known"`
		KnownConditions json.RawMessage                 `json:"knownConditions"`
		Relations       json.RawMessage                 `json:"relations"`
		Asks            json.RawMessage                 `json:"asks"`
		Diagram         struct {
			Entities      []questiondomain.DiagramEntity         `json:"entities"`
			Relations     []questiondomain.DiagramRelation       `json:"relations"`
			Coordinates   map[string]questiondomain.DiagramPoint `json:"coordinates"`
			Points        []string                               `json:"points"`
			Segments      []string                               `json:"segments"`
			Intersections questiondomain.DiagramIntersections    `json:"intersections"`
			Collinear     [][]string                             `json:"collinear"`
			Figures       []questiondomain.DiagramFigure         `json:"figures"`
		} `json:"diagram"`
	}
	if err := json.Unmarshal(normalizedRaw, &value); err != nil {
		return nil, err
	}
	knowns, err := decodeKnowns(value.Knowns, value.Known, value.KnownConditions)
	if err != nil {
		return nil, fmt.Errorf("decode knowns: %w", err)
	}
	relationsText, err := decodeRelationsText(value.Relations)
	if err != nil {
		return nil, fmt.Errorf("decode relations: %w", err)
	}
	asksText, err := decodeAsksText(value.Asks)
	if err != nil {
		return nil, fmt.Errorf("decode asks: %w", err)
	}
	topic := strings.TrimSpace(value.Topic)
	if topic == "" {
		topic = strings.TrimSpace(value.Type)
	}
	if topic == "" {
		topic = strings.TrimSpace(value.QuestionType)
	}
	if strings.Contains(topic, "几何") || strings.Contains(topic, "证明") {
		topic = "geometry_numeric"
	}
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if value.NormalizedText == "" {
		value.NormalizedText = strings.TrimSpace(text)
	}
	entities := append([]questiondomain.DiagramEntity(nil), value.Diagram.Entities...)
	for _, point := range value.Diagram.Points {
		entities = append(entities, questiondomain.DiagramEntity{ID: point, Type: "point"})
	}
	relations := append([]questiondomain.DiagramRelation(nil), value.Diagram.Relations...)
	for _, segment := range value.Diagram.Segments {
		relations = append(relations, questiondomain.DiagramRelation{Type: "segment", Target: segment})
	}
	for _, intersection := range value.Diagram.Intersections {
		relations = append(relations, questiondomain.DiagramRelation{Type: "intersection", Target: intersection.Point + ":" + strings.Join(intersection.Of, "、")})
	}
	for _, points := range value.Diagram.Collinear {
		relations = append(relations, questiondomain.DiagramRelation{Type: "collinear", Points: points})
	}
	figures := value.Diagram.Figures
	for index := range figures {
		if figures[index].ID == "" {
			figures[index].ID = fmt.Sprintf("figure_%d", index+1)
		}
		if figures[index].Title == "" {
			figures[index].Title = fmt.Sprintf("图%d", index+1)
		}
	}
	if len(figures) == 0 && (len(value.Diagram.Points) > 0 || len(value.Diagram.Segments) > 0) {
		figures = []questiondomain.DiagramFigure{{
			ID: "figure_1", Title: "图1", Points: value.Diagram.Points, Segments: value.Diagram.Segments,
			Intersections: value.Diagram.Intersections, Collinear: value.Diagram.Collinear,
			Relations: value.Diagram.Relations, Coordinates: value.Diagram.Coordinates,
		}}
	}
	if !isGeometryTopic(topic) {
		// 非几何题的图形字段不能进入领域事实链路；即使供应商误返回 figures，也只能在适配层丢弃。
		figures = nil
	}
	uncertainItems := append([]string(nil), value.UncertainItems...)
	supplementTexts := append([]string{value.NormalizedText, text}, asksText...)
	stripUnboundFigureConstraints(figures, &uncertainItems, supplementTexts...)
	enrichDiagramConstraints(figures, knowns, relationsText, value.NormalizedText, text)
	return &questiondomain.StructuredQuestion{
		SchemaVersion: VisionSchemaVersion, Grade: value.Grade, Topic: topic, NormalizedText: value.NormalizedText,
		KnowledgePoints: value.KnowledgePoints, Tags: value.Tags, Formula: value.Formula,
		ParameterSchema: value.ParameterSchema, UncertainItems: uncertainItems,
		Expressions: value.Expressions, Payload: value.Payload,
		Diagram: &questiondomain.DiagramGraph{Entities: entities, Relations: relations, Coordinates: value.Diagram.Coordinates, Figures: figures},
		Knowns:  knowns, Relations: relationsText, Asks: asksText,
	}, nil
}

func isGeometryTopic(topic string) bool {
	topic = strings.ToLower(strings.TrimSpace(topic))
	return strings.Contains(topic, "geometry") || strings.Contains(topic, "几何") || strings.Contains(topic, "证明")
}

// stripUnboundFigureConstraints 防止待补全图把尚未绘制的点线伪装成当前图的既有事实。
// 约束文本仍进入 uncertainItems，交给后续补图/解题流程处理，而不是静默丢弃或猜测坐标。
func stripUnboundFigureConstraints(figures []questiondomain.DiagramFigure, uncertainItems *[]string, texts ...string) {
	// 只有题目明确要求补图时，缺失点才可能是“尚未绘制”；普通题目的缺失引用必须继续由严格校验拦截。
	allowIncompleteFigure := false
	for _, text := range texts {
		if strings.Contains(text, "补全") || strings.Contains(text, "待补") || strings.Contains(text, "尚未画") || strings.Contains(text, "需补") {
			allowIncompleteFigure = true
			break
		}
	}
	if !allowIncompleteFigure {
		return
	}
	for index := range figures {
		figure := &figures[index]
		kept := make([]questiondomain.DiagramConstraint, 0, len(figure.Constraints))
		for _, rawConstraint := range figure.Constraints {
			constraint := canonicalDiagramConstraint(rawConstraint)
			if constraintBelongsToFigure(*figure, constraint) {
				kept = append(kept, constraint)
				continue
			}
			*uncertainItems = append(*uncertainItems, fmt.Sprintf("%s 的 %s 约束引用了当前图中尚未绘制的点或线段", figure.ID, constraint.Type))
		}
		figure.Constraints = kept
	}
}

// enrichDiagramConstraints 把题面已识别出的数学不变量补入图形协议，避免模型把关系只放在 knowns/relations 时下游丢失。
// 这里只做确定性模式收敛，不根据图形比例猜测关系；无法确认的内容继续留在原始文本中。
func enrichDiagramConstraints(figures []questiondomain.DiagramFigure, knowns []questiondomain.Known, relations []string, extra ...string) {
	texts := make([]string, 0, len(knowns)+len(relations)+len(extra))
	for _, known := range knowns {
		texts = append(texts, known.Name, known.Value)
	}
	texts = append(texts, relations...)
	// 正式题面（normalizedText 与原始 text）里也可能包含“∠C=90°”等关系，模型未必同步写进 knowns/relations。
	texts = append(texts, extra...)
	for index := range figures {
		for _, text := range texts {
			addConstraintFromText(&figures[index], text)
		}
	}
}

// normalizeConstraintText 把题面中常见的全角符号归一为半角，保证约束正则能稳定命中。
// 视觉模型常把等号/不等号输出成全角（＝、＞、＜），若不归一化会导致“∠C＝90°”无法被提取。
func normalizeConstraintText(text string) string {
	return strings.NewReplacer(
		" ", "",
		"　", "",
		"：", ":",
		"＝", "=",
		"＞", ">",
		"＜", "<",
	).Replace(text)
}

func addConstraintFromText(figure *questiondomain.DiagramFigure, text string) {
	compact := normalizeConstraintText(text)
	if matches := regexp.MustCompile(`∠([A-Za-z]{1,3})=?90`).FindStringSubmatch(compact); len(matches) == 2 {
		anglePoints := []rune(strings.ToUpper(matches[1]))
		point := string(anglePoints[0])
		if len(anglePoints) == 3 {
			point = string(anglePoints[1])
		}
		appendDiagramConstraint(figure, questiondomain.DiagramConstraint{Type: "right_angle", Points: []string{point}})
	}
	if matches := regexp.MustCompile(`([A-Za-z]{2})[＞>]([A-Za-z]{2})`).FindStringSubmatch(compact); len(matches) == 3 {
		appendDiagramConstraint(figure, questiondomain.DiagramConstraint{Type: "length_compare", Segments: []string{strings.ToUpper(matches[1]), strings.ToUpper(matches[2])}, Operator: ">"})
	}
	if matches := regexp.MustCompile(`([A-Za-z])(?:是|为)([A-Za-z]{2})的中点`).FindStringSubmatch(compact); len(matches) == 3 {
		appendDiagramConstraint(figure, questiondomain.DiagramConstraint{Type: "midpoint", Point: strings.ToUpper(matches[1]), Segment: strings.ToUpper(matches[2])})
	}
	if matches := regexp.MustCompile(`([A-Za-z]{2})[⊥⟂]([A-Za-z]{2})`).FindStringSubmatch(compact); len(matches) == 3 {
		appendDiagramConstraint(figure, questiondomain.DiagramConstraint{Type: "perpendicular", Segments: []string{strings.ToUpper(matches[1]), strings.ToUpper(matches[2])}})
	}
}

func appendDiagramConstraint(figure *questiondomain.DiagramFigure, candidate questiondomain.DiagramConstraint) {
	candidate = canonicalDiagramConstraint(candidate)
	// 多图题只能接收本图点集可承载的关系，防止图1的辅助线污染图2。
	if !constraintBelongsToFigure(*figure, candidate) {
		return
	}
	for _, existing := range figure.Constraints {
		existing = canonicalDiagramConstraint(existing)
		if existing.Type == candidate.Type && strings.Join(existing.Points, ",") == strings.Join(candidate.Points, ",") && strings.Join(existing.Lines, ",") == strings.Join(candidate.Lines, ",") && strings.Join(existing.Segments, ",") == strings.Join(candidate.Segments, ",") && existing.Operator == candidate.Operator {
			return
		}
	}
	figure.Constraints = append(figure.Constraints, candidate)
}

func canonicalDiagramConstraint(value questiondomain.DiagramConstraint) questiondomain.DiagramConstraint {
	value.Type = canonicalConstraintType(value.Type)
	if len(value.Points) == 0 && value.Point != "" {
		value.Points = []string{value.Point}
	}
	if len(value.Points) == 0 && value.At != "" {
		value.Points = []string{value.At}
	}
	if len(value.Segments) == 0 && value.Segment != "" {
		value.Segments = []string{value.Segment}
	}
	return value
}

func constraintBelongsToFigure(figure questiondomain.DiagramFigure, constraint questiondomain.DiagramConstraint) bool {
	pointSet := make(map[string]bool, len(figure.Points))
	for _, point := range figure.Points {
		pointSet[strings.ToUpper(strings.TrimSpace(point))] = true
	}
	for _, segment := range figure.Segments {
		for _, point := range geometryReferencePoints(segment) {
			pointSet[point] = true
		}
	}
	if len(pointSet) == 0 {
		return true
	}
	references := append([]string(nil), constraint.Points...)
	references = append(references, constraint.Point, constraint.At)
	for _, line := range append(append([]string(nil), constraint.Lines...), constraint.Segments...) {
		references = append(references, geometryReferencePoints(line)...)
	}
	if constraint.Segment != "" {
		references = append(references, geometryReferencePoints(constraint.Segment)...)
	}
	for _, point := range references {
		point = strings.ToUpper(strings.TrimSpace(point))
		if point != "" && !pointSet[point] {
			return false
		}
	}
	return true
}

func geometryReferencePoints(value string) []string {
	letters := regexp.MustCompile(`[A-Za-z]`).FindAllString(strings.ToUpper(value), -1)
	if len(letters) > 3 {
		return nil
	}
	return letters
}

// decodeKnowns 兼容模型把已知条件返回为对象数组或简写字符串数组的情况。
// 统一转换为领域模型，避免供应商字段形态变化直接中断整条识别流水线。
func decodeKnowns(primary, fallback, alternate json.RawMessage) ([]questiondomain.Known, error) {
	items, err := rawList(primary)
	if err != nil || len(items) == 0 {
		items, err = rawList(fallback)
	}
	if err != nil || len(items) == 0 {
		items, err = rawList(alternate)
	}
	if err != nil {
		return nil, err
	}
	knowns := make([]questiondomain.Known, 0, len(items))
	for _, item := range items {
		var value struct {
			Name  string `json:"name"`
			Value string `json:"value"`
			Unit  string `json:"unit"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal(item, &value); err == nil && (value.Name != "" || value.Value != "" || value.Unit != "" || value.Text != "") {
			// 富对象协议中的 text 承载完整已知条件；内部 Known 没有供应商 id/type/points 字段，统一落到 Name。
			if strings.TrimSpace(value.Name) == "" {
				value.Name = strings.TrimSpace(value.Text)
			}
			known := questiondomain.Known{Name: value.Name, Value: value.Value, Unit: value.Unit}
			knowns = append(knowns, known)
			continue
		}
		var text string
		if err := json.Unmarshal(item, &text); err != nil {
			return nil, err
		}
		if strings.TrimSpace(text) != "" {
			knowns = append(knowns, questiondomain.Known{Name: strings.TrimSpace(text)})
		}
	}
	return knowns, nil
}

// decodeAsksText 将结构化问题目标压缩为旧领域模型支持的可读文本；对象内容仍由原始响应和图形结构保留。
func decodeAsksText(raw json.RawMessage) ([]string, error) {
	items, err := rawList(raw)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		var text string
		if err := json.Unmarshal(item, &text); err == nil {
			if strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
			continue
		}
		var value map[string]any
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, err
		}
		// 供应商的 asks 对象字段并不稳定：优先保留真实问题文本，只有确实没有文本时才退化为 type+target。
		handled := false
		for _, key := range []string{"text", "question", "goal", "task"} {
			if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
				result = appendNonEmpty(result, text)
				handled = true
				break
			}
		}
		if handled {
			continue
		}
		if name, _ := value["name"].(string); strings.TrimSpace(name) != "" {
			// name/value 是历史富对象协议；value 为空时仍保留 name，避免整个 asks 被误判为空。
			if val, _ := value["value"].(string); strings.TrimSpace(val) != "" {
				unit, _ := value["unit"].(string)
				result = appendNonEmpty(result, strings.TrimSpace(name)+"："+strings.TrimSpace(val)+strings.TrimSpace(unit))
			} else {
				result = appendNonEmpty(result, name)
			}
			continue
		}
		var ask questiondomain.QuestionAsk
		if target, ok := value["target"].(string); ok {
			value["target"] = []string{target}
			item, _ = json.Marshal(value)
		} else if statement, ok := value["statement"].(string); ok && strings.TrimSpace(statement) != "" {
			// statement 是部分公共题型响应中的目标字段，先收敛为内部 target，再沿用统一格式化逻辑。
			value["target"] = []string{statement}
			item, _ = json.Marshal(value)
		}
		if err := json.Unmarshal(item, &ask); err != nil {
			return nil, err
		}
		formatted := formatQuestionParts(ask.Type, ask.Target)
		// 只保存分类标签没有题意，必须让格式/语义校验失败并触发有限纠错，而不是伪装成有效 ask。
		if isAskTypeOnly(formatted, ask.Type, ask.Target) {
			return nil, &visionValidationError{path: "asks", message: "item must contain question text or target"}
		}
		result = appendNonEmpty(result, formatted)
	}
	return result, nil
}

func isAskTypeOnly(formatted, kind string, target []string) bool {
	return strings.TrimSpace(formatted) == strings.TrimSpace(kind) && strings.TrimSpace(kind) != "" && len(target) == 0
}

func decodeRelationsText(raw json.RawMessage) ([]string, error) {
	items, err := rawList(raw)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		var text string
		if err := json.Unmarshal(item, &text); err == nil {
			if strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
			continue
		}
		var rich map[string]any
		if err := json.Unmarshal(item, &rich); err == nil {
			if name, _ := rich["name"].(string); strings.TrimSpace(name) != "" {
				value, _ := rich["value"].(string)
				result = appendNonEmpty(result, strings.TrimSpace(name)+func() string {
					if strings.TrimSpace(value) == "" {
						return ""
					}
					return "：" + strings.TrimSpace(value)
				}())
				continue
			}
		}
		var relation questiondomain.QuestionRelation
		if err := json.Unmarshal(item, &relation); err != nil {
			return nil, err
		}
		parts := append([]string{}, relation.Points...)
		parts = append(parts, relation.Segments...)
		parts = append(parts, relation.Lines...)
		parts = append(parts, relation.Point)
		parts = append(parts, relation.Target...)
		result = appendNonEmpty(result, formatQuestionParts(relation.Type, parts))
	}
	return result, nil
}

func rawList(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func formatQuestionParts(kind string, parts []string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			values = append(values, strings.TrimSpace(part))
		}
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return strings.Join(values, "、")
	}
	if len(values) == 0 {
		return kind
	}
	return kind + "：" + strings.Join(values, "、")
}

func appendNonEmpty(items []string, value string) []string {
	if strings.TrimSpace(value) != "" {
		return append(items, strings.TrimSpace(value))
	}
	return items
}
