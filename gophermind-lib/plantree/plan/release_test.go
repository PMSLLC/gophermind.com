package plan

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

// heldRepo is a repo whose steps s1 and s2 both wait for an answer.
func heldRepo(t *testing.T) *plantree.Repo {
	t.Helper()
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if _, err := holdSteps(r, []string{"phase-001"}); err != nil {
		t.Fatal(err)
	}
	return r
}

func ask(t *testing.T, r *plantree.Repo, text string, affects ...string) string {
	t.Helper()
	q := twoOptions()
	q.Question = text
	q.Affects = affects
	got, err := AddQuestions(r, []NewQuestion{q})
	if err != nil {
		t.Fatal(err)
	}
	return got[0].ID
}

func TestReleaseAnsweredWaitsForEveryQuestionThatAffectsAStep(t *testing.T) {
	r := heldRepo(t)
	first := ask(t, r, "First?", s1)
	second := ask(t, r, "Second?", s1, s2)

	if n, err := ReleaseAnswered(r); err != nil || n != 0 {
		t.Fatalf("nothing answered: released %d, err %v", n, err)
	}
	if _, err := AnswerQuestion(r, first, Answer{OptionIDs: []string{"opt-1"}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("step 1 still has an open question, released %d", n)
	}
	if _, err := AnswerQuestion(r, second, Answer{Text: "either"}); err != nil {
		t.Fatal(err)
	}
	n, err := ReleaseAnswered(r)
	if err != nil || n != 2 {
		t.Fatalf("released %d, err %v; want 2", n, err)
	}
	for _, id := range []string{s1, s2} {
		if got := stageOf(t, r, id); got != plantree.StageInspected {
			t.Errorf("%s = %s, want inspected", id, got)
		}
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("a second call released %d, want 0", n)
	}
}

func TestReleaseAnsweredHonorsAQuestionAboveTheStep(t *testing.T) {
	r := heldRepo(t)
	id := ask(t, r, "About the whole task?", "phase-001.task-001")
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("an open question on the task holds its steps, released %d", n)
	}
	if _, err := AnswerQuestion(r, id, Answer{Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 2 {
		t.Errorf("released %d, want 2", n)
	}
	ask(t, r, "Plan-wide?", plantree.RootID)
	if _, err := holdSteps(r, []string{"phase-001"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("a plan-wide open question holds everything, released %d", n)
	}
}

func TestReleaseAnsweredIgnoresSkeletonsAndHeldSteps(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil { // both steps are skeletons, not waiting
		t.Fatal(err)
	}
	if n, err := ReleaseAnswered(r); err != nil || n != 0 {
		t.Fatalf("released %d, err %v", n, err)
	}
	if _, err := holdSteps(r, []string{s2}); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Get(s2)
	if _, err := r.Update(s2, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("a step on hold must not be released, released %d", n)
	}
	if got := stageOf(t, r, s2); got != plantree.StageAwaitingAnswers {
		t.Errorf("stage = %s", got)
	}
}

// TestPass1QuestionAnswerThenPass2 walks the whole question loop with fakes:
// pass 1 asks, pass 2 skips the held step, the owner answers, pass 2 releases
// and specifies it with the decision in its prompt, and only approval is left.
func TestPass1QuestionAnswerThenPass2(t *testing.T) {
	dir := t.TempDir()
	r := plantree.Open(dir)
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts); err != nil {
		t.Fatal(err)
	}

	first := specFake()
	res, err := RunPass2(context.Background(), r, first, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps != 2 || res.Released != 0 || len(first.prompts) != 2 {
		t.Errorf("first pass 2: %+v, %d calls; want 2 steps, none released, 2 calls (the held task is skipped)", res, len(first.prompts))
	}
	if got := actionKinds(t, r); !strings.Contains(got, "answer:phase-001.task-002.step-001") || strings.Contains(got, "approve") {
		t.Errorf("NextActions = %s", got)
	}

	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-2"}, Text: "B fits our stack"}); err != nil {
		t.Fatal(err)
	}
	second := specFake()
	res, err = RunPass2(context.Background(), plantree.Open(dir), second, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Released != 1 || res.Steps != 1 || len(second.prompts) != 1 {
		t.Errorf("second pass 2: %+v, %d calls; want 1 released, 1 step, 1 call", res, len(second.prompts))
	}
	if !strings.Contains(second.prompts[0], `"Which framework?" -> B (note: B fits our stack)`) {
		t.Error("the owner's decision must reach the prompt of the task it affects")
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}
}
