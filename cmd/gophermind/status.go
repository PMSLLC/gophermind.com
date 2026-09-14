package main

import (
	"context"
	"strings"
	"time"

	"gophermind/gophermind-lib/gitenv"
)

// promptLine renders a compact one-line status for embedding in a shell prompt
// (PS1/starship): the gopher glyph, the active model (or "auto"), and the
// current git branch when in a repo.
func promptLine(model, branch string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		m = "auto"
	}
	s := "🐹 " + m
	if strings.TrimSpace(branch) != "" {
		s += " ⎇ " + branch
	}
	return s
}

// gitBranchOf returns the current branch of root, or "" outside a repo. Uses
// gitenv so an inherited GIT_DIR (which every git hook exports) cannot
// redirect it at some other repository; see internal/gitenv for the incident
// that made this necessary.
func gitBranchOf(root string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := gitenv.CommandContext(ctx, root, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
