package plan

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

// specified is a tree whose every step carries a specification, which is the
// state the two passes leave behind and the only state approval accepts.
func specified(t *testing.T) *plantree.Repo {
	t.Helper()
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if _, err := RunPass2(context.Background(), r, specFake(), Options2{}); err != nil {
		t.Fatal(err)
	}
	return r
}

func stagesAndStatuses(t *testing.T, r *plantree.Repo) (approved, reviewed, steps int) {
	t.Helper()
	if err := r.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindStep {
			return nil
		}
		steps++
		if n.Planning.Stage == plantree.StageApproved {
			approved++
		}
		if n.Status == plantree.StatusReviewed {
			reviewed++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return approved, reviewed, steps
}

func TestApproveMarksEveryStep(t *testing.T) {
	r := specified(t)
	if err := Approvable(r); err != nil {
		t.Fatalf("a fully specified plan must be approvable: %v", err)
	}
	got, err := Approve(r)
	if err != nil {
		t.Fatal(err)
	}
	approved, reviewed, steps := stagesAndStatuses(t, r)
	if steps == 0 || approved != steps || reviewed != steps {
		t.Errorf("%d of %d steps approved, %d reviewed", approved, steps, reviewed)
	}
	if got.Steps != steps || got.Already != 0 {
		t.Errorf("Approval = %+v, want %d steps and nothing already done", got, steps)
	}
	// The root derives its status from the steps.
	s, err := r.Summarize(plantree.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != plantree.StatusReviewed {
		t.Errorf("the root summarizes as %s, want reviewed", s.Status)
	}
	// And nothing is left to do.
	a, err := NextActions(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Runnable) != 0 || len(a.Blocked) != 0 {
		t.Errorf("after approval NextActions = %+v, want nothing", a)
	}
}

func TestApproveIsIdempotent(t *testing.T) {
	r := specified(t)
	first, err := Approve(r)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Approve(r)
	if err != nil {
		t.Fatalf("approving an approved plan must not fail: %v", err)
	}
	if second.Steps != 0 || second.Already != first.Steps {
		t.Errorf("second Approve = %+v, want nothing new and %d already done", second, first.Steps)
	}
}

// TestApproveAfterAPartialApprovalCompletesIt is the crash case: the process
// died part way through, so some steps are approved and some are not. A
// rerun must finish, not refuse.
func TestApproveAfterAPartialApprovalCompletesIt(t *testing.T) {
	r := specified(t)
	// Simulate a crash after the first step was written.
	first := "phase-001.task-001.step-001"
	cur, err := r.Get(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(first, cur.NodeRevision, func(n *plantree.Node) error {
		n.Planning.Stage = plantree.StageApproved
		n.Status = plantree.StatusReviewed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := Approvable(r); err != nil {
		t.Fatalf("a half-approved plan must still be approvable: %v", err)
	}
	got, err := Approve(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Already != 1 {
		t.Errorf("Approval = %+v, want the one already-approved step counted", got)
	}
	approved, reviewed, steps := stagesAndStatuses(t, r)
	if approved != steps || reviewed != steps {
		t.Errorf("%d of %d approved, %d reviewed after the rerun", approved, steps, reviewed)
	}
}

func TestApproveRefusesWhileAQuestionIsOpen(t *testing.T) {
	r := specified(t)
	if _, err := AddQuestions(r, []NewQuestion{twoOptions()}); err != nil {
		t.Fatal(err)
	}
	err := Approve1Err(t, r)
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "question(s) are still open") {
		t.Errorf("Approve = %v, want a refusal naming the open question", err)
	}
	if approved, _, _ := stagesAndStatuses(t, r); approved != 0 {
		t.Error("a refused approval must write nothing")
	}
}

func TestApproveRefusesWhileAStepWaitsToBeRePlanned(t *testing.T) {
	r, q := answeredRepo(t)
	draft(t, r, "phase-001.task-001.step-001")
	draft(t, r, "phase-001.task-001.step-002")
	if _, _, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	err := Approve1Err(t, r)
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "planned again") {
		t.Errorf("Approve = %v, want a refusal naming the steps waiting to be re-planned", err)
	}
}

func TestApproveRefusesATaskWithNoSteps(t *testing.T) {
	r := specified(t)
	empty, err := newSkeleton("phase-001.task-003", "Empty", "no steps yet", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Create(empty); err != nil {
		t.Fatal(err)
	}
	err = Approve1Err(t, r)
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "task(s) have no steps") ||
		!strings.Contains(err.Error(), "phase-001.task-003") {
		t.Errorf("Approve = %v, want a refusal naming the empty task", err)
	}
}

func TestApproveRefusesAnUnspecifiedStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	} // no pass 2 ran, so every step is still a skeleton
	err := Approve1Err(t, r)
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "draft") {
		t.Errorf("Approve = %v, want a refusal naming the drafting still to do", err)
	}
}

func TestApproveRefusesAHeldStep(t *testing.T) {
	r := specified(t)
	id := "phase-001.task-001.step-001"
	cur, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(id, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusBlocked
		n.Reason = "waiting on an upstream decision"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err = Approve1Err(t, r)
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), id) {
		t.Errorf("Approve = %v, want a refusal naming the held step", err)
	}
}

func TestApproveRefusesAnEmptyPlan(t *testing.T) {
	r := newRepo(t) // a root and nothing else
	err := Approve1Err(t, r)
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "decompose") {
		t.Errorf("Approve = %v, want a refusal naming the decompose action", err)
	}
}

// Approve1Err runs Approve and returns only its error, asserting it wrote
// nothing when it refused.
func Approve1Err(t *testing.T, r *plantree.Repo) error {
	t.Helper()
	got, err := Approve(r)
	if err != nil && (got.Steps != 0 || got.Already != 0) {
		t.Errorf("a refused Approve reported %+v", got)
	}
	return err
}

// setStatus writes a status onto a step.
func setStatus(t *testing.T, r *plantree.Repo, id string, st plantree.Status) {
	t.Helper()
	cur, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(id, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = st
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestApproveRefusesAStepAlreadyExecuting(t *testing.T) {
	for _, st := range []plantree.Status{plantree.StatusInProgress, plantree.StatusCompleted} {
		r := specified(t)
		id := "phase-001.task-001.step-001"
		setStatus(t, r, id, st)
		err := Approve1Err(t, r)
		if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "step(s) already started or finished") ||
			!strings.Contains(err.Error(), id) {
			t.Errorf("status %s: Approve = %v, want a refusal naming the step", st, err)
		}
		if got, _ := r.Get(id); got.Status != st {
			t.Errorf("status %s was rewritten to %s", st, got.Status)
		}
		if approved, _, _ := stagesAndStatuses(t, r); approved != 0 {
			t.Errorf("status %s: a refused approval wrote stages", st)
		}
	}
}

func TestApproveRefusesWhileAnotherProcessHoldsTheRunLock(t *testing.T) {
	r := specified(t)
	other, err := lockfile.TryAcquire(runLockFileFor(t, r))
	if err != nil {
		t.Fatal(err)
	}
	err = Approve1Err(t, r)
	if !errors.Is(err, ErrRunBusy) {
		t.Errorf("Approve = %v, want ErrRunBusy", err)
	}
	if approved, _, _ := stagesAndStatuses(t, r); approved != 0 {
		t.Error("a busy refusal must write nothing")
	}
	other()
	if _, err := Approve(r); err != nil {
		t.Fatalf("Approve after the lock was released: %v", err)
	}
	// It released its own hold.
	again, err := lockfile.TryAcquire(runLockFileFor(t, r))
	if err != nil {
		t.Fatalf("Approve left the run lock held: %v", err)
	}
	again()
}

func TestApproveRefusesABrokenTreeBeforeWriting(t *testing.T) {
	r := specified(t)
	id := "phase-001.task-001.step-002"
	cur, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(id, cur.NodeRevision, func(n *plantree.Node) error {
		n.DependsOn = []string{"phase-009.task-009.step-009"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err = Approve1Err(t, r)
	if err == nil || !strings.Contains(err.Error(), "which does not exist") {
		t.Errorf("Approve = %v, want Verify's refusal", err)
	}
	if approved, _, _ := stagesAndStatuses(t, r); approved != 0 {
		t.Error("a refused approval must write nothing")
	}
}

func TestChangedAnswerAfterApprovalUnApprovesThePlan(t *testing.T) {
	r, q := answeredRepo(t)
	if _, err := Approve(r); err != nil {
		t.Fatal(err)
	}
	if s, _ := r.Summarize(plantree.RootID); s.Status != plantree.StatusReviewed {
		t.Fatalf("root = %s after approval, want reviewed", s.Status)
	}
	if _, _, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	s, err := r.Summarize(plantree.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status == plantree.StatusReviewed || s.Counts.NeedsReconciliation == 0 {
		t.Errorf("root = %s with %d needing reconciliation, want the plan un-approved", s.Status, s.Counts.NeedsReconciliation)
	}
	if err := Approvable(r); !errors.Is(err, ErrNotApprovable) {
		t.Errorf("Approvable = %v, want a refusal until the steps are re-planned", err)
	}
	// The step the answer does not affect stays approved.
	other, _ := r.Get("phase-001.task-002.step-001")
	if other.Planning.Stage != plantree.StageApproved {
		t.Errorf("an unaffected step is %s, want approved", other.Planning.Stage)
	}
}

func TestPartialApprovalThenPass2AndRerun(t *testing.T) {
	r := specified(t)
	first := "phase-001.task-001.step-001"
	cur, _ := r.Get(first)
	if _, err := r.Update(first, cur.NodeRevision, func(n *plantree.Node) error {
		n.Planning.Stage = plantree.StageApproved
		n.Status = plantree.StatusReviewed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := r.Get(first)
	if _, err := RunPass2(context.Background(), r, specFake(), Options2{}); err != nil {
		t.Fatal(err)
	}
	after, _ := r.Get(first)
	if after.Planning.Stage != plantree.StageApproved || after.Status != plantree.StatusReviewed || after.NodeRevision != before.NodeRevision {
		t.Errorf("RunPass2 touched an approved step: %s/%s rev %v -> %v", after.Planning.Stage, after.Status, before.NodeRevision, after.NodeRevision)
	}
	got, err := Approve(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Already != 1 {
		t.Errorf("Approval = %+v, want the one approved step counted", got)
	}
	if approved, reviewed, steps := stagesAndStatuses(t, r); approved != steps || reviewed != steps {
		t.Errorf("%d/%d approved, %d reviewed", approved, steps, reviewed)
	}
}

func TestPartialApprovalThenChangedAnswerFlagsApprovedSteps(t *testing.T) {
	r, q := answeredRepo(t)
	id := "phase-001.task-001.step-001"
	cur, _ := r.Get(id)
	if _, err := r.Update(id, cur.NodeRevision, func(n *plantree.Node) error {
		n.Planning.Stage = plantree.StageApproved
		n.Status = plantree.StatusReviewed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, rec, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Flagged) != 2 {
		t.Errorf("Flagged = %v, want both steps under the task, the approved one included", rec.Flagged)
	}
	got, _ := r.Get(id)
	if got.Planning.Stage != plantree.StageNeedsReconciliation {
		t.Errorf("the approved step is %s, want needs_reconciliation", got.Planning.Stage)
	}
	if err := Approvable(r); !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "planned again") {
		t.Errorf("Approvable = %v, want a refusal until re-planned", err)
	}
}
