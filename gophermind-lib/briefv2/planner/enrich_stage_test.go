package planner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

var allGroups = []string{"rationale", "construction", "alternatives", "portability", "security", "performance", "observability",
	"refactor_notes", "profile_hooks", "assumptions", "open_questions", "decision_ids", "node_class"}

func enrichRig(t *testing.T, respond enrichResponder) (*rig, *enrichCalls) {
	t.Helper()
	g := newRig(t, approving(), fixtureDir(t, map[string]string{"clarify.more.txt": "[]"}))
	return g, withEnrichment(g, respond)
}

func TestEnrichWritesEveryGroupIntoTheDraftsAndTheTree(t *testing.T) {
	g, _ := enrichRig(t, allGood)
	if out, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	n := 0
	for comp, drafts := range g.drafts().Components {
		for _, d := range drafts {
			n++
			for _, k := range allGroups {
				if _, ok := d[k]; !ok {
					t.Errorf("%s/%v lacks %s", comp, d["id"], k)
				}
			}
			ct := d["contract"].(map[string]any)
			if ins, _ := ct["inputs"].([]any); len(ins) > 0 {
				if _, ok := ins[0].(map[string]any)["go_type"]; !ok {
					t.Errorf("%v: inputs carry no go_type", d["id"])
				}
			}
			for _, e := range ct["errors"].([]any) {
				if e.(map[string]any)["kind"] == nil {
					t.Errorf("%v: an error has no kind", d["id"])
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no drafts")
	}
	var st struct {
		Done       bool                      `json:"done"`
		Components map[string]map[string]any `json:"components"`
		Root       map[string]any            `json:"root"`
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "_state", "enriched.json"))
	if err := json.Unmarshal(raw, &st); err != nil || !st.Done || len(st.Components) == 0 || st.Root["rationale"] == nil {
		t.Fatalf("enriched.json = %s (%v)", raw, err)
	}
	// The conversation is in the tree: the root carries every settled question.
	rootRaw, err := os.ReadFile(filepath.Join(g.runDir, "root.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	_ = json.Unmarshal(rootRaw, &root)
	decisions, _ := root["decisions"].([]any)
	if len(decisions) == 0 || root["rationale"] == nil {
		t.Fatalf("root.json carries no decisions or rationale: %s", rootRaw)
	}
	first := decisions[0].(map[string]any)
	if first["id"] != "q1" || first["answered_by"] != "human" || first["answer"] != "yes" || first["raised_by"] != "clarify" {
		t.Errorf("first root decision = %v", first)
	}
	entries, _ := filepath.Glob(filepath.Join(g.runDir, "*", "component.json"))
	if len(entries) == 0 {
		t.Fatal("no component nodes written")
	}
	for _, e := range entries {
		raw, _ := os.ReadFile(e)
		if !strings.Contains(string(raw), `"decisions"`) || !strings.Contains(string(raw), `"rationale"`) {
			t.Errorf("%s lacks decisions or rationale", e)
		}
	}
}

func TestEnrichRepairsOnlyTheDefectiveNode(t *testing.T) {
	g, calls := enrichRig(t, dropGroup("fn-greet", "security", false))
	if _, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil {
		t.Fatal(err)
	}
	if calls.count("enrich:_fix") != 1 {
		t.Fatalf("repair calls = %d, want 1 (stages: %v)", calls.count("enrich:_fix"), calls.stages)
	}
	var fixPrompt string
	for i, s := range calls.stages {
		if s == "enrich:_fix" {
			fixPrompt = calls.prompts[i]
		}
	}
	if !strings.Contains(fixPrompt, "This is a repair") || !strings.Contains(fixPrompt, "fn-greet: group_missing") {
		t.Errorf("the repair prompt does not name the node and the defect:\n%s", fixPrompt)
	}
	if strings.Contains(fixPrompt, "fn-farewell") {
		t.Error("the repair prompt carries a node that passed")
	}
	for _, drafts := range g.drafts().Components {
		for _, d := range drafts {
			if d["id"] == "fn-greet" && d["security"] == nil {
				t.Error("the repaired node still has no security group")
			}
		}
	}
}

func TestEnrichFailsNamingTheNodeWhenTheRepairBoundIsSpent(t *testing.T) {
	g, calls := enrichRig(t, dropGroup("fn-greet", "security", true))
	_, err := g.plan(planner.Options{StopAfter: "enrich"})
	if err == nil || !strings.Contains(err.Error(), "fn-greet") || !strings.Contains(err.Error(), "group_missing") {
		t.Fatalf("err = %v, want the node and the defect kind", err)
	}
	if calls.count("enrich:_fix") != 2 {
		t.Errorf("repair calls = %d, want 2", calls.count("enrich:_fix"))
	}
}

func TestEnrichRejectsADecisionIdThatWasNeverSettled(t *testing.T) {
	respond := func(stage string, call int, nodes []enrichNode, _ string) string {
		out := make([]map[string]any, len(nodes))
		for i, n := range nodes {
			out[i] = goodEnrichment(n)
			if stage != "enrich:_fix" {
				out[i]["decision_ids"] = []any{"q99"}
			}
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	g, calls := enrichRig(t, respond)
	if _, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil {
		t.Fatal(err)
	}
	if calls.count("enrich:_fix") == 0 {
		t.Error("an unknown decision id was accepted")
	}
}

func TestEnrichDoesNotRepeatWhatAResumeAlreadyHas(t *testing.T) {
	failFarewell := func(stage string, call int, nodes []enrichNode, p string) string {
		if stage == "enrich:farewell" {
			return "this is not json"
		}
		return goodEnrichmentJSON(nodes)
	}
	g, first := enrichRig(t, failFarewell)
	if _, err := g.plan(planner.Options{StopAfter: "enrich"}); err == nil {
		t.Fatal("a stage with a reply nobody can read must fail")
	}
	second := withEnrichment(g, allGood)
	if out, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("resume = %v, %v", out, err)
	}
	for _, stage := range []string{"enrich:greeting", "enrich:types"} {
		if got := first.count(stage) + second.count(stage); got != 1 {
			t.Errorf("%s was asked %d times across the two runs, want 1", stage, got)
		}
	}
}

func TestEnrichQuestionPausesAsksAndAsksTheModelAgain(t *testing.T) {
	asked := 0
	respond := func(stage string, call int, nodes []enrichNode, prompt string) string {
		if stage == "enrich:greeting" && !strings.Contains(prompt, "Answer from the owner") {
			asked++
			return "QUESTION:\nShould Greet log refused names?"
		}
		return goodEnrichmentJSON(nodes)
	}
	g, calls := enrichRig(t, respond)
	gate := g.gate.(*scriptGate)
	gate.answer = func(q human.Question) string {
		if strings.HasPrefix(q.ID, "enrich-") {
			return "no, never log names"
		}
		return "yes"
	}
	if out, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	if asked != 1 || calls.count("enrich:greeting") != 2 {
		t.Errorf("model asked %d times, greeting calls = %d, want 1 and 2", asked, calls.count("enrich:greeting"))
	}
	found := false
	for _, q := range gate.asked {
		if strings.HasPrefix(q.ID, "enrich-greeting-q") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the gate was not asked an enrich-greeting question: %v", gate.rounds)
	}
	store := string(g.read("_state/clarify/questions.json"))
	if !strings.Contains(store, "enrich-greeting-q") || !strings.Contains(store, "enrich:greeting") {
		t.Errorf("the question is not in the store: %s", store)
	}
	if !g.has("decisions/enrich-greeting-q2.md") {
		t.Error("no decision record for the question Enrich raised")
	}
}

// enrichAbsentGate answers Clarify and refuses to answer a question an Enrich call raised.
type enrichAbsentGate struct{ *scriptGate }

func (g enrichAbsentGate) Ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	for _, q := range qs {
		if strings.HasPrefix(q.ID, "enrich") {
			return nil, human.ErrWaiting
		}
	}
	return g.scriptGate.Ask(ctx, qs)
}

func TestEnrichQuestionWithNobodyToAnswerWaitsAndResumesWithoutAskingTheModelAgain(t *testing.T) {
	var asked int
	respond := func(stage string, call int, nodes []enrichNode, prompt string) string {
		if stage == "enrich:greeting" && !strings.Contains(prompt, "Answer from the owner") {
			asked++
			return "QUESTION:\nShould Greet log refused names?"
		}
		return goodEnrichmentJSON(nodes)
	}
	g := newRig(t, enrichAbsentGate{approving()}, fixtureDir(t, map[string]string{"clarify.more.txt": "[]"}))
	withEnrichment(g, respond)
	out, err := g.plan(planner.Options{StopAfter: "enrich"})
	if err != nil || out != planner.Waiting {
		t.Fatalf("plan = %v, %v; want waiting", out, err)
	}
	g.gate = approving()
	g.deps.Gate = g.gate
	if out, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("resume = %v, %v", out, err)
	}
	if asked != 1 {
		t.Errorf("the model was asked the question %d times, want 1 (the resume asks the person, not the model)", asked)
	}
}

func TestEnrichStagesFollowTheNodeTier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		route bool
		want  string
	}{{"routing on", true, "any"}, {"routing off", false, "strong"}} {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := enrichRig(t, allGood)
			g.cfg.Defaults.RouteByNodeTier = &tc.route
			g.mustPlan(planner.Options{StopAfter: "enrich"})
			rows, err := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "enrich:greeting"})
			if err != nil || len(rows) == 0 {
				t.Fatalf("rows = %d, %v", len(rows), err)
			}
			for _, row := range rows {
				if row.Tier != tc.want {
					t.Errorf("enrich:greeting tier = %q, want %q", row.Tier, tc.want)
				}
			}
			comp, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "enrich_root"})
			for _, row := range comp {
				if row.Tier != "strong" {
					t.Errorf("enrich_root tier = %q, want strong", row.Tier)
				}
			}
		})
	}
}
