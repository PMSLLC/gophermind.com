package executor

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// A root test /bin/sh cannot parse stops the run acceptance_test_invalid and
// no leaf is reopened for it: the code never ran.
func TestShellSyntaxFaultStopsTheRun(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	rc.accept = oneBullets(`printf '/bin/sh: -c: line 0: syntax error near unexpected token %s\n' "'('" >&2; exit 2`)
	_, se, err := rc.acceptance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if se == nil || se.Status != "failed" || se.Reason != "acceptance_test_invalid" {
		t.Fatalf("stop = %+v, want failed acceptance_test_invalid", se)
	}
	if !strings.Contains(se.Message, "root test A1 #1") || !strings.Contains(se.Message, "syntax error") || !strings.Contains(se.Message, "the root test is wrong") {
		t.Errorf("message = %q", se.Message)
	}
	if evs := g.sink.OfKind("reopened"); len(evs) != 0 {
		t.Errorf("a leaf was reopened: %+v", evs)
	}
	if got := stopFailure(se); len(got) != 1 {
		t.Errorf("stopFailure = %v, want the message", got)
	}
}

// Exit 2 from a program's own usage error, with no syntax error text, is an
// ordinary failure.
func TestShellSyntaxFaultIgnoresOtherExit2(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	rc.accept = oneBullets(`printf 'usage: greet NAME\n' >&2; exit 2`)
	_, failed, err := rc.acceptanceRun(context.Background(), rc.accept, 0)
	if err != nil {
		t.Fatalf("an ordinary exit 2 stopped the run: %v", err)
	}
	if len(failed) != 1 || failed["A1"].ExitCode != 2 {
		t.Fatalf("failed = %+v, want A1 failed with exit 2", failed)
	}
	if _, ok := shellSyntaxFault("usage: greet NAME\n"); ok {
		t.Error("a usage line is named a shell syntax fault")
	}
	if _, ok := shellSyntaxFault("/bin/sh: -c: line 1: unexpected EOF while looking for matching `''\n/bin/sh: -c: line 2: syntax error: unexpected end of file\n"); !ok {
		t.Error("the unexpected EOF form is not named")
	}
}

// The acceptance report's requirements total counts only the requirements
// coverage accounts for, not the test obligations.
func TestAcceptanceTotalExcludesTestObligations(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	rc.plan.Requirements = []planner.Requirement{
		{ID: "C1", Kind: planner.ReqConstraint, Text: "c"},
		{ID: "A1", Kind: planner.ReqAcceptance, Text: "a"},
		{ID: "T1", Kind: planner.ReqTest, Text: "t"},
	}
	file, _, err := rc.acceptanceRun(context.Background(), oneBullets(`printf ok`), 0)
	if err != nil {
		t.Fatal(err)
	}
	if file.RequirementsCovered.Total != 2 {
		t.Errorf("requirements_covered.total = %d, want 2", file.RequirementsCovered.Total)
	}
}
