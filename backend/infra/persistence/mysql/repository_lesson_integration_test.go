package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

// 配置 MYSQL_GORM_TEST_DSN 后执行，普通测试不依赖外部 MySQL。
func TestLessonRepositoryRoundTrip(t *testing.T) {
	dsn := os.Getenv("MYSQL_GORM_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_GORM_TEST_DSN is not configured")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err = AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	lesson := &domain.Lesson{ID: "gorm-round-trip-test", Status: domain.StatusCreated, LearnerProfile: domain.LearnerProfile{Grade: 5}, CreatedAt: now, UpdatedAt: now}
	repo := NewLessonRepository(db)
	if err = db.Exec("DELETE FROM lessons WHERE id = ?", lesson.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = repo.Create(ctx, lesson); err != nil {
		t.Fatal(err)
	}
	lesson.Status = domain.StatusRecognizing
	lesson.StatusEvents = append(lesson.StatusEvents, domain.StatusEvent{From: domain.StatusCreated, To: domain.StatusRecognizing, Stage: domain.Stage(domain.StatusRecognizing), CreatedAt: now})
	if err = repo.Update(ctx, lesson); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, lesson.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusRecognizing || len(got.StatusEvents) != 1 || got.Revision != lesson.Revision {
		t.Fatalf("unexpected GORM repository result: %+v", got)
	}
	stale := *got
	got.Status = domain.StatusStructuring
	if err = repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err = repo.Update(ctx, &stale); err != domain.ErrConcurrentUpdate {
		t.Fatalf("expected optimistic concurrency error, got %v", err)
	}
	_ = db.Exec("DELETE FROM lessons WHERE id = ?", lesson.ID).Error
}
