package planner_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
)

// weakCoverage is the greeter coverage reply with one root test replaced.
func weakCoverage(t *testing.T, a3 string) string {
	t.Helper()
	raw := string(mustRead(t, filepath.Join(greeter, "coverage.txt")))
	old := `"command": "go test ./..."`
	if !strings.Contains(raw, old) {
		t.Fatal("fixture changed")
	}
	return strings.Replace(raw, old, `"command": `+jsonString(a3), 1)
}

func jsonString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

const fixA3 = `{"map": [], "root_tests": [{"requirement": "A3", "name": "all tests pass", "given": "g", "expect": "e", "command": "go test ./..."}]}`

// A weak root test is a coverage defect: the fill prompt names the requirement
// and the finding, never the command, and the fill's replacement is kept.
func TestCoverageQualityGateFillReplacesAWeakRootTest(t *testing.T) {
	const canary = "WEAKCANARYWORD"
	g := newRig(t, approving(), variant(t, map[string]string{
		"coverage.txt":      weakCoverage(t, "go test ./... || echo "+canary),
		"coverage_fill.txt": fixA3,
	}))
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	if n := count(g.stagesCalled(), "coverage_fill"); n != 1 {
		t.Fatalf("fill rounds = %d, want 1", n)
	}
	var fill string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "coverage_fill" {
			fill = r.Messages[1].Content
		}
	}
	if !strings.Contains(fill, "A3") || !strings.Contains(fill, "masked_failure") {
		t.Errorf("fill prompt does not list A3 and masked_failure:\n%s", fill)
	}
	if strings.Contains(fill, canary) || strings.Contains(fill, "go test ./... || echo") {
		t.Error("the fill prompt quotes the weak command")
	}
	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil || cov.Rounds != 1 {
		t.Fatalf("coverage = %+v, %v", cov, err)
	}
	var cmds []string
	for _, rt := range cov.RootTests {
		if rt.Requirement == "A3" {
			cmds = append(cmds, rt.Command)
		}
	}
	if len(cmds) != 1 || cmds[0] != "go test ./..." {
		t.Errorf("A3 root tests = %q, want only the replacement", cmds)
	}
	var seen bool
	for _, e := range g.sink.OfKind(events.KindCoverageGap) {
		if strings.Contains(e.Message, "A3") && strings.Contains(e.Message, "masked_failure") {
			seen = true
		}
		if strings.Contains(e.Message, canary) {
			t.Error("an event quotes the weak command")
		}
	}
	if !seen {
		t.Error("no coverage_gap event names A3 and masked_failure")
	}
}

// After the rounds a weak root test stops the planner before approval, with a
// fixed message of ids and finding names.
func TestCoverageQualityGateStopsBeforeApproval(t *testing.T) {
	const canary = "WEAKCANARYWORD"
	gate := approving()
	dir := variant(t, map[string]string{
		"coverage.txt":      weakCoverage(t, "go test ./... || echo "+canary),
		"coverage_fill.txt": `{"map": [], "root_tests": []}`,
	})
	g := newRig(t, gate, dir)
	_, err := g.plan(planner.Options{})
	var qe *planner.QualityError
	if !errors.As(err, &qe) {
		t.Fatalf("err = %v, want a *QualityError", err)
	}
	if len(qe.Items) != 1 || qe.Items[0].Requirement != "A3" || strings.Join(qe.Items[0].Findings, ",") != "masked_failure" {
		t.Errorf("items = %+v", qe.Items)
	}
	msg := err.Error()
	if !strings.Contains(msg, "A3 (masked_failure)") || strings.Contains(msg, canary) {
		t.Errorf("message = %q", msg)
	}
	if n := count(g.stagesCalled(), "coverage_fill"); n != 2 {
		t.Errorf("fill rounds = %d, want 2 (max_coverage_rounds)", n)
	}
	if g.has("coverage.json") || g.has("approval.json") || len(gate.plans) != 0 {
		t.Error("a plan with a weak root test reached approval")
	}
}

func TestQualityErrorNamesAtMostTenRequirements(t *testing.T) {
	var items []planner.QualityItem
	for i := 1; i <= 13; i++ {
		items = append(items, planner.QualityItem{Requirement: "A" + strings.Repeat("1", i), Findings: []string{"print_only", "masked_failure"}})
	}
	msg := (&planner.QualityError{Items: items}).Error()
	if strings.Count(msg, "(print_only, masked_failure)") != 10 || !strings.Contains(msg, "13 root test") || !strings.Contains(msg, "3 more") {
		t.Errorf("message = %q", msg)
	}
}

// The approval summary shows a quality column.
func TestApprovalShowsQualityColumn(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	md, _, err := planner.RenderPlan(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "| Requirement | Text | Covered by | Root tests | Quality |") {
		t.Errorf("no quality column:\n%s", md)
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| A3 |") && !strings.HasSuffix(line, "| ok |") {
			t.Errorf("A3 row = %q, want quality ok", line)
		}
		if strings.HasPrefix(line, "| F1 |") && !strings.HasSuffix(line, "| ok |") {
			t.Errorf("F1 row = %q", line)
		}
	}
}

// A coverage.json edited after the stage (weak root test, no quality pass)
// is refused at approval, never approved.
func TestApprovalRefusesAWeakRootTest(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	p := filepath.Join(g.runDir, "coverage.json")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), `"go test ./..."`, `"go test ./... || true"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	g.wire()
	_, err := g.plan(planner.Options{RunID: greeterID})
	var qe *planner.QualityError
	if !errors.As(err, &qe) || g.has("approval.json") || len(gate.plans) != 0 {
		t.Fatalf("err = %v, approval written = %v", err, g.has("approval.json"))
	}
}
