package repo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"forge/internal/llm"
	"forge/internal/tools"
)

func BuildRepoSummary(ctx context.Context, root string, client *llm.Client) (string, error) {
	tree, err := Tree(root, 500)
	if err != nil {
		return "", err
	}
	files := LikelyImportantFiles(tree)

	var snippets []string
	for _, rel := range files {
		content, err := tools.ReadFile(root, rel)
		if err != nil {
			continue
		}
		snippets = append(snippets, fmt.Sprintf("FILE: %s\n%s", rel, truncate(content, 2500)))
	}

	messages := []llm.Message{
		{
			Role: "system",
			Content: "You summarize Go repositories for an engineering agent. Keep it concise and structured. " +
				"Describe the purpose, entrypoints, major packages, likely workflows, and key commands.",
		},
		{
			Role:    "user",
			Content: "Repository tree:\n" + tree + "\n\nImportant file excerpts:\n" + strings.Join(snippets, "\n\n"),
		},
	}
	return client.Chat(ctx, messages, nil)
}

func BuildTaskContext(root, summary, tree string, files map[string]string, prompt string) string {
	var b strings.Builder
	b.WriteString("USER GOAL:\n")
	b.WriteString(prompt)
	b.WriteString("\n\nREPOSITORY SUMMARY:\n")
	b.WriteString(summary)
	b.WriteString("\n\nREPOSITORY TREE:\n")
	b.WriteString(tree)
	b.WriteString("\n\nSELECTED FILES:\n")
	for path, content := range files {
		b.WriteString("FILE: ")
		b.WriteString(filepath.ToSlash(path))
		b.WriteString("\n")
		b.WriteString(truncate(content, 12000))
		b.WriteString("\n\n")
	}
	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... [truncated]"
}
