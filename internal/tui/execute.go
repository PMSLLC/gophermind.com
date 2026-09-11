package tui

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/orchestrate"
	"gophermind/internal/phaseflow"
)

// This file implements the `/project-execute` command: the autonomous per-task
// executor (Spec 2). It runs every pending task in .planning/assignments.json,
// each in a fresh, isolated agent context, streaming per-task progress to the
// transcript as the run proceeds. See
// docs/superpowers/specs/2026-07-16-project-execute-design.md.

// execProgressMsg carries one finished task's outcome from the executor
// goroutine to Update, so it can be appended to the transcript as it happens.
type execProgressMsg phaseflow.TaskOutcome

// execDoneMsg carries the final run summary once every pending task has been
// processed (or the run was cancelled and stopped early).
type execDoneMsg struct{ summary phaseflow.RunSummary }

// handleProjectExecuteCommand dispatches "/project-execute": gated on the plan
// being approved (like /phase plan|execute|verify|milestone), it launches the
// executor on a goroutine and returns immediately with the model set to
// stateWorking; progress streams back via execProgressMsg/execDoneMsg.
func (m model) handleProjectExecuteCommand() (model, tea.Cmd) {
	root, err := os.Getwd()
	if err != nil {
		m.appendLine("project-execute: cannot determine working directory: " + err.Error())
		m.sync()
		return m, nil
	}
	e := phaseflow.New(root)
	if !e.Approved() {
		m.appendLine("⚠ project outline not approved — run /project to finish it first")
		m.sync()
		return m, nil
	}
	if m.agent == nil {
		m.appendLine("project-execute: no active session")
		m.sync()
		return m, nil
	}

	assignments, found, err := phaseflow.LoadAssignments(root)
	if err != nil {
		m.appendLine("project-execute: " + err.Error())
		m.sync()
		return m, nil
	}
	pending := 0
	if found {
		for _, t := range assignments.Tasks {
			if t.Status == phaseflow.StatusPending {
				pending++
			}
		}
	}
	if pending == 0 {
		m.appendLine("project-execute: no pending tasks to run")
		m.sync()
		return m, nil
	}

	// Hand the session's audit log to the executor so an unattended run leaves
	// the same tamper-evident chain an interactive one does. The approval policy
	// is not passed here: this session's approve closure may be the interactive
	// prompt, which has no human behind it during an unattended run. Supplying
	// the composed policy stack with a non-blocking fallback is the harness's
	// job (see WithApproval).
	taskRunner := orchestrate.NewRunner(m.agent.LLM(), m.agent.Registry(), root, m.speedModel, m.model, m.agent.MaxIter(),
		orchestrate.WithAuditLog(m.agent.AuditLog()))

	// Wrap in a FallbackRunner so a task whose first candidate model fails
	// still gets a shot at the next one instead of the whole task failing
	// outright. StatusVerifyingRunner adapts taskRunner (which already does
	// its own verify-and-correct per candidate) to FallbackRunner's
	// separate Inner/Verify steps.
	//
	// DefaultCandidates is given this session's endpoint because every
	// candidate is sent to it: the runner resolves a candidate by cloning
	// the configured client, so a model belonging to some other provider
	// would just 404 and burn one of the task's attempts.
	sv := orchestrate.NewStatusVerifyingRunner(taskRunner)
	runner := &phaseflow.FallbackRunner{
		Inner:      sv,
		Verify:     sv,
		Candidates: orchestrate.DefaultCandidates(m.agent.LLM().BaseURL),
	}

	m.appendLine(projectBannerStyle.Render(fmt.Sprintf("executing %d tasks, auto-approve", pending)))
	m.st = stateWorking
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	go func() {
		emit := func(o phaseflow.TaskOutcome) { sub <- execProgressMsg(o) }
		summary, err := phaseflow.Execute(ctx, root, runner, emit)
		if err != nil {
			sub <- errMsg{err: err}
			return
		}
		if ctx.Err() != nil {
			// Cancelled (Ctrl-C/Esc): reuse the standard cancellation path so it
			// renders the same "⨯ cancelled" line as any other in-flight turn.
			sub <- errMsg{err: ctx.Err()}
			return
		}
		sub <- execDoneMsg{summary: summary}
	}()
	m.sync()
	return m, nil
}

// renderExecOutcome formats one finished task's line for the transcript, e.g.
// "✓ 02-01 done" / "✓ 02-02 corrected" / "✗ 02-03 failed: <detail>".
func renderExecOutcome(o phaseflow.TaskOutcome) string {
	switch o.Status {
	case phaseflow.StatusFailed:
		return "✗ " + o.ID + " failed: " + o.Detail
	case phaseflow.StatusNeedsRevision:
		// Not a success. Every candidate model failed this task, so it is
		// waiting on a revised definition. A checkmark here would read as
		// "fine" on the one outcome that most needs attention.
		return "⚠ " + o.ID + " needs revision: " + o.Detail
	case phaseflow.StatusEscalated:
		return "⚠ " + o.ID + " escalated, needs human input: " + o.Detail
	}
	return "✓ " + o.ID + " " + o.Status
}

// renderExecSummary formats the final run summary line.
func renderExecSummary(s phaseflow.RunSummary) string {
	line := fmt.Sprintf("run complete: %d done, %d corrected, %d failed", s.Done, s.Corrected, s.Failed)
	// Only mentioned when non-zero, so an ordinary run reads exactly as before,
	// but the counts always add up to the tasks actually attempted.
	if s.NeedsRevision > 0 {
		line += fmt.Sprintf(", %d need revision", s.NeedsRevision)
	}
	if s.Escalated > 0 {
		line += fmt.Sprintf(", %d escalated", s.Escalated)
	}
	return line
}
