package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
)

const twoGreeters = `{"types": [], "functions": [
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go", "signature": "func Greet(name string) (string, error)", "doc": "Greet greets.", "uses": ["name-error"]},
  {"id": "fn-greet-twice", "package": "greet", "file": "internal/greet/twice.go", "signature": "func GreetTwice(name string, times int) (string, error)", "doc": "GreetTwice greets more than once.", "uses": ["name-error"]}
], "more": false}`

func nodeJSON(id, sig, outputs, description string) string {
	return `{"id": "` + id + `", "title": "T ` + id + `", "description": "` + description + `", "model_tier": "any", "node_class": "pure", "depends_on": ["name-error"],
 "contract": {"package": "greet", "file": "internal/greet/x.go", "signature": "` + sig + `",
  "inputs": [{"name": "name", "type": "string"}, {"name": "times", "type": "int"}],
  "outputs": ` + outputs + `,
  "errors": [{"when": "name is empty", "returns": "*NameError"}], "side_effects": []}}`
}

const (
	goodOutputs = `[{"name": "message", "type": "string"}, {"name": "err", "type": "error"}]`
	twiceSig    = "func GreetTwice(name string, times int) (string, error)"
)

// greetNode is the decompose reply for fn-greet (a good node, one parameter).
func greetNode() string {
	return `{"id": "fn-greet", "title": "Greet", "description": "Greets.", "model_tier": "any", "node_class": "pure", "depends_on": ["name-error"],
 "contract": {"package": "greet", "file": "internal/greet/greet.go", "signature": "func Greet(name string) (string, error)",
  "inputs": [{"name": "name", "type": "string"}], "outputs": ` + goodOutputs + `,
  "errors": [{"when": "name is empty", "returns": "*NameError"}], "side_effects": []}}`
}

func decomposeRig(t *testing.T, twice string, extra map[string]string) *rig {
	t.Helper()
	files := map[string]string{
		"contract.greeting.txt":  twoGreeters,
		"decompose.greeting.txt": "[" + greetNode() + "," + twice + "]",
	}
	for k, v := range extra {
		files[k] = v
	}
	return newRig(t, approving(), variant(t, files))
}

func leafWarnings(g *rig) []events.Event {
	var out []events.Event
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if strings.Contains(e.Message, "leaf_defaulted") {
			out = append(out, e)
		}
	}
	return out
}

// One node of the batch has no outputs: the batch is not discarded and only that
// node is asked for again, for the one field.
func TestOneDefectiveNodeIsRepairedAlone(t *testing.T) {
	g := decomposeRig(t, nodeJSON("fn-greet-twice", twiceSig, "[]", "Greets twice."), map[string]string{
		"decompose._fix.txt": `[{"id": "fn-greet-twice", "title": "CHANGED", "contract": {"outputs": ` + goodOutputs + `, "inputs": []}}, {"id": "fn-greet", "title": "HIJACK"}]`,
	})
	g.mustPlan(planner.Options{StopAfter: "decompose"})
	if got := count(g.stagesCalled(), "decompose:_fix"); got != 1 {
		t.Fatalf("repair calls = %d, want 1 (stages %v)", got, g.stagesCalled())
	}
	var p string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:_fix" {
			p = r.Messages[1].Content
		}
	}
	if !strings.Contains(p, `function "fn-greet-twice"`) || !strings.Contains(p, "outputs_count") || strings.Contains(p, `function "fn-greet"`) {
		t.Errorf("the repair prompt must name only the defective node and its defect:\n%.700s", p)
	}
	if !strings.Contains(p, twiceSig) {
		t.Error("the prompt lacks the contract signature")
	}
	st := string(g.read("_state/decomposed.json"))
	if !strings.Contains(st, `"fn-greet-twice"`) || strings.Contains(st, "CHANGED") || strings.Contains(st, "HIJACK") || strings.Contains(st, `"pending": [`) {
		t.Errorf("the repaired node must be stored with only the asked field replaced and nothing pending:\n%.500s", st)
	}
	if !strings.Contains(st, "T fn-greet-twice") {
		t.Error("the node's other fields were not kept")
	}
}

// The nodes that passed are stored at once. A failed repair leaves them, the
// pending node and the spent attempt in the state, and the resume asks only for
// the repair.
func TestGoodNodesAreKeptWhenTheRepairFailsAndResumeAsksOnlyTheRepair(t *testing.T) {
	g := decomposeRig(t, nodeJSON("fn-greet-twice", twiceSig, "[]", "Greets twice."), map[string]string{
		"decompose._fix.txt": "CANARY the model fell over", "decompose._fix.2.txt": "CANARY again",
	})
	_, err := g.plan(planner.Options{StopAfter: "decompose"})
	if err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err = %v, want a failure that quotes no reply text", err)
	}
	st := string(g.read("_state/decomposed.json"))
	if !strings.Contains(st, `"fn-greet"`) || !strings.Contains(st, `"pending": [`) || !strings.Contains(st, `"tries": 1`) {
		t.Fatalf("the good node, the pending node and the attempt must be stored:\n%.600s", st)
	}
	g.wire(variant(t, map[string]string{"decompose._fix.txt": `[{"id": "fn-greet-twice", "contract": {"outputs": ` + goodOutputs + `}}]`}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
	if got := strings.Join(g.stagesCalled(), " "); got != "decompose:_fix" {
		t.Errorf("resume called %q, want only the repair", got)
	}
}

// After the bound the structural fields come from the signature.
func TestStructuralFieldsAreDerivedAfterTheBound(t *testing.T) {
	g := decomposeRig(t, nodeJSON("fn-greet-twice", twiceSig, "[]", "Greets twice."), map[string]string{
		"decompose._fix.txt": `[]`, "decompose._fix.2.txt": `[]`,
	})
	g.mustPlan(planner.Options{StopAfter: "decompose"})
	if n := count(g.stagesCalled(), "decompose:_fix"); n != 2 {
		t.Errorf("repair calls = %d, want the bound of 2", n)
	}
	st := string(g.read("_state/decomposed.json"))
	for _, want := range []string{`"name": "result"`, `"description": "returned value"`, `"name": "err"`} {
		if !strings.Contains(st, want) {
			t.Errorf("derived outputs lack %s", want)
		}
	}
	w := leafWarnings(g)
	if len(w) != 1 || !strings.Contains(w[0].Message, "1 ") || !strings.Contains(w[0].Message, `"fn-greet-twice"`) {
		t.Errorf("warnings = %v", w)
	}
}

// A behaviour description is never invented.
func TestEmptyDescriptionAfterTheBoundFailsNamingTheNode(t *testing.T) {
	g := decomposeRig(t, nodeJSON("fn-greet-twice", twiceSig, goodOutputs, ""), map[string]string{
		"decompose._fix.txt": `[]`, "decompose._fix.2.txt": `[]`,
	})
	_, err := g.plan(planner.Options{StopAfter: "decompose"})
	if err == nil || !strings.Contains(err.Error(), `"fn-greet-twice"`) || !strings.Contains(err.Error(), "description") {
		t.Fatalf("err = %v, want the node and the defect word", err)
	}
	if !strings.Contains(string(g.read("_state/decomposed.json")), `"fn-greet"`) {
		t.Error("the node that passed was lost")
	}
}

// A reply that is not a JSON array at all is still an unusable reply.
func TestUnparseableDecomposeReplyIsStillUnusable(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": twoGreeters, "decompose.greeting.txt": "CANARY prose, no nodes",
	}))
	_, err := g.plan(planner.Options{StopAfter: "decompose"})
	if err == nil || strings.Contains(err.Error(), "CANARY") || !strings.Contains(err.Error(), "unusable") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(string(g.read("_state/decomposed.json")), `"pending": [`) {
		t.Error("an unparseable reply must not leave pending nodes")
	}
}
