package solving

import (
	"context"
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type customTopicVerifier struct{}

func (customTopicVerifier) VerifyDraft(context.Context, domain.StructuredQuestion, domain.SolutionDraft) (domain.VerifiedSolution, error) {
	return domain.VerifiedSolution{Validation: domain.ValidationEvidence{Status: domain.ValidationVerified, Message: "custom verifier"}}, nil
}

func TestVerifierRegistryAllowsNewTopicWithoutChangingDispatcher(t *testing.T) {
	registry := NewVerifierRegistry()
	registry.Register("function", customTopicVerifier{})
	verified, err := NewVerifier(registry).VerifyDraft(context.Background(), domain.StructuredQuestion{Topic: "function"}, domain.SolutionDraft{})
	if err != nil || verified.Validation.Status != domain.ValidationVerified || verified.Validation.Message != "custom verifier" {
		t.Fatalf("custom topic verifier was not dispatched: %+v, %v", verified, err)
	}
}

func TestVerifierRegistryMarksUnknownTopicUnsupported(t *testing.T) {
	verified, err := NewVerifierRegistry().VerifyDraft(context.Background(), domain.StructuredQuestion{Topic: "probability"}, domain.SolutionDraft{})
	if err != nil || verified.Validation.Status != domain.ValidationUnsupported {
		t.Fatalf("unknown topic should be unsupported: %+v, %v", verified, err)
	}
}
