package report

import (
	"fmt"
	"strings"
	"unicode"
)

// esc makes text that derives from an id or a caller string safe to print:
// control and other non-printing runes are shown as \xNN or \uNNNN.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || (unicode.IsPrint(r) && !unicode.IsControl(r)):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

// Summary is the printed summary. The last two lines are the proof lines and
// nothing follows them.
func (r Report) Summary() string {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	title := fmt.Sprintf("Run %s: %s", esc(r.RunID), esc(r.Status))
	if r.StopReason != "" {
		title += " (" + esc(r.StopReason) + ")"
	}
	line("%s", title)
	line("Sandbox: %s", esc(r.Sandbox))
	line("Nodes: %d total, %d verified, %d failed, %d escalated, %d blocked",
		r.Nodes.Total, r.Nodes.Verified, r.Nodes.Failed, r.Nodes.Escalated, r.Nodes.Blocked)
	line("Waves: %d", r.Waves)
	for _, e := range r.ByTaskType {
		line("%s  %s  calls %d  ok %d  malformed %d  retries %d  escalations m%d/r%d/h%d  tokens %d/%d",
			esc(e.TaskType), esc(e.Model), e.Calls, e.OK, e.Malformed, e.Retries,
			e.ModelEscalations, e.RevisionEscalations, e.HumanEscalations, e.PromptTokens, e.CompletionTokens)
	}
	line("Weak tests: %d  Repairs: %d", r.WeakTests, r.Repairs)
	line("Constraints checked: %d of %d", r.Constraints.Passed, r.Constraints.Total)
	if len(r.Failures) > 0 {
		line("Failures:")
		for _, f := range r.Failures {
			line("  %s", esc(f))
		}
	}
	if r.Landing != nil {
		line("Landing: branch %s, commit %s, merged into %s", esc(r.Landing.Branch), esc(r.Landing.Commit), esc(r.Landing.MergedInto))
	}
	if r.Incomplete {
		line("Incomplete: ledger writes failed")
	}
	line("Requirements covered: %d of %d", r.Requirements.Covered, r.Requirements.Total)
	line("Acceptance passed: %d of %d", r.Acceptance.Passed, r.Acceptance.Total)
	return b.String()
}
