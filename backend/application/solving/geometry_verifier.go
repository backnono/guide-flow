package solving

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type GeometryVerifier struct{}

var (
	perpendicularPattern = regexp.MustCompile(`(?i)^(?:perpendicular|prove_perpendicular)\(([^,]+),([^\)]+)\)$`)
	parallelPattern      = regexp.MustCompile(`(?i)^parallel\(([^,]+),([^\)]+)\)$`)
	dotPattern           = regexp.MustCompile(`(?i)^dot\(([^,]+),([^\)]+)\)=0$`)
	lengthPattern        = regexp.MustCompile(`(?i)^length\(([^\)]+)\)=([+-]?\d+(?:\.\d+)?)$`)
)

// VerifyDraft 校验显式坐标题的几何关系；没有坐标或表达式超出白名单时不做猜测。
func (GeometryVerifier) VerifyDraft(_ context.Context, question domain.StructuredQuestion, draft domain.SolutionDraft) (domain.VerifiedSolution, error) {
	result := domain.VerifiedSolution{
		Parameters:        draft.Variables,
		Expressions:       append([]string(nil), draft.Expressions...),
		IntermediateSteps: append([]string(nil), draft.Steps...),
		FinalAnswer:       draft.FinalAnswer,
	}
	if len(draft.Uncertainties) > 0 {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Checks: draft.Uncertainties, Message: "几何草稿包含未确定项"}
		return result, nil
	}
	if question.Diagram == nil || len(question.Diagram.Coordinates) == 0 {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationUnsupported, Message: "缺少明确坐标，当前几何校验器不根据图形比例推断"}
		return result, nil
	}
	if len(draft.Expressions) == 0 {
		result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Message: "几何草稿缺少可校验关系"}
		return result, nil
	}
	checks := make([]string, 0, len(draft.Expressions))
	for _, expression := range draft.Expressions {
		check, err := verifyGeometryExpression(question.Diagram, strings.ReplaceAll(expression, " ", ""))
		if err != nil {
			result.Validation = domain.ValidationEvidence{Status: domain.ValidationNeedsConfirmation, Checks: checks, Message: err.Error()}
			return result, nil
		}
		checks = append(checks, check)
	}
	result.Validation = domain.ValidationEvidence{Status: domain.ValidationVerified, Checks: checks}
	return result, nil
}

func verifyGeometryExpression(diagram *domain.DiagramGraph, expression string) (string, error) {
	if match := perpendicularPattern.FindStringSubmatch(expression); match != nil {
		first, err := vectorForLine(diagram, match[1])
		if err != nil {
			return "", err
		}
		second, err := vectorForLine(diagram, match[2])
		if err != nil {
			return "", err
		}
		if first.dot(second).Sign() != 0 {
			return "", fmt.Errorf("%s 不垂直", expression)
		}
		return expression + " 已通过点积为零检查", nil
	}
	if match := parallelPattern.FindStringSubmatch(expression); match != nil {
		first, err := vectorForLine(diagram, match[1])
		if err != nil {
			return "", err
		}
		second, err := vectorForLine(diagram, match[2])
		if err != nil {
			return "", err
		}
		if first.cross(second).Sign() != 0 {
			return "", fmt.Errorf("%s 不平行", expression)
		}
		return expression + " 已通过叉积为零检查", nil
	}
	if match := dotPattern.FindStringSubmatch(expression); match != nil {
		first, err := vectorForLine(diagram, match[1])
		if err != nil {
			return "", err
		}
		second, err := vectorForLine(diagram, match[2])
		if err != nil {
			return "", err
		}
		if first.dot(second).Sign() != 0 {
			return "", fmt.Errorf("%s 的点积不为零", expression)
		}
		return expression + " 已通过点积检查", nil
	}
	if match := lengthPattern.FindStringSubmatch(expression); match != nil {
		line, err := vectorForLine(diagram, match[1])
		if err != nil {
			return "", err
		}
		expected, ok := new(big.Rat).SetString(match[2])
		if !ok || expected.Sign() < 0 {
			return "", fmt.Errorf("长度目标无效：%s", match[2])
		}
		actualSquared := line.dot(line)
		expectedSquared := new(big.Rat).Mul(expected, expected)
		if actualSquared.Cmp(expectedSquared) != 0 {
			return "", fmt.Errorf("%s 的长度不等于目标值", expression)
		}
		return expression + " 已通过长度平方检查", nil
	}
	return "", fmt.Errorf("不支持的几何校验表达式：%s", expression)
}

type vector struct{ x, y *big.Rat }

func vectorForLine(diagram *domain.DiagramGraph, line string) (vector, error) {
	line = strings.TrimSpace(line)
	if len([]rune(line)) != 2 {
		return vector{}, fmt.Errorf("线段必须由两个点组成：%s", line)
	}
	points := []rune(line)
	from, ok := diagram.Coordinates[string(points[0])]
	if !ok {
		return vector{}, fmt.Errorf("缺少点 %s 的坐标", string(points[0]))
	}
	to, ok := diagram.Coordinates[string(points[1])]
	if !ok {
		return vector{}, fmt.Errorf("缺少点 %s 的坐标", string(points[1]))
	}
	x1, ok := new(big.Rat).SetString(from.X)
	if !ok {
		return vector{}, fmt.Errorf("点 %s 的横坐标无效", string(points[0]))
	}
	y1, ok := new(big.Rat).SetString(from.Y)
	if !ok {
		return vector{}, fmt.Errorf("点 %s 的纵坐标无效", string(points[0]))
	}
	x2, ok := new(big.Rat).SetString(to.X)
	if !ok {
		return vector{}, fmt.Errorf("点 %s 的横坐标无效", string(points[1]))
	}
	y2, ok := new(big.Rat).SetString(to.Y)
	if !ok {
		return vector{}, fmt.Errorf("点 %s 的纵坐标无效", string(points[1]))
	}
	return vector{x: new(big.Rat).Sub(x2, x1), y: new(big.Rat).Sub(y2, y1)}, nil
}

func (v vector) dot(other vector) *big.Rat {
	return new(big.Rat).Add(new(big.Rat).Mul(v.x, other.x), new(big.Rat).Mul(v.y, other.y))
}

func (v vector) cross(other vector) *big.Rat {
	return new(big.Rat).Sub(new(big.Rat).Mul(v.x, other.y), new(big.Rat).Mul(v.y, other.x))
}
