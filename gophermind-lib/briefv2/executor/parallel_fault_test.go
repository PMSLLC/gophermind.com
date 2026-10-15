package executor

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/runner"
)

// interruptedIDs is the leaf of every interrupted event, in order.
func (g *rig) interruptedIDs() []string {
	var out []string
	for _, e := range g.sink.OfKind("interrupted") {
		out = append(out, e.NodeID)
	}
	return out
}

// assertKept says a leaf ended with its candidate kept: no commit, the stub on
// disk, the row ready and unclaimed.
func assertKept(t *testing.T, g *rig, id string) {
	t.Helper()
	l := g.plan.Leaf(id)
	if !fileExists(candidatePath(g, id)) {
		t.Errorf("%s: the candidate was not kept", id)
	}
	if g.leafCommits(id) != 0 || !fileExists(g.stubPath(l)) || fileExists(g.realPath(l)) {
		t.Errorf("%s: a commit, or not the stub, in the main tree", id)
	}
	if r := g.row(t, id); r.Status != blackboard.StatusReady || r.Claim != nil {
		t.Errorf("%s = %s claim %v, want ready and released", id, r.Status, r.Claim)
	}
}

// Task 6 review: the context ends during the landing check. The landing is
// interrupted (landInterrupted), not a conflict: the candidate stays for the
// next run, nothing is committed, no attempt is charged.
func TestCancelDuringTheLandingCheckKeepsTheCandidate(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict(), {Class: runner.ClassCancelled}}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc.Hook = func(repo string) {
		if repo == g.repo {
			cancel() // the first landing check: farewell's
		}
	}
	wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop == nil || wr.Stop.Status != "interrupted" || len(wr.Verified)+len(wr.Failed) != 0 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	assertKept(t, g, "fn-farewell")
	assertKept(t, g, "fn-greet")
	if v := verdictsOf(t, g.board, "fn-farewell"); v != "" {
		t.Fatalf("fn-farewell attempts = %q, want none", v)
	}
	if n := len(g.sink.OfKind("land_conflict")); n != 0 {
		t.Fatalf("land_conflict events = %d", n)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Task 6 review: a leaf whose private ladder is used up waits for its turn to
// revise (afterRevision, ruling 6). Behind a failed landing commit it gets
// errLandingFault and ends interrupted: no revise, no escalation.
func TestALeafBehindAFailedLandingDoesNotRevise(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1), workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    distinct("fn-greet", 2),
	})
	twice(fc, "fn-farewell")
	fc.LeafScript["fn-greet"] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet")}
	rc.git = commitFailGit{Repo: rc.git, id: "fn-farewell"}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err == nil || !strings.Contains(err.Error(), "committing leaf fn-farewell failed") || len(wr.Verified)+len(wr.Failed)+len(wr.Escalated) != 0 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if wr.Stop == nil || wr.Stop.Status != "interrupted" || !reflect.DeepEqual(g.interruptedIDs(), []string{"fn-greet"}) {
		t.Fatalf("stop %+v, interrupted %v: want fn-greet interrupted", wr.Stop, g.interruptedIDs())
	}
	if n := len(g.leafCalls("fn-greet")); n != 2 {
		t.Fatalf("fn-greet made %d model calls, want its two attempts and no revise", n)
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusReady || r.Claim != nil || r.Revision != 0 {
		t.Fatalf("fn-greet = %s claim %v revision %d", r.Status, r.Claim, r.Revision)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Ruling C8, spec section 10: a private tree that cannot be made is a harness
// fault of that job only. It sets no fault flag: the jobs behind it land in id
// order; the leaf is never claimed and stays ready, with nothing left behind.
func TestAMakeTreeFailureLetsTheOthersLand(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 3))
	rc, _ := g.schedRC(t, sixScript(g))
	rc.chk = g.sixChecker()
	ids := []string{"fn-cheer", "fn-farewell", "fn-greet"}
	others := ids[1:]
	ready := make(chan string, 2)
	rc.afterTree = func(id string) { ready <- id }
	rc.treeFault = func(id string) bool {
		if id != "fn-cheer" {
			return false
		}
		for range others { // every other job is started before this one fails
			select {
			case <-ready:
			case <-time.After(30 * time.Second):
				t.Error("the other trees were never made")
			}
		}
		return true
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(ids...))
	if err == nil || !strings.Contains(err.Error(), "the private tree of fn-cheer could not be made") {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if wr.Stop != nil || !reflect.DeepEqual(wr.Verified, others) || len(g.interruptedIDs()) != 0 {
		t.Fatalf("wave = %+v, interrupted %v: want the others verified", wr, g.interruptedIDs())
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, others) {
		t.Fatalf("commit order = %v", got)
	}
	r := g.row(t, "fn-cheer")
	if r.Status != blackboard.StatusReady || r.Claim != nil || len(r.Attempts) != 0 || len(g.leafCalls("fn-cheer")) != 0 {
		t.Fatalf("fn-cheer = %s claim %v, %d attempts, %d calls", r.Status, r.Claim, len(r.Attempts), len(g.leafCalls("fn-cheer")))
	}
	if fileExists(candidatePath(g, "fn-cheer")) || g.leafCommits("fn-cheer") != 0 {
		t.Fatal("fn-cheer has a candidate or a commit")
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Ruling C5, spec 7.3, along a chain: the leaf that landed before the failed
// landing commit keeps its commit; the fault passes down every job behind it,
// each interrupted with its candidate kept. All four are started before any
// lands (a barrier at the private checks).
func TestAFailedLandingCommitPassesDownTheChain(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 4))
	rc, _ := g.schedRC(t, sixScript(g))
	fc := g.sixChecker()
	rc.chk = fc
	ids := []string{"fn-cheer", "fn-farewell", "fn-greet", "fn-shout"}
	meet := newBarrier(len(ids))
	fc.Hook = func(repo string) {
		if repo != g.repo && !meet.arrive() {
			t.Error("the four private checks did not meet")
		}
	}
	rc.git = commitFailGit{Repo: rc.git, id: "fn-farewell"}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(ids...))
	if err == nil || !strings.Contains(err.Error(), "committing leaf fn-farewell failed") {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if !reflect.DeepEqual(wr.Verified, []string{"fn-cheer"}) || wr.Stop == nil || wr.Stop.Status != "interrupted" {
		t.Fatalf("wave = %+v", wr)
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, []string{"fn-cheer"}) {
		t.Fatalf("commits = %v", got)
	}
	if r := g.row(t, "fn-cheer"); r.Status != blackboard.StatusVerified || !fileExists(g.realPath(g.plan.Leaf("fn-cheer"))) {
		t.Fatalf("fn-cheer = %s, or its file is gone", r.Status)
	}
	if got := g.interruptedIDs(); !reflect.DeepEqual(got, []string{"fn-greet", "fn-shout"}) {
		t.Fatalf("interrupted = %v", got)
	}
	for _, id := range []string{"fn-greet", "fn-shout"} {
		assertKept(t, g, id)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Task 7 review: the context ends while a leaf with a saved candidate waits
// for its turn behind a lower id still checking. The wait ends with the
// context: the leaf is interrupted, its candidate kept, nothing lands.
func TestCancelWhileACandidateWaitsForItsTurn(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{{Class: runner.ClassCancelled}}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saved := make(chan struct{})
	var once sync.Once
	rc.o.Sink = hookSink{next: rc.o.Sink, on: func(e events.Event) {
		if e.Kind == "leaf_candidate" && e.NodeID == "fn-greet" {
			once.Do(func() { close(saved) })
		}
	}}
	fc.Hook = func(repo string) {
		if repo == g.repo || !strings.HasSuffix(repo, "fn-farewell") {
			return
		}
		select { // farewell's private check holds the turn until greet waits for it
		case <-saved:
		case <-time.After(30 * time.Second):
			t.Error("fn-greet never saved its candidate")
		}
		time.Sleep(300 * time.Millisecond) // let greet reach its wait
		cancel()
	}
	wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop == nil || wr.Stop.Status != "interrupted" || len(wr.Verified)+len(wr.Failed) != 0 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if !inList(g.interruptedIDs(), "fn-greet") {
		t.Fatalf("interrupted = %v", g.interruptedIDs())
	}
	assertKept(t, g, "fn-greet")
	if g.leafCommits("fn-farewell") != 0 || len(g.leafCalls("fn-greet")) != 1 {
		t.Fatal("a landing after the cancel, or a second model call for fn-greet")
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Found by TestParallelKillAndResume: the context ends after a leaf is claimed
// and started but before its run is built (newLeafRun reads the row). That is
// an interrupt, not a harness fault: the leaf is released, ready, claimable.
func TestCancelAsALeafStartsIsAnInterrupt(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.schedRC(t, Script{"implement:fn-farewell": {reply(good("fn-farewell"))}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc.o.Sink = hookSink{next: rc.o.Sink, on: func(e events.Event) {
		if e.Kind == "leaf_started" {
			cancel()
		}
	}}
	wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell"))
	if err != nil || wr.Stop == nil || wr.Stop.Status != "interrupted" {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if r := g.row(t, "fn-farewell"); r.Status != blackboard.StatusReady || r.Claim != nil {
		t.Fatalf("fn-farewell = %s claim %v, want ready and released", r.Status, r.Claim)
	}
}
