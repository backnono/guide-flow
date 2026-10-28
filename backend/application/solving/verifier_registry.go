package solving

import (
	"context"
	"strings"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

// TopicVerifier 是题型校验模块的最小接口；题型细节和确定性校验规则封装在实现内部。
type TopicVerifier interface {
	VerifyDraft(context.Context, domain.StructuredQuestion, domain.SolutionDraft) (domain.VerifiedSolution, error)
}

// VerifierRegistry 通过题型注册表隔离新增题型，避免每增加一个题型就修改统一校验器的分支。
type VerifierRegistry struct {
	verifiers map[string]TopicVerifier
}

func NewVerifierRegistry() *VerifierRegistry {
	registry := &VerifierRegistry{verifiers: make(map[string]TopicVerifier)}
	registry.Register("geometry_numeric", GeometryVerifier{})
	registry.Register("geometry_proof", GeometryVerifier{})
	registry.Register("arithmetic", ArithmeticVerifier{})
	registry.Register("fraction", ArithmeticVerifier{})
	registry.Register("equation", ArithmeticVerifier{})
	registry.Register("word_problem", ArithmeticVerifier{})
	registry.Register("fraction_word_problem", ArithmeticVerifier{})
	return registry
}

func (r *VerifierRegistry) Register(topic string, verifier TopicVerifier) {
	if r == nil || verifier == nil {
		return
	}
	if r.verifiers == nil {
		r.verifiers = make(map[string]TopicVerifier)
	}
	r.verifiers[strings.ToLower(strings.TrimSpace(topic))] = verifier
}

func (r *VerifierRegistry) VerifyDraft(ctx context.Context, question domain.StructuredQuestion, draft domain.SolutionDraft) (domain.VerifiedSolution, error) {
	if r != nil {
		if verifier, ok := r.verifiers[strings.ToLower(strings.TrimSpace(question.Topic))]; ok {
			return verifier.VerifyDraft(ctx, question, draft)
		}
	}
	return unsupportedVerification(question, draft), nil
}

func unsupportedVerification(question domain.StructuredQuestion, draft domain.SolutionDraft) domain.VerifiedSolution {
	return domain.VerifiedSolution{
		Parameters:        draft.Variables,
		Expressions:       append([]string(nil), draft.Expressions...),
		IntermediateSteps: append([]string(nil), draft.Steps...),
		FinalAnswer:       draft.FinalAnswer,
		Validation: domain.ValidationEvidence{
			Status:  domain.ValidationUnsupported,
			Message: "当前未注册题型校验器：" + question.Topic,
		},
	}
}
