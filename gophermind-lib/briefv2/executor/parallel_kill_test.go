package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/runner"
)

// Spec 15.2: one leaf fails its ladder and writes a stray file in its tree;
// the others land in id order, untouched. Each sibling tree is hashed at its
// private check and again at its landing (ruling C15): only the leaf's own
// file and stub may differ.
func TestParallelFailingLeafDoesNotTouchOthers(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0), sixpack(t, 3))
	script := sixScript(g)
	script["implement:fn-greet"] = distinct("fn-greet", 2)
	rc, _ := g.schedRC(t, script)
	fc := g.sixChecker()
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), failVerdict("test_fail", "x", "TestGreet")}
	rc.chk = fc
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	stray := filepath.Join("internal", "greet", "stray_note.txt")
	var mu sync.Mutex
	var leaks []string
	wrote := false
	hashes := map[string]string{} // sibling id -> hash at its private check
	var changed []string
	compared := 0
	siblingHash := func(id, repo string) string {
		l := rc.plan.Leaf(id)
		return treeHash(t, repo, l.File, l.StubFile)
	}
	rc.o.Sink = hookSink{next: rc.o.Sink, on: func(e events.Event) {
		if e.Kind != "leaf_landed" {
			return
		}
		h := siblingHash(e.NodeID, filepath.Join(rc.scratch, "trees", e.NodeID))
		mu.Lock()
		defer mu.Unlock()
		compared++
		if h != hashes[e.NodeID] {
			changed = append(changed, e.NodeID)
		}
	}}
	fc.Hook = func(repo string) {
		if repo == g.repo {
			return
		}
		if id := filepath.Base(repo); id != "fn-greet" {
			h := siblingHash(id, repo)
			mu.Lock()
			if _, seen := hashes[id]; !seen {
				hashes[id] = h
			}
			mu.Unlock()
		}
		if filepath.Base(repo) == "fn-greet" {
			mu.Lock()
			first := !wrote
			wrote = true
			mu.Unlock()
			if first {
				_ = os.WriteFile(filepath.Join(repo, stray), []byte("x"), 0o644)
			}
			return
		}
		if fileExists(filepath.Join(repo, stray)) {
			mu.Lock()
			leaks = append(leaks, filepath.Base(repo))
			mu.Unlock()
		}
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	others := []string{"fn-cheer", "fn-farewell", "fn-shout", "fn-thank", "fn-welcome"}
	if err != nil || !reflect.DeepEqual(wr.Verified, others) || !reflect.DeepEqual(wr.Failed, []string{"fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if len(leaks) != 0 || fileExists(filepath.Join(g.repo, stray)) {
		t.Fatalf("the stray file reached %v or the main tree", leaks)
	}
	if compared != len(others) || len(changed) != 0 {
		t.Fatalf("sibling trees compared %d, changed between check and landing: %v", compared, changed)
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, others) {
		t.Fatalf("commit order = %v", got)
	}
	l := rc.plan.Leaf("fn-greet")
	if fileExists(candidatePath(g, "fn-greet")) || !fileExists(g.stubPath(l)) || fileExists(g.realPath(l)) {
		t.Fatal("the failed leaf left a candidate or lost its stub")
	}
	if r := reasonsOf(t, g.board, "fn-greet"); len(r) != 2 || !strings.HasPrefix(r[0], ClassForbiddenWrite) {
		t.Fatalf("reasons = %v", r)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Spec 15.3: two leaves of one package declare the same helper. The lower id
// lands; the higher is refused at landing by the gate (ruling 9), goes on in
// the main tree and verifies; commit order is still id order.
func TestParallelLandingConflictFallsBackToSerial(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	helper := "\nfunc clean(s string) string { return strings.TrimSpace(s) }\n"
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell") + helper)},
		"implement:fn-greet":    {reply(good("fn-greet") + helper), reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell")
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), passVerdict()}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	ev := g.sink.OfKind("land_conflict")
	if len(ev) != 1 || ev[0].NodeID != "fn-greet" || ev[0].Message != runner.ClassMalformed {
		t.Fatalf("land_conflict = %+v", ev)
	}
	if v := verdictsOf(t, g.board, "fn-greet"); v != "fail,pass" {
		t.Fatalf("fn-greet attempts = %s", v)
	}
	calls := g.leafCalls("fn-greet")
	if len(calls) != 2 || !strings.Contains(userText(calls[1]), noteMalformed) {
		t.Fatal("the attempt after the conflict does not say why")
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("commit order = %v", got)
	}
}

// Spec 15.4, in process: cancel before the first landing (three candidates
// saved) or after the second landing, then resume with two workers.
func TestParallelKillAndResume(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		name, kind  string
		at, landed  int
		wantCandMin int
	}{
		{"before the first landing", "leaf_candidate", 3, 0, 3},
		{"after the second landing", "leaf_landed", 2, 2, 0},
	} {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t, sixpack(t, 3))
			rc, _ := g.schedRC(t, sixScript(g))
			g.fake.Loop = true // a leaf cut off before its candidate asks again on resume
			fc := g.sixChecker()
			rc.chk = fc
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			release := make(chan struct{})
			var once sync.Once
			var mu sync.Mutex
			n, cands := 0, 0
			if v.kind == "leaf_candidate" {
				// The lowest id waits in its check until two others saved a candidate, so none lands first.
				fc.Hook = func(repo string) {
					if filepath.Base(repo) == "fn-cheer" {
						select {
						case <-release:
						case <-time.After(30 * time.Second):
						}
					}
				}
			}
			rc.o.Sink = hookSink{next: rc.o.Sink, on: func(e events.Event) {
				mu.Lock()
				defer mu.Unlock()
				if e.Kind == "leaf_candidate" {
					if cands++; cands == 2 {
						once.Do(func() { close(release) })
					}
				}
				if e.Kind == v.kind {
					if n++; n == v.at {
						cancel()
					}
				}
			}}
			wr, err := rc.scheduleLeaves(ctx, g.leaves(sixIDs...))
			if err != nil || wr.Stop == nil || wr.Stop.Status != "interrupted" {
				t.Fatalf("first run = %+v, %v", wr, err)
			}
			committed := 0
			for _, id := range sixIDs {
				committed += g.leafCommits(id)
			}
			had := map[string]bool{}
			for _, id := range sixIDs {
				if fileExists(candidatePath(g, id)) {
					had[id] = true
				}
			}
			if committed != v.landed || len(had) < v.wantCandMin {
				t.Fatalf("after the stop: %d commits, candidates %v", committed, had)
			}
			if left := treesLeft(t, rc); len(left) != 0 {
				t.Fatalf("left behind: %v", left)
			}
			rc.close()

			g.cfg.Executor.Workers = 2
			before := map[string]int{}
			for _, id := range sixIDs {
				before[id] = len(g.leafCalls(id))
			}
			rc2, err := startRun(context.Background(), g.options())
			if rc2 != nil {
				defer rc2.close()
			}
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			rc2.chk = g.sixChecker()
			wr2, err := rc2.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
			if err != nil || wr2.Stop != nil || !reflect.DeepEqual(wr2.Verified, sixIDs) {
				t.Fatalf("resumed run = %+v, %v", wr2, err)
			}
			for id := range had {
				if n := len(g.leafCalls(id)) - before[id]; n != 0 {
					t.Errorf("%s had a candidate and made %d model calls on resume", id, n)
				}
			}
			for _, id := range sixIDs {
				if c := g.leafCommits(id); c != 1 {
					t.Errorf("%s committed %d times", id, c)
				}
			}
			if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, sixIDs) {
				t.Fatalf("commit order = %v", got)
			}
		})
	}
}

// TestParallelChildProcess is the process TestParallelSIGKILLAndResume kills.
// It parks itself at a safe point: the lowest id (fn-cheer, the first to land)
// waits forever in its private check, so no landing write has begun and the
// main tree is quiet. Of the other leaves only the first two pass their check;
// any further one waits in its check too, before it writes a candidate. Once
// the second candidate is saved the child names the point and waits to be
// killed, so no worker is mid-write in the main tree or the state directory.
func TestParallelChildProcess(t *testing.T) {
	if os.Getenv("GM_PAR_CHILD") != "1" {
		return
	}
	dir := os.Getenv("GM_PAR_DIR")
	g := newRigIn(t, dir, Script{}, sixpack(t, 3))
	g.wire(sixScript(g))
	fc := g.sixChecker()
	var mu sync.Mutex
	allowed := map[string]bool{}
	fc.Hook = func(repo string) {
		if repo == g.repo {
			return
		}
		id := filepath.Base(repo)
		mu.Lock()
		ok := id != "fn-cheer" && (allowed[id] || len(allowed) < 2)
		if ok {
			allowed[id] = true
		}
		mu.Unlock()
		if !ok {
			select {} // parked until the parent kills the process
		}
	}
	n := 0
	hook := func(rc *runCtx) {
		rc.chk = fc
		rc.o.Sink = hookSink{next: rc.o.Sink, on: func(e events.Event) {
			if e.Kind != "leaf_candidate" {
				return
			}
			mu.Lock()
			n++
			k := n
			mu.Unlock()
			if k == 2 {
				blockHere(dir)
			}
		}}
	}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: hook})
	t.Fatalf("the child ran to its end (%s) without reaching the kill point: %v", rep.Status, err)
}

// Spec 15.4, SIGKILL: a process killed mid-level leaves stale claims and
// candidates; the next run (workers 2) lands the candidates with no model call.
// The kill is the parent's SIGKILL, sent only once the child reports it is
// parked at the safe point of TestParallelChildProcess. A kill at a random
// instant can land inside a file write and leave a `.name.go.tmp...` file or a
// git lock behind; the executor then stops with foreign_dirt, which is the
// conservative behavior of the serial swap code and is covered by
// TestKillMidWriteLeavesForeignDirtAndResumeStopsCleanly, not by this test.
func TestParallelSIGKILLAndResume(t *testing.T) {
	t.Parallel()
	if goCacheOverride == "" {
		t.Skip("no shared go cache")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newRigIn(t, dir, Script{}, sixpack(t, 3)).close() // plans once, in the parent
	cmd := exec.Command(os.Args[0], "-test.run=^TestParallelChildProcess$")
	cmd.Env = append(os.Environ(), "GM_PAR_CHILD=1", "GM_PAR_DIR="+dir, "GM_TEST_GOCACHE="+goCacheOverride)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	marker := filepath.Join(dir, "in-flight")
	deadline := time.After(8 * time.Minute)
wait:
	for {
		select {
		case <-exited:
			t.Fatalf("the child exited before the kill point:\n%s", out.String())
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("the child never reached the kill point:\n%s", out.String())
		case <-time.After(50 * time.Millisecond):
			if fileExists(marker) {
				break wait
			}
		}
	}
	_ = cmd.Process.Signal(syscall.SIGKILL)
	<-exited
	runDir := filepath.Join(dir, "repo", ".gophermind", greeterID)
	had := map[string]bool{}
	for _, id := range sixIDs {
		if fileExists(filepath.Join(runDir, "_state", candidatePrefix+id)) {
			had[id] = true
		}
	}
	if len(had) == 0 {
		t.Fatalf("the kill left no candidate:\n%s", out.String())
	}
	scratch := filepath.Join(dir, "repo", ".gophermind", greeterID+"-scratch")
	killedTrees := dirEntries(filepath.Join(scratch, "trees")) + dirEntries(filepath.Join(scratch, "w"))
	time.Sleep(1500 * time.Millisecond) // stale_claim_seconds is 1 in newRigIn
	g := newRigIn(t, dir, Script{}, sixpack(t, 2))
	g.wire(sixScript(g))
	// Spec 15.4: when the resume announces itself, the killed run's trees are
	// gone and its stale claims are swept.
	var atResume struct {
		seen          bool
		trees, claims []string
	}
	opts := g.options()
	opts.Sink = hookSink{next: opts.Sink, on: func(e events.Event) {
		if e.Kind != "resume" || atResume.seen {
			return
		}
		atResume.seen = true
		for _, d := range []string{"trees", "w", "base"} {
			if fileExists(filepath.Join(scratch, d)) && (d == "base" || dirEntries(filepath.Join(scratch, d)) > 0) {
				atResume.trees = append(atResume.trees, d)
			}
		}
		for _, id := range sixIDs {
			if r := g.row(t, id); r.Status == blackboard.StatusClaimed || r.Status == blackboard.StatusInProgress || r.Claim != nil {
				atResume.claims = append(atResume.claims, id+":"+string(r.Status))
			}
		}
	}}
	rep, err := run(context.Background(), opts, runFlags{skipAcceptance: true, afterStart: useChecker(g.sixChecker())})
	if err != nil || rep.Status != "verified" {
		t.Fatalf("resumed run = %s (%s) %v: %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	if !atResume.seen {
		t.Fatal("the resume event was never emitted")
	}
	if killedTrees == 0 {
		t.Error("the kill left no private tree to clean, the check below proves nothing")
	}
	if len(atResume.trees) != 0 {
		t.Errorf("at the resume, trees/w/base were left: %v", atResume.trees)
	}
	if len(atResume.claims) != 0 {
		t.Errorf("at the resume, stale claims were not swept: %v", atResume.claims)
	}
	for id := range had {
		if n := len(g.leafCalls(id)); n != 0 {
			t.Errorf("%s had a candidate and made %d model calls", id, n)
		}
	}
	var six []string
	for _, id := range subjectIDs(g.leafSubjects()) {
		if inList(sixIDs, id) {
			six = append(six, id)
		}
	}
	if !reflect.DeepEqual(six, sixIDs) {
		t.Fatalf("commit order = %v", six)
	}
	for _, id := range sixIDs {
		if c := g.leafCommits(id); c != 1 {
			t.Errorf("%s committed %d times", id, c)
		}
	}
}

// A kill in the middle of a file write leaves the writer's temp file in the
// main tree. The resume treats it as foreign dirt: it stops, names the file,
// changes nothing and calls no model. That is the serial swap code's
// conservative behavior, pinned here so the parallel level does not change it.
func TestKillMidWriteLeavesForeignDirtAndResumeStopsCleanly(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 3))
	g.started(t)
	l := g.plan.Leaf("fn-cheer")
	stray := filepath.Join(g.repo, filepath.Dir(filepath.FromSlash(l.File)), ".cheer.go.tmp123")
	write(t, stray, "package greet\n// half a file")
	spy := &spyGit{Repo: g.git}
	g.wire(sixScript(g))
	o := g.options()
	o.Git = spy
	rep, err := run(context.Background(), o, runFlags{skipAcceptance: true, afterStart: useChecker(g.sixChecker())})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Status != "failed" || rep.StopReason != "foreign_dirt" || rep.ExitCode != 1 {
		t.Fatalf("report = %s (%s) exit %d", rep.Status, rep.StopReason, rep.ExitCode)
	}
	if !strings.Contains(strings.Join(rep.Failures, "\n"), ".cheer.go.tmp123") {
		t.Errorf("failures %q do not name the temp file", rep.Failures)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("provider calls = %d, want 0", n)
	}
	if raw, err := os.ReadFile(stray); err != nil || string(raw) != "package greet\n// half a file" {
		t.Errorf("the temp file = %q, %v; want it untouched", raw, err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("git calls %v, want none that discard work", spy.calls)
	}
	for _, id := range sixIDs {
		if c := g.leafCommits(id); c != 0 {
			t.Errorf("%s committed %d times", id, c)
		}
	}
}

// treeHash hashes the files of a tree, path and content, but its .git and the
// given paths (slash separated, relative to root).
func treeHash(t *testing.T, root string, skip ...string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() && rel == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() || inList(skip, rel) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Errorf("hashing %s: %v", root, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// diffOnly makes the brief land as diff_only.
func diffOnly() func(*rigOpts) {
	return func(o *rigOpts) {
		prev := o.BriefEdit
		o.BriefEdit = func(s string) string {
			if prev != nil {
				s = prev(s)
			}
			return strings.Replace(s, "landing: commit", "landing: diff_only", 1)
		}
	}
}

// Spec 15.13: diff_only with three workers leaves the same working tree diff
// as one worker, and commits no leaf.
func TestParallelDiffOnlyMatchesSerial(t *testing.T) {
	t.Parallel()
	diff := func(workers int) []byte {
		g := newRig(t, diffOnly(), sixpack(t, workers))
		rc, _ := g.schedRC(t, sixScript(g))
		rc.chk = g.sixChecker()
		wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
		if err != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
			t.Fatalf("workers %d: %+v, %v", workers, wr, err)
		}
		for _, id := range sixIDs {
			if g.leafCommits(id) != 0 || !fileExists(g.realPath(g.plan.Leaf(id))) {
				t.Fatalf("workers %d: %s was committed or is not on disk", workers, id)
			}
		}
		d, err := rc.git.Diff("main")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if s, p := diff(1), diff(3); !bytes.Equal(s, p) {
		t.Fatalf("diff_only diffs differ:\nserial %d bytes, parallel %d bytes", len(s), len(p))
	}
}

// dirEntries counts the entries of a directory, 0 when it is missing.
func dirEntries(p string) int {
	e, _ := os.ReadDir(p)
	return len(e)
}
