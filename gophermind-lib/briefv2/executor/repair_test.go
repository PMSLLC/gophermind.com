package executor

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// repairRig is one entry, one fix, no extra revisions and the given number of
// repair rounds.
func repairRig(rounds int) func(*rigOpts) {
	return func(o *rigOpts) {
		oneEntryRevisions(1, 0)(o)
		base := o.Settings
		o.Settings = func(c *settings.Config) {
			base(c)
			c.Executor.RepairRounds = rounds
		}
	}
}

// repairRC is a started run whose wave 0 leaves verify at once and whose fn-greet
// has n more distinct good replies for repair rounds. The repository-wide
// checks pass unless a test scripts them.
func (g *rig) repairRC(t *testing.T, n int) (*runCtx, *fakeChecker) {
	t.Helper()
	greet := []step{reply(good("fn-greet"))}
	for i := 1; i <= n; i++ {
		greet = append(greet, reply(variant(good("fn-greet"), i)))
	}
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    greet,
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	for i := 0; i <= n; i++ {
		fc.LeafScript["fn-greet"] = append(fc.LeafScript["fn-greet"], passVerdict())
	}
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	return rc, fc
}

func greetFails(fc *fakeChecker, g *rig, n int) {
	a := g.plan.Leaf("fn-greet")
	for i := 0; i < n; i++ {
		fc.RepoScript.Test["./"+a.Dir] = append(fc.RepoScript.Test["./"+a.Dir],
			runner.Verdict{Class: runner.ClassTestFail, Names: []string{a.TestFunc + "/empty"}})
	}
}

func (fc *fakeChecker) testsOf(pkg string) int {
	n := 0
	for _, c := range fc.tests() {
		if c.Pkg == pkg {
			n++
		}
	}
	return n
}

func eventCount(g *rig, kind string) int {
	n := 0
	for _, e := range g.sink.Events() {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func TestRepairLoopBound(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(2))
	rc, fc := g.repairRC(t, 2)
	greetFails(fc, g, 10)
	stop, err := rc.runWave(context.Background(), 0)
	if err != nil || stop == nil || stop.Status != "escalated" || stop.Reason != "human_stop" {
		t.Fatalf("runWave = %+v, %v, want escalated human_stop", stop, err)
	}
	if n := eventCount(g, "reopened"); n != 2 || rc.rep.repairs != 2 {
		t.Errorf("reopened %d times, repairs %d, want 2 and 2", n, rc.rep.repairs)
	}
	if n := len(implementCalls(t, g.led, "fn-greet")); n != 3 {
		t.Errorf("fn-greet ladder ran %d times, want 1 + 2", n)
	}
	if n := fc.testsOf("./internal/greet"); n != 3 {
		t.Errorf("the wave was checked %d times, want 1 + 2", n)
	}
	esc := g.gate.Escalations()
	if len(esc) != 1 || esc[0].NodeID != "fn-greet" || !strings.HasPrefix(esc[0].Reason, "integration:") {
		t.Fatalf("gate escalations = %+v, want one integration escalation of fn-greet", esc)
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusEscalated {
		t.Errorf("fn-greet = %s, want escalated", r.Status)
	}
	if r := g.row(t, "fn-farewell"); r.Status != blackboard.StatusVerified || len(implementCalls(t, g.led, "fn-farewell")) != 1 {
		t.Errorf("fn-farewell = %s: a verified leaf that was not attributed must stay untouched", r.Status)
	}
}

func TestRepairEscalatesAfterBound(t *testing.T) {
	t.Parallel()
	// retry grants exactly one more round, then a check.
	g := newRig(t, repairRig(2))
	rc, fc := g.repairRC(t, 3)
	greetFails(fc, g, 3) // the initial check and two rounds fail, the extra round passes
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "mind the empty name"}}
	if stop, err := rc.runWave(context.Background(), 0); err != nil || stop != nil {
		t.Fatalf("runWave = %+v, %v, want a pass after the extra round", stop, err)
	}
	if n := fc.testsOf("./internal/greet"); n != 4 {
		t.Errorf("checks = %d, want 1 + 2 + 1", n)
	}
	if n := len(implementCalls(t, g.led, "fn-greet")); n != 4 {
		t.Errorf("ladder runs = %d, want 1 + 3", n)
	}
	if rc.state.ExtraRepair["0"] != 1 {
		t.Errorf("ExtraRepair = %v, want wave 0 granted one round", rc.state.ExtraRepair)
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusVerified {
		t.Errorf("fn-greet = %s, want verified", r.Status)
	}

	// stop ends the run escalated with the leaf escalated.
	g2 := newRig(t, repairRig(1))
	rc2, fc2 := g2.repairRC(t, 1)
	greetFails(fc2, g2, 10)
	stop, err := rc2.runWave(context.Background(), 0)
	if err != nil || stop == nil || stop.Status != "escalated" || stop.Reason != "human_stop" {
		t.Fatalf("runWave = %+v, %v", stop, err)
	}
	if r := g2.row(t, "fn-greet"); r.Status != blackboard.StatusEscalated {
		t.Errorf("fn-greet = %s, want escalated", r.Status)
	}
}

func TestRepairIntegrationSkipStopsRun(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(1))
	rc, fc := g.repairRC(t, 1)
	greetFails(fc, g, 10)
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	stop, err := rc.runWave(context.Background(), 0)
	if err != nil || stop == nil || stop.Status != "failed" || stop.Reason != "integration_skipped" {
		t.Fatalf("runWave = %+v, %v, want failed integration_skipped", stop, err)
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusFailed {
		t.Errorf("fn-greet = %s, want failed", r.Status)
	}
	for _, id := range []string{"fn-bye", "fn-hello", "fn-serve"} {
		if n := len(g.leafCalls(id)); n != 0 {
			t.Errorf("a later wave ran: %s had %d calls", id, n)
		}
	}
}

func TestWaveChecksReopenAttributedLeafOnly(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(2))
	rc, fc := g.repairRC(t, 1)
	a := g.plan.Leaf("fn-greet")
	fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassBuild, Locations: []runner.Location{loc(a.File, 4)}, Out: outputOf("CANARY-compiler-text")}}
	if stop, err := rc.runWave(context.Background(), 0); err != nil || stop != nil {
		t.Fatalf("runWave = %+v, %v", stop, err)
	}
	if n := eventCount(g, "reopened"); n != 1 {
		t.Errorf("reopened %d leaves, want only fn-greet", n)
	}
	if n := len(implementCalls(t, g.led, "fn-farewell")); n != 1 {
		t.Errorf("fn-farewell was called %d times, want once", n)
	}
	if n := len(implementCalls(t, g.led, "fn-greet")); n != 2 {
		t.Errorf("fn-greet was called %d times, want twice", n)
	}
	for _, id := range []string{"fn-greet", "fn-farewell"} {
		if r := g.row(t, id); r.Status != blackboard.StatusVerified {
			t.Errorf("%s = %s, want verified", id, r.Status)
		}
	}
	// The repair prompt carries the integration failure lines of the leaf, not
	// compiler output.
	last := userText(g.leafCalls("fn-greet")[1])
	if !strings.Contains(last, a.File+":4") {
		t.Errorf("the repair prompt lacks the failing location")
	}
	if strings.Contains(last, "CANARY-compiler-text") || hasCanary(t, g, "CANARY-compiler-text") {
		t.Error("compiler output reached a prompt or a store")
	}
}

func TestRepairCommitsAsRepairRound(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(2))
	rc, fc := g.repairRC(t, 1)
	a := g.plan.Leaf("fn-greet")
	greetFails(fc, g, 1)
	var seen []string
	fc.Hook = func(string) {
		s := "real="
		if fileExists(g.realPath(a)) {
			s += "y"
		} else {
			s += "n"
		}
		s += " stub="
		if fileExists(g.stubPath(a)) {
			s += "y"
		} else {
			s += "n"
		}
		seen = append(seen, s)
	}
	if stop, err := rc.runWave(context.Background(), 0); err != nil || stop != nil {
		t.Fatalf("runWave = %+v, %v", stop, err)
	}
	if out := g.gitCmd("log", "--format=%s", "--grep=^gm(fn-greet): repair round 1"); strings.TrimSpace(out) != "gm(fn-greet): repair round 1" {
		t.Fatalf("repair commit subjects = %q", out)
	}
	body := g.gitCmd("log", "-1", "--format=%B", "--grep=^gm(fn-greet): repair round 1")
	for _, want := range []string{"GopherMind-Node: fn-greet", "GopherMind-Repair: 1", "GopherMind-Run: " + g.id} {
		if !strings.Contains(body, want) {
			t.Errorf("the repair commit lacks the trailer %q:\n%s", want, body)
		}
	}
	// Both checks of fn-greet (the first pass and the repair) saw the real file
	// and no stub: the stub is not put back between the reopen and the repair.
	// Checks in order: fn-farewell, fn-greet, then fn-greet's repair.
	if len(seen) != 3 || seen[1] != "real=y stub=n" || seen[2] != "real=y stub=n" {
		t.Errorf("the state of fn-greet's files at each leaf check = %q", seen)
	}
	if n := g.leafCommits("fn-greet"); n != 2 {
		t.Errorf("fn-greet has %d commits, want the leaf commit and one repair", n)
	}
}

func TestFailedRepairRestoresLastVerifiedFile(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(2))
	rc, fc := g.repairRC(t, 2)
	a := g.plan.Leaf("fn-greet")
	greetFails(fc, g, 10)
	// The repair's own attempts fail; the gate skips the leaf.
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), failVerdict("test_fail", "x", "TestA"), failVerdict("test_fail", "x", "TestA")}
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	ctx := context.Background()
	if wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell", "fn-greet")); err != nil || len(wr.Verified) != 2 {
		t.Fatalf("wave 0 = %+v, %v", wr, err)
	}
	want, err := os.ReadFile(g.realPath(a))
	if err != nil || len(want) == 0 {
		t.Fatalf("the verified file is not on disk: %v", err)
	}
	stop, err := rc.runWave(ctx, 0)
	if err != nil || stop == nil || stop.Status != "failed" {
		t.Fatalf("runWave = %+v, %v, want a failed stop", stop, err)
	}
	got, err := os.ReadFile(g.realPath(a))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("the file after a failed repair differs from the last verified one (%v)", err)
	}
	if fileExists(g.stubPath(a)) {
		t.Error("the stub was put back over a verified file")
	}
	if dirty, err := g.git.Dirty(); err != nil || len(dirty) != 0 {
		t.Errorf("git status is not clean after a failed repair: %v, %v", dirty, err)
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusFailed {
		t.Errorf("fn-greet = %s, want failed", r.Status)
	}
}

func TestReopenKeepsRevisionAllowance(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(2))
	rc, _ := g.repairRC(t, 1)
	ctx := context.Background()
	l := g.plan.Leaf("fn-greet")
	if wr, err := rc.scheduleLeaves(ctx, []*Leaf{l}); err != nil || len(wr.Verified) != 1 {
		t.Fatalf("%+v %v", wr, err)
	}
	before := g.row(t, "fn-greet").Revision
	max0 := rc.maxRevisions(l)
	if err := rc.reopen(ctx, l, "integration: test"); err != nil {
		t.Fatal(err)
	}
	r := g.row(t, "fn-greet")
	if r.Status != blackboard.StatusReady || r.Revision != before+1 {
		t.Errorf("after reopen: %s revision %d, want ready and %d", r.Status, r.Revision, before+1)
	}
	if rc.state.ExtraRevisions["fn-greet"] != 1 || rc.maxRevisions(l) != max0+1 {
		t.Errorf("extra_revisions = %v, max %d -> %d: a repair must not use the original allowance", rc.state.ExtraRevisions, max0, rc.maxRevisions(l))
	}
	st, err := LoadState(g.runDir)
	if err != nil || st.ExtraRevisions["fn-greet"] != 1 {
		t.Errorf("the state was not saved: %+v, %v", st.ExtraRevisions, err)
	}
	if res, _ := LoadLeafResults(g.runDir); res["fn-greet"].Reason != "integration: test" {
		t.Errorf("the reopen reason was not recorded: %+v", res["fn-greet"])
	}
	if err := rc.reopen(ctx, g.plan.Leaf("fn-bye"), "integration: test"); err == nil {
		t.Error("a leaf that is not verified was reopened")
	}
}
