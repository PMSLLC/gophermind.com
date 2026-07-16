package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"gophermind/internal/agent"
	"gophermind/internal/llm"
	"gophermind/internal/phaseflow"
	"gophermind/internal/safety"
	"gophermind/internal/tools"
)

// TestProjectExecuteGatedOnApproval verifies "/project-execute" against an
// unapproved plan prints the same gate message as /phase's gated subcommands
// and never enters stateWorking (no run starts).
func TestProjectExecuteGatedOnApproval(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		m := testModel(t)
		m.input.SetValue("/project-execute")
		m2, _ := m.handleSubmit()
		if !strings.Contains(m2.content, "not approved") {
			t.Errorf("expected a 'not approved' gate message, got %q", m2.content)
		}
		if m2.st == stateWorking {
			t.Error("gated /project-execute should not enter stateWorking")
		}
	})
}

// TestProjectExecuteInHelp verifies /project-execute is discoverable via /help.
func TestProjectExecuteInHelp(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("/help")
	m2, _ := m.handleSubmit()
	if !strings.Contains(m2.content, "/project-execute") {
		t.Errorf("help text missing /project-execute: %q", m2.content)
	}
}

// TestProjectExecuteApprovedStartsRun verifies that against an approved plan
// with a pending task, "/project-execute" enters stateWorking and launches the
// run. The context is cancelled immediately after handleSubmit returns so the
// background goroutine's phaseflow.Execute call observes ctx.Err() before
// dispatching the (would-be networked) task run — this test only exercises the
// synchronous gate + launch, not the real agent turn (E2's job).
func TestProjectExecuteApprovedStartsRun(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		e := phaseflow.New(dir)
		a := phaseflow.Assignments{Tasks: []phaseflow.Task{{
			ID:                 "01-01",
			Phase:              "1",
			Title:              "a task",
			Description:        "do a thing",
			AcceptanceCriteria: []string{"it works"},
			Agent:              "coder",
			Model:              "speed",
			Status:             phaseflow.StatusPending,
		}}}
		if err := a.Save(dir); err != nil {
			t.Fatal(err)
		}
		if err := e.Approve(); err != nil {
			t.Fatal(err)
		}

		client := llm.New("http://127.0.0.1:1", "", "m", time.Second, false)
		reg := tools.NewRegistry()
		ag := agent.New(client, reg, 5, safety.Auto, nil)

		m := testModel(t)
		m.agent = ag
		m.input.SetValue("/project-execute")
		m2, _ := m.handleSubmit()

		if m2.st != stateWorking {
			t.Errorf("state = %v, want stateWorking", m2.st)
		}
		if m2.cancel == nil {
			t.Fatal("expected cancel func to be set")
		}
		if !strings.Contains(m2.content, "executing 1 task") {
			t.Errorf("expected a header line announcing the run, got %q", m2.content)
		}
		// Stop the run before it can make a real network call.
		m2.cancel()
	})
}

// TestExecProgressMsgDone verifies that execProgressMsg with a done outcome
// appends the correctly formatted line to the transcript.
func TestExecProgressMsgDone(t *testing.T) {
	m := testModel(t)
	outcome := phaseflow.TaskOutcome{ID: "02-01", Status: phaseflow.StatusDone}
	m2, _ := m.Update(execProgressMsg(outcome))
	mm := m2.(model)

	if !strings.Contains(mm.content, "02-01") {
		t.Errorf("transcript missing task ID: %q", mm.content)
	}
	if !strings.Contains(mm.content, "done") {
		t.Errorf("transcript missing status: %q", mm.content)
	}
	if !strings.Contains(mm.content, "✓") {
		t.Errorf("transcript missing success marker: %q", mm.content)
	}
}

// TestExecProgressMsgCorrected verifies that execProgressMsg with a corrected
// outcome appends the correctly formatted line to the transcript.
func TestExecProgressMsgCorrected(t *testing.T) {
	m := testModel(t)
	outcome := phaseflow.TaskOutcome{ID: "02-02", Status: phaseflow.StatusCorrected}
	m2, _ := m.Update(execProgressMsg(outcome))
	mm := m2.(model)

	if !strings.Contains(mm.content, "02-02") {
		t.Errorf("transcript missing task ID: %q", mm.content)
	}
	if !strings.Contains(mm.content, "corrected") {
		t.Errorf("transcript missing status: %q", mm.content)
	}
	if !strings.Contains(mm.content, "✓") {
		t.Errorf("transcript missing success marker: %q", mm.content)
	}
}

// TestExecProgressMsgFailed verifies that execProgressMsg with a failed outcome
// appends the correctly formatted line including the detail text.
func TestExecProgressMsgFailed(t *testing.T) {
	m := testModel(t)
	outcome := phaseflow.TaskOutcome{
		ID:     "02-03",
		Status: phaseflow.StatusFailed,
		Detail: "network timeout during execution",
	}
	m2, _ := m.Update(execProgressMsg(outcome))
	mm := m2.(model)

	if !strings.Contains(mm.content, "02-03") {
		t.Errorf("transcript missing task ID: %q", mm.content)
	}
	if !strings.Contains(mm.content, "failed") {
		t.Errorf("transcript missing status: %q", mm.content)
	}
	if !strings.Contains(mm.content, "network timeout") {
		t.Errorf("transcript missing detail: %q", mm.content)
	}
	if !strings.Contains(mm.content, "✗") {
		t.Errorf("transcript missing failure marker: %q", mm.content)
	}
}

// TestExecDoneMsgReset verifies that execDoneMsg appends the summary line,
// resets state to idle, and clears the cancel function.
func TestExecDoneMsgReset(t *testing.T) {
	m := testModel(t)
	m.st = stateWorking
	_, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	summary := phaseflow.RunSummary{Done: 2, Corrected: 1, Failed: 1}
	m2, _ := m.Update(execDoneMsg{summary: summary})
	mm := m2.(model)

	if mm.st != stateIdle {
		t.Errorf("state = %v, want idle", mm.st)
	}
	if mm.cancel != nil {
		t.Error("cancel func not cleared")
	}
	if !strings.Contains(mm.content, "run complete") {
		t.Errorf("transcript missing summary: %q", mm.content)
	}
	if !strings.Contains(mm.content, "2 done") {
		t.Errorf("transcript missing done count: %q", mm.content)
	}
	if !strings.Contains(mm.content, "1 corrected") {
		t.Errorf("transcript missing corrected count: %q", mm.content)
	}
	if !strings.Contains(mm.content, "1 failed") {
		t.Errorf("transcript missing failed count: %q", mm.content)
	}
}
