package executor

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// schedRC is a started run like leafRC, but every leaf stays pending: the
// scheduler is the one that makes leaves ready.
func (g *rig) schedRC(t *testing.T, script Script) (*runCtx, *fakeChecker) {
	t.Helper()
	g.wire(script)
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		t.Cleanup(rc.close)
	}
	if err != nil {
		t.Fatalf("startRun: %v", err)
	}
	fc := newFakeChecker(g)
	rc.chk = fc
	return rc, fc
}

func (g *rig) leaves(ids ...string) []*Leaf {
	var out []*Leaf
	for _, id := range ids {
		out = append(out, g.plan.Leaf(id))
	}
	return out
}

func implementOrder(g *rig) []string {
	var out []string
	for _, s := range stagesOf(g.fake) {
		if strings.HasPrefix(s, "implement:") {
			out = append(out, strings.TrimPrefix(s, "implement:"))
		}
	}
	return out
}

func TestSchedulerDeterministicOrder(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	// Handed in reverse: the order of the calls does not depend on it.
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-greet", "fn-farewell"))
	if err != nil || wr.Stop != nil {
		t.Fatalf("scheduleLeaves = %+v, %v", wr, err)
	}
	if got := implementOrder(g); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("calls arrived in order %v", got)
	}
	if !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("verified = %v", wr.Verified)
	}
}

// A leaf whose dependency was skipped is named, never claimed, never called.
func TestBlockedLeavesReported(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    distinct("fn-greet", 2),
		"implement:fn-bye":      {reply(good("fn-bye"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	failing(fc, "fn-greet", 2)
	fc.LeafScript["fn-bye"] = []runner.Verdict{passVerdict()}
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}

	w0, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || w0.Stop != nil {
		t.Fatalf("wave 0 = %+v, %v", w0, err)
	}
	if !reflect.DeepEqual(w0.Verified, []string{"fn-farewell"}) || !reflect.DeepEqual(w0.Failed, []string{"fn-greet"}) {
		t.Fatalf("wave 0 verified %v failed %v", w0.Verified, w0.Failed)
	}
	if rc.rep.reasons["fn-greet"] != "skipped by human" {
		t.Fatalf("reason = %q", rc.rep.reasons["fn-greet"])
	}

	w1, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-hello", "fn-bye"))
	if err != nil || w1.Stop != nil {
		t.Fatalf("wave 1 = %+v, %v", w1, err)
	}
	if !reflect.DeepEqual(w1.Blocked, map[string]string{"fn-hello": "fn-greet"}) || !reflect.DeepEqual(w1.Verified, []string{"fn-bye"}) {
		t.Fatalf("wave 1 blocked %v verified %v", w1.Blocked, w1.Verified)
	}
	if rc.rep.blocked["fn-hello"] != "fn-greet" {
		t.Fatalf("report blocked = %v", rc.rep.blocked)
	}
	ev := g.sink.OfKind("blocked")
	if len(ev) != 1 || ev[0].NodeID != "fn-hello" || !strings.Contains(ev[0].Message, "fn-greet") {
		t.Fatalf("blocked events = %+v", ev)
	}
	if r := g.row(t, "fn-hello"); r.Status != blackboard.StatusPending {
		t.Fatalf("blocked row = %s, want pending", r.Status)
	}
	for _, s := range stagesOf(g.fake) {
		if strings.HasSuffix(s, "fn-hello") {
			t.Fatalf("a call was made for the blocked leaf: %s", s)
		}
	}
	// Every leaf that ended has its status and reason on disk.
	res, err := LoadLeafResults(g.runDir)
	if err != nil || res["fn-greet"].Status != "failed" || res["fn-greet"].Reason != "skipped by human" || res["fn-farewell"].Status != "verified" {
		t.Fatalf("persisted results = %+v, %v", res, err)
	}
}

func TestSchedulerStopsOnStopError(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	rc, fc := g.schedRC(t, Script{"implement:fn-farewell": distinct("fn-farewell", 2)})
	failing(fc, "fn-farewell", 2)
	rc.o.Gate = nil // no gate: the escalation is a stop
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil {
		t.Fatal(err)
	}
	if wr.Stop == nil || wr.Stop.Status != "escalated" || wr.Stop.Reason != "human_stop" {
		t.Fatalf("stop = %+v", wr.Stop)
	}
	if !reflect.DeepEqual(wr.Escalated, []string{"fn-farewell"}) {
		t.Fatalf("escalated = %v", wr.Escalated)
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusPending && r.Status != blackboard.StatusReady {
		t.Fatalf("the next leaf is %s, want pending or ready", r.Status)
	}
	for _, s := range stagesOf(g.fake) {
		if strings.HasSuffix(s, "fn-greet") {
			t.Fatalf("a call was made after the stop: %s", s)
		}
	}
}

func TestSchedulerNoClaimAfterCancel(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.schedRC(t, Script{})
	tb := traced(rc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell", "fn-greet"))
	if err != nil {
		t.Fatal(err)
	}
	if wr.Stop == nil || wr.Stop.Status != "interrupted" || wr.Stop.Reason != "cancelled" {
		t.Fatalf("stop = %+v", wr.Stop)
	}
	for _, e := range tb.Trace() {
		if strings.HasPrefix(e, "claim ") {
			t.Fatalf("a claim was made on a cancelled context: %v", tb.Trace())
		}
	}
	if len(g.fake.Requests()) != 0 {
		t.Fatal("a model was called on a cancelled context")
	}
}

// A run-level interrupt (nothing could answer) stops the scheduler, leaves the
// leaf claimable and never a failed one, and the leaf is not claimed again in
// this run however often the scheduler is called.
func TestInterruptedLeafStopsSchedulerAndIsNotRetriedInALoop(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rl := step{Err: provider.ErrRateLimited{}}
	rc, _ := g.schedRC(t, Script{"implement:fn-farewell": {rl, rl}})
	tb := traced(rc)
	leaves := g.leaves("fn-farewell", "fn-greet")

	wr, err := rc.scheduleLeaves(context.Background(), leaves)
	if err != nil {
		t.Fatal(err)
	}
	if wr.Stop == nil || wr.Stop.Status != "interrupted" || wr.Stop.Reason != "provider_unavailable" {
		t.Fatalf("stop = %+v", wr.Stop)
	}
	if len(wr.Failed)+len(wr.Escalated)+len(wr.Verified) != 0 {
		t.Fatalf("an interrupted leaf was counted: %+v", wr)
	}
	if r := g.row(t, "fn-farewell"); r.Status != blackboard.StatusReady || r.Claim != nil {
		t.Fatalf("farewell row = %+v, want ready and released", r)
	}
	if res, _ := LoadLeafResults(g.runDir); len(res) != 0 {
		t.Fatalf("a result was recorded for an interrupted leaf: %+v", res)
	}
	if len(g.sink.OfKind("interrupted")) != 1 {
		t.Fatalf("interrupted events = %d", len(g.sink.OfKind("interrupted")))
	}
	claims := len(tb.Trace())
	calls := len(g.fake.Requests())

	for i := 0; i < 3; i++ {
		wr, err = rc.scheduleLeaves(context.Background(), leaves)
		if err != nil || wr.Stop == nil || wr.Stop.Reason != "interrupted_repeatedly" {
			t.Fatalf("call %d: stop = %+v, %v", i, wr.Stop, err)
		}
	}
	if len(tb.Trace()) != claims || len(g.fake.Requests()) != calls {
		t.Fatalf("the interrupted leaf was claimed or called again: trace %v", tb.Trace())
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusPending {
		t.Fatalf("greet row = %s, want untouched", r.Status)
	}
}

// A leaf claimed by a worker that died is released by the stale sweep and run.
// A claim with a live heartbeat is not touched and stops the scheduler.
func TestStaleClaimReleasedBySweep(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.schedRC(t, Script{"implement:fn-farewell": {reply(good("fn-farewell"))}})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	rc.heartbeat = 10 * time.Millisecond
	ctx := context.Background()
	if err := g.board.SetStatus(ctx, g.id, "fn-farewell", blackboard.StatusPending, blackboard.StatusReady); err != nil {
		t.Fatal(err)
	}
	if ok, err := g.board.Claim(ctx, g.id, "fn-farewell", "crashed-worker"); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}

	// Fresh heartbeat: the claim is held, the scheduler stops and touches nothing.
	rc.heartbeat = time.Hour
	wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell"))
	if err != nil || wr.Stop == nil || wr.Stop.Reason != "claim_held" {
		t.Fatalf("held claim: %+v, %v", wr, err)
	}
	if r := g.row(t, "fn-farewell"); r.Status != blackboard.StatusClaimed || r.Claim == nil || r.Claim.Worker != "crashed-worker" {
		t.Fatalf("a live claim was disturbed: %+v", r)
	}

	// The worker stops heartbeating.
	rc.heartbeat = 10 * time.Millisecond
	time.Sleep(200 * time.Millisecond)
	wr, err = rc.scheduleLeaves(ctx, g.leaves("fn-farewell"))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell"}) {
		t.Fatalf("after the sweep: %+v, %v", wr, err)
	}
	if len(g.sink.OfKind("warning")) == 0 {
		t.Error("the released claim was not reported")
	}
}

// Two workers race for one ready leaf: exactly one wins and the leaf is built once.
func TestClaimRaceExactlyOneWinner(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var verified, refused int
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := rc.runLeaf(context.Background(), g.plan.Leaf(leafID), leafIn{})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && out.Status == blackboard.StatusVerified:
				verified++
			case err != nil && strings.Contains(err.Error(), "could not be claimed"):
				refused++
			default:
				t.Errorf("unexpected result %+v, %v", out, err)
			}
		}()
	}
	wg.Wait()
	if verified != 1 || refused != 3 {
		t.Fatalf("verified %d refused %d, want 1 and 3", verified, refused)
	}
	if n := g.leafCommits(leafID); n != 1 {
		t.Fatalf("leaf commits = %d", n)
	}
}

// A leaf an earlier run left escalated is asked about again (S8).
func TestEscalatedLeafAskedAgainOnRestart(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	rc, fc := g.schedRC(t, Script{"implement:fn-farewell": append(distinct("fn-farewell", 2), reply(good("fn-farewell")))})
	failing(fc, "fn-farewell", 2)
	g.gate.waiting = true
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell"))
	if err != nil || wr.Stop == nil || wr.Stop.Reason != "waiting_on_human" || !reflect.DeepEqual(wr.Escalated, []string{"fn-farewell"}) {
		t.Fatalf("first run: %+v, %v", wr, err)
	}
	if r := g.row(t, "fn-farewell"); r.Status != blackboard.StatusEscalated {
		t.Fatalf("row = %s", r.Status)
	}

	// The person answers: retry with a note.
	g.gate.waiting = false
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "use TrimSpace"}}
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	wr, err = rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell"))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell"}) {
		t.Fatalf("second run: %+v, %v", wr, err)
	}
	if n := len(g.gate.Escalations()); n != 2 {
		t.Fatalf("the gate was asked %d times, want 2", n)
	}
	last := g.leafCalls("fn-farewell")
	if !strings.Contains(userText(last[len(last)-1]), "use TrimSpace") {
		t.Error("the note is not in the prompt")
	}
}

func TestWorkerCountCappedByProviders(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.Workers = 8 } })
	rc, _ := g.schedRC(t, Script{})
	if got := rc.workerCount(g.leaves("fn-greet")); got != 2 {
		t.Fatalf("workers = %d, want 2 (the sum of max_concurrent of a and b)", got)
	}
	rc.cfg.Executor.Workers = 1
	if got := rc.workerCount(g.leaves("fn-greet")); got != 1 {
		t.Fatalf("workers = %d, want 1", got)
	}
	rc.cfg.Executor.Workers = 8
	rc.cfg.Models = map[string][]string{"strong": {"a/m1"}, "standard": {"a/m1"}, "any": {"a/m1"}}
	if got := rc.workerCount(g.leaves("fn-greet")); got != 1 {
		t.Fatalf("workers = %d, want 1 (the mini is one)", got)
	}

}
