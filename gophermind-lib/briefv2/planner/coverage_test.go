package planner_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

const (
	greeterGap   = "testdata/greeter-gap"
	greeterStuck = "testdata/greeter-stuck"
)

func (g *rig) rootTests() []map[string]any {
	g.t.Helper()
	var root struct {
		Tests []map[string]any `json:"tests"`
	}
	if err := json.Unmarshal(g.read("root.json"), &root); err != nil {
		g.t.Fatal(err)
	}
	return root.Tests
}

func TestCoverageMapsEveryRequirement(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "coverage"})

	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Rounds != 0 || len(cov.Covered) != 7 || len(cov.RootTests) != 4 || len(cov.Warnings) != 0 {
		t.Fatalf("coverage.json = %d rounds, %d covered, %d root tests, warnings %q", cov.Rounds, len(cov.Covered), len(cov.RootTests), cov.Warnings)
	}
	tests := g.rootTests()
	if len(tests) != 4 || tests[1]["name"] != "A1: builds" || tests[1]["level"] != "acceptance" || tests[1]["command"] != "go build ./..." {
		t.Errorf("root tests = %v", tests)
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil || st.Requirements != 7 || st.Covered != 7 {
		t.Errorf("status = %d of %d covered, %v", st.Covered, st.Requirements, err)
	}
	if n := count(g.stagesCalled(), "coverage"); n != 1 || count(g.stagesCalled(), "coverage_fill") != 0 {
		t.Errorf("coverage calls = %d, fill calls = %d", n, count(g.stagesCalled(), "coverage_fill"))
	}
	var prompt string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "coverage" {
			prompt = r.Messages[1].Content
		}
	}
	for _, want := range []string{"F1 (feature, line", "Greeting: `Greet(name)` returns", "C2 (constraint, line", "A3 (acceptance, line", "fn-greet | function | greeting | Greet a name | internal/greet/greet.go"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("coverage prompt lacks %q", want)
		}
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "coverage"})
	if len(rows) != 1 || rows[0].Scope != "brief" {
		t.Errorf("coverage rows = %+v", rows)
	}
}

// The canned coverage reply leaves an acceptance bullet without a root test
// and a constraint without a node. The fill reply adds a root test and
// declares a new function, which has to be decomposed before the plan counts
// as covered.
func TestCoverageFillClosesGapsAndDecomposesNewFunctions(t *testing.T) {
	g := newRig(t, approving(), greeterGap)
	g.mustPlan(planner.Options{StopAfter: "coverage"})

	calls := g.stagesCalled()
	if count(calls, "coverage") != 1 || count(calls, "coverage_fill") != 1 || count(calls, "decompose:greeting") != 2 {
		t.Fatalf("calls = %v, want one coverage, one fill and a second decompose for greeting", calls)
	}
	var fill, redo string
	for _, r := range g.fake.Requests() {
		switch planner.StageOf(r) {
		case "coverage_fill":
			fill = r.Messages[1].Content
		case "decompose:greeting":
			redo = r.Messages[1].Content
		}
	}
	for _, want := range []string{"C1 (line", "reason: no node and no root test", "A3 (line", "reason: acceptance bullet has no root test", "greeting (package greet)"} {
		if !strings.Contains(fill, want) {
			t.Errorf("fill prompt lacks %q", want)
		}
	}
	if !strings.Contains(redo, "fn-imports-are-standard") || strings.Contains(redo, `"id": "fn-greet"`) {
		t.Error("the second decompose call must ask for the new function only")
	}

	c := g.contracts()
	if c.Revision != 1 || len(c.Functions) != 4 {
		t.Errorf("contract revision %d with %d functions, want 1 and 4", c.Revision, len(c.Functions))
	}
	if got := len(g.drafts().Components["greeting"]); got != 2 {
		t.Errorf("greeting has %d drafts, want 2", got)
	}
	var comp struct {
		Children []string `json:"children"`
	}
	if err := json.Unmarshal(g.read("greeting/component.json"), &comp); err != nil || strings.Join(comp.Children, " ") != "fn-greet fn-imports-are-standard" {
		t.Errorf("greeting children = %v, %v", comp.Children, err)
	}
	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Rounds != 1 || len(cov.Covered) != 7 || len(cov.RootTests) != 4 {
		t.Errorf("coverage.json = %d rounds, %d covered, %d root tests", cov.Rounds, len(cov.Covered), len(cov.RootTests))
	}
	for _, cv := range cov.Covered {
		if cv.Requirement == "C1" && strings.Join(cv.Nodes, " ") != "fn-imports-are-standard" {
			t.Errorf("C1 is covered by %v", cv.Nodes)
		}
	}
	if gaps := g.sink.OfKind(events.KindCoverageGap); len(gaps) != 2 || !strings.Contains(gaps[0].Message, "round 1: C1") {
		t.Errorf("coverage_gap events = %+v", gaps)
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "coverage_fill"})
	if len(rows) != 1 || rows[0].TaskType != "coverage" {
		t.Errorf("fill rows = %+v (a fill call is coverage work)", rows)
	}
}

// A fill that still leaves a gap stops the run in Coverage. Nothing is
// approved, and the resume repeats only that stage.
func TestCoverageStopsWhenAGapIsLeft(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, greeterStuck)
	_, err := g.plan(planner.Options{})
	var ce *planner.CoverageError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a *CoverageError", err)
	}
	if len(ce.Gaps) != 1 || ce.Gaps[0].Requirement != "A3" || ce.Gaps[0].Reason != "acceptance bullet has no root test" {
		t.Fatalf("gaps = %+v", ce.Gaps)
	}
	if !strings.Contains(err.Error(), "A3 (line") || !strings.Contains(err.Error(), "`go test ./...` passes.") {
		t.Errorf("the error must name the requirement and its text: %v", err)
	}
	if n := count(g.stagesCalled(), "coverage_fill"); n != 2 {
		t.Errorf("fill rounds = %d, want 2 (max_coverage_rounds)", n)
	}
	if g.has("coverage.json") || g.has("approval.json") || len(gate.plans) != 0 {
		t.Error("an uncovered plan reached approval")
	}
	if len(g.rootTests()) != 0 {
		t.Error("root tests were written for an uncovered plan")
	}

	g.wire(greeterStuck)
	if _, err := g.plan(planner.Options{RunID: greeterID}); !errors.As(err, &ce) {
		t.Fatalf("resume err = %v, want a *CoverageError again", err)
	}
	for _, s := range g.stagesCalled() {
		if s != "coverage" && s != "coverage_fill" {
			t.Errorf("resume called %s; only the coverage stage may run", s)
		}
	}
}

func TestCoverageRepliesThatCannotBeUsedAreRetried(t *testing.T) {
	good, err := os.ReadFile(filepath.Join(greeterGap, "coverage_fill.txt"))
	if err != nil {
		t.Fatal(err)
	}
	base, _ := os.ReadFile(filepath.Join(greeter, "coverage.txt"))
	cases := []struct {
		name  string
		dirs  []string
		stage string
	}{
		{"a requirement the brief does not have", []string{variant(t, map[string]string{
			"coverage.txt":   `{"map": [{"requirement": "C9", "nodes": []}], "root_tests": []}`,
			"coverage.2.txt": string(base),
		})}, "coverage"},
		{"a new function in a component that does not exist", []string{variant(t, map[string]string{
			"coverage_fill.txt":   strings.Replace(string(good), `"component": "greeting"`, `"component": "ghost"`, 1),
			"coverage_fill.2.txt": string(good),
		}), greeterGap}, "coverage_fill"},
		{"a new function whose file leaves the repository", []string{variant(t, map[string]string{
			"coverage_fill.txt":   strings.Replace(string(good), `internal/greet/imports.go`, `../imports.go`, 1),
			"coverage_fill.2.txt": string(good),
		}), greeterGap}, "coverage_fill"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving(), c.dirs...)
			g.mustPlan(planner.Options{StopAfter: "coverage"})
			rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: c.stage})
			if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeMalformed || rows[1].Outcome != ledger.OutcomeOK {
				t.Fatalf("%s rows = %+v, want a malformed attempt then a good one", c.stage, rows)
			}
			if cov, err := planner.ReadCoverage(g.runDir); err != nil || len(cov.Covered) != 7 {
				t.Errorf("coverage = %+v, %v", cov, err)
			}
		})
	}
}

func TestCoverageWarningsAreRecordedAndReported(t *testing.T) {
	g := newRig(t, approving())
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		s = strings.Replace(s, "- `internal/greet`: pure functions", "- `cmd/greeter`: the binary.\n- `internal/greet`: pure functions", 1)
		return strings.Replace(s, "- `go test ./...` passes.", "- `go test -race ./...` passes.", 1)
	})
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"`cmd/greeter` is named in the brief but no function file is under it",
		"A3: no root test runs the command the bullet begins with (`go test -race ./...`)",
	}
	if strings.Join(cov.Warnings, "\n") != strings.Join(want, "\n") {
		t.Fatalf("warnings = %q\nwant       %q", cov.Warnings, want)
	}
	n := 0
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if e.Stage == "coverage" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d coverage warning events, want 2", n)
	}
}

// A run that died after a fill round extended the contract, but before the
// new function was decomposed, picks the function up when Coverage runs again.
func TestCoverageDecomposesFunctionsLeftWithoutANode(t *testing.T) {
	g := newRig(t, approving(), greeterGap)
	g.mustPlan(planner.Options{StopAfter: "decompose"})

	var doc map[string]any
	if err := json.Unmarshal(g.read("contracts.json"), &doc); err != nil {
		t.Fatal(err)
	}
	doc["functions"] = append(doc["functions"].([]any), map[string]any{
		"id": "fn-imports-are-standard", "package": "greet", "file": "internal/greet/imports.go",
		"signature": "func ImportsAreStandard(paths []string) bool", "doc": "d", "uses": []any{}, "component": "greeting"})
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(g.runDir, "contracts.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	g.wire(variant(t, map[string]string{
		"decompose.greeting.txt": string(mustRead(t, filepath.Join(greeterGap, "decompose.greeting.2.txt"))),
		"enrich.greeting.2.txt":  string(mustRead(t, filepath.Join(greeterGap, "enrich.greeting.2.txt"))),
	}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "coverage"})
	if got := strings.Join(g.stagesCalled(), " "); got != "enrich:types enrich:greeting enrich:farewell enrich_comp:types enrich_comp:greeting enrich_comp:farewell enrich_root decompose:greeting enrich:greeting coverage" {
		t.Errorf("calls = %q, want the missing node decomposed, enriched and then coverage", got)
	}
	if got := len(g.drafts().Components["greeting"]); got != 2 {
		t.Errorf("greeting has %d drafts, want 2", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
