package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/agent"
)

func testModel(t *testing.T) model {
	t.Helper()
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	m := newModel(func(sub chan tea.Msg, allowed *allowSet) *agent.Agent { return nil }, "m", "auto", "dark", false, false)
	m.width, m.height, m.ready = 80, 24, true
	return m
}

func TestSlashClearResetsState(t *testing.T) {
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
	m.temperature = 0.5
	m.input.SetValue("/temp 0")
	m2, _ := m.handleSubmit()
	if m2.temperature != 0 {
		t.Errorf("temperature = %v, want explicit 0", m2.temperature)
	}
}

func TestSlashTempInvalidLeavesValueUnchanged(t *testing.T) {
	m := testModel(t)
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
	m := testModel(t)
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
		m := testModel(t)
		m.temperature = 0.3
		m.input.SetValue("/temp " + bad)
		m2, _ := m.handleSubmit()
		if m2.temperature != 0.3 {
			t.Errorf("/temp %s changed value to %v, want 0.3", bad, m2.temperature)
		}
	}
}

func TestSlashToppUpdatesAndUnsets(t *testing.T) {
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
	if got := desiredInputRows(m); got != 1 {
		t.Errorf("desiredInputRows(empty) = %d, want 1", got)
	}
}

func TestDesiredInputRowsMultiLineClampsToFour(t *testing.T) {
	m := testModel(t)
	// 5 short logical lines: one row each, clamped down to the 4-row max.
	m.input.SetValue("a\nb\nc\nd\ne")
	if got := desiredInputRows(m); got != 4 {
		t.Errorf("desiredInputRows(5 lines) = %d, want 4 (clamped)", got)
	}
}

func TestDesiredInputRowsThreeLinesNoClampNeeded(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("a\nb\nc")
	if got := desiredInputRows(m); got != 3 {
		t.Errorf("desiredInputRows(3 lines) = %d, want 3", got)
	}
}

func TestDesiredInputRowsLongLineWraps(t *testing.T) {
	m := testModel(t)
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

func TestDesiredInputRowsExactMultipleOfWidth(t *testing.T) {
	m := testModel(t)
	textWidth := m.input.Width()
	if textWidth < 1 {
		t.Fatalf("textarea width = %d, want >= 1", textWidth)
	}
	// When a line's display width is an exact multiple of textWidth, bubbles
	// appends a trailing padding row. The formula w/textWidth + 1 accounts for
	// this, whereas ceil(w/textWidth) does not.

	// Line with width exactly equal to textWidth should wrap to 2 rows.
	exact := strings.Repeat("x", textWidth)
	m.input.SetValue(exact)
	if got, want := desiredInputRows(m), 2; got != want {
		t.Errorf("desiredInputRows(width=%d, textWidth=%d) = %d, want %d", textWidth, textWidth, got, want)
	}

	// Line with width exactly equal to 2*textWidth should wrap to 3 rows.
	m.input.SetValue(strings.Repeat("x", textWidth*2))
	if got, want := desiredInputRows(m), 3; got != want {
		t.Errorf("desiredInputRows(width=%d, textWidth=%d) = %d, want %d", textWidth*2, textWidth, got, want)
	}
}

func TestApplyInputHeightRecomputesViewportHeight(t *testing.T) {
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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
	m := testModel(t)
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

	m := testModel(t)
	m.input.SetValue("/help")
	m2, _ := m.handleSubmit()
	for _, name := range commandNames() {
		if !strings.Contains(m2.content, name) {
			t.Errorf("/help output missing registered command %q: %q", name, m2.content)
		}
	}
}

// TestSubmitNormalPromptRecordsHistory covers Task 10: submitting a real
// prompt (not a slash command) must land in the prompt-history store so the
// recall provider can surface it later.
func TestSubmitNormalPromptRecordsHistory(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	m := testModel(t)
	m.input.SetValue("write me a haiku about gophers")
	m2, _ := m.handleSubmit()

	entries := m2.hist.All()
	found := false
	for _, e := range entries {
		if e == "write me a haiku about gophers" {
			found = true
		}
	}
	if !found {
		t.Errorf("history = %v, want it to contain the submitted prompt", entries)
	}
}

// TestSubmitSlashCommandDoesNotRecordHistory covers Task 10: slash commands
// are not real prompts and must not pollute recall/markov training data.
func TestSubmitSlashCommandDoesNotRecordHistory(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	m := testModel(t)
	m.input.SetValue("/help")
	m2, _ := m.handleSubmit()

	if entries := m2.hist.All(); len(entries) != 0 {
		t.Errorf("history = %v, want empty after a slash command", entries)
	}
}

// TestNewModelBuildsCompletionWithEmptyHistory covers Task 10: newModel must
// construct cleanly and install all four providers even when there is no
// history on disk yet (a brand-new GOPHERMIND_CONFIG_DIR).
func TestNewModelBuildsCompletionWithEmptyHistory(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	m := testModel(t)

	if m.hist == nil {
		t.Fatal("hist is nil, want a non-nil (possibly empty) store")
	}
	if m.ngram == nil {
		t.Fatal("ngram is nil, want a constructed model")
	}

	// The command provider is wired if Query on a slash-command prefix
	// surfaces a suggestion; this proves SetProviders actually installed
	// providers rather than the model being left with an empty list.
	cm := m.complete.Query("/he", 3)
	if !cm.Active() {
		t.Errorf("complete.Query(%q) not active, want the command provider to suggest /help", "/he")
	}
}

// TestSlashGoalSetsGoal covers Task GOAL: "/goal <text>" sets the
// session-scoped steering goal and confirms in the transcript.
func TestSlashGoalSetsGoal(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("/goal do X")
	m2, _ := m.handleSubmit()
	if m2.goal != "do X" {
		t.Errorf("goal = %q, want %q", m2.goal, "do X")
	}
	if !strings.Contains(m2.content, "goal set: do X") {
		t.Errorf("missing confirmation in transcript: %q", m2.content)
	}
}

// TestSlashGoalShowsCurrentGoal covers Task GOAL: bare "/goal" prints the
// current goal when one is set.
func TestSlashGoalShowsCurrentGoal(t *testing.T) {
	m := testModel(t)
	m.goal = "do X"
	m.input.SetValue("/goal")
	m2, _ := m.handleSubmit()
	if m2.goal != "do X" {
		t.Errorf("goal changed by a bare /goal: %q", m2.goal)
	}
	if !strings.Contains(m2.content, "do X") {
		t.Errorf("missing current goal in transcript: %q", m2.content)
	}
}

// TestSlashGoalShowsNoGoalSet covers Task GOAL: bare "/goal" with none set
// prints "no goal set".
func TestSlashGoalShowsNoGoalSet(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("/goal")
	m2, _ := m.handleSubmit()
	if !strings.Contains(m2.content, "no goal set") {
		t.Errorf("missing 'no goal set' in transcript: %q", m2.content)
	}
}

// TestSlashGoalClearEmptiesGoal covers Task GOAL: "/goal clear" empties the
// goal and confirms in the transcript.
func TestSlashGoalClearEmptiesGoal(t *testing.T) {
	m := testModel(t)
	m.goal = "do X"
	m.input.SetValue("/goal clear")
	m2, _ := m.handleSubmit()
	if m2.goal != "" {
		t.Errorf("goal = %q, want empty after /goal clear", m2.goal)
	}
	if !strings.Contains(m2.content, "goal cleared") {
		t.Errorf("missing confirmation in transcript: %q", m2.content)
	}
}

// TestGoalPreambleContainsGoalAndText covers Task GOAL: the injection helper
// used by handleSubmit's agent-send branch (ag.Send runs in a goroutine, so
// the preamble logic is unit-tested directly here rather than via the send).
func TestGoalPreambleContainsGoalAndText(t *testing.T) {
	got := goalPreamble("do X", "hello")
	if !strings.Contains(got, "do X") {
		t.Errorf("preamble %q missing goal", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("preamble %q missing raw text", got)
	}
}

// TestSlashGoalRegisteredInHelp covers Task GOAL: "/goal" must appear in
// commandNames() / the "/help" output.
func TestSlashGoalRegisteredInHelp(t *testing.T) {
	found := false
	for _, name := range commandNames() {
		if name == "/goal" {
			found = true
		}
	}
	if !found {
		t.Fatal("/goal missing from commandNames()")
	}

	m := testModel(t)
	m.input.SetValue("/help")
	m2, _ := m.handleSubmit()
	if !strings.Contains(m2.content, "/goal") {
		t.Errorf("/help output missing /goal: %q", m2.content)
	}
}

// TestSlashGoalNotRecordedToHistory covers Task GOAL: a "/goal ..." line is a
// slash command, not a real prompt, and must not be recorded to prompt
// history (mirrors TestSubmitSlashCommandDoesNotRecordHistory for Task 10).
func TestSlashGoalNotRecordedToHistory(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("/goal do X")
	m2, _ := m.handleSubmit()

	if entries := m2.hist.All(); len(entries) != 0 {
		t.Errorf("history = %v, want empty after /goal", entries)
	}
}
