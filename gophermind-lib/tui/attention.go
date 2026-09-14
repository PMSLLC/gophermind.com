package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The attention signal: when gophermind starts waiting on the user — a gated
// tool needs a decision, or a turn finished and handed control back — the whole
// screen flashes inverse a few times and then STAYS inverse until the user
// touches the keyboard. It is aimed at the walk-away case: a long autonomous
// run that needs you back, noticed from across the room.
const (
	ansiReverse = "\x1b[7m"
	ansiReset   = "\x1b[0m"

	// flashInterval is one phase (one inverse or one normal frame). Four
	// flashes at this rate take just under a second — long enough to catch the
	// eye, short enough not to feel like a fault.
	flashInterval = 110 * time.Millisecond

	// DefaultAttentionFlashes is the flash count when nothing is configured.
	DefaultAttentionFlashes = 4
)

// flashMsg advances the attention flash by one phase.
type flashMsg struct{}

// flashTick schedules the next flash phase.
func flashTick() tea.Cmd {
	return tea.Tick(flashInterval, func(time.Time) tea.Msg { return flashMsg{} })
}

// beginAttention starts the signal. It returns the command that drives the
// flashing, or nil when the feature is off (attentionFlashes == 0) or the
// signal is already showing — a second trigger must not restart the flashing
// underneath the first.
func (m *model) beginAttention() tea.Cmd {
	if m.attentionFlashes == 0 || m.attention {
		return nil
	}
	m.attention = true
	// Two phases per flash (inverse, then normal), after which flashLeft is 0
	// and the screen settles inverse until a key arrives.
	m.flashLeft = m.attentionFlashes * 2
	return flashTick()
}

// clearAttention dismisses the signal. Called on every keypress: the whole
// point is to get the user back to the keyboard, so touching it means the
// message landed.
func (m *model) clearAttention() {
	m.attention = false
	m.flashLeft = 0
}

// attentionVisible reports whether THIS frame renders inverse. While flashing,
// even phases are inverse and odd ones are not; once flashLeft reaches 0 the
// screen stays inverse for as long as the signal is up.
func (m model) attentionVisible() bool {
	if !m.attention {
		return false
	}
	return m.flashLeft%2 == 0
}

// invertFrame renders a composed frame in inverse video.
//
// Wrapping the frame in a lipgloss Reverse style does NOT work: styled spans in
// the transcript end with a reset, which clears the reverse attribute for
// everything after them on that line. Setting a foreground or background color
// does not clear it, so the only thing that must be repaired is the reset — the
// frame is re-armed after every one, and each line is opened inverse and closed
// reset so nothing bleeds past the frame.
func invertFrame(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		ln = strings.ReplaceAll(ln, ansiReset, ansiReset+ansiReverse)
		ln = strings.ReplaceAll(ln, "\x1b[m", "\x1b[m"+ansiReverse) // the empty-parameter reset
		lines[i] = ansiReverse + ln + ansiReset
	}
	return strings.Join(lines, "\n")
}
