package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// recallHistory implements shell-style Up/Down prompt recall over the same
// persisted store that backs the recall/markov completion providers, so what
// Up walks through is exactly what was submitted, across sessions.
//
// It reports whether it consumed the key. Three cases deliberately decline, and
// the key falls through to the textarea as before:
//
//   - history is empty (first run),
//   - Up with the cursor below the first line, or Down above the last line —
//     inside a multi-line draft the arrows move the cursor, matching every
//     shell; only from the edge lines do they mean "history",
//   - Down while not browsing, which is a plain cursor move.
//
// An open completion menu never reaches here: handleKey gives the completion
// controller first refusal on every key, so its ↑/↓ navigation still wins.
func (m model) recallHistory(k tea.KeyType) (model, bool) {
	if m.hist == nil {
		return m, false
	}
	entries := m.hist.All()
	if len(entries) == 0 {
		return m, false
	}

	switch k {
	case tea.KeyUp:
		if m.input.Line() != 0 {
			return m, false
		}
		if m.histIdx >= len(entries) {
			// Starting a browse: stash the in-progress line before it is
			// overwritten so Down can restore it.
			m.histDraft = m.input.Value()
			m.histIdx = len(entries)
		}
		if m.histIdx == 0 {
			// Pinned at the oldest entry. Consume the key anyway, so a held Up
			// does not start yanking the cursor around the recalled text.
			return m, true
		}
		m.histIdx--

	case tea.KeyDown:
		if m.histIdx >= len(entries) {
			return m, false
		}
		if m.input.Line() != m.input.LineCount()-1 {
			return m, false
		}
		m.histIdx++
		if m.histIdx == len(entries) {
			m.setInputFromRecall(m.histDraft)
			return m, true
		}

	default:
		return m, false
	}

	m.setInputFromRecall(entries[m.histIdx])
	return m, true
}

// setInputFromRecall replaces the prompt with a recalled line, parks the cursor
// at the end (as a shell does), and re-runs the layout and suggestion passes
// that any other input mutation would trigger.
func (m *model) setInputFromRecall(s string) {
	m.input.SetValue(s)
	m.input.CursorEnd()
	applyInputHeight(m)
	m.queryComplete()
}

// resetRecall returns to the not-browsing state. Called on submit so the next
// Up starts from the newest entry rather than resuming an old browse.
func (m *model) resetRecall() {
	if m.hist != nil {
		m.histIdx = len(m.hist.All())
	}
	m.histDraft = ""
}
