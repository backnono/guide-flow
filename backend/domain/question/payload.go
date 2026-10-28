package question

import (
	"encoding/json"
	"fmt"
	"strings"
)

// QuestionPayload 是通用题目外壳与题型细节之间的扩展 seam。
// 新题型只需增加 kind 对应的载荷和处理器，不需要继续扩张 StructuredQuestion 的公共字段。
type QuestionPayload struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// MathExpression 同时保留原始公式和规范化表达式，避免 OCR/视觉模型改写公式后无法追溯。
// 当前题型暂不强制填写 LaTeX；后续函数、根式和三角函数题可在题型模块中补充校验。
type MathExpression struct {
	Raw       string   `json:"raw"`
	LaTeX     string   `json:"latex,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Variables []string `json:"variables,omitempty"`
	Uncertain []string `json:"uncertainItems,omitempty"`
}

// UnmarshalJSON 兼容模型将公式返回为 string[] 或 object[] 的两种协议，领域层统一保留为对象。
// 字符串形式只填 Raw，不臆造 LaTeX 或表达式类型，避免把 OCR 结果误当成规范公式。
func (expression *MathExpression) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err == nil {
		if strings.TrimSpace(raw) == "" {
			return fmt.Errorf("math expression must not be empty")
		}
		expression.Raw = strings.TrimSpace(raw)
		expression.LaTeX = ""
		expression.Kind = ""
		expression.Variables = nil
		expression.Uncertain = nil
		return nil
	}
	type expressionAlias MathExpression
	var value expressionAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if strings.TrimSpace(value.Raw) == "" {
		return fmt.Errorf("math expression raw must not be empty")
	}
	*expression = MathExpression(value)
	return nil
}
