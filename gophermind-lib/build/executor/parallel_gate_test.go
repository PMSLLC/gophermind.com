package executor

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/human"
)

// overlapGate counts the escalations in flight and answers skip after a pause.
type overlapGate struct {
	*scriptGate
	cur, max, asked int32
}

func (o *overlapGate) Escalate(ctx context.Context, e human.Escalation) (human.Resolution, error) {
	atomic.AddInt32(&o.asked, 1)
	n := atomic.AddInt32(&o.cur, 1)
	for {
		m := atomic.LoadInt32(&o.max)
		if n <= m || atomic.CompareAndSwapInt32(&o.max, m, n) {
			break
		}
	}
	time.Sleep(150 * time.Millisecond)
	atomic.AddInt32(&o.cur, -1)
	return human.Resolution{Action: human.ActionSkip}, nil
}

// Spec 15.9 and F11: two leaves escalating at once reach the gate one at a time.
func TestTwoEscalationsReachTheGateOneAtATime(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0), workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": distinct("fn-farewell", 2),
		"implement:fn-greet":    distinct("fn-greet", 2),
	})
	failing(fc, "fn-farewell", 2)
	failing(fc, "fn-greet", 2)
	og := &overlapGate{scriptGate: g.gate}
	rc.o.Gate = og
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || !reflect.DeepEqual(wr.Failed, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if asked, max := atomic.LoadInt32(&og.asked), atomic.LoadInt32(&og.max); asked != 2 || max != 1 {
		t.Fatalf("the gate was asked %d times with %d at once, want 2 and 1", asked, max)
	}
}

// F8: workers above every provider's max_concurrent is a notice, not an error.
func TestWorkersAboveModelConcurrencyIsANotice(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(3))
	g.schedRC(t, Script{})
	found := false
	for _, e := range g.sink.OfKind("warning") {
		found = found || e.Message == "workers 3 exceeds model concurrency 1; extra workers only overlap checks"
	}
	if !found {
		t.Fatalf("warnings = %+v", g.sink.OfKind("warning"))
	}
	g1 := newRig(t)
	g1.schedRC(t, Script{})
	for _, e := range g1.sink.OfKind("warning") {
		if strings.Contains(e.Message, "model concurrency") {
			t.Fatal("workers 1 gave the notice")
		}
	}
}

// The report and the run state say a level ran in parallel.
func TestTheReportAndTheStateCountParallelLevels(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell", "fn-greet")
	if _, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet")); err != nil {
		t.Fatal(err)
	}
	rep, err := rc.buildReport(context.Background(), finishResult{Status: "failed", Reason: "leaves_not_verified"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Parallel == nil || rep.Parallel.Workers != 2 || rep.Parallel.ParallelLevels != 1 || rep.Parallel.LandingConflicts != 0 {
		t.Fatalf("parallel = %+v", rep.Parallel)
	}
	st, err := LoadState(g.runDir)
	if err != nil || st.Workers != 2 {
		t.Fatalf("state workers = %d, %v", st.Workers, err)
	}
}

// Ruling 16: a resume refused by live claims names every live leaf.
func TestResumeNamesEveryLiveLeaf(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.started(t)
	g.plantDead(t, "fn-greet", true)
	g.plantDead(t, "fn-farewell", true)
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		defer rc.close()
	}
	if err == nil || !strings.Contains(err.Error(), "nodes fn-farewell, fn-greet are held by live workers") {
		t.Fatalf("startRun error = %v", err)
	}
}
