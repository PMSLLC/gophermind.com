package phaseflow

import (
	"context"
	"errors"
	"testing"
)

// scriptedResult is what a fakeRunner returns for a given task id.
type scriptedResult struct {
	status string
	detail string
	err    error
}

// fakeRunner is a TaskRunner double: it records call order and returns a
// scripted result per task id. If a task id has no script entry it returns
// StatusDone with no error (a permissive default so tests only need to
// script the ids they care about).
type fakeRunner struct {
	script []scriptedResult
	byID   map[string]scriptedResult
	calls  []string
}

func (f *fakeRunner) Run(_ context.Context, t Task) (string, string, error) {
	f.calls = append(f.calls, t.ID)
	if r, ok := f.byID[t.ID]; ok {
		return r.status, r.detail, r.err
	}
	return StatusDone, "", nil
}

func writeAssignments(t *testing.T, root string, tasks ...Task) {
	t.Helper()
	if err := (Assignments{Tasks: tasks}).Save(root); err != nil {
		t.Fatalf("save assignments: %v", err)
	}
}

func pendingTask(id string) Task {
	return Task{ID: id, Phase: "1", Title: "T " + id, Agent: "coder", Model: "strong", Status: StatusPending}
}

func TestExecuteAscendingIDOrder(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("02-01"), pendingTask("01-02"), pendingTask("01-01"), pendingTask("10-01"))

	runner := &fakeRunner{byID: map[string]scriptedResult{}}
	if _, err := Execute(context.Background(), root, runner, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := []string{"01-01", "01-02", "02-01", "10-01"}
	if len(runner.calls) != len(want) {
		t.Fatalf("call order = %v, want %v", runner.calls, want)
	}
	for i, id := range want {
		if runner.calls[i] != id {
			t.Errorf("call order[%d] = %q, want %q (full: %v)", i, runner.calls[i], id, runner.calls)
		}
	}
}

func TestExecuteStatusPersistedAfterEachTask(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-01": {status: StatusDone},
		"01-02": {status: StatusCorrected},
	}}
	summary, err := Execute(context.Background(), root, runner, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if summary.Done != 1 || summary.Corrected != 1 || summary.Failed != 0 {
		t.Errorf("summary = %+v, want Done=1 Corrected=1 Failed=0", summary)
	}

	reloaded, found, err := LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("reload: err=%v found=%v", err, found)
	}
	tk1, _ := reloaded.Task("01-01")
	if tk1.Status != StatusDone {
		t.Errorf("01-01 status = %q, want %q", tk1.Status, StatusDone)
	}
	tk2, _ := reloaded.Task("01-02")
	if tk2.Status != StatusCorrected {
		t.Errorf("01-02 status = %q, want %q", tk2.Status, StatusCorrected)
	}
}

func TestExecuteRunnerErrorMarksFailedAndContinues(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"), pendingTask("01-03"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-02": {err: errors.New("boom")},
	}}
	summary, err := Execute(context.Background(), root, runner, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// All three tasks must have been attempted (continue past the failure).
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %v, want 3 tasks attempted", runner.calls)
	}

	reloaded, _, _ := LoadAssignments(root)
	tk2, _ := reloaded.Task("01-02")
	if tk2.Status != StatusFailed {
		t.Errorf("01-02 status = %q, want %q", tk2.Status, StatusFailed)
	}
	tk3, _ := reloaded.Task("01-03")
	if tk3.Status != StatusDone {
		t.Errorf("01-03 status = %q, want %q (should still have run)", tk3.Status, StatusDone)
	}
	if summary.Done != 2 || summary.Failed != 1 {
		t.Errorf("summary = %+v, want Done=2 Failed=1", summary)
	}
}

func TestExecuteUnknownStatusTreatedAsFailed(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-01": {status: "bogus-status"},
	}}
	summary, err := Execute(context.Background(), root, runner, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if summary.Failed != 1 || summary.Done != 0 {
		t.Errorf("summary = %+v, want Failed=1", summary)
	}
	reloaded, _, _ := LoadAssignments(root)
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusFailed {
		t.Errorf("status = %q, want %q", tk.Status, StatusFailed)
	}
}

func TestExecuteCtxCancelStopsAndRevertsRunningTask(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"), pendingTask("01-03"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-01": {status: StatusDone},
		"01-02": {err: context.Canceled},
	}}
	summary, err := Execute(context.Background(), root, runner, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The loop must stop after the cancel: 01-03's runner call never happens.
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %v, want exactly [01-01 01-02]", runner.calls)
	}

	reloaded, _, _ := LoadAssignments(root)
	tk1, _ := reloaded.Task("01-01")
	if tk1.Status != StatusDone {
		t.Errorf("01-01 status = %q, want done (earlier done tasks stay done)", tk1.Status)
	}
	tk2, _ := reloaded.Task("01-02")
	if tk2.Status != StatusPending {
		t.Errorf("01-02 status = %q, want pending (reverted after cancel)", tk2.Status)
	}
	tk3, _ := reloaded.Task("01-03")
	if tk3.Status != StatusPending {
		t.Errorf("01-03 status = %q, want pending (untouched)", tk3.Status)
	}
	if summary.Done != 1 || summary.Failed != 0 || summary.Corrected != 0 {
		t.Errorf("summary = %+v, want only Done=1 counted (cancelled task not tallied)", summary)
	}
}

func TestExecuteCtxCanceledBeforeRun(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &fakeRunner{byID: map[string]scriptedResult{}}
	summary, err := Execute(ctx, root, runner, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %v, want none: ctx already canceled before first run", runner.calls)
	}
	reloaded, _, _ := LoadAssignments(root)
	tk1, _ := reloaded.Task("01-01")
	if tk1.Status != StatusPending {
		t.Errorf("01-01 status = %q, want pending", tk1.Status)
	}
	if summary.Done != 0 && summary.Failed != 0 {
		t.Errorf("summary = %+v, want no tasks tallied", summary)
	}
}

func TestExecuteSkipsAlreadyDoneTasks(t *testing.T) {
	root := t.TempDir()
	done := pendingTask("01-01")
	done.Status = StatusDone
	failed := pendingTask("01-02")
	failed.Status = StatusFailed
	writeAssignments(t, root, done, failed, pendingTask("01-03"))

	runner := &fakeRunner{byID: map[string]scriptedResult{}}
	summary, err := Execute(context.Background(), root, runner, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(runner.calls) != 1 || runner.calls[0] != "01-03" {
		t.Errorf("calls = %v, want only [01-03]; done/failed tasks must be skipped", runner.calls)
	}
	if summary.Done != 1 {
		t.Errorf("summary = %+v, want Done=1 (only the pending task tallied)", summary)
	}

	reloaded, _, _ := LoadAssignments(root)
	tk1, _ := reloaded.Task("01-01")
	if tk1.Status != StatusDone {
		t.Errorf("01-01 status = %q, want unchanged done", tk1.Status)
	}
	tk2, _ := reloaded.Task("01-02")
	if tk2.Status != StatusFailed {
		t.Errorf("01-02 status = %q, want unchanged failed", tk2.Status)
	}
}

func TestExecuteEmitsOutcomePerFinishedTask(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-02": {err: errors.New("boom")},
	}}
	var emitted []TaskOutcome
	summary, err := Execute(context.Background(), root, runner, func(o TaskOutcome) {
		emitted = append(emitted, o)
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(emitted) != 2 {
		t.Fatalf("emitted = %+v, want 2 outcomes", emitted)
	}
	if emitted[0].ID != "01-01" || emitted[0].Status != StatusDone {
		t.Errorf("emitted[0] = %+v, want ID=01-01 Status=done", emitted[0])
	}
	if emitted[1].ID != "01-02" || emitted[1].Status != StatusFailed {
		t.Errorf("emitted[1] = %+v, want ID=01-02 Status=failed", emitted[1])
	}
	if len(summary.Outcomes) != 2 {
		t.Errorf("summary.Outcomes = %+v, want 2 entries", summary.Outcomes)
	}
}

func TestExecuteNilEmitDoesNotPanic(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))
	runner := &fakeRunner{byID: map[string]scriptedResult{}}
	if _, err := Execute(context.Background(), root, runner, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestExecuteMissingAssignmentsReturnsError(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{byID: map[string]scriptedResult{}}
	if _, err := Execute(context.Background(), root, runner, nil); err == nil {
		t.Error("expected an error when .planning/assignments.json does not exist")
	}
}
