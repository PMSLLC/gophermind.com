package phaseflow

import (
	"context"
	"testing"
)

// This file guards the interaction described in
// docs/superpowers/plans/2026-09-11-pipeline-piece2.md: a task that
// exhausted its candidate models is StatusNeedsRevision, not
// StatusFailed, and ExecuteWithRounds' retry logic must leave it alone.
// Retrying it would re-run the same task against the same exhausted model
// list, burning free-tier quota for an identical result.

// TestNeedsRevisionStatusPersistedNotFailed: a runner returning
// StatusNeedsRevision must leave the task needs_revision on disk, not
// silently normalized to failed the way an actually-unknown status is.
func TestNeedsRevisionStatusPersistedNotFailed(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-01": {status: StatusNeedsRevision, detail: "all candidate models failed"},
	}}
	if _, err := Execute(context.Background(), root, runner, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusNeedsRevision {
		t.Errorf("status = %q, want %q", tk.Status, StatusNeedsRevision)
	}
}

// TestResetFailedToPendingLeavesNeedsRevisionAlone: resetFailedToPending
// must requeue a failed task and leave a needs_revision one untouched.
func TestResetFailedToPendingLeavesNeedsRevisionAlone(t *testing.T) {
	root := t.TempDir()
	needsRevision := pendingTask("01-01")
	needsRevision.Status = StatusNeedsRevision
	failed := pendingTask("01-02")
	failed.Status = StatusFailed
	writeAssignments(t, root, needsRevision, failed)

	if err := resetFailedToPending(root); err != nil {
		t.Fatalf("resetFailedToPending: %v", err)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk1, _ := reloaded.Task("01-01")
	if tk1.Status != StatusNeedsRevision {
		t.Errorf("01-01 status = %q, want unchanged %q", tk1.Status, StatusNeedsRevision)
	}
	tk2, _ := reloaded.Task("01-02")
	if tk2.Status != StatusPending {
		t.Errorf("01-02 status = %q, want %q (requeued)", tk2.Status, StatusPending)
	}
}

// TestResetFailedToPendingLeavesEscalatedAlone: same as above for
// StatusEscalated, which also must never be requeued automatically.
func TestResetFailedToPendingLeavesEscalatedAlone(t *testing.T) {
	root := t.TempDir()
	escalated := pendingTask("01-01")
	escalated.Status = StatusEscalated
	writeAssignments(t, root, escalated)

	if err := resetFailedToPending(root); err != nil {
		t.Fatalf("resetFailedToPending: %v", err)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusEscalated {
		t.Errorf("status = %q, want unchanged %q", tk.Status, StatusEscalated)
	}
}

// TestNeedsRevisionAttemptedOnceAcrossRounds is the quota-burning case the
// plan calls out by name: across rounds, a task that exhausted its models
// must be attempted exactly once, not once per round. The assertion is on
// the runner's call count, not just the final status, because a status
// check alone would pass even if the work were silently redone.
func TestNeedsRevisionAttemptedOnceAcrossRounds(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-01": {status: StatusNeedsRevision, detail: "all candidate models failed"},
	}}
	if _, err := ExecuteWithRounds(context.Background(), root, runner, nil, DefaultMaxRounds); err != nil {
		t.Fatalf("ExecuteWithRounds: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v, want exactly 1 (an exhausted task must not be re-run once per round)", runner.calls)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusNeedsRevision {
		t.Errorf("status = %q, want %q", tk.Status, StatusNeedsRevision)
	}
}

// TestEscalatedAttemptedOnceAcrossRounds mirrors the above for an escalated
// task: it too must never be requeued automatically.
func TestEscalatedAttemptedOnceAcrossRounds(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	runner := &fakeRunner{byID: map[string]scriptedResult{
		"01-01": {status: StatusEscalated, detail: "revision cap reached, needs a human"},
	}}
	if _, err := ExecuteWithRounds(context.Background(), root, runner, nil, DefaultMaxRounds); err != nil {
		t.Fatalf("ExecuteWithRounds: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v, want exactly 1", runner.calls)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusEscalated {
		t.Errorf("status = %q, want %q", tk.Status, StatusEscalated)
	}
}
