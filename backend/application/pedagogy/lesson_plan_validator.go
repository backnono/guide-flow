package pedagogy

import (
	"fmt"
	"strings"

	"github.com/guide-flow/guide-flow/backend/application/solving"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

var allowedVisualTypes = map[string]struct{}{
	"ratio_bar": {}, "groups": {}, "number_line": {}, "formula_reveal": {},
}

// LessonPlanValidator 保证讲解只是已验证解法的教学投影，不能篡改最终答案或输出未定义的视觉类型。
type LessonPlanValidator struct{}

func (LessonPlanValidator) Validate(plan domain.LessonPlan, verified domain.VerifiedSolution) error {
	if !verified.IsVerified() {
		return fmt.Errorf("verified solution is not verified")
	}
	if strings.TrimSpace(plan.Title) == "" || strings.TrimSpace(plan.FinalAnswer) == "" {
		return fmt.Errorf("lesson plan title and finalAnswer are required")
	}
	if plan.FinalAnswer != verified.FinalAnswer {
		return fmt.Errorf("lesson plan finalAnswer differs from verified solution")
	}
	if len(plan.Steps) == 0 || len(plan.Steps) > 3 {
		return fmt.Errorf("lesson plan steps must contain 1 to 3 items")
	}
	for index, step := range plan.Steps {
		if strings.TrimSpace(step.Title) == "" || strings.TrimSpace(step.Explanation) == "" || strings.TrimSpace(step.Formula) == "" || strings.TrimSpace(step.VoiceText) == "" {
			return fmt.Errorf("lesson step %d has empty required content", index+1)
		}
		if _, ok := allowedVisualTypes[step.Visual.Type]; !ok {
			return fmt.Errorf("lesson step %d uses unsupported visual type %q", index+1, step.Visual.Type)
		}
		for _, segment := range step.Visual.Segments {
			if segment <= 0 {
				return fmt.Errorf("lesson step %d contains a non-positive visual segment", index+1)
			}
		}
	}
	// 相似练习是可选能力；没有可靠的题型参数时宁可不生成，也不能回退到固定 Demo 题污染当前讲解。
	if strings.TrimSpace(plan.Practice.Question) == "" && strings.TrimSpace(plan.Practice.Answer) == "" {
		return nil
	}
	if strings.TrimSpace(plan.Practice.Question) == "" || strings.TrimSpace(plan.Practice.Answer) == "" {
		return fmt.Errorf("practice question and answer must be provided together")
	}
	if strings.TrimSpace(plan.Practice.CheckExpression) != "" {
		practiceResult, err := (solving.ArithmeticVerifier{}).VerifyDraft(nil, domain.StructuredQuestion{Topic: "arithmetic"}, domain.SolutionDraft{
			Expressions: []string{plan.Practice.CheckExpression},
			FinalAnswer: plan.Practice.Answer,
		})
		if err != nil {
			return fmt.Errorf("practice answer validation failed: %w", err)
		}
		if !practiceResult.IsVerified() {
			return fmt.Errorf("practice answer validation failed: %s", practiceResult.Validation.Message)
		}
	}
	return nil
}
