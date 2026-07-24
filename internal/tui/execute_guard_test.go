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

// stubAgent builds an agent pointed at a dead endpoint; the guard tests never
// let it make a call, they just need m.agent to be non-nil.
func stubAgent(t *testing.T) *agent.Agent {
	t.Helper()
	client := llm.New("http://127.0.0.1:1", "", "m", time.Second, false)
	return agent.New(client, tools.NewRegistry(), 5, safety.Auto, nil)
}

// TestProjectExecuteRefusesUnapprovedProject: the executor must not run a plan
// the user has not approved, even if tasks exist.
func TestProjectExecuteRefusesUnapprovedProject(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := testModel(t)
	m2, cmd := m.handleProjectExecuteCommand()

	if !strings.Contains(m2.content, "not approved") {
		t.Errorf("expected an 'not approved' guard message:\n%s", m2.content)
	}
	if cmd != nil {
		t.Error("no execution command should be issued for an unapproved project")
	}
}

// TestProjectExecuteWithNoPendingTasks: an approved plan with nothing pending
// reports that rather than spinning up a runner.
func TestProjectExecuteWithNoPendingTasks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Approve the project and write an all-done plan.
	e := phaseflow.New(dir)
	if err := e.Approve(); err != nil {
		t.Fatal(err)
	}
	a := &phaseflow.Assignments{Tasks: []phaseflow.Task{
		{ID: "01-01", Phase: "1", Title: "t", AcceptanceCriteria: []string{"c"},
			Agent: "a", Model: "speed", Status: phaseflow.StatusDone},
	}}
	if err := a.Save(dir); err != nil {
		t.Fatal(err)
	}

	m := testModel(t)
	m.agent = stubAgent(t)
	m2, cmd := m.handleProjectExecuteCommand()

	if !strings.Contains(m2.content, "no pending tasks") {
		t.Errorf("expected a 'no pending tasks' message:\n%s", m2.content)
	}
	if cmd != nil {
		t.Error("no execution command should be issued when nothing is pending")
	}
}
