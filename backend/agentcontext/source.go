package agentcontext

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type FileReader interface {
	ReadFile(path string) ([]byte, error)
	Glob(pattern string) ([]string, error)
}

type OSFileReader struct{}

func (OSFileReader) ReadFile(path string) ([]byte, error)  { return os.ReadFile(path) }
func (OSFileReader) Glob(pattern string) ([]string, error) { return filepath.Glob(pattern) }

type GitReader interface {
	CurrentBranch(ctx context.Context, repoRoot string) (string, error)
	CurrentCommit(ctx context.Context, repoRoot string) (string, error)
	Status(ctx context.Context, repoRoot string) (string, error)
	Diff(ctx context.Context, repoRoot string) (string, error)
}

type CommandGitReader struct{}

func (CommandGitReader) CurrentBranch(ctx context.Context, repoRoot string) (string, error) {
	return runGit(ctx, repoRoot, "branch", "--show-current")
}

func (CommandGitReader) CurrentCommit(ctx context.Context, repoRoot string) (string, error) {
	return runGit(ctx, repoRoot, "rev-parse", "HEAD")
}

func (CommandGitReader) Status(ctx context.Context, repoRoot string) (string, error) {
	return runGit(ctx, repoRoot, "status", "--short")
}

func (CommandGitReader) Diff(ctx context.Context, repoRoot string) (string, error) {
	return runGit(ctx, repoRoot, "diff", "HEAD", "--no-ext-diff", "--unified=3")
}

func runGit(ctx context.Context, repoRoot string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = repoRoot
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
