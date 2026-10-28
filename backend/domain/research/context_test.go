package research

import "testing"

func TestCompactForTutorialKeepsAtMostThreeUsefulSources(t *testing.T) {
	longSummary := "题解 " + string(make([]rune, 1600))
	context := Context{
		Query:           "原题",
		Status:          StatusFound,
		MatchedQuestion: &MatchedQuestion{Title: "命中题", MatchScore: .9},
		Sources: []Source{
			{Title: "普通来源", URL: "https://example.com/1", Summary: "来源一", TrustScore: .8},
			{Title: "命中题", URL: "https://example.com/2", Summary: longSummary, TrustScore: .5},
			{Title: "来源三", URL: "https://example.com/3", Summary: "来源三", TrustScore: .7},
			{Title: "来源四", URL: "https://example.com/4", Summary: "来源四", TrustScore: .6},
		},
	}

	compacted := CompactForTutorial(context)
	if len(compacted.Sources) != 3 {
		t.Fatalf("expected at most three sources, got %d", len(compacted.Sources))
	}
	if compacted.Sources[0].Title != "命中题" {
		t.Fatalf("matched source should be prioritized, got %+v", compacted.Sources[0])
	}
	if len([]rune(compacted.Sources[0].Summary)) > TutorialEvidenceSummaryLimit {
		t.Fatalf("source summary was not bounded: %d", len([]rune(compacted.Sources[0].Summary)))
	}
	if len(context.Sources[1].Summary) <= len(compacted.Sources[0].Summary) {
		t.Fatal("compaction must not mutate the persisted research context")
	}
}
