package solution

type ValidationStatus string

const (
	ValidationVerified          ValidationStatus = "verified"
	ValidationNeedsConfirmation ValidationStatus = "needs_confirmation"
	ValidationRejected          ValidationStatus = "rejected"
	ValidationUnsupported       ValidationStatus = "unsupported"
)

type Draft struct {
	SchemaVersion string            `json:"schemaVersion"`
	Method        string            `json:"method"`
	Assumptions   []string          `json:"assumptions"`
	Variables     map[string]string `json:"variables"`
	Steps         []string          `json:"steps"`
	Expressions   []string          `json:"expressions"`
	FinalAnswer   string            `json:"finalAnswer"`
	Uncertainties []string          `json:"uncertainties"`
}

const DraftSchemaVersion = "solution-draft-v1"

type Template struct {
	ID              string            `json:"id"`
	Version         int64             `json:"version"`
	Name            string            `json:"name"`
	Topics          []string          `json:"topics,omitempty"`
	KnowledgePoints []string          `json:"knowledgePoints,omitempty"`
	Formula         string            `json:"formula,omitempty"`
	Applicability   []string          `json:"applicability,omitempty"`
	VariableSchema  map[string]string `json:"variableSchema,omitempty"`
	Steps           []string          `json:"steps,omitempty"`
	CommonMistakes  []string          `json:"commonMistakes,omitempty"`
}

type Candidate struct {
	TemplateID       string  `json:"templateId"`
	TemplateVersion  int64   `json:"templateVersion"`
	Score            float64 `json:"score"`
	MatchReason      string  `json:"matchReason,omitempty"`
	SourceQuestionID string  `json:"sourceQuestionId,omitempty"`
}

// VerifiedSolution 是计算与教学之间的稳定交接对象；只有验证通过的结果才能进入讲解。
type VerifiedSolution struct {
	TemplateID        string             `json:"templateId,omitempty"`
	TemplateVersion   int64              `json:"templateVersion,omitempty"`
	Parameters        map[string]string  `json:"parameters,omitempty"`
	Expressions       []string           `json:"expressions,omitempty"`
	IntermediateSteps []string           `json:"intermediateSteps,omitempty"`
	FinalAnswer       string             `json:"finalAnswer"`
	Validation        ValidationEvidence `json:"validation"`
}

type ValidationEvidence struct {
	Status  ValidationStatus `json:"status"`
	Checks  []string         `json:"checks,omitempty"`
	Message string           `json:"message,omitempty"`
}

// IsVerified 是进入讲解阶段的唯一门禁，调用方不得只判断 FinalAnswer 是否非空。
func (v VerifiedSolution) IsVerified() bool {
	return v.Validation.Status == ValidationVerified
}
