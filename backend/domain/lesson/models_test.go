package domain

import "testing"

func TestQuestionDraftNeedsConfirmationChecksCriticalFields(t *testing.T) {
	base := QuestionDraft{Text: "一道题", Confidence: .99, FieldConfidence: map[string]float64{"numbers": .99, "units": .99}}
	if base.NeedsConfirmation() {
		t.Fatal("high-confidence draft should not need confirmation")
	}
	base.FieldConfidence["numbers"] = .79
	if !base.NeedsConfirmation() {
		t.Fatal("low-confidence number must require confirmation")
	}
	base.Confidence = .8
	base.FieldConfidence["numbers"] = .8
	if !base.NeedsConfirmation() {
		t.Fatal("confidence at the confirmation threshold must require confirmation")
	}
}
