package executor

import (
	"context"
	"runtime"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/settings"
)

// goroutinesSettle fails the test unless the goroutine count comes back to at
// most before within 5 s. Meaningful only in a test that is not parallel
// (ruling 20): Go pauses parallel tests while a sequential one runs.
func goroutinesSettle(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= before {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			buf = buf[:runtime.Stack(buf, true)]
			t.Fatalf("%d goroutines outlived the level (before %d):\n%s", n-before, before, buf)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Spec 14 item 10: no goroutine outlives a parallel level. Not parallel on purpose.
func TestNoGoroutineOutlivesAParallelLevel(t *testing.T) {
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell", "fn-greet")
	before := runtime.NumGoroutine()
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop != nil || len(wr.Verified) != 2 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	goroutinesSettle(t, before)
}

// panicGate panics on its first escalation and answers like its scriptGate after.
type panicGate struct {
	*scriptGate
	panicked bool
}

func (p *panicGate) Escalate(ctx context.Context, e human.Escalation) (human.Resolution, error) {
	if !p.panicked {
		p.panicked = true
		panic("gate failed")
	}
	return p.scriptGate.Escalate(ctx, e)
}

// Task 8 review (a): a gate that panics must not leave the gate mutex held, or
// every later escalating leaf of the run waits for ever. runLeaf turns the
// panic into a failure of that leaf; the next leaf must still reach the gate.
func TestAPanicInTheGateDoesNotWedgeLaterEscalations(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.schedRC(t, Script{})
	g.gate.waiting = true
	rc.o.Gate = &panicGate{scriptGate: g.gate}
	l := g.plan.Leaf("fn-greet")
	func() {
		defer func() { _ = recover() }()
		_, _ = rc.escalate(context.Background(), l, blackboard.StatusEscalated, "escalated", nil)
	}()
	done := make(chan error, 1)
	go func() {
		_, err := rc.escalate(context.Background(), l, blackboard.StatusEscalated, "escalated", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if se, ok := err.(*stopError); !ok || se.Reason != "waiting_on_human" {
			t.Fatalf("second escalation = %v, want waiting_on_human", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second escalation never reached the gate: the gate mutex stayed locked after a panic")
	}
}

// Task 8 review (b), spec 8.4: the notice compares workers with the providers
// of the executor chains only: standard and strong (checkChains, the revise
// stage) and the tier of every leaf (ladderEntries). A fast provider named only
// in a chain no leaf uses does not raise the model concurrency; once a leaf
// uses that chain, it does.
func TestModelConcurrencyReadsOnlyTheExecutorChains(t *testing.T) {
	t.Parallel()
	cfg := settings.Default()
	cfg.Providers = []settings.ProviderConfig{
		{Name: "a", MaxConcurrent: 1}, {Name: "c", MaxConcurrent: 4},
	}
	cfg.Models = map[string][]string{"standard": {"a/m1"}, "strong": {"a/m1"}, "any": {"c/m3"}}
	standard := []*Leaf{{ID: "fn-a", Tier: "standard"}, {ID: "fn-b", Tier: "strong"}}
	if m := modelConcurrency(cfg, standard); m != 1 {
		t.Fatalf("leaves on standard and strong: model concurrency %d, want 1 (the any chain counted)", m)
	}
	if m := modelConcurrency(cfg, append(standard, &Leaf{ID: "fn-c", Tier: "any"})); m != 4 {
		t.Fatalf("a leaf on any: model concurrency %d, want 4", m)
	}
}
