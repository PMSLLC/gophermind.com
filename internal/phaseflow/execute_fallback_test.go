package phaseflow

import (
	"context"
	"errors"
	"testing"
)

// This file covers Execute wired to a FallbackRunner (pipeline piece 2, task
// 3): attempts persisted through Update survive a reload, and a plan that
// predates candidate models still runs against t.Model unchanged.

// TestExecuteWithFallbackPersistsAttemptsInOrder: attempts recorded by a
// FallbackRunner during Execute must survive a reload from disk, in the
// order they were tried, and only the winning attempt's detail is kept as
// the task's outcome.
func TestExecuteWithFallbackPersistsAttemptsInOrder(t *testing.T) {
	root := t.TempDir()
	task := fallbackTask("01-01", "model-a", "model-b")
	writeAssignments(t, root, task)

	inner := &callRunner{byModel: map[string]scriptedResult{
		"model-a": {err: errors.New("model-a unreachable")},
	}}
	fr := &FallbackRunner{
		Inner:  inner,
		Verify: verifyFunc(func(context.Context, Task, string) (bool, string) { return true, "" }),
	}

	summary, err := Execute(context.Background(), root, fr, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if summary.Done != 1 {
		t.Fatalf("summary = %+v, want Done=1", summary)
	}
	if summary.Outcomes[0].Detail != "output:model-b" {
		t.Errorf("outcome detail = %q, want the winning candidate's output only", summary.Outcomes[0].Detail)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, ok := reloaded.Task("01-01")
	if !ok {
		t.Fatal("task 01-01 not found after reload")
	}
	if len(tk.Attempts) != 2 {
		t.Fatalf("Attempts = %+v, want 2 entries surviving the reload", tk.Attempts)
	}
	if tk.Attempts[0].Model != "model-a" || tk.Attempts[0].Verdict != "fail" {
		t.Errorf("Attempts[0] = %+v, want model-a/fail", tk.Attempts[0])
	}
	if tk.Attempts[1].Model != "model-b" || tk.Attempts[1].Verdict != "pass" {
		t.Errorf("Attempts[1] = %+v, want model-b/pass", tk.Attempts[1])
	}
	if tk.Status != StatusDone {
		t.Errorf("status = %q, want %q", tk.Status, StatusDone)
	}
}

// TestExecuteWithFallbackAttemptsSurviveLaterTasksSave: the first task's
// attempts, written through Update, must still be on disk after Execute
// moves on and calls the plain a.Save for a later task in the same pass. If
// the in-memory copy were not kept in sync with what Update wrote, that
// later Save would silently overwrite the first task's attempt history.
func TestExecuteWithFallbackAttemptsSurviveLaterTasksSave(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, fallbackTask("01-01", "model-a"), fallbackTask("01-02", "model-a"))

	inner := &callRunner{byModel: map[string]scriptedResult{}}
	fr := &FallbackRunner{
		Inner:  inner,
		Verify: verifyFunc(func(context.Context, Task, string) (bool, string) { return true, "" }),
	}

	if _, err := Execute(context.Background(), root, fr, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk1, _ := reloaded.Task("01-01")
	if len(tk1.Attempts) != 1 {
		t.Errorf("01-01 Attempts = %+v, want 1 entry surviving 01-02's later Save", tk1.Attempts)
	}
	tk2, _ := reloaded.Task("01-02")
	if len(tk2.Attempts) != 1 {
		t.Errorf("01-02 Attempts = %+v, want 1 entry", tk2.Attempts)
	}
}

// TestExecuteWithFallbackModelOnlyPlanStillRuns: a task written before
// candidate models existed (only Model set) must still execute correctly
// through a FallbackRunner, with no CandidateModels and no Candidates
// override supplied.
func TestExecuteWithFallbackModelOnlyPlanStillRuns(t *testing.T) {
	root := t.TempDir()
	task := pendingTask("01-01")
	task.Model = "strong"
	writeAssignments(t, root, task)

	inner := &callRunner{byModel: map[string]scriptedResult{}}
	fr := &FallbackRunner{
		Inner:  inner,
		Verify: verifyFunc(func(context.Context, Task, string) (bool, string) { return true, "" }),
	}

	summary, err := Execute(context.Background(), root, fr, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if summary.Done != 1 {
		t.Fatalf("summary = %+v, want Done=1", summary)
	}
	if len(inner.models) != 1 || inner.models[0] != "strong" {
		t.Errorf("inner calls = %v, want [strong]", inner.models)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if len(tk.Attempts) != 1 || tk.Attempts[0].Model != "strong" {
		t.Errorf("Attempts = %+v, want a single strong attempt", tk.Attempts)
	}
}

