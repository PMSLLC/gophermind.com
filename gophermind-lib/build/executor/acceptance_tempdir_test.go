package executor

import (
	"context"
	"strings"
	"testing"
)

func TestTempDirFault(t *testing.T) {
	t.Parallel()
	const line = "mktemp: mkstemp failed on /var/folders/pj/1n4nrxzd08g7tmc390k51t480000gn/T/tmp.6KO5TLmThF: Operation not permitted"
	got, ok := tempDirFault(line + "\n/bin/sh: : No such file or directory\n")
	if !ok || got != line {
		t.Errorf("tempDirFault = %q, %v, want the mktemp line and true", got, ok)
	}
	if _, ok := tempDirFault("mktemp: mkdtemp failed on /var/folders/x/T/tmp.abc: Operation not permitted\n"); !ok {
		t.Error("a mkdtemp failure is not named")
	}
	goTest := "--- FAIL: TestGreet (0.00s)\n    greet_test.go:12: got \"Hi\", want \"Hello\"\nFAIL\nFAIL\tgreeter\t0.01s\n"
	if _, ok := tempDirFault(goTest); ok {
		t.Error("a failing go test assertion is named a temp dir fault")
	}
	named := "=== RUN   TestMktempFallback\n--- PASS: TestMktempFallback (0.00s)\nPASS\n"
	if _, ok := tempDirFault(named); ok {
		t.Error("a test name mentioning mktemp is named a temp dir fault")
	}
}

// A root test whose mktemp could not create its file stops the run
// acceptance_environment and no leaf is reopened for it.
func TestAcceptanceTempDirFaultStops(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	rc.accept = oneBullets(`printf 'mktemp: mkstemp failed on /var/folders/x/T/tmp.abc: Operation not permitted\n' >&2; exit 1`)
	_, se, err := rc.acceptance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if se == nil || se.Status != "failed" || se.Reason != "acceptance_environment" {
		t.Fatalf("stop = %+v, want failed acceptance_environment", se)
	}
	if !strings.Contains(se.Message, "root test A1 #1") || !strings.Contains(se.Message, "mkstemp failed") || !strings.Contains(se.Message, "$TMPDIR") {
		t.Errorf("message = %q", se.Message)
	}
	if evs := g.sink.OfKind("reopened"); len(evs) != 0 {
		t.Errorf("a leaf was reopened: %+v", evs)
	}
}
