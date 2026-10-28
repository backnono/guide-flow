package lesson_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/guide-flow/guide-flow/backend/application/lesson"
	appresearch "github.com/guide-flow/guide-flow/backend/application/research"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
	researchdomain "github.com/guide-flow/guide-flow/backend/domain/research"
	"github.com/guide-flow/guide-flow/backend/infra/ai"
	"github.com/guide-flow/guide-flow/backend/infra/objectstore"
	"github.com/guide-flow/guide-flow/backend/infra/persistence/memory"
)

type immediateQueue struct{ ids []string }

func (q *immediateQueue) Enqueue(id string) error { q.ids = append(q.ids, id); return nil }

func TestCreateAndConfirm(t *testing.T) {
	repo := memory.NewLessonRepository()
	queue := &immediateQueue{}
	service := lesson.NewService(repo, queue, objectstore.NewMemoryImageStore())
	l, err := service.CreateLesson(context.Background(), strings.NewReader("image"), "question.jpg", domain.LearnerProfile{Grade: 5})
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != domain.StatusCreated || len(queue.ids) != 1 {
		t.Fatalf("unexpected creation: %+v", l)
	}
}

type researchedVision struct{}

func (researchedVision) Analyze(context.Context, io.Reader, string) (domain.OCRResult, error) {
	return domain.OCRResult{
		Draft:    domain.QuestionDraft{Text: "证明三角形中的长度关系", Confidence: 1},
		Analysis: &questiondomain.QuestionAnalysis{Type: "geometry_proof", NormalizedText: "证明三角形中的长度关系", Confidence: .95},
		// 用受支持的数值题型承载检索路由，确保本测试只验证阶段解耦而不混入几何校验范围。
		StructuredQuestion: &questiondomain.StructuredQuestion{Topic: "arithmetic", Relations: []string{"AB⊥CD"}},
	}, nil
}

type fixedSearch struct {
	context researchdomain.Context
	called  int
}

func (s *fixedSearch) Search(context.Context, string) (researchdomain.Context, error) {
	s.called++
	return s.context, nil
}

func TestResearchReadyDoesNotGenerateTutorialUntilRequested(t *testing.T) {
	repo := memory.NewLessonRepository()
	images := objectstore.NewMemoryImageStore()
	queue := &immediateQueue{}
	service := lesson.NewService(repo, queue, images)
	created, err := service.CreateLesson(context.Background(), strings.NewReader("image"), "geometry.png", domain.LearnerProfile{Grade: 8})
	if err != nil {
		t.Fatal(err)
	}
	search := &fixedSearch{context: researchdomain.Context{
		Status:            researchdomain.StatusVerified,
		Sources:           []researchdomain.Source{{Title: "标准题解", URL: "https://example.com/answer", Summary: "原题和步骤一致", TrustScore: .95}},
		MatchedQuestion:   &researchdomain.MatchedQuestion{Title: "某年中考几何题", MatchScore: .95, Checks: []string{"题干匹配", "图形关系匹配"}},
		ReferenceSolution: &researchdomain.ReferenceSolution{Status: "verified", Steps: []string{"建立辅助关系"}, FinalAnswer: "结论成立"},
		RetrievedAt:       time.Now(),
	}}
	worker := lesson.NewWorker(repo, ai.DemoOCR{}, ai.RuleBasedStructurer{}, ai.DemoSolver{}, ai.DemoPlanner{}, ai.PassRenderer{}, images).
		WithVisionAnalyzer(researchedVision{}).
		WithResearchAdapters(appresearch.Router{}, search)
	if err := worker.Process(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	current, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.StatusAwaitingConfirmation {
		t.Fatalf("expected confirmation, got %s", current.Status)
	}
	if _, err := service.ConfirmQuestion(context.Background(), created.ID, current.QuestionDraft.Text); err != nil {
		t.Fatal(err)
	}
	if err := worker.Process(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = repo.Get(context.Background(), created.ID)
	if current.Status != domain.StatusResearchReady || current.TutorialStatus != domain.TutorialNotStarted || current.LessonPlan != nil {
		t.Fatalf("research must pause before tutorial: %+v", current)
	}
	if search.called != 1 {
		t.Fatalf("expected one research call, got %d", search.called)
	}
	if _, err := service.RequestTutorial(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if err := worker.Process(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = repo.Get(context.Background(), created.ID)
	if current.Status != domain.StatusReady || current.TutorialStatus != domain.TutorialReady || current.ResearchContext.ReferenceSolution == nil {
		t.Fatalf("tutorial should preserve research context: %+v", current)
	}
	if search.called != 1 {
		t.Fatalf("tutorial must not repeat research, got %d calls", search.called)
	}
}

type unavailableSearch struct{}

func (unavailableSearch) Search(context.Context, string) (researchdomain.Context, error) {
	return researchdomain.Context{ErrorCode: "web_search_error"}, context.DeadlineExceeded
}

func TestResearchFailureStopsBeforeSolver(t *testing.T) {
	repo := memory.NewLessonRepository()
	images := objectstore.NewMemoryImageStore()
	queue := &immediateQueue{}
	service := lesson.NewService(repo, queue, images)
	created, _ := service.CreateLesson(context.Background(), strings.NewReader("image"), "geometry.png", domain.LearnerProfile{Grade: 8})
	worker := lesson.NewWorker(repo, ai.DemoOCR{}, ai.RuleBasedStructurer{}, ai.DemoSolver{}, ai.DemoPlanner{}, ai.PassRenderer{}, images).
		WithVisionAnalyzer(researchedVision{}).
		WithResearchAdapters(appresearch.Router{}, unavailableSearch{})
	_ = worker.Process(context.Background(), created.ID)
	current, _ := repo.Get(context.Background(), created.ID)
	_, _ = service.ConfirmQuestion(context.Background(), created.ID, current.QuestionDraft.Text)
	_ = worker.Process(context.Background(), created.ID)
	current, _ = repo.Get(context.Background(), created.ID)
	if current.Status != domain.StatusResearchUnavailable || current.SolutionDraft != nil || current.TutorialStatus != domain.TutorialNotStarted || current.ResearchContext == nil || current.ResearchContext.ErrorCode != "web_search_error" {
		t.Fatalf("research failure must stop before tutorial: %+v", current)
	}
}

func TestWorkerProducesReadyLesson(t *testing.T) {
	repo := memory.NewLessonRepository()
	images := objectstore.NewMemoryImageStore()
	queue := &immediateQueue{}
	service := lesson.NewService(repo, queue, images)
	l, _ := service.CreateLesson(context.Background(), strings.NewReader("image"), "guide-flow-demo.png", domain.LearnerProfile{Grade: 5})
	w := lesson.NewWorker(repo, ai.DemoOCR{}, ai.DemoStructurer{}, ai.DemoSolver{}, ai.DemoPlanner{}, ai.PassRenderer{}, images)
	w.Process(context.Background(), l.ID)
	got, err := repo.Get(context.Background(), l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusAwaitingConfirmation {
		t.Fatalf("unexpected result: %+v", got)
	}
	if _, err := service.ConfirmQuestion(context.Background(), l.ID, got.QuestionDraft.Text); err != nil {
		t.Fatal(err)
	}
	if err := w.Process(context.Background(), l.ID); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(context.Background(), l.ID)
	if err != nil || got.Status != domain.StatusReady || got.LessonPlan == nil || len(got.LessonPlan.Steps) != 3 || got.SolutionDraft == nil {
		t.Fatalf("unexpected confirmed result: %+v", got)
	}
	result, err := service.SubmitPractice(context.Background(), l.ID, "12")
	if err != nil || !result.Correct {
		t.Fatalf("practice failed: %+v %v", result, err)
	}
}

func TestWorkerPausesForLowConfidence(t *testing.T) {
	repo := memory.NewLessonRepository()
	images := objectstore.NewMemoryImageStore()
	queue := &immediateQueue{}
	service := lesson.NewService(repo, queue, images)
	l, err := service.CreateLesson(context.Background(), strings.NewReader("image"), "blurry.jpg", domain.LearnerProfile{Grade: 5})
	if err != nil {
		t.Fatal(err)
	}
	w := lesson.NewWorker(repo, lowConfidenceOCR{}, ai.DemoStructurer{}, ai.DemoSolver{}, ai.DemoPlanner{}, ai.PassRenderer{}, images)
	w.Process(context.Background(), l.ID)
	got, err := repo.Get(context.Background(), l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusAwaitingConfirmation {
		t.Fatalf("expected confirmation, got %s", got.Status)
	}
	if _, err := service.ConfirmQuestion(context.Background(), l.ID, "修正后的题目"); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmEditedQuestionInvalidatesDerivedResults(t *testing.T) {
	repo := memory.NewLessonRepository()
	images := objectstore.NewMemoryImageStore()
	queue := &immediateQueue{}
	service := lesson.NewService(repo, queue, images)
	created, err := service.CreateLesson(context.Background(), strings.NewReader("image"), "blurry.jpg", domain.LearnerProfile{Grade: 5})
	if err != nil {
		t.Fatal(err)
	}
	w := lesson.NewWorker(repo, lowConfidenceOCR{}, ai.DemoStructurer{}, ai.DemoSolver{}, ai.DemoPlanner{}, ai.PassRenderer{}, images)
	if err := w.Process(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	current, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Question = &domain.StructuredQuestion{Topic: "geometry_numeric"}
	current.QuestionAnalysis = &domain.QuestionAnalysis{NormalizedText: "旧题面"}
	current.ResearchContext = &domain.ResearchContext{Status: "used"}
	current.SolutionCandidates = []domain.SolutionCandidate{{TemplateID: "old"}}
	current.SolutionDraft = &domain.SolutionDraft{FinalAnswer: "旧答案"}
	current.SolutionTrace = &domain.ModelTrace{Provider: "old"}
	current.VerifiedSolution = &domain.VerifiedSolution{FinalAnswer: "旧答案"}
	current.LessonPlan = &domain.LessonPlan{Title: "旧讲解"}
	if err := repo.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmQuestion(context.Background(), created.ID, "全新的题面"); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Question != nil || updated.QuestionAnalysis != nil || updated.ResearchContext != nil || len(updated.SolutionCandidates) != 0 || updated.SolutionDraft != nil || updated.SolutionTrace != nil || updated.VerifiedSolution != nil || updated.LessonPlan != nil || updated.QuestionVersionID != 0 {
		t.Fatalf("editing question must invalidate all derived results: %+v", updated)
	}
}

type lowConfidenceOCR struct{}

func (lowConfidenceOCR) Recognize(context.Context, io.Reader, string) (domain.QuestionDraft, error) {
	return domain.QuestionDraft{Text: "看不清", Confidence: .4}, nil
}
