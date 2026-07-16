package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jbrahy/bubblecomplete"
)

// typeString drives m through handleKey one rune at a time, as real
// keystrokes would, so the completion Query wiring (queryComplete, called
// after every non-consumed mutating key) actually runs.
func typeString(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = got.(model)
	}
	return m
}

// TestCursorOffsetSingleLine covers cursor-offset Strategy A (see
// completion_wire.go doc comment) on a single logical line: the flat rune
// offset must equal the textarea's column directly.
func TestCursorOffsetSingleLine(t *testing.T) {
	ta := textarea.New()
	ta.SetWidth(40)
	ta.Focus()
	ta.SetValue("hello world")
	ta.SetCursor(5)
	if got, want := cursorOffset(ta), 5; got != want {
		t.Errorf("cursorOffset = %d, want %d", got, want)
	}
}

// TestCursorOffsetMultiLine covers Strategy A across logical lines: the
// offset must include the rune length (plus newline) of every prior line.
func TestCursorOffsetMultiLine(t *testing.T) {
	ta := textarea.New()
	ta.SetWidth(40)
	ta.Focus()
	ta.SetValue("ab\ncd") // cursor lands at the end (row 1, "cd") after SetValue
	ta.SetCursor(1)       // between 'c' and 'd'
	// "ab" (2 runes) + '\n' (1) + col 1 = 4.
	if got, want := cursorOffset(ta), 4; got != want {
		t.Errorf("cursorOffset = %d, want %d", got, want)
	}
}

// TestApplyCandidateReplaceDeletesRunes covers the brief's Replace>0 case
// directly: applyCandidate must delete exactly Replace runes before the
// cursor before inserting Text.
func TestApplyCandidateReplaceDeletesRunes(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("hello wrld") // cursor lands at the end after SetValue
	m.applyCandidate(bubblecomplete.Candidate{Text: "world", Replace: 4})
	if got, want := m.input.Value(), "hello world"; got != want {
		t.Errorf("input.Value() = %q, want %q", got, want)
	}
}

// TestSlashMenuTabCompletes covers: typing a prefix that matches multiple
// slash commands shows a popup menu containing /help, and Tab completes the
// selected (first) candidate into the input. ("/he" only matches /help in
// the current registry — see completion_providers_test.go's
// TestCommandProviderSingleMatchGhost — so a bare "/" is used here to
// legitimately exercise ModeMenu with /help as one of the candidates,
// per Task 11's brief intent.)
func TestSlashMenuTabCompletes(t *testing.T) {
	m := testModel(t)
	m = typeString(t, m, "/")
	if m.complete.Mode() != bubblecomplete.ModeMenu {
		t.Fatalf("setup: mode = %v, want ModeMenu after typing \"/\"", m.complete.Mode())
	}
	if view := m.complete.View(); !strings.Contains(view, "/help") {
		t.Errorf("menu view missing /help: %q", view)
	}

	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m2 := got.(model)
	if m2.input.Value() != "/help" {
		t.Errorf("input.Value() after Tab = %q, want \"/help\"", m2.input.Value())
	}
	if m2.complete.Mode() != bubblecomplete.ModeNone {
		t.Errorf("mode after Tab-accept = %v, want ModeNone", m2.complete.Mode())
	}
}

// TestRecallGhostRightAcceptsAtEndOnly covers: a recalled prompt prefix
// shows inline ghost text; → accepts it when the cursor is at the true end
// of the input, and only moves the cursor (does not accept) otherwise.
func TestRecallGhostRightAcceptsAtEndOnly(t *testing.T) {
	m := testModel(t)
	const full = "zzzqux unique prompt for recall testing"
	const prefix = "zzzqux unique"

	m.input.SetValue(full)
	m, _ = m.handleSubmit() // records `full` into history
	// handleSubmit sets stateWorking and normally waits on the agent
	// goroutine's doneMsg to return to stateIdle; testModel's agent is nil,
	// so there is no such goroutine here — reset it directly, as a real
	// completed turn would.
	m.st = stateIdle

	m = typeString(t, m, prefix)
	if m.complete.Mode() != bubblecomplete.ModeGhost {
		t.Fatalf("setup: mode = %v, want ModeGhost after typing prefix", m.complete.Mode())
	}
	wantGhost := full[len(prefix):]
	if got := m.complete.Ghost(); got != wantGhost {
		t.Fatalf("setup: ghost = %q, want %q", got, wantGhost)
	}

	// → NOT at end: back the cursor up one rune first, so → only restores
	// the cursor position instead of accepting.
	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	mLeft := got.(model)
	got2, _ := mLeft.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	mNotEnd := got2.(model)
	if mNotEnd.input.Value() != prefix {
		t.Errorf("→ not at end mutated input: %q, want unchanged %q", mNotEnd.input.Value(), prefix)
	}

	// → AT end (from the original, untouched cursor position): accepts.
	got3, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	mEnd := got3.(model)
	if mEnd.input.Value() != full {
		t.Errorf("→ at end = %q, want accepted %q", mEnd.input.Value(), full)
	}
	if mEnd.complete.Mode() != bubblecomplete.ModeNone {
		t.Errorf("mode after accept = %v, want ModeNone", mEnd.complete.Mode())
	}
}

// TestEnterAcceptsOpenMenuInsteadOfSubmitting covers: Enter with a popup
// menu open accepts the selected candidate and does NOT submit the prompt.
// (Enter with no active suggestion still submits — see the pre-existing
// TestPlainEnterStillSubmits in update_test.go, unaffected by this wiring.)
func TestEnterAcceptsOpenMenuInsteadOfSubmitting(t *testing.T) {
	m := testModel(t)
	m = typeString(t, m, "/")
	if m.complete.Mode() != bubblecomplete.ModeMenu {
		t.Fatalf("setup: mode = %v, want ModeMenu", m.complete.Mode())
	}

	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := got.(model)
	if m2.input.Value() != "/help" {
		t.Errorf("input.Value() after Enter-accept = %q, want \"/help\"", m2.input.Value())
	}
	if m2.content != "" {
		t.Errorf("transcript = %q, want empty (Enter must accept, not submit)", m2.content)
	}
	if m2.st != stateIdle {
		t.Errorf("st = %v, want stateIdle (no submission started)", m2.st)
	}
}

// TestEscDismissesSuggestionBeforeCancel covers: Esc with an active
// suggestion dismisses it and does NOT also trigger the cancel/interrupt
// behavior; a second Esc (nothing active) falls through to that behavior.
func TestEscDismissesSuggestionBeforeCancel(t *testing.T) {
	m := testModel(t)
	cancelled := false
	m.cancel = func() { cancelled = true }

	m = typeString(t, m, "/")
	if !m.complete.Active() {
		t.Fatalf("setup: expected an active suggestion after typing \"/\", mode=%v", m.complete.Mode())
	}

	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := got.(model)
	if m2.complete.Active() {
		t.Errorf("suggestion still active after Esc, want dismissed")
	}
	if cancelled {
		t.Errorf("cancel() was called on the dismiss-only Esc, want it suppressed")
	}

	got2, _ := m2.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	_ = got2.(model)
	if !cancelled {
		t.Errorf("cancel() was not called on the second Esc (nothing active), want it invoked")
	}
}

// TestNoSuggestionsWhileWorking covers: suggestions are never queried/shown
// outside stateIdle. Typing must still land in the input (unchanged
// pre-Task-11 behavior), but m.complete must not react to it.
func TestNoSuggestionsWhileWorking(t *testing.T) {
	m := testModel(t)
	m.st = stateWorking

	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m2 := got.(model)
	if m2.complete.Mode() != bubblecomplete.ModeNone {
		t.Errorf("mode = %v, want ModeNone while working", m2.complete.Mode())
	}
	if m2.input.Value() != "/" {
		t.Errorf("input.Value() = %q, want \"/\" (typing must still work while the agent runs)", m2.input.Value())
	}
}

// TestViewSplicesMenuAboveInput covers view.go: an open popup menu is
// spliced into the rendered view above the bordered input box.
func TestViewSplicesMenuAboveInput(t *testing.T) {
	m := testModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(model)
	m = typeString(t, m, "/")
	if m.complete.Mode() != bubblecomplete.ModeMenu {
		t.Fatalf("setup: mode = %v, want ModeMenu", m.complete.Mode())
	}

	rendered := m.View()
	if !strings.Contains(rendered, "/help") {
		t.Errorf("rendered view missing spliced menu content: %q", rendered)
	}
}

// TestViewInlineGhostSameLine covers view.go's TRULY-INLINE ghost rendering:
// the ghost continuation must appear on the SAME rendered line as the typed
// text, not as a separate line.
func TestViewInlineGhostSameLine(t *testing.T) {
	m := testModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(model)

	const full = "zzzqux unique prompt for recall testing"
	const prefix = "zzzqux unique"
	m.input.SetValue(full)
	m, _ = m.handleSubmit() // records `full` into history
	m.st = stateIdle        // see the identical comment in TestRecallGhostRightAcceptsAtEndOnly

	m = typeString(t, m, prefix)
	if m.complete.Mode() != bubblecomplete.ModeGhost {
		t.Fatalf("setup: mode = %v, want ModeGhost", m.complete.Mode())
	}

	rendered := m.View()
	var ghostLine string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, prefix) {
			ghostLine = line
			break
		}
	}
	if ghostLine == "" {
		t.Fatalf("no rendered line contains the typed prefix %q:\n%s", prefix, rendered)
	}
	wantTail := full[len(prefix):]
	if !strings.Contains(ghostLine, strings.TrimSpace(wantTail)) {
		t.Errorf("ghost tail %q not inline on the same line as the prefix: %q", wantTail, ghostLine)
	}
}
