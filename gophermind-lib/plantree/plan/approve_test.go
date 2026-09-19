package plan

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "open") {
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
	if !errors.Is(err, ErrNotApprovable) || !strings.Contains(err.Error(), "phase-001.task-003") {
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
	if !errors.Is(err, ErrNotApprovable) {
		t.Errorf("Approve = %v, want a refusal", err)
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
