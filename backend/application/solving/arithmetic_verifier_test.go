package solving

import (
	"context"
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestArithmeticVerifierVerifiesExactFractionSteps(t *testing.T) {
	verified, err := (ArithmeticVerifier{}).VerifyDraft(context.Background(), domain.StructuredQuestion{Topic: "fraction_word_problem"}, domain.SolutionDraft{
		Expressions: []string{"120 * (1 - 1/3) = 80", "80 * 1/4 = 20"},
		Steps:       []string{"先算剩余页数", "再算剩余页数的四分之一"},
		FinalAnswer: "20 页",
	})
	if err != nil || !verified.IsVerified() {
		t.Fatalf("expected verified result: %+v, err=%v", verified, err)
	}
}

func TestArithmeticVerifierRejectsWrongAnswer(t *testing.T) {
	verified, err := (ArithmeticVerifier{}).VerifyDraft(context.Background(), domain.StructuredQuestion{Topic: "arithmetic"}, domain.SolutionDraft{
		Expressions: []string{"1 + 1 = 2"}, FinalAnswer: "3",
	})
	if err != nil || verified.Validation.Status != domain.ValidationRejected {
		t.Fatalf("expected rejected result: %+v, err=%v", verified, err)
	}
}

func TestArithmeticVerifierMarksUnsupportedTopic(t *testing.T) {
	verified, err := (ArithmeticVerifier{}).VerifyDraft(context.Background(), domain.StructuredQuestion{Topic: "geometry_proof"}, domain.SolutionDraft{
		Expressions: []string{"1 + 1 = 2"}, FinalAnswer: "2",
	})
	if err != nil || verified.Validation.Status != domain.ValidationUnsupported {
		t.Fatalf("expected unsupported result: %+v, err=%v", verified, err)
	}
}
