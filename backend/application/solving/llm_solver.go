package solving

import (
	"context"
	"io"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type DraftGenerator interface {
	GenerateDraft(context.Context, domain.StructuredQuestion, *domain.SolutionCandidate, *domain.ResearchContext) (domain.SolutionDraft, domain.ModelTrace, error)
}

type imageAwareDraftGenerator interface {
	GenerateDraftWithImage(context.Context, domain.StructuredQuestion, *domain.SolutionCandidate, *domain.ResearchContext, io.Reader, string) (domain.SolutionDraft, domain.ModelTrace, error)
}

// LLMSolver 将外部 LLM 的草稿能力与确定性校验器组合，禁止 LLM 直接声称答案已验证。
type LLMSolver struct {
	generator DraftGenerator
	verifier  Verifier
}

func NewLLMSolver(generator DraftGenerator) *LLMSolver {
	return &LLMSolver{generator: generator}
}

// NewLLMSolverWithVerifier 允许应用装配一次题型注册表，避免每次校验都重新构造题型路由。
func NewLLMSolverWithVerifier(generator DraftGenerator, verifier Verifier) *LLMSolver {
	return &LLMSolver{generator: generator, verifier: verifier}
}

func (s *LLMSolver) GenerateDraft(ctx context.Context, question domain.StructuredQuestion, candidate *domain.SolutionCandidate, researchContext *domain.ResearchContext) (domain.SolutionDraft, domain.ModelTrace, error) {
	return s.generator.GenerateDraft(ctx, question, candidate, researchContext)
}

// GenerateDraftWithImage 透传原图能力，避免外层 Solver 包装器把视觉事实截断为纯结构化题面。
func (s *LLMSolver) GenerateDraftWithImage(ctx context.Context, question domain.StructuredQuestion, candidate *domain.SolutionCandidate, researchContext *domain.ResearchContext, image io.Reader, imageName string) (domain.SolutionDraft, domain.ModelTrace, error) {
	if generator, ok := s.generator.(imageAwareDraftGenerator); ok {
		return generator.GenerateDraftWithImage(ctx, question, candidate, researchContext, image, imageName)
	}
	return s.generator.GenerateDraft(ctx, question, candidate, researchContext)
}

func (s *LLMSolver) VerifyDraft(ctx context.Context, question domain.StructuredQuestion, draft domain.SolutionDraft) (domain.VerifiedSolution, error) {
	return s.verifier.VerifyDraft(ctx, question, draft)
}

func (s *LLMSolver) SolveAndValidate(ctx context.Context, question domain.StructuredQuestion, candidate *domain.SolutionCandidate) (domain.VerifiedSolution, error) {
	draft, _, err := s.GenerateDraft(ctx, question, candidate, nil)
	if err != nil {
		return domain.VerifiedSolution{}, err
	}
	return s.VerifyDraft(ctx, question, draft)
}
