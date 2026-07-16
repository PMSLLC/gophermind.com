package tui

import (
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
		m := testModel()
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
	m := testModel()
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

		m := testModel()
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
