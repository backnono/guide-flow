package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	lesson "github.com/guide-flow/guide-flow/backend/application/lesson"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

type LessonRepository struct {
	db *gorm.DB
}

func NewLessonRepository(db *gorm.DB) *LessonRepository {
	return &LessonRepository{db: db}
}

func (r *LessonRepository) Create(ctx context.Context, lesson *domain.Lesson) error {
	if lesson.Revision == 0 {
		lesson.Revision = 1
	}
	return r.db.WithContext(ctx).Transaction(
		func(tx *gorm.DB) error {
			model, err := toLessonModel(lesson)
			if err != nil {
				return err
			}
			if err = tx.Create(&model).Error; err != nil {
				return err
			}
			if err = saveChildren(tx, lesson, lesson.Revision); err != nil {
				return err
			}
			return tx.Create(&OutboxEventModel{LessonID: lesson.ID, EventType: "lesson.created", Status: "pending", CreatedAt: lesson.CreatedAt}).Error
		},
	)
}

func (r *LessonRepository) PendingOutbox(ctx context.Context, limit int) ([]lesson.OutboxEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []OutboxEventModel
	if err := r.db.WithContext(ctx).Where("status = ?", "pending").Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]lesson.OutboxEvent, 0, len(rows))
	for _, row := range rows {
		result = append(result, lesson.OutboxEvent{ID: row.ID, LessonID: row.LessonID, Type: row.EventType})
	}
	return result, nil
}

func (r *LessonRepository) MarkOutboxPublished(ctx context.Context, id uint64) error {
	now := domain.Now()
	return r.db.WithContext(ctx).Model(&OutboxEventModel{}).Where("id = ? AND status = ?", id, "pending").Updates(map[string]any{"status": "published", "published_at": now}).Error
}

func (r *LessonRepository) Get(ctx context.Context, id string) (*domain.Lesson, error) {
	var model LessonModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return r.loadLesson(ctx, model)
}

func (r *LessonRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Lesson, error) {
	var model LessonModel
	if err := r.db.WithContext(ctx).First(&model, "idempotency_key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return r.loadLesson(ctx, model)
}

func (r *LessonRepository) Update(ctx context.Context, lesson *domain.Lesson) error {
	return r.db.WithContext(ctx).Transaction(
		func(tx *gorm.DB) error {
			var current LessonModel
			// 锁住当前行后再校验 revision，避免两个 Worker 同时基于旧快照写回。
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(
				&current,
				"id = ?",
				lesson.ID,
			).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domain.ErrNotFound
				}
				return err
			}
			if current.Revision != lesson.Revision {
				return domain.ErrConcurrentUpdate
			}
			nextRevision := lesson.Revision + 1
			model, err := toLessonModel(lesson)
			if err != nil {
				return err
			}
			model.Revision = nextRevision
			updates := map[string]any{
				"status":                   model.Status,
				"learner_profile_json":     model.LearnerProfileJSON,
				"image_name":               model.ImageName,
				"idempotency_key":          model.IdempotencyKey,
				"image_object_key":         model.ImageObjectKey,
				"thumbnail_object_key":     model.ThumbnailObjectKey,
				"question_draft_json":      model.QuestionDraftJSON,
				"question_analysis_json":   model.QuestionAnalysisJSON,
				"research_context_json":    model.ResearchContextJSON,
				"research_status":          model.ResearchStatus,
				"tutorial_status":          model.TutorialStatus,
				"solution_draft_json":      model.SolutionDraftJSON,
				"solution_trace_json":      model.SolutionTraceJSON,
				"current_question_version": model.CurrentQuestionVersion,
				"question_json":            model.QuestionJSON,
				"question_id":              model.QuestionID,
				"question_version_id":      model.QuestionVersionID,
				"solution_candidates_json": model.SolutionCandidatesJSON,
				"verified_solution_json":   model.VerifiedSolutionJSON,
				"error":                    model.Error,
				"error_code":               model.ErrorCode,
				"failure_kind":             model.FailureKind,
				"failed_from":              model.FailedFrom,
				"retry_count":              model.RetryCount,
				"last_error_at":            model.LastErrorAt,
				"revision":                 model.Revision,
				"updated_at":               model.UpdatedAt,
			}
			if err = tx.Model(&LessonModel{}).Where(
				"id = ? AND revision = ?",
				lesson.ID,
				lesson.Revision,
			).Updates(updates).Error; err != nil {
				return err
			}
			if err = saveChildren(tx, lesson, nextRevision); err != nil {
				return err
			}
			lesson.Revision = nextRevision
			return nil
		},
	)
}

func (r *LessonRepository) loadLesson(ctx context.Context, model LessonModel) (*domain.Lesson, error) {
	lesson, err := fromLessonModel(model)
	if err != nil {
		return nil, err
	}
	db := r.db.WithContext(ctx)
	var versions []QuestionVersionModel
	if err = db.Where("lesson_id = ?", lesson.ID).Order("version_id").Find(&versions).Error; err != nil {
		return nil, err
	}
	var ocrTraces []OCRTraceModel
	if err = db.Where("lesson_id = ?", lesson.ID).Order("created_at, id").Find(&ocrTraces).Error; err != nil {
		return nil, err
	}
	for _, trace := range ocrTraces {
		item := domain.OCRTrace{Provider: trace.Provider, RequestID: trace.RequestID, PromptVersion: trace.PromptVersion, SchemaVersion: trace.SchemaVersion, RawResponse: trace.RawResponse, FinishReason: trace.FinishReason, ParseError: trace.ParseError, ParseErrorPath: trace.ParseErrorPath, DurationMs: trace.DurationMs, CreatedAt: trace.CreatedAt}
		lesson.OCRTraces = append(lesson.OCRTraces, item)
	}
	if len(lesson.OCRTraces) > 0 {
		lesson.OCRTrace = &lesson.OCRTraces[len(lesson.OCRTraces)-1]
	}
	for _, version := range versions {
		lesson.QuestionVersions = append(
			lesson.QuestionVersions,
			domain.QuestionVersion{
				ID:         version.VersionID,
				Text:       version.Text,
				Source:     version.Source,
				Confidence: version.Confidence,
				CreatedAt:  version.CreatedAt,
			},
		)
	}
	var plan LessonPlanModel
	if err = db.First(&plan, "lesson_id = ?", lesson.ID).Error; err == nil {
		lesson.LessonPlan = &domain.LessonPlan{}
		if err = json.Unmarshal(plan.PlanJSON, lesson.LessonPlan); err != nil {
			return nil, fmt.Errorf("decode lesson plan: %w", err)
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if len(model.SolutionCandidatesJSON) > 0 {
		if err := json.Unmarshal(model.SolutionCandidatesJSON, &lesson.SolutionCandidates); err != nil {
			return nil, err
		}
	}
	if len(model.VerifiedSolutionJSON) > 0 {
		lesson.VerifiedSolution = &domain.VerifiedSolution{}
		if err := json.Unmarshal(model.VerifiedSolutionJSON, lesson.VerifiedSolution); err != nil {
			return nil, err
		}
	}
	var events []StatusEventModel
	if err = db.Where("lesson_id = ?", lesson.ID).Order("revision").Find(&events).Error; err != nil {
		return nil, err
	}
	for _, event := range events {
		lesson.StatusEvents = append(
			lesson.StatusEvents,
			domain.StatusEvent{
				Revision:  event.Revision,
				From:      domain.LessonStatus(valueOrEmpty(event.FromStatus)),
				To:        domain.LessonStatus(event.ToStatus),
				Stage:     event.Stage,
				ErrorCode: valueOrEmpty(event.ErrorCode),
				CreatedAt: event.CreatedAt,
			},
		)
	}
	var results []PracticeResultModel
	if err = db.Where("lesson_id = ?", lesson.ID).Order("attempt").Find(&results).Error; err != nil {
		return nil, err
	}
	for _, result := range results {
		lesson.PracticeResults = append(
			lesson.PracticeResults,
			domain.PracticeResult{
				Attempt:        result.Attempt,
				Answer:         result.Answer,
				Correct:        result.Correct,
				Feedback:       result.Feedback,
				ExpectedAnswer: result.ExpectedAnswer,
			},
		)
	}
	return lesson, nil
}

func saveChildren(tx *gorm.DB, lesson *domain.Lesson, revision int64) error {
	for _, version := range lesson.QuestionVersions {
		if err := tx.Save(
			&QuestionVersionModel{
				LessonID:   lesson.ID,
				VersionID:  version.ID,
				Text:       version.Text,
				Source:     version.Source,
				Confidence: version.Confidence,
				CreatedAt:  version.CreatedAt,
			},
		).Error; err != nil {
			return err
		}
	}
	traces := lesson.OCRTraces
	if len(traces) == 0 && lesson.OCRTrace != nil {
		traces = []domain.OCRTrace{*lesson.OCRTrace}
	}
	for _, trace := range traces {
		model := OCRTraceModel{LessonID: lesson.ID, Provider: trace.Provider, RequestID: trace.RequestID, PromptVersion: trace.PromptVersion, SchemaVersion: trace.SchemaVersion, RawResponse: trace.RawResponse, FinishReason: trace.FinishReason, ParseError: trace.ParseError, ParseErrorPath: trace.ParseErrorPath, DurationMs: trace.DurationMs, CreatedAt: trace.CreatedAt}
		query := tx.Where("lesson_id = ? AND request_id = ?", lesson.ID, trace.RequestID)
		if err := query.Assign(model).FirstOrCreate(&model).Error; err != nil {
			return err
		}
	}
	if lesson.LessonPlan == nil {
		if err := tx.Delete(&LessonPlanModel{}, "lesson_id = ?", lesson.ID).Error; err != nil {
			return err
		}
	} else {
		plan, err := json.Marshal(lesson.LessonPlan)
		if err != nil {
			return err
		}
		if err = tx.Save(
			&LessonPlanModel{
				LessonID:  lesson.ID,
				PlanJSON:  plan,
				CreatedAt: lesson.CreatedAt,
				UpdatedAt: lesson.UpdatedAt,
			},
		).Error; err != nil {
			return err
		}
	}
	for _, result := range lesson.PracticeResults {
		if err := tx.Save(
			&PracticeResultModel{
				LessonID:       lesson.ID,
				Attempt:        result.Attempt,
				Answer:         result.Answer,
				Correct:        result.Correct,
				Feedback:       result.Feedback,
				ExpectedAnswer: result.ExpectedAnswer,
				CreatedAt:      lesson.UpdatedAt,
			},
		).Error; err != nil {
			return err
		}
	}
	for i := range lesson.StatusEvents {
		event := lesson.StatusEvents[i]
		if event.Revision == 0 {
			event.Revision = revision
			lesson.StatusEvents[i].Revision = revision
		}
		if err := tx.Save(
			&StatusEventModel{
				LessonID:   lesson.ID,
				Revision:   event.Revision,
				FromStatus: stringPtr(string(event.From)),
				ToStatus:   string(event.To),
				Stage:      event.Stage,
				ErrorCode:  stringPtr(event.ErrorCode),
				CreatedAt:  event.CreatedAt,
			},
		).Error; err != nil {
			return err
		}
	}
	return nil
}

func toLessonModel(lesson *domain.Lesson) (LessonModel, error) {
	profile, err := json.Marshal(lesson.LearnerProfile)
	if err != nil {
		return LessonModel{}, err
	}
	draft, err := optionalJSON(lesson.QuestionDraft)
	if err != nil {
		return LessonModel{}, err
	}
	analysis, err := optionalJSON(lesson.QuestionAnalysis)
	if err != nil {
		return LessonModel{}, err
	}
	researchContext, err := optionalJSON(lesson.ResearchContext)
	if err != nil {
		return LessonModel{}, err
	}
	solutionDraft, err := optionalJSON(lesson.SolutionDraft)
	if err != nil {
		return LessonModel{}, err
	}
	solutionTrace, err := optionalJSON(lesson.SolutionTrace)
	if err != nil {
		return LessonModel{}, err
	}
	question, err := optionalJSON(lesson.Question)
	if err != nil {
		return LessonModel{}, err
	}
	candidates, err := optionalJSON(lesson.SolutionCandidates)
	if err != nil {
		return LessonModel{}, err
	}
	verified, err := optionalJSON(lesson.VerifiedSolution)
	if err != nil {
		return LessonModel{}, err
	}
	return LessonModel{
		ID:                     lesson.ID,
		Status:                 string(lesson.Status),
		LearnerProfileJSON:     profile,
		ImageName:              lesson.ImageName,
		IdempotencyKey:         lesson.IdempotencyKey,
		ImageObjectKey:         lesson.ImageObjectKey,
		ThumbnailObjectKey:     lesson.ThumbnailObjectKey,
		QuestionDraftJSON:      draft,
		QuestionAnalysisJSON:   analysis,
		ResearchContextJSON:    researchContext,
		ResearchStatus:         string(lesson.ResearchStatus),
		TutorialStatus:         string(lesson.TutorialStatus),
		SolutionDraftJSON:      solutionDraft,
		SolutionTraceJSON:      solutionTrace,
		CurrentQuestionVersion: lesson.CurrentQuestionVersion,
		QuestionJSON:           question,
		QuestionID:             lesson.QuestionID,
		QuestionVersionID:      lesson.QuestionVersionID,
		SolutionCandidatesJSON: candidates,
		VerifiedSolutionJSON:   verified,
		Error:                  stringPtr(lesson.Error),
		ErrorCode:              stringPtr(lesson.ErrorCode),
		FailureKind:            stringPtr(string(lesson.FailureKind)),
		FailedFrom:             stringPtr(string(lesson.FailedFrom)),
		RetryCount:             lesson.RetryCount,
		LastErrorAt:            lesson.LastErrorAt,
		Revision:               lesson.Revision,
		CreatedAt:              lesson.CreatedAt,
		UpdatedAt:              lesson.UpdatedAt,
	}, nil
}

func fromLessonModel(model LessonModel) (*domain.Lesson, error) {
	researchStatus := domain.ResearchStatus(model.ResearchStatus)
	if researchStatus == "" {
		researchStatus = domain.ResearchNotStarted
	}
	tutorialStatus := domain.TutorialStatus(model.TutorialStatus)
	if tutorialStatus == "" {
		tutorialStatus = domain.TutorialNotStarted
	}
	lesson := &domain.Lesson{
		ID:                     model.ID,
		Status:                 domain.LessonStatus(model.Status),
		ImageName:              model.ImageName,
		IdempotencyKey:         model.IdempotencyKey,
		ImageObjectKey:         model.ImageObjectKey,
		ThumbnailObjectKey:     model.ThumbnailObjectKey,
		CurrentQuestionVersion: model.CurrentQuestionVersion,
		QuestionID:             model.QuestionID,
		QuestionVersionID:      model.QuestionVersionID,
		Error:                  valueOrEmpty(model.Error),
		ErrorCode:              valueOrEmpty(model.ErrorCode),
		FailureKind:            domain.FailureKind(valueOrEmpty(model.FailureKind)),
		FailedFrom:             domain.LessonStatus(valueOrEmpty(model.FailedFrom)),
		RetryCount:             model.RetryCount,
		LastErrorAt:            model.LastErrorAt,
		Revision:               model.Revision,
		CreatedAt:              model.CreatedAt,
		UpdatedAt:              model.UpdatedAt,
		ResearchStatus:         researchStatus,
		TutorialStatus:         tutorialStatus,
	}
	if err := json.Unmarshal(model.LearnerProfileJSON, &lesson.LearnerProfile); err != nil {
		return nil, err
	}
	if len(model.QuestionDraftJSON) > 0 {
		lesson.QuestionDraft = &domain.QuestionDraft{}
		if err := json.Unmarshal(model.QuestionDraftJSON, lesson.QuestionDraft); err != nil {
			return nil, err
		}
	}
	if len(model.QuestionAnalysisJSON) > 0 {
		lesson.QuestionAnalysis = &domain.QuestionAnalysis{}
		if err := json.Unmarshal(model.QuestionAnalysisJSON, lesson.QuestionAnalysis); err != nil {
			return nil, err
		}
	}
	if len(model.ResearchContextJSON) > 0 {
		lesson.ResearchContext = &domain.ResearchContext{}
		if err := json.Unmarshal(model.ResearchContextJSON, lesson.ResearchContext); err != nil {
			return nil, err
		}
	}
	if len(model.SolutionDraftJSON) > 0 {
		lesson.SolutionDraft = &domain.SolutionDraft{}
		if err := json.Unmarshal(model.SolutionDraftJSON, lesson.SolutionDraft); err != nil {
			return nil, err
		}
	}
	if len(model.SolutionTraceJSON) > 0 {
		lesson.SolutionTrace = &domain.ModelTrace{}
		if err := json.Unmarshal(model.SolutionTraceJSON, lesson.SolutionTrace); err != nil {
			return nil, err
		}
	}
	if len(model.QuestionJSON) > 0 {
		lesson.Question = &domain.StructuredQuestion{}
		if err := json.Unmarshal(model.QuestionJSON, lesson.Question); err != nil {
			return nil, err
		}
	}
	return lesson, nil
}

func optionalJSON(value any) (datatypes.JSON, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	return data, err
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
