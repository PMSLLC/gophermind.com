// Package persona provides task-tuned system-prompt presets selectable with
// --persona, so a run can be biased toward reviewing, designing, or testing
// without hand-writing --append-system-prompt each time.
package persona

import (
	"sort"
	"strings"
)

// presets maps a persona name to the instruction block appended to the system
// prompt. Kept intentionally short and behavior-focused.
var presets = map[string]string{
	"reviewer": `You are acting as a code reviewer. Prioritize correctness, edge cases,
security, and clarity. Point out bugs, risky assumptions, and missing tests
before praising. Prefer concrete, actionable feedback with file:line references.
Do not rewrite large sections unless asked; suggest the smallest correct change.`,

	"architect": `You are acting as a software architect. Think about structure, boundaries,
and trade-offs before code. Surface assumptions and alternatives, call out
coupling and scalability concerns, and prefer the simplest design that meets the
requirement. Explain the "why" of a design decision, not just the "what".`,

	"tester": `You are acting as a test engineer. Favor test-driven development: write a
failing test that captures the requirement or reproduces the bug before changing
implementation. Cover edge cases and error paths, keep tests deterministic and
isolated, and verify behavior end-to-end rather than asserting on internals.`,
}

// Preset returns the system-prompt fragment for a persona name (case-insensitive)
// and whether it is known.
func Preset(name string) (string, bool) {
	text, ok := presets[strings.ToLower(strings.TrimSpace(name))]
	return text, ok
}

// Names returns the known persona names, sorted.
func Names() []string {
	out := make([]string, 0, len(presets))
	for n := range presets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
