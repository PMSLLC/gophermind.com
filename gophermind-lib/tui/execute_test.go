package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/agent"
	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/orchestrate"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/safety"
	"gophermind/gophermind-lib/tools"
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

// A task that exhausted every candidate model must not render as a success.
// It rendered as "✓ id needs_revision" before, which reads as fine on the one
// outcome that most needs attention.
func TestRenderExecOutcomeDoesNotTickNeedsRevision(t *testing.T) {
	got := renderExecOutcome(phaseflow.TaskOutcome{
		ID: "subs-endpoint", Status: phaseflow.StatusNeedsRevision, Detail: "3 models failed",
	})
	if strings.Contains(got, "✓") {
		t.Errorf("needs_revision rendered with a success tick: %q", got)
	}
	if !strings.Contains(got, "needs revision") {
		t.Errorf("needs_revision does not say so: %q", got)
	}
}

// The summary must account for every task attempted, not silently drop the
// ones waiting on a revision.
func TestRenderExecSummaryCountsNeedsRevision(t *testing.T) {
	got := renderExecSummary(phaseflow.RunSummary{Done: 2, NeedsRevision: 1})
	if !strings.Contains(got, "1 need revision") {
		t.Errorf("summary omits needs_revision: %q", got)
	}
	// An ordinary run must read exactly as it did before.
	plain := renderExecSummary(phaseflow.RunSummary{Done: 3, Corrected: 1})
	if strings.Contains(plain, "need revision") || strings.Contains(plain, "escalated") {
		t.Errorf("summary added noise to an ordinary run: %q", plain)
	}
}

// A contract flag is the single most important outcome to surface: later work
// would be building against a contract already known to be wrong. It must not
// render with a success tick, the same bug 2cf599e fixed for needs_revision.
func TestRenderExecOutcomeDoesNotTickContractFlagged(t *testing.T) {
	got := renderExecOutcome(phaseflow.TaskOutcome{
		ID: "subs-endpoint", Status: phaseflow.StatusContractFlagged, Detail: "contract is missing field X",
	})
	if strings.Contains(got, "✓") {
		t.Errorf("contract_flagged rendered with a success tick: %q", got)
	}
	if !strings.Contains(got, "contract") {
		t.Errorf("contract_flagged does not say so: %q", got)
	}
	if !strings.Contains(got, "contract is missing field X") {
		t.Errorf("contract_flagged drops the flag's reason: %q", got)
	}
}

// The summary must account for a contract flag too, not silently drop the
// outcome that stopped the run.
func TestRenderExecSummaryCountsContractFlagged(t *testing.T) {
	got := renderExecSummary(phaseflow.RunSummary{Done: 2, ContractFlagged: 1})
	if !strings.Contains(got, "contract") {
		t.Errorf("summary omits contract_flagged: %q", got)
	}
	// An ordinary run must read exactly as it did before.
	plain := renderExecSummary(phaseflow.RunSummary{Done: 3, Corrected: 1})
	if strings.Contains(plain, "contract") {
		t.Errorf("summary added noise to an ordinary run: %q", plain)
	}
}

// TestRenderExecEventToolCallShowsTaskAndName is the deferred follow-up from
// feat/project-execute (#3): a task's tool activity was invisible until it
// finished. renderExecEvent must prefix the interactive session's own
// tool-call rendering with the task ID, so a wave's concurrent tasks are
// distinguishable in the shared transcript.
func TestRenderExecEventToolCallShowsTaskAndName(t *testing.T) {
	got := renderExecEvent(orchestrate.TaskEvent{
		TaskID: "02-01",
		Event:  agent.Event{Type: "tool_call", Name: "read_file", Text: `{"path":"x.txt"}`},
	})
	if !strings.Contains(got, "02-01") {
		t.Errorf("missing task ID: %q", got)
	}
	if !strings.Contains(got, "read_file") {
		t.Errorf("missing tool name: %q", got)
	}
}

// TestRenderExecEventToolResultShowsTask verifies the result side is also
// tagged with its task.
func TestRenderExecEventToolResultShowsTask(t *testing.T) {
	got := renderExecEvent(orchestrate.TaskEvent{
		TaskID: "02-02",
		Event:  agent.Event{Type: "tool_result", Text: "file contents"},
	})
	if !strings.Contains(got, "02-02") {
		t.Errorf("missing task ID: %q", got)
	}
	if !strings.Contains(got, "file contents") {
		t.Errorf("missing result text: %q", got)
	}
}

// TestExecEventMsgAppendsToTranscript verifies Update wires execEventMsg
// through to the transcript rather than silently discarding it, which is
// exactly what happened before this feature existed (Runner.newTaskAgent's
// onEvent was hardcoded nil).
func TestExecEventMsgAppendsToTranscript(t *testing.T) {
	m := testModel(t)
	m2, _ := m.Update(execEventMsg(orchestrate.TaskEvent{
		TaskID: "03-01",
		Event:  agent.Event{Type: "tool_call", Name: "run_shell", Text: `{"cmd":"go test"}`},
	}))
	mm := m2.(model)

	if !strings.Contains(mm.content, "03-01") {
		t.Errorf("transcript missing task ID: %q", mm.content)
	}
	if !strings.Contains(mm.content, "run_shell") {
		t.Errorf("transcript missing tool name: %q", mm.content)
	}
}

// TestExecProgressMsgAccumulatesOutcomes verifies each execProgressMsg is
// also recorded on m.execOutcomes, not just appended to the transcript --
// this is the record a Ctrl-C cancel tallies into a partial summary.
func TestExecProgressMsgAccumulatesOutcomes(t *testing.T) {
	m := testModel(t)
	m2, _ := m.Update(execProgressMsg(phaseflow.TaskOutcome{ID: "01-01", Status: phaseflow.StatusDone}))
	m3, _ := m2.(model).Update(execProgressMsg(phaseflow.TaskOutcome{ID: "01-02", Status: phaseflow.StatusFailed}))
	mm := m3.(model)

	if len(mm.execOutcomes) != 2 {
		t.Fatalf("execOutcomes has %d entries, want 2", len(mm.execOutcomes))
	}
	if mm.execOutcomes[0].ID != "01-01" || mm.execOutcomes[1].ID != "01-02" {
		t.Errorf("execOutcomes = %+v, want them in arrival order", mm.execOutcomes)
	}
}

// TestExecDoneMsgClearsOutcomes verifies a completed run's outcome log is
// cleared once its authoritative summary has been shown, so it cannot leak
// into a later, unrelated cancel's partial summary.
func TestExecDoneMsgClearsOutcomes(t *testing.T) {
	m := testModel(t)
	m.execOutcomes = []phaseflow.TaskOutcome{{ID: "01-01", Status: phaseflow.StatusDone}}
	m2, _ := m.Update(execDoneMsg{summary: phaseflow.RunSummary{Done: 1}})
	mm := m2.(model)

	if mm.execOutcomes != nil {
		t.Errorf("execOutcomes = %+v, want nil after execDoneMsg", mm.execOutcomes)
	}
}

// TestCancelledExecutorRunShowsPartialSummary is the deferred follow-up from
// feat/project-execute (#7): Ctrl-C mid-run showed only "cancelled", with no
// tally of what had already finished. It must now report the same counts
// renderExecSummary would, using whatever outcomes arrived before the cancel.
func TestCancelledExecutorRunShowsPartialSummary(t *testing.T) {
	m := testModel(t)
	m.st = stateWorking
	m.execOutcomes = []phaseflow.TaskOutcome{
		{ID: "01-01", Status: phaseflow.StatusDone},
		{ID: "01-02", Status: phaseflow.StatusFailed, Detail: "boom"},
	}
	_, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m2, _ := m.Update(errMsg{err: context.Canceled})
	mm := m2.(model)

	if !strings.Contains(mm.content, "cancelled") {
		t.Errorf("transcript missing cancelled indication: %q", mm.content)
	}
	if !strings.Contains(mm.content, "1 done") || !strings.Contains(mm.content, "1 failed") {
		t.Errorf("transcript missing partial tally: %q", mm.content)
	}
	if mm.execOutcomes != nil {
		t.Errorf("execOutcomes = %+v, want cleared after the cancel is reported", mm.execOutcomes)
	}
}

// TestCancelledPlainTurnShowsNoTally guards the ordinary (non-executor) cancel
// path: with no execOutcomes recorded, the cancel line must read exactly as
// it did before this feature existed, with no stray tally text.
func TestCancelledPlainTurnShowsNoTally(t *testing.T) {
	m := testModel(t)
	m.st = stateWorking
	_, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m2, _ := m.Update(errMsg{err: context.Canceled})
	mm := m2.(model)

	if !strings.Contains(mm.content, "cancelled") {
		t.Errorf("transcript missing cancelled indication: %q", mm.content)
	}
	if strings.Contains(mm.content, "done") || strings.Contains(mm.content, "failed") {
		t.Errorf("plain cancel should not show a task tally: %q", mm.content)
	}
}
