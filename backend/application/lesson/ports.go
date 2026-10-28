package lesson

import (
	"context"
	"io"
	"time"

	"github.com/guide-flow/guide-flow/backend/application/retrieval"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type LessonRepository interface {
	Create(context.Context, *domain.Lesson) error
	Get(context.Context, string) (*domain.Lesson, error)
	Update(context.Context, *domain.Lesson) error
}

// IdempotencyRepository 将请求幂等键落到持久化层，避免进程重启或多实例部署后重复创建 Lesson。
type IdempotencyRepository interface {
	GetByIdempotencyKey(context.Context, string) (*domain.Lesson, error)
}

type OutboxEvent struct {
	ID       uint64
	LessonID string
	Type     string
}

// OutboxRepository 要求事件与业务记录在同一数据库事务中落库，再由后台投递到队列。
type OutboxRepository interface {
	PendingOutbox(context.Context, int) ([]OutboxEvent, error)
	MarkOutboxPublished(context.Context, uint64) error
}

type OcrAdapter interface {
	Recognize(context.Context, io.Reader, string) (domain.QuestionDraft, error)
}

// RichOCRAdapter 供需要保留供应商原始响应的实现使用；旧的测试桩仍可只实现 OcrAdapter。
type RichOCRAdapter interface {
	RecognizeWithMetadata(context.Context, io.Reader, string) (domain.OCRResult, error)
}

// StructuredQuestionAnalyzer 允许多模态供应商一次返回题目结构；旧 OCR 供应商仍可只实现 OcrAdapter。
type StructuredQuestionAnalyzer interface {
	Analyze(context.Context, io.Reader, string) (domain.OCRResult, error)
}

type QuestionStructurer interface {
	Structure(context.Context, domain.QuestionDraft, domain.LearnerProfile) (domain.StructuredQuestion, error)
}

type Solver interface {
	SolveAndValidate(context.Context, domain.StructuredQuestion, *domain.SolutionCandidate) (
		domain.VerifiedSolution,
		error,
	)
}

type DraftGenerator interface {
	GenerateDraft(
		context.Context,
		domain.StructuredQuestion,
		*domain.SolutionCandidate,
		*domain.ResearchContext,
	) (domain.SolutionDraft, domain.ModelTrace, error)
}

// ImageAwareDraftGenerator 要求解题模型同时看到原题图片和结构化题面；结构化关系只是索引，不能替代原图事实。
type ImageAwareDraftGenerator interface {
	GenerateDraftWithImage(context.Context, domain.StructuredQuestion, *domain.SolutionCandidate, *domain.ResearchContext, io.Reader, string) (domain.SolutionDraft, domain.ModelTrace, error)
}

type DraftVerifier interface {
	VerifyDraft(context.Context, domain.StructuredQuestion, domain.SolutionDraft) (domain.VerifiedSolution, error)
}

type LessonPlanner interface {
	Plan(context.Context, domain.StructuredQuestion, domain.VerifiedSolution, domain.LearnerProfile) (
		domain.LessonPlan,
		error,
	)
}

type LessonPlanValidator interface {
	Validate(domain.LessonPlan, domain.VerifiedSolution) error
}

// SolutionMatcher 隔离题库检索策略；应用层只关心候选解法，不感知 SQL、全文索引或向量数据库。
type SolutionMatcher = retrieval.Matcher

type Renderer interface {
	Render(context.Context, domain.LessonPlan) (domain.LessonPlan, error)
}

type JobQueue interface{ Enqueue(string) error }

type JobMetadata struct {
	ID       string
	LessonID string
	Attempt  int
}

type JobAttemptRecorder interface {
	StartJobAttempt(context.Context, string, string, string, int) error
	FinishJobAttempt(context.Context, string, string, string, string) error
}

type ImageStore interface {
	Save(context.Context, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type ImageReferenceStore interface {
	References(context.Context, string) (domain.ImageReferences, error)
}

// TemporaryURLStore 由需要向客户端暴露图片的对象存储实现；核心业务不依赖具体云厂商 SDK。
type TemporaryURLStore interface {
	ImageStore
	Presign(context.Context, string, time.Duration) (string, error)
}
