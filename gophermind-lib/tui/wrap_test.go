package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// sizedModel is a ready model at the given terminal size with an empty
// transcript (the banner is dropped so tests assert on their own content).
func sizedModel(t *testing.T, w, h int) model {
	t.Helper()
	m := testModel(t)
	m.banner = ""
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = u.(model)
	m.content = ""
	m.sync()
	return m
}

// TestTranscriptLinesWrapToWidth is the reported bug: a transcript line longer
// than the terminal is silently cut off at the right edge instead of wrapping,
// so the tail of an error (or any long line) is unreadable.
func TestTranscriptLinesWrapToWidth(t *testing.T) {
	m := sizedModel(t, 80, 24)

	long := "error: iteration 0: perform request: Post " +
		`"http://192.168.5.2:8080/v1/chat/completions": dial tcp: connection refused`
	m.appendLine(renderError(long))
	m.sync()

	view := m.viewport.View()
	for i, ln := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(ln); w > m.viewport.Width {
			t.Fatalf("viewport line %d is %d wide, want <= %d: %q", i, w, m.viewport.Width, ansi.Strip(ln))
		}
	}
	// The tail must survive the wrap rather than being truncated away.
	if !strings.Contains(ansi.Strip(view), "connection refused") {
		t.Errorf("wrapped transcript lost the tail of the line:\n%s", ansi.Strip(view))
	}
}

// TestTranscriptWrapPreservesExplicitNewlines guards against a wrap that
// collapses the transcript's own line structure.
func TestTranscriptWrapPreservesExplicitNewlines(t *testing.T) {
	m := sizedModel(t, 80, 24)
	m.appendLine("alpha")
	m.appendLine("beta")
	m.sync()

	var trimmed []string
	for _, ln := range strings.Split(ansi.Strip(m.viewport.View()), "\n") {
		trimmed = append(trimmed, strings.TrimRight(ln, " "))
	}
	got := strings.Join(trimmed, "\n")
	if !strings.Contains(got, "alpha\nbeta") {
		t.Errorf("line structure lost:\n%q", got)
	}
}

// TestInputFitsInsideItsBox is the second reported bug: the textarea renders
// wider than the bordered box's content area, so a full-width line overflows
// and lipgloss wraps the remainder onto spurious extra rows — the box appears
// to sprout a stray newline.
func TestInputFitsInsideItsBox(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100} {
		m := sizedModel(t, w, 24)
		m.input.SetValue(strings.Repeat("x", 500))
		applyInputHeight(&m)

		// boxStyle is Width(w-2) with Padding(0, 1): content area is w-4.
		inner := w - 4
		for i, ln := range strings.Split(m.input.View(), "\n") {
			if got := ansi.StringWidth(ln); got > inner {
				t.Errorf("term %d: input row %d renders %d wide, box content area is %d", w, i, got, inner)
				break
			}
		}

		wantRows := desiredInputRows(m)
		if got := lipgloss.Height(boxStyle.Width(w - 2).Render(m.input.View())); got != wantRows+2 {
			t.Errorf("term %d: input box is %d rows tall, want %d (%d content + 2 borders)", w, got, wantRows+2, wantRows)
		}
	}
}
