package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// submitsQuit reports whether pressing Enter on the current input quits.
func submitsQuit(t *testing.T, m model) bool {
	t.Helper()
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestExitQuitsFromEveryState is the reported bug: "/exit" was only honored
// while idle, so a user who typed it during a long agent turn — exactly when
// they most want out — got nothing.
func TestExitQuitsFromEveryState(t *testing.T) {
	for _, word := range []string{"/exit", "/quit", "  /exit  "} {
		for _, st := range []struct {
			name string
			st   state
		}{
			{"idle", stateIdle},
			{"working", stateWorking},
			{"approval", stateApproval},
		} {
			m := sizedModel(t, 80, 24)
			m.st = st.st
			if st.st == stateApproval {
				m.pending = approvalMsg{tool: "run_shell", args: "{}", reply: make(chan bool, 1)}
			}
			m.input.SetValue(word)

			if !submitsQuit(t, m) {
				t.Errorf("%q in state %s did not quit", word, st.name)
			}
		}
	}
}

// TestExitMidTurnCancelsTheAgent: quitting must stop the in-flight request, not
// leave the agent goroutine running against a dead UI.
func TestExitMidTurnCancelsTheAgent(t *testing.T) {
	m := sizedModel(t, 80, 24)
	m.st = stateWorking
	cancelled := false
	m.cancel = func() { cancelled = true }
	m.input.SetValue("/exit")

	if !submitsQuit(t, m) {
		t.Fatal("/exit did not quit while working")
	}
	if !cancelled {
		t.Error("/exit quit without cancelling the in-flight turn")
	}
}

// TestExitDuringApprovalReleasesTheWaitingTool: the agent goroutine is parked on
// the approval channel, so quitting must answer it (with a denial) rather than
// abandoning it.
func TestExitDuringApprovalReleasesTheWaitingTool(t *testing.T) {
	m := sizedModel(t, 80, 24)
	m.st = stateApproval
	reply := make(chan bool, 1)
	m.pending = approvalMsg{tool: "run_shell", args: "{}", reply: reply}
	m.input.SetValue("/exit")

	if !submitsQuit(t, m) {
		t.Fatal("/exit did not quit while awaiting approval")
	}
	select {
	case got := <-reply:
		if got {
			t.Error("quitting approved the pending tool; it must deny")
		}
	default:
		t.Error("quitting left the tool waiting on the approval channel")
	}
}

// TestOrdinaryPromptStillSubmits guards the gate above from swallowing normal
// input that merely mentions the word.
func TestOrdinaryPromptStillSubmits(t *testing.T) {
	m := sizedModel(t, 80, 24)
	m.input.SetValue("how do I /exit this thing")
	if submitsQuit(t, m) {
		t.Error("a prompt containing /exit quit the program")
	}
}
