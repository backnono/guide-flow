package retrieval

import (
	"context"
	"testing"

	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

type templateRepo struct{ templates []solutiondomain.Template }

func (r templateRepo) ListActive(context.Context) ([]solutiondomain.Template, error) {
	return r.templates, nil
}

func TestStructuredMatcherRanksTopicAndFormula(t *testing.T) {
	matcher := NewStructuredMatcher(templateRepo{templates: []solutiondomain.Template{
		{ID: "topic-only", Version: 1, Topics: []string{"fraction_word_problem"}},
		{ID: "exact", Version: 2, Topics: []string{"fraction_word_problem"}, KnowledgePoints: []string{"remaining"}, Formula: "total-total*ratio"},
	}})
	candidates, err := matcher.Match(context.Background(), questiondomain.StructuredQuestion{
		Topic: "fraction_word_problem", KnowledgePoints: []string{"remaining"}, Formula: "total-total*ratio",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].TemplateID != "exact" || candidates[0].Score <= candidates[1].Score {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
}
