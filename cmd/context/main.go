package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/guide-flow/guide-flow/backend/agentcontext"
)

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) != "" {
		*values = append(*values, value)
	}
	return nil
}

func main() {
	var knowledge stringList
	var (
		task        = flag.String("task", "", "task objective")
		taskID      = flag.String("task-id", "", "change ID, for example CHANGE-001")
		phase       = flag.String("phase", "propose", "phase: propose, apply, review")
		repoRoot    = flag.String("repo-root", ".", "repository root")
		contextRoot = flag.String("context-root", "", "context root; defaults to <repo-root>/.agent-context")
		output      = flag.String("output", "", "markdown output path; stdout when empty")
		manifest    = flag.String("manifest", "", "JSON manifest output path; disabled when empty")
		maxBytes    = flag.Int("max-bytes", 32*1024, "maximum markdown size")
	)
	flag.Var(&knowledge, "knowledge", "explicit knowledge file; repeatable")
	flag.Parse()

	absoluteRepo, err := filepath.Abs(*repoRoot)
	if err != nil {
		fail(err)
	}
	if *contextRoot == "" {
		*contextRoot = filepath.Join(absoluteRepo, ".agent-context")
	} else if !filepath.IsAbs(*contextRoot) {
		*contextRoot, err = filepath.Abs(*contextRoot)
		if err != nil {
			fail(err)
		}
	}

	pack, err := agentcontext.NewCompiler(nil, nil).Compile(context.Background(), agentcontext.BuildRequest{
		TaskID: *taskID, Task: *task, Phase: agentcontext.Phase(*phase), RepoRoot: absoluteRepo,
		ContextRoot: *contextRoot, Knowledge: knowledge, MaxBytes: *maxBytes,
	})
	if err != nil {
		fail(err)
	}
	if *output == "" {
		fmt.Print(pack.Markdown)
	} else if err := write(*output, []byte(pack.Markdown)); err != nil {
		fail(err)
	}
	if *manifest != "" {
		content, err := json.MarshalIndent(pack, "", "  ")
		if err != nil {
			fail(err)
		}
		if err := write(*manifest, append(content, '\n')); err != nil {
			fail(err)
		}
	}
}

func write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
