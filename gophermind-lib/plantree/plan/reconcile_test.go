package plan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

// replanned is a step in the state a changed answer leaves it in: specified,
// flagged for re-planning, with a note saying why.
func replanned(t *testing.T, id string) plantree.Node {
	t.Helper()
	n := node(t, id, "Create module", "needed to compile", "")
	n.Planning.Stage = plantree.StageNeedsReconciliation
	n.Work = &plantree.Work{Description: "the old description", AcceptanceCriteria: []string{"it works"}}
	n.ResumeNote = "re-plan: the answer to q-001 changed: \"Which database?\" -> Postgres"
	return n
}

func TestPass2PromptShowsWhatARePlannedStepIsReplacing(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	old := replanned(t, s1)
	fresh := node(t, s2, "Add CI", "catch breakage", "")

	p := Pass2Prompt(Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{old, fresh}, Batch: []plantree.Node{old}})
	for _, want := range []string{
		"previous specification: the old description",
		"being re-planned because: re-plan: the answer to q-001 changed",
		"is being re-planned because the owner changed a decision",
		s1 + ": Create module [specified, being re-planned]",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}

	// An ordinary batch says none of it, so the instruction cannot be read as
	// applying to a step that has never been specified.
	q := Pass2Prompt(Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{old, fresh}, Batch: []plantree.Node{fresh}})
	for _, unwanted := range []string{"previous specification:", "being re-planned because:", "changed a decision"} {
		if strings.Contains(q, unwanted) {
			t.Errorf("an ordinary batch carries %q", unwanted)
		}
	}
}

// TestPass2PromptWorstCaseSizeWhenRePlanning pins the largest prompt a
// reconciling pass can produce. It must stay inside the same budget as an
// ordinary pass, which is why such a batch holds fewer steps.
func TestPass2PromptWorstCaseSizeWhenRePlanning(t *testing.T) {
	for _, tc := range []struct {
		name string
		unit string
	}{{"ascii", "s"}, {"multibyte", "\U0001D11E"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := func(n int) string { return strings.Repeat(tc.unit, n) }
			phase := node(t, "phase-001", r(200), r(500), r(1000))
			task := node(t, "phase-001.task-001", r(200), r(500), r(1000))
			var steps []plantree.Node
			for i := 1; i <= 100; i++ {
				n := node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), r(200), r(500), "")
				n.Planning.Stage = plantree.StageNeedsReconciliation
				n.Work = &plantree.Work{Description: r(5000), AcceptanceCriteria: []string{"c"}}
				n.ResumeNote = r(5000)
				steps = append(steps, n)
			}
			p := Pass2Prompt(Pass2Input{
				Project: r(100), Overview: strings.Repeat("o", 20000), Facts: r(20000),
				Decisions: worstDecisions(tc.unit) + strings.Repeat("d", 20000), Excerpts: strings.Repeat("e", 20000),
				Phase: phase, Task: task, Siblings: steps, Batch: steps[:reconcileStepsPerPass],
			})
			t.Logf("worst-case re-planning pass-2 prompt (%s): %d bytes", tc.name, len(p))
			if len(p) > 27000 {
				t.Errorf("worst-case re-planning prompt is %d bytes, want at most 27000", len(p))
			}
			if !utf8.ValidString(p) {
				t.Error("the prompt is not valid UTF-8")
			}
		})
	}
}

// reconcileRepo is a two-step task, both steps specified, whose answer the
// owner then changes, so both steps are flagged for re-planning.
func reconcileRepo(t *testing.T) (*plantree.Repo, Question) {
	t.Helper()
	r, q := answeredRepo(t)
	if _, _, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	return r, q
}

func TestRunPass2LeavesRePlanningAloneUnlessAsked(t *testing.T) {
	r, _ := reconcileRepo(t)
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps != 0 || res.Reconciled != 0 || res.Passes != 0 || len(f.prompts) != 0 {
		t.Errorf("an ordinary resume redid work: %+v after %d prompts", res, len(f.prompts))
	}
	if got := stageOf(t, r, "phase-001.task-001.step-001"); got != plantree.StageNeedsReconciliation {
		t.Errorf("step-001 is %s, want needs_reconciliation", got)
	}
}

func TestRunPass2RePlansOnlyTheFlaggedSteps(t *testing.T) {
	r, q := reconcileRepo(t)
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{Reconcile: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reconciled != 2 || res.Steps != 2 || res.Passes != 1 || res.Unspecified != 0 {
		t.Errorf("Result2 = %+v, want 2 reconciled in one pass", res)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		n, err := r.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s is %s, want drafted again", id, n.Planning.Stage)
		}
		if n.Work == nil || n.Work.Description != "implement "+id {
			t.Errorf("%s work = %+v, want the new specification", id, n.Work)
		}
		if !strings.Contains(n.ResumeNote, "the previous specification was: build "+id) {
			t.Errorf("%s resume note = %q, want the replaced specification", id, n.ResumeNote)
		}
	}
	// The task the question never named was not touched by any of this.
	if n, _ := r.Get("phase-001.task-002.step-001"); n.Work.Description != "build phase-001.task-002.step-001" {
		t.Errorf("an unaffected step was re-specified: %+v", n.Work)
	}
	if len(f.prompts) != 1 || !strings.Contains(f.prompts[0], "previous specification: build phase-001.task-001.step-001") {
		t.Errorf("the pass did not see what it was replacing: %d prompts", len(f.prompts))
	}
	if !strings.Contains(f.prompts[0], q.Question) {
		t.Errorf("the pass did not see the decision that changed")
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval once everything is re-planned", got)
	}
}

func TestRunPass2ReconcileBatchesStayInsideTheirSize(t *testing.T) {
	r, _ := answeredRepo(t)
	for i := 3; i <= 2+reconcileStepsPerPass; i++ {
		n, err := newSkeleton(fmt.Sprintf("phase-001.task-001.step-%03d", i), "extra", "extra", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Create(n); err != nil {
			t.Fatal(err)
		}
		draft(t, r, n.ID)
	}
	qs, err := LoadQuestions(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, rec, err := ChangeAnswer(r, qs[0].ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	} else if len(rec.Flagged) != 2+reconcileStepsPerPass {
		t.Fatalf("flagged %d steps", len(rec.Flagged))
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{Reconcile: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passes != 2 || res.Reconciled != 2+reconcileStepsPerPass {
		t.Errorf("Result2 = %+v, want %d steps over two passes", res, 2+reconcileStepsPerPass)
	}
	for _, p := range f.prompts {
		if got := len(stepsToSpecify(p)); got > reconcileStepsPerPass {
			t.Errorf("a re-planning pass asked for %d steps, cap %d", got, reconcileStepsPerPass)
		}
	}
}

func TestNextActionsWhileStepsWaitToBeRePlanned(t *testing.T) {
	r, q := reconcileRepo(t)
	a, err := NextActions(r)
	if err != nil {
		t.Fatal(err)
	}
	var reconcile int
	for _, x := range a.Runnable {
		if x.Kind == plantree.ActionApprove {
			t.Error("approve is offered while a step waits to be re-planned")
		}
		if x.Kind != plantree.ActionReconcile {
			continue
		}
		reconcile++
		if !strings.Contains(x.Reason, q.ID) {
			t.Errorf("reconcile reason = %q, want the question whose answer changed", x.Reason)
		}
	}
	if reconcile != 2 {
		t.Errorf("%d reconcile actions, want 2", reconcile)
	}

	// An open question keeps approval away as well, even after re-planning.
	if _, err := RunPass2(context.Background(), r, specFake(), Options2{Reconcile: true}); err != nil {
		t.Fatal(err)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Fatalf("NextActions = %s, want only approval", got)
	}
	if _, err := AddQuestions(r, []NewQuestion{{Question: "One more thing?", Why: "w", Affects: []string{"phase-001.task-002"}}}); err != nil {
		t.Fatal(err)
	}
	a, err = NextActions(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range a.Runnable {
		if x.Kind == plantree.ActionApprove {
			t.Error("approve is offered while a question is open")
		}
	}
	if len(a.Blocked) != 1 || a.Blocked[0].Kind != plantree.ActionAnswer {
		t.Errorf("Blocked = %+v, want one answer action", a.Blocked)
	}
}
