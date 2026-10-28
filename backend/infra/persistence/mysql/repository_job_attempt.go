package mysql

import (
	"context"
	"errors"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	"gorm.io/gorm"
)

func (r *LessonRepository) StartJobAttempt(ctx context.Context, lessonID, jobID, stage string, attempt int) error {
	now := domain.Now()
	var record JobAttemptModel
	err := r.db.WithContext(ctx).Where("job_id = ? AND attempt = ?", jobID, attempt).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(&JobAttemptModel{
			LessonID: lessonID, JobID: jobID, Stage: stage, Attempt: attempt,
			Status: "running", StartedAt: &now, CreatedAt: now,
		}).Error
	}
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&record).Updates(map[string]any{
		"status": "running", "stage": stage, "started_at": now, "finished_at": nil, "error_code": nil,
	}).Error
}

func (r *LessonRepository) FinishJobAttempt(ctx context.Context, lessonID, jobID, status, errorCode string) error {
	now := domain.Now()
	return r.db.WithContext(ctx).Model(&JobAttemptModel{}).
		Where("lesson_id = ? AND job_id = ? AND status = ?", lessonID, jobID, "running").
		Updates(map[string]any{"status": status, "error_code": nullableString(errorCode), "finished_at": now}).Error
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
