package domain_test

import (
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestLessonStateTransitions(t *testing.T) {
	valid := [][2]domain.LessonStatus{
		{domain.StatusCreated, domain.StatusRecognizing},
		{domain.StatusRecognizing, domain.StatusAwaitingConfirmation},
		{domain.StatusRecognizing, domain.StatusStructuring},
		{domain.StatusStructuring, domain.StatusSolving},
		{domain.StatusSolving, domain.StatusPlanning},
		{domain.StatusPlanning, domain.StatusRendering},
		{domain.StatusRendering, domain.StatusReady},
		{domain.StatusAwaitingConfirmation, domain.StatusStructuring},
		{domain.StatusFailed, domain.StatusStructuring},
	}
	for _, pair := range valid {
		if err := domain.ValidateTransition(pair[0], pair[1]); err != nil {
			t.Errorf("expected %s -> %s to be valid: %v", pair[0], pair[1], err)
		}
	}
	for _, pair := range [][2]domain.LessonStatus{{domain.StatusCreated, domain.StatusReady}, {domain.StatusReady, domain.StatusPlanning}, {domain.StatusAwaitingConfirmation, domain.StatusReady}} {
		if err := domain.ValidateTransition(pair[0], pair[1]); err == nil {
			t.Errorf("expected %s -> %s to be invalid", pair[0], pair[1])
		}
	}
}
