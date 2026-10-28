package memory

import (
	"context"
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestLessonRepositoryRejectsStaleRevision(t *testing.T) {
	repo := NewLessonRepository()
	original := &domain.Lesson{ID: "lesson-1", Status: domain.StatusCreated}
	if err := repo.Create(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	first, err := repo.Get(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Get(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	first.Status = domain.StatusRecognizing
	if err := repo.Update(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second.Status = domain.StatusRecognizing
	if err := repo.Update(context.Background(), second); err != domain.ErrConcurrentUpdate {
		t.Fatalf("expected concurrent update error, got %v", err)
	}
}
