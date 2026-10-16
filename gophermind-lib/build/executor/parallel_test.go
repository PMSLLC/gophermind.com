package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/gitland"
	"gophermind/gophermind-lib/build/runner"
	"gophermind/gophermind-lib/build/settings"
)

// workersN sets executor.workers after any rig option given before it.
func workersN(n int) func(*rigOpts) {
	return func(o *rigOpts) {
		prev := o.Settings
		o.Settings = func(c *settings.Config) {
			if prev != nil {
				prev(c)
			}
			c.Executor.Workers = n
		}
	}
}

// twice scripts a pass in the private tree and a pass at landing for each leaf.
func twice(fc *fakeChecker, ids ...string) {
	for _, id := range ids {
		fc.LeafScript[id] = append(fc.LeafScript[id], passVerdict(), passVerdict())
	}
}

// leafSubjects is the subject of every leaf commit on the current branch, oldest first.
func (g *rig) leafSubjects() []string {
	out := strings.TrimSpace(g.gitCmd("log", "--reverse", "--format=%s", "--grep=^gm(fn-"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// subjectIDs is the leaf id of each "gm(<id>): ..." subject.
func subjectIDs(subjects []string) []string {
	var out []string
	for _, s := range subjects {
		if id, _, ok := strings.Cut(strings.TrimPrefix(s, "gm("), "): "); ok {
			out = append(out, id)
		}
	}
	return out
}

// hookSink calls on, then passes the event on.
type hookSink struct {
	next events.Sink
	on   func(events.Event)
}

func (s hookSink) Emit(e events.Event) { s.on(e); s.next.Emit(e) }

// barrier lets its first n callers through together, or each after 20 s.
// arrive is true for a caller that met the others; later callers pass at once
// and get false.
type barrier struct {
	mu   sync.Mutex
	n    int
	seen int
	all  chan struct{}
}

func newBarrier(n int) *barrier { return &barrier{n: n, all: make(chan struct{})} }

func (b *barrier) arrive() bool {
	b.mu.Lock()
	b.seen++
	k := b.seen
	if k == b.n {
		close(b.all)
	}
	b.mu.Unlock()
	if k > b.n {
		return false
	}
	select {
	case <-b.all:
		return true
	case <-time.After(20 * time.Second):
		return false
	}
}

// blockingChecker blocks every private check until the context ends.
type blockingChecker struct {
	*fakeChecker
	main    string
	started chan string
}

func (b *blockingChecker) CheckLeaf(ctx context.Context, c runner.LeafCheck) runner.Verdict {
	if c.Repo == b.main {
		return b.fakeChecker.CheckLeaf(ctx, c)
	}
	b.started <- c.Repo
	<-ctx.Done()
	return runner.Verdict{Class: runner.ClassCancelled}
}

func treesLeft(t *testing.T, rc *runCtx) []string {
	t.Helper()
	var out []string
	for _, d := range []string{"trees", "w"} {
		entries, _ := os.ReadDir(filepath.Join(rc.scratch, d))
		for _, e := range entries {
			out = append(out, d+"/"+e.Name())
		}
	}
	if fileExists(filepath.Join(rc.scratch, "base")) {
		out = append(out, "base")
	}
	return out
}

func TestAParallelLevelOverlapsAndLandsInIDOrder(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell", "fn-greet")
	meet := newBarrier(2)
	var mu sync.Mutex
	var private []string
	met := 0
	fc.Hook = func(repo string) {
		if repo == g.repo {
			return
		}
		ok := meet.arrive()
		mu.Lock()
		private = append(private, repo)
		if ok {
			met++
		}
		mu.Unlock()
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-greet", "fn-farewell"))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("scheduleLeaves = %+v, %v", wr, err)
	}
	if met != 2 || len(private) != 2 || private[0] == private[1] {
		t.Fatalf("private checks %q, met %d: want two distinct trees at once", private, met)
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("commit order = %v", got)
	}
	var verified []string
	for _, e := range g.sink.OfKind("leaf_verified") {
		verified = append(verified, e.NodeID)
	}
	if !reflect.DeepEqual(verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("leaf_verified order = %v", verified)
	}
	if ev := g.sink.OfKind("level_parallel"); len(ev) != 1 || !strings.HasSuffix(ev[0].Message, "level 1: 2 leaves, 2 workers") {
		t.Fatalf("level_parallel = %+v", ev)
	}
	if n := len(g.sink.OfKind("tree_ready")); n != 2 {
		t.Fatalf("tree_ready events = %d", n)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Spec 15.7: cancel while the leaves of a parallel level are being checked.
func TestCancelDuringAParallelLevel(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	bc := &blockingChecker{fakeChecker: fc, main: g.repo, started: make(chan string, 4)}
	rc.chk = bc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-bc.started
		<-bc.started
		cancel()
	}()
	wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop == nil || wr.Stop.Status != "interrupted" || len(wr.Verified)+len(wr.Failed) != 0 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	for _, id := range []string{"fn-farewell", "fn-greet"} {
		l := rc.plan.Leaf(id)
		if r := g.row(t, id); r.Status != blackboard.StatusReady || r.Claim != nil {
			t.Errorf("%s = %s claim %v, want ready and released", id, r.Status, r.Claim)
		}
		if g.leafCommits(id) != 0 || fileExists(candidatePath(g, id)) || !fileExists(g.stubPath(l)) {
			t.Errorf("%s: a commit, a candidate or a missing stub after the cancel", id)
		}
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Spec 15.8: a panic in one leaf fails that leaf; a panic outside a leaf body
// is a harness fault of its job; the other leaf lands and the process lives.
func TestPanicInOneLeafLetsTheOtherLand(t *testing.T) {
	t.Parallel()
	t.Run("inside the leaf", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, workersN(2))
		rc, fc := g.schedRC(t, Script{
			"implement:fn-farewell": {reply(good("fn-farewell"))},
			"implement:fn-greet":    {reply(good("fn-greet"))},
		})
		fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
		twice(fc, "fn-greet")
		fc.Hook = func(repo string) {
			if filepath.Base(repo) == "fn-farewell" {
				panic("boom")
			}
		}
		wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
		if err != nil || !reflect.DeepEqual(wr.Failed, []string{"fn-farewell"}) || !reflect.DeepEqual(wr.Verified, []string{"fn-greet"}) {
			t.Fatalf("wave = %+v, %v", wr, err)
		}
		if left := treesLeft(t, rc); len(left) != 0 {
			t.Fatalf("left behind: %v", left)
		}
	})
	t.Run("outside the leaf", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, workersN(2))
		rc, fc := g.schedRC(t, Script{"implement:fn-farewell": {reply(good("fn-farewell"))}})
		twice(fc, "fn-farewell")
		rc.afterTree = func(id string) {
			if id == "fn-greet" {
				panic("tree glue")
			}
		}
		wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
		if err == nil || !strings.Contains(err.Error(), "panicked") || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell"}) {
			t.Fatalf("wave = %+v, %v", wr, err)
		}
		if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusReady || r.Claim != nil {
			t.Fatalf("fn-greet = %s claim %v", r.Status, r.Claim)
		}
		if left := treesLeft(t, rc); len(left) != 0 {
			t.Fatalf("left behind: %v", left)
		}
	})
}

// F12: a level whose copy would be over the limit runs serially with a warning.
func TestATreeTooLargeRunsTheLevelSerially(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	f, err := os.Create(filepath.Join(g.repo, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTreeBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	assertSerialFallback(t, g, rc, fc, "this level runs one leaf at a time: the tree copy is over 256 MiB")
}

// Review Focus 1: a link out of the repository makes the level serial, not a failure.
func TestALinkOutOfTheTreeRunsTheLevelSerially(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	if err := os.Symlink("/etc/hosts", filepath.Join(g.repo, "hosts-link")); err != nil {
		t.Fatal(err)
	}
	assertSerialFallback(t, g, rc, fc, "this level runs one leaf at a time: a file of the repository cannot be copied into a private tree")
}

func assertSerialFallback(t *testing.T, g *rig, rc *runCtx, fc *fakeChecker, warning string) {
	t.Helper()
	fc.Hook = func(repo string) {
		if repo != g.repo {
			t.Errorf("a check ran in %s", repo)
		}
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	found := false
	for _, e := range g.sink.OfKind("warning") {
		found = found || e.Message == warning
	}
	if !found || len(g.sink.OfKind("level_parallel")) != 0 {
		t.Fatalf("want the warning %q and no parallel level; warnings %+v", warning, g.sink.OfKind("warning"))
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Review Focus 5: two leaves sharing a path go to different sub-levels.
func TestSplitOverlapsPutsASharedPathInALaterSubLevel(t *testing.T) {
	t.Parallel()
	a := &Leaf{ID: "fn-a", File: "p/a.go", StubFile: "p/zz_gm_stub_fn-a.go", TestFile: "p/shared_test.go"}
	b := &Leaf{ID: "fn-b", File: "p/b.go", StubFile: "p/zz_gm_stub_fn-b.go", TestFile: "p/shared_test.go"}
	c := &Leaf{ID: "fn-c", File: "p/c.go", StubFile: "p/zz_gm_stub_fn-c.go", TestFile: "p/fn_c_test.go"}
	ids := func(ls []*Leaf) string {
		var out []string
		for _, l := range ls {
			out = append(out, l.ID)
		}
		return strings.Join(out, " ")
	}
	got := splitOverlaps([]*Leaf{a, b, c})
	if len(got) != 2 || ids(got[0]) != "fn-a fn-c" || ids(got[1]) != "fn-b" {
		t.Fatalf("sub-levels = %d: %v", len(got), got)
	}
	if got := splitOverlaps([]*Leaf{a, c}); len(got) != 1 {
		t.Fatalf("leaves that share nothing were split: %d", len(got))
	}
}

func TestOverlappingLeavesOfALevelLandOneAfterTheOther(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	ls := g.leaves("fn-farewell", "fn-greet")
	ls[1].TestFile, ls[1].TestSHA256 = ls[0].TestFile, ls[0].TestSHA256 // a plan defect: one test file for two leaves
	wr, err := rc.scheduleLeaves(context.Background(), ls)
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if len(g.sink.OfKind("level_parallel")) != 0 || len(g.sink.OfKind("tree_ready")) != 0 {
		t.Fatal("overlapping leaves ran in parallel")
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("commit order = %v", got)
	}
}

// needsMain: a real file on disk, a candidate, a reopen marker or an
// escalated row keeps a leaf in the main tree (ruling 5).
func TestNeedsMainKeepsResumedLeavesInTheMainTree(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, _ := g.schedRC(t, Script{})
	farewell, greet := rc.plan.Leaf("fn-farewell"), rc.plan.Leaf("fn-greet")
	if m, err := rc.needsMain(greet, blackboard.StatusPending); err != nil || m {
		t.Fatalf("a stub-only leaf: %v %v", m, err)
	}
	if m, _ := rc.needsMain(greet, blackboard.StatusEscalated); !m {
		t.Fatal("an escalated leaf may run in a private tree")
	}
	// A valid candidate, written by the real writer: readCandidate keeps it.
	if err := saveCandidate(g.runDir, "fn-greet", candidate{Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "abc123", Source: good("fn-greet")}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := readCandidate(g.runDir, "fn-greet"); err != nil || !found {
		t.Fatalf("the fixture candidate is not one readCandidate keeps: %v %v", found, err)
	}
	if m, _ := rc.needsMain(greet, blackboard.StatusReady); !m {
		t.Fatal("a leaf with a candidate may run in a private tree")
	}
	g.plantReal(t, "fn-farewell", good("fn-farewell"))
	if m, _ := rc.needsMain(farewell, blackboard.StatusReady); !m {
		t.Fatal("a leaf with a real file on disk may run in a private tree")
	}
}

// commitFailGit fails the leaf commit of one leaf: a harness fault at landing.
type commitFailGit struct {
	gitland.Repo
	id string
}

func (c commitFailGit) CommitLeaf(id, title string, add, remove []string) (string, error) {
	if id == c.id {
		return "", errors.New("commit refused")
	}
	return c.Repo.CommitLeaf(id, title, add, remove)
}

// Ruling C5 (spec 7.3): after a landing commit fails, the jobs behind it do
// not land; each ends interrupted with its candidate kept.
func TestAFailedLandingCommitStopsTheLeavesBehindIt(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell")
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()} // its private pass only: it never lands
	rc.git = commitFailGit{Repo: rc.git, id: "fn-farewell"}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err == nil || !strings.Contains(err.Error(), "committing leaf fn-farewell failed") || len(wr.Verified) != 0 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if g.leafCommits("fn-farewell")+g.leafCommits("fn-greet") != 0 {
		t.Fatal("a leaf landed after a failed landing commit")
	}
	if !fileExists(candidatePath(g, "fn-greet")) {
		t.Fatal("the candidate of the leaf behind the failed landing was not kept")
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusReady || r.Claim != nil {
		t.Fatalf("fn-greet = %s claim %v, want ready and released", r.Status, r.Claim)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Rulings 5 and (a): a main job of a parallel level waits for its turn and
// holds it (the body mutex) for the whole leaf; its saved candidate lands in
// the main tree with no model call.
func TestAMainJobHoldsItsTurnWhileItsCandidateLands(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{})
	l := rc.plan.Leaf("fn-farewell")
	if err := rc.o.Board.SetStatus(context.Background(), rc.plan.RunID, l.ID, blackboard.StatusPending, blackboard.StatusReady); err != nil {
		t.Fatal(err)
	}
	if err := saveCandidate(g.runDir, l.ID, candidate{Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "abc123", Source: good(l.ID)}); err != nil {
		t.Fatal(err)
	}
	fc.LeafScript[l.ID] = []runner.Verdict{passVerdict()}
	var mu sync.Mutex
	var repos []string
	var held []bool
	fc.Hook = func(repo string) {
		free := rc.sched.body.TryLock()
		if free {
			rc.sched.body.Unlock()
		}
		mu.Lock()
		repos, held = append(repos, repo), append(held, !free)
		mu.Unlock()
	}
	prev := make(chan struct{})
	tn := newTurn(prev, &rc.sched.body)
	j := &job{l: l, status: blackboard.StatusReady, main: true}
	type res struct {
		out leafOutcome
		err error
	}
	done := make(chan res, 1)
	go func() {
		defer tn.release()
		out, err := rc.runParallelJob(context.Background(), j, tn)
		done <- res{out, err}
	}()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	early := len(repos)
	mu.Unlock()
	if early != 0 {
		t.Fatal("a main job checked before its turn")
	}
	close(prev)
	r := <-done
	if r.err != nil || r.out.Status != blackboard.StatusVerified {
		t.Fatalf("runParallelJob = %+v, %v", r.out, r.err)
	}
	if !reflect.DeepEqual(repos, []string{g.repo}) || !held[0] {
		t.Fatalf("checks in %q, turn held %v: want one check in the main tree under the turn", repos, held)
	}
	if g.leafCommits(l.ID) != 1 || len(g.leafCalls(l.ID)) != 0 || fileExists(candidatePath(g, l.ID)) {
		t.Fatal("the candidate did not land in the main tree without a model call")
	}
}
