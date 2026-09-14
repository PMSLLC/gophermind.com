package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"gophermind/gophermind-lib/phaseflow"
)

// colorful forces a real color profile so styled spans actually emit SGR codes;
// without it lipgloss strips them in tests and the invert has nothing to fight.
func colorful(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

// TestInvertFrameSurvivesStyledSpans is the reason a plain lipgloss wrapper
// does not work: an inner style ends with a reset, which clears reverse for
// everything after it on that line. Every reset must re-arm it.
func TestInvertFrameSurvivesStyledSpans(t *testing.T) {
	colorful(t)
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Render("error")
	got := invertFrame(styled + " and then plain")

	if !strings.HasPrefix(got, ansiReverse) {
		t.Errorf("frame does not start inverted: %q", got)
	}
	if !strings.HasSuffix(got, ansiReset) {
		t.Errorf("frame does not end reset (it would bleed past the frame): %q", got)
	}
	// The styled span's own reset must be immediately re-armed, or the text
	// after it renders normally while everything around it is inverse.
	if strings.Count(got, ansiReset) < 2 {
		t.Fatalf("expected the inner reset to survive: %q", got)
	}
	inner := got[:strings.LastIndex(got, ansiReset)] // drop the frame's closing reset
	if !strings.Contains(inner, ansiReset+ansiReverse) {
		t.Errorf("reverse was not re-armed after the styled span:\n%q", got)
	}
}

func TestInvertFrameInvertsEveryLine(t *testing.T) {
	got := invertFrame("one\ntwo\nthree")
	for i, ln := range strings.Split(got, "\n") {
		if !strings.HasPrefix(ln, ansiReverse) || !strings.HasSuffix(ln, ansiReset) {
			t.Errorf("line %d not independently inverted: %q", i, ln)
		}
	}
}

// inverted reports whether the rendered view is in attention (inverse) mode.
func inverted(m model) bool {
	return strings.HasPrefix(m.View(), ansiReverse)
}

// TestApprovalInvertsTheScreen: a gated tool cannot proceed without the user,
// which is the strongest "your turn" signal there is.
func TestApprovalInvertsTheScreen(t *testing.T) {
	colorful(t)
	m := sizedModel(t, 80, 24)
	m.st = stateWorking
	if inverted(m) {
		t.Fatal("screen is inverted while working")
	}

	u, _ := m.Update(approvalMsg{tool: "run_shell", args: "{}", reply: make(chan bool, 1)})
	if !inverted(u.(model)) {
		t.Error("approval prompt did not invert the screen")
	}
}

// TestTurnCompletionInvertsTheScreen is the walk-away case: a long turn ends
// and hands control back.
func TestTurnCompletionInvertsTheScreen(t *testing.T) {
	colorful(t)
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"answer", doneMsg{answer: "done"}},
		{"error", errMsg{err: errors.New("boom")}},
		{"executor", execDoneMsg{summary: phaseflow.RunSummary{}}},
	} {
		m := sizedModel(t, 80, 24)
		m.st = stateWorking
		u, _ := m.Update(tc.msg)
		if !inverted(u.(model)) {
			t.Errorf("%s: turn completion did not invert the screen", tc.name)
		}
	}
}

// TestAnyKeyClearsTheInvert: the signal exists to get the user back to the
// keyboard, so touching the keyboard is what dismisses it.
func TestAnyKeyClearsTheInvert(t *testing.T) {
	colorful(t)
	m := sizedModel(t, 80, 24)
	m.st = stateWorking
	u, _ := m.Update(doneMsg{answer: "done"})
	m = u.(model)
	if !inverted(m) {
		t.Fatal("setup: screen not inverted after the turn ended")
	}

	u2, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if inverted(u2.(model)) {
		t.Error("typing did not clear the invert")
	}
}

// TestStreamingDoesNotInvert keeps the signal meaningful: while the agent is
// producing output it is not waiting on anyone.
func TestStreamingDoesNotInvert(t *testing.T) {
	colorful(t)
	m := sizedModel(t, 80, 24)
	m.st = stateWorking
	u, _ := m.Update(tokenMsg("thinking"))
	if inverted(u.(model)) {
		t.Error("screen inverted mid-stream")
	}
}

// TestProjectTurnDoesNotInvert: an interview turn completing hands straight
// back to the state machine, which starts the next turn without the user.
func TestProjectTurnDoesNotInvert(t *testing.T) {
	colorful(t)
	m := sizedModel(t, 80, 24)
	m.st = stateWorking
	m.projTurn = true
	m.proj = projInterview
	u, _ := m.Update(doneMsg{answer: "{}"})
	if inverted(u.(model)) {
		t.Error("a /project turn inverted the screen mid-flow")
	}
}
