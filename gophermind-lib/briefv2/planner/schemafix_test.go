package planner_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
)

func bigGreeting(n int) string {
	var fns []string
	for i := 0; i < n; i++ {
		fns = append(fns, fmt.Sprintf(`{"id": "fn-g%03d", "package": "greet", "file": "internal/greet/g%03d.go", "signature": "func G%03d() error", "doc": "G%03d does a thing.", "uses": ["name-error"]}`, i, i, i, i))
	}
	return `{"types": [], "functions": [` + strings.Join(fns, ",") + `], "more": false}`
}

// brokenRig plans a contract whose greeting component has 136 functions, then
// damages the stored state the way the final validation would see a gap: delete
// contracts.json and remove one field of function fn-g135.
func brokenRig(t *testing.T, field string, extra map[string]string) *rig {
	t.Helper()
	files := map[string]string{"contract.greeting.txt": bigGreeting(136)}
	g := newRig(t, approving(), variant(t, files))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	breakState(t, g, field)
	g.wire(variant(t, extra))
	return g
}

// breakState removes one field of fn-g135 from the stored state and deletes
// contracts.json, the way the final validation would find a gap.
func breakState(t *testing.T, g *rig, field string) {
	path := filepath.Join(g.runDir, "_state", "contract.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range st["doc"].(map[string]any)["functions"].([]any) {
		if m := f.(map[string]any); m["id"] == "fn-g135" {
			delete(m, field)
			found = true
		}
	}
	if !found {
		t.Fatal("fn-g135 not in the state")
	}
	out, _ := json.Marshal(st)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(g.runDir, "contracts.json")); err != nil {
		t.Fatal(err)
	}
}

func fnByID(g *rig, id string) (doc, sig string) {
	for _, f := range g.contracts().Functions {
		if f.ID == id {
			return f.Doc, f.Signature
		}
	}
	return "<missing>", ""
}

// One function of 136 lacks its doc: only that id is asked again, and only the
// missing field is taken from the reply.
func TestOneMissingDocIsRepairedAskingOnlyThatID(t *testing.T) {
	g := brokenRig(t, "doc", map[string]string{
		"contract._schema.txt": `{"types": [], "functions": [{"id": "fn-g135", "doc": "G135 is the last one.", "signature": "func Changed() error", "file": "elsewhere.go"}, {"id": "fn-g000", "doc": "hijack"}]}`,
	})
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := strings.Join(g.stagesCalled(), " "); got != "contract:_schema" {
		t.Fatalf("calls = %q, want only the schema repair", got)
	}
	p := g.fake.Requests()[0].Messages[1].Content
	if !strings.Contains(p, `"fn-g135"`) || strings.Contains(p, "fn-g000") || !strings.Contains(p, "doc") {
		t.Errorf("repair prompt does not name only fn-g135 and its missing field:\n%.600s", p)
	}
	doc, sig := fnByID(g, "fn-g135")
	if doc != "G135 is the last one." || sig != "func G135() error" {
		t.Errorf("doc %q signature %q, want the doc replaced and the signature kept", doc, sig)
	}
	if d, _ := fnByID(g, "fn-g000"); d != "G000 does a thing." {
		t.Errorf("a node that was not asked for changed: %q", d)
	}
}

func TestDocStillMissingAfterTheBoundIsDefaultedWithAWarning(t *testing.T) {
	g := brokenRig(t, "doc", map[string]string{
		"contract._schema.txt": `{"functions": []}`, "contract._schema.2.txt": `{"functions": []}`,
	})
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if n := count(g.stagesCalled(), "contract:_schema"); n != 2 {
		t.Errorf("schema repair calls = %d, want the bound of 2", n)
	}
	if doc, _ := fnByID(g, "fn-g135"); doc != "G135 implements greeting behaviour described in the brief." {
		t.Errorf("doc = %q", doc)
	}
	var w []events.Event
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if strings.Contains(e.Message, "doc_defaulted") {
			w = append(w, e)
		}
	}
	if len(w) != 1 || !strings.Contains(w[0].Message, "1 ") || !strings.Contains(w[0].Message, `"fn-g135"`) {
		t.Errorf("warnings = %v", w)
	}
}

// A field that is not descriptive is never invented: the stage fails naming
// the node and the field.
func TestMissingSignatureFailsNamingTheNodeAndKeepsTheState(t *testing.T) {
	g := brokenRig(t, "signature", map[string]string{
		"contract._schema.txt": `{"functions": []}`, "contract._schema.2.txt": `{"functions": []}`,
	})
	_, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), `function "fn-g135"`) || !strings.Contains(err.Error(), "signature") || strings.Contains(err.Error(), "/functions/") {
		t.Fatalf("err = %v, want the node id and the field, not a pointer", err)
	}
	if g.has("contracts.json") {
		t.Error("contracts.json was written for an invalid contract")
	}
	st := string(g.read("_state/contract.json"))
	if !strings.Contains(st, `"fn-g134"`) || !strings.Contains(st, `"schema_repairs": 2`) {
		t.Errorf("the merged contract or the repair count is not in the state:\n%.300s", st)
	}
}

// A failure at the end leaves every pass stored: the resume runs the repair
// and nothing else, and the failed attempt counted.
func TestResumeAfterAFinalValidationFailureRunsOnlyTheRepair(t *testing.T) {
	g := brokenRig(t, "doc", map[string]string{
		"contract._schema.txt": "CANARY the model fell over", "contract._schema.2.txt": "CANARY and again",
	})
	_, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err = %v, want a failure that quotes no reply text", err)
	}
	if g.has("contracts.json") {
		t.Fatal("contracts.json exists")
	}
	g.wire(variant(t, map[string]string{"contract._schema.txt": `{"functions": [{"id": "fn-g135", "doc": "Fixed."}]}`}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := strings.Join(g.stagesCalled(), " "); got != "contract:_schema" {
		t.Errorf("resume called %q, want only the repair", got)
	}
	if doc, _ := fnByID(g, "fn-g135"); doc != "Fixed." {
		t.Errorf("doc = %q", doc)
	}
}
