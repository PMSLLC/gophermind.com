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
