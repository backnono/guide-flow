package lesson

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/guide-flow/guide-flow/backend/application/pedagogy"
	"github.com/guide-flow/guide-flow/backend/application/research"
	"github.com/guide-flow/guide-flow/backend/application/retrieval"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type Worker struct {
	repo           LessonRepository
	ocr            OcrAdapter
	vision         StructuredQuestionAnalyzer
	structurer     QuestionStructurer
	matcher        SolutionMatcher
	solver         Solver
	planner        LessonPlanner
	renderer       Renderer
	images         ImageStore
	attempts       JobAttemptRecorder
	researchRouter research.Router
	searchAdapter  research.SearchAdapter
	planValidator  LessonPlanValidator
}

func NewWorker(
	repo LessonRepository,
	ocr OcrAdapter,
	structurer QuestionStructurer,
	solver Solver,
	planner LessonPlanner,
	renderer Renderer,
	images ImageStore,
) *Worker {
	return NewWorkerWithMatcher(repo, ocr, structurer, retrieval.NoopMatcher{}, solver, planner, renderer, images)
}

func NewWorkerWithMatcher(
	repo LessonRepository,
	ocr OcrAdapter,
	structurer QuestionStructurer,
	matcher SolutionMatcher,
	solver Solver,
	planner LessonPlanner,
	renderer Renderer,
	images ImageStore,
) *Worker {
	return &Worker{
		repo:           repo,
		ocr:            ocr,
		structurer:     structurer,
		matcher:        matcher,
		solver:         solver,
		planner:        planner,
		renderer:       renderer,
		images:         images,
		researchRouter: research.Router{},
		searchAdapter:  research.NoopSearchAdapter{},
		planValidator:  pedagogy.LessonPlanValidator{},
	}
}

// WithResearchAdapters 替换检索路由和搜索供应商；默认使用 Noop Adapter，保证本地 Demo 不会隐式访问网络。
func (w *Worker) WithResearchAdapters(router research.Router, adapter research.SearchAdapter) *Worker {
	w.researchRouter = router
	if adapter != nil {
		w.searchAdapter = adapter
	}
	return w
}

// WithVisionAnalyzer 注入可选多模态题目分析器；未配置时继续使用 OCR + 规则结构化，兼容本地 Demo 和旧适配器。
func (w *Worker) WithVisionAnalyzer(analyzer StructuredQuestionAnalyzer) *Worker {
	w.vision = analyzer
	return w
}

func NewWorkerWithAttemptRecorder(
	repo LessonRepository,
	ocr OcrAdapter,
	structurer QuestionStructurer,
	solver Solver,
	planner LessonPlanner,
	renderer Renderer,
	images ImageStore,
	attempts JobAttemptRecorder,
) *Worker {
	worker := NewWorker(repo, ocr, structurer, solver, planner, renderer, images)
	worker.attempts = attempts
	return worker
}

func NewWorkerWithDependencies(
	repo LessonRepository,
	ocr OcrAdapter,
	structurer QuestionStructurer,
	matcher SolutionMatcher,
	solver Solver,
	planner LessonPlanner,
	renderer Renderer,
	images ImageStore,
	attempts JobAttemptRecorder,
) *Worker {
	worker := NewWorkerWithMatcher(repo, ocr, structurer, matcher, solver, planner, renderer, images)
	worker.attempts = attempts
	return worker
}

// Process resumes from the last persisted stage after a worker restart.
func (w *Worker) Process(ctx context.Context, id string) error {
	return w.ProcessJob(ctx, JobMetadata{ID: "direct-" + id, LessonID: id, Attempt: 1})
}

func (w *Worker) ProcessJob(ctx context.Context, job JobMetadata) error {
	l, err := w.repo.Get(ctx, job.LessonID)
	if err != nil {
		return err
	}
	if l.Status == domain.StatusReady || l.Status == domain.StatusRejected || l.Status == domain.StatusAwaitingConfirmation {
		return nil
	}
	if job.Attempt <= 0 {
		job.Attempt = 1
	}
	stage := string(l.Status)
	if w.attempts != nil {
		if err = w.attempts.StartJobAttempt(ctx, l.ID, job.ID, stage, job.Attempt); err != nil {
			return err
		}
		defer func() {
			status, code := "succeeded", ""
			if err != nil {
				status, code = "failed", "stage_failed"
			}
			_ = w.attempts.FinishJobAttempt(context.Background(), l.ID, job.ID, status, code)
		}()
	}
	if l.Status == domain.StatusFailed && l.FailedFrom != "" {
		// Redis 重投递时从上次失败阶段恢复；不会重新 OCR，也不会创建新的 Lesson。
		if err = w.resumeFailed(ctx, l); err != nil {
			return err
		}
	}
	// 每完成一个阶段都会先持久化，再进入下一个阶段，因此进程重启后可从当前状态继续。
	for err == nil {
		switch l.Status {
		case domain.StatusCreated:
			err = w.transition(ctx, l, domain.StatusRecognizing)
		case domain.StatusRecognizing:
			err = w.recognize(ctx, l)
		case domain.StatusStructuring:
			err = w.structure(ctx, l)
		case domain.StatusResearching:
			err = w.research(ctx, l)
		case domain.StatusResearchReady, domain.StatusResearchUnavailable:
			return nil
		case domain.StatusTeaching:
			err = w.teach(ctx, l)
		case domain.StatusSolving:
			err = w.solve(ctx, l)
		case domain.StatusPlanning:
			err = w.plan(ctx, l)
		case domain.StatusRendering:
			err = w.render(ctx, l)
		default:
			return nil
		}
		if err != nil || l.Status == domain.StatusAwaitingConfirmation || l.Status == domain.StatusReady {
			break
		}
	}
	if err != nil {
		w.fail(ctx, l, err)
	}
	return err
}

func (w *Worker) recognize(ctx context.Context, l *domain.Lesson) error {
	image, err := w.images.Open(ctx, l.ID)
	if err != nil {
		return err
	}
	defer image.Close()
	var result domain.OCRResult
	if w.vision != nil {
		// DeepSeek Flash 需要看到原图，不能只把 OCR 结果作为普通文本传入。
		result, err = w.vision.Analyze(ctx, image, l.ImageName)
	} else if analyzer, ok := w.ocr.(StructuredQuestionAnalyzer); ok {
		// 优先使用结构化分析结果；新多模态适配器可以一次产出题型、关系和不确定项。
		result, err = analyzer.Analyze(ctx, image, l.ImageName)
	} else if rich, ok := w.ocr.(RichOCRAdapter); ok {
		result, err = rich.RecognizeWithMetadata(ctx, image, l.ImageName)
	} else {
		result.Draft, err = w.ocr.Recognize(ctx, image, l.ImageName)
	}
	if err != nil {
		// 结构解码失败时也保存供应商原文，便于定位模型字段漂移，不能因返回 error 丢失唯一诊断证据。
		if result.Provider != "" || len(result.RawResponse) > 0 {
			recordOCRTrace(l, result)
			if persistErr := w.persist(ctx, l); persistErr != nil {
				return persistErr
			}
		}
		return err
	}
	draft := result.Draft
	if result.CreatedAt.IsZero() {
		result.CreatedAt = domain.Now()
	}
	if result.Provider != "" || len(result.RawResponse) > 0 {
		recordOCRTrace(l, result)
	}
	if draft.Text == "" {
		return domain.ErrOCRUnrecognized
	}
	l.QuestionDraft = &draft
	if result.Analysis != nil {
		l.QuestionAnalysis = result.Analysis
	}
	if result.StructuredQuestion != nil {
		l.Question = result.StructuredQuestion
	}
	l.QuestionVersions = append(
		l.QuestionVersions,
		domain.QuestionVersion{
			ID:         int64(len(l.QuestionVersions) + 1),
			Text:       draft.Text,
			Source:     "ocr",
			Confidence: draft.Confidence,
			CreatedAt:  domain.Now(),
		},
	)
	l.CurrentQuestionVersion = int64(len(l.QuestionVersions))
	// 读题和正式题面生成完成后统一暂停；无论置信度高低，都必须让用户先确认原图与题面对应关系。
	return w.transition(ctx, l, domain.StatusAwaitingConfirmation)
}

func recordOCRTrace(l *domain.Lesson, result domain.OCRResult) {
	trace := domain.OCRTrace{
		Provider: result.Provider, RequestID: result.RequestID, PromptVersion: result.PromptVersion,
		SchemaVersion: result.SchemaVersion, RawResponse: string(result.RawResponse), FinishReason: result.FinishReason,
		ParseError: result.ParseError, ParseErrorPath: result.ParseErrorPath, DurationMs: result.DurationMs, CreatedAt: result.CreatedAt,
	}
	l.OCRTrace = &trace
	l.OCRTraces = append(l.OCRTraces, trace)
}

func (w *Worker) structure(ctx context.Context, l *domain.Lesson) error {
	if l.QuestionDraft == nil {
		return domain.ErrNeedsConfirmation
	}
	var q domain.StructuredQuestion
	var err error
	if l.Question != nil {
		// 多模态分析已经结合原图生成了图形关系，直接沿用，避免规则结构化把图形再次丢掉。
		q = *l.Question
	} else {
		q, err = w.structurer.Structure(ctx, *l.QuestionDraft, l.LearnerProfile)
		if err != nil {
			return err
		}
	}
	l.Question = &q
	if strings.TrimSpace(q.NormalizedText) == "" {
		// 结构化协议必须携带用户确认后的正式题面，教程模型不能只依赖供应商摘要。
		q.NormalizedText = strings.TrimSpace(l.QuestionDraft.Text)
		l.Question = &q
	}
	decision := w.researchRouter.Decide(ctx, l.QuestionAnalysis, false)
	if decision.NeedResearch {
		l.ResearchStatus = domain.ResearchSearching
		l.TutorialStatus = domain.TutorialNotStarted
		if l.ResearchContext == nil {
			l.ResearchContext = &domain.ResearchContext{Query: w.researchRouter.BuildQuery(l.QuestionDraft.Text, l.QuestionAnalysis, l.Question), Status: "searching"}
		} else {
			l.ResearchContext.Query = w.researchRouter.BuildQuery(l.QuestionDraft.Text, l.QuestionAnalysis, l.Question)
			l.ResearchContext.Status = "searching"
		}
		// 先持久化 searching，再发起外部请求；重启后可以区分未开始和进行中过的检索。
		if err = w.persist(ctx, l); err != nil {
			return err
		}
		return w.transition(ctx, l, domain.StatusResearching)
	}
	if w.matcher != nil {
		candidates, matchErr := w.matcher.Match(ctx, q)
		if matchErr != nil {
			return matchErr
		}
		l.SolutionCandidates = candidates
	}
	// 结构化结果先落库，再转入求解，避免求解阶段失败后丢失上游结果。
	if err = w.persist(ctx, l); err != nil {
		return err
	}
	return w.transition(ctx, l, domain.StatusSolving)
}

func (w *Worker) research(ctx context.Context, l *domain.Lesson) error {
	query := ""
	if l.ResearchContext != nil {
		query = strings.TrimSpace(l.ResearchContext.Query)
	}
	started := time.Now()
	result, searchErr := w.searchAdapter.Search(ctx, query)
	duration := time.Since(started).Milliseconds()
	if result.Query == "" {
		result.Query = query
	}
	result.DurationMs = duration
	if result.RetrievedAt.IsZero() {
		result.RetrievedAt = domain.Now()
	}
	if searchErr != nil {
		result.Status = "research_unavailable"
		// Adapter 已经将可确定的供应商阶段写入 ErrorCode；只有没有具体分类时才回退为通用错误。
		if strings.TrimSpace(result.ErrorCode) == "" {
			result.ErrorCode = researchErrorCode(searchErr)
		}
	} else {
		result = research.ValidateContext(result)
	}
	l.ResearchContext = &result
	if result.Status == "research_unavailable" || result.Status == "conflict" || len(result.Sources) == 0 {
		l.ResearchStatus = domain.ResearchUnavailableStatus
		if result.Status == "conflict" {
			l.ResearchStatus = domain.ResearchConflict
		}
		if err := w.persist(ctx, l); err != nil {
			return err
		}
		return w.transition(ctx, l, domain.StatusResearchUnavailable)
	}
	l.ResearchStatus = domain.ResearchVerified
	if result.Status == "used" || result.Status == "found" {
		l.ResearchStatus = domain.ResearchFound
	}
	// 研究完成即返回可读参考，不在同一 Job 中隐式调用 LLM 教程生成。
	if err := w.persist(ctx, l); err != nil {
		return err
	}
	return w.transition(ctx, l, domain.StatusResearchReady)
}

func researchErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "research_timeout"
	}
	return "research_provider_error"
}

func (w *Worker) teach(ctx context.Context, l *domain.Lesson) error {
	// 教程重试只从 teaching 继续，已持久化的参考资料不会被重新搜索或清除。
	l.TutorialStatus = domain.TutorialGenerating
	if err := w.persist(ctx, l); err != nil {
		return err
	}
	return w.solve(ctx, l)
}
func (w *Worker) solve(ctx context.Context, l *domain.Lesson) error {
	if l.Question == nil {
		return domain.ErrInvalidTransition
	}
	var candidate *domain.SolutionCandidate
	if len(l.SolutionCandidates) > 0 {
		candidate = &l.SolutionCandidates[0]
	}
	var draftGenerated bool
	if draftGenerator, ok := w.solver.(DraftGenerator); ok {
		// 草稿先持久化并保留模型追踪；即使模型输出合法，也必须继续经过 Solver 校验。
		draft, trace, draftErr := w.generateDraft(ctx, l, draftGenerator, candidate)
		if draftErr != nil {
			if trace.Provider != "" || trace.RequestID != "" || trace.RawResponse != "" {
				// 即使 HTTP 在响应体返回前超时，也要保存 provider/model/requestId，避免只能留下通用失败。
				l.SolutionTrace = &trace
				l.ErrorCode = tutorialDraftErrorCode(draftErr)
				if persistErr := w.persist(ctx, l); persistErr != nil {
					return persistErr
				}
			}
			return draftErr
		}
		l.SolutionDraft = &draft
		l.SolutionTrace = &trace
		draftGenerated = true
		if persistErr := w.persist(ctx, l); persistErr != nil {
			return persistErr
		}
	}
	var verified domain.VerifiedSolution
	var err error
	if draftGenerated {
		if draftVerifier, ok := w.solver.(DraftVerifier); ok {
			verified, err = draftVerifier.VerifyDraft(ctx, *l.Question, *l.SolutionDraft)
		} else {
			verified, err = w.solver.SolveAndValidate(ctx, *l.Question, candidate)
		}
	} else {
		verified, err = w.solver.SolveAndValidate(ctx, *l.Question, candidate)
	}
	if err != nil {
		return err
	}
	if !verified.IsVerified() {
		// 条件不足或题型超出当前校验器能力时暂停确认，禁止把失败结果推进到教学规划。
		if verified.Validation.Status == domain.ValidationNeedsConfirmation || verified.Validation.Status == domain.ValidationUnsupported {
			if verified.Validation.Status == domain.ValidationUnsupported {
				l.ErrorCode = "solution_unsupported"
			} else {
				l.ErrorCode = "solution_needs_confirmation"
			}
			if persistErr := w.persist(ctx, l); persistErr != nil {
				return persistErr
			}
			return w.transition(ctx, l, domain.StatusAwaitingConfirmation)
		}
		return domain.ErrNeedsConfirmation
	}
	l.VerifiedSolution = &verified
	if l.TutorialStatus == domain.TutorialGenerating {
		l.TutorialStatus = domain.TutorialReady
	}
	if verified.TemplateID != "" {
		l.QuestionVersionID = l.CurrentQuestionVersion
	}
	return w.transition(ctx, l, domain.StatusPlanning)
}

func tutorialDraftErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "tutorial_timeout"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "decode deepseek draft") || strings.Contains(message, "must include") || strings.Contains(message, "no assistant content") {
		return "tutorial_invalid_output"
	}
	return "tutorial_provider_error"
}

func (w *Worker) generateDraft(ctx context.Context, l *domain.Lesson, generator DraftGenerator, candidate *domain.SolutionCandidate) (domain.SolutionDraft, domain.ModelTrace, error) {
	if imageGenerator, ok := generator.(ImageAwareDraftGenerator); ok {
		image, err := w.images.Open(ctx, l.ID)
		if err != nil {
			return domain.SolutionDraft{}, domain.ModelTrace{}, err
		}
		defer image.Close()
		// 原图每次从对象存储重新读取，避免把一次请求的 reader 跨重试或跨阶段复用。
		return imageGenerator.GenerateDraftWithImage(ctx, *l.Question, candidate, l.ResearchContext, image, l.ImageName)
	}
	return generator.GenerateDraft(ctx, *l.Question, candidate, l.ResearchContext)
}
func (w *Worker) plan(ctx context.Context, l *domain.Lesson) error {
	if l.Question == nil || l.VerifiedSolution == nil {
		return domain.ErrInvalidTransition
	}
	planned, err := w.planner.Plan(ctx, *l.Question, *l.VerifiedSolution, l.LearnerProfile)
	if err != nil {
		return err
	}
	if w.planValidator != nil {
		if err := w.planValidator.Validate(planned, *l.VerifiedSolution); err != nil {
			return err
		}
	}
	l.LessonPlan = &planned
	return w.transition(ctx, l, domain.StatusRendering)
}
func (w *Worker) render(ctx context.Context, l *domain.Lesson) error {
	if l.LessonPlan == nil {
		return domain.ErrInvalidTransition
	}
	rendered, err := w.renderer.Render(ctx, *l.LessonPlan)
	if err != nil {
		return err
	}
	l.LessonPlan = &rendered
	return w.transition(ctx, l, domain.StatusReady)
}
func (w *Worker) transition(ctx context.Context, l *domain.Lesson, status domain.LessonStatus) error {
	if err := domain.ValidateTransition(l.Status, status); err != nil {
		return err
	}
	from := l.Status
	l.Status = status
	l.UpdatedAt = domain.Now()
	// 所有状态变化都记录事件，便于进度查询、失败定位和后续持久化迁移。
	l.StatusEvents = append(
		l.StatusEvents,
		domain.StatusEvent{From: from, To: status, Stage: domain.Stage(status), CreatedAt: l.UpdatedAt},
	)
	return w.persist(ctx, l)
}
func (w *Worker) persist(ctx context.Context, l *domain.Lesson) error { return w.repo.Update(ctx, l) }

func (w *Worker) resumeFailed(ctx context.Context, l *domain.Lesson) error {
	if err := domain.ValidateTransition(l.Status, l.FailedFrom); err != nil {
		return err
	}
	from, to := l.Status, l.FailedFrom
	l.Status = to
	l.FailedFrom = ""
	l.RetryCount++
	l.Error = ""
	l.ErrorCode = ""
	l.FailureKind = ""
	l.LastErrorAt = nil
	l.UpdatedAt = domain.Now()
	l.StatusEvents = append(
		l.StatusEvents,
		domain.StatusEvent{From: from, To: to, Stage: domain.Stage(to), CreatedAt: l.UpdatedAt},
	)
	return w.persist(ctx, l)
}

func (w *Worker) fail(ctx context.Context, l *domain.Lesson, cause error) {
	if l.Status == domain.StatusReady || l.Status == domain.StatusRejected {
		return
	}
	now := domain.Now()
	failedFrom := l.Status
	if failedFrom == domain.StatusTeaching {
		l.TutorialStatus = domain.TutorialFailed
	}
	preservedErrorCode := l.ErrorCode
	// 保留 FailedFrom，RetryLesson 才能从原失败阶段恢复，而不是盲目从头执行。
	l.FailedFrom = failedFrom
	l.Status = domain.StatusFailed
	l.Error = cause.Error()
	l.ErrorCode, l.FailureKind = classifyFailure(failedFrom, cause, preservedErrorCode)
	l.LastErrorAt = &now
	l.UpdatedAt = now
	l.StatusEvents = append(
		l.StatusEvents,
		domain.StatusEvent{
			From:      l.FailedFrom,
			To:        domain.StatusFailed,
			Stage:     domain.Stage(l.FailedFrom),
			CreatedAt: now,
			ErrorCode: l.ErrorCode,
		},
	)
	_ = w.repo.Update(ctx, l)
}

func classifyFailure(failedFrom domain.LessonStatus, cause error, preservedErrorCode string) (string, domain.FailureKind) {
	if preservedErrorCode != "" {
		return preservedErrorCode, domain.FailureExternal
	}
	// 解题阶段的上下文超时通常来自模型请求未在配置时限内返回，单独分类便于前端和运维区分 OCR 失败。
	if failedFrom == domain.StatusSolving && errors.Is(cause, context.DeadlineExceeded) {
		return "llm_timeout", domain.FailureExternal
	}
	if failedFrom == domain.StatusTeaching && errors.Is(cause, context.DeadlineExceeded) {
		return "tutorial_timeout", domain.FailureExternal
	}
	if failedFrom == domain.StatusTeaching {
		return "tutorial_failed", domain.FailureExternal
	}
	if errors.Is(cause, domain.ErrOCRUnrecognized) {
		return "ocr_unrecognized", domain.FailureBusiness
	}
	if errors.Is(cause, domain.ErrDemoOCROnly) {
		return "ocr_demo_only", domain.FailureBusiness
	}
	if errors.Is(cause, domain.ErrOCRProvider) || errors.Is(cause, domain.ErrOCRInvalidResponse) {
		return "ocr_provider_error", domain.FailureExternal
	}
	return "stage_failed", domain.FailureExternal
}
