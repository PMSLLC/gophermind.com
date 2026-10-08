package planner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

type savedAnswer struct {
	ID, Stage, Question, Answer string
	Assumed                     bool
}

func (g *rig) answers() []savedAnswer {
	g.t.Helper()
	var f struct {
		Answers []savedAnswer `json:"answers"`
	}
	if err := json.Unmarshal(g.read("answers.json"), &f); err != nil {
		g.t.Fatal(err)
	}
	return f.Answers
}

func TestClarifyAsksThePersonAndStoresTheAnswers(t *testing.T) {
	gate := approving()
	gate.answer = func(q human.Question) string { return "Trim it, and collapse inner runs of spaces." }
	g := newRig(t, gate)
	g.mustPlan(planner.Options{StopAfter: "clarify"})

	if len(gate.asked) != 1 || gate.asked[0].ID != "q1" || gate.asked[0].Recommended != "Yes, trim it." ||
		!strings.Contains(gate.asked[0].Why, "It changes what Greet returns") {
		t.Fatalf("gate was asked %+v", gate.asked)
	}
	as := g.answers()
	if len(as) != 1 || as[0].Stage != "clarify" || as[0].Answer != "Trim it, and collapse inner runs of spaces." || as[0].Assumed {
		t.Errorf("answers.json = %+v", as)
	}
	rows, err := g.led.List(context.Background(), greeterID, ledger.Filter{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("ledger rows = %d, %v; want 2 (the questions, then the follow-up round)", len(rows), err)
	}
	if rows[0].Stage != "clarify" || rows[0].TaskType != "clarify" || rows[0].Scope != "brief" || rows[0].Tier != "strong" {
		t.Errorf("ledger row = %+v", rows[0])
	}
}

func TestClarifyTakesTheDefaultsWhenTheBriefSaysToAssume(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		return strings.Replace(s, "on_ambiguity: halt", "on_ambiguity: assume_and_document", 1)
	})
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	if len(gate.asked) != 0 {
		t.Errorf("the gate was asked %d questions in assume mode", len(gate.asked))
	}
	as := g.answers()
	if len(as) != 1 || as[0].Answer != "Yes, trim it." || !as[0].Assumed {
		t.Errorf("answers.json = %+v", as)
	}
}

func TestClarifyWithNoQuestionsSkipsTheGate(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{"clarify.txt": "No questions.\n```json\n[]\n```"}))
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	if len(gate.asked) != 0 || len(g.answers()) != 0 {
		t.Errorf("asked %d, stored %d; want none", len(gate.asked), len(g.answers()))
	}
}

// A gate that nobody has answered yet stops the run as Waiting. The resume
// asks the person again and must not ask the model again.
func TestClarifyWaitingThenResumeDoesNotCallTheModelAgain(t *testing.T) {
	gate := approving()
	gate.err = human.ErrWaiting
	g := newRig(t, gate)
	out, err := g.plan(planner.Options{StopAfter: "clarify"})
	if err != nil || out != planner.Waiting {
		t.Fatalf("Run = %q, %v; want waiting", out, err)
	}
	if g.has("answers.json") {
		t.Fatal("answers.json exists before anyone answered")
	}
	if st, _ := planner.ReadStatus(greeterID); st.Waiting != "clarify" {
		t.Errorf("status.Waiting = %q, want clarify", st.Waiting)
	}

	gate.err = nil
	g.wire() // a new process: fresh provider, same run
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "clarify"})
	// The question was not asked of the model again; only the follow-up round
	// (what the answer unblocked) is a new call.
	if got := g.stagesCalled(); len(got) != 1 || got[0] != "clarify:more" {
		t.Errorf("resume called the model for %v, want only clarify:more", got)
	}
	if len(g.answers()) != 1 {
		t.Errorf("answers.json = %+v", g.answers())
	}
	if st, _ := planner.ReadStatus(greeterID); st.Waiting != "" {
		t.Errorf("status.Waiting = %q after the answer, want empty", st.Waiting)
	}
}

func TestAMalformedClarifyReplyIsRetriedAndRecorded(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"clarify.txt":   "I have some thoughts but no JSON.",
		"clarify.2.txt": "[]",
	}))
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{})
	if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeMalformed || rows[1].Outcome != ledger.OutcomeOK {
		t.Fatalf("ledger rows = %+v", rows)
	}
}

// When every model that could answer is a public provider, the error must
// name the way out instead of a bare "no model answered".
func TestAPrivacyDeadEndNamesTheSettingToChange(t *testing.T) {
	g := newRig(t, approving())
	cfg := planner.FixtureSettings()
	cfg.Providers[0].Visibility = settings.Public
	g.router = router.New(cfg, map[string]provider.Provider{"fake": g.fake}, g.led, g.sink)
	g.deps.Caller = g.router
	_, err := g.plan(planner.Options{StopAfter: "clarify"})
	if err == nil || !strings.Contains(err.Error(), "--allow-public") || !strings.Contains(err.Error(), "brief scope") {
		t.Fatalf("err = %v, want it to name --allow-public and the scope", err)
	}
	if len(g.fake.Requests()) != 0 {
		t.Error("a public provider was sent the brief")
	}
}

// A gate that returns the wrong number of answers is an error, not a panic.
func TestAShortGateAnswerIsAnError(t *testing.T) {
	gate := &shortGate{scriptGate: *approving()}
	g := newRig(t, gate)
	_, err := g.plan(planner.Options{StopAfter: "clarify"})
	if err == nil || !strings.Contains(err.Error(), "answers for") {
		t.Fatalf("err = %v, want a count error", err)
	}
}

type shortGate struct{ scriptGate }

func (g *shortGate) Ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	return nil, nil
}

func TestAClarifyReplyOfNullIsRetried(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"clarify.txt": "null", "clarify.2.txt": "[]"}))
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	if got := string(g.read("_state/clarify/questions.json")); strings.Contains(got, "null") || !strings.Contains(got, `"questions": []`) {
		t.Errorf("questions.json = %s", got)
	}
}
