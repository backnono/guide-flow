package agentcontext

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func RenderMarkdown(pack ContextPack, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	var builder strings.Builder
	appendText(&builder, maxBytes, "# Agent Context\n\n## Task\n\n")
	appendText(&builder, maxBytes, fmt.Sprintf("- Phase: `%s`\n", pack.Task.Phase))
	if pack.Task.TaskID != "" {
		appendText(&builder, maxBytes, fmt.Sprintf("- Task ID: `%s`\n", pack.Task.TaskID))
	}
	appendText(&builder, maxBytes, fmt.Sprintf("\n%s\n\n", pack.Task.Task))

	for _, item := range pack.Items {
		if item.Kind == "task" {
			continue
		}
		appendText(&builder, maxBytes, fmt.Sprintf("## %s: %s\n\n", title(item.Kind), item.Source))
		appendText(&builder, maxBytes, fmt.Sprintf("Authority: `%s`\nReason: %s\n\n", item.Authority, item.Reason))
		if item.Version != "" {
			appendText(&builder, maxBytes, fmt.Sprintf("Version: `%s`\n\n", item.Version))
		}
		appendText(&builder, maxBytes, "```text\n")
		appendText(&builder, maxBytes, item.Content+"\n")
		appendText(&builder, maxBytes, "```\n\n")
	}
	if len(pack.Warnings) > 0 {
		appendText(&builder, maxBytes, "## Warnings\n\n")
		for _, warning := range pack.Warnings {
			appendText(&builder, maxBytes, "- "+warning+"\n")
		}
	}
	return builder.String()
}

func appendText(builder *strings.Builder, maxBytes int, text string) {
	remaining := maxBytes - builder.Len()
	if remaining <= 0 {
		return
	}
	if len(text) <= remaining {
		builder.WriteString(text)
		return
	}
	truncated := text[:remaining]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	builder.WriteString(truncated)
}

func title(kind string) string {
	switch kind {
	case "project":
		return "Project"
	case "rule":
		return "Rule"
	case "change":
		return "Change"
	case "knowledge":
		return "Knowledge"
	case "workspace":
		return "Workspace"
	case "diff":
		return "Diff"
	default:
		return "Context"
	}
}
