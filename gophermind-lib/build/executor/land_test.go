package executor

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/runner"
)

// privateRunCtx runs one leaf the way a parallel level does: a private tree
// from a fresh base copy and a turn that follows prev (nil: the turn is free).
// tree, when set, receives the tree's path. Tree and base are removed after.
func privateRunCtx(t *testing.T, ctx context.Context, rc *runCtx, id string, prev <-chan struct{}, tree *string) (leafOutcome, error) {
	t.Helper()
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	wt, err := rc.makeTree(id)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.dropTree(wt)
	if tree != nil {
		*tree = wt.Repo
	}
	tn := newTurn(prev, &rc.sched.body)
	defer tn.release()
	return rc.runLeaf(ctx, rc.plan.Leaf(id), leafIn{Tree: wt, Turn: tn})
}

func privateRun(t *testing.T, rc *runCtx, id string, prev <-chan struct{}, tree *string) (leafOutcome, error) {
	t.Helper()
	return privateRunCtx(t, context.Background(), rc, id, prev, tree)
}

// eventOrder is the kinds, among those named, of the leaf's events in emission order.
func eventOrder(c *events.Collector, id string, kinds ...string) []string {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var out []string
	for _, e := range c.Events() {
		if e.NodeID == id && want[e.Kind] {
			out = append(out, e.Kind)
		}
	}
	return out
}

func candidatePath(g *rig, id string) string {
	return filepath.Join(g.runDir, "_state", candidatePrefix+id)
}

// repoLog records the repository of every leaf check.
func repoLog(fc *fakeChecker) func() []string {
	var mu sync.Mutex
	var repos []string
	fc.Hook = func(repo string) { mu.Lock(); repos = append(repos, repo); mu.Unlock() }
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), repos...) }
}

func TestAPrivatePassLandsInTheMainTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	repos := repoLog(fc)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), passVerdict()}
	var tree string
	out, err := privateRun(t, rc, leafID, nil, &tree)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if got := repos(); len(got) != 2 || got[0] != tree || got[1] != g.repo {
		t.Fatalf("checks ran in %q, want the tree then the main tree", got)
	}
	if n := g.leafCommits(leafID); n != 1 {
		t.Fatalf("leaf commits = %d", n)
	}
	if v := verdictsOf(t, g.board, leafID); v != "pass" {
		t.Fatalf("attempts = %s, want the one pass recorded at landing", v)
	}
	if a := attemptsOf(t, g.board, leafID)[0]; a.Provider != "a" || a.Model != "m1" {
		t.Fatalf("pass attempt = %+v", a)
	}
	if got := eventOrder(g.sink, leafID, "leaf_candidate", "leaf_landed", "leaf_verified"); !reflect.DeepEqual(got, []string{"leaf_candidate", "leaf_landed", "leaf_verified"}) {
		t.Fatalf("events = %v", got)
	}
	if ev := g.sink.OfKind("leaf_landed"); len(ev) != 1 || ev[0].Message == "" || ev[0].Message == "uncommitted" {
		t.Fatalf("leaf_landed = %+v, want the short hash", ev)
	}
	if fileExists(candidatePath(g, leafID)) || fileExists(tree) {
		t.Fatal("the candidate or the tree outlived the landing")
	}
}

func TestALandingConflictContinuesInTheMainTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(variant(good(leafID), 1)), reply(good(leafID))}}, true)
	repos := repoLog(fc)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), failVerdict(runner.ClassBuild, "./greet.go:9: clean redeclared in this block"), passVerdict()}
	var tree string
	out, err := privateRun(t, rc, leafID, nil, &tree)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if got := repos(); len(got) != 3 || got[0] != tree || got[1] != g.repo || got[2] != g.repo {
		t.Fatalf("checks ran in %q", got)
	}
	if ev := g.sink.OfKind("land_conflict"); len(ev) != 1 || ev[0].NodeID != leafID || ev[0].Message != runner.ClassBuild {
		t.Fatalf("land_conflict = %+v", ev)
	}
	if v := verdictsOf(t, g.board, leafID); v != "fail,pass" {
		t.Fatalf("attempts = %s, want the failed landing then the pass", v)
	}
	calls := g.leafCalls(leafID)
	if len(calls) != 2 || !strings.Contains(userText(calls[1]), "clean redeclared") {
		t.Fatal("the attempt after the conflict does not carry the landing failure")
	}
	repMu.Lock()
	n := rc.rep.conflicts
	repMu.Unlock()
	if n != 1 || g.leafCommits(leafID) != 1 || fileExists(candidatePath(g, leafID)) {
		t.Fatalf("conflicts %d, commits %d", n, g.leafCommits(leafID))
	}
}

// Review Focus 2: a candidate edited while it waited (a forbidden import and a
// secret) is refused by the gate at landing; nothing of it is committed or stored.
func TestATamperedCandidateIsRefusedAtLanding(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(variant(good(leafID), 1)), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), passVerdict()}
	turnFree := make(chan struct{})
	go func() {
		defer close(turnFree)
		for i := 0; i < 600 && !fileExists(candidatePath(g, leafID)); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		c, found, err := readCandidate(g.runDir, leafID)
		if err != nil || !found {
			t.Error("no candidate was saved")
			return
		}
		c.Source = strings.Replace(c.Source, `import "strings"`, "import (\n\t\"os/exec\"\n\t\"strings\"\n)\n\nvar runCmd = exec.Command", 1) + "\n// " + canarySecret + "\n"
		if err := saveCandidate(g.runDir, leafID, c); err != nil {
			t.Error(err)
		}
	}()
	out, err := privateRun(t, rc, leafID, turnFree, nil)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if ev := g.sink.OfKind("land_conflict"); len(ev) != 1 || ev[0].Message != ClassImportNotAllowed {
		t.Fatalf("land_conflict = %+v, want the gate's class", ev)
	}
	if strings.Contains(g.gitCmd("log", "-p"), "os/exec") {
		t.Fatal("the tampered source was committed")
	}
	if hasCanary(t, g, canarySecret) {
		t.Fatal("the secret in the tampered candidate reached a store")
	}
}

// Review Focus 2: a candidate written for an earlier revision is dropped and
// the ladder makes a fresh attempt.
func TestAStaleCandidateIsDroppedAndTheLadderRuns(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	if err := saveCandidate(g.runDir, leafID, candidate{Revision: 3, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "old", Source: good(leafID)}); err != nil {
		t.Fatal(err)
	}
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if n := len(g.leafCalls(leafID)); n != 1 {
		t.Fatalf("model calls = %d, want the fresh attempt", n)
	}
	found := false
	for _, e := range g.sink.OfKind("warning") {
		found = found || (e.NodeID == leafID && e.Message == "a saved candidate of an earlier revision was dropped")
	}
	if !found || fileExists(candidatePath(g, leafID)) {
		t.Fatal("the stale candidate was not dropped with a warning")
	}
}

// Spec section 11 step 3: a saved candidate lands on the next run with no model call.
func TestASavedCandidateLandsWithoutAModelCall(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	if err := saveCandidate(g.runDir, leafID, candidate{Revision: 0, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "s", Source: good(leafID)}); err != nil {
		t.Fatal(err)
	}
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("model calls = %d, want none", n)
	}
	a := attemptsOf(t, g.board, leafID)
	if len(a) != 1 || a[0].Provider != "a" || a[0].Model != "m1" || a[0].ReplySHA256 != "s" {
		t.Fatalf("attempts = %+v", a)
	}
	if g.leafCommits(leafID) != 1 || fileExists(candidatePath(g, leafID)) {
		t.Fatal("the candidate was not committed once and removed")
	}
}

// Ruling 6: the revise rung runs in the main tree; the leaf moves there first.
func TestAPrivateLeafMovesToTheMainTreeBeforeItsRevise(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1))
	rc, fc := g.leafRC(t, Script{
		"implement:" + leafID: append(distinct(leafID, 2), reply(good(leafID))),
		"revise:" + leafID:    {reviseHint("trim tabs too")},
	}, true)
	repos := repoLog(fc)
	failing(fc, leafID, 2)
	fc.LeafScript[leafID] = append(fc.LeafScript[leafID], passVerdict())
	var tree string
	out, err := privateRun(t, rc, leafID, nil, &tree)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if got := repos(); len(got) != 3 || got[0] != tree || got[1] != tree || got[2] != g.repo {
		t.Fatalf("checks ran in %q, want two in the tree and the last in the main tree", got)
	}
	if got := strings.Join(stagesOf(g.fake), " "); got != "implement:fn-greet implement:fn-greet revise:fn-greet implement:fn-greet" {
		t.Fatalf("stages = %s", got)
	}
	if len(g.sink.OfKind("leaf_candidate")) != 0 || g.leafCommits(leafID) != 1 {
		t.Fatal("the leaf after its revise must pass in the main tree directly")
	}
}

// Spec section 10: a leaf waiting for its turn when the context ends keeps its
// candidate, releases its claim and commits nothing.
func TestAnInterruptedWaitKeepsTheCandidate(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for i := 0; i < 600 && !fileExists(candidatePath(g, leafID)); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	out, err := privateRunCtx(t, ctx, rc, leafID, make(chan struct{}), nil)
	if err != nil || !out.Interrupted {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	l := rc.plan.Leaf(leafID)
	if !fileExists(candidatePath(g, leafID)) || g.leafCommits(leafID) != 0 || !fileExists(g.stubPath(l)) || fileExists(g.realPath(l)) {
		t.Fatal("an interrupted wait must keep the candidate, commit nothing and leave the stub")
	}
	if r := g.row(t, leafID); r.Status != blackboard.StatusReady || r.Claim != nil {
		t.Fatalf("row = %s claim %v, want ready and released", r.Status, r.Claim)
	}
}
