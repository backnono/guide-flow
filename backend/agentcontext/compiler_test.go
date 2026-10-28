package agentcontext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

type fakeFiles struct{ files map[string][]byte }

func (f fakeFiles) ReadFile(path string) ([]byte, error) {
	content, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return content, nil
}

func (f fakeFiles) Glob(pattern string) ([]string, error) {
	var paths []string
	for path := range f.files {
		matched, err := filepath.Match(pattern, path)
		if err != nil {
			return nil, err
		}
		if matched {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

type fakeGit struct{}

func (fakeGit) CurrentBranch(context.Context, string) (string, error) { return "feature/test", nil }
func (fakeGit) CurrentCommit(context.Context, string) (string, error) { return "abc123", nil }
func (fakeGit) Status(context.Context, string) (string, error) {
	return " M backend/application/lesson/service.go", nil
}
func (fakeGit) Diff(context.Context, string) (string, error) { return "+ changed", nil }

func TestCompileSelectsSourcesByPhase(t *testing.T) {
	repo, root := "/repo", "/context"
	files := fakeFiles{files: map[string][]byte{
		filepath.Join(repo, "README.md"):                             []byte("project"),
		filepath.Join(root, "rules", "security.md"):                  []byte("security rule"),
		filepath.Join(root, "rules", "project.md"):                   []byte("project rule"),
		filepath.Join(root, "changes", "C-1", "spec.md"):             []byte("spec"),
		filepath.Join(root, "changes", "C-1", "tasks.md"):            []byte("tasks"),
		filepath.Join(root, "changes", "C-1", "log.md"):              []byte("log"),
		filepath.Join(root, "changes", "C-1", "review-checklist.md"): []byte("review"),
	}}
	pack, err := NewCompiler(files, fakeGit{}).Compile(context.Background(), BuildRequest{
		TaskID: "C-1", Task: "review change", Phase: PhaseReview, RepoRoot: repo, ContextRoot: root,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, expected := range []string{"project", "security rule", "project rule", "spec", "tasks", "log", "review", "feature/test", "+ changed"} {
		if !strings.Contains(pack.Markdown, expected) {
			t.Errorf("Markdown does not contain %q", expected)
		}
	}
}

func TestCompileSkipsGitForPropose(t *testing.T) {
	pack, err := NewCompiler(fakeFiles{files: map[string][]byte{}}, fakeGit{}).Compile(context.Background(), BuildRequest{
		Task: "propose change", Phase: PhasePropose, RepoRoot: "/repo",
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, item := range pack.Items {
		if item.Kind == "workspace" || item.Kind == "diff" {
			t.Fatalf("propose should not include git item: %+v", item)
		}
	}
}

func TestCompileRejectsInvalidRequest(t *testing.T) {
	_, err := NewCompiler(nil, nil).Compile(context.Background(), BuildRequest{Phase: PhaseApply, RepoRoot: "/repo"})
	if err == nil {
		t.Fatal("Compile() should reject an empty task")
	}
}

func TestRenderMarkdownHonorsByteBudget(t *testing.T) {
	pack := ContextPack{Task: BuildRequest{Task: "任务", Phase: PhaseApply}, Items: []ContextItem{{Kind: "rule", Source: "rule.md", Content: strings.Repeat("内容", 100)}}}
	markdown := RenderMarkdown(pack, 128)
	if len(markdown) > 128 {
		t.Fatalf("RenderMarkdown() length = %d, want <= 128", len(markdown))
	}
	if !utf8.ValidString(markdown) {
		t.Fatal("RenderMarkdown() returned invalid UTF-8")
	}
}
