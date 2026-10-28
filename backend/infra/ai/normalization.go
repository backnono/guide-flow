package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

// NormalizationReport 描述供应商响应如何被收敛为内部协议，便于发现新响应形状而不把供应商字段扩散到业务层。
type NormalizationReport struct {
	SchemaVersion string
	Shape         string
	Warnings      []string
	UnknownPaths  []string
}

// DraftNormalizer 是供应商适配层的唯一边界；Worker 和 Verifier 只接收归一化后的 SolutionDraft。
type DraftNormalizer interface {
	Normalize(content string) (domain.SolutionDraft, NormalizationReport, error)
}

// DeepSeekDraftNormalizer 只识别已登记的 DeepSeek 响应形状，未知结构必须失败并保留原文，禁止递归猜字段。
type DeepSeekDraftNormalizer struct{}

type deepSeekStructuredDraft struct {
	Uncertainties     []string               `json:"uncertainties"`
	SolutionDraft     []deepSeekSolutionPart `json:"solutionDraft"`
	FinalAnswersDraft []string               `json:"finalAnswersDraft"`
	Verification      struct {
		UnclearPoints []string `json:"unclearPoints"`
	} `json:"verification"`
}

type deepSeekSolutionPart struct {
	Steps      []deepSeekSolutionStep `json:"steps"`
	Conclusion string                 `json:"conclusion"`
}

type deepSeekSolutionStep struct {
	Text string `json:"text"`
}

func (DeepSeekDraftNormalizer) Normalize(content string) (domain.SolutionDraft, NormalizationReport, error) {
	content = strings.TrimSpace(extractJSONObject(content))
	if content == "" {
		return domain.SolutionDraft{}, NormalizationReport{}, fmt.Errorf("DeepSeek assistant content is empty")
	}
	draft, report, found, err := normalizeDeepSeekNode([]byte(content), "$", 0)
	if err != nil {
		return domain.SolutionDraft{}, report, err
	}
	if !found {
		return domain.SolutionDraft{}, NormalizationReport{Warnings: []string{"unsupported_response_shape"}}, fmt.Errorf("unsupported DeepSeek draft response shape")
	}
	return draft, report, nil
}

func normalizeDeepSeekNode(raw []byte, path string, depth int) (domain.SolutionDraft, NormalizationReport, bool, error) {
	if depth > 3 {
		return domain.SolutionDraft{}, NormalizationReport{}, false, nil
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &shape); err != nil {
		return domain.SolutionDraft{}, NormalizationReport{}, false, err
	}
	// 先处理已登记的“固定协议字段类型漂移”，例如 DeepSeek 将 steps 从 string[] 返回为对象数组。
	// 这类确定性转换必须在适配层完成，领域层永远只看到 string[]。
	if hasCanonicalDraftKeys(shape) {
		canonical, report, err := normalizeDeepSeekCanonical(shape, path)
		if err != nil {
			return domain.SolutionDraft{}, report, false, err
		}
		return canonical, report, true, nil
	}
	var canonical domain.SolutionDraft
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return domain.SolutionDraft{}, NormalizationReport{}, false, err
	}
	if hasDraftContent(canonical) {
		if canonical.SchemaVersion == "" {
			canonical.SchemaVersion = solutiondomain.DraftSchemaVersion
		}
		return canonical, NormalizationReport{SchemaVersion: canonical.SchemaVersion, Shape: path + ":solution-draft-v1"}, true, nil
	}

	var structured deepSeekStructuredDraft
	if err := json.Unmarshal(raw, &structured); err != nil {
		return domain.SolutionDraft{}, NormalizationReport{}, false, err
	}
	if len(structured.SolutionDraft) > 0 || len(structured.FinalAnswersDraft) > 0 {
		result := domain.SolutionDraft{
			SchemaVersion: solutiondomain.DraftSchemaVersion,
			Method:        "structured_solution_draft",
			Uncertainties: append([]string(nil), structured.Uncertainties...),
		}
		result.Uncertainties = append(result.Uncertainties, structured.Verification.UnclearPoints...)
		for _, part := range structured.SolutionDraft {
			for _, step := range part.Steps {
				if text := strings.TrimSpace(step.Text); text != "" {
					result.Steps = append(result.Steps, text)
				}
			}
			if conclusion := strings.TrimSpace(part.Conclusion); conclusion != "" {
				result.Expressions = append(result.Expressions, conclusion)
			}
		}
		result.FinalAnswer = strings.Join(structured.FinalAnswersDraft, "\n")
		return result, NormalizationReport{SchemaVersion: result.SchemaVersion, Shape: path + ":deepseek-structured-draft-v1"}, true, nil
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return domain.SolutionDraft{}, NormalizationReport{}, false, err
	}
	// 只遍历已登记的传输包装字段，避免把任意业务字段误认成答案。
	for _, key := range []string{"content", "draft", "solution"} {
		nested, ok := envelope[key]
		if !ok || len(nested) == 0 || string(nested) == "null" {
			continue
		}
		draft, report, found, err := normalizeDeepSeekNode(nested, path+"."+key, depth+1)
		if err != nil {
			return domain.SolutionDraft{}, report, false, err
		}
		if found {
			return draft, report, true, nil
		}
	}
	return domain.SolutionDraft{}, NormalizationReport{}, false, nil
}

func hasCanonicalDraftKeys(shape map[string]json.RawMessage) bool {
	for _, key := range []string{"schemaVersion", "method", "steps", "expressions", "finalAnswer"} {
		if _, ok := shape[key]; ok {
			return true
		}
	}
	return false
}

func normalizeDeepSeekCanonical(shape map[string]json.RawMessage, path string) (domain.SolutionDraft, NormalizationReport, error) {
	result := domain.SolutionDraft{SchemaVersion: solutiondomain.DraftSchemaVersion}
	warnings := make([]string, 0)
	unknownPaths := make([]string, 0)
	decodeString(shape, "schemaVersion", &result.SchemaVersion)
	decodeString(shape, "method", &result.Method)
	decodeString(shape, "finalAnswer", &result.FinalAnswer)
	decodeStringSlice(shape, "assumptions", &result.Assumptions)
	decodeStringSlice(shape, "uncertainties", &result.Uncertainties)
	decodeStringMap(shape, "variables", &result.Variables)
	decodeDraftSteps(shape, &result.Steps, &result.Expressions, &warnings, &unknownPaths)
	var explicitExpressions []string
	decodeStringSlice(shape, "expressions", &explicitExpressions)
	if len(explicitExpressions) > 0 {
		result.Expressions = explicitExpressions
	}
	if result.SchemaVersion == "" {
		result.SchemaVersion = solutiondomain.DraftSchemaVersion
	}
	if len(warnings) > 0 {
		return result, NormalizationReport{SchemaVersion: result.SchemaVersion, Shape: path + ":solution-draft-v1", Warnings: warnings, UnknownPaths: unknownPaths}, nil
	}
	return result, NormalizationReport{SchemaVersion: result.SchemaVersion, Shape: path + ":solution-draft-v1", UnknownPaths: unknownPaths}, nil
}

func decodeString(shape map[string]json.RawMessage, key string, target *string) {
	if raw, ok := shape[key]; ok {
		_ = json.Unmarshal(raw, target)
	}
}

func decodeStringSlice(shape map[string]json.RawMessage, key string, target *[]string) {
	raw, ok := shape[key]
	if !ok {
		return
	}
	if json.Unmarshal(raw, target) == nil {
		return
	}
	var single string
	if json.Unmarshal(raw, &single) == nil && strings.TrimSpace(single) != "" {
		*target = []string{single}
	}
}

func decodeStringMap(shape map[string]json.RawMessage, key string, target *map[string]string) {
	raw, ok := shape[key]
	if !ok {
		return
	}
	if json.Unmarshal(raw, target) == nil {
		return
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return
	}
	result := make(map[string]string, len(values))
	for name, value := range values {
		var text string
		if json.Unmarshal(value, &text) == nil {
			result[name] = text
			continue
		}
		result[name] = strings.TrimSpace(string(value))
	}
	*target = result
}

func decodeDraftSteps(shape map[string]json.RawMessage, steps *[]string, expressions *[]string, warnings *[]string, unknownPaths *[]string) {
	raw, ok := shape["steps"]
	if !ok {
		return
	}
	if json.Unmarshal(raw, steps) == nil {
		return
	}
	var objects []map[string]json.RawMessage
	if json.Unmarshal(raw, &objects) != nil {
		return
	}
	// json.Unmarshal 在类型不匹配时可能已经写入部分空元素，切掉这些中间结果后再逐项归一化。
	*steps = nil
	for index, item := range objects {
		text, expression, inferred := normalizeStepObject(item)
		if text != "" {
			*steps = append(*steps, text)
		}
		if expression != "" {
			*expressions = append(*expressions, expression)
		}
		if inferred {
			*warnings = append(*warnings, "steps_semantic_field_inferred")
		}
		if text == "" {
			*unknownPaths = append(*unknownPaths, fmt.Sprintf("steps[%d]", index))
		}
	}
	*warnings = append(*warnings, "steps_object_items_normalized")
}

// normalizeStepObject 先使用稳定别名，再对未登记字段做受约束的语义推断。
// 推断只允许“唯一候选”或“一个自然语言字段 + 一个数学表达式字段”，否则保留未知路径并交给校验失败处理。
func normalizeStepObject(item map[string]json.RawMessage) (string, string, bool) {
	text := firstObjectString(item, "text", "description", "desc", "explanation", "detail")
	expression := firstObjectString(item, "expression", "formula", "expr", "equation")
	if text != "" || expression != "" {
		return text, expression, false
	}

	stringsByKey := make(map[string]string)
	for key, raw := range item {
		if isStepControlField(key) {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			stringsByKey[key] = strings.TrimSpace(value)
		}
	}
	if len(stringsByKey) == 0 {
		return "", "", false
	}
	if len(stringsByKey) == 1 {
		for _, value := range stringsByKey {
			return value, "", true
		}
	}

	var textKey, expressionKey string
	for key, value := range stringsByKey {
		if looksLikeExpression(value) {
			if expressionKey != "" {
				return "", "", false
			}
			expressionKey = key
			continue
		}
		if textKey != "" {
			return "", "", false
		}
		textKey = key
	}
	if textKey == "" || expressionKey == "" {
		return "", "", false
	}
	return stringsByKey[textKey], stringsByKey[expressionKey], true
}

func isStepControlField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "step", "index", "id", "part", "order", "type":
		return true
	default:
		return false
	}
}

func looksLikeExpression(value string) bool {
	if !strings.ContainsAny(value, "=∠/+-×÷∥<>^") {
		return false
	}
	// 说明文本可能同时包含“AB=AD”，长中文句子优先视为步骤描述，避免把整句误判成公式。
	chineseCount := 0
	for _, char := range value {
		if (char >= '\u4e00' && char <= '\u9fff') || (char >= '\u3400' && char <= '\u4dbf') {
			chineseCount++
		}
	}
	return chineseCount < 4 || len([]rune(value)) <= 8
}

func firstObjectString(item map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if raw, ok := item[key]; ok {
			var text string
			if json.Unmarshal(raw, &text) == nil && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func hasDraftContent(draft domain.SolutionDraft) bool {
	return strings.TrimSpace(draft.Method) != "" || strings.TrimSpace(draft.FinalAnswer) != "" || len(draft.Steps) > 0 || len(draft.Expressions) > 0
}

// decodeDraftContent 保留给适配层回放测试使用，业务代码只能依赖 DraftNormalizer。
func decodeDraftContent(content string) (domain.SolutionDraft, error) {
	draft, _, err := (DeepSeekDraftNormalizer{}).Normalize(content)
	return draft, err
}

// extractJSONObject 兼容模型附带说明文字或 Markdown 围栏，但只提取一个完整对象，不猜字段。
func extractJSONObject(content string) string {
	start := strings.IndexByte(content, '{')
	if start < 0 {
		return content
	}
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(content); index++ {
		char := content[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : index+1]
			}
		}
	}
	return content[start:]
}
