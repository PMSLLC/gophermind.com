// Package safety provides the three independent guards that bound what the
// agent can do: path containment, a shell deny-list, and an approval gate.
package safety

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafeJoin resolves rel against root and guarantees the result stays inside
// root, rejecting absolute paths and "../" escapes.
func SafeJoin(root, rel string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("abs root: %w", err)
	}
	abs, err := filepath.Abs(filepath.Join(cleanRoot, rel))
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}
	if abs != cleanRoot && !strings.HasPrefix(abs, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes repo root: %s", rel)
	}
	return abs, nil
}

// blockedPatterns are substrings that are never allowed in a shell command,
// even in auto-approval mode. Deny-list (not allow-list) so the agent can run
// arbitrary legitimate build and test commands.
var blockedPatterns = []string{
	"rm -rf",
	"sudo ",
	"git reset --hard",
	"git clean -fd",
	"> /",
	">~",
	":(){",
}

// CheckCommand rejects empty or denied commands.
func CheckCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return fmt.Errorf("empty command")
	}
	for _, blocked := range blockedPatterns {
		if strings.Contains(trimmed, blocked) {
			return fmt.Errorf("blocked command pattern: %s", blocked)
		}
	}
	return nil
}

// ApprovalFunc decides whether a gated, mutating tool call may proceed. It
// receives the tool name and its raw JSON arguments and returns true to allow.
type ApprovalFunc func(tool, argsJSON string) bool

// Auto always approves.
func Auto(tool, argsJSON string) bool { return true }

// Gated reports whether a tool requires approval before running.
func Gated(tool string) bool {
	switch tool {
	case "write_file", "edit_file", "run_shell":
		return true
	default:
		return false
	}
}
