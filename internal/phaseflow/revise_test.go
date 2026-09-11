package phaseflow

import (
	"context"
	"testing"
	"time"
)

// This file covers the Reviser seam (pipeline piece 4, task 1): ApplyRevision
// as a pure in-memory mutation, NeedsEscalation as a pure decision over
// RevisionRounds, and the pre-existing guarantee piece 4 depends on - that an
// escalated task is never re-run automatically.

// TestApplyRevisionSetsStatusPendingClearsAttemptsAppendsRound: a successful
// revision must reset the task for another pass and record itself as round 1.
func TestApplyRevisionSetsStatusPendingClearsAttemptsAppendsRound(t *testing.T) {
	tk := pendingTask("01-01")
	tk.Status = StatusNeedsRevision
	tk.Attempts = []Attempt{{Model: "model-a", Verdict: "fail", Reason: "missed trailing newline"}}

	rev := Revision{Note: "clarified the trailing-newline requirement", Deliverable: "revised deliverable"}
	if err := ApplyRevision(&tk, rev); err != nil {
		t.Fatalf("ApplyRevision: %v", err)
	}

	if tk.Status != StatusPending {
		t.Errorf("status = %q, want %q", tk.Status, StatusPending)
	}
	if tk.Attempts != nil {
		t.Errorf("Attempts = %+v, want cleared", tk.Attempts)
	}
	if len(tk.RevisionRounds) != 1 {
		t.Fatalf("RevisionRounds = %+v, want 1 entry", tk.RevisionRounds)
	}
	if tk.RevisionRounds[0].Round != 1 {
		t.Errorf("Round = %d, want 1", tk.RevisionRounds[0].Round)
	}
	if tk.RevisionRounds[0].Note != rev.Note {
		t.Errorf("Note = %q, want %q", tk.RevisionRounds[0].Note, rev.Note)
	}
	if tk.RevisionRounds[0].At.IsZero() {
		t.Error("At = zero, want a timestamp")
	}
}

// TestApplyRevisionHistoryAccumulates: a second revision must not lose the
// first - RevisionRounds is the durable record the next revision needs to
// see the pattern in.
func TestApplyRevisionHistoryAccumulates(t *testing.T) {
	tk := pendingTask("01-01")
	tk.Status = StatusNeedsRevision

	first := Revision{Note: "first revision", Deliverable: "d1"}
	if err := ApplyRevision(&tk, first); err != nil {
		t.Fatalf("first ApplyRevision: %v", err)
	}

	tk.Status = StatusNeedsRevision
	tk.Attempts = []Attempt{{Model: "model-b", Verdict: "fail", Reason: "still missing edge case"}}
	second := Revision{Note: "second revision", Deliverable: "d2"}
	if err := ApplyRevision(&tk, second); err != nil {
		t.Fatalf("second ApplyRevision: %v", err)
	}

	if len(tk.RevisionRounds) != 2 {
		t.Fatalf("RevisionRounds = %+v, want 2 entries surviving both revisions", tk.RevisionRounds)
	}
	if tk.RevisionRounds[0].Note != "first revision" || tk.RevisionRounds[0].Round != 1 {
		t.Errorf("RevisionRounds[0] = %+v, want round 1 first revision", tk.RevisionRounds[0])
	}
	if tk.RevisionRounds[1].Note != "second revision" || tk.RevisionRounds[1].Round != 2 {
		t.Errorf("RevisionRounds[1] = %+v, want round 2 second revision", tk.RevisionRounds[1])
	}
	if tk.Attempts != nil {
		t.Errorf("Attempts = %+v, want cleared by the second revision", tk.Attempts)
	}
}

// TestApplyRevisionRejectsEmptyRevision: a revision with neither a
// deliverable nor a test change is a no-op that would consume a round and
// teach nothing, so it must be rejected rather than recorded.
func TestApplyRevisionRejectsEmptyRevision(t *testing.T) {
	tk := pendingTask("01-01")
	tk.Status = StatusNeedsRevision
	before := tk

	empty := Revision{Note: "nothing actually changed"}
	if err := ApplyRevision(&tk, empty); err == nil {
		t.Fatal("ApplyRevision with empty deliverable and test: want an error, got nil")
	}

	if tk.Status != before.Status || len(tk.RevisionRounds) != 0 {
		t.Errorf("task mutated by a rejected revision: status=%q RevisionRounds=%+v", tk.Status, tk.RevisionRounds)
	}
}

// TestApplyRevisionAcceptsTestOnlyChange: a revision that only rewrites the
// test (not the deliverable) is still a real change, not a no-op.
func TestApplyRevisionAcceptsTestOnlyChange(t *testing.T) {
	tk := pendingTask("01-01")
	tk.Status = StatusNeedsRevision

	rev := Revision{Note: "loosened an overly strict acceptance criterion", Test: []string{"output contains a greeting"}}
	if err := ApplyRevision(&tk, rev); err != nil {
		t.Fatalf("ApplyRevision: %v", err)
	}
	if len(tk.RevisionRounds) != 1 {
		t.Fatalf("RevisionRounds = %+v, want 1 entry", tk.RevisionRounds)
	}
}

// TestNeedsEscalationAtCap: a task escalates once it has used every revision
// round MaxRevisionRounds allows, not before.
func TestNeedsEscalationAtCap(t *testing.T) {
	tk := pendingTask("01-01")
	for i := 0; i < MaxRevisionRounds-1; i++ {
		tk.RevisionRounds = append(tk.RevisionRounds, Revision{Round: i + 1, At: time.Now()})
	}
	if NeedsEscalation(tk) {
		t.Errorf("NeedsEscalation = true with %d of %d rounds used, want false", len(tk.RevisionRounds), MaxRevisionRounds)
	}

	tk.RevisionRounds = append(tk.RevisionRounds, Revision{Round: MaxRevisionRounds, At: time.Now()})
	if !NeedsEscalation(tk) {
		t.Errorf("NeedsEscalation = false at %d of %d rounds used, want true", len(tk.RevisionRounds), MaxRevisionRounds)
	}
}

// TestEscalatedTaskConsumesNoFurtherAttempts pins the guarantee piece 4
// depends on: once a task is StatusEscalated (whether set directly, as here,
// or by the run loop hitting NeedsEscalation), it must never be re-run
// automatically. The assertion is on the runner's call count, not just the
// final status, the way piece 2 proved an exhausted task is attempted once
// rather than once per round - a status check alone would pass even if the
// work were silently redone.
func TestEscalatedTaskConsumesNoFurtherAttempts(t *testing.T) {
	root := t.TempDir()
	tk := pendingTask("01-01")
	tk.Status = StatusEscalated
	tk.RevisionRounds = []Revision{
		{Round: 1, At: time.Now(), Note: "first revision"},
		{Round: 2, At: time.Now(), Note: "second revision"},
	}
	writeAssignments(t, root, tk)

	runner := &fakeRunner{byID: map[string]scriptedResult{}}
	if _, err := ExecuteWithRounds(context.Background(), root, runner, nil, DefaultMaxRounds); err != nil {
		t.Fatalf("ExecuteWithRounds: %v", err)
	}

	if len(runner.calls) != 0 {
		t.Fatalf("calls = %v, want none: an escalated task must not be attempted automatically", runner.calls)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.Task("01-01")
	if got.Status != StatusEscalated {
		t.Errorf("status = %q, want unchanged %q", got.Status, StatusEscalated)
	}
	if len(got.RevisionRounds) != 2 {
		t.Errorf("RevisionRounds = %+v, want the 2 entries preserved", got.RevisionRounds)
	}
}
