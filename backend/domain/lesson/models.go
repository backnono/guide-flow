package domain

import (
	"encoding/json"
	"strings"
	"time"

	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
	researchdomain "github.com/guide-flow/guide-flow/backend/domain/research"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

type LessonStatus string

const (
	StatusCreated              LessonStatus = "created"
	StatusRecognizing          LessonStatus = "recognizing"
	StatusAwaitingConfirmation LessonStatus = "awaiting_confirmation"
	StatusStructuring          LessonStatus = "structuring"
	StatusResearching          LessonStatus = "researching"
	StatusResearchReady        LessonStatus = "research_ready"
	StatusResearchUnavailable  LessonStatus = "research_unavailable"
	StatusTeaching             LessonStatus = "teaching"
	StatusSolving              LessonStatus = "solving"
	StatusPlanning             LessonStatus = "planning"
	StatusRendering            LessonStatus = "rendering"
	StatusReady                LessonStatus = "ready"
	StatusRejected             LessonStatus = "rejected"
	StatusFailed               LessonStatus = "failed"
)

type LearnerProfile struct {
	Grade int `json:"grade"`
}

type QuestionDraft struct {
	Text            string             `json:"text"`
	Confidence      float64            `json:"confidence"`
	FieldConfidence map[string]float64 `json:"fieldConfidence,omitempty"`
}

// OCRResult 同时保存前端可消费的结构化结果和后端追溯信息；原始响应不会通过 API 暴露。
type OCRResult struct {
	Draft    QuestionDraft
	Analysis *questiondomain.QuestionAnalysis
	// StructuredQuestion 由多模态分析器直接产出，保留图片中的图形关系，避免再次退化成纯文本规则推断。
	StructuredQuestion *questiondomain.StructuredQuestion
	Provider           string
	RequestID          string
	RawResponse        json.RawMessage
	CreatedAt          time.Time
	PromptVersion      string
	SchemaVersion      string
	FinishReason       string
	ParseError         string
	ParseErrorPath     string
	DurationMs         int64
	// RequestCount 统计本次 Analyze 包含的供应商请求次数（含一次有限纠错），供 soak 安全阀核算成本。
	RequestCount int
}

type OCRTrace struct {
	Provider       string
	RequestID      string
	PromptVersion  string
	SchemaVersion  string
	RawResponse    string
	FinishReason   string
	ParseError     string
	ParseErrorPath string
	DurationMs     int64
	CreatedAt      time.Time
}

const OCRConfidenceThreshold = 0.8

// NeedsConfirmation 按关键字段判定，不使用整图平均置信度掩盖数字、单位和问题目标的不确定性。
func (d QuestionDraft) NeedsConfirmation() bool {
	if strings.TrimSpace(d.Text) == "" || d.Confidence <= OCRConfidenceThreshold {
		return true
	}
	for field, confidence := range d.FieldConfidence {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "text", "numbers", "number", "fractions", "fraction", "units", "unit", "formula", "ask", "asks", "question_target", "diagram":
			if confidence <= OCRConfidenceThreshold {
				return true
			}
		}
	}
	return false
}

type QuestionVersion struct {
	ID         int64     `json:"versionId"`
	Text       string    `json:"text"`
	Source     string    `json:"source"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Known = questiondomain.Known
type QuestionAnalysis = questiondomain.QuestionAnalysis
type QuestionRelation = questiondomain.QuestionRelation
type QuestionAsk = questiondomain.QuestionAsk
type StructuredQuestion = questiondomain.StructuredQuestion
type DiagramGraph = questiondomain.DiagramGraph
type DiagramEntity = questiondomain.DiagramEntity
type DiagramRelation = questiondomain.DiagramRelation
type DiagramPoint = questiondomain.DiagramPoint
type DiagramConstraint = questiondomain.DiagramConstraint
type DiagramLayoutHints = questiondomain.DiagramLayoutHints
type DiagramAnchor = questiondomain.DiagramAnchor
type SolutionCandidate = solutiondomain.Candidate
type VerifiedSolution = solutiondomain.VerifiedSolution
type SolutionDraft = solutiondomain.Draft
type ValidationEvidence = solutiondomain.ValidationEvidence
type ValidationStatus = solutiondomain.ValidationStatus

const (
	ValidationVerified          = solutiondomain.ValidationVerified
	ValidationNeedsConfirmation = solutiondomain.ValidationNeedsConfirmation
	ValidationRejected          = solutiondomain.ValidationRejected
	ValidationUnsupported       = solutiondomain.ValidationUnsupported
)

type ResearchContext = researchdomain.Context
type ResearchSource = researchdomain.Source
type ResearchMatchedQuestion = researchdomain.MatchedQuestion
type ResearchReferenceSolution = researchdomain.ReferenceSolution

type ResearchStatus string
type TutorialStatus string

const (
	ResearchNotStarted        ResearchStatus = "not_started"
	ResearchSearching         ResearchStatus = "searching"
	ResearchUnavailableStatus ResearchStatus = "unavailable"
	ResearchFound             ResearchStatus = "found"
	ResearchVerified          ResearchStatus = "verified"
	ResearchConflict          ResearchStatus = "conflict"

	TutorialNotStarted TutorialStatus = "not_started"
	TutorialGenerating TutorialStatus = "generating"
	TutorialFailed     TutorialStatus = "failed"
	TutorialReady      TutorialStatus = "ready"
)

type ModelTrace struct {
	Provider                  string   `json:"provider"`
	Model                     string   `json:"model"`
	PromptVersion             string   `json:"promptVersion"`
	RequestID                 string   `json:"requestId"`
	OutputSchemaVersion       string   `json:"outputSchemaVersion,omitempty"`
	OutputShape               string   `json:"outputShape,omitempty"`
	NormalizationWarnings     []string `json:"normalizationWarnings,omitempty"`
	NormalizationUnknownPaths []string `json:"normalizationUnknownPaths,omitempty"`
	// RawResponse 只进入数据库追踪，不会通过 Lesson API 暴露；保留它才能诊断供应商超时、截断和字段漂移。
	RawResponse string    `json:"rawResponse,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Visual struct {
	Type     string `json:"type"`
	Segments []int  `json:"segments,omitempty"`
}

type LessonStep struct {
	Title       string `json:"title"`
	Explanation string `json:"explanation"`
	Formula     string `json:"formula"`
	Visual      Visual `json:"visual"`
	VoiceText   string `json:"voiceText"`
}

type Practice struct {
	Question        string `json:"question"`
	Answer          string `json:"answer"`
	CheckExpression string `json:"checkExpression,omitempty"`
}

type LessonPlan struct {
	Title          string       `json:"title"`
	Steps          []LessonStep `json:"steps"`
	FinalAnswer    string       `json:"finalAnswer"`
	CommonMistakes []string     `json:"commonMistakes,omitempty"`
	Practice       Practice     `json:"practice"`
}

type FailureKind string

const (
	FailureUserInput FailureKind = "user_input"
	FailureBusiness  FailureKind = "business"
	FailureExternal  FailureKind = "external"
	FailureSystem    FailureKind = "system"
)

type StatusEvent struct {
	Revision  int64        `json:"revision"`
	From      LessonStatus `json:"from,omitempty"`
	To        LessonStatus `json:"to"`
	Stage     string       `json:"stage"`
	CreatedAt time.Time    `json:"createdAt"`
	ErrorCode string       `json:"errorCode,omitempty"`
}

type Lesson struct {
	ID                     string              `json:"lessonId"`
	Status                 LessonStatus        `json:"status"`
	Stage                  string              `json:"stage"`
	Progress               int                 `json:"progress"`
	NextAction             string              `json:"nextAction"`
	QuestionDraft          *QuestionDraft      `json:"questionDraft,omitempty"`
	QuestionAnalysis       *QuestionAnalysis   `json:"questionAnalysis,omitempty"`
	ResearchContext        *ResearchContext    `json:"researchContext,omitempty"`
	ResearchStatus         ResearchStatus      `json:"researchStatus"`
	TutorialStatus         TutorialStatus      `json:"tutorialStatus"`
	SolutionDraft          *SolutionDraft      `json:"solutionDraft,omitempty"`
	SolutionTrace          *ModelTrace         `json:"-"`
	OCRTrace               *OCRTrace           `json:"-"`
	OCRTraces              []OCRTrace          `json:"-"`
	QuestionVersions       []QuestionVersion   `json:"questionVersions,omitempty"`
	CurrentQuestionVersion int64               `json:"currentQuestionVersion,omitempty"`
	Question               *StructuredQuestion `json:"question,omitempty"`
	QuestionID             string              `json:"questionId,omitempty"`
	QuestionVersionID      int64               `json:"questionVersionId,omitempty"`
	SolutionCandidates     []SolutionCandidate `json:"solutionCandidates,omitempty"`
	VerifiedSolution       *VerifiedSolution   `json:"verifiedSolution,omitempty"`
	LessonPlan             *LessonPlan         `json:"lessonPlan,omitempty"`
	IdempotencyKey         string              `json:"-"`
	LearnerProfile         LearnerProfile      `json:"learnerProfile"`
	ImageName              string              `json:"imageName,omitempty"`
	ImageObjectKey         string              `json:"imageObjectKey,omitempty"`
	ThumbnailObjectKey     string              `json:"thumbnailObjectKey,omitempty"`
	Error                  string              `json:"error,omitempty"`
	ErrorCode              string              `json:"errorCode,omitempty"`
	FailureKind            FailureKind         `json:"failureKind,omitempty"`
	FailedFrom             LessonStatus        `json:"failedFrom,omitempty"`
	RetryCount             int                 `json:"retryCount"`
	Revision               int64               `json:"revision"`
	LastErrorAt            *time.Time          `json:"lastErrorAt,omitempty"`
	StatusEvents           []StatusEvent       `json:"statusEvents,omitempty"`
	PracticeResults        []PracticeResult    `json:"practiceResults,omitempty"`
	CreatedAt              time.Time           `json:"createdAt"`
	UpdatedAt              time.Time           `json:"updatedAt"`
}

// ImageReferences 保存对象存储中的稳定对象键，而不是临时签名 URL。
// 签名 URL 会过期，数据库应保存可追溯的 key，展示时再按需生成临时访问地址。
type ImageReferences struct {
	OriginalObjectKey  string `json:"originalObjectKey"`
	ThumbnailObjectKey string `json:"thumbnailObjectKey"`
}

type PracticeResult struct {
	Attempt        int    `json:"attempt"`
	Answer         string `json:"answer"`
	Correct        bool   `json:"correct"`
	Feedback       string `json:"feedback"`
	ExpectedAnswer string `json:"expectedAnswer"`
}
