package research

import (
	"strings"
	"time"
)

const (
	// TutorialEvidenceSourceLimit 限制教程模型看到的来源数量，搜索结果仍完整保存在 ResearchContext 中供审计和展开查看。
	TutorialEvidenceSourceLimit = 3
	// TutorialEvidenceSummaryLimit 防止网页长文、乱码和重复内容拖慢教程模型生成。
	TutorialEvidenceSummaryLimit = 1200
)

// Status 表示检索是否参与了当前解题，避免把“未检索”误报成“参考了资料”。
type Status string

const (
	StatusUsed        Status = "used"
	StatusNotNeeded   Status = "not_needed"
	StatusUnavailable Status = "research_unavailable"
	StatusConflict    Status = "conflict"
	StatusRejected    Status = "rejected"
	StatusFound       Status = "found"
	StatusVerified    Status = "verified"
)

type Source struct {
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Summary    string  `json:"summary"`
	TrustScore float64 `json:"trustScore"`
}

// MatchedQuestion 记录本地对候选来源的核对结果；匹配分数不能替代逐项校验。
type MatchedQuestion struct {
	Title                   string   `json:"title"`
	MatchScore              float64  `json:"matchScore"`
	MatchedSubquestionCount int      `json:"matchedSubquestionCount"`
	Checks                  []string `json:"checks"`
}

// ReferenceSolution 是统一内部协议，不暴露搜索供应商的原始字段结构。
type ReferenceSolution struct {
	Status        string   `json:"status"`
	Steps         []string `json:"steps"`
	FinalAnswer   string   `json:"finalAnswer"`
	Uncertainties []string `json:"uncertainties"`
}

type Context struct {
	Query             string             `json:"query"`
	Sources           []Source           `json:"sources"`
	MatchedQuestion   *MatchedQuestion   `json:"matchedQuestion,omitempty"`
	ReferenceSolution *ReferenceSolution `json:"referenceSolution,omitempty"`
	RetrievedAt       time.Time          `json:"retrievedAt"`
	Status            Status             `json:"status"`
	DurationMs        int64              `json:"durationMs,omitempty"`
	ErrorCode         string             `json:"errorCode,omitempty"`
}

// CompactForTutorial 生成教程模型专用的证据包，不修改持久化的完整检索上下文。
// 命中原题优先，其余来源保持搜索供应商给出的顺序，避免把排序分数误当成最终答案置信度。
func CompactForTutorial(value Context) Context {
	compacted := value
	compacted.Sources = make([]Source, 0, TutorialEvidenceSourceLimit)
	seenURLs := make(map[string]struct{}, TutorialEvidenceSourceLimit)
	ordered := make([]Source, 0, len(value.Sources))
	if value.MatchedQuestion != nil {
		for _, source := range value.Sources {
			if strings.TrimSpace(source.Title) == strings.TrimSpace(value.MatchedQuestion.Title) {
				ordered = append(ordered, source)
			}
		}
	}
	ordered = append(ordered, value.Sources...)
	for _, source := range ordered {
		url := strings.TrimSpace(source.URL)
		summary := strings.TrimSpace(source.Summary)
		if url == "" || summary == "" || strings.ContainsRune(summary, '\uFFFD') {
			continue
		}
		if _, exists := seenURLs[url]; exists {
			continue
		}
		seenURLs[url] = struct{}{}
		source.URL = url
		source.Summary = truncateRunes(summary, TutorialEvidenceSummaryLimit)
		compacted.Sources = append(compacted.Sources, source)
		if len(compacted.Sources) == TutorialEvidenceSourceLimit {
			break
		}
	}
	return compacted
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
