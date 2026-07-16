package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/agent"
)

func testModel() model {
	m := newModel(func(sub chan tea.Msg, allowed *allowSet) *agent.Agent { return nil }, "m", "auto", "dark", false, false)
	m.width, m.height, m.ready = 80, 24, true
	return m
}

func TestSlashClearResetsState(t *testing.T) {
	m := testModel()
	m.stream = "leftover"
	m.content = "old transcript"
	m.input.SetValue("/clear")
	m2, _ := m.handleSubmit()
	if m2.stream != "" {
		t.Errorf("stream not cleared: %q", m2.stream)
	}
	if m2.content != "" {
		t.Errorf("content not cleared: %q", m2.content)
	}
	if m2.st != stateIdle {
		t.Errorf("state = %v, want idle", m2.st)
	}
}

func TestSlashExitQuits(t *testing.T) {
	m := testModel()
	m.input.SetValue("/exit")
	_, cmd := m.handleSubmit()
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("got %T, want tea.QuitMsg", msg)
	}
}

func TestSlashTempUpdatesTemperature(t *testing.T) {
	m := testModel()
	m.input.SetValue("/temp 0.7")
	m2, _ := m.handleSubmit()
	if m2.temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", m2.temperature)
	}
	if !strings.Contains(m2.content, "temperature set to 0.7") {
		t.Errorf("missing confirmation in transcript: %q", m2.content)
	}
}

func TestSlashTempZeroIsValid(t *testing.T) {
	m := testModel()
	m.temperature = 0.5
	m.input.SetValue("/temp 0")
	m2, _ := m.handleSubmit()
	if m2.temperature != 0 {
		t.Errorf("temperature = %v, want explicit 0", m2.temperature)
	}
}

func TestSlashTempInvalidLeavesValueUnchanged(t *testing.T) {
	m := testModel()
	m.temperature = 0.3
	m.input.SetValue("/temp abc")
	m2, _ := m.handleSubmit()
	if m2.temperature != 0.3 {
		t.Errorf("temperature changed on bad input: %v, want 0.3", m2.temperature)
	}
	if !strings.Contains(m2.content, "invalid number") {
		t.Errorf("expected error message in transcript, got: %q", m2.content)
	}
}

func TestSlashTempOutOfRangeRejected(t *testing.T) {
	m := testModel()
	m.temperature = 0.3
	m.input.SetValue("/temp 5")
	m2, _ := m.handleSubmit()
	if m2.temperature != 0.3 {
		t.Errorf("temperature changed on out-of-range input: %v, want 0.3", m2.temperature)
	}
	if !strings.Contains(m2.content, "error:") {
		t.Errorf("expected error message, got: %q", m2.content)
	}
}

func TestSlashTempRejectsNonFinite(t *testing.T) {
	// strconv.ParseFloat accepts "Inf"/"NaN" without error, so the validator —
	// not the parser — must reject them. Guards against a non-finite value
	// reaching the API.
	for _, bad := range []string{"Inf", "+Inf", "NaN", "-Inf"} {
		m := testModel()
		m.temperature = 0.3
		m.input.SetValue("/temp " + bad)
		m2, _ := m.handleSubmit()
		if m2.temperature != 0.3 {
			t.Errorf("/temp %s changed value to %v, want 0.3", bad, m2.temperature)
		}
	}
}

func TestSlashToppUpdatesAndUnsets(t *testing.T) {
	m := testModel()
	m.input.SetValue("/topp 0.9")
	m2, _ := m.handleSubmit()
	if m2.topP == nil || *m2.topP != 0.9 {
		t.Fatalf("topP = %v, want 0.9", m2.topP)
	}

	// Out-of-range top_p must not change the value.
	m2.input.SetValue("/topp 2")
	m3, _ := m2.handleSubmit()
	if m3.topP == nil || *m3.topP != 0.9 {
		t.Errorf("topP changed on bad input: %v, want 0.9", m3.topP)
	}
}

func TestSlashTempNoArgReportsCurrent(t *testing.T) {
	m := testModel()
	m.temperature = 0.42
	m.input.SetValue("/temp")
	m2, _ := m.handleSubmit()
	if m2.temperature != 0.42 {
		t.Errorf("temperature changed by bare /temp: %v", m2.temperature)
	}
	if !strings.Contains(m2.content, "temperature is 0.42") {
		t.Errorf("expected current value report, got: %q", m2.content)
	}
}

// TestCtrlCMidStreamCancelsWithoutQuitting verifies that Ctrl-C while a turn is
// in flight cancels that turn's context and stays in the session (no tea.Quit),
// rather than exiting the program.
func TestCtrlCMidStreamCancelsWithoutQuitting(t *testing.T) {
	m := testModel()
	ctx, cancel := context.WithCancel(context.Background())
	m.st = stateWorking
	m.cancel = cancel

	m2, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Must NOT quit while working.
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Ctrl-C mid-stream quit the program; want cancel-and-stay")
		}
	}
	// The in-flight context must have been cancelled.
	if ctx.Err() != context.Canceled {
		t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
	// Still in the session (state unchanged here; idle transition happens when the
	// cancelled Send reports back via errMsg).
	_ = m2.(model)
}

// TestCtrlCIdleQuits verifies Ctrl-C with no in-flight request still quits.
func TestCtrlCIdleQuits(t *testing.T) {
	m := testModel()
	m.st = stateIdle
	m.cancel = nil

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command when idle")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("got %T, want tea.QuitMsg", cmd())
	}
}

// TestCancelledErrMsgShowsCancelledAndReturnsIdle verifies a context.Canceled
// error from the agent renders as a clean "cancelled" line and returns the model
// to idle with the partial stream and cancel func cleared.
func TestCancelledErrMsgShowsCancelledAndReturnsIdle(t *testing.T) {
	m := testModel()
	m.st = stateWorking
	m.stream = "partial output"
	_, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m2, _ := m.Update(errMsg{err: context.Canceled})
	mm := m2.(model)

	if mm.st != stateIdle {
		t.Errorf("state = %v, want idle", mm.st)
	}
	if mm.stream != "" {
		t.Errorf("stream not cleared: %q", mm.stream)
	}
	if mm.cancel != nil {
		t.Error("cancel func not cleared")
	}
	if !strings.Contains(mm.content, "cancelled") {
		t.Errorf("transcript missing cancelled indication: %q", mm.content)
	}
	if strings.Contains(mm.content, "context canceled") {
		t.Errorf("raw error leaked into transcript: %q", mm.content)
	}
}

func TestApprovalKeysReply(t *testing.T) {
	m := testModel()
	reply := make(chan bool, 1)
	m.st = stateApproval
	m.pending = approvalMsg{tool: "write_file", args: "{}", reply: reply}

	m2, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	mm := m2.(model)
	select {
	case v := <-reply:
		if !v {
			t.Error("'a' should approve")
		}
	default:
		t.Fatal("no reply sent")
	}
	if !mm.allowed.has("write_file") {
		t.Error("'a' should add to always-allow")
	}
	if mm.st != stateWorking {
		t.Errorf("state = %v, want working", mm.st)
	}
}

// --- Task 9: multi-line auto-growing input ---

func TestDesiredInputRowsEmptyIsOne(t *testing.T) {
	m := testModel()
	if got := desiredInputRows(m); got != 1 {
		t.Errorf("desiredInputRows(empty) = %d, want 1", got)
	}
}

func TestDesiredInputRowsMultiLineClampsToFour(t *testing.T) {
	m := testModel()
	// 5 short logical lines: one row each, clamped down to the 4-row max.
	m.input.SetValue("a\nb\nc\nd\ne")
	if got := desiredInputRows(m); got != 4 {
		t.Errorf("desiredInputRows(5 lines) = %d, want 4 (clamped)", got)
	}
}

func TestDesiredInputRowsThreeLinesNoClampNeeded(t *testing.T) {
	m := testModel()
	m.input.SetValue("a\nb\nc")
	if got := desiredInputRows(m); got != 3 {
		t.Errorf("desiredInputRows(3 lines) = %d, want 3", got)
	}
}

func TestDesiredInputRowsLongLineWraps(t *testing.T) {
	m := testModel()
	textWidth := m.input.Width()
	if textWidth < 1 {
		t.Fatalf("textarea width = %d, want >= 1", textWidth)
	}
	// A single logical line more than 2x textWidth needs 3 wrapped rows.
	long := strings.Repeat("x", textWidth*2+5)
	m.input.SetValue(long)
	if got, want := desiredInputRows(m), 3; got != want {
		t.Errorf("desiredInputRows(long line, textWidth=%d) = %d, want %d", textWidth, got, want)
	}
}

func TestApplyInputHeightRecomputesViewportHeight(t *testing.T) {
	m := testModel()
	m.input.SetValue("line one\nline two") // 2 short logical lines -> 2 rows
	applyInputHeight(&m)
	if got := m.input.Height(); got != 2 {
		t.Fatalf("input.Height() = %d, want 2", got)
	}
	want := m.height - (2 + 2) - statusHeight
	if want < 1 {
		want = 1
	}
	if m.viewport.Height != want {
		t.Errorf("viewport.Height = %d, want %d", m.viewport.Height, want)
	}
}

func TestAltEnterInsertsNewlineAndGrows(t *testing.T) {
	m := testModel()
	m.input.SetValue("hello")
	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m2 := got.(model)
	if !strings.Contains(m2.input.Value(), "\n") {
		t.Fatalf("Alt+Enter did not insert a newline: %q", m2.input.Value())
	}
	if m2.input.Height() < 2 {
		t.Errorf("input.Height() = %d, want >= 2 after newline insert", m2.input.Height())
	}
	if m2.st != stateIdle {
		t.Errorf("state = %v, want idle (Alt+Enter must not submit)", m2.st)
	}
}

func TestCtrlJInsertsNewlineAndGrows(t *testing.T) {
	m := testModel()
	m.input.SetValue("hello")
	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m2 := got.(model)
	if !strings.Contains(m2.input.Value(), "\n") {
		t.Fatalf("Ctrl+J did not insert a newline: %q", m2.input.Value())
	}
	if m2.input.Height() < 2 {
		t.Errorf("input.Height() = %d, want >= 2 after newline insert", m2.input.Height())
	}
}

func TestPlainEnterStillSubmits(t *testing.T) {
	m := testModel()
	m.input.SetValue("hi there")
	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := got.(model)
	if m2.input.Value() != "" {
		t.Errorf("input not reset after submit: %q", m2.input.Value())
	}
	if !strings.Contains(m2.content, "hi there") {
		t.Errorf("submitted text missing from transcript: %q", m2.content)
	}
}

func TestInputShrinksToOneRowAfterSubmit(t *testing.T) {
	m := testModel()
	m.input.SetValue("line one\nline two\nline three")
	applyInputHeight(&m)
	if m.input.Height() < 2 {
		t.Fatalf("setup: expected grown input before submit, got height %d", m.input.Height())
	}
	m2, _ := m.handleSubmit()
	if got := m2.input.Height(); got != 1 {
		t.Errorf("input.Height() after submit = %d, want 1", got)
	}
}

func TestSlashHelpListsRegisteredCommands(t *testing.T) {
	if len(slashCommands) == 0 {
		t.Fatal("slashCommands registry is empty")
	}
	for _, c := range slashCommands {
		if c.Desc == "" {
			t.Errorf("command %q has empty Desc", c.Name)
		}
	}

	m := testModel()
	m.input.SetValue("/help")
	m2, _ := m.handleSubmit()
	for _, name := range commandNames() {
		if !strings.Contains(m2.content, name) {
			t.Errorf("/help output missing registered command %q: %q", name, m2.content)
		}
	}
}
