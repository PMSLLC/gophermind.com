package executor

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/proxy"
	"gophermind/gophermind-lib/briefv2/runner"
)

// verifiedProbe notes how many attempts the row holds at the moment a leaf is
// set verified.
type verifiedProbe struct {
	blackboard.Blackboard
	attemptsAtVerified atomic.Int32
	seen               atomic.Bool
}

func (b *verifiedProbe) SetStatus(ctx context.Context, runID, nodeID string, from, to blackboard.Status) error {
	if to == blackboard.StatusVerified {
		if row, err := b.Blackboard.Get(ctx, runID, nodeID); err == nil {
			b.attemptsAtVerified.Store(int32(len(row.Attempts)))
			b.seen.Store(true)
		}
	}
	return b.Blackboard.SetStatus(ctx, runID, nodeID, from, to)
}

// The pass attempt is on the row before the row says verified, so a crash
// between the two cannot lose it.
func TestPassAttemptRecordedBeforeVerified(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	probe := &verifiedProbe{Blackboard: rc.o.Board}
	rc.o.Board = probe
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if !probe.seen.Load() || probe.attemptsAtVerified.Load() != 1 {
		t.Fatalf("attempts on the row when it was set verified = %d (seen %v), want 1", probe.attemptsAtVerified.Load(), probe.seen.Load())
	}
	if v := verdictsOf(t, g.board, leafID); v != "pass" {
		t.Fatalf("verdicts = %q, want pass", v)
	}
}

type panicChecker struct{ Checker }

func (panicChecker) CheckLeaf(context.Context, runner.LeafCheck) runner.Verdict {
	panic("boom " + canarySecret)
}

// A panic inside a leaf ends that leaf in a terminal status and does not take
// the run down; nothing of the panic value is kept.
func TestRunLeafPanicBecomesTerminalFailure(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	rc.chk = panicChecker{rc.chk}
	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatalf("runLeaf returned an error: %v", err)
	}
	if out.Status != blackboard.StatusFailed || out.Reason != reasonPanic {
		t.Fatalf("outcome = %+v, want failed with reason %s", out, reasonPanic)
	}
	if r := g.row(t, leafID); r.Status != blackboard.StatusFailed {
		t.Fatalf("row = %s, want failed", r.Status)
	}
	if !fileExists(g.stubPath(g.plan.Leaf(leafID))) {
		t.Error("the stub was not restored")
	}
	if hasCanary(t, g, canarySecret) {
		t.Error("the panic value reached a store")
	}
}

// claimCancelBoard cancels the run's context right after the n-th claim has
// been taken, the way a cancel can land between the claim and the check.
type claimCancelBoard struct {
	blackboard.Blackboard
	n      int32
	calls  atomic.Int32
	cancel context.CancelFunc
}

func (b *claimCancelBoard) Claim(ctx context.Context, runID, nodeID, worker string) (bool, error) {
	ok, err := b.Blackboard.Claim(ctx, runID, nodeID, worker)
	if b.calls.Add(1) == b.n {
		b.cancel()
	}
	return ok, err
}

// A human retry that is cancelled after it took the claim releases the claim.
func TestCancelledHumanRetryReleasesTheClaim(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 4)}, true)
	failing(fc, id, 4)
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc.o.Board = &claimCancelBoard{Blackboard: rc.o.Board, n: 2, cancel: cancel}
	out, err := rc.runLeaf(ctx, g.plan.Leaf(id), leafIn{})
	if err != nil {
		t.Fatalf("runLeaf: %v", err)
	}
	if !out.Interrupted {
		t.Fatalf("outcome = %+v, want interrupted", out)
	}
	if r := g.row(t, id); r.Status != blackboard.StatusReady {
		t.Fatalf("row = %s, want ready: the claim leaked", r.Status)
	}
}

// Notes (from the revise rung and from a person) never hold a secret value.
func TestNotesAreScrubbed(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	id := leafID
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: append(distinct(id, 4), reply(good(id))),
		"revise:" + id:    {reviseHint("model hint " + canarySecret)},
	}, true)
	failing(fc, id, 4)
	fc.LeafScript[id] = append(fc.LeafScript[id], passVerdict())
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "token is " + canarySecret}}
	if out := runOne(t, rc, id); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	notes, err := LoadNotes(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes[id]) == 0 {
		t.Fatal("no notes were kept")
	}
	for _, n := range notes[id] {
		if strings.Contains(n, canarySecret) {
			t.Fatalf("a note holds the secret: %q", n)
		}
	}
}

// A warning line never holds a secret value (a host can be built from data).
func TestWarningLineIsScrubbed(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	line := rc.warningLine(proxy.Failure{Host: "x" + canarySecret + ".example.com", Kind: "denied"})
	if strings.Contains(line, canarySecret) {
		t.Fatalf("warning line holds the secret: %q", line)
	}
	if got := rc.warningLine(proxy.Failure{Host: "api.example.com", Kind: "denied"}); got != "a request to api.example.com failed (denied)" {
		t.Fatalf("clean warning line = %q", got)
	}
}

// A reply whose source holds a secret value is refused like any forbidden
// write: it is never written, run or committed.
func TestReplySourceWithSecretIsRefused(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	leaky := strings.Replace(good(leafID), "package greet", "package greet\n\n// "+canarySecret, 1)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(leaky), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if fc.checks(leafID) != 1 {
		t.Fatalf("checks = %d, want 1: the leaky reply must not be checked", fc.checks(leafID))
	}
	if v := verdictsOf(t, g.board, leafID); v != "fail,pass" {
		t.Fatalf("verdicts = %q, want fail,pass", v)
	}
	if r := reasonsOf(t, g.board, leafID); r[0] != ClassForbiddenWrite {
		t.Fatalf("reason = %q, want %s", r[0], ClassForbiddenWrite)
	}
	if hasCanary(t, g, canarySecret) {
		t.Error("the secret reached a store")
	}
}
