package response

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestNewLessonResponseFormatsTimesForAPI(t *testing.T) {
	created := time.Date(2026, 9, 23, 7, 40, 55, 0, time.UTC)
	lesson := &domain.Lesson{
		ID: "lesson-1", Status: domain.StatusCreated, CreatedAt: created, UpdatedAt: created,
		QuestionVersions: []domain.QuestionVersion{{ID: 1, Text: "题目", CreatedAt: created}},
		StatusEvents:     []domain.StatusEvent{{Revision: 1, To: domain.StatusCreated, CreatedAt: created}},
	}

	body, err := json.Marshal(NewLessonResponse(lesson))
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(body)
	for _, expected := range []string{
		`"createdAt":"2026-09-23 15:40:55"`,
		`"updatedAt":"2026-09-23 15:40:55"`,
		`"createdAt":"2026-09-23 15:40:55"`,
	} {
		if !strings.Contains(jsonText, expected) {
			t.Fatalf("response does not contain %q: %s", expected, jsonText)
		}
	}
	if strings.Contains(jsonText, "+08:00") || strings.Contains(jsonText, "T07:40:55") {
		t.Fatalf("response still exposes RFC3339 timestamp: %s", jsonText)
	}
}

func TestNewLessonResponseExplainsLLMTimeout(t *testing.T) {
	response := NewLessonResponse(&domain.Lesson{ID: "lesson-timeout", ErrorCode: "llm_timeout"})
	if response.Error != "解题服务响应超时，请稍后重试" {
		t.Fatalf("unexpected timeout error: %q", response.Error)
	}
	if response.ErrorCode != "llm_timeout" {
		t.Fatalf("unexpected timeout error code: %q", response.ErrorCode)
	}
}
