package phaseflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/internal/codeindex"
)

// This file implements the autonomous per-task executor for `/project-execute`
// (Spec 2): pure orchestration over the plan produced by `/project` and
// persisted in assignments.json. It never calls an LLM itself — a TaskRunner
// (e.g. an agent dispatcher) does the actual work; Execute just sequences
// pending tasks, persists status transitions as it goes, and tallies results.

// TaskOutcome records the terminal result of one finished task.
type TaskOutcome struct {
	ID     string
	Status string
	Detail string
}

// RunSummary tallies the outcome of an Execute run.
type RunSummary struct {
	Done      int
	Corrected int
	Failed    int
	// NeedsRevision and Escalated are counted separately rather than folded
	// into Failed. A task that exhausted its candidate models has not failed
	// in the ordinary sense, it is waiting on a revised definition, and a
	// summary that omitted them would not add up to the number of tasks run.
	NeedsRevision int
	Escalated     int
	Outcomes      []TaskOutcome
}

// TaskRunner executes a single task and reports its terminal status. Status
// must be one of StatusDone/StatusCorrected/StatusFailed; any other value is
// treated as StatusFailed by Execute. A non-nil err that wraps
// context.Canceled or context.DeadlineExceeded signals the caller wants the
// run stopped rather than the task marked failed.
type TaskRunner interface {
	Run(ctx context.Context, t Task) (status string, detail string, err error)
}

// Execute runs all pending tasks in assignments.json in ascending ID order,
// persisting each task's status transition to disk as it happens so a killed
// or interrupted run leaves an accurate on-disk record. Non-pending tasks
// (already done/failed/corrected from a prior run) are left untouched.
//
// A runner error marks that task failed and execution continues with the
// next task. If ctx is canceled (either observed via ctx.Err() before a task
// runs, or reported by the runner wrapping context.Canceled/
// DeadlineExceeded), the in-flight task is reverted to pending and the loop
// stops immediately, leaving later tasks pending for a future run.
func Execute(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome)) (RunSummary, error) {
	return ExecuteWithRounds(ctx, root, runner, emit, DefaultMaxRounds)
}

// DefaultMaxRounds bounds how many times ExecuteWithRounds re-attempts the
// tasks that failed, so an unattended run finishes rather than spinning.
const DefaultMaxRounds = 3

// ExecuteWithRounds runs passes over the plan until every task is accounted
// for, retrying failures with their failure detail fed back to the runner. It
// stops at the first of: no failures left, a round that fixed nothing (further
// retries would only repeat themselves), or maxRounds.
//
// The failure note is handed to the runner on the retry attempt but never
// persisted, so assignments.json keeps the plan the user approved.
func ExecuteWithRounds(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), maxRounds int) (RunSummary, error) {
	if maxRounds < 1 {
		maxRounds = 1
	}
	var summary RunSummary
	lastDetail := map[string]string{}
	// Only tasks this run actually attempted are tallied; work left done by an
	// earlier run must not inflate this run's summary.
	touched := map[string]bool{}

	for round := 0; round < maxRounds; round++ {
		passSummary, ran, err := executePass(ctx, root, runner, emit, lastDetail)
		summary.Outcomes = append(summary.Outcomes, passSummary.Outcomes...)
		for _, o := range passSummary.Outcomes {
			touched[o.ID] = true
		}
		if err != nil {
			return summary, err
		}
		if !ran || ctx.Err() != nil {
			break
		}

		progressed := passSummary.Done + passSummary.Corrected
		if passSummary.Failed == 0 || progressed == 0 || round == maxRounds-1 {
			break
		}
		// Another round is coming: return the failures to pending so the next
		// pass picks them up.
		if err := resetFailedToPending(root); err != nil {
			return summary, err
		}
	}

	// Counts reflect the plan's final state, not the sum of every attempt, so a
	// task that failed once and then succeeded counts once, as done.
	final, _, err := LoadAssignments(root)
	if err != nil {
		return summary, err
	}
	for _, t := range final.Tasks {
		if !touched[t.ID] {
			continue
		}
		switch t.Status {
		case StatusDone:
			summary.Done++
		case StatusCorrected:
			summary.Corrected++
		case StatusFailed:
			summary.Failed++
		case StatusNeedsRevision:
			summary.NeedsRevision++
		case StatusEscalated:
			summary.Escalated++
		}
	}
	return summary, nil
}

// resetFailedToPending requeues failed tasks for the next retry round. It
// matches StatusFailed exactly and so, deliberately, leaves
// StatusNeedsRevision and StatusEscalated tasks alone: those are not
// ordinary failures, and requeuing one would re-run it against the same
// exhausted candidate model list for an identical result. See
// normalizeStatus, which is what keeps such a task from ever becoming
// StatusFailed in the first place.
func resetFailedToPending(root string) error {
	a, _, err := LoadAssignments(root)
	if err != nil {
		return err
	}
	for i, t := range a.Tasks {
		if t.Status == StatusFailed {
			a.Tasks[i].Status = StatusPending
		}
	}
	return a.Save(root)
}

// executePass runs every currently-pending task once. It reports whether any
// task ran, so the caller can stop when the plan is exhausted.
func executePass(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), lastDetail map[string]string) (RunSummary, bool, error) {
	summary, err := executeOnce(ctx, root, runner, emit, lastDetail)
	return summary, len(summary.Outcomes) > 0, err
}

func executeOnce(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), lastDetail map[string]string) (RunSummary, error) {
	a, found, err := LoadAssignments(root)
	if err != nil {
		return RunSummary{}, err
	}
	if !found {
		return RunSummary{}, fmt.Errorf("phaseflow: no assignments found at %s", AssignmentsPath(root))
	}

	recovered := false
	for i, t := range a.Tasks {
		if t.Status == StatusRunning {
			a.Tasks[i].Status = StatusPending
			recovered = true
		}
	}
	if recovered {
		if err := a.Save(root); err != nil {
			return RunSummary{}, err
		}
	}

	pending := make([]int, 0, len(a.Tasks))
	for i, t := range a.Tasks {
		if t.Status == StatusPending {
			pending = append(pending, i)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return a.Tasks[pending[i]].ID < a.Tasks[pending[j]].ID
	})

	var summary RunSummary
	for _, idx := range pending {
		if ctx.Err() != nil {
			return summary, nil
		}

		a.Tasks[idx].Status = StatusRunning
		if err := a.Save(root); err != nil {
			return summary, err
		}

		// On a retry round, hand the runner the same task plus what went wrong
		// last time. The copy is deliberate: the note must not reach disk.
		attempt := a.Tasks[idx]
		if note := lastDetail[attempt.ID]; note != "" {
			attempt.AgentAddendum = strings.TrimSpace(attempt.AgentAddendum +
				"\n\nA previous attempt at this task failed with:\n" + note +
				"\nFix that before proceeding.")
		}

		status, detail, runErr := runner.Run(ctx, attempt)

		// A runner that tracks per-model attempts (FallbackRunner) exposes
		// them here so they can be persisted, even on a round that ends in
		// cancellation partway through a candidate list. Attempts must
		// accumulate rather than overwrite, so this goes through Update, not
		// a.Save - and the in-memory copy is updated to match so a later
		// a.Save in this loop (for the next task) does not write over it.
		if fr, ok := runner.(*FallbackRunner); ok {
			if err := recordAttempts(root, &a, idx, fr.LastAttempts); err != nil {
				return summary, err
			}
		}

		if ctx.Err() != nil || isCancel(runErr) {
			a.Tasks[idx].Status = StatusPending
			if err := a.Save(root); err != nil {
				return summary, err
			}
			return summary, nil
		}

		if runErr != nil {
			a.Tasks[idx].Status = StatusFailed
			detail = runErr.Error()
		} else {
			a.Tasks[idx].Status = normalizeStatus(status)
		}
		if err := a.Save(root); err != nil {
			return summary, err
		}

		if a.Tasks[idx].Status == StatusFailed {
			lastDetail[a.Tasks[idx].ID] = detail
		} else {
			delete(lastDetail, a.Tasks[idx].ID)
		}

		// Refresh the symbol index so the next task searches the tree as this
		// one left it. Best-effort by design: the index is a convenience, and
		// failing a completed task because a Markdown file could not be written
		// would be worse than an index that is one task stale.
		_, _ = codeindex.BuildAndWrite(root)

		outcome := TaskOutcome{ID: a.Tasks[idx].ID, Status: a.Tasks[idx].Status, Detail: detail}

		// Record run state before the next task starts. The next task runs in a
		// cleared context, so this file is how it learns what already happened.
		// Best-effort for the same reason as the index.
		_ = UpsertContextDoc(root, RenderContextDocBody(projectNameFor(root), &a, outcome))
		summary.Outcomes = append(summary.Outcomes, outcome)
		switch outcome.Status {
		case StatusDone:
			summary.Done++
		case StatusCorrected:
			summary.Corrected++
		case StatusFailed:
			summary.Failed++
		case StatusNeedsRevision:
			summary.NeedsRevision++
		case StatusEscalated:
			summary.Escalated++
		}
		if emit != nil {
			emit(outcome)
		}
	}

	return summary, nil
}

// isCancel reports whether err represents a run-stopping cancellation rather
// than an ordinary task failure.
func isCancel(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

// recordAttempts appends newAttempts to a.Tasks[idx] both on disk, through
// the locked Update primitive so accumulation is safe once more than one
// task can be running at a time, and in the in-memory copy executeOnce keeps
// saving, so a later plain Save in this pass (for a different task) does
// not clobber what Update just wrote. A no-op when there is nothing to
// record.
func recordAttempts(root string, a *Assignments, idx int, newAttempts []Attempt) error {
	if len(newAttempts) == 0 {
		return nil
	}
	id := a.Tasks[idx].ID
	for _, at := range newAttempts {
		a.Tasks[idx].RecordAttempt(at)
	}
	return Update(root, func(as *Assignments) error {
		for i := range as.Tasks {
			if as.Tasks[i].ID == id {
				for _, at := range newAttempts {
					as.Tasks[i].RecordAttempt(at)
				}
				return nil
			}
		}
		return fmt.Errorf("phaseflow: task %q not found while recording attempts", id)
	})
}

// normalizeStatus treats any status other than done/corrected/needs_revision/
// escalated as failed. StatusNeedsRevision and StatusEscalated must pass
// through unchanged: a task in either state already ran its candidate models
// to exhaustion (see FallbackRunner), and folding it into StatusFailed here
// would make resetFailedToPending requeue it for an identical, quota-burning
// re-run.
func normalizeStatus(status string) string {
	switch status {
	case StatusDone, StatusCorrected, StatusNeedsRevision, StatusEscalated:
		return status
	default:
		return StatusFailed
	}
}

// projectNameFor labels the context doc with the project directory's name, so
// the doc is never anonymous.
func projectNameFor(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		return filepath.Base(abs)
	}
	return filepath.Base(root)
}
