package plantree

import (
	"fmt"
	"testing"
)

const (
	step2 = "phase-001.task-001.step-002"
	task1 = "phase-001.task-001"
)

// setStep moves a step to a stage (and status), adding work when the stage
// requires it.
func setStep(t *testing.T, r *Repo, id string, stage Stage, status Status, reason string) {
	t.Helper()
	cur, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Update(id, cur.NodeRevision, func(n *Node) error {
		n.Planning.Stage = stage
		n.Status = status
		n.Reason = reason
		if stage == StageDrafted || stage == StageApproved {
			n.Work = draftedWork()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func kinds(as []Action) string {
	var s []string
	for _, a := range as {
		s = append(s, fmt.Sprintf("%s:%s", a.Kind, a.NodeID))
	}
	return fmt.Sprint(s)
}

func TestNextActionsEmptyPlanNeedsDecomposition(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(mk(t, RootID)); err != nil {
		t.Fatal(err)
	}
	got, err := r.NextActions()
	if err != nil {
		t.Fatal(err)
	}
	if kinds(got.Runnable) != "[decompose:plan]" || len(got.Blocked) != 0 {
		t.Errorf("empty plan: runnable=%s blocked=%s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestNextActionsChildlessStructuralNodesNeedDecomposition(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.Init(mk(t, RootID)))
	must(r.Create(mk(t, "phase-001")))
	got, _ := r.NextActions()
	if kinds(got.Runnable) != "[decompose:phase-001]" {
		t.Errorf("phase without tasks: %s", kinds(got.Runnable))
	}
	must(r.Create(mk(t, task1)))
	got, _ = r.NextActions()
	if kinds(got.Runnable) != "[decompose:phase-001.task-001]" {
		t.Errorf("task without steps: %s", kinds(got.Runnable))
	}
}

func TestNextActionsDraftReconcileAndPrecedence(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, step2)
	got, _ := r.NextActions()
	if kinds(got.Runnable) != "[draft:"+step1+" draft:"+step2+"]" {
		t.Fatalf("two skeletons: %s", kinds(got.Runnable))
	}

	setStep(t, r, step1, StageDrafted, StatusUntouched, "")
	got, _ = r.NextActions()
	if kinds(got.Runnable) != "[draft:"+step2+"]" {
		t.Errorf("after drafting step-001: %s", kinds(got.Runnable))
	}

	// A changed requirement outranks fresh drafting, even though the step
	// needing a draft comes first in tree order.
	setStep(t, r, step2, StageNeedsReconciliation, StatusUntouched, "")
	setStep(t, r, step1, StageSkeleton, StatusUntouched, "")
	got, _ = r.NextActions()
	if kinds(got.Runnable) != "[reconcile:"+step2+" draft:"+step1+"]" {
		t.Errorf("reconcile must come first: %s", kinds(got.Runnable))
	}
}

func TestNextActionsBlockedQuestionDoesNotStallOtherWork(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, step2)
	setStep(t, r, step1, StageAwaitingAnswers, StatusUntouched, "")
	got, _ := r.NextActions()
	if kinds(got.Runnable) != "[draft:"+step2+"]" || kinds(got.Blocked) != "[answer:"+step1+"]" {
		t.Errorf("runnable=%s blocked=%s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestNextActionsHeldStepsAreIgnored(t *testing.T) {
	r, _ := newRepo(t)
	setStep(t, r, step1, StageSkeleton, StatusDelayed, "waiting for hardware")
	got, _ := r.NextActions()
	if len(got.Runnable) != 0 || len(got.Blocked) != 0 {
		t.Errorf("held step produced actions: %s %s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestNextActionsApproveOnlyWhenNothingElseIsOutstanding(t *testing.T) {
	r, dir := newRepo(t)
	addStep(t, r, step2)
	setStep(t, r, step1, StageDrafted, StatusUntouched, "")
	setStep(t, r, step2, StageAwaitingAnswers, StatusUntouched, "")
	got, _ := r.NextActions()
	for _, a := range append(got.Runnable, got.Blocked...) {
		if a.Kind == ActionApprove {
			t.Fatalf("approve offered while a question is open: %s", kinds(got.Runnable))
		}
	}

	setStep(t, r, step2, StageDrafted, StatusUntouched, "")
	got, _ = Open(dir).NextActions() // a fresh process
	if kinds(got.Runnable) != "[approve:plan]" {
		t.Fatalf("all drafted: %s", kinds(got.Runnable))
	}

	setStep(t, r, step1, StageApproved, StatusReviewed, "")
	setStep(t, r, step2, StageApproved, StatusReviewed, "")
	got, _ = r.NextActions()
	if len(got.Runnable) != 0 || len(got.Blocked) != 0 {
		t.Errorf("an approved plan has nothing left: %s %s", kinds(got.Runnable), kinds(got.Blocked))
	}
}

func TestSummarizeCountsAndDerivedStatus(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, step2)
	addStep(t, r, "phase-001.task-001.step-003")
	setStep(t, r, step1, StageApproved, StatusReviewed, "")
	setStep(t, r, step2, StageAwaitingAnswers, StatusUntouched, "")
	setStep(t, r, "phase-001.task-001.step-003", StageSkeleton, StatusSkipped, "out of scope")

	s, err := r.Summarize(task1)
	if err != nil {
		t.Fatal(err)
	}
	if s.Leaves != 3 || s.Counts.Approved != 1 || s.Counts.AwaitingAnswers != 1 || s.Counts.Skeleton != 1 || s.Counts.Skipped != 1 {
		t.Errorf("counts = %+v", s)
	}
	if s.Status != StatusUntouched {
		t.Errorf("a subtree with unfinished or skipped leaves must not be reviewed, got %q", s.Status)
	}

	setStep(t, r, step2, StageApproved, StatusReviewed, "")
	setStep(t, r, "phase-001.task-001.step-003", StageApproved, StatusReviewed, "")
	s, _ = r.Summarize(RootID)
	if s.Leaves != 3 || s.Status != StatusReviewed {
		t.Errorf("all leaves approved: %+v", s)
	}

	empty := Open(t.TempDir())
	_ = empty.Init(mk(t, RootID))
	if s, _ := empty.Summarize(RootID); s.Leaves != 0 || s.Status != StatusUntouched {
		t.Errorf("an empty plan is not reviewed: %+v", s)
	}
}

func TestNextActionsNoApproveWhileAStepIsSkipped(t *testing.T) {
	r, _ := newRepo(t)
	addStep(t, r, step2)
	setStep(t, r, step1, StageDrafted, StatusUntouched, "")
	setStep(t, r, step2, StageSkeleton, StatusSkipped, "out of scope")
	got, _ := r.NextActions()
	if len(got.Runnable) != 0 || len(got.Blocked) != 0 {
		t.Errorf("skipped step must hold approve: %s %s", kinds(got.Runnable), kinds(got.Blocked))
	}
}
