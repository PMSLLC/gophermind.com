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
	a, err := r.NextActions()
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
