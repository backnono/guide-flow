package retrieval

import (
	"context"
	"sort"
	"strings"

	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

type TemplateRepository interface {
	ListActive(context.Context) ([]solutiondomain.Template, error)
}

type TemplateStore interface {
	TemplateRepository
	CreateTemplate(context.Context, solutiondomain.Template) error
	PublishTemplate(context.Context, string, int64) error
}

type TemplateService interface {
	Create(context.Context, solutiondomain.Template) error
	Publish(context.Context, string, int64) error
}

// Matcher 是检索模块对用例层提供的窄接口；具体实现可组合结构化、文本、向量和重排策略。
type Matcher interface {
	Match(context.Context, questiondomain.StructuredQuestion) ([]solutiondomain.Candidate, error)
}

type NoopMatcher struct{}

func (NoopMatcher) Match(context.Context, questiondomain.StructuredQuestion) ([]solutiondomain.Candidate, error) {
	return nil, nil
}

// StructuredMatcher 先用可解释的结构化信号匹配；后续向量检索和重排只需替换 Matcher 实现。
type StructuredMatcher struct {
	repo TemplateRepository
}

func NewStructuredMatcher(repo TemplateRepository) *StructuredMatcher {
	return &StructuredMatcher{repo: repo}
}

func (m *StructuredMatcher) Match(ctx context.Context, question questiondomain.StructuredQuestion) ([]solutiondomain.Candidate, error) {
	templates, err := m.repo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	type scored struct {
		template solutiondomain.Template
		score    float64
		reason   string
	}
	var matches []scored
	for _, template := range templates {
		score := 0.0
		reasons := make([]string, 0, 3)
		if contains(template.Topics, question.Topic) {
			score += 0.5
			reasons = append(reasons, "topic")
		}
		for _, point := range question.KnowledgePoints {
			if contains(template.KnowledgePoints, point) {
				score += 0.15
			}
		}
		if template.Formula != "" && template.Formula == question.Formula {
			score += 0.35
			reasons = append(reasons, "formula")
		}
		if score >= 0.5 {
			matches = append(matches, scored{template: template, score: score, reason: strings.Join(reasons, "+")})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	result := make([]solutiondomain.Candidate, 0, len(matches))
	for _, match := range matches {
		result = append(result, solutiondomain.Candidate{TemplateID: match.template.ID, TemplateVersion: match.template.Version, Score: match.score, MatchReason: match.reason})
	}
	return result, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
