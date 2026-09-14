package phaseflow

import (
	"context"
	"errors"
	"testing"
)

// This file covers wiring the Reviser seam (revise.go) into the run loop
// (pipeline piece 4, task 2, see docs/superpowers/plans
// /2026-09-11-pipeline-piece4.md): revision happens between rounds, a
// revised task re-enters the loop with a fresh attempt count, two failed
// revisions escalate without a further model attempt, a nil Reviser changes
// nothing, and a Reviser error never loses the task.

// scriptedReviser is a Reviser double: it returns a scripted Revision or
// error per call, in order, and records the attempts it was handed each
// time so a test can assert the prompt-carrying contract without a real LLM.
type scriptedReviser struct {
	revisions   []Revision
	errs        []error
	calls       int
	gotAttempts [][]Attempt
}

func (r *scriptedReviser) Revise(_ context.Context, _ Task, attempts []Attempt) (Revision, error) {
	i := r.calls
	r.calls++
	r.gotAttempts = append(r.gotAttempts, attempts)
	var rev Revision
	if i < len(r.revisions) {
		rev = r.revisions[i]
	}
	var err error
	if i < len(r.errs) {
		err = r.errs[i]
	}
	return rev, err
}

// TestReviseThenSucceed: a task that exhausts its models is revised once and
// then succeeds - final status done, one entry in RevisionRounds, and the
// attempts from the successful pass present.
func TestReviseThenSucceed(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	calls := 0
	// A stub TaskRunner that fails the first call (needs_revision) and
	// succeeds on the second (post-revision) call.
	stub := stubStatusRunner(func(callN int, tk Task) (string, string, error) {
		calls++
		if callN == 1 {
			return StatusNeedsRevision, "all candidate models failed: missing trailing newline", nil
		}
		return StatusDone, "ok", nil
	})

	reviser := &scriptedReviser{revisions: []Revision{
		{Note: "clarified the trailing-newline requirement", Deliverable: "revised deliverable"},
	}}

	summary, err := ExecuteWithReviser(context.Background(), root, stub, reviser, nil, 5, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithReviser: %v", err)
	}
	if calls != 2 {
		t.Fatalf("runner calls = %d, want 2 (original + one revised re-run)", calls)
	}
	if reviser.calls != 1 {
		t.Fatalf("reviser calls = %d, want 1", reviser.calls)
	}
	if summary.Done != 1 {
		t.Errorf("summary = %+v, want Done=1", summary)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusDone {
		t.Errorf("status = %q, want %q", tk.Status, StatusDone)
	}
	if len(tk.RevisionRounds) != 1 {
		t.Fatalf("RevisionRounds = %+v, want 1 entry", tk.RevisionRounds)
	}
}

// TestReviseTwiceEscalates: a task that fails revision twice ends escalated,
// with two revisions recorded and no third revised re-run. Total runner
// calls are 1 (the original definition) + MaxRevisionRounds (one re-run per
// applied revision); the call count pins that no fourth (post-cap) call
// happens, mirroring how piece 2 and piece 3 already proved an exhausted or
// escalated task is attempted once rather than once per round.
func TestReviseTwiceEscalates(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	calls := 0
	stub := stubStatusRunner(func(callN int, tk Task) (string, string, error) {
		calls++
		return StatusNeedsRevision, "all candidate models failed: same edge case every time", nil
	})

	reviser := &scriptedReviser{revisions: []Revision{
		{Note: "revision 1", Deliverable: "d1"},
		{Note: "revision 2", Deliverable: "d2"},
	}}

	summary, err := ExecuteWithReviser(context.Background(), root, stub, reviser, nil, 6, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithReviser: %v", err)
	}

	wantCalls := 1 + MaxRevisionRounds
	if calls != wantCalls {
		t.Fatalf("runner calls = %d, want %d (no attempt past the revision cap)", calls, wantCalls)
	}
	if reviser.calls != MaxRevisionRounds {
		t.Fatalf("reviser calls = %d, want %d (no revision attempted past the cap)", reviser.calls, MaxRevisionRounds)
	}
	if summary.Escalated != 1 {
		t.Errorf("summary = %+v, want Escalated=1", summary)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusEscalated {
		t.Errorf("status = %q, want %q", tk.Status, StatusEscalated)
	}
	if len(tk.RevisionRounds) != MaxRevisionRounds {
		t.Fatalf("RevisionRounds = %+v, want %d entries", tk.RevisionRounds, MaxRevisionRounds)
	}
}

// TestNilReviserLeavesNeedsRevisionAlone: a nil Reviser must not panic and
// must leave a needs_revision task exactly as ExecuteWithConcurrency always
// has - existing callers that never revise keep working unchanged.
func TestNilReviserLeavesNeedsRevisionAlone(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	calls := 0
	stub := stubStatusRunner(func(callN int, tk Task) (string, string, error) {
		calls++
		return StatusNeedsRevision, "all candidate models failed", nil
	})

	summary, err := ExecuteWithReviser(context.Background(), root, stub, nil, nil, 3, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithReviser: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want exactly 1 (a nil Reviser must not trigger a re-run)", calls)
	}
	if summary.NeedsRevision != 1 {
		t.Errorf("summary = %+v, want NeedsRevision=1", summary)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusNeedsRevision {
		t.Errorf("status = %q, want unchanged %q", tk.Status, StatusNeedsRevision)
	}
}

// TestReviserErrorLeavesTaskNeedsRevision: a Reviser error must not lose the
// task - it stays needs_revision, ready to be revised again on a later run,
// rather than silently vanishing or being escalated on a mere reviser
// hiccup.
func TestReviserErrorLeavesTaskNeedsRevision(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	calls := 0
	stub := stubStatusRunner(func(callN int, tk Task) (string, string, error) {
		calls++
		return StatusNeedsRevision, "all candidate models failed", nil
	})

	reviser := &scriptedReviser{errs: []error{errors.New("reviser LLM call failed")}}

	summary, err := ExecuteWithReviser(context.Background(), root, stub, reviser, nil, 3, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithReviser: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want exactly 1 (a reviser error must not trigger a re-run)", calls)
	}
	if reviser.calls != 1 {
		t.Fatalf("reviser calls = %d, want 1", reviser.calls)
	}
	if summary.NeedsRevision != 1 {
		t.Errorf("summary = %+v, want NeedsRevision=1", summary)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusNeedsRevision {
		t.Errorf("status = %q, want unchanged %q (task must not be lost on a reviser error)", tk.Status, StatusNeedsRevision)
	}
	if len(tk.RevisionRounds) != 0 {
		t.Errorf("RevisionRounds = %+v, want none applied on a reviser error", tk.RevisionRounds)
	}
}

// stubStatusRunner adapts a plain function to TaskRunner, numbering calls
// from 1 so a test's scripting reads naturally ("the first call", "the
// second call").
func stubStatusRunner(fn func(callN int, t Task) (string, string, error)) *stubStatusRunnerAdapter {
	return &stubStatusRunnerAdapter{fn: fn}
}

type stubStatusRunnerAdapter struct {
	fn    func(callN int, t Task) (string, string, error)
	calls int
}

func (s *stubStatusRunnerAdapter) Run(_ context.Context, t Task) (string, string, error) {
	s.calls++
	return s.fn(s.calls, t)
}
