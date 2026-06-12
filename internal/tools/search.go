package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Search returns the search tool, which greps the repository for a pattern.
// It prefers ripgrep and falls back to grep.
func Search(root string) Tool {
	return Tool{
		Name:        "search",
		Description: "Search the repository for a regular-expression pattern and return matching lines with file:line prefixes. Uses ripgrep when available.",
		Schema:      object(map[string]any{"pattern": str("Regular expression to search for.")}, "pattern"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Pattern string `json:"pattern"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(a.Pattern) == "" {
				return "", fmt.Errorf("empty pattern")
			}

			// The "--" terminator forces the user-supplied pattern to be parsed
			// as a positional argument, never as an option. Without it a pattern
			// like "--pre=…" or "-f…" would be interpreted as a flag (rg's
			// --pre can run an external preprocessor) — argument injection.
			var cmd *exec.Cmd
			if _, err := exec.LookPath("rg"); err == nil {
				cmd = exec.CommandContext(ctx, "rg", "-n", "--hidden", "--glob", "!.git", "--", a.Pattern)
			} else {
				cmd = exec.CommandContext(ctx, "grep", "-rn", "--exclude-dir=.git", "-e", a.Pattern, "--", ".")
			}
			cmd.Dir = root

			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()
			text := strings.TrimSpace(out.String())
			if err != nil {
				// Exit code 1 means "no matches" for both rg and grep — not an error.
				if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
					return "(no matches)", nil
				}
				return "", fmt.Errorf("search failed: %w; %s", err, text)
			}
			if text == "" {
				return "(no matches)", nil
			}
			return truncate(text), nil
		},
	}
}

func truncate(s string) string {
	const max = 12000
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... [truncated]"
}
