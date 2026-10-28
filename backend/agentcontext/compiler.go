package agentcontext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultMaxBytes = 32 * 1024

type ContextCompiler struct {
	files FileReader
	git   GitReader
}

func NewCompiler(files FileReader, git GitReader) *ContextCompiler {
	if files == nil {
		files = OSFileReader{}
	}
	if git == nil {
		git = CommandGitReader{}
	}
	return &ContextCompiler{files: files, git: git}
}

func (c *ContextCompiler) Compile(ctx context.Context, request BuildRequest) (ContextPack, error) {
	if err := validateRequest(&request); err != nil {
		return ContextPack{}, err
	}
	if request.MaxBytes == 0 {
		request.MaxBytes = defaultMaxBytes
	}

	pack := ContextPack{Task: request}
	pack.Items = append(pack.Items, ContextItem{
		Kind: "task", Source: "user.task", Content: request.Task,
		Authority: "required", Reason: "当前任务目标",
	})

	// Required material is selected before the README reference so a small
	// budget never silently removes project rules or change requirements.
	c.addRules(&pack, request.ContextRoot)
	c.addChangeFiles(&pack, request)
	c.addKnowledge(&pack, request)
	c.addGitState(ctx, &pack, request)
	c.addREADME(&pack, request.RepoRoot)

	pack.Markdown = RenderMarkdown(pack, request.MaxBytes)
	return pack, nil
}

func validateRequest(request *BuildRequest) error {
	if strings.TrimSpace(request.Task) == "" {
		return errors.New("task must not be empty")
	}
	if request.Phase != PhasePropose && request.Phase != PhaseApply && request.Phase != PhaseReview {
		return fmt.Errorf("invalid phase %q: want propose, apply, or review", request.Phase)
	}
	if strings.TrimSpace(request.RepoRoot) == "" {
		return errors.New("repo root must not be empty")
	}
	if request.ContextRoot == "" {
		request.ContextRoot = filepath.Join(request.RepoRoot, ".agent-context")
	}
	if request.MaxBytes < 0 {
		return errors.New("max bytes must not be negative")
	}
	return nil
}

func (c *ContextCompiler) addREADME(pack *ContextPack, repoRoot string) {
	c.addFile(pack, filepath.Join(repoRoot, "README.md"), "project", "reference", "项目入口说明")
}

func (c *ContextCompiler) addRules(pack *ContextPack, contextRoot string) {
	paths, err := c.files.Glob(filepath.Join(contextRoot, "rules", "*.md"))
	if err != nil {
		pack.Warnings = append(pack.Warnings, fmt.Sprintf("读取规则目录失败: %v", err))
		return
	}
	if len(paths) == 0 {
		pack.Warnings = append(pack.Warnings, fmt.Sprintf("未找到项目规则: %s", filepath.Join(contextRoot, "rules")))
		return
	}
	sort.Strings(paths)
	for _, path := range paths {
		c.addFile(pack, path, "rule", "required", "当前项目规则")
	}
}

func (c *ContextCompiler) addChangeFiles(pack *ContextPack, request BuildRequest) {
	if request.TaskID == "" {
		return
	}
	changeRoot := filepath.Join(request.ContextRoot, "changes", request.TaskID)
	for _, name := range changeFileNames(request.Phase) {
		c.addFile(pack, filepath.Join(changeRoot, name), "change", "required", "当前变更的阶段材料")
	}
}

func changeFileNames(phase Phase) []string {
	switch phase {
	case PhasePropose:
		return []string{"spec.md"}
	case PhaseApply:
		return []string{"spec.md", "tasks.md", "log.md"}
	case PhaseReview:
		return []string{"spec.md", "tasks.md", "log.md", "review-checklist.md"}
	default:
		return nil
	}
}

func (c *ContextCompiler) addKnowledge(pack *ContextPack, request BuildRequest) {
	for _, path := range request.Knowledge {
		if strings.TrimSpace(path) != "" {
			c.addFile(pack, path, "knowledge", "reference", "任务显式关联的知识")
		}
	}
}

func (c *ContextCompiler) addGitState(ctx context.Context, pack *ContextPack, request BuildRequest) {
	if request.Phase == PhasePropose {
		return
	}
	branch, err := c.git.CurrentBranch(ctx, request.RepoRoot)
	if err != nil {
		pack.Warnings = append(pack.Warnings, fmt.Sprintf("读取 Git 分支失败: %v", err))
		return
	}
	commit, _ := c.git.CurrentCommit(ctx, request.RepoRoot)
	status, statusErr := c.git.Status(ctx, request.RepoRoot)
	if statusErr != nil {
		pack.Warnings = append(pack.Warnings, fmt.Sprintf("读取 Git 状态失败: %v", statusErr))
	}
	pack.Items = append(pack.Items, ContextItem{
		Kind: "workspace", Source: "git.status",
		Content:   strings.TrimSpace(fmt.Sprintf("Branch: %s\nCommit: %s\n\n%s", branch, commit, status)),
		Authority: "required", Reason: "确认 Agent 的工作现场", Version: commit,
	})

	if request.Phase != PhaseReview {
		return
	}
	diff, diffErr := c.git.Diff(ctx, request.RepoRoot)
	if diffErr != nil {
		pack.Warnings = append(pack.Warnings, fmt.Sprintf("读取 Git diff 失败: %v", diffErr))
		return
	}
	if strings.TrimSpace(diff) != "" {
		pack.Items = append(pack.Items, ContextItem{
			Kind: "diff", Source: "git.diff", Content: strings.TrimSpace(diff),
			Authority: "required", Reason: "Code Review 的变更证据", Version: commit,
		})
	}
}

func (c *ContextCompiler) addFile(pack *ContextPack, path, kind, authority, reason string) {
	content, err := c.files.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if authority == "required" {
				pack.Warnings = append(pack.Warnings, fmt.Sprintf("未找到%s: %s", kind, path))
			}
			return
		}
		pack.Warnings = append(pack.Warnings, fmt.Sprintf("读取%s失败 %s: %v", kind, path, err))
		return
	}
	pack.Items = append(pack.Items, ContextItem{
		Kind: kind, Source: path, Content: strings.TrimSpace(string(content)),
		Authority: authority, Reason: reason, SHA256: sha256Hex(content),
	})
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
