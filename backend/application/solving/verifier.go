package solving

import (
	"context"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

// Verifier 按题型选择确定性校验器；未知题型明确返回 unsupported，不降级为猜测。
type Verifier struct {
	registry *VerifierRegistry
}

// NewVerifier 允许 Bootstrap 或测试注入扩展题型注册表；旧调用继续使用 Verifier{}。
func NewVerifier(registry *VerifierRegistry) Verifier { return Verifier{registry: registry} }

func (v Verifier) VerifyDraft(ctx context.Context, question domain.StructuredQuestion, draft domain.SolutionDraft) (domain.VerifiedSolution, error) {
	registry := v.registry
	if registry == nil {
		registry = NewVerifierRegistry()
	}
	return registry.VerifyDraft(ctx, question, draft)
}
