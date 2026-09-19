package plan

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

const questionJSON = `"questions":[{"question":"Which framework?","why":"it changes T2","options":[{"label":"A","description":"first"},{"label":"B","description":"second"}],"multi_select":false,"recommended":["A"],"rationale":"common","affects":["T2"]}],`

// reply2Q is reply2 with a question that affects the task titled T2.
var reply2Q = strings.Replace(reply2, `"overview":"overview 2"`, questionJSON+`"overview":"overview 2"`, 1)

// byChunkQ answers like byChunk but asks a question in chunk 2.
func byChunkQ(n int, prompt string) (string, error) {
	if strings.Contains(prompt, "second part text") {
		return reply2Q, nil
	}
	return byChunk(n, prompt)
}

func stageOf(t *testing.T, r *plantree.Repo, id string) plantree.Stage {
	t.Helper()
	n, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return n.Planning.Stage
}

func TestRunPass1AsksAQuestionAndHoldsTheAffectedSteps(t *testing.T) {
	r := plantree.Open(t.TempDir())
	res, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Questions != 1 {
		t.Errorf("Result.Questions = %d, want 1", res.Questions)
	}
	qs, err := LoadQuestions(r)
	if err != nil || len(qs) != 1 {
		t.Fatalf("questions = %+v, %v", qs, err)
	}
	q := qs[0]
	if q.Status != QuestionOpen || q.Source != "chunk 2 of the brief" || len(q.Affects) != 1 || q.Affects[0] != "phase-001.task-002" ||
		q.Recommended == nil || q.Recommended.OptionIDs[0] != "opt-1" {
		t.Errorf("question = %+v", q)
	}
	if got := stageOf(t, r, "phase-001.task-002.step-001"); got != plantree.StageAwaitingAnswers {
		t.Errorf("the affected step is %s, want awaiting_answers", got)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-002.task-001.step-001"} {
		if got := stageOf(t, r, id); got != plantree.StageSkeleton {
			t.Errorf("%s = %s, want skeleton (the question does not affect it)", id, got)
		}
	}
	a, _ := r.NextActions()
	var answers int
	for _, x := range a.Blocked {
		if x.Kind == plantree.ActionAnswer && x.NodeID == "phase-001.task-002.step-001" {
			answers++
		}
	}
	if answers != 1 || len(a.Runnable) != 2 {
		t.Errorf("Blocked=%v Runnable=%v; want one answer action for the held step and two draft actions", a.Blocked, a.Runnable)
	}
}

func TestReplayingAPassAsksNothingTwice(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts); err != nil {
		t.Fatal(err)
	}
	out, err := ParsePass1(reply2Q)
	if err != nil {
		t.Fatal(err)
	}
	_, touched, err := mergeTracked(r, out)
	if err != nil {
		t.Fatal(err)
	}
	added, err := applyPass1Questions(r, Chunk{Index: 1}, out, touched)
	if err != nil || added != 0 {
		t.Fatalf("replay added %d questions, err=%v; want 0", added, err)
	}
	if qs, _ := LoadQuestions(r); len(qs) != 1 {
		t.Errorf("%d questions after a replay, want 1", len(qs))
	}
}

func TestAnAnsweredQuestionHoldsNothingOnReplay(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts); err != nil {
		t.Fatal(err)
	}
	// Put the held step back as a skeleton, as ReleaseAnswered will, then answer.
	step, _ := r.Get("phase-001.task-002.step-001")
	if _, err := r.Update(step.ID, step.NodeRevision, func(n *plantree.Node) error {
		n.Planning.Stage = plantree.StageSkeleton
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	out, _ := ParsePass1(reply2Q)
	_, touched, _ := mergeTracked(r, out)
	if _, err := applyPass1Questions(r, Chunk{Index: 1}, out, touched); err != nil {
		t.Fatal(err)
	}
	if got := stageOf(t, r, step.ID); got != plantree.StageSkeleton {
		t.Errorf("a replay of an answered question re-held the step: %s", got)
	}
}

func TestQuestionTitlesThatMatchNothingAreIgnored(t *testing.T) {
	r := plantree.Open(t.TempDir())
	odd := strings.Replace(reply2Q, `"affects":["T2"]`, `"affects":["No such task"]`, 1)
	f := &fake{reply: func(n int, p string) (string, error) {
		if strings.Contains(p, "second part text") {
			return odd, nil
		}
		return byChunk(n, p)
	}}
	if _, err := RunPass1(context.Background(), r, threePartBrief, f, opts); err != nil {
		t.Fatal(err)
	}
	qs, _ := LoadQuestions(r)
	if len(qs) != 1 || len(qs[0].Affects) != 0 {
		t.Errorf("questions = %+v; want one question with no resolved affects", qs)
	}
	if got := stageOf(t, r, "phase-001.task-002.step-001"); got != plantree.StageSkeleton {
		t.Errorf("a question that affects nothing must hold nothing, got %s", got)
	}
}

func TestHoldStepsCoversTheWholeSubtreeAndSkipsHeldOrSpecifiedSteps(t *testing.T) {
	r := newRepo(t)
	out := sampleOut()
	out.Phases[0].Tasks[0].Steps = append(out.Phases[0].Tasks[0].Steps, StepOut{Title: "Add lint", Digest: "style"})
	if _, err := Merge(r, out); err != nil {
		t.Fatal(err)
	}
	// step-002 is skipped, step-003 already drafted.
	second, _ := r.Get(s2)
	if _, err := r.Update(s2, second.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	third, _ := r.Get(s3)
	if _, err := r.Update(s3, third.NodeRevision, func(n *plantree.Node) error {
		n.Work = draftedWorkFor()
		n.Planning.Stage = plantree.StageDrafted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	held, err := holdSteps(r, []string{"phase-001"})
	if err != nil || held != 1 {
		t.Fatalf("held = %d, err = %v; want only step 1", held, err)
	}
	if stageOf(t, r, s1) != plantree.StageAwaitingAnswers || stageOf(t, r, s3) != plantree.StageDrafted {
		t.Error("only the waiting step may be held")
	}
	if held, _ := holdSteps(r, []string{"phase-001"}); held != 0 {
		t.Error("holding again must change nothing")
	}
	if held, _ := holdSteps(r, nil); held != 0 {
		t.Error("no ids holds nothing")
	}
}

func draftedWorkFor() *plantree.Work {
	return &plantree.Work{Description: "d", AcceptanceCriteria: []string{"c"}}
}
