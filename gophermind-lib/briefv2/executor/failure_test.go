package executor

import (
	"fmt"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/runner"
)

func TestFailureReasonFormat(t *testing.T) {
	many := []string{"TestK", "TestJ", "TestI", "TestH", "TestG", "TestF", "TestE", "TestD", "TestC", "TestB", "TestA"}
	cases := []struct {
		name  string
		class string
		names []string
		want  string
	}{
		{"class only", "build", nil, "build"},
		{"class and sorted names", "test_fail", []string{"TestB/x", "TestA"}, "test_fail: TestA, TestB/x"},
		{"exactly eight", "test_fail", many[3:], "test_fail: TestA, TestB, TestC, TestD, TestE, TestF, TestG, TestH"},
		{"more than eight cut", "test_fail", many, "test_fail: TestA, TestB, TestC, TestD, TestE, TestF, TestG, TestH (+3 more)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := failureReason(c.class, c.names); got != c.want {
				t.Fatalf("failureReason = %q, want %q", got, c.want)
			}
		})
	}
	if got := failureReason("x", []string{"a\nb\r\nc\td"}); strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("failureReason kept a control character: %q", got)
	}
	long := strings.Repeat("N", 500)
	if got := failureReason("x", []string{long}); len(got) > 200 {
		t.Fatalf("failureReason kept a %d byte name", len(got))
	}
	if got := failureReason("x", []string{"", "   "}); got != "x" {
		t.Fatalf("blank names = %q, want the bare class", got)
	}
	// The input slice is not reordered.
	in := []string{"TestB", "TestA"}
	failureReason("x", in)
	if in[0] != "TestB" {
		t.Fatal("failureReason sorted its argument in place")
	}
}

func TestClassifyFailures(t *testing.T) {
	// Every class of runner.Verdict and every class the executor adds maps to
	// the text persisted as failure_reason: a class, then names, never output.
	runnerCases := []struct {
		class string
		names []string
		want  string
		leaf  bool // a leaf failure that consumes a fix attempt
	}{
		{runner.ClassMalformed, nil, "malformed", true},
		{runner.ClassBuild, nil, "build", true},
		{runner.ClassVet, nil, "vet", true},
		{runner.ClassTestFail, []string{"TestX/b", "TestX/a"}, "test_fail: TestX/a, TestX/b", true},
		{runner.ClassTestPanic, []string{"TestX"}, "test_panic: TestX", true},
		{runner.ClassTestTimeout, []string{"TestX"}, "test_timeout: TestX", true},
		{runner.ClassNoTestsRan, nil, "no_tests_ran", true},
		{runner.ClassTimeout, nil, "timeout", true},
		{runner.ClassHarness, nil, "harness", false},
		{runner.ClassCancelled, nil, "cancelled", false},
	}
	for _, c := range runnerCases {
		v := runner.Verdict{Class: c.class, Names: c.names, Events: 1}
		sorted := append([]string(nil), c.names...)
		if got := failureReason(c.class, sorted); got != c.want {
			t.Errorf("failureReason(%s) = %q, want %q", c.class, got, c.want)
		}
		// runner.Verdict.Reason lists names as the runner sorted them; the
		// two agree when the names are sorted already.
		if len(c.names) < 2 && v.Reason() != c.want {
			t.Errorf("Verdict.Reason(%s) = %q, want %q", c.class, v.Reason(), c.want)
		}
		if got := isLeafFailure(v); got != c.leaf {
			t.Errorf("isLeafFailure(%s) = %v, want %v", c.class, got, c.leaf)
		}
	}
	executorCases := map[string]string{
		ClassRateLimited: "rate_limited", ClassTimeout: "timeout", ClassContextTooLong: "context_too_long",
		ClassImportNotAllowed: "import_not_allowed", ClassForbiddenWrite: "forbidden_write",
		ClassContractProblem: "contract_problem", ClassNetworkCritical: "network_critical", ClassIdentical: "identical_reply",
	}
	for class, want := range executorCases {
		if class != want {
			t.Errorf("class constant %q, want %q", class, want)
		}
		if got := failureReason(class, nil); got != want {
			t.Errorf("failureReason(%s) = %q", class, got)
		}
	}
	if got := failureReason(ClassNetworkCritical, []string{"api.example.com"}); got != "network_critical: api.example.com" {
		t.Errorf("network_critical with a host = %q", got)
	}
	// The executor adds no class the runner already owns.
	if ClassTimeout != runner.ClassTimeout {
		t.Errorf("ClassTimeout %q differs from runner.ClassTimeout %q", ClassTimeout, runner.ClassTimeout)
	}
	// A pass with no test event is not a pass (spec R4).
	if v := settle(runner.Verdict{Events: 0}); v.Pass() || v.Class != runner.ClassNoTestsRan {
		t.Errorf("settle of a pass with zero events = %q, want no_tests_ran", v.Class)
	}
	if v := settle(runner.Verdict{Events: 3}); !v.Pass() {
		t.Errorf("settle of a pass with events = %q, want a pass", v.Class)
	}
	if v := settle(runner.Verdict{Class: runner.ClassBuild}); v.Class != runner.ClassBuild {
		t.Errorf("settle changed a build failure to %q", v.Class)
	}
}

func TestMergeFailuresAndStrip(t *testing.T) {
	const secret = "s3cr3t-value-xyz"
	// failureOf strips lines that hold a secret value (plain or encoded) and keeps the cap.
	var out strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&out, "line %d of the compiler output\n", i)
	}
	out.WriteString("token=" + secret + "\n")
	v := runner.Verdict{Class: runner.ClassTestFail, Names: []string{"TestB", "TestA"}}
	v.Out = outputOf(out.String())
	f := failureOf(v, []string{secret})
	if len(f.Lines) > 31 {
		t.Errorf("failureOf kept %d lines, want at most 30 plus the marker", len(f.Lines))
	}
	size := 0
	for _, l := range f.Lines {
		size += len(l) + 1
	}
	if size > 2048 {
		t.Errorf("failureOf kept %d bytes, want at most 2048", size)
	}
	if strings.Contains(strings.Join(f.Lines, "\n"), secret) {
		t.Error("failureOf kept a line with a secret value")
	}
	if len(f.Names) != 2 {
		t.Errorf("failureOf names = %v, want the two failed tests", f.Names)
	}

	// Two failures merged keep the cap, failed names first, no secret.
	a := packer.NewFailure([]string{"TestA", "TestB"}, strings.Repeat("alpha line\n", 40), nil)
	b := packer.NewFailure([]string{"TestB", "TestC"}, strings.Repeat("beta line\n", 40)+"password "+secret+"\n", nil)
	m := mergeFailures(a, b)
	if strings.Join(m.Names, ",") != "TestA,TestB,TestC" {
		t.Errorf("merged names = %v, want the union in order", m.Names)
	}
	if len(m.Lines) > 31 {
		t.Errorf("merged %d lines, want at most 30 plus the marker", len(m.Lines))
	}
	size = 0
	for _, l := range m.Lines {
		size += len(l) + 1
	}
	if size > 2048 {
		t.Errorf("merged %d bytes, want at most 2048", size)
	}
	first := mergeFailures(a)
	if strings.Join(first.Names, ",") != "TestA,TestB" {
		t.Errorf("a single failure changed by merge: %v", first.Names)
	}
	if !mergeFailures().Empty() {
		t.Error("merging nothing is not empty")
	}
	var nine []string
	for i := 0; i < 9; i++ {
		nine = append(nine, fmt.Sprintf("Test%d", i))
	}
	if got := mergeFailures(packer.Failure{Names: nine[:5]}, packer.Failure{Names: nine}); len(got.Names) != 8 {
		t.Errorf("merged %d names, want the cap of 8", len(got.Names))
	}
}

func TestFixTemperature(t *testing.T) {
	for k, want := range []float64{0, 0, 0.3, 0.3, 0.3} {
		if got := fixTemperature(k); got != want {
			t.Errorf("fixTemperature(%d) = %v, want %v", k, got, want)
		}
	}
}

func TestHistoryLine(t *testing.T) {
	a := blackboard.Attempt{Provider: "mini", Model: "qwen", Verdict: blackboard.VerdictFail, FailureReason: "test_fail: TestX/a"}
	if got, want := historyLine(3, a), "attempt 3 mini/qwen fail test_fail: TestX/a"; got != want {
		t.Errorf("historyLine = %q, want %q", got, want)
	}
	p := blackboard.Attempt{Provider: "mini", Model: "qwen", Verdict: blackboard.VerdictPass}
	if got, want := historyLine(4, p), "attempt 4 mini/qwen pass"; got != want {
		t.Errorf("historyLine = %q, want %q", got, want)
	}
	nl := blackboard.Attempt{Provider: "a", Model: "m", Verdict: blackboard.VerdictError, FailureReason: "x\ny"}
	if strings.Contains(historyLine(1, nl), "\n") {
		t.Error("historyLine holds a newline")
	}
}
