package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	questiondomain "github.com/guide-flow/guide-flow/backend/domain/question"
)

type visionFixtureManifest struct {
	ResponseFile      string   `json:"responseFile"`
	Expected          string   `json:"expected"`
	ExpectedErrorPath string   `json:"expectedErrorPath"`
	ExpectedAsks      []string `json:"expectedAsks"`
}

// TestVisionTraceCorpus 复用生产解码、归一化和语义校验入口，防止测试悄悄维护另一套供应商协议。
func TestVisionTraceCorpus(t *testing.T) {
	root := filepath.Join("testdata", "vision")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("vision corpus is empty")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			dir := filepath.Join(root, entry.Name())
			manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest visionFixtureManifest
			if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, manifest.ResponseFile))
			if err != nil {
				t.Fatal(err)
			}
			// 夹具正文保存供应商返回的 structuredQuestion，测试运行时补上最小 Chat Completion envelope，
			// 让回放真正经过“供应商 envelope → 归一化 → 领域协议 → 语义校验”生产入口。
			modelPayload, err := json.Marshal(map[string]any{"text": "fixture question", "confidence": 0.95, "structuredQuestion": json.RawMessage(raw)})
			if err != nil {
				t.Fatal(err)
			}
			providerResponse, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
				"finish_reason": "stop",
				"message":       map[string]string{"content": string(modelPayload)},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			result, decodeErr := DecodeVisionProviderResponse(providerResponse)
			var question *questiondomain.StructuredQuestion
			if result.StructuredQuestion != nil {
				question = result.StructuredQuestion
			}
			if manifest.Expected == "failure" {
				if decodeErr == nil || validationErrorPath(decodeErr) != manifest.ExpectedErrorPath {
					t.Fatalf("expected failure path %q, got question=%+v err=%v", manifest.ExpectedErrorPath, question, decodeErr)
				}
				return
			}
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if err := validateVisionQuestion(question); err != nil {
				t.Fatal(err)
			}
			if len(question.Asks) != len(manifest.ExpectedAsks) {
				t.Fatalf("asks=%v, want=%v", question.Asks, manifest.ExpectedAsks)
			}
			for i := range manifest.ExpectedAsks {
				if question.Asks[i] != manifest.ExpectedAsks[i] {
					t.Fatalf("asks[%d]=%q, want=%q", i, question.Asks[i], manifest.ExpectedAsks[i])
				}
			}
		})
	}
}

func TestVisionTraceNormalizesSemicircleIntoArc(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "var", "vision-soak", "20261014T073411Z", "raw", "image-3-round-07.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, decodeErr := DecodeVisionProviderResponse(raw)
	if decodeErr == nil {
		// 该文件是完整供应商响应，若解码成功必须保留可渲染的半圆原语。
		if result.StructuredQuestion == nil || result.StructuredQuestion.Diagram == nil || len(result.StructuredQuestion.Diagram.Figures) == 0 {
			t.Fatal("expected a diagram figure")
		}
		curves := result.StructuredQuestion.Diagram.Figures[0].Curves
		if len(curves) != 1 || curves[0].Type != "arc" || curves[0].Center != "O" {
			t.Fatalf("semicircle was not normalized to an arc: %+v", curves)
		}
		return
	}
	// 真实回放文件的 envelope 可能随 soak 工具版本变化；直接回放其 content 仍必须覆盖同一归一化入口。
	t.Fatalf("decode real semicircle response: %v", decodeErr)
}
