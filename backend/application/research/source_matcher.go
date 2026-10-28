package research

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	domain "github.com/guide-flow/guide-flow/backend/domain/research"
)

// SourceMatch 是本地确定性核对结果；模型分数只能作为参考，不能替代题干和关系核对。
type SourceMatch struct {
	Title      string
	Score      float64
	Readable   bool
	Matched    bool
	ShouldStop bool
	Checks     []string
}

var (
	technicalTokenPattern = regexp.MustCompile(`[A-Za-z]{2,6}|[0-9]{2,4}`)
	corruptFormulaPattern = regexp.MustCompile(`[A-Z]{4,}=[A-Z]{4,}`)
	questionNumberPattern = regexp.MustCompile(`^\s*[0-9]+[.．、)]?`)
)

// MatchSources 用确认题面中的稳定片段、年份/数字和几何标识进行本地匹配，避免把供应商的 trustScore 当成题目置信度。
func MatchSources(query string, sources []domain.Source) SourceMatch {
	queryText := normalizeMatchText(query)
	if queryText == "" || len(sources) == 0 {
		return SourceMatch{}
	}
	phrases := queryPhrases(query)
	queryTokens := technicalTokenPattern.FindAllString(strings.ToUpper(query), -1)
	best := SourceMatch{}
	for _, source := range sources {
		text := normalizeMatchText(source.Title + "\n" + source.Summary)
		if text == "" {
			continue
		}
		score := 0.0
		checks := make([]string, 0, 4)
		matchedPhrases := 0
		for _, phrase := range phrases {
			if strings.Contains(text, phrase) {
				matchedPhrases++
			}
		}
		if len(phrases) > 0 && matchedPhrases > 0 {
			score += 0.55 * float64(matchedPhrases) / float64(len(phrases))
			checks = append(checks, "题干关键片段匹配")
		}
		matchedTokens := 0
		seenTokens := map[string]struct{}{}
		for _, token := range queryTokens {
			if _, seen := seenTokens[token]; seen {
				continue
			}
			seenTokens[token] = struct{}{}
			if strings.Contains(strings.ToUpper(text), strings.ToUpper(token)) {
				matchedTokens++
			}
		}
		if len(seenTokens) > 0 {
			score += 0.25 * float64(matchedTokens) / float64(len(seenTokens))
			if matchedTokens > 0 {
				checks = append(checks, "年份和数学标识匹配")
			}
		}
		if subquestionCount(query) > 0 && subquestionCount(source.Summary) >= subquestionCount(query) {
			score += 0.2
			checks = append(checks, "小问数量匹配")
		}
		score = clampMatchScore(score)
		readable := isReadableSource(source.Summary)
		candidate := SourceMatch{Title: source.Title, Score: score, Readable: readable, Matched: score >= 0.65, Checks: checks}
		candidate.ShouldStop = candidate.Matched && candidate.Readable && candidate.Score >= 0.8
		if candidate.Score > best.Score || (candidate.Score == best.Score && candidate.Readable && !best.Readable) {
			best = candidate
		}
	}
	return best
}

func queryPhrases(query string) []string {
	query = strings.ReplaceAll(query, "geometry_numeric", "")
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("。！？；;\n\r", r)
	})
	phrases := make([]string, 0, 4)
	for _, part := range parts {
		rawLength := len([]rune(strings.TrimSpace(part)))
		part = questionNumberPattern.ReplaceAllString(part, "")
		part = normalizeMatchText(part)
		if rawLength >= 8 && len([]rune(part)) >= 4 {
			phrases = append(phrases, part)
		}
	}
	sort.Slice(phrases, func(i, j int) bool { return len([]rune(phrases[i])) > len([]rune(phrases[j])) })
	if len(phrases) > 4 {
		phrases = phrases[:4]
	}
	return phrases
}

func normalizeMatchText(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func isReadableSource(summary string) bool {
	value := strings.TrimSpace(summary)
	if value == "" || strings.ContainsRune(value, '\ufffd') {
		return false
	}
	// 公式被网页抽取成连续大写串时，保留来源用于题目命中，但禁止直接作为参考解答。
	return !corruptFormulaPattern.MatchString(value)
}

func subquestionCount(value string) int {
	return strings.Count(value, "(1)") + strings.Count(value, "（1）") + strings.Count(value, "(2)") + strings.Count(value, "（2）")
}

func clampMatchScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
