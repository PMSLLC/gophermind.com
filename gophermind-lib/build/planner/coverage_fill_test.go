package planner

import (
	"strings"
	"testing"
)

// Run hello-v1.1-20261106-r007: the fill round declared Greeting and Run a
// second time under new ids to close literal gaps. A fill function whose name
// and package an existing function already has is refused.
func TestCoverageFillRefusesADuplicateFunction(t *testing.T) {
	reqs := []Requirement{{ID: "F1", Kind: ReqFeature, Name: "Greeting", Text: "t", Line: 1}}
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	base["functions"] = []any{map[string]any{"id": "fn-greeting", "package": "x", "file": "internal/x/greet.go",
		"signature": "func Greeting(name string) string", "doc": "d", "uses": []any{}, "component": "greeting"}}
	reply := func(id, sig string) string {
		return `{"map": [{"requirement": "F1", "nodes": ["` + id + `"]}], "functions": [{"id": "` + id + `", "package": "x", "file": "internal/x/empty.go", "signature": "` + sig + `", "doc": "d", "uses": [], "component": "greeting"}]}`
	}
	_, _, err = parseCoverageFill(reply("fn-greeting-empty", "func Greeting(name string) string"), reqs, base, testRunID)
	if err == nil || !strings.Contains(err.Error(), "duplicates fn-greeting") {
		t.Errorf("duplicate: err = %v, want duplicates fn-greeting", err)
	}
	if _, _, err := parseCoverageFill(reply("fn-greet", "func Greet(name string) string"), reqs, base, testRunID); err != nil {
		t.Errorf("new name: err = %v, want none", err)
	}
}

// Run hello-v1.1-20261106-r008: fn-run's doc did not state the usage line a
// feature requires, and the fill reply had no way to say it. A fill reply may
// restate a function's doc; the contract it returns carries the new doc.
func TestCoverageFillRestatesADoc(t *testing.T) {
	reqs := []Requirement{{ID: "F2", Kind: ReqFeature, Name: "Usage", Text: "t", Line: 29}}
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	base["components"] = append(base["components"].([]any), map[string]any{"id": "cli", "package": "cli", "exports": []any{}})
	base["functions"] = []any{map[string]any{"id": "fn-run", "package": "cli", "file": "internal/cli/run.go",
		"signature": "func Run(args []string, stdout, stderr io.Writer) int", "doc": "Handles arguments and writes greetings to stdout and usage errors to stderr.",
		"uses": []any{}, "component": "cli"}}
	rev, _ := base["revision"].(int)
	want := "Writes exactly usage: hello [NAME] to stderr and returns 2 with more than one argument."
	reply := func(id, doc string) string {
		return `{"map": [], "root_tests": [], "docs": [{"id": "` + id + `", "doc": "` + doc + `"}]}`
	}
	_, cp, err := parseCoverageFill(reply("fn-run", want), reqs, base, testRunID)
	if err != nil || cp == nil {
		t.Fatalf("restate: contract %v, err %v", cp, err)
	}
	fns := objects(cp["functions"])
	if len(fns) != 1 || strField(fns[0], "doc") != want {
		t.Errorf("functions = %v, want fn-run with the new doc", fns)
	}
	if got, _ := cp["revision"].(int); got != rev+1 {
		t.Errorf("revision = %v, want %d", cp["revision"], rev+1)
	}
	if strField(objects(base["functions"])[0], "doc") == want {
		t.Error("the given contract was changed in place")
	}
	if _, _, err := parseCoverageFill(reply("fn-nope", want), reqs, base, testRunID); err == nil || !strings.Contains(err.Error(), "unknown function fn-nope") {
		t.Errorf("unknown id: err = %v, want unknown function fn-nope", err)
	}
	if _, _, err := parseCoverageFill(reply("fn-run", " "), reqs, base, testRunID); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("empty doc: err = %v, want is empty", err)
	}
}

// The gap list puts the id on a line of its own, so the model cites the id
// and not the whole line.
func TestGapsAreListedById(t *testing.T) {
	got := gapsText([]Gap{{Requirement: "F2", Line: 29, Text: "With more than one argument it writes `usage: hello [NAME]\\n` to stderr.",
		Reason: "behaviour missing: usage: hello [NAME]\\n"}})
	if !strings.HasPrefix(got, "id: F2\n  text: ") {
		t.Errorf("gaps text = %q, want it to start with the id line", got)
	}
	if !strings.Contains(got, "\n  reason: behaviour missing:") {
		t.Errorf("gaps text = %q, want the reason on its own line", got)
	}
	if gapsText(nil) != "none" {
		t.Errorf("no gaps = %q, want none", gapsText(nil))
	}
}
