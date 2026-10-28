package agentcontext

import "context"

type Phase string

const (
	PhasePropose Phase = "propose"
	PhaseApply   Phase = "apply"
	PhaseReview  Phase = "review"
)

type BuildRequest struct {
	TaskID      string   `json:"task_id"`
	Task        string   `json:"task"`
	Phase       Phase    `json:"phase"`
	RepoRoot    string   `json:"repo_root"`
	ContextRoot string   `json:"context_root"`
	Knowledge   []string `json:"knowledge,omitempty"`
	MaxBytes    int      `json:"max_bytes"`
}

type ContextItem struct {
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	Content   string `json:"content"`
	Authority string `json:"authority"`
	Reason    string `json:"reason"`
	Version   string `json:"version,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

type ContextPack struct {
	Task     BuildRequest  `json:"task"`
	Items    []ContextItem `json:"items"`
	Warnings []string      `json:"warnings,omitempty"`
	Markdown string        `json:"markdown"`
}

// Compiler is the only interface that callers need to know.
type Compiler interface {
	Compile(ctx context.Context, request BuildRequest) (ContextPack, error)
}
