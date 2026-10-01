package executor

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/runner"
)

// manualClock is the injected wall clock of the limit: nothing happens until a
// test calls fire.
type manualClock struct {
	mu    sync.Mutex
	d     []time.Duration
	f     func()
	fired bool
}

func (m *manualClock) afterFunc(d time.Duration, f func()) (stop func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.d = append(m.d, d)
	m.f = f
	return func() {}
}

func (m *manualClock) fire() {
	m.mu.Lock()
	f := m.f
	m.fired = true
	m.mu.Unlock()
	if f != nil {
		f()
	}
}

func (m *manualClock) durations() []time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Duration(nil), m.d...)
}

func TestMaxRunMinutesStopsCleanly(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	clock := &manualClock{}
	var tb *traceBoard
	script := goodScript(g)
	script["implement:fn-greet"] = []step{{Text: good("fn-greet"), Delay: 2 * time.Second}}
	g.wire(script)
	g.fake.Before = func(stage string) {
		if stage == "implement:fn-greet" {
			tb.note("LIMIT")
			clock.fire() // the limit elapses while the call is in flight
		}
	}
	fc := g.fastChecker()
	started := time.Now()
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: func(rc *runCtx) {
		rc.chk, rc.afterFunc = fc, clock.afterFunc
		tb = traced(rc)
	}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if time.Since(started) > 30*time.Second {
		t.Error("the run did not stop promptly")
	}
	if rep.Status != "interrupted" || rep.StopReason != "max_run_minutes" || rep.ExitCode != 5 {
		t.Fatalf("report = %s (%s) exit %d, want interrupted max_run_minutes 5", rep.Status, rep.StopReason, rep.ExitCode)
	}
	rd := readReportFile(t, g)
	if rd.Status != "interrupted" || rd.StopReason != "max_run_minutes" {
		t.Errorf("report.json = %s (%s)", rd.Status, rd.StopReason)
	}
	rows, err := g.board.List(context.Background(), g.id, blackboard.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Status == blackboard.StatusClaimed || r.Status == blackboard.StatusInProgress || r.Claim != nil {
			t.Errorf("%s is %s with claim %v after the run: claims must be released", r.NodeID, r.Status, r.Claim)
		}
	}
	after := false
	for _, e := range tb.Trace() {
		if e == "LIMIT" {
			after = true
		} else if after && len(e) > 5 && e[:5] == "claim" {
			t.Errorf("%q: a claim was made after the limit", e)
		}
	}
	greet := g.implementRows(t, "fn-greet")
	if len(greet) != 1 || greet[0].ErrorKind != "cancelled" {
		t.Fatalf("the cut-off call's ledger rows = %+v, want one with error_kind cancelled", greet)
	}
	if row := g.row(t, "fn-greet"); row.Status != blackboard.StatusReady || len(row.Attempts) != 0 || row.Revision != 0 {
		t.Errorf("fn-greet = %s attempts %d revision %d, want ready with nothing charged", row.Status, len(row.Attempts), row.Revision)
	}
	if !fileExists(g.stubPath(g.plan.Leaf("fn-greet"))) {
		t.Error("the stub of the cut-off leaf was not restored")
	}

	// A resume starts the limit from zero: the whole of max_run_minutes again.
	clock2 := &manualClock{}
	g.wire(Script{
		"implement:fn-greet": {reply(good("fn-greet"))}, "implement:fn-bye": {reply(good("fn-bye"))},
		"implement:fn-hello": {reply(good("fn-hello"))}, "implement:fn-serve": {reply(good("fn-serve"))},
	})
	fc2 := newFakeChecker(g)
	fc2.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	for _, id := range []string{"fn-greet", "fn-bye", "fn-hello", "fn-serve"} {
		fc2.LeafScript[id] = []runner.Verdict{passVerdict()}
	}
	rep, err = run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: func(rc *runCtx) { rc.chk, rc.afterFunc = fc2, clock2.afterFunc }})
	if err != nil || rep.Status != "verified" || !rep.Resumed {
		t.Fatalf("second run = %s (%s) resumed %v, %v, failures %v", rep.Status, rep.StopReason, rep.Resumed, err, rep.Failures)
	}
	if d := clock2.durations(); len(d) != 1 || d[0] != time.Duration(g.cfg.Executor.MaxRunMinutes)*time.Minute {
		t.Errorf("the second run's limit = %v, want the full %d minutes", d, g.cfg.Executor.MaxRunMinutes)
	}
}

// The real timer: rc.limit replaces max_run_minutes, and the limit measures
// from the call of runContext.
func TestRunContextLimitFiresWithRealTimer(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.schedRC(t, Script{})
	rc.limit = 50 * time.Millisecond
	ctx, cause, stop := rc.runContext(context.Background())
	defer stop()
	if cause() != "" {
		t.Fatalf("cause at the start = %q", cause())
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the limit never fired")
	}
	if cause() != "max_run_minutes" {
		t.Errorf("cause = %q, want max_run_minutes", cause())
	}
	// A parent that ends first is cancelled, not a limit.
	rc.limit = time.Hour
	parent, cancel := context.WithCancel(context.Background())
	ctx2, cause2, stop2 := rc.runContext(parent)
	defer stop2()
	cancel()
	<-ctx2.Done()
	if cause2() != "cancelled" {
		t.Errorf("cause = %q, want cancelled", cause2())
	}
}

// Not parallel: the signal goes to the whole test binary.
func TestInterruptReleasesClaims(t *testing.T) {
	t.Cleanup(func() { signal.Reset(os.Interrupt, syscall.SIGTERM) })
	g := newRig(t)
	script := goodScript(g)
	script["implement:fn-greet"] = []step{{Text: good("fn-greet"), Delay: 2 * time.Second}}
	g.wire(script)
	g.fake.Before = func(stage string) {
		if stage == "implement:fn-greet" {
			_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		}
	}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(g.fastChecker())})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Status != "interrupted" || rep.StopReason != "signal" || rep.ExitCode != 5 {
		t.Fatalf("report = %s (%s) exit %d, want interrupted signal 5", rep.Status, rep.StopReason, rep.ExitCode)
	}
	rows, err := g.board.List(context.Background(), g.id, blackboard.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Status == blackboard.StatusClaimed || r.Status == blackboard.StatusInProgress || r.Claim != nil {
			t.Errorf("%s is %s with claim %v: claims must be released", r.NodeID, r.Status, r.Claim)
		}
	}
	if rd := readReportFile(t, g); rd.StopReason != "signal" {
		t.Errorf("report.json stop_reason = %q", rd.StopReason)
	}

	g.wire(Script{
		"implement:fn-greet": {reply(good("fn-greet"))}, "implement:fn-bye": {reply(good("fn-bye"))},
		"implement:fn-hello": {reply(good("fn-hello"))}, "implement:fn-serve": {reply(good("fn-serve"))},
	})
	rep, err = run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(g.fastChecker())})
	if err != nil || rep.Status != "verified" || !rep.Resumed {
		t.Fatalf("resumed run = %s (%s) resumed %v, %v", rep.Status, rep.StopReason, rep.Resumed, err)
	}
}

// hbBoard counts the heartbeats the run sends.
type hbBoard struct {
	blackboard.Blackboard
	n atomic.Int64
}

func (b *hbBoard) Heartbeat(ctx context.Context, runID, nodeID, worker string) error {
	b.n.Add(1)
	return b.Blackboard.Heartbeat(ctx, runID, nodeID, worker)
}

func TestHeartbeatStopsWithLeaf(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	script := Script{"implement:fn-greet": {{Text: good("fn-greet"), Delay: 400 * time.Millisecond}}}
	rc, fc := g.leafRC(t, script, true)
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	hb := &hbBoard{Blackboard: rc.o.Board}
	rc.o.Board = hb
	rc.heartbeat = 50 * time.Millisecond

	var during atomic.Int64
	g.fake.Before = func(stage string) {
		first := g.row(t, "fn-greet").HeartbeatAt
		time.AfterFunc(300*time.Millisecond, func() {
			if g.row(t, "fn-greet").HeartbeatAt.After(first) {
				during.Store(1)
			}
		})
	}
	if out := runOne(t, rc, "fn-greet"); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	n1 := hb.n.Load()
	if n1 < 3 {
		t.Errorf("heartbeats during a 400 ms leaf at 50 ms = %d, want at least 3", n1)
	}
	if during.Load() != 1 {
		t.Error("HeartbeatAt did not advance while the leaf was held")
	}
	time.Sleep(300 * time.Millisecond)
	if n2 := hb.n.Load(); n2 != n1 {
		t.Errorf("heartbeats after the leaf ended went from %d to %d: the goroutine outlived the leaf", n1, n2)
	}
}
