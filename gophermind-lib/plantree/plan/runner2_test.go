package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

var stepIDRE = regexp.MustCompile(`phase-\d{3}\.task-\d{3}\.step-\d{3}`)

// stepsToSpecify returns the step ids listed under "Steps to specify now".
func stepsToSpecify(prompt string) []string {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	if start < 0 || end < start {
		return nil
	}
	return stepIDRE.FindAllString(prompt[start:end], -1)
}

// pass2Reply answers a pass-2 prompt with a valid specification for every step
// it asks for. Step 2 of a task depends on step 1.
func pass2Reply(prompt string) string {
	var steps []StepSpecOut
	for _, id := range stepsToSpecify(prompt) {
		s := StepSpecOut{
			ID:                 id,
			Description:        "implement " + id,
			TargetPaths:        []string{"pkg/" + id[len(id)-8:] + ".go"},
			AcceptanceCriteria: []string{"it works"},
			TestCommand:        []string{"go", "test", "./..."},
			DependsOn:          []string{},
		}
		if strings.HasSuffix(id, "step-002") {
			s.DependsOn = []string{strings.TrimSuffix(id, "step-002") + "step-001"}
		}
		steps = append(steps, s)
	}
	b, _ := json.Marshal(Pass2Output{Steps: steps})
	return string(b)
}

func specFake() *fake {
	return &fake{reply: func(_ int, p string) (string, error) { return pass2Reply(p), nil }}
}

// planned runs pass 1 over threePartBrief and returns the repo and the planning
// directory it lives in, so a test can reopen it as a new process would.
func planned(t *testing.T) (*plantree.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	r := plantree.Open(dir)
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func actionKinds(t *testing.T, r *plantree.Repo) string {
	t.Helper()
	a, err := NextActions(r)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range append(a.Runnable, a.Blocked...) {
		out = append(out, string(x.Kind)+":"+x.NodeID)
	}
	return fmt.Sprint(out)
}

func TestRunPass2SpecifiesEveryStepAndLeavesOnlyApproval(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result2{Tasks: 1, Steps: 2, Passes: 1}) {
		t.Errorf("Result2 = %+v", res)
	}
	a, _ := r.Get(s1)
	b, _ := r.Get(s2)
	if a.Planning.Stage != plantree.StageDrafted || a.Status != plantree.StatusUntouched || a.Work == nil ||
		a.Work.Description != "implement "+s1 || len(a.Work.TestCommand) != 3 || a.Work.TargetPaths[0] != "pkg/step-001.go" {
		t.Errorf("step 1 = %+v work=%+v", a, a.Work)
	}
	if len(b.DependsOn) != 1 || b.DependsOn[0] != s1 {
		t.Errorf("step 2 depends_on = %v", b.DependsOn)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if !strings.Contains(f.prompts[0], `"demo"`) {
		t.Error("the project name defaults to the root title")
	}
}

func TestRunPass2BatchesStepsOfOneTask(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passes != 2 || res.Steps != 2 || res.Tasks != 1 || len(f.prompts) != 2 {
		t.Errorf("Result2 = %+v, %d calls", res, len(f.prompts))
	}
	if got := stepsToSpecify(f.prompts[1]); len(got) != 1 || got[0] != s2 {
		t.Errorf("the second batch must be only step 2, got %v", got)
	}
}

func TestRunPass2ResumesFromTheTreeAfterAFailure(t *testing.T) {
	r, dir := planned(t)
	broken := &fake{reply: func(_ int, p string) (string, error) {
		if strings.Contains(p, "Task: T2") {
			return "", errors.New("model unavailable")
		}
		return pass2Reply(p), nil
	}}
	res, err := RunPass2(context.Background(), r, broken, Options2{})
	if err == nil || !strings.Contains(err.Error(), "task phase-001.task-002") || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("err = %v, want it to name the task and the cause", err)
	}
	if res.Tasks != 1 || res.Steps != 1 {
		t.Errorf("Result2 = %+v", res)
	}
	first, _ := r.Get("phase-001.task-001.step-001")
	second, _ := r.Get("phase-001.task-002.step-001")
	if first.Planning.Stage != plantree.StageDrafted || second.Planning.Stage != plantree.StageSkeleton {
		t.Errorf("stages after failure: %s and %s", first.Planning.Stage, second.Planning.Stage)
	}

	good := specFake() // a new process, a new client
	res, err = RunPass2(context.Background(), plantree.Open(dir), good, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if len(good.prompts) != 2 || res.Tasks != 2 || res.Steps != 2 {
		t.Errorf("resume made %d calls, Result2 = %+v; want 2 calls for the 2 tasks left", len(good.prompts), res)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s", got)
	}
}

func TestRunPass2ShowsEachTaskOnlyTheBriefThatProducedIt(t *testing.T) {
	r, _ := planned(t)
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 3 {
		t.Fatalf("%d calls, want 3", len(f.prompts))
	}
	// Task order: phase-001.task-001 (chunk 1), phase-001.task-002 (chunk 2), phase-002.task-001 (chunk 3).
	want := []struct{ has, not string }{
		{"first part text", "third part text"},
		{"second part text", "third part text"},
		{"third part text", "first part text"},
	}
	for i, w := range want {
		if !strings.Contains(f.prompts[i], w.has) || strings.Contains(f.prompts[i], w.not) {
			t.Errorf("prompt %d must contain %q and not %q", i, w.has, w.not)
		}
	}
	if !strings.Contains(f.prompts[0], "[part 1 of 3]") {
		t.Error("excerpts are labeled with their place in the brief")
	}
}

func TestRunPass2WithoutAStoredBriefStillWorks(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "(not available)") {
		t.Error("a plan with no stored brief must say the excerpts are not available")
	}
}

func TestRunPass2RetriesAMalformedReplyOnce(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := &fake{reply: func(n int, p string) (string, error) {
		if n == 0 {
			return "Sorry, here is prose only.", nil
		}
		return pass2Reply(p), nil
	}}
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil || len(f.prompts) != 2 || res.Steps != 2 {
		t.Fatalf("err=%v calls=%d res=%+v", err, len(f.prompts), res)
	}
	if !strings.Contains(f.prompts[1], "Sorry, here is prose only.") {
		t.Error("the retry must quote the rejected reply")
	}
}

func TestRunPass2RejectedTwiceLeavesTheStepsAlone(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	onlyOne := func(_ int, p string) (string, error) {
		ids := stepsToSpecify(p)
		s := StepSpecOut{ID: ids[0], Description: "d", AcceptanceCriteria: []string{"c"}, DependsOn: []string{}}
		b, _ := json.Marshal(Pass2Output{Steps: []StepSpecOut{s}}) // the second step is missing
		return string(b), nil
	}
	res, err := RunPass2(context.Background(), r, &fake{reply: onlyOne}, Options2{})
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "missing from the reply") {
		t.Fatalf("err = %v", err)
	}
	if res.Steps != 0 {
		t.Errorf("Result2 = %+v", res)
	}
	a, _ := r.Get(s1)
	if a.Planning.Stage != plantree.StageSkeleton || a.Work != nil {
		t.Errorf("a rejected reply must write nothing: %+v", a)
	}
}

func TestRunPass2SkipsStepsThatAreDraftedOrOnHold(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
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
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if got := stepsToSpecify(f.prompts[0]); len(got) != 1 || got[0] != s1 {
		t.Errorf("only step 1 should be asked for, got %v", got)
	}
	skipped, _ := r.Get(s2)
	if skipped.Work != nil {
		t.Error("a skipped step must not be specified")
	}

	again := specFake()
	res, err := RunPass2(context.Background(), r, again, Options2{})
	if err != nil || len(again.prompts) != 0 || res != (Result2{}) {
		t.Errorf("nothing left to specify: err=%v calls=%d res=%+v", err, len(again.prompts), res)
	}
}

func TestRunPass2AddsAHintForAContextWindowError(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("server said no")
	f := &fake{reply: func(int, string) (string, error) {
		return "", fmt.Errorf("%w: status 400: {\"error\":{\"type\":\"exceed_context_size_error\",\"n_ctx\":4096}}", sentinel)
	}}
	_, err := RunPass2(context.Background(), r, f, Options2{})
	if err == nil || !strings.Contains(err.Error(), "lower Options2.BriefBytes") || !errors.Is(err, sentinel) {
		t.Errorf("err = %v", err)
	}
}

func TestRunPass2NeedsAPlanAndHonorsCancellation(t *testing.T) {
	empty := plantree.Open(t.TempDir())
	if _, err := RunPass2(context.Background(), empty, specFake(), Options2{}); !errors.Is(err, plantree.ErrNotFound) {
		t.Errorf("no plan: err = %v, want ErrNotFound", err)
	}
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := specFake()
	if _, err := RunPass2(ctx, r, f, Options2{}); !errors.Is(err, context.Canceled) || len(f.prompts) != 0 {
		t.Errorf("cancelled: err=%v calls=%d", err, len(f.prompts))
	}
}

func TestRunPass2RefusesADependencyOnAHeldStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Get(s1)
	if _, err := r.Update(s1, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dependsOnHeld := func(_ int, p string) (string, error) {
		s := StepSpecOut{ID: stepsToSpecify(p)[0], Description: "d", AcceptanceCriteria: []string{"c"}, DependsOn: []string{s1}}
		b, _ := json.Marshal(Pass2Output{Steps: []StepSpecOut{s}})
		return string(b), nil
	}
	_, err := RunPass2(context.Background(), r, &fake{reply: dependsOnHeld}, Options2{})
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "not a step of this task") {
		t.Fatalf("err = %v", err)
	}
	b, _ := r.Get(s2)
	if b.Planning.Stage != plantree.StageSkeleton || b.Work != nil {
		t.Errorf("step 2 must stay a skeleton: %+v", b)
	}
}

func TestRunPass2SecondBatchSeesTheFirstBatchAsSpecified(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 2 {
		t.Fatalf("%d prompts", len(f.prompts))
	}
	second := f.prompts[1]
	if !regexp.MustCompile(`step-001: .* \[specified\]`).MatchString(second) || !regexp.MustCompile(`step-002: .* \[to specify\]`).MatchString(second) {
		t.Errorf("second prompt step list:\n%s", second)
	}
	if strings.Contains(f.prompts[0], "[specified]") {
		t.Error("the first prompt must not show any step as specified")
	}
}

func TestRunPass2ReportsTasksWithoutSteps(t *testing.T) {
	r := newRepo(t)
	out := sampleOut()
	out.Phases[0].Tasks = append(out.Phases[0].Tasks, TaskOut{Title: "Docs", Digest: "explain it"})
	if _, err := Merge(r, out); err != nil {
		t.Fatal(err)
	}
	res, err := RunPass2(context.Background(), r, specFake(), Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EmptyTasks != 1 {
		t.Errorf("EmptyTasks = %d, want 1", res.EmptyTasks)
	}
	got, err := EmptyTasks(r)
	if err != nil || len(got) != 1 || got[0] != "phase-001.task-002" {
		t.Errorf("EmptyTasks(repo) = %v, %v", got, err)
	}
	kinds := actionKinds(t, r)
	if !strings.Contains(kinds, "decompose:phase-001.task-002") || strings.Contains(kinds, "approve") {
		t.Errorf("NextActions = %s", kinds)
	}
}

func TestRunPass2ShowsTheStoredFactsAndAnOptionOverridesThem(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if err := WriteFacts(r, "STORED FACTS: go test ./..."); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "STORED FACTS: go test ./...") {
		t.Error("RunPass2 must show the stored facts")
	}

	r2 := newRepo(t)
	if _, err := Merge(r2, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if err := WriteFacts(r2, "STORED FACTS"); err != nil {
		t.Fatal(err)
	}
	f2 := specFake()
	if _, err := RunPass2(context.Background(), r2, f2, Options2{Facts: "OVERRIDE FACTS"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f2.prompts[0], "OVERRIDE FACTS") || strings.Contains(f2.prompts[0], "STORED FACTS") {
		t.Error("Options2.Facts must override the stored facts")
	}
}

// askingFake specifies every step of a batch except askID, which it asks a
// question about instead.
func askingFake(askID string) *fake {
	return &fake{reply: func(_ int, p string) (string, error) {
		var steps []StepSpecOut
		var asks []string
		for _, id := range stepsToSpecify(p) {
			if id == askID {
				asks = append(asks, id)
				continue
			}
			steps = append(steps, StepSpecOut{
				ID: id, Description: "implement " + id, AcceptanceCriteria: []string{"it works"},
				TestCommand: []string{"go", "test"}, DependsOn: []string{},
			})
		}
		var qs []QuestionOut
		if len(asks) > 0 {
			q := goodQ()
			q.Affects = asks
			qs = append(qs, q)
		}
		b, _ := json.Marshal(Pass2Output{Steps: steps, Questions: qs})
		return string(b), nil
	}}
}

func TestRunPass2AsksInsteadOfGuessingAndHoldsThatStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	res, err := RunPass2(context.Background(), r, askingFake(s1), Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Questions != 1 || res.Steps != 1 || res.Passes != 1 {
		t.Errorf("Result2 = %+v, want 1 question, 1 step specified, 1 pass", res)
	}
	if got := stageOf(t, r, s1); got != plantree.StageAwaitingAnswers {
		t.Errorf("the step asked about is %s, want awaiting_answers", got)
	}
	if got := stageOf(t, r, s2); got != plantree.StageDrafted {
		t.Errorf("the other step is %s, want drafted", got)
	}
	if n, _ := r.Get(s1); n.Work != nil {
		t.Error("a step asked about must get no specification")
	}
	qs, _ := LoadQuestions(r)
	if len(qs) != 1 || qs[0].Source != "specifying task phase-001.task-001" || len(qs[0].Affects) != 1 || qs[0].Affects[0] != s1 {
		t.Errorf("questions = %+v", qs)
	}
	if got := actionKinds(t, r); !strings.Contains(got, "answer:"+s1) || strings.Contains(got, "approve") {
		t.Errorf("NextActions = %s; want an answer action for the held step and no approve", got)
	}
}

func TestReplayingAPass2QuestionAsksNothingTwice(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	out := Pass2Output{Questions: []QuestionOut{askAbout(s1)}}
	first, err := recordPass2Questions(r, "phase-001.task-001", out)
	second, err2 := recordPass2Questions(r, "phase-001.task-001", out)
	if err != nil || err2 != nil || first != 1 || second != 0 {
		t.Errorf("first=%d second=%d errs=%v %v; want 1 then 0", first, second, err, err2)
	}
	if qs, _ := LoadQuestions(r); len(qs) != 1 {
		t.Errorf("%d questions, want 1", len(qs))
	}
}

func TestRunPass2ShowsTheOwnersDecisionsToTheTasksTheyAffect(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	q := twoOptions()
	q.Question = "Which database should the module use?"
	q.Affects = []string{"phase-001.task-001"}
	if _, err := AddQuestions(r, []NewQuestion{q}); err != nil {
		t.Fatal(err)
	}
	other := twoOptions()
	other.Question = "Unrelated decision about billing?"
	other.Affects = []string{"phase-009"}
	if _, err := AddQuestions(r, []NewQuestion{other}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"q-001", "q-002"} {
		if _, err := AnswerQuestion(r, id, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
			t.Fatal(err)
		}
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], `"Which database should the module use?" -> Postgres`) {
		t.Error("the decision that affects this task must be shown")
	}
	if strings.Contains(f.prompts[0], "billing") {
		t.Error("a decision about another part of the plan must not be shown")
	}
}

func TestASecondBatchSeesAnAskedStepAsWaiting(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := askingFake(s1)
	if _, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 2 || !strings.Contains(f.prompts[1], s1+": Create module [waiting for an answer]") {
		t.Errorf("%d prompts; the second must show step 1 as waiting for an answer", len(f.prompts))
	}
}

func TestAnsweredDuplicateQuestionShowsItsDecisionForTheNewStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	q := twoOptions()
	q.Affects = []string{"phase-009"} // another part of the plan
	if _, err := AddQuestions(r, []NewQuestion{q}); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	// A later pass asks the same, already answered, question about step 2.
	if n, err := recordPass2Questions(r, "phase-001.task-001", Pass2Output{Questions: []QuestionOut{askAbout(s2)}}); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v; a duplicate is not a new question", n, err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], `"Which database?" -> Postgres`) {
		t.Error("the answered decision must be visible to the pass that specifies the newly named step")
	}
	if got := stageOf(t, r, s2); got != plantree.StageDrafted {
		t.Errorf("step 2 is %s, want drafted", got)
	}
	again := specFake()
	if _, err := RunPass2(context.Background(), r, again, Options2{}); err != nil || len(again.prompts) != 0 {
		t.Errorf("nothing may be selected again: err=%v calls=%d", err, len(again.prompts))
	}
}

func TestOpenDuplicateQuestionExtendsAffectsAndHoldsTheNewStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if _, err := recordPass2Questions(r, "phase-001.task-001", Pass2Output{Questions: []QuestionOut{askAbout(s1)}}); err != nil {
		t.Fatal(err)
	}
	if n, err := recordPass2Questions(r, "phase-001.task-001", Pass2Output{Questions: []QuestionOut{askAbout(s2)}}); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	qs, _ := LoadQuestions(r)
	if len(qs) != 1 || len(qs[0].Affects) != 2 || qs[0].Affects[0] != s1 || qs[0].Affects[1] != s2 {
		t.Fatalf("questions = %+v; want one question affecting step 1 then step 2", qs)
	}
	if got := stageOf(t, r, s2); got != plantree.StageAwaitingAnswers {
		t.Errorf("step 2 is %s, want awaiting_answers", got)
	}
	// Replaying either pass must not grow Affects again.
	for _, id := range []string{s1, s2, s2} {
		if _, err := recordPass2Questions(r, "phase-001.task-001", Pass2Output{Questions: []QuestionOut{askAbout(id)}}); err != nil {
			t.Fatal(err)
		}
	}
	qs, _ = LoadQuestions(r)
	if len(qs) != 1 || len(qs[0].Affects) != 2 {
		t.Errorf("affects after replay = %v, want 2", qs[0].Affects)
	}
}
