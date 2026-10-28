package memory

import (
	"context"
	"sync"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type LessonRepository struct {
	mu      sync.RWMutex
	lessons map[string]*domain.Lesson
}

func NewLessonRepository() *LessonRepository {
	return &LessonRepository{lessons: map[string]*domain.Lesson{}}
}

func (r *LessonRepository) Create(_ context.Context, lesson *domain.Lesson) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.lessons[lesson.ID]; exists {
		return domain.ErrAlreadyExists
	}
	if lesson.Revision == 0 {
		lesson.Revision = 1
	}
	r.lessons[lesson.ID] = clone(lesson)
	return nil
}

func (r *LessonRepository) Get(_ context.Context, id string) (*domain.Lesson, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lesson, ok := r.lessons[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(lesson), nil
}

func (r *LessonRepository) GetByIdempotencyKey(_ context.Context, key string) (*domain.Lesson, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, lesson := range r.lessons {
		if lesson.IdempotencyKey == key && key != "" {
			return clone(lesson), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *LessonRepository) Update(_ context.Context, lesson *domain.Lesson) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.lessons[lesson.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if lesson.Revision != current.Revision {
		return domain.ErrConcurrentUpdate
	}
	lesson.Revision++
	for i := range lesson.StatusEvents {
		if lesson.StatusEvents[i].Revision == 0 {
			lesson.StatusEvents[i].Revision = lesson.Revision
		}
	}
	r.lessons[lesson.ID] = clone(lesson)
	return nil
}

func clone(lesson *domain.Lesson) *domain.Lesson {
	copy := *lesson
	if lesson.QuestionDraft != nil {
		draft := *lesson.QuestionDraft
		copy.QuestionDraft = &draft
	}
	if lesson.QuestionAnalysis != nil {
		analysis := *lesson.QuestionAnalysis
		analysis.Entities = append([]domain.DiagramEntity(nil), lesson.QuestionAnalysis.Entities...)
		analysis.Relations = append([]domain.QuestionRelation(nil), lesson.QuestionAnalysis.Relations...)
		analysis.Knowns = append([]domain.Known(nil), lesson.QuestionAnalysis.Knowns...)
		analysis.Asks = append([]domain.QuestionAsk(nil), lesson.QuestionAnalysis.Asks...)
		analysis.FieldConfidence = cloneFloatMap(lesson.QuestionAnalysis.FieldConfidence)
		analysis.UncertainItems = append([]string(nil), lesson.QuestionAnalysis.UncertainItems...)
		copy.QuestionAnalysis = &analysis
	}
	if lesson.ResearchContext != nil {
		researchContext := *lesson.ResearchContext
		researchContext.Sources = append([]domain.ResearchSource(nil), lesson.ResearchContext.Sources...)
		if lesson.ResearchContext.MatchedQuestion != nil {
			matched := *lesson.ResearchContext.MatchedQuestion
			matched.Checks = append([]string(nil), matched.Checks...)
			researchContext.MatchedQuestion = &matched
		}
		if lesson.ResearchContext.ReferenceSolution != nil {
			solution := *lesson.ResearchContext.ReferenceSolution
			solution.Steps = append([]string(nil), solution.Steps...)
			solution.Uncertainties = append([]string(nil), solution.Uncertainties...)
			researchContext.ReferenceSolution = &solution
		}
		copy.ResearchContext = &researchContext
	}
	if lesson.SolutionDraft != nil {
		draft := *lesson.SolutionDraft
		draft.Assumptions = append([]string(nil), lesson.SolutionDraft.Assumptions...)
		draft.Steps = append([]string(nil), lesson.SolutionDraft.Steps...)
		draft.Expressions = append([]string(nil), lesson.SolutionDraft.Expressions...)
		draft.Uncertainties = append([]string(nil), lesson.SolutionDraft.Uncertainties...)
		draft.Variables = cloneStringMap(lesson.SolutionDraft.Variables)
		copy.SolutionDraft = &draft
	}
	if lesson.SolutionTrace != nil {
		trace := *lesson.SolutionTrace
		copy.SolutionTrace = &trace
	}
	if lesson.OCRTrace != nil {
		trace := *lesson.OCRTrace
		copy.OCRTrace = &trace
	}
	copy.OCRTraces = append([]domain.OCRTrace(nil), lesson.OCRTraces...)
	if lesson.Question != nil {
		question := *lesson.Question
		question.Knowns = append([]domain.Known(nil), lesson.Question.Knowns...)
		question.Relations = append([]string(nil), lesson.Question.Relations...)
		question.Asks = append([]string(nil), lesson.Question.Asks...)
		copy.Question = &question
	}
	copy.QuestionVersions = append([]domain.QuestionVersion(nil), lesson.QuestionVersions...)
	copy.SolutionCandidates = append([]domain.SolutionCandidate(nil), lesson.SolutionCandidates...)
	if lesson.VerifiedSolution != nil {
		verified := *lesson.VerifiedSolution
		verified.Parameters = cloneStringMap(lesson.VerifiedSolution.Parameters)
		verified.Expressions = append([]string(nil), lesson.VerifiedSolution.Expressions...)
		verified.IntermediateSteps = append([]string(nil), lesson.VerifiedSolution.IntermediateSteps...)
		verified.Validation.Checks = append([]string(nil), lesson.VerifiedSolution.Validation.Checks...)
		copy.VerifiedSolution = &verified
	}
	if lesson.LessonPlan != nil {
		plan := *lesson.LessonPlan
		plan.Steps = append([]domain.LessonStep(nil), lesson.LessonPlan.Steps...)
		copy.LessonPlan = &plan
	}
	copy.StatusEvents = append([]domain.StatusEvent(nil), lesson.StatusEvents...)
	copy.PracticeResults = append([]domain.PracticeResult(nil), lesson.PracticeResults...)
	if lesson.LastErrorAt != nil {
		timestamp := *lesson.LastErrorAt
		copy.LastErrorAt = &timestamp
	}
	return &copy
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	if values == nil {
		return nil
	}
	copy := make(map[string]float64, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
