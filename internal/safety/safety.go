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
// root, rejecting absolute paths and "../" escapes. It is also symlink-aware:
// a lexically-contained path that escapes the root through a symlink component
// (e.g. repo/evil -> /etc, then "evil/passwd") is rejected.
func SafeJoin(root, rel string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("abs root: %w", err)
	}
	abs, err := filepath.Abs(filepath.Join(cleanRoot, rel))
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}
	// Lexical containment check rejects "../" escapes up front, including for
	// paths that do not exist yet (write_file creating new files).
	if abs != cleanRoot && !strings.HasPrefix(abs, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes repo root: %s", rel)
	}
	// Symlink-aware containment: resolve the real root and the deepest existing
	// ancestor of abs, then re-check. This defeats a symlink *inside* the repo
	// that points outside it — a lexical prefix check alone cannot catch this.
	realRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	resolved, err := resolveExisting(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if resolved != realRoot && !strings.HasPrefix(resolved, realRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes repo root via symlink: %s", rel)
	}
	return abs, nil
}

// resolveExisting resolves symlinks on the deepest existing ancestor of p,
// re-appending the trailing components that do not exist yet. This lets the
// containment check work for not-yet-created files (e.g. write_file targets)
// while still resolving any symlinked directory in the existing prefix.
func resolveExisting(p string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(p)
	if parent == p { // reached the filesystem root without resolving
		return p, nil
	}
	resolvedParent, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(p)), nil
}

// blockedPatterns are substrings that are never allowed in a shell command,
// even in auto-approval mode. Deny-list (not allow-list) so the agent can run
// arbitrary legitimate build and test commands. Patterns are matched against a
// whitespace-normalized form of the command (see CheckCommand), so they are
// written single-spaced. A deny-list is best-effort defense-in-depth — the
// approval gate (ApprovalFunc) is the primary control for shell execution.
var blockedPatterns = []string{
	"rm -rf",
	"rm -fr",
	"rm -r -f",
	"rm -f -r",
	"sudo ",
	"git reset --hard",
	"git clean -fd",
	"mkfs",
	"dd if=",
	"> /",
	">/",
	"> ~",
	">~",
	":(){",
}

// CheckCommand rejects empty or denied commands. The command is whitespace-
// normalized before matching so trivial spacing tricks ("rm  -rf", a tab after
// "sudo") cannot slip a blocked pattern past the substring check.
func CheckCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return fmt.Errorf("empty command")
	}
	normalized := strings.Join(strings.Fields(trimmed), " ")
	for _, blocked := range blockedPatterns {
		if strings.Contains(normalized, blocked) {
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
