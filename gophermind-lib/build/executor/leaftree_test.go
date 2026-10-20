package executor

import (
	"context"
	"sync"
	"testing"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/runner"
)

// A serial leaf is checked in the main tree as before; a check in a private
// tree reaches the checker with the tree as its repository.
func TestLeafChecksGoToTheLeafsWorkTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	var mu sync.Mutex
	var repos []string
	fc.Hook = func(repo string) { mu.Lock(); repos = append(repos, repo); mu.Unlock() }
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	wt, err := rc.makeTree("fn-farewell")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.dropTree(wt)
	l := rc.plan.Leaf("fn-farewell")
	fc.LeafScript[l.ID] = []runner.Verdict{passVerdict()}
	rc.leafCheckIn(context.Background(), wt, l, []string{l.File})
	mu.Lock()
	defer mu.Unlock()
	if len(repos) != 2 || repos[0] != g.repo || repos[1] != wt.Repo {
		t.Fatalf("checks ran in %q", repos)
	}
}
