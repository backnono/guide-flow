package main

import "testing"

func TestFailureFingerprintKeepsProviderFailuresObservable(t *testing.T) {
	record := runRecord{PromptVersion: "question-vision-v4", Error: "deepseek vision status 402 Payment Required: {\"error\":{\"message\":\"Insufficient Balance (request_id: abc)\"}}"}
	got := failureFingerprint(record)
	if got != "question-vision-v4|request|provider status 402 Payment Required" {
		t.Fatalf("unexpected provider failure fingerprint: %q", got)
	}
}
