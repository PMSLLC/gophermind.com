package planner_test

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

func (g *rig) contracts() *contract.Contracts {
	g.t.Helper()
	c, err := contract.Load(g.read("contracts.json"))
	if err != nil {
		g.t.Fatalf("contracts.json: %v", err)
	}
	return c
}

func TestContractIsBuiltInPassesAndValidated(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})

	if got := strings.Join(g.stagesCalled(), " "); got != "clarify contract:outline contract:types contract:greeting contract:farewell" {
		t.Errorf("calls = %s", got)
	}
	c := g.contracts()
	if c.BriefID != greeterID || c.Module != "example.com/greeter" || len(c.Types) != 1 || len(c.Functions) != 3 || len(c.Components) != 3 {
		t.Fatalf("contract = %d types, %d functions, %d components", len(c.Types), len(c.Functions), len(c.Components))
	}
	owner := map[string]string{}
	for _, f := range c.Functions {
		owner[f.ID] = f.Component
	}
	if owner["fn-name-error-error"] != "types" || owner["fn-greet"] != "greeting" || owner["fn-farewell"] != "farewell" {
		t.Errorf("function owners = %v (the harness sets component from the pass)", owner)
	}
	for _, comp := range c.Components {
		if len(comp.Exports) != 1 {
			t.Errorf("component %s exports %v, want its one function (filled by the harness)", comp.ID, comp.Exports)
		}
	}

	// The component pass sees what earlier passes declared, and the answers.
	var greeting string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:greeting" {
			greeting = r.Messages[1].Content
		}
	}
	for _, want := range []string{"fn-name-error-error: func (e *NameError) Error() string", "type NameError struct", "### Greeting", "A: yes", "(none yet)"} {
		if !strings.Contains(greeting, want) {
			t.Errorf("contract:greeting prompt lacks %q", want)
		}
	}

	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "contract"})
	if len(rows) != 4 {
		t.Fatalf("contract rows = %d, want 4", len(rows))
	}
	if rows[0].Stage != "contract:outline" || rows[0].Scope != "brief" || rows[1].Stage != "contract:types" || rows[1].Scope != "component" {
		t.Errorf("rows = %s/%s then %s/%s", rows[0].Stage, rows[0].Scope, rows[1].Stage, rows[1].Scope)
	}
}

// A component reply that ends with "more": true is continued: the same stage
// is called again, told what is already written, and both replies are merged.
func TestContractContinuesAComponentWhileMoreIsTrue(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": `{"types": [], "functions": [
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go",
   "signature": "func Greet(name string) (string, error)", "doc": "Greet returns Hello, <name>!.", "uses": ["name-error"]}
], "more": true}`,
		"contract.greeting.2.txt": `{"types": [
  {"id": "shout-mode", "package": "greet", "file": "internal/greet/shout.go", "decl": "// ShoutMode says how loud a greeting is.\ntype ShoutMode int"}
], "functions": [
  {"id": "fn-greet-loud", "package": "greet", "file": "internal/greet/shout.go",
   "signature": "func GreetLoud(name string, mode ShoutMode) (string, error)", "doc": "GreetLoud is Greet in capitals.", "uses": ["fn-greet", "shout-mode"]}
], "more": false}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})

	calls := g.stagesCalled()
	if count(calls, "contract:greeting") != 2 || count(calls, "contract:types") != 1 || count(calls, "contract:farewell") != 1 {
		t.Fatalf("calls = %v, want two for greeting and one for each other component", calls)
	}
	n := 0
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) != "contract:greeting" {
			continue
		}
		n++
		written := strings.Contains(r.Messages[1].Content, "<written>\nfn-greet\n</written>")
		if n == 1 && written || n == 2 && !written {
			t.Errorf("call %d for greeting: already-written list is wrong", n)
		}
	}
	c := g.contracts()
	if len(c.Functions) != 4 || len(c.Types) != 2 {
		t.Fatalf("contract = %d functions and %d types, want 4 and 2 (both passes merged)", len(c.Functions), len(c.Types))
	}
	for _, comp := range c.Components {
		if comp.ID == "greeting" && strings.Join(comp.Exports, " ") != "fn-greet fn-greet-loud" {
			t.Errorf("greeting exports = %v", comp.Exports)
		}
	}
}

// Finished components are kept in _state/contract.json, so a run that died
// in the middle of the contract continues from the next component.
func TestContractResumeSkipsFinishedComponents(t *testing.T) {
	broken := variant(t, map[string]string{"contract.farewell.txt": "the model fell over", "contract.farewell.2.txt": "and again"})
	g := newRig(t, approving(), broken)
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil || !strings.Contains(err.Error(), "contract") {
		t.Fatalf("err = %v, want the contract stage to fail on farewell", err)
	}
	if g.has("contracts.json") {
		t.Fatal("contracts.json was written before every pass finished")
	}

	g.wire()
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := strings.Join(g.stagesCalled(), " "); got != "contract:farewell" {
		t.Errorf("resume called %q, want only contract:farewell", got)
	}
	if len(g.contracts().Functions) != 3 {
		t.Error("the resumed contract is incomplete")
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Stages {
		if want := s.Name == "load" || s.Name == "clarify" || s.Name == "contract"; s.Done != want {
			t.Errorf("stage %s done = %v, want %v", s.Name, s.Done, want)
		}
	}
}
