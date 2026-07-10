package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
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

// gitBranchOf returns the current branch of root, or "" outside a repo.
func gitBranchOf(root string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
