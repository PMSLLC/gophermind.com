// Package project loads per-repository instruction files (CLAUDE.md, AGENTS.md)
// and renders them as a system-prompt suffix, so the agent respects a repo's
// conventions automatically.
package project

import (
	"os"
	"path/filepath"
	"strings"
)

// instructionFiles are read in order; each present, non-empty one is included.
var instructionFiles = []string{"CLAUDE.md", "AGENTS.md"}

// Instructions reads the project instruction files from root and returns them
// concatenated as a system-prompt suffix, each wrapped in a tagged section, or
// "" when none exist.
func Instructions(root string) string {
	var parts []string
	for _, name := range instructionFiles {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(b))
		if content == "" {
			continue
		}
		parts = append(parts, "<project_instructions source=\""+name+"\">\n"+content+"\n</project_instructions>")
	}
	return strings.Join(parts, "\n\n")
}
