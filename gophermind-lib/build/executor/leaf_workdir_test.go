package executor

import (
	"testing"
)

// The request a leaf attempt makes names the leaf's worktree and its target
// file, which providers that work on disk (Cline) need.
func TestLeafRequestCarriesWorkdirAndTargetFile(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, false)
	l := g.plan.Leaf(leafID)
	runOne(t, rc, leafID)

	reqs := g.leafCalls(leafID)
	if len(reqs) != 1 {
		t.Fatalf("calls = %d, want 1", len(reqs))
	}
	if reqs[0].Workdir != g.repo {
		t.Errorf("Workdir = %q, want %q", reqs[0].Workdir, g.repo)
	}
	if reqs[0].TargetFile != l.File {
		t.Errorf("TargetFile = %q, want %q", reqs[0].TargetFile, l.File)
	}
}
