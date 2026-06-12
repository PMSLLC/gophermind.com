package tui

import (
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
	m.input.SetValue("/clear")
	m2, cmd := m.handleSubmit()
	if m2.stream != "" {
		t.Errorf("stream not cleared: %q", m2.stream)
	}
	if m2.st != stateIdle {
		t.Errorf("state = %v, want idle", m2.st)
	}
	if cmd == nil {
		t.Error("expected a clear-screen command")
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
