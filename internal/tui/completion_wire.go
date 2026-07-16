package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jbrahy/bubblecomplete"
)

// cursorOffset computes the real flat RUNE offset of ta's cursor into
// ta.Value(). This is cursor-offset Strategy A from the Task 11 brief:
// bubbles textarea has no direct flat-offset getter, but Line() reports the
// logical ("\n"-separated) line index and LineInfo() reports enough to
// reconstruct the column within that logical line — StartColumn (the rune
// offset of the current soft-wrapped row's start within the logical line)
// plus ColumnOffset (the column within that soft-wrapped row) together equal
// the logical column, even though each field individually is relative to a
// wrapped sub-row, not the logical line. Summing the rune length of every
// prior logical line (+1 per newline) and adding that column gives the exact
// offset Query/Update expect, so →-accept and ghost-only-at-end are correct
// relative to the ACTUAL cursor position rather than assuming end-of-input.
func cursorOffset(ta textarea.Model) int {
	lines := strings.Split(ta.Value(), "\n")
	row := ta.Line()
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}

	offset := 0
	for i := 0; i < row; i++ {
		offset += utf8.RuneCountInString(lines[i]) + 1 // +1 for the newline
	}

	li := ta.LineInfo()
	offset += li.StartColumn + li.ColumnOffset
	return offset
}

// applyCandidate mutates the textarea to reflect an accepted completion
// Candidate: delete Replace runes immediately before the cursor, then insert
// Text. Replace is simulated via the textarea's own Backspace key handling
// (so it respects the textarea's cursor/line bookkeeping) rather than string
// surgery.
func (m *model) applyCandidate(c bubblecomplete.Candidate) {
	for i := 0; i < c.Replace; i++ {
		m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m.input.InsertString(c.Text)
}

// queryComplete re-runs completion Query against the input's current value
// and real cursor offset. Callers must only invoke this while m.st ==
// stateIdle — suggestions are never computed while working or awaiting
// approval (see handleKey).
func (m *model) queryComplete() {
	m.complete = m.complete.Query(m.input.Value(), cursorOffset(m.input))
}

// singleVisualLine reports whether the input currently renders as exactly
// one visual row (no explicit newline, no soft-wrap). This is the v1
// simplification boundary for the inline ghost compose in view.go: a
// fully-correct multi-line inline compose (accounting for wrapped rows and
// the textarea's own per-row padding) is out of scope for Task 11, so the
// ghost is only spliced inline when the box is showing a single row.
func singleVisualLine(m model) bool {
	return desiredInputRows(m) == 1
}

// cursorAtEnd reports whether the cursor sits at the true end of the input
// value, using the same flat rune offset as Query/Accept. The inline ghost
// is only rendered when this holds, since it is drawn appended after the
// input's text.
func cursorAtEnd(m model) bool {
	return cursorOffset(m.input) == utf8.RuneCountInString(m.input.Value())
}
