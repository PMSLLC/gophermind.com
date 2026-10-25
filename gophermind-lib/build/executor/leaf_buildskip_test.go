package executor

import (
	"context"
	"testing"

	"gophermind/gophermind-lib/build/runner"
)

// A model that failed to compile a whole round of attempts is not asked again
// by the next revision while another entry is left.
func TestEntryThatFailedToBuildIsSkippedNextRevision(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	var steps []step
	for i := 0; i < 9; i++ {
		steps = append(steps, reply(variant(bad(leafID, 1), i)))
	}
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: steps}, true)
	for i := 0; i < 3; i++ {
		fc.LeafScript[leafID] = append(fc.LeafScript[leafID], failVerdict(runner.ClassBuild, "x"))
	}
	for i := 0; i < 6; i++ {
		fc.LeafScript[leafID] = append(fc.LeafScript[leafID], failVerdict("test_fail", "x", "TestX"))
	}
	lr, err := rc.newLeafRun(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if res, err := lr.oneRevision(context.Background()); err != nil || res != revExhausted {
			t.Fatalf("round %d: oneRevision = %v, %v; want revExhausted", round, res, err)
		}
	}
	var onA, onB int
	for _, c := range implementCalls(t, g.led, leafID) {
		if c.Provider == "a" {
			onA++
		} else {
			onB++
		}
	}
	if onA != 3 || onB != 6 {
		t.Fatalf("calls a=%d b=%d, want 3 and 6: a failed to build a whole round and must not be asked again", onA, onB)
	}
}
