package planner_test

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

func greetReply(uses ...string) string {
	q := make([]string, len(uses))
	for i, u := range uses {
		q[i] = `"` + u + `"`
	}
	return `{"types": [], "functions": [
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go",
   "signature": "func Greet(name string) (string, error)", "doc": "Greet returns Hello, <name>!.", "uses": [` + strings.Join(q, ", ") + `]}
], "more": false}`
}

func repairStages(g *rig) int { return count(g.stagesCalled(), "contract:repair") }

// A function may use a function of a component that a later pass writes.
func TestComponentPassMayUseAFunctionOfALaterComponent(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": greetReply("name-error", "fn-farewell"),
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := repairStages(g); n != 0 {
		t.Errorf("repair calls = %d, want none: the reference resolves once every component is written", n)
	}
	if n := count(g.stagesCalled(), "contract:greeting"); n != 1 {
		t.Errorf("greeting calls = %d, want 1 (not retried as unusable)", n)
	}
	if len(g.contracts().Functions) != 3 {
		t.Errorf("functions = %d, want 3", len(g.contracts().Functions))
	}
}

const ghostFn = `{"types": [], "functions": [
  {"id": "fn-ghost", "package": "greet", "component": "greeting", "file": "internal/greet/ghost.go",
   "signature": "func Ghost() error", "doc": "Ghost was asked for by a repair pass.", "uses": []}
]}`

func TestADanglingFunctionReferenceIsRepairedInOnePass(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": greetReply("name-error", "fn-ghost"),
		"contract.repair.txt":   ghostFn,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := repairStages(g); n != 1 {
		t.Fatalf("repair calls = %d, want 1", n)
	}
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:repair" {
			p := r.Messages[1].Content
			if !strings.Contains(p, "<unresolved>\nfn-ghost (used by fn-greet in component greeting)\n</unresolved>") {
				t.Errorf("repair prompt lacks the unresolved id with its owner:\n%s", p)
			}
		}
	}
	c := g.contracts()
	owner := ""
	for _, f := range c.Functions {
		if f.ID == "fn-ghost" {
			owner = f.Component
		}
	}
	if owner != "greeting" {
		t.Errorf("fn-ghost component = %q, want greeting", owner)
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "contract"})
	n := 0
	for _, r := range rows {
		if r.Stage == "contract:repair" {
			n++
			if r.Outcome != ledger.OutcomeOK {
				t.Errorf("repair row outcome = %s", r.Outcome)
			}
		}
	}
	if n != 1 {
		t.Errorf("repair ledger rows = %d, want 1", n)
	}
}

func TestStillDanglingAfterTheRepairBoundNamesTheIDs(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": greetReply("name-error", "fn-ghost", "ghost CANARY words"),
		"contract.repair.txt":   `{"types": [], "functions": []}`,
		"contract.repair.2.txt": `{"types": [], "functions": []}`,
	}))
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), `"fn-ghost"`) || !strings.Contains(err.Error(), "2 repair passes") || !strings.Contains(err.Error(), "contract:repair") {
		t.Fatalf("err = %v, want the stage, the id and the bound", err)
	}
	if strings.Contains(err.Error(), "CANARY") || strings.Contains(err.Error(), "ghost C") {
		t.Errorf("err quotes text that fails the id syntax: %v", err)
	}
	if n := repairStages(g); n != 2 {
		t.Errorf("repair calls = %d, want 2", n)
	}
}

func TestComponentRepairPassesAreStoredAndNotReAsked(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": greetReply("name-error", "fn-ghost"),
		"contract.repair.txt":   `{"types": [], "functions": []}`,
		"contract.repair.2.txt": "the model fell over",
		"contract.repair.3.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the second repair to fail")
	}
	g.wire(variant(t, map[string]string{"contract.repair.txt": ghostFn}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := strings.Join(g.stagesCalled(), " "); got != "contract:repair" {
		t.Errorf("resume called %q, want only the unfinished repair", got)
	}
}

// A resume between components keeps the references open until every
// component is written.
func TestResumeBetweenComponentsDefersReferences(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": greetReply("name-error", "fn-farewell"),
		"contract.farewell.txt": "the model fell over", "contract.farewell.2.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the farewell pass to fail")
	}
	g.wire(variant(t, map[string]string{"contract.greeting.txt": greetReply("name-error", "fn-farewell")}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := strings.Join(g.stagesCalled(), " "); got != "contract:farewell" {
		t.Errorf("resume called %q, want only contract:farewell", got)
	}
}
