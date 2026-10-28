package solving

import (
	"bytes"
	"context"
	"io"
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type fixedDraftGenerator struct{}

func (fixedDraftGenerator) GenerateDraft(context.Context, domain.StructuredQuestion, *domain.SolutionCandidate, *domain.ResearchContext) (domain.SolutionDraft, domain.ModelTrace, error) {
	return domain.SolutionDraft{Method: "arithmetic", Steps: []string{"计算"}, Expressions: []string{"1+1=2"}, FinalAnswer: "2"}, domain.ModelTrace{Provider: "test"}, nil
}

type imageAwareFixedGenerator struct {
	seenImage bool
}

func (g *imageAwareFixedGenerator) GenerateDraft(context.Context, domain.StructuredQuestion, *domain.SolutionCandidate, *domain.ResearchContext) (domain.SolutionDraft, domain.ModelTrace, error) {
	return domain.SolutionDraft{Method: "arithmetic", Steps: []string{"计算"}, Expressions: []string{"1+1=2"}, FinalAnswer: "2"}, domain.ModelTrace{Provider: "test"}, nil
}

func (g *imageAwareFixedGenerator) GenerateDraftWithImage(_ context.Context, _ domain.StructuredQuestion, _ *domain.SolutionCandidate, _ *domain.ResearchContext, image io.Reader, _ string) (domain.SolutionDraft, domain.ModelTrace, error) {
	data, _ := io.ReadAll(image)
	g.seenImage = bytes.Equal(data, []byte("original"))
	return domain.SolutionDraft{Method: "arithmetic", Steps: []string{"计算"}, Expressions: []string{"1+1=2"}, FinalAnswer: "2"}, domain.ModelTrace{Provider: "test"}, nil
}

func TestLLMSolverVerifiesGeneratedDraft(t *testing.T) {
	solver := NewLLMSolver(fixedDraftGenerator{})
	verified, err := solver.SolveAndValidate(context.Background(), domain.StructuredQuestion{Topic: "arithmetic"}, nil)
	if err != nil || !verified.IsVerified() {
		t.Fatalf("expected verified draft: %+v, err=%v", verified, err)
	}
}

func TestLLMSolverForwardsOriginalImageToWrappedGenerator(t *testing.T) {
	generator := &imageAwareFixedGenerator{}
	solver := NewLLMSolver(generator)
	_, _, err := solver.GenerateDraftWithImage(context.Background(), domain.StructuredQuestion{Topic: "geometry_numeric"}, nil, nil, bytes.NewReader([]byte("original")), "question.png")
	if err != nil || !generator.seenImage {
		t.Fatalf("wrapped solver must preserve original image input: err=%v seen=%v", err, generator.seenImage)
	}
}
