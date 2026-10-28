package ai

import (
	"context"
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestRuleBasedStructurerDoesNotRewriteGeometryAsFraction(t *testing.T) {
	question, err := (RuleBasedStructurer{}).Structure(
		context.Background(),
		domain.QuestionDraft{Text: "已知四边形 ABCD 是矩形，求证 BD 垂直 EC", Confidence: .95},
		domain.LearnerProfile{Grade: 5},
	)
	if err != nil {
		t.Fatal(err)
	}
	if question.Topic != "geometry_numeric" {
		t.Fatalf("expected geometry topic, got %+v", question)
	}
	if question.NormalizedText == "" || len(question.Knowns) != 0 {
		t.Fatalf("structurer must preserve text without inventing knowns: %+v", question)
	}
}

func TestRuleBasedStructurerRoutesPlainTextWithoutFakeNumbers(t *testing.T) {
	question, err := (RuleBasedStructurer{}).Structure(
		context.Background(),
		domain.QuestionDraft{Text: "小明有一些铅笔，送出一部分后还剩多少？", Confidence: .95},
		domain.LearnerProfile{Grade: 5},
	)
	if err != nil {
		t.Fatal(err)
	}
	if question.Topic != "word_problem" || len(question.Knowns) != 0 {
		t.Fatalf("structurer returned fabricated structure: %+v", question)
	}
}

func TestAdaptivePlannerUsesVerifiedSolutionInsteadOfDemoContent(t *testing.T) {
	plan, err := (AdaptivePlanner{}).Plan(
		context.Background(),
		domain.StructuredQuestion{Topic: "geometry_numeric"},
		domain.VerifiedSolution{
			IntermediateSteps: []string{"由已知条件得到 AB = 1", "代入关系得到 AE = 2"},
			Expressions:       []string{"AB = 1", "AE = 2"},
			FinalAnswer:       "AE = 2",
		},
		domain.LearnerProfile{Grade: 9},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Steps[1].Formula != "AE = 2" || plan.FinalAnswer != "AE = 2" {
		t.Fatalf("planner did not project verified solution: %+v", plan)
	}
	if plan.Practice.Question != "" || plan.Practice.Answer != "" {
		t.Fatalf("planner must not inject a fixed demo practice: %+v", plan.Practice)
	}
}
