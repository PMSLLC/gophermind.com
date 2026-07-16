package phaseflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	Outcomes  []TaskOutcome
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

		status, detail, runErr := runner.Run(ctx, a.Tasks[idx])

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

		outcome := TaskOutcome{ID: a.Tasks[idx].ID, Status: a.Tasks[idx].Status, Detail: detail}
		summary.Outcomes = append(summary.Outcomes, outcome)
		switch outcome.Status {
		case StatusDone:
			summary.Done++
		case StatusCorrected:
			summary.Corrected++
		case StatusFailed:
			summary.Failed++
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

// normalizeStatus treats any status other than done/corrected as failed.
func normalizeStatus(status string) string {
	switch status {
	case StatusDone, StatusCorrected:
		return status
	default:
		return StatusFailed
	}
}
