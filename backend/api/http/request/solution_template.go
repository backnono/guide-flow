package request

type CreateSolutionTemplateRequest struct {
	ID              string            `json:"id"`
	Version         int64             `json:"version"`
	Name            string            `json:"name"`
	Topics          []string          `json:"topics"`
	KnowledgePoints []string          `json:"knowledgePoints"`
	Formula         string            `json:"formula"`
	Applicability   []string          `json:"applicability"`
	VariableSchema  map[string]string `json:"variableSchema"`
	Steps           []string          `json:"steps"`
	CommonMistakes  []string          `json:"commonMistakes"`
}

type PublishSolutionTemplateRequest struct {
	Version int64 `json:"version"`
}
