package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/guide-flow/guide-flow/backend/application/solving"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

type DemoOCR struct{}

func (DemoOCR) Recognize(_ context.Context, _ io.Reader, imageName string) (domain.QuestionDraft, error) {
	if !strings.HasPrefix(imageName, "guide-flow-demo") {
		return domain.QuestionDraft{}, fmt.Errorf("%w: 请配置真实 OCR 服务后再上传自有题目", domain.ErrDemoOCROnly)
	}
	return domain.QuestionDraft{Text: "小明看一本 120 页的书，第一天看了全书的 1/3，第二天看了剩下的 1/4。第二天看了多少页？", Confidence: .98}, nil
}

func (d DemoOCR) RecognizeWithMetadata(ctx context.Context, image io.Reader, name string) (domain.OCRResult, error) {
	draft, err := d.Recognize(ctx, image, name)
	if err != nil {
		return domain.OCRResult{}, err
	}
	analysis := &questiondomain.QuestionAnalysis{
		Grade: 5, Subject: "数学", Type: "word_problem", RawText: draft.Text, NormalizedText: draft.Text,
		Knowns:     []questiondomain.Known{{Name: "book_pages", Value: "120", Unit: "页"}},
		Asks:       []questiondomain.QuestionAsk{{Type: "calculate", Target: []string{"day_2"}}},
		Confidence: draft.Confidence, FieldConfidence: map[string]float64{"text": draft.Confidence, "numbers": draft.Confidence, "asks": draft.Confidence}, UncertainItems: []string{},
	}
	raw, _ := json.Marshal(map[string]any{"text": draft.Text, "confidence": draft.Confidence, "fieldConfidence": draft.FieldConfidence, "analysis": analysis})
	return domain.OCRResult{Draft: draft, Analysis: analysis, Provider: "demo", RawResponse: raw, CreatedAt: domain.Now()}, nil
}

type DemoStructurer struct{}

func (DemoStructurer) Structure(_ context.Context, d domain.QuestionDraft, p domain.LearnerProfile) (domain.StructuredQuestion, error) {
	return domain.StructuredQuestion{Grade: p.Grade, Topic: "fraction_word_problem", Knowns: []domain.Known{{Name: "book_pages", Value: "120", Unit: "页"}}, Relations: []string{"day_1 = total * 1/3", "day_2 = remaining * 1/4"}, Asks: []string{"day_2"}}, nil
}

// RuleBasedStructurer 是当前无题目分析模型时的保守兜底：只做题型路由和原文保留，
// 不凭空生成数字、关系或答案；复杂题交给后续能力判断，避免 Demo 数据污染真实 Lesson。
type RuleBasedStructurer struct{}

func (RuleBasedStructurer) Structure(_ context.Context, d domain.QuestionDraft, p domain.LearnerProfile) (domain.StructuredQuestion, error) {
	text := strings.TrimSpace(d.Text)
	question := domain.StructuredQuestion{Grade: p.Grade, NormalizedText: text, Asks: []string{"solve_question"}}
	if containsAny(text, "矩形", "三角形", "四边形", "平行", "垂直", "角", "几何", "证明", "圆") {
		// 没有明确坐标时，GeometryVerifier 会返回 unsupported；不能根据图片外观猜比例。
		question.Topic = "geometry_numeric"
		question.Tags = []string{"needs_geometry_analysis"}
		return question, nil
	}
	if containsAny(text, "分数", "几分之", "/", "页", "剩下") {
		question.Topic = "fraction_word_problem"
		return question, nil
	}
	question.Topic = "word_problem"
	return question, nil
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

type DemoSolver struct{}

func (DemoSolver) GenerateDraft(_ context.Context, _ domain.StructuredQuestion, _ *domain.SolutionCandidate, _ *domain.ResearchContext) (domain.SolutionDraft, domain.ModelTrace, error) {
	return domain.SolutionDraft{
		SchemaVersion: solutiondomain.DraftSchemaVersion,
		Method:        "fraction_remaining",
		Assumptions:   []string{"第一天和第二天的分数均以全书或剩余页数为基准"},
		Variables:     map[string]string{"total": "120", "completed_ratio": "1/3", "second_ratio": "1/4"},
		Steps:         []string{"先计算剩余页数", "再计算剩余页数的 1/4"},
		Expressions:   []string{"120 * (1 - 1/3) = 80", "80 * 1/4 = 20"},
		FinalAnswer:   "20 页",
		Uncertainties: []string{},
	}, domain.ModelTrace{Provider: "demo", Model: "demo-solver", PromptVersion: "solution-draft-v1", RequestID: "demo-solution"}, nil
}

func (DemoSolver) VerifyDraft(ctx context.Context, question domain.StructuredQuestion, draft domain.SolutionDraft) (domain.VerifiedSolution, error) {
	return (solving.Verifier{}).VerifyDraft(ctx, question, draft)
}

func (DemoSolver) SolveAndValidate(context.Context, domain.StructuredQuestion, *domain.SolutionCandidate) (domain.VerifiedSolution, error) {
	return domain.VerifiedSolution{
		TemplateID:        "fraction_remaining_v1",
		TemplateVersion:   1,
		Expressions:       []string{"remaining = total * (1 - completed_ratio)", "answer = remaining * second_ratio"},
		IntermediateSteps: []string{"120 * (1 - 1/3) = 80", "80 * 1/4 = 20"},
		FinalAnswer:       "20 页",
		Validation:        domain.ValidationEvidence{Status: "verified", Checks: []string{"代回原题关系成立", "单位一致"}},
	}, nil
}

type DemoPlanner struct{}

func (DemoPlanner) Plan(_ context.Context, _ domain.StructuredQuestion, verified domain.VerifiedSolution, _ domain.LearnerProfile) (domain.LessonPlan, error) {
	return domain.LessonPlan{Title: "先找出剩下的页数", Steps: []domain.LessonStep{{Title: "先找出“剩下的页数”", Explanation: "第一天看了全书的 1/3，所以还剩下全书的 2/3。", Formula: "1 - 1/3 = 2/3", Visual: domain.Visual{Type: "ratio_bar", Segments: []int{1, 2}}, VoiceText: "我们先不急着算第二天，先找出剩下的部分。"}, {Title: "再算“剩下的 1/4”", Explanation: "120 × 2/3 = 80 页，第二天看了 80 ÷ 4 = 20 页。", Formula: "120 × 2/3 = 80", Visual: domain.Visual{Type: "groups", Segments: []int{4}}, VoiceText: "把剩下的八十页平均分成四份。"}, {Title: "回头检查，答案是 20 页", Explanation: "20 × 4 = 80，和剩下的页数一致，算对啦！", Formula: "20 × 4 = 80", Visual: domain.Visual{Type: "formula_reveal"}, VoiceText: "我们倒过来检查一下，答案正确。"}}, FinalAnswer: verified.FinalAnswer, CommonMistakes: []string{"把第二天的 1/4 误认为全书的 1/4", "忘记先计算剩余页数"}, Practice: domain.Practice{Question: "一盒彩笔有 48 支，送给弟弟全盒的 1/4，送了多少支？", Answer: "12", CheckExpression: "48 * 1/4 = 12"}}, nil
}

// AdaptivePlanner 将已经验证的解法投影成讲解步骤，只使用当前题目的中间步骤、算式和答案。
// 它不生成固定示例题；无法可靠生成相似练习时留空，由前端隐藏练习区，避免出现“题目变了但答案没变”。
type AdaptivePlanner struct{}

func (AdaptivePlanner) Plan(_ context.Context, question domain.StructuredQuestion, verified domain.VerifiedSolution, _ domain.LearnerProfile) (domain.LessonPlan, error) {
	steps := make([]domain.LessonStep, 0, 3)
	for index, explanation := range verified.IntermediateSteps {
		if index >= 3 {
			break
		}
		explanation = strings.TrimSpace(explanation)
		if explanation == "" {
			continue
		}
		formula := explanation
		if index < len(verified.Expressions) && strings.TrimSpace(verified.Expressions[index]) != "" {
			formula = strings.TrimSpace(verified.Expressions[index])
		}
		steps = append(steps, domain.LessonStep{
			Title:       fmt.Sprintf("第 %d 步：先看清这一层关系", index+1),
			Explanation: explanation,
			Formula:     formula,
			Visual:      domain.Visual{Type: "formula_reveal"},
			VoiceText:   explanation,
		})
	}
	if len(steps) == 0 {
		fallback := strings.TrimSpace(verified.FinalAnswer)
		if fallback == "" {
			return domain.LessonPlan{}, fmt.Errorf("verified solution has no teachable steps")
		}
		steps = append(steps, domain.LessonStep{
			Title:       "先确认解题结果",
			Explanation: fallback,
			Formula:     fallback,
			Visual:      domain.Visual{Type: "formula_reveal"},
			VoiceText:   fallback,
		})
	}

	title := "按步骤看懂这道题"
	if question.Topic == "geometry_numeric" {
		title = "先看清图形关系，再验证结论"
	} else if question.Topic == "fraction_word_problem" {
		title = "先找出题目中的数量关系"
	}
	return domain.LessonPlan{
		Title:       title,
		Steps:       steps,
		FinalAnswer: verified.FinalAnswer,
		Practice:    domain.Practice{},
	}, nil
}

type PassRenderer struct{}

func (PassRenderer) Render(_ context.Context, p domain.LessonPlan) (domain.LessonPlan, error) {
	return p, nil
}
