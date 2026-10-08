package planner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/tree"
)

type draftFile struct {
	Components map[string][]map[string]any `json:"components"`
	Done       bool                        `json:"done"`
}

func (g *rig) drafts() draftFile {
	g.t.Helper()
	var d draftFile
	if err := json.Unmarshal(g.read("_state/decomposed.json"), &d); err != nil {
		g.t.Fatal(err)
	}
	return d
}

func strs(v any) []string {
	var out []string
	arr, _ := v.([]any)
	for _, x := range arr {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func TestDecomposeWritesDraftsClassesAndTheSkeleton(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "decompose"})

	d := g.drafts()
	if !d.Done || len(d.Components["types"]) != 1 || len(d.Components["greeting"]) != 1 || len(d.Components["farewell"]) != 1 {
		t.Fatalf("decomposed.json = done %v, %d components", d.Done, len(d.Components))
	}
	greet := d.Components["greeting"][0]
	if greet["kind"] != "function" || greet["parent"] != "greeting" || greet["status"] != "pending" ||
		greet["brief_ref"] != "#features/greeting" || greet["spec_version"] != "2.0" {
		t.Errorf("harness-owned fields on fn-greet = %v", greet)
	}
	for _, k := range []string{"node_class", "tests", "wave"} {
		if _, ok := greet[k]; ok {
			t.Errorf("draft still carries %q", k)
		}
	}
	// depends_on keeps node ids only; the type reaches the node as a signature.
	if got := strs(greet["depends_on"]); len(got) != 1 || got[0] != "fn-name-error-error" {
		t.Errorf("fn-greet depends_on = %v, want [fn-name-error-error]", got)
	}
	ctx := greet["context"].(map[string]any)
	sigs := strings.Join(strs(ctx["dependency_signatures"]), "\n")
	if !strings.Contains(sigs, "type NameError struct") || !strings.Contains(sigs, "func (e *NameError) Error() string") {
		t.Errorf("fn-greet dependency_signatures = %q", sigs)
	}
	if got := strs(ctx["constraints"]); len(got) != 2 || got[0] != "Standard library only." {
		t.Errorf("fn-greet constraints = %v, want the brief's two", got)
	}
	// The fixture's farewell draft has the wrong signature; the contract's wins.
	fw := d.Components["farewell"][0]["contract"].(map[string]any)
	if fw["signature"] != "func Farewell(name string) (string, error)" {
		t.Errorf("fn-farewell signature = %v, want the contract's", fw["signature"])
	}

	var classes map[string]string
	if err := json.Unmarshal(g.read("_state/classes.json"), &classes); err != nil {
		t.Fatal(err)
	}
	if classes["fn-greet"] != "validation" || classes["fn-name-error-error"] != "pure" || len(classes) != 3 {
		t.Errorf("classes.json = %v", classes)
	}

	// Root and components are real tree nodes already; function nodes are not written yet.
	tr, err := tree.NewStore(g.runDir).Load()
	if err != nil {
		t.Fatalf("the run folder must load as a tree with its working files in place: %v", err)
	}
	if len(tr.Nodes) != 4 {
		t.Errorf("tree has %d nodes, want the root and three components", len(tr.Nodes))
	}
	if err := tr.CheckStructure(); err != nil {
		t.Error(err)
	}
	if kids := tr.Nodes["greeting"].Children; len(kids) != 1 || kids[0] != "fn-greet" {
		t.Errorf("greeting children = %v", kids)
	}
	var comp map[string]any
	if err := json.Unmarshal(g.read("greeting/component.json"), &comp); err != nil {
		t.Fatal(err)
	}
	if tests := comp["tests"].([]any); len(tests) != 1 || tests[0].(map[string]any)["level"] != "integration" {
		t.Errorf("greeting tests = %v, want the contract's integration test", comp["tests"])
	}
	if comp["title"] != "Greeting" || comp["brief_ref"] != "#features/greeting" {
		t.Errorf("greeting title and ref = %v, %v", comp["title"], comp["brief_ref"])
	}

	nodes, err := planner.PlanNodes(g.runDir)
	if err != nil || len(nodes) != 7 {
		t.Fatalf("PlanNodes = %d, %v; want 7", len(nodes), err)
	}
	if nodes[0].Kind != tree.KindRoot || nodes[0].Title != "Greeter" || nodes[6].File != "internal/greet/farewell.go" {
		t.Errorf("PlanNodes = %+v", nodes)
	}

	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "decompose"})
	if len(rows) != 3 || rows[0].Scope != "component" || rows[0].Stage != "decompose:types" {
		t.Errorf("decompose rows = %d (%+v)", len(rows), rows)
	}
}

// nineFunctions builds a fixture whose greeting component has nine functions,
// so Decompose has to make two calls for it (eight, then one).
func nineFunctions(t *testing.T) string {
	var fns, first, second []string
	for i := 1; i <= 9; i++ {
		id := fmt.Sprintf("fn-greet-%d", i)
		sig := fmt.Sprintf("func Greet%d(name string) string", i)
		fns = append(fns, fmt.Sprintf(`{"id": %q, "package": "greet", "file": "internal/greet/greet%d.go", "signature": %q, "doc": "Greeting number %d.", "uses": []}`, id, i, sig, i))
		draft := fmt.Sprintf(`{"id": %q, "title": "Greeting %d", "description": "Greeting number %d.", "node_class": "pure", "depends_on": [],
 "contract": {"inputs": [{"name": "name", "type": "string"}], "outputs": [{"name": "message", "type": "string"}], "errors": [], "side_effects": []}}`, id, i, i)
		if i <= 8 {
			first = append(first, draft)
		} else {
			second = append(second, draft)
		}
	}
	return variant(t, map[string]string{
		"contract.greeting.txt":    `{"types": [], "functions": [` + strings.Join(fns, ",\n") + `], "more": false}`,
		"decompose.greeting.txt":   "[" + strings.Join(first, ",\n") + "]",
		"decompose.greeting.2.txt": "[" + strings.Join(second, ",\n") + "]",
	})
}

func TestDecomposeBatchesALargeComponent(t *testing.T) {
	g := newRig(t, approving(), nineFunctions(t))
	g.mustPlan(planner.Options{StopAfter: "decompose"})
	if n := count(g.stagesCalled(), "decompose:greeting"); n != 2 {
		t.Fatalf("decompose:greeting was called %d times, want 2 (eight functions, then one)", n)
	}
	var asked []int
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:greeting" {
			asked = append(asked, strings.Count(r.Messages[1].Content, `"id": "fn-greet-`))
		}
	}
	if len(asked) != 2 || asked[0] != 8 || asked[1] != 1 {
		t.Errorf("functions per call = %v, want [8 1]", asked)
	}
	if got := len(g.drafts().Components["greeting"]); got != 9 {
		t.Errorf("greeting has %d drafts, want 9", got)
	}
}

const question = "QUESTION:\nShould Greet capitalise the name?\n"

// A QUESTION: reply pauses that one call: the person is asked, the answer is
// stored, and only that call is made again, with the answer in its prompt.
func TestAQuestionPausesOneCallAndRerunsOnlyThatCall(t *testing.T) {
	greeting, err := os.ReadFile(filepath.Join(greeter, "decompose.greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	gate := approving()
	gate.answer = func(q human.Question) string {
		if strings.Contains(q.Text, "capitalise") {
			return "No, keep it as typed."
		}
		return "yes"
	}
	g := newRig(t, gate, variant(t, map[string]string{
		"decompose.greeting.txt":   question,
		"decompose.greeting.2.txt": string(greeting),
	}))
	g.mustPlan(planner.Options{StopAfter: "decompose"})

	calls := g.stagesCalled()
	if count(calls, "decompose:greeting") != 2 || count(calls, "decompose:types") != 1 || count(calls, "decompose:farewell") != 1 {
		t.Fatalf("calls = %v, want the greeting call twice and the others once", calls)
	}
	as := g.answers()
	last := as[len(as)-1]
	if last.Stage != "decompose:greeting" || last.Question != "Should Greet capitalise the name?" || last.Answer != "No, keep it as typed." {
		t.Errorf("stored answer = %+v", last)
	}
	var second string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:greeting" {
			second = r.Messages[1].Content
		}
	}
	if !strings.Contains(second, "Answer from the owner to your question:\nShould Greet capitalise the name?\nNo, keep it as typed.") {
		t.Error("the rerun prompt does not carry the answer")
	}
	if g.has("_state/question.json") {
		t.Error("the pending question was not cleared once answered")
	}
	st := readStore(t, g.runDir)
	var mid *storedQ
	for i := range st.Questions {
		if strings.HasPrefix(st.Questions[i].ID, "decompose-") {
			mid = &st.Questions[i]
		}
	}
	if mid == nil || mid.Status != "settled" || mid.AnsweredBy != "human" {
		t.Fatalf("the mid-stage question was not recorded in the store: %+v", st.Questions)
	}
	if _, err := os.Stat(filepath.Join(g.runDir, "decisions", mid.ID+".md")); err != nil {
		t.Errorf("no decision record for the mid-stage question: %v", err)
	}
}

func TestAQuestionInAssumeModeGoesBackToTheModel(t *testing.T) {
	greeting, _ := os.ReadFile(filepath.Join(greeter, "decompose.greeting.txt"))
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{
		"decompose.greeting.txt":   question,
		"decompose.greeting.2.txt": string(greeting),
	}))
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		return strings.Replace(s, "on_ambiguity: halt", "on_ambiguity: assume_and_document", 1)
	})
	g.mustPlan(planner.Options{StopAfter: "decompose"})
	if len(gate.asked) != 0 {
		t.Errorf("the gate was asked %d questions in assume mode", len(gate.asked))
	}
	var second string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:greeting" {
			second = r.Messages[1].Content
		}
	}
	if !strings.Contains(second, "No human is available. Choose the most conservative option") {
		t.Error("the rerun prompt does not tell the model to assume")
	}
	var mid *storedQ
	st := readStore(t, g.runDir)
	for i := range st.Questions {
		if strings.HasPrefix(st.Questions[i].ID, "decompose-") {
			mid = &st.Questions[i]
		}
	}
	if mid == nil || mid.Status != "settled" || mid.AnsweredBy != "unattended-default" {
		t.Fatalf("a mid-stage question in assume mode must be stored as a settled unattended answer: %+v", st.Questions)
	}
	if as := g.answers(); len(as) == 0 || !as[len(as)-1].Assumed {
		t.Errorf("answers.json does not show the assumption: %+v", as)
	}
}

// With a gate nobody has answered, the question is kept, the run waits, and
// the resume asks the person, not the model, for it.
func TestAPendingQuestionSurvivesAResume(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{"decompose.greeting.txt": question}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	gate.err = human.ErrWaiting
	out, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
	if err != nil || out != planner.Waiting {
		t.Fatalf("Run = %q, %v; want waiting", out, err)
	}
	if !g.has("_state/question.json") {
		t.Fatal("the question was not kept for the resume")
	}

	gate.err = nil
	gate.answer = func(q human.Question) string { return "No." }
	g.wire() // the real reply this time
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
	calls := g.stagesCalled()
	if count(calls, "decompose:greeting") != 1 || count(calls, "decompose:types") != 0 {
		t.Errorf("resume calls = %v, want one decompose:greeting and nothing already done", calls)
	}
	if !strings.Contains(g.fake.Requests()[0].Messages[1].Content, "Should Greet capitalise the name?\nNo.") {
		t.Error("the resumed call does not carry the answer")
	}
}

func TestDecomposeResumeSkipsFinishedComponents(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"decompose.farewell.txt": "nope", "decompose.farewell.2.txt": "nope"}))
	if _, err := g.plan(planner.Options{StopAfter: "decompose"}); err == nil {
		t.Fatal("decompose must fail on the broken farewell reply")
	}
	g.wire()
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
	if got := strings.Join(g.stagesCalled(), " "); got != "decompose:farewell" {
		t.Errorf("resume called %q, want only decompose:farewell", got)
	}
}

// Two functions that use each other pass the contract (a cycle in uses is
// legal Go), so the plan must break one edge instead of failing after every
// Decompose call, on every resume.
func TestFunctionsThatUseEachOtherStillPlan(t *testing.T) {
	fn := func(id, name, file, uses string) string {
		return `{"id": "` + id + `", "package": "greet", "file": "internal/greet/` + file + `",
   "signature": "func ` + name + `(name string) (string, error)", "doc": "d", "uses": ["name-error", "` + uses + `"]}`
	}
	draft := func(id, title, name, file string) string {
		return `{"id": "` + id + `", "title": "` + title + `", "description": "d.", "model_tier": "any", "node_class": "validation", "depends_on": [],
    "contract": {"package": "greet", "file": "internal/greet/` + file + `", "signature": "func ` + name + `(name string) (string, error)",
      "inputs": [{"name": "name", "type": "string"}],
      "outputs": [{"name": "message", "type": "string"}, {"name": "err", "type": "error"}],
      "errors": [{"when": "name is empty", "returns": "*NameError"}], "side_effects": []}}`
	}
	dir := variant(t, map[string]string{
		"contract.greeting.txt": `{"types": [], "functions": [` + fn("fn-greet", "Greet", "greet.go", "fn-farewell") + `,` +
			fn("fn-farewell", "Farewell", "farewell.go", "fn-greet") + `], "more": false}`,
		"contract.farewell.txt": `{"types": [], "functions": [], "more": false}`,
		"decompose.greeting.txt": "```json\n[" + draft("fn-greet", "Greet", "Greet", "greet.go") + "," +
			draft("fn-farewell", "Farewell", "Farewell", "farewell.go") + "]\n```",
	})
	g := newRig(t, approving(), dir)
	g.mustPlan(planner.Options{StopAfter: "decompose"})

	d := g.drafts()
	if !d.Done {
		t.Fatal("decomposed.json is not done")
	}
	edges := 0
	for _, drafts := range d.Components {
		for _, n := range drafts {
			for _, dep := range strs(n["depends_on"]) {
				if dep == "fn-greet" || dep == "fn-farewell" {
					edges++
				}
			}
		}
	}
	if edges != 1 {
		t.Errorf("%d edges between the two functions, want exactly 1 (one back edge dropped)", edges)
	}
	// A resume must not fail again.
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
}
