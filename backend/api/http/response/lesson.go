package response

import (
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

// LessonResponse 是 API 对外契约，避免直接把领域对象作为长期稳定的接口结构。
type LessonResponse struct {
	ID                     string                     `json:"lessonId"`
	Status                 domain.LessonStatus        `json:"status"`
	Stage                  string                     `json:"stage"`
	Progress               int                        `json:"progress"`
	NextAction             string                     `json:"nextAction"`
	QuestionDraft          *domain.QuestionDraft      `json:"questionDraft,omitempty"`
	QuestionAnalysis       *domain.QuestionAnalysis   `json:"questionAnalysis,omitempty"`
	ResearchContext        *domain.ResearchContext    `json:"researchContext,omitempty"`
	ResearchStatus         domain.ResearchStatus      `json:"researchStatus"`
	TutorialStatus         domain.TutorialStatus      `json:"tutorialStatus"`
	QuestionVersions       []QuestionVersionResponse  `json:"questionVersions,omitempty"`
	CurrentQuestionVersion int64                      `json:"currentQuestionVersion,omitempty"`
	Question               *domain.StructuredQuestion `json:"question,omitempty"`
	QuestionID             string                     `json:"questionId,omitempty"`
	QuestionVersionID      int64                      `json:"questionVersionId,omitempty"`
	SolutionCandidates     []domain.SolutionCandidate `json:"solutionCandidates,omitempty"`
	VerifiedSolution       *domain.VerifiedSolution   `json:"verifiedSolution,omitempty"`
	LessonPlan             *domain.LessonPlan         `json:"lessonPlan,omitempty"`
	LearnerProfile         domain.LearnerProfile      `json:"learnerProfile"`
	ImageName              string                     `json:"imageName,omitempty"`
	ImageObjectKey         string                     `json:"imageObjectKey,omitempty"`
	ThumbnailObjectKey     string                     `json:"thumbnailObjectKey,omitempty"`
	Error                  string                     `json:"error,omitempty"`
	ErrorCode              string                     `json:"errorCode,omitempty"`
	FailureKind            domain.FailureKind         `json:"failureKind,omitempty"`
	FailedFrom             domain.LessonStatus        `json:"failedFrom,omitempty"`
	RetryCount             int                        `json:"retryCount"`
	Revision               int64                      `json:"revision"`
	LastErrorAt            string                     `json:"lastErrorAt,omitempty"`
	StatusEvents           []StatusEventResponse      `json:"statusEvents,omitempty"`
	PracticeResults        []domain.PracticeResult    `json:"practiceResults,omitempty"`
	CreatedAt              string                     `json:"createdAt"`
	UpdatedAt              string                     `json:"updatedAt"`
}

type QuestionVersionResponse struct {
	ID         int64   `json:"versionId"`
	Text       string  `json:"text"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	CreatedAt  string  `json:"createdAt"`
}

type StatusEventResponse struct {
	Revision  int64  `json:"revision"`
	From      string `json:"from,omitempty"`
	To        string `json:"to"`
	Stage     string `json:"stage"`
	ErrorCode string `json:"errorCode,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func NewLessonResponse(l *domain.Lesson) LessonResponse {
	return LessonResponse{
		ID: l.ID, Status: l.Status, Stage: domain.Stage(l.Status), Progress: domain.Progress(l.Status), NextAction: domain.NextAction(l.Status),
		QuestionDraft: l.QuestionDraft, QuestionAnalysis: l.QuestionAnalysis, ResearchContext: l.ResearchContext, ResearchStatus: l.ResearchStatus, TutorialStatus: l.TutorialStatus, QuestionVersions: mapQuestionVersions(l.QuestionVersions), CurrentQuestionVersion: l.CurrentQuestionVersion,
		Question: l.Question, QuestionID: l.QuestionID, QuestionVersionID: l.QuestionVersionID, SolutionCandidates: l.SolutionCandidates, VerifiedSolution: l.VerifiedSolution, LessonPlan: l.LessonPlan, LearnerProfile: l.LearnerProfile, ImageName: l.ImageName, ImageObjectKey: l.ImageObjectKey, ThumbnailObjectKey: l.ThumbnailObjectKey,
		Error: publicError(l), ErrorCode: l.ErrorCode, FailureKind: l.FailureKind, FailedFrom: l.FailedFrom, RetryCount: l.RetryCount, Revision: l.Revision,
		LastErrorAt: formatOptionalTime(l.LastErrorAt), StatusEvents: mapStatusEvents(l.StatusEvents), PracticeResults: l.PracticeResults,
		CreatedAt: formatTime(l.CreatedAt), UpdatedAt: formatTime(l.UpdatedAt),
	}
}

func publicError(l *domain.Lesson) string {
	switch l.ErrorCode {
	case "ocr_demo_only":
		return "当前仅支持示例题；配置真实 OCR 后才能识别上传的题目"
	case "ocr_unrecognized":
		return "我没能看清这道题，请拍清楚题干、数字和图形后重试"
	case "ocr_provider_error":
		return "题目识别服务暂时不可用，请稍后重试"
	case "llm_timeout":
		return "解题服务响应超时，请稍后重试"
	case "stage_failed":
		return "这道题暂时没有整理好，可以再试一次"
	case "solution_unsupported":
		return "题目已经读懂，但当前几何解题校验能力还不足，暂时无法生成可靠讲解"
	case "research_unavailable":
		return "未找到可靠参考解答，可以稍后重试检索"
	case "research_timeout", "research_provider_error":
		return "题目检索服务暂时不可用，可以稍后重试"
	case "tutorial_failed":
		return "参考解答已找到，但个性化讲解暂时生成失败，可以只重试讲解"
	case "tutorial_invalid_output":
		return "参考解答已找到，但讲解模型返回格式异常，可以只重试讲解"
	case "tutorial_provider_error":
		return "参考解答已找到，但讲解服务暂时不可用，可以只重试讲解"
	case "tutorial_timeout":
		return "参考解答已找到，但讲解生成超时，可以只重试讲解"
	default:
		return l.Error
	}
}

var apiLocation = domain.BusinessLocation

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.In(apiLocation).Format("2006-01-02 15:04:05")
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func mapQuestionVersions(values []domain.QuestionVersion) []QuestionVersionResponse {
	if len(values) == 0 {
		return nil
	}
	result := make([]QuestionVersionResponse, 0, len(values))
	for _, value := range values {
		result = append(result, QuestionVersionResponse{ID: value.ID, Text: value.Text, Source: value.Source, Confidence: value.Confidence, CreatedAt: formatTime(value.CreatedAt)})
	}
	return result
}

func mapStatusEvents(values []domain.StatusEvent) []StatusEventResponse {
	if len(values) == 0 {
		return nil
	}
	result := make([]StatusEventResponse, 0, len(values))
	for _, value := range values {
		result = append(result, StatusEventResponse{Revision: value.Revision, From: string(value.From), To: string(value.To), Stage: value.Stage, ErrorCode: value.ErrorCode, CreatedAt: formatTime(value.CreatedAt)})
	}
	return result
}

type PracticeResultResponse struct {
	Attempt        int    `json:"attempt"`
	Answer         string `json:"answer"`
	Correct        bool   `json:"correct"`
	Feedback       string `json:"feedback"`
	ExpectedAnswer string `json:"expectedAnswer"`
}

func NewPracticeResultResponse(r domain.PracticeResult) PracticeResultResponse {
	return PracticeResultResponse{Attempt: r.Attempt, Answer: r.Answer, Correct: r.Correct, Feedback: r.Feedback, ExpectedAnswer: r.ExpectedAnswer}
}
