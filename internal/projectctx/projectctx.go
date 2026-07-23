// Package projectctx gathers a bounded digest of what a repository already
// knows about itself — a prior plan under .planning/, session memory under
// .remember/, and the task ledger under .superpowers/ — so the /project
// interview can prepopulate answers instead of asking about work already done.
//
// Everything here is size-capped on purpose. These trees run to megabytes, and
// a local model may have only a few thousand tokens of context: an unbounded
// digest would crowd out the interview it is meant to inform.
package projectctx

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// perSourceMax caps one source's contribution, so a single long file
	// cannot consume the whole budget.
	perSourceMax = 1200
	// totalMax caps the digest overall. Roughly 1k tokens, which leaves room
	// for the interview prompt and the answers within an 8k window.
	totalMax = 4000
)

// source is one file to look for, and how to trim it when it is too long.
type source struct {
	path  string
	label string
	tail  bool // keep the end (append-only logs) rather than the beginning
}

// sources are consulted in priority order: an existing plan is the most
// directly reusable, then current session state, then the task ledger.
var sources = []source{
	{path: filepath.Join(".planning", "SPEC.md"), label: "Existing spec (.planning/SPEC.md)"},
	{path: filepath.Join(".planning", "ROADMAP.md"), label: "Existing roadmap (.planning/ROADMAP.md)"},
	{path: filepath.Join(".remember", "now.md"), label: "Current session state (.remember/now.md)"},
	{path: filepath.Join(".remember", "recent.md"), label: "Recent work (.remember/recent.md)"},
	{path: filepath.Join(".superpowers", "sdd", "progress.md"), label: "Task ledger (.superpowers/sdd/progress.md)", tail: true},
	{path: "PROJECT.md", label: "Project conventions (PROJECT.md)"},
}

// Gather returns a digest of the context found under root, or "" when there is
// nothing to report. The result never exceeds totalMax runes.
func Gather(root string) string {
	var b strings.Builder
	used := 0

	for _, s := range sources {
		if used >= totalMax {
			break
		}
		data, err := os.ReadFile(filepath.Join(root, s.path))
		if err != nil {
			continue // absent sources are the normal case, not an error
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}

		budget := perSourceMax
		if remaining := totalMax - used; remaining < budget {
			budget = remaining
		}
		text = trim(text, budget, s.tail)

		b.WriteString("### " + s.label + "\n")
		b.WriteString(text)
		b.WriteString("\n\n")
		used += len(text)
	}

	return strings.TrimSpace(b.String())
}

// trim shortens text to at most max runes, marking where content was dropped so
// the model is not misled into thinking it saw the whole file.
func trim(text string, max int, tail bool) string {
	r := []rune(text)
	if len(r) <= max {
		return text
	}
	if tail {
		return "…(earlier entries omitted)…\n" + string(r[len(r)-max:])
	}
	return string(r[:max]) + "\n…(truncated)…"
}
