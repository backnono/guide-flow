package solving

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"unicode"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type ArithmeticVerifier struct{}

// VerifyDraft 只验证可转换为精确有理数的算式，不使用浮点数，避免分数题出现精度误判。
func (ArithmeticVerifier) VerifyDraft(_ context.Context, question domain.StructuredQuestion, draft domain.SolutionDraft) (domain.VerifiedSolution, error) {
	result := domain.VerifiedSolution{
		Parameters:        draft.Variables,
		Expressions:       append([]string(nil), draft.Expressions...),
		IntermediateSteps: append([]string(nil), draft.Steps...),
		FinalAnswer:       draft.FinalAnswer,
	}
	if len(draft.Uncertainties) > 0 {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Checks: draft.Uncertainties, Message: "解题草稿包含未确定项"}
		return result, nil
	}
	if !supportedTopic(question.Topic) {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationUnsupported, Message: "当前校验器不支持该题型"}
		return result, nil
	}
	if len(draft.Expressions) == 0 || strings.TrimSpace(draft.FinalAnswer) == "" {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Message: "草稿缺少可校验算式或最终答案"}
		return result, nil
	}
	checks := make([]string, 0, len(draft.Expressions)+1)
	var lastRight *big.Rat
	for _, expression := range draft.Expressions {
		left, right, err := splitEquation(expression)
		if err != nil {
			result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Checks: checks, Message: err.Error()}
			return result, nil
		}
		leftValue, err := parseArithmetic(left)
		if err != nil {
			result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Checks: checks, Message: fmt.Sprintf("无法计算左侧表达式：%v", err)}
			return result, nil
		}
		rightValue, err := parseArithmetic(right)
		if err != nil {
			result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Checks: checks, Message: fmt.Sprintf("无法计算右侧表达式：%v", err)}
			return result, nil
		}
		if leftValue.Cmp(rightValue) != 0 {
			result.Validation = domain.ValidationEvidence{Status: domain.ValidationRejected, Checks: checks, Message: "算式等式不成立"}
			return result, nil
		}
		lastRight = rightValue
		checks = append(checks, expression+" 等式成立")
	}
	answer, err := parseAnswer(draft.FinalAnswer)
	if err != nil {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Checks: checks, Message: err.Error()}
		return result, nil
	}
	if lastRight == nil || answer.Cmp(lastRight) != 0 {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationRejected, Checks: checks, Message: "最终答案与最后一个校验结果不一致"}
		return result, nil
	}
	checks = append(checks, "最终答案与最后一步结果一致")
	result.Validation = domain.ValidationEvidence{Status: domain.ValidationVerified, Checks: checks}
	return result, nil
}

func supportedTopic(topic string) bool {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "arithmetic", "fraction", "equation", "word_problem", "fraction_word_problem":
		return true
	default:
		return false
	}
}

func splitEquation(expression string) (string, string, error) {
	parts := strings.Split(expression, "=")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("算式必须包含一个等号：%s", expression)
	}
	return parts[0], parts[1], nil
}

var answerNumber = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?(?:\s*/\s*[-+]?\d+)?`)

func parseAnswer(answer string) (*big.Rat, error) {
	value := answerNumber.FindString(answer)
	if value == "" {
		return nil, fmt.Errorf("最终答案不是可解析的数值：%s", answer)
	}
	return parseArithmetic(value)
}

type parser struct {
	tokens []rune
	pos    int
}

func parseArithmetic(expression string) (*big.Rat, error) {
	p := &parser{tokens: []rune(strings.ReplaceAll(strings.TrimSpace(expression), " ", ""))}
	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.tokens) {
		return nil, fmt.Errorf("无法解析表达式：%s", expression)
	}
	return value, nil
}

func (p *parser) parseExpression() (*big.Rat, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for p.pos < len(p.tokens) && (p.tokens[p.pos] == '+' || p.tokens[p.pos] == '-') {
		op := p.tokens[p.pos]
		p.pos++
		right, rightErr := p.parseTerm()
		if rightErr != nil {
			return nil, rightErr
		}
		if op == '+' {
			left.Add(left, right)
		} else {
			left.Sub(left, right)
		}
	}
	return left, nil
}

func (p *parser) parseTerm() (*big.Rat, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for p.pos < len(p.tokens) && (p.tokens[p.pos] == '*' || p.tokens[p.pos] == '/') {
		op := p.tokens[p.pos]
		p.pos++
		right, rightErr := p.parseFactor()
		if rightErr != nil {
			return nil, rightErr
		}
		if op == '*' {
			left.Mul(left, right)
		} else if right.Sign() == 0 {
			return nil, fmt.Errorf("除数不能为零")
		} else {
			left.Quo(left, right)
		}
	}
	return left, nil
}

func (p *parser) parseFactor() (*big.Rat, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("表达式意外结束")
	}
	if p.tokens[p.pos] == '+' || p.tokens[p.pos] == '-' {
		negative := p.tokens[p.pos] == '-'
		p.pos++
		value, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		if negative {
			value.Neg(value)
		}
		return value, nil
	}
	if p.tokens[p.pos] == '(' {
		p.pos++
		value, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.pos >= len(p.tokens) || p.tokens[p.pos] != ')' {
			return nil, fmt.Errorf("括号不匹配")
		}
		p.pos++
		return value, nil
	}
	start := p.pos
	for p.pos < len(p.tokens) && (unicode.IsDigit(p.tokens[p.pos]) || p.tokens[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return nil, fmt.Errorf("表达式包含不支持的字符")
	}
	value, ok := new(big.Rat).SetString(string(p.tokens[start:p.pos]))
	if !ok {
		return nil, fmt.Errorf("数字格式无效")
	}
	return value, nil
}
