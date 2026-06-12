package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/agent"
)

func testModel() model {
	m := newModel(func(sub chan tea.Msg, allowed *allowSet) *agent.Agent { return nil }, "m", "auto")
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
