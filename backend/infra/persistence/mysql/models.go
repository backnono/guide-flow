package mysql

import (
	"time"

	"gorm.io/datatypes"
)

type LessonModel struct {
	ID                     string         `gorm:"primaryKey;size:64"`
	Status                 string         `gorm:"size:32;not null;index:idx_lessons_status_updated,priority:1"`
	LearnerProfileJSON     datatypes.JSON `gorm:"type:json;not null"`
	ImageName              string         `gorm:"size:255;not null"`
	IdempotencyKey         string         `gorm:"size:128;uniqueIndex"` // 创建请求幂等键，防止重试产生重复 Lesson。
	ImageObjectKey         string         `gorm:"size:512"`
	ThumbnailObjectKey     string         `gorm:"size:512"`
	QuestionDraftJSON      datatypes.JSON `gorm:"type:json"`
	QuestionAnalysisJSON   datatypes.JSON `gorm:"type:json"`                              // 多模态题目结构化分析结果。
	ResearchContextJSON    datatypes.JSON `gorm:"type:json"`                              // 本题检索上下文及检索状态。
	ResearchStatus         string         `gorm:"size:32;not null;default:'not_started'"` // 检索阶段状态。
	TutorialStatus         string         `gorm:"size:32;not null;default:'not_started'"` // 教程阶段状态。
	SolutionDraftJSON      datatypes.JSON `gorm:"type:json"`                              // LLM 生成的未校验解题草稿。
	SolutionTraceJSON      datatypes.JSON `gorm:"type:json"`                              // LLM 模型、Prompt 和请求追踪信息。
	CurrentQuestionVersion int64          `gorm:"not null;default:0"`
	QuestionJSON           datatypes.JSON `gorm:"type:json"`
	QuestionID             string         `gorm:"size:64"`            // 归属的题目知识资产 ID。
	QuestionVersionID      int64          `gorm:"not null;default:0"` // 本次讲解使用的题目版本。
	SolutionCandidatesJSON datatypes.JSON `gorm:"type:json"`          // 检索候选及评分，保留匹配可解释性。
	VerifiedSolutionJSON   datatypes.JSON `gorm:"type:json"`          // 经过校验的当前题目解法结果。
	Error                  *string        `gorm:"type:text"`
	ErrorCode              *string        `gorm:"size:64"`
	FailureKind            *string        `gorm:"size:32"`
	FailedFrom             *string        `gorm:"size:32"`
	RetryCount             int            `gorm:"not null;default:0"`
	LastErrorAt            *time.Time     `gorm:"precision:3"`
	Revision               int64          `gorm:"not null;default:1"`
	CreatedAt              time.Time      `gorm:"precision:3;not null"`
	UpdatedAt              time.Time      `gorm:"precision:3;not null;index:idx_lessons_status_updated,priority:2"`
}

func (LessonModel) TableName() string { return "lessons" }

type QuestionVersionModel struct {
	LessonID   string    `gorm:"primaryKey;size:64"`
	VersionID  int64     `gorm:"primaryKey"`
	Text       string    `gorm:"type:text;not null"`
	Source     string    `gorm:"size:32;not null"`
	Confidence float64   `gorm:"type:decimal(6,5);not null"`
	CreatedAt  time.Time `gorm:"precision:3;not null"`
}

type OCRTraceModel struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement"`
	LessonID       string    `gorm:"size:64;not null;index:idx_ocr_traces_lesson_created,priority:1"`
	Provider       string    `gorm:"size:64;not null"`
	RequestID      string    `gorm:"size:128"`
	PromptVersion  string    `gorm:"size:64"`
	SchemaVersion  string    `gorm:"size:64"`
	RawResponse    string    `gorm:"type:longtext;not null"`
	FinishReason   string    `gorm:"size:32"`
	ParseError     string    `gorm:"type:text"`
	ParseErrorPath string    `gorm:"size:255"`
	DurationMs     int64     `gorm:"not null;default:0"`
	CreatedAt      time.Time `gorm:"precision:3;not null;index:idx_ocr_traces_lesson_created,priority:2"`
}

func (OCRTraceModel) TableName() string { return "ocr_traces" }

func (QuestionVersionModel) TableName() string { return "question_versions" }

type LessonPlanModel struct {
	LessonID  string         `gorm:"primaryKey;size:64"`
	PlanJSON  datatypes.JSON `gorm:"type:json;not null"`
	CreatedAt time.Time      `gorm:"precision:3;not null"`
	UpdatedAt time.Time      `gorm:"precision:3;not null"`
}

func (LessonPlanModel) TableName() string { return "lesson_plans" }

type PracticeResultModel struct {
	LessonID       string    `gorm:"primaryKey;size:64"`
	Attempt        int       `gorm:"primaryKey"`
	Answer         string    `gorm:"type:text;not null"`
	Correct        bool      `gorm:"not null"`
	Feedback       string    `gorm:"type:text;not null"`
	ExpectedAnswer string    `gorm:"type:text;not null"`
	CreatedAt      time.Time `gorm:"precision:3;not null"`
}

func (PracticeResultModel) TableName() string { return "practice_results" }

type StatusEventModel struct {
	LessonID   string    `gorm:"primaryKey;size:64"`
	Revision   int64     `gorm:"primaryKey"`
	FromStatus *string   `gorm:"size:32"`
	ToStatus   string    `gorm:"size:32;not null"`
	Stage      string    `gorm:"size:32;not null"`
	ErrorCode  *string   `gorm:"size:64"`
	CreatedAt  time.Time `gorm:"precision:3;not null"`
}

func (StatusEventModel) TableName() string { return "lesson_status_events" }

type JobAttemptModel struct {
	ID         uint64     `gorm:"primaryKey;autoIncrement"`
	LessonID   string     `gorm:"size:64;not null;index:idx_job_attempts_lesson_created,priority:1"`
	JobID      string     `gorm:"size:128;not null;uniqueIndex:uk_job_attempt,priority:1"`
	Stage      string     `gorm:"size:32;not null"`
	Attempt    int        `gorm:"not null;uniqueIndex:uk_job_attempt,priority:2"`
	Status     string     `gorm:"size:32;not null"`
	ErrorCode  *string    `gorm:"size:64"`
	StartedAt  *time.Time `gorm:"precision:3"`
	FinishedAt *time.Time `gorm:"precision:3"`
	CreatedAt  time.Time  `gorm:"precision:3;not null;index:idx_job_attempts_lesson_created,priority:2"`
}

func (JobAttemptModel) TableName() string { return "job_attempts" }

type OutboxEventModel struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement"` // Outbox 事件主键。
	LessonID    string     `gorm:"size:64;not null;index"`   // 待投递事件关联的 Lesson。
	EventType   string     `gorm:"size:64;not null"`         // 事件类型，例如 lesson.created。
	Status      string     `gorm:"size:16;not null;index"`   // 投递状态：pending 或 published。
	CreatedAt   time.Time  `gorm:"precision:3;not null"`     // 事件创建时间。
	PublishedAt *time.Time `gorm:"precision:3"`              // 成功投递时间。
}

func (OutboxEventModel) TableName() string { return "outbox_events" }

type SolutionTemplateModel struct {
	ID                  string         `gorm:"primaryKey;size:64"`          // 解法模板稳定 ID。
	Version             int64          `gorm:"primaryKey"`                  // 模板不可变版本号。
	Name                string         `gorm:"size:255;not null"`           // 面向运营和调试的模板名称。
	TopicsJSON          datatypes.JSON `gorm:"type:json"`                   // 适用题型列表。
	KnowledgePointsJSON datatypes.JSON `gorm:"type:json"`                   // 适用知识点列表。
	Formula             string         `gorm:"type:text"`                   // 结构化公式表达式。
	ApplicabilityJSON   datatypes.JSON `gorm:"type:json"`                   // 适用条件。
	VariableSchemaJSON  datatypes.JSON `gorm:"type:json"`                   // 参数变量定义。
	StepsJSON           datatypes.JSON `gorm:"type:json"`                   // 解法步骤模板。
	CommonMistakesJSON  datatypes.JSON `gorm:"type:json"`                   // 常见错误说明。
	Active              bool           `gorm:"not null;default:true;index"` // 是否参与检索。
	CreatedAt           time.Time      `gorm:"precision:3;not null"`        // 创建时间。
}

func (SolutionTemplateModel) TableName() string { return "solution_templates" }
