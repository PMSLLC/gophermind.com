package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
)

func dupWarnings(g *rig) []events.Event {
	var out []events.Event
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if strings.Contains(e.Message, "outline_duplicate_ignored") {
			out = append(out, e)
		}
	}
	return out
}

func greetingOther() string {
	return `{"id": "greeting", "package": "other", "exports": []}`
}

// Model noise: the same id twice with different content. The first emission
// wins, the later one is dropped and reported, the run goes on.
func TestOutlineDuplicateInOneReplyKeepsTheFirst(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt": `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `, ` + greetingOther() + `, ` + comp("farewell") + `], "types": [` + nameErrorType + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := componentIDs(g); got != "types greeting farewell" {
		t.Errorf("components = %q", got)
	}
	for _, c := range g.contracts().Components {
		if c.ID == "greeting" && c.Package != "greet" {
			t.Errorf("greeting package = %q, want the first emission (greet)", c.Package)
		}
	}
	w := dupWarnings(g)
	if len(w) != 1 || !strings.Contains(w[0].Message, `"greeting"`) || !strings.Contains(w[0].Message, "1 ") {
		t.Fatalf("warnings = %v, want one naming greeting and a count", w)
	}
	if !strings.Contains(string(g.read("_state/contract.json")), "greeting") {
		t.Error("the ignored id is not persisted in _state/contract.json")
	}
	if n := len(outlineStages(g)); n != 1 {
		t.Errorf("outline calls = %d, want 1 (no retry for noise)", n)
	}
}

func TestOutlineDuplicateAcrossPassesKeepsTheFirstAndResumeKeepsTheResult(t *testing.T) {
	first := `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `], "types": [` + nameErrorType + `], "more": true}`
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   first,
		"contract.outline.2.txt": `{"components": [` + greetingOther() + `, ` + comp("farewell") + `], "more": false}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := componentIDs(g); got != "types greeting farewell" {
		t.Errorf("components = %q", got)
	}
	for _, c := range g.contracts().Components {
		if c.ID == "greeting" && c.Package != "greet" {
			t.Errorf("greeting package = %q, want the first emission", c.Package)
		}
	}
	if len(dupWarnings(g)) != 1 {
		t.Errorf("warnings = %v", dupWarnings(g))
	}

	// Resume: pass 1 had a dropped duplicate of its own, pass 2 dies, then works.
	r := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `, ` + greetingOther() + `], "types": [` + nameErrorType + `], "more": true}`,
		"contract.outline.2.txt": "the model fell over", "contract.outline.3.txt": "and again",
	}))
	if _, err := r.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want pass 2 to fail")
	}
	r.wire(variant(t, map[string]string{"contract.outline.txt": `{"components": [` + comp("farewell") + `]}`}))
	r.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	for _, c := range r.contracts().Components {
		if c.ID == "greeting" && c.Package != "greet" {
			t.Errorf("after resume greeting package = %q", c.Package)
		}
	}
	if !strings.Contains(string(r.read("_state/contract.json")), "greeting") {
		t.Error("the ignored id was lost over the resume")
	}
}

// The warning carries the id (when it passes the id syntax) and a count, never
// content from the dropped emission.
func TestOutlineDuplicateWarningQuotesNoReplyText(t *testing.T) {
	dupType := `{"id": "name-error", "package": "greet", "file": "internal/greet/errors.go", "decl": "// CANARY-reply-text\ntype NameError int"}`
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt": `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `, ` + comp("farewell") + `], "types": [` + nameErrorType + `, ` + dupType + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	w := dupWarnings(g)
	if len(w) != 1 || strings.Contains(w[0].Message, "CANARY") || !strings.Contains(w[0].Message, `"name-error"`) {
		t.Fatalf("warnings = %v", w)
	}
	if strings.Contains(string(g.read("_state/contract.json")), "CANARY") {
		t.Error("reply text reached _state")
	}
}

func TestComponentPassDuplicateFunctionKeepsTheFirst(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": `{"types": [], "functions": [
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go", "signature": "func Greet(name string) (string, error)", "doc": "First.", "uses": ["name-error"]},
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go", "signature": "func Greet(name, other string) (string, error)", "doc": "Second.", "uses": ["name-error"]}
], "more": false}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	for _, f := range g.contracts().Functions {
		if f.ID == "fn-greet" && f.Doc != "First." {
			t.Errorf("fn-greet doc = %q, want the first emission", f.Doc)
		}
	}
	if w := dupWarnings(g); len(w) != 1 || !strings.Contains(w[0].Message, `"fn-greet"`) || strings.Contains(w[0].Message, "Second") {
		t.Errorf("warnings = %v", w)
	}
	if n := count(g.stagesCalled(), "contract:greeting"); n != 1 {
		t.Errorf("greeting calls = %d, want 1", n)
	}
}
