package research

import (
	"context"
	"net/url"
	"strings"

	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
	researchdomain "github.com/guide-flow/guide-flow/backend/domain/research"
)

// ValidateContext 只执行本地可确定的检索结果校验，不根据摘要猜测题干或图形关系。
// used 结果缺少可核对来源时必须降级为 conflict，避免后续模型把空结果当成外部答案。
func ValidateContext(value researchdomain.Context) researchdomain.Context {
	if value.Status != researchdomain.StatusUsed && value.Status != researchdomain.StatusFound && value.Status != researchdomain.StatusVerified {
		return value
	}
	if len(value.Sources) == 0 {
		value.Status = researchdomain.StatusConflict
		return value
	}
	for _, source := range value.Sources {
		parsed, err := url.Parse(strings.TrimSpace(source.URL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.TrimSpace(source.Summary) == "" || source.TrustScore < 0 || source.TrustScore > 1 {
			value.Status = researchdomain.StatusConflict
			return value
		}
	}
	if value.MatchedQuestion != nil && (value.MatchedQuestion.MatchScore < 0 || value.MatchedQuestion.MatchScore > 1 || strings.TrimSpace(value.MatchedQuestion.Title) == "") {
		value.Status = researchdomain.StatusConflict
	}
	return value
}

type SearchAdapter interface {
	Search(context.Context, string) (researchdomain.Context, error)
}

type Decision struct {
	NeedResearch bool
	Query        string
	Reason       string
}

// Router 是阶段二的最小确定性路由器；路由原因必须可解释，便于后续统计成本和误触发。
type Router struct{}

func (Router) Decide(_ context.Context, analysis *questiondomain.QuestionAnalysis, userRequested bool) Decision {
	if userRequested {
		return Decision{NeedResearch: true, Reason: "user_requested"}
	}
	if analysis == nil {
		return Decision{NeedResearch: true, Reason: "analysis_missing"}
	}
	if len(analysis.UncertainItems) > 0 || analysis.Confidence < 0.8 {
		return Decision{NeedResearch: true, Query: analysis.NormalizedText, Reason: "low_confidence"}
	}
	switch strings.ToLower(analysis.Type) {
	case "geometry_proof", "unsupported":
		return Decision{NeedResearch: true, Query: analysis.NormalizedText, Reason: "complex_or_unsupported_type"}
	default:
		return Decision{Reason: "direct_solver_supported"}
	}
}

// BuildQuery 只组合确认后的题面和已归一化关系，避免把供应商原始字段直接传给搜索层。
func (Router) BuildQuery(confirmedText string, analysis *questiondomain.QuestionAnalysis, question *questiondomain.StructuredQuestion) string {
	parts := make([]string, 0, 4)
	if text := strings.TrimSpace(confirmedText); text != "" {
		parts = append(parts, text)
	}
	if analysis != nil && strings.TrimSpace(analysis.NormalizedText) != "" && analysis.NormalizedText != confirmedText {
		parts = append(parts, strings.TrimSpace(analysis.NormalizedText))
	}
	if question != nil {
		if topic := strings.TrimSpace(question.Topic); topic != "" {
			parts = append(parts, topic)
		}
		if len(question.Relations) > 0 {
			parts = append(parts, strings.Join(question.Relations, " "))
		}
	}
	return strings.Join(parts, " ")
}

// NoopSearchAdapter 显式表示本阶段未联网，避免把空结果误当成搜索成功。
type NoopSearchAdapter struct{}

func (NoopSearchAdapter) Search(_ context.Context, _ string) (researchdomain.Context, error) {
	return researchdomain.Context{Status: researchdomain.StatusUnavailable}, nil
}
