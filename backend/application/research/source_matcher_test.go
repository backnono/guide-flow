package research

import (
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/research"
)

func TestMatchSourcesStopsOnExactReadableQuestion(t *testing.T) {
	query := "3.（2020福建）如图，△ADE由△ABC绕点A按逆时针方向旋转90°得到，且点B的对应点D恰好落在BC的延长线上，AD，EC相交于点P。 (1)求∠BDE的度数；(2)F是EC延长线上的点，且∠CDF=∠DAC。①判断DF和PF的数量关系，并证明；②求证：EP/PF=PC/CF。 geometry_numeric"
	sources := []domain.Source{{
		Title:   "2020年福建中考数学第24题",
		URL:     "https://example.com/problem",
		Summary: "2020福建 如图，△ADE由△ABC绕点A按逆时针方向旋转90°得到，且点B的对应点D恰好落在BC的延长线上，AD，EC相交于点P。 (1)求∠BDE的度数；(2)F是EC延长线上的点，且∠CDF=∠DAC。①判断DF和PF的数量关系，并证明；②求证：EP/PF=PC/CF。",
	}}
	match := MatchSources(query, sources)
	if !match.Matched || !match.Readable || !match.ShouldStop || match.Score < 0.8 {
		t.Fatalf("expected early stop for exact readable source: %+v", match)
	}
}

func TestMatchSourcesKeepsCorruptedSourceAsEvidenceOnly(t *testing.T) {
	sources := []domain.Source{{
		Title:   "2020福建中考数学第24题",
		URL:     "https://example.com/problem",
		Summary: "2020福建 △ADE旋转90° AD与EC相交于P；答案 EPPF=PCCF",
	}}
	match := MatchSources("2020福建 △ADE旋转90° AD与EC相交于P", sources)
	if match.Readable || match.ShouldStop {
		t.Fatalf("corrupted source must not be used as readable answer: %+v", match)
	}
}

func TestMatchSourcesMatchesShortStablePhrase(t *testing.T) {
	match := MatchSources("2020福建原题检索", []domain.Source{{Title: "原题来源", Summary: "2020福建原题检索搜索摘要"}})
	if !match.Matched || !match.Readable || !match.ShouldStop {
		t.Fatalf("expected short stable phrase to stop: %+v", match)
	}
}
