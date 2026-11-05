package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/packer"
	"gophermind/gophermind-lib/build/router"
	"gophermind/gophermind-lib/build/runner"
)

// A function outside the validated main whose node says it calls os.Exit can
// never pass the reply gate: the leaf fails with a fixed reason before any
// model call.
func TestPreflightFailsAPolicyConflictBeforeTheLadder(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	l := g.plan.Leaf(leafID)
	l.SideEffects = []string{"calls os.Exit with the code"}

	out, err := rc.runLeaf(context.Background(), l, leafIn{})
	if out.Status != blackboard.StatusFailed || out.Reason != reasonPolicyConflict {
		t.Fatalf("outcome = %+v, want failed %s", out, reasonPolicyConflict)
	}
	var se *stopError
	if !errors.As(err, &se) || se.Reason != reasonPolicyConflict {
		t.Fatalf("err = %v, want a stop error with reason %s", err, reasonPolicyConflict)
	}
	for _, want := range []string{leafID, "requires os.Exit", "model code may not end the process"} {
		if !strings.Contains(se.Message, want) {
			t.Errorf("cause %q does not name %q", se.Message, want)
		}
	}
	if n := len(g.leafCalls(leafID)); n != 0 {
		t.Fatalf("implement calls = %d, want 0", n)
	}
	if row := g.row(t, leafID); row.Status != blackboard.StatusFailed {
		t.Fatalf("row status = %s, want failed", row.Status)
	}
}

// The word must be os.Exit itself: a description that names something else
// does not trip the preflight.
func TestPreflightMatchesTheWordOnly(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"calls os.Exit with the code", true},
		{"ends with os.Exit(1)", true},
		{"never calls gmos.Exit", false},
		{"returns an exit code", false},
		{"os.Exiting is not a thing", false},
	} {
		if got := mentionsExit(c.text); got != c.want {
			t.Errorf("mentionsExit(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// The same os.Exit reply twice on one entry ends the revision: the next
// ladder entry is not tried.
func TestRepeatedForbiddenReplyEndsTheRevision(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	exit := strings.Replace(good(leafID), `import "strings"`, "import (\n\t\"os\"\n\t\"strings\"\n)", 1)
	exit = strings.Replace(exit, "name = strings.TrimSpace(name)", "os.Exit(1)\n\tname = strings.TrimSpace(name)", 1)
	steps := []step{}
	for i := 0; i < 8; i++ {
		steps = append(steps, reply(exit))
	}
	steps = append(steps, reply(good(leafID)))
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: steps}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	lr, err := rc.newLeafRun(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lr.oneRevision(context.Background())
	if err != nil || res != revPolicy {
		t.Fatalf("oneRevision = %v, %v; want revPolicy", res, err)
	}
	r := reasonsOf(t, g.board, leafID)
	if len(r) != 2 || r[1] != "identical_reply" {
		t.Fatalf("reasons = %v, want malformed then identical_reply", r)
	}
	var onB int
	for _, c := range implementCalls(t, g.led, leafID) {
		if c.Provider != "a" {
			onB++
		}
	}
	if onB != 0 {
		t.Fatalf("calls on the next entry = %d, want 0", onB)
	}
	if fc.checks(leafID) != 0 {
		t.Fatal("a forbidden reply was checked")
	}
}

// The preflight rule on its own, without a planned run: only a node outside
// the validated main that asks for os.Exit is a conflict.
func TestPreflightRule(t *testing.T) {
	t.Parallel()
	run := &Leaf{ID: "fn-run", Package: "cli", Signature: "func Run() int", SideEffects: []string{"calls os.Exit with the code"}}
	err := (&leafRun{l: run}).preflight()
	var se *stopError
	if !errors.As(err, &se) || se.Status != "failed" || se.Reason != reasonPolicyConflict {
		t.Fatalf("preflight = %v, want a failed stop with %s", err, reasonPolicyConflict)
	}
	for _, want := range []string{"fn-run", "requires os.Exit", "model code may not end the process"} {
		if !strings.Contains(se.Message, want) {
			t.Errorf("cause %q does not name %q", se.Message, want)
		}
	}
	desc := &Leaf{ID: "fn-run", NodeDescription: "Run parses the arguments and ends with os.Exit."}
	if err := (&leafRun{l: desc}).preflight(); err == nil {
		t.Error("a description that asks for os.Exit passed the preflight")
	}
	main := &Leaf{ID: "fn-main", Package: "main", Signature: "func main()", SideEffects: []string{"calls os.Exit with the code cli.Run returns"}}
	if err := (&leafRun{l: main, expect: packer.Expect{AllowExit: true}}).preflight(); err != nil {
		t.Errorf("the validated main failed the preflight: %v", err)
	}
	plain := &Leaf{ID: "fn-run", SideEffects: []string{"writes to stdout"}, NodeDescription: "returns an exit code"}
	if err := (&leafRun{l: plain}).preflight(); err != nil {
		t.Errorf("a node that never names os.Exit failed the preflight: %v", err)
	}
}

// providerError on its own, without a planned run: a forbidden refusal whose
// reply repeats the entry's last reply ends the revision; any other repeat,
// or a forbidden refusal of a new reply, keeps today's ending.
func TestRepeatedForbiddenReplyEndsTheEntryUnit(t *testing.T) {
	t.Parallel()
	forbidden := parseErrFor(t, "package cli\n\nimport \"os\"\n\n// Run runs.\nfunc Run() int {\n\tos.Exit(1)\n\treturn 1\n}\n")
	syntax := parseErrFor(t, "package cli\n\nfunc Run( {\n")
	if packer.ErrorClass(forbidden) != packer.ClassForbidden || packer.ErrorClass(syntax) != packer.ClassSyntax {
		t.Fatalf("classes = %q, %q", packer.ErrorClass(forbidden), packer.ErrorClass(syntax))
	}
	for _, c := range []struct {
		name     string
		prev     string
		parseErr error
		want     entryEnd
	}{
		{"forbidden repeat", "sha-1", forbidden, endPolicy},
		{"syntax repeat", "sha-1", syntax, endIdentical},
		{"forbidden new reply", "sha-0", forbidden, endMalformed},
	} {
		lr := unitLeafRun(t)
		e := ladderEntry{Name: "a/m1", Pos: 1}
		lr.lastSHA[e.Name] = c.prev
		end, done, err := lr.providerError(context.Background(), e, time.Now(), &router.ChainExhausted{ParseErr: c.parseErr}, "sha-1", c.parseErr)
		if err != nil || !done || end != c.want {
			t.Errorf("%s: providerError = %v, %v, %v; want %v, done", c.name, end, done, err, c.want)
		}
		if c.want == endPolicy && !strings.Contains(failureLines(lr.prev), "Never call os.Exit") {
			t.Errorf("%s: the next prompt does not carry the forbidden-call rule", c.name)
		}
	}
}

func parseErrFor(t *testing.T, src string) error {
	t.Helper()
	e, err := packer.NewExpect("cli", "func Run() int")
	if err != nil {
		t.Fatal(err)
	}
	e.Declared = []string{}
	_, perr := packer.ParseReply(src, e)
	if perr == nil {
		t.Fatal("the reply was accepted")
	}
	return perr
}

// unitLeafRun is a leafRun over a file blackboard with one node and nothing
// else: enough for record and providerError.
func unitLeafRun(t *testing.T) *leafRun {
	t.Helper()
	dir := t.TempDir()
	board := blackboard.NewFS(func(string) (string, error) { return dir, nil }, blackboard.WithFlatLayout())
	if err := board.InitRun(context.Background(), "r", []string{"fn-run"}, nil); err != nil {
		t.Fatal(err)
	}
	rc := &runCtx{o: Options{Board: board, Sink: events.NewCollector(), Now: time.Now}, plan: &Plan{RunID: "r"}}
	return &leafRun{rc: rc, l: &Leaf{ID: "fn-run", Package: "cli", Signature: "func Run() int"}, lastSHA: map[string]string{}, cp: map[int]int{}}
}
