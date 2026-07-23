package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jbrahy/bubblecomplete"
	"gophermind/internal/agent"
)

// seedHistory submits prompts through the real path so the store, the n-gram
// trainer, and the recall provider all see them exactly as they would live.
func seedHistory(t *testing.T, m model, prompts ...string) model {
	t.Helper()
	for _, p := range prompts {
		m.input.SetValue(p)
		m2, _ := m.handleSubmit()
		m = m2
		m.st = stateIdle // handleSubmit moves to stateWorking; tests drive the idle path
	}
	return m
}

func press(m model, k tea.KeyType) model {
	next, _ := m.handleKey(tea.KeyMsg{Type: k})
	return next.(model)
}

// TestUpRecallsPreviousPrompt is the core ask: with the prompt empty, Up walks
// backwards through what was submitted, most recent first.
func TestUpRecallsPreviousPrompt(t *testing.T) {
	m := seedHistory(t, testModel(t), "first thing", "second thing")

	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "second thing" {
		t.Fatalf("after one Up, input = %q, want %q", got, "second thing")
	}

	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "first thing" {
		t.Fatalf("after two Ups, input = %q, want %q", got, "first thing")
	}
}

// TestUpStopsAtOldestPrompt keeps Up from walking off the end of the list.
func TestUpStopsAtOldestPrompt(t *testing.T) {
	m := seedHistory(t, testModel(t), "only one")

	m = press(m, tea.KeyUp)
	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "only one" {
		t.Fatalf("input = %q, want to stay pinned at %q", got, "only one")
	}
}

// TestDownReturnsTowardNewest walks back down the list after going up.
func TestDownReturnsTowardNewest(t *testing.T) {
	m := seedHistory(t, testModel(t), "alpha", "beta")

	m = press(m, tea.KeyUp)
	m = press(m, tea.KeyUp) // at "alpha"
	m = press(m, tea.KeyDown)
	if got := m.input.Value(); got != "beta" {
		t.Fatalf("after Down, input = %q, want %q", got, "beta")
	}
}

// TestDownPastNewestRestoresDraft protects the half-typed line: browsing up
// then back down must hand back what was being typed, not an empty box.
func TestDownPastNewestRestoresDraft(t *testing.T) {
	m := seedHistory(t, testModel(t), "committed")
	m.input.SetValue("half-typed draft")

	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "committed" {
		t.Fatalf("Up did not recall: %q", got)
	}
	m = press(m, tea.KeyDown)
	if got := m.input.Value(); got != "half-typed draft" {
		t.Fatalf("draft not restored: input = %q, want %q", got, "half-typed draft")
	}
}

// TestDownWithoutRecallLeavesInputAlone: Down is only a history key while
// browsing. Otherwise it belongs to the textarea.
func TestDownWithoutRecallLeavesInputAlone(t *testing.T) {
	m := seedHistory(t, testModel(t), "something")
	m.input.SetValue("typing")

	m = press(m, tea.KeyDown)
	if got := m.input.Value(); got != "typing" {
		t.Fatalf("Down clobbered the input: %q", got)
	}
}

// TestSubmitResetsRecall: after submitting, Up must start again from the newest
// entry rather than resuming wherever the last browse left off.
func TestSubmitResetsRecall(t *testing.T) {
	m := seedHistory(t, testModel(t), "one", "two")

	m = press(m, tea.KeyUp)
	m = press(m, tea.KeyUp) // parked at "one"

	m = seedHistory(t, m, "three")
	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "three" {
		t.Fatalf("recall did not reset after submit: input = %q, want %q", got, "three")
	}
}

// TestUpOnLaterLineMovesCursorNotHistory is the multi-line rule: inside a
// multi-line draft, Up/Down move the cursor. Only from the first/last line does
// the key mean "history".
func TestUpOnLaterLineMovesCursorNotHistory(t *testing.T) {
	m := seedHistory(t, testModel(t), "prior prompt")
	m.input.SetValue("line one\nline two")
	// SetValue leaves the cursor at the end, i.e. on the last line.

	before := m.input.Value()
	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != before {
		t.Fatalf("Up from line 2 replaced the draft with history: %q", got)
	}
	if m.input.Line() != 0 {
		t.Fatalf("Up did not move the cursor to line 0, got line %d", m.input.Line())
	}

	// Now on the first line, a second Up does mean history.
	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "prior prompt" {
		t.Fatalf("Up from line 1 did not recall history: %q", got)
	}
}

// TestOpenMenuKeepsArrowKeys is the precedence guard: while the completion
// menu is open its ↑/↓ select candidates, so recall must not fire and must not
// replace the input with a history entry.
func TestOpenMenuKeepsArrowKeys(t *testing.T) {
	m := seedHistory(t, testModel(t), "a real prompt")

	m = typeString(t, m, "/")
	if m.complete.Mode() != bubblecomplete.ModeMenu {
		t.Fatalf("setup: mode = %v, want ModeMenu after typing \"/\"", m.complete.Mode())
	}

	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "/" {
		t.Errorf("Up with the menu open recalled history: input = %q, want %q", got, "/")
	}
	if m.complete.Mode() != bubblecomplete.ModeMenu {
		t.Errorf("menu closed by Up: mode = %v, want ModeMenu", m.complete.Mode())
	}
}

// TestGhostSuggestionStillAllowsRecall is the other side of that boundary:
// inline ghost text does not claim the arrows, so Up must still recall.
func TestGhostSuggestionStillAllowsRecall(t *testing.T) {
	m := seedHistory(t, testModel(t), "zzz distinctive prompt")

	m = typeString(t, m, "zzz dist")
	if m.complete.Mode() != bubblecomplete.ModeGhost {
		t.Fatalf("setup: mode = %v, want ModeGhost", m.complete.Mode())
	}

	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "zzz distinctive prompt" {
		t.Errorf("Up under ghost text did not recall: input = %q", got)
	}
}

// TestRecallSurvivesRestart is the cross-session promise: a second model built
// against the same config dir must recall what the first one submitted, since
// prompthistory persists to <GOPHERMIND_CONFIG_DIR>/history.
func TestRecallSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)

	first := newModel(func(sub chan tea.Msg, allowed *allowSet) *agent.Agent { return nil }, "m", "", "auto", "dark", false, false)
	first.width, first.height, first.ready = 80, 24, true
	first = seedHistory(t, first, "prompt from the last session")

	// A fresh model stands in for a restart: same config dir, new process.
	second := newModel(func(sub chan tea.Msg, allowed *allowSet) *agent.Agent { return nil }, "m", "", "auto", "dark", false, false)
	second.width, second.height, second.ready = 80, 24, true

	second = press(second, tea.KeyUp)
	if got := second.input.Value(); got != "prompt from the last session" {
		t.Errorf("restarted model did not recall prior history: input = %q", got)
	}
}

// TestEmptyHistoryUpIsInert guards the first-run path.
func TestEmptyHistoryUpIsInert(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("draft")

	m = press(m, tea.KeyUp)
	if got := m.input.Value(); got != "draft" {
		t.Fatalf("Up with empty history changed the input: %q", got)
	}
}
