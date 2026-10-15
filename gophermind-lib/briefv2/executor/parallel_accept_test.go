package executor

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/runner"
)

// recChecker records the test function and repository of every leaf check and
// makes the first two private checks meet at a barrier (ruling 19).
type recChecker struct {
	Checker
	main  string
	meet  *barrier
	mu    sync.Mutex
	calls []runner.LeafCheck
	met   int
}

func (r *recChecker) CheckLeaf(ctx context.Context, c runner.LeafCheck) runner.Verdict {
	r.mu.Lock()
	r.calls = append(r.calls, runner.LeafCheck{TestFunc: c.TestFunc, Repo: c.Repo})
	r.mu.Unlock()
	if r.meet != nil && c.Repo != r.main && r.meet.arrive() {
		r.mu.Lock()
		r.met++
		r.mu.Unlock()
	}
	return r.Checker.CheckLeaf(ctx, c)
}

// GOAL-UNIFICATION task 6 acceptance (spec 15.1): six independent leaves with
// workers 3, the real runner, all verified, fixed commit order, no file outside
// a leaf's paths, private checks in distinct trees that overlapped.
func TestParallelSixIndependentLeaves(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 3))
	g.wire(sixScript(g))
	rec := &recChecker{main: g.repo, meet: newBarrier(2)}
	rep, err := run(context.Background(), g.options(), runFlags{afterStart: func(rc *runCtx) { rec.Checker = rc.chk; rc.chk = rec }})
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s) %v: %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	var six []string
	for _, id := range subjectIDs(g.leafSubjects()) {
		if inList(sixIDs, id) {
			six = append(six, id)
		}
	}
	if !reflect.DeepEqual(six, sixIDs) {
		t.Fatalf("leaf commits of the level in order %v, want %v", six, sixIDs)
	}
	for _, id := range sixIDs {
		l := g.plan.Leaf(id)
		hash := strings.TrimSpace(g.gitCmd("log", "--format=%H", "--grep=^gm("+id+"): "))
		files := strings.Fields(g.gitCmd("show", "--name-only", "--format=", hash))
		want := []string{l.File, l.StubFile}
		sort.Strings(files)
		sort.Strings(want)
		if !reflect.DeepEqual(files, want) {
			t.Errorf("%s commit changes %v, want %v", id, files, want)
		}
	}
	wave0 := strings.TrimSpace(g.gitCmd("log", "--format=%H", "--grep=^gm(wave0)"))
	allowed := map[string]bool{}
	for _, l := range g.plan.Leaves {
		allowed[l.File], allowed[l.StubFile] = true, true
	}
	for _, f := range strings.Fields(g.gitCmd("diff", "--name-only", wave0+"..HEAD")) {
		if !allowed[f] {
			t.Errorf("%s changed outside every leaf's paths", f)
		}
	}
	if st := strings.TrimSpace(g.gitCmd("status", "--porcelain")); st != "" {
		t.Fatalf("the tree is not clean:\n%s", st)
	}
	byFunc := map[string]string{}
	for _, l := range g.plan.Leaves {
		byFunc[l.TestFunc] = l.ID
	}
	scratch := filepath.Join(g.repo, ".gophermind", g.id+"-scratch")
	trees, landed := map[string]string{}, map[string]int{}
	for _, c := range rec.calls {
		id := byFunc[c.TestFunc]
		if !inList(sixIDs, id) {
			continue
		}
		if c.Repo == g.repo {
			landed[id]++
			continue
		}
		if want := filepath.Join(scratch, "trees", id); c.Repo != want {
			t.Errorf("%s was checked in %s, want its own tree", id, c.Repo)
		}
		trees[id] = c.Repo
	}
	for _, id := range sixIDs {
		if landed[id] != 1 {
			t.Errorf("%s has %d landing checks, want 1", id, landed[id])
		}
	}
	if len(trees) != 6 || len(landed) != 6 || rec.met != 2 {
		t.Fatalf("private trees %d, leaves landed %d, met %d: want 6, 6, 2", len(trees), len(landed), rec.met)
	}
	if ev := g.sink.OfKind("level_parallel"); len(ev) == 0 || !strings.Contains(ev[0].Message, "6 leaves, 3 workers") {
		t.Fatalf("level_parallel = %+v", ev)
	}
	var ver []string
	for _, e := range g.sink.OfKind("leaf_verified") {
		if inList(sixIDs, e.NodeID) {
			ver = append(ver, e.NodeID)
		}
	}
	if !reflect.DeepEqual(ver, sixIDs) {
		t.Fatalf("leaf_verified order = %v", ver)
	}
	// Ruling 18: the second wave's package is verified and landed too.
	verified := map[string]bool{}
	for _, e := range g.sink.OfKind("leaf_verified") {
		verified[e.NodeID] = true
	}
	subjects := subjectIDs(g.leafSubjects())
	for _, id := range []string{"fn-bye", "fn-hello"} {
		l := g.plan.Leaf(id)
		if l == nil || l.Package != "main" || l.Wave <= g.plan.Leaf("fn-greet").Wave {
			t.Errorf("%s is not a later-wave main leaf: %+v", id, l)
		}
		if !verified[id] || !inList(subjects, id) {
			t.Errorf("%s verified %v, landed %v", id, verified[id], inList(subjects, id))
		}
	}
	if rep.Parallel == nil || rep.Parallel.Workers != 3 || rep.Parallel.ParallelLevels < 1 {
		t.Fatalf("report parallel = %+v", rep.Parallel)
	}
	for _, d := range []string{"trees", "w", "base"} {
		if entries, _ := os.ReadDir(filepath.Join(scratch, d)); len(entries) != 0 {
			t.Errorf("%s holds %d entries after the run", d, len(entries))
		}
	}
	if m, _ := filepath.Glob(filepath.Join(g.runDir, "_state", candidatePrefix+"*")); len(m) != 0 {
		t.Errorf("candidates left: %v", m)
	}
}

type levelRun struct{ subjects, nodes, verified []string }

// sixLevelRun runs the six-leaf level once with the fake checker, each check
// delayed by a random 0 to 30 ms (seed 0: no delay).
func sixLevelRun(t *testing.T, workers int, seed int64) levelRun {
	t.Helper()
	g := newRig(t, sixpack(t, workers))
	rc, _ := g.schedRC(t, sixScript(g))
	fc := g.sixChecker()
	rc.chk = fc
	if seed != 0 {
		rng := rand.New(rand.NewSource(seed))
		var mu sync.Mutex
		fc.Hook = func(string) {
			mu.Lock()
			d := time.Duration(rng.Intn(30)) * time.Millisecond
			mu.Unlock()
			time.Sleep(d)
		}
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
		t.Fatalf("workers %d seed %d: %+v, %v", workers, seed, wr, err)
	}
	var out levelRun
	out.subjects = g.leafSubjects()
	out.nodes = strings.Fields(g.gitCmd("log", "--reverse", "--format=%(trailers:key=GopherMind-Node,valueonly)", "--grep=^gm(fn-"))
	for _, e := range g.sink.OfKind("leaf_verified") {
		out.verified = append(out.verified, e.NodeID)
	}
	return out
}

// Spec 15.5: twenty runs with random delays and 2, 3 or 6 workers give the
// serial run's commit subjects, node trailers and leaf_verified order.
func TestParallelDeterministicCommitOrder(t *testing.T) {
	t.Parallel()
	serial := sixLevelRun(t, 1, 0)
	if !reflect.DeepEqual(subjectIDs(serial.subjects), sixIDs) {
		t.Fatalf("serial order = %v", serial.subjects)
	}
	for i := 0; i < 20; i++ {
		w := []int{2, 3, 6}[i%3]
		got := sixLevelRun(t, w, int64(i+1))
		if !reflect.DeepEqual(got.subjects, serial.subjects) || !reflect.DeepEqual(got.nodes, serial.nodes) || !reflect.DeepEqual(got.verified, sixIDs) {
			t.Fatalf("run %d (workers %d): subjects %v nodes %v verified %v; serial %v %v", i, w, got.subjects, got.nodes, got.verified, serial.subjects, serial.nodes)
		}
	}
}

// Review Focus 4: with more leaves than workers, never more than `workers`
// trees on disk or checks in flight, and nothing left after.
func TestMoreLeavesThanWorkersNeverHoldMoreTrees(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 2))
	rc, _ := g.schedRC(t, sixScript(g))
	fc := g.sixChecker()
	rc.chk = fc
	treesDir := filepath.Join(rc.scratch, "trees")
	var mu sync.Mutex
	maxTrees, cur, maxChecks := 0, 0, 0
	fc.Hook = func(string) {
		entries, _ := os.ReadDir(treesDir)
		mu.Lock()
		if len(entries) > maxTrees {
			maxTrees = len(entries)
		}
		cur++
		if cur > maxChecks {
			maxChecks = cur
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		cur--
		mu.Unlock()
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	if err != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if maxTrees < 1 || maxTrees > 2 || maxChecks > 2 {
		t.Fatalf("trees at once %d, checks at once %d: want at most 2", maxTrees, maxChecks)
	}
	if ev := g.sink.OfKind("level_parallel"); len(ev) != 1 || !strings.HasSuffix(ev[0].Message, "6 leaves, 2 workers") {
		t.Fatalf("level_parallel = %+v", ev)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Ruling 5: a leaf with a real file on disk is adopted in the main tree in its
// turn while the others run in private trees; no model call for it.
func TestAnAdoptedLeafOfAParallelLevelLandsInItsTurn(t *testing.T) {
	t.Parallel()
	const adopted = "fn-greet" // third by id, workers 3: landing out of turn breaks the order
	g := newRig(t, sixpack(t, 3))
	rc, _ := g.schedRC(t, sixScript(g))
	g.plantReal(t, adopted, sixGood(adopted))
	rec := &recChecker{Checker: g.sixChecker(), main: g.repo}
	rc.chk = rec
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	if err != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if n := len(g.leafCalls(adopted)); n != 0 {
		t.Fatalf("the adopted leaf made %d model calls", n)
	}
	byFunc := map[string]string{}
	for _, l := range g.plan.Leaves {
		byFunc[l.TestFunc] = l.ID
	}
	scratch := filepath.Join(g.repo, ".gophermind", g.id+"-scratch")
	inMain, private := map[string]int{}, map[string]string{}
	for _, c := range rec.calls {
		id := byFunc[c.TestFunc]
		if c.Repo == g.repo {
			inMain[id]++
		} else {
			private[id] = c.Repo
		}
	}
	if _, ok := private[adopted]; ok || inMain[adopted] != 1 {
		t.Fatalf("the adopted leaf: main checks %d, private %q: want 1 main check, none private", inMain[adopted], private[adopted])
	}
	for _, id := range sixIDs {
		if id == adopted {
			continue
		}
		if want := filepath.Join(scratch, "trees", id); private[id] != want {
			t.Errorf("%s was checked in %q, want its own tree %s", id, private[id], want)
		}
	}
	if ev := g.sink.OfKind("level_parallel"); len(ev) != 1 || !strings.HasSuffix(ev[0].Message, "6 leaves, 3 workers") {
		t.Fatalf("level_parallel = %+v", ev)
	}
	if !reflect.DeepEqual(subjectIDs(g.leafSubjects()), sixIDs) {
		t.Fatalf("order %v", g.leafSubjects())
	}
}
