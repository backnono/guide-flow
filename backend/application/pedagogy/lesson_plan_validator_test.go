package pedagogy

import (
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func validPlan() domain.LessonPlan {
	return domain.LessonPlan{
		Title: "讲解", FinalAnswer: "20 页",
		Steps:    []domain.LessonStep{{Title: "第一步", Explanation: "先算剩余部分", Formula: "1-1/3", VoiceText: "我们先算剩余部分", Visual: domain.Visual{Type: "formula_reveal"}}},
		Practice: domain.Practice{Question: "1+1=?", Answer: "2"},
	}
}

func validSolution() domain.VerifiedSolution {
	return domain.VerifiedSolution{FinalAnswer: "20 页", Validation: domain.ValidationEvidence{Status: domain.ValidationVerified}}
}

func TestLessonPlanValidatorAcceptsVerifiedProjection(t *testing.T) {
	if err := (LessonPlanValidator{}).Validate(validPlan(), validSolution()); err != nil {
		t.Fatal(err)
	}
}

func TestLessonPlanValidatorRejectsChangedFinalAnswer(t *testing.T) {
	plan := validPlan()
	plan.FinalAnswer = "21 页"
	if err := (LessonPlanValidator{}).Validate(plan, validSolution()); err == nil {
		t.Fatal("expected final answer mismatch")
	}
}

func TestLessonPlanValidatorRejectsUnknownVisual(t *testing.T) {
	plan := validPlan()
	plan.Steps[0].Visual.Type = "geometry_overlay"
	if err := (LessonPlanValidator{}).Validate(plan, validSolution()); err == nil {
		t.Fatal("expected unknown visual type rejection")
	}
}

func TestLessonPlanValidatorChecksPracticeExpression(t *testing.T) {
	plan := validPlan()
	plan.Practice.Answer = "12"
	plan.Practice.CheckExpression = "48 * 1/4 = 12"
	if err := (LessonPlanValidator{}).Validate(plan, validSolution()); err != nil {
		t.Fatal(err)
	}
	plan.Practice.Answer = "13"
	if err := (LessonPlanValidator{}).Validate(plan, validSolution()); err == nil {
		t.Fatal("expected practice answer mismatch")
	}
}
