package planner_test

import (
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

// A csvstat-shaped contract: one component with a type from the outline and
// two functions written in one component pass.
var csvstatFiles = map[string]string{
	"contract.outline.txt": `{"module": "example.com/csvstat",
 "conventions": {"layout": ["internal/stat"], "naming": ["Verbs."], "errors": "Return errors.", "testing": "Table-driven."},
 "components": [{"id": "csvstat", "package": "stat"}],
 "types": [{"id": "summary", "package": "stat", "file": "internal/stat/summary.go", "decl": "// Summary holds column statistics.\ntype Summary struct {\n\tMean float64\n}"}]}`,
	"contract.csvstat.txt": `{"types": [], "functions": [
  {"id": "fn-mean", "package": "stat", "file": "internal/stat/mean.go", "signature": "func Mean(xs []float64) (float64, error)", "doc": "Mean averages xs. An empty slice is an error.", "uses": []},
  {"id": "fn-summarize", "package": "stat", "file": "internal/stat/summary.go", "signature": "func Summarize(xs []float64) (Summary, error)", "doc": "Summarize builds a Summary using Mean.", "uses": ["summary", "fn-mean"]}
], "more": false}`,
}

// The golden files were recorded from the planner at 682d332, before the
// outline passes and the deferred reference checks existed (extracted with
// `git archive 682d332 | tar -x`, run there, sha256 equal to the committed
// files): csvstat.contracts.json 753c2a4b1cda2374fa4d0de6651ff3c8180dd35778fb1d83cd65774fc9711512,
// csvstat.dependencies.json 37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570.
func TestSingleComponentContractWritesThePreWaveBytes(t *testing.T) {
	g := newRig(t, approving(), variant(t, csvstatFiles))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := string(g.read("contracts.json")); got != readGolden(t, "csvstat.contracts.json") {
		t.Error("csvstat contracts.json differs from the pre-wave bytes")
	}
	if got := string(g.read("dependencies.json")); got != readGolden(t, "csvstat.dependencies.json") {
		t.Errorf("dependencies.json = %q", got)
	}
}
