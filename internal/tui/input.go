package tui

import (
	"strings"

	"github.com/rivo/uniseg"
)

// maxInputRows is the tallest the input box auto-grows to before the
// textarea starts scrolling its own content internally.
const maxInputRows = 4

// desiredInputRows returns the number of rows the input textarea needs to
// show its current content without internal scrolling, clamped to
// [1, maxInputRows]. It counts wrapped rows per logical ("\n"-separated)
// line rather than relying on textarea.Model.LineCount(), which only counts
// logical lines and misses soft-wrapping — so a single long line also grows
// the box, not just explicit newlines.
//
// textWidth is taken directly from m.input.Width(), with NO further
// subtraction for the prompt. That deviates from the naive expectation that
// the prompt column still needs to be carved out here: textarea.Model.
// SetWidth already reserves the prompt width (promptWidth, from Prompt or
// SetPromptFunc) internally before storing the field Width() returns, and
// the textarea wraps each line against exactly that stored width
// (memoizedWrap(line, m.width) in bubbles v1.0.0). Subtracting the prompt
// width again here would double-count it and under-estimate how much text
// fits per row.
func desiredInputRows(m model) int {
	value := m.input.Value()
	if value == "" {
		return 1
	}

	textWidth := m.input.Width()
	if textWidth < 1 {
		textWidth = 1
	}

	rows := 0
	for _, line := range strings.Split(value, "\n") {
		w := uniseg.StringWidth(line)
		r := (w + textWidth - 1) / textWidth // ceil(w / textWidth)
		if r < 1 {
			r = 1
		}
		rows += r
	}
	if rows < 1 {
		rows = 1
	}
	if rows > maxInputRows {
		rows = maxInputRows
	}
	return rows
}

// applyInputHeight resizes the input textarea to fit its current content
// (see desiredInputRows) and shrinks/grows the transcript viewport to make
// room for it. It must be called after anything that can change the input's
// row count: window resizes, keys that mutate the input, and submit (which
// resets the input back down to 1 row).
func applyInputHeight(m *model) {
	rows := desiredInputRows(*m)
	m.input.SetHeight(rows)
	if !m.ready {
		return
	}
	// rows+2 accounts for the bordered input box's top/bottom border.
	h := m.height - (rows + 2) - statusHeight
	if h < 1 {
		h = 1
	}
	m.viewport.Height = h
}
