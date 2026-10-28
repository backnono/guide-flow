package research

import (
	"context"
	"testing"

	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
	researchdomain "github.com/guide-flow/guide-flow/backend/domain/research"
)

func TestValidateContextRejectsUsedResultWithoutVerifiableSources(t *testing.T) {
	for _, value := range []researchdomain.Context{
		{Status: researchdomain.StatusUsed},
		{Status: researchdomain.StatusUsed, Sources: []researchdomain.Source{{URL: "javascript:bad", Summary: "题解", TrustScore: .9}}},
		{Status: researchdomain.StatusUsed, Sources: []researchdomain.Source{{URL: "https://example.com", Summary: "", TrustScore: .9}}},
	} {
		if got := ValidateContext(value); got.Status != researchdomain.StatusConflict {
			t.Fatalf("expected conflict for unverifiable context: %+v", got)
		}
	}
}

func TestValidateContextKeepsUsableSources(t *testing.T) {
	value := researchdomain.Context{Status: researchdomain.StatusUsed, Sources: []researchdomain.Source{{URL: "https://example.com/answer", Summary: "题目摘要", TrustScore: .8}}}
	if got := ValidateContext(value); got.Status != researchdomain.StatusUsed {
		t.Fatalf("valid research context should remain usable: %+v", got)
	}
}

func TestRouterUsesDirectSolverForSupportedHighConfidenceQuestion(t *testing.T) {
	decision := (Router{}).Decide(context.Background(), &questiondomain.QuestionAnalysis{Type: "arithmetic", Confidence: .99}, false)
	if decision.NeedResearch {
		t.Fatalf("expected direct solver, got %+v", decision)
	}
}

func TestRouterResearchesLowConfidenceOrComplexQuestion(t *testing.T) {
	cases := []*questiondomain.QuestionAnalysis{
		{Type: "arithmetic", Confidence: .5},
		{Type: "geometry_proof", Confidence: .99},
	}
	for _, analysis := range cases {
		if decision := (Router{}).Decide(context.Background(), analysis, false); !decision.NeedResearch {
			t.Fatalf("expected research for %+v", analysis)
		}
	}
}
