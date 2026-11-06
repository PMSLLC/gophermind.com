package planner_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/acceptcheck"
	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/planner"
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

// A root test whose mktemp has no $TMPDIR template is a quality gap: macOS
// mktemp ignores TMPDIR and the run's sandbox denies the default temp dir.
// The hello A4 shape around the fixture's own probe (its contracts build no
// binary, so a bare program name would also be vacuous).
func TestCoverageQualityGateRefusesABareMktemp(t *testing.T) {
	bare := `out=$(mktemp); err=$(mktemp); go test ./... >"$out" 2>"$err"; status=$?; test "$status" -eq 0 && test ! -s "$err"`
	g := newRig(t, approving(), variant(t, map[string]string{
		"coverage.txt":      weakCoverage(t, bare),
		"coverage_fill.txt": fixA3,
	}))
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	var seen bool
	for _, e := range g.sink.OfKind(events.KindCoverageGap) {
		if strings.Contains(e.Message, "A3 root test quality [mktemp_outside_tmpdir]") {
			seen = true
		}
	}
	if !seen {
		t.Errorf("no coverage_gap event names A3 root test quality [mktemp_outside_tmpdir]: %+v", g.sink.OfKind(events.KindCoverageGap))
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

func TestCoveragePromptTeachesTheCapturePattern(t *testing.T) {
	b, err := os.ReadFile("prompts/coverage.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"status=$?", `2>"$err"`, "Never write `2>&1`", "may name only constructs the brief forbids everywhere", `mktemp "$TMPDIR/out.XXXXXX"`,
		"grep -qxF 'usage: hello [NAME]'", "Compare exact text with grep -qxF", "`cat a.go b.go | grep -c 'func '`", "gofmt -l . must print nothing, go vet ./... must exit 0."} {
		if !strings.Contains(string(b), want) {
			t.Errorf("coverage prompt lacks %q", want)
		}
	}
	if strings.Contains(string(b), "grep -qx ") || strings.Contains(string(b), "grep -qx`") {
		t.Error("coverage prompt still shows grep -qx without -F")
	}
}

// A constraint that names gofmt and go vet in backticks and is tested without
// running either is a quality gap named by the tools it never invokes.
func TestCoverageQualityGateRefusesAConstraintThatSkipsItsTools(t *testing.T) {
	raw := string(mustRead(t, filepath.Join(greeter, "coverage.txt")))
	old := `"command": "test -z \"$(gofmt -l .)\" && go vet ./..."`
	if !strings.Contains(raw, old) {
		t.Fatal("fixture changed")
	}
	g := newRig(t, approving(), variant(t, map[string]string{
		"coverage.txt":      strings.Replace(raw, old, `"command": "go test ./..."`, 1),
		"coverage_fill.txt": `{"map": [], "root_tests": [{"requirement": "C2", "name": "formatted and vetted", "given": "g", "expect": "e", "command": "test -z \"$(gofmt -l .)\" && go vet ./..."}]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	var seen bool
	for _, e := range g.sink.OfKind(events.KindCoverageGap) {
		if strings.Contains(e.Message, "C2 root test quality [tool_not_invoked: gofmt, go vet]") {
			seen = true
		}
	}
	if !seen {
		t.Errorf("no coverage_gap event names C2 root test quality [tool_not_invoked: gofmt, go vet]: %+v", g.sink.OfKind(events.KindCoverageGap))
	}
}

// The coverage prompt never teaches a form the checker refuses: every
// backticked command example in its rules (a span with a space that does not
// follow "never") is clean under acceptcheck.Quality with the hello binary.
// The harness contract is cut out: it lists refused forms on purpose.
func TestCoveragePromptExamplesPassTheQualityGate(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	var prompt string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "coverage" {
			prompt = r.Messages[1].Content
		}
	}
	_, rules, ok := strings.Cut(prompt, "\nRules:\n")
	if !ok {
		t.Fatalf("no rules in the rendered coverage prompt:\n%s", prompt)
	}
	harness, err := os.ReadFile("prompts/harness.md")
	if err != nil || !strings.Contains(rules, strings.TrimSpace(string(harness))) {
		t.Fatalf("the harness contract is not in the rendered prompt (%v)", err)
	}
	rules = strings.Replace(rules, strings.TrimSpace(string(harness)), "", 1)
	parts := strings.Split(rules, "`")
	var checked []string
	for i := 1; i < len(parts); i += 2 {
		span, before := parts[i], strings.ToLower(strings.TrimSpace(parts[i-1]))
		if !strings.Contains(span, " ") || strings.HasSuffix(before, "never") {
			continue
		}
		checked = append(checked, span)
		if got := acceptcheck.Quality(span, acceptcheck.Options{Bins: []string{"hello"}}); got != nil {
			t.Errorf("prompt example %q: %v", span, got)
		}
	}
	var capture, count bool
	for _, c := range checked {
		capture = capture || strings.Contains(c, "grep -qxF 'usage: hello [NAME]'")
		count = count || strings.Contains(c, "| grep -c ")
	}
	if !capture || !count {
		t.Errorf("capture example checked = %v, count example checked = %v: %q", capture, count, checked)
	}
}
