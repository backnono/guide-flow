package lesson

import (
	"context"
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestClassifyFailureMarksSolvingTimeout(t *testing.T) {
	code, kind := classifyFailure(domain.StatusSolving, context.DeadlineExceeded, "")
	if code != "llm_timeout" {
		t.Fatalf("unexpected error code: %q", code)
	}
	if kind != domain.FailureExternal {
		t.Fatalf("unexpected failure kind: %q", kind)
	}
}

func TestClassifyFailureMarksTeachingTimeout(t *testing.T) {
	code, kind := classifyFailure(domain.StatusTeaching, context.DeadlineExceeded, "")
	if code != "tutorial_timeout" {
		t.Fatalf("unexpected tutorial timeout error code: %q", code)
	}
	if kind != domain.FailureExternal {
		t.Fatalf("unexpected failure kind: %q", kind)
	}
}

func TestClassifyFailurePreservesExistingBusinessCode(t *testing.T) {
	code, kind := classifyFailure(domain.StatusSolving, context.DeadlineExceeded, "solution_unsupported")
	if code != "solution_unsupported" || kind != domain.FailureExternal {
		t.Fatalf("existing error code was not preserved: code=%q kind=%q", code, kind)
	}
}
