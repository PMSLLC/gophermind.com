package planner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
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
	if !strings.Contains(err.Error(), "_state/enriched.json") {
		t.Errorf("the error does not say how to reset: %v", err)
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

// A decision settled while the stage runs (a QUESTION: answered in the same
// run) can be cited by the reply that was shaped by it.
func TestEnrichReplyMayCiteTheDecisionItsOwnQuestionSettled(t *testing.T) {
	respond := func(stage string, call int, nodes []enrichNode, prompt string) string {
		if stage == "enrich:greeting" && !strings.Contains(prompt, "Answer from the owner") {
			return "QUESTION:\nShould Greet log refused names?"
		}
		out := make([]map[string]any, len(nodes))
		for i, n := range nodes {
			out[i] = goodEnrichment(n)
			if stage == "enrich:greeting" {
				out[i]["decision_ids"] = []any{"enrich-greeting-q2"}
			}
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	g, calls := enrichRig(t, respond)
	gate := g.gate.(*scriptGate)
	gate.answer = func(q human.Question) string {
		if strings.HasPrefix(q.ID, "enrich-") {
			return "no, never log names"
		}
		return "yes"
	}
	g.mustPlan(planner.Options{StopAfter: "approve"})
	if calls.count("enrich:_fix") != 0 {
		t.Errorf("the reply that cites its own decision needed %d repairs", calls.count("enrich:_fix"))
	}
	rec := string(g.read("decisions/enrich-greeting-q2.md"))
	if !strings.Contains(rec, "fn-greet") {
		t.Errorf("the decision record does not list the node that cites it:\n%s", rec)
	}
}

func staleEnriched(t *testing.T, g *rig, pending []map[string]any) {
	t.Helper()
	var st map[string]any
	if err := json.Unmarshal(g.read("_state/enriched.json"), &st); err != nil {
		t.Fatal(err)
	}
	st["done"], st["pending"] = false, pending
	raw, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(g.runDir, "_state", "enriched.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func planWithin(t *testing.T, g *rig, o planner.Options) (planner.Outcome, error) {
	t.Helper()
	type res struct {
		out planner.Outcome
		err error
	}
	ch := make(chan res, 1)
	go func() { out, err := g.plan(o); ch <- res{out, err} }()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(20 * time.Second):
		t.Fatal("the repair loop did not end")
		return "", nil
	}
}

func TestEnrichRepairEndsWhenAPendingEntryNamesAComponentThatIsGone(t *testing.T) {
	g, _ := enrichRig(t, allGood)
	g.mustPlan(planner.Options{StopAfter: "enrich"})
	staleEnriched(t, g, []map[string]any{{"component": "ghost", "id": "fn-greet", "defects": []string{"group_missing"}, "fields": []string{"security"}}})
	if out, err := planWithin(t, g, planner.Options{RunID: greeterID, StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("resume = %v, %v", out, err)
	}
}

func TestEnrichRepairDropsAPendingEntryWithNoDraftAndNeverCallsWithAnEmptyBatch(t *testing.T) {
	g, _ := enrichRig(t, allGood)
	g.mustPlan(planner.Options{StopAfter: "enrich"})
	staleEnriched(t, g, []map[string]any{{"component": "greeting", "id": "fn-nope", "defects": []string{"group_missing"}, "fields": []string{"security"}}})
	calls := withEnrichment(g, func(stage string, call int, nodes []enrichNode, prompt string) string {
		if len(nodes) == 0 {
			t.Errorf("the model was called with an empty batch in stage %s", stage)
		}
		return goodEnrichmentJSON(nodes)
	})
	if out, err := planWithin(t, g, planner.Options{RunID: greeterID, StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("resume = %v, %v", out, err)
	}
	if n := calls.count("enrich:_fix"); n != 0 {
		t.Errorf("%d repair calls for a node that has no draft", n)
	}
	if raw := g.read("_state/enriched.json"); !strings.Contains(string(raw), "fn-nope") || strings.Contains(string(raw), `"pending"`) {
		t.Errorf("the dropped entry should be noted in warnings and gone from pending: %s", raw)
	}
}

func TestEnrichKeepsOnlyKnownFieldsOfAPendingReply(t *testing.T) {
	const canary = "CANARY-raw-9921"
	respond := func(stage string, call int, nodes []enrichNode, _ string) string {
		out := make([]map[string]any, len(nodes))
		for i, n := range nodes {
			out[i] = goodEnrichment(n)
			if n.ID == "fn-greet" {
				delete(out[i], "security")
				out[i]["zz_extra"] = canary
			}
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	g, _ := enrichRig(t, respond)
	if _, err := g.plan(planner.Options{StopAfter: "enrich"}); err == nil {
		t.Fatal("a node that never gets its security group must fail the stage")
	}
	raw := g.read("_state/enriched.json")
	if strings.Contains(string(raw), canary) || !strings.Contains(string(raw), "fn-greet") {
		t.Errorf("enriched.json = %s", raw)
	}
}

func TestEnrichIgnoresANodeOutsideTheBatch(t *testing.T) {
	respond := func(stage string, call int, nodes []enrichNode, _ string) string {
		out := make([]map[string]any, 0, len(nodes)+1)
		for _, n := range nodes {
			out = append(out, goodEnrichment(n))
		}
		if stage == "enrich:greeting" {
			other := goodEnrichment(enrichNode{ID: "fn-farewell"})
			other["rationale"] = "SNEAKED in by the greeting call for a node of another component."
			out = append(out, other)
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	g, _ := enrichRig(t, respond)
	g.mustPlan(planner.Options{StopAfter: "enrich"})
	for _, d := range g.drafts().Components["farewell"] {
		if r, _ := d["rationale"].(string); strings.Contains(r, "SNEAKED") {
			t.Errorf("fn-farewell took a rationale from another component's call: %v", r)
		}
	}
}

// Two functions in one component, so that one Enrich batch holds two nodes.
var twoFunctionFiles = map[string]string{
	"contract.greeting.txt": `{"types": [], "functions": [
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go",
   "signature": "func Greet(name string) (string, error)",
   "doc": "Greet trims the name and returns Hello, <name>!. An empty name is an error.",
   "uses": ["name-error", "fn-name-error-error"]},
  {"id": "fn-welcome", "package": "greet", "file": "internal/greet/welcome.go",
   "signature": "func Welcome(name string) (string, error)",
   "doc": "Welcome trims the name and returns Welcome, <name>!. An empty name is an error.",
   "uses": ["name-error"]}
], "more": false}`,
	"decompose.greeting.txt": `[
  {"id": "fn-greet", "title": "Greet a name", "description": "Build the greeting for a name, refusing an empty one.",
   "model_tier": "any", "node_class": "validation", "depends_on": ["name-error", "fn-name-error-error"],
   "contract": {"package": "greet", "file": "internal/greet/greet.go", "signature": "func Greet(name string) (string, error)",
    "inputs": [{"name": "name", "type": "string", "constraints": ["May be empty"]}],
    "outputs": [{"name": "message", "type": "string", "description": "Hello, <name>!"}, {"name": "err", "type": "error", "description": "nil when usable"}],
    "errors": [{"when": "name is empty after trimming", "returns": "*NameError"}], "side_effects": []}},
  {"id": "fn-welcome", "title": "Welcome a name", "description": "Build the welcome for a name, refusing an empty one.",
   "model_tier": "any", "node_class": "validation", "depends_on": ["name-error"],
   "contract": {"package": "greet", "file": "internal/greet/welcome.go", "signature": "func Welcome(name string) (string, error)",
    "inputs": [{"name": "name", "type": "string", "constraints": ["May be empty"]}],
    "outputs": [{"name": "message", "type": "string", "description": "Welcome, <name>!"}, {"name": "err", "type": "error", "description": "nil when usable"}],
    "errors": [{"when": "name is empty after trimming", "returns": "*NameError"}], "side_effects": []}}
]`,
}

func TestATruncatedEnrichReplyHalvesTheBatchAndGoesOn(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, map[string]string{"clarify.more.txt": "[]"}), variant(t, twoFunctionFiles))
	var sizes []int
	calls := withEnrichmentErr(g, func(stage string, _ int, nodes []enrichNode, _ string) (string, error) {
		if stage == "enrich:greeting" {
			sizes = append(sizes, len(nodes))
			if len(nodes) > 1 {
				return "", provider.ErrTruncated{Provider: "fake"}
			}
		}
		return goodEnrichmentJSON(nodes), nil
	})
	if _, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil {
		t.Fatalf("a truncated reply must halve the batch, not fail the stage: %v", err)
	}
	if len(sizes) < 3 || sizes[0] != 2 || sizes[len(sizes)-1] != 1 || sizes[len(sizes)-2] != 1 {
		t.Errorf("batch sizes asked for = %v, want 2 (truncated) and then 1, 1", sizes)
	}
	if calls.count("enrich:_fix") != 0 {
		t.Errorf("repair calls = %d, want none", calls.count("enrich:_fix"))
	}
	enriched := 0
	for _, drafts := range g.drafts().Components {
		for _, d := range drafts {
			if d["rationale"] != nil {
				enriched++
			}
		}
	}
	if enriched != 4 {
		t.Errorf("%d nodes carry their groups, want 4", enriched)
	}
}

func TestATruncatedReplyForASingleNodeStillFails(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, map[string]string{"clarify.more.txt": "[]"}), variant(t, twoFunctionFiles))
	withEnrichmentErr(g, func(stage string, _ int, nodes []enrichNode, _ string) (string, error) {
		if stage == "enrich:greeting" {
			return "", provider.ErrTruncated{Provider: "fake"}
		}
		return goodEnrichmentJSON(nodes), nil
	})
	if _, err := g.plan(planner.Options{StopAfter: "enrich"}); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("err = %v, want the stage to fail naming the truncation once the batch is one node", err)
	}
}
