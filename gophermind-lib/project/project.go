// Package project loads per-repository instruction files (CLAUDE.md, AGENTS.md)
// and renders them as a system-prompt suffix, so the agent respects a repo's
// conventions automatically.
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// instructionFiles are read in order; each present, non-empty one is included.
// .gophermind/MIND.md is gophermind-specific instructions (lean, focused).
// CLAUDE.md is a fallback for project-level or global conventions.
// .gophermind/prompt.md is an additional override.
var instructionFiles = []string{".gophermind/MIND.md", "CLAUDE.md", "AGENTS.md", ".gophermind/prompt.md"}

// Instructions reads the project instruction files from root and returns them
// concatenated as a system-prompt suffix, each wrapped in a tagged section, or
// "" when none exist.
func Instructions(root string) string {
	var parts []string
	var totalSize int
	for _, name := range instructionFiles {
		fullPath := filepath.Join(root, name)
		b, err := os.ReadFile(fullPath)
		if err != nil {
			// Debug: file not found or unreadable
			fmt.Fprintf(os.Stderr, "[instructions] %s: not found\n", name)
			continue
		}
		content := strings.TrimSpace(expandIncludes(root, string(b), 0))
		if content == "" {
			fmt.Fprintf(os.Stderr, "[instructions] %s: 0 bytes (empty or whitespace)\n", name)
			continue
		}
		// Debug: show filename and size
		contentSize := len(content)
		totalSize += contentSize
		fmt.Fprintf(os.Stderr, "[instructions] %s: %d bytes\n", name, contentSize)
		parts = append(parts, "<project_instructions source=\""+name+"\">\n"+content+"\n</project_instructions>")
	}
	if len(parts) > 0 {
		fmt.Fprintf(os.Stderr, "[instructions] total: %d bytes\n", totalSize)
	}
	return strings.Join(parts, "\n\n")
}
