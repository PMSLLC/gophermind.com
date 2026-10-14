package executor

import (
	"context"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/runner"
)

// A revision step that runs entirely between chainsIntact's copy of the
// in-memory tips and its read lock makes the files lead the memory: a spurious
// plan_changed (re-review of sub-project E). The reader must take the lock
// before it reads the memory.
func TestChainsIntactReadsTheMemoryUnderTheChainLock(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet")} // the red check of the new test
	prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	rc.afterRevStep = func(n int) error {
		if n == 6 {
			return errRevisionKilled
		}
		return nil
	}
	if _, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); err != errRevisionKilled {
		t.Fatalf("applyRevision = %v", err)
	}
	rc.afterRevStep = nil
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	recs, _ := revlog.Read(dir)
	commit := recs[0]
	commit.Kind = revlog.KindCommit
	// The memory as a reader saw it before the intent: no tip for the node.
	rc.plan.revMu.Lock()
	delete(rc.plan.Revised, leafID)
	rc.plan.revMu.Unlock()

	// A writer holds the chain lock, as a transaction step does, and finishes
	// the commit (record and tip) before it lets go.
	rc.plan.chainMu.Lock()
	got := make(chan bool, 1)
	go func() { got <- rc.plan.chainsIntact() }()
	time.Sleep(200 * time.Millisecond) // the reader is now blocked on the lock
	if err := revlog.Append(dir, commit); err != nil {
		rc.plan.chainMu.Unlock()
		t.Fatal(err)
	}
	rc.plan.setTip(leafID, RevisedTip{Key: rc.plan.nodeKey(leafID), N: commit.N, BodySHA256: commit.BodySHA256, TestSHA256: commit.TestSHA256After})
	rc.plan.chainMu.Unlock()
	if !<-got {
		t.Fatal("chainsIntact compared a memory read before the lock with files written under it")
	}
}

// Smoke test (ruling C1): chainsIntact on one goroutine never fails while a
// whole revision transaction runs on another (the race detector watches every
// shared field). The deterministic proof is the test above.
func TestChainsIntactSmokeWhileARevisionRunsOnAnotherGoroutine(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet")}
	prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	stop := make(chan struct{})
	bad := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if !rc.plan.chainsIntact() {
				bad <- struct{}{}
				return
			}
		}
	}()
	_, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-bad:
		t.Fatal("chainsIntact failed while a revision ran")
	default:
	}
}
