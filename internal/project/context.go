package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// maxContextEntries caps how many top-level entries and status lines are shown
// so the injected context stays compact.
const maxContextEntries = 40

// RepoContext builds a compact, one-shot orientation block for the system
// prompt: the current git branch and short status (when in a repo) plus the
// top-level directory listing. It lets the model orient without spending tool
// calls. All git steps degrade silently outside a repo.
func RepoContext(root string) string {
	var b strings.Builder
	b.WriteString("<repo_context>\n")

	// --show-current reports the branch name even on an unborn branch (before the
	// first commit), unlike rev-parse --abbrev-ref HEAD which yields "HEAD".
	if branch := strings.TrimSpace(gitOutput(root, "branch", "--show-current")); branch != "" {
		fmt.Fprintf(&b, "branch: %s\n", branch)
	}
	if status := gitOutput(root, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		b.WriteString("uncommitted changes:\n")
		writeCapped(&b, strings.Split(strings.TrimRight(status, "\n"), "\n"))
	}

	b.WriteString("top-level entries:\n")
	writeCapped(&b, topLevelEntries(root))

	b.WriteString("</repo_context>")
	return b.String()
}

// writeCapped writes up to maxContextEntries lines, each indented, with an
// overflow note.
func writeCapped(b *strings.Builder, lines []string) {
	shown := lines
	if len(shown) > maxContextEntries {
		shown = shown[:maxContextEntries]
	}
	for _, l := range shown {
		b.WriteString("  " + strings.TrimSpace(l) + "\n")
	}
	if len(lines) > len(shown) {
		fmt.Fprintf(b, "  … %d more\n", len(lines)-len(shown))
	}
}

// topLevelEntries lists the root directory's entries (dirs suffixed with "/"),
// skipping dotfiles, sorted.
func topLevelEntries(root string) []string {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// gitOutput runs a read-only git command in root and returns its stdout, or ""
// on any error (e.g. not a repo).
func gitOutput(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
