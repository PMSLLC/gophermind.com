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

	runner := orchestrate.NewRunner(m.agent.LLM(), m.agent.Registry(), root, m.speedModel, m.model, m.agent.MaxIter())

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
	if o.Status == phaseflow.StatusFailed {
		return "✗ " + o.ID + " failed: " + o.Detail
	}
	return "✓ " + o.ID + " " + o.Status
}

// renderExecSummary formats the final run summary line.
func renderExecSummary(s phaseflow.RunSummary) string {
	return fmt.Sprintf("run complete: %d done, %d corrected, %d failed", s.Done, s.Corrected, s.Failed)
}
