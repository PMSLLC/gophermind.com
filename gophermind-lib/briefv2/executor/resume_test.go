package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// cancelAt cancels a context when the run emits an event of the kind (for the
// node, when one is named): the in-process stand-in for a kill between rows.
type cancelAt struct {
	*events.Collector
	kind, node string
	cancel     context.CancelFunc
	once       sync.Once
}

func (c *cancelAt) Emit(e events.Event) {
	c.Collector.Emit(e)
	if e.Kind == c.kind && (c.node == "" || e.NodeID == c.node) {
		c.once.Do(c.cancel)
	}
}

// interruptedRun runs the executor with a fake checker and cancels it when the
// event happens; it returns the report of the interrupted invocation.
func (g *rig) interruptedRun(t *testing.T, script Script, fc *fakeChecker, kind, node string) Report {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.wire(script)
	o := g.options()
	o.Sink = &cancelAt{Collector: g.sink, kind: kind, node: node, cancel: cancel}
	rep, err := run(ctx, o, runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil {
		t.Fatalf("the interrupted run returned an error: %v", err)
	}
	if rep.Status != "interrupted" {
		t.Fatalf("the interrupted run = %s (%s), failures %v", rep.Status, rep.StopReason, rep.Failures)
	}
	return rep
}

// started makes a run that has done Wave 0 and nothing else, with the real
// checks, as a process killed before its first claim leaves it.
func (g *rig) started(t *testing.T) {
	t.Helper()
	g.wire(Script{})
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		rc.close()
	}
	if err != nil {
		t.Fatalf("startRun: %v", err)
	}
}

// plantDead is a worker killed while it held the leaf: claimed, or in_progress.
func (g *rig) plantDead(t *testing.T, id string, inProgress bool) {
	t.Helper()
	ctx := context.Background()
	if err := g.board.SetStatus(ctx, g.id, id, blackboard.StatusPending, blackboard.StatusReady); err != nil {
		t.Fatal(err)
	}
	if ok, err := g.board.Claim(ctx, g.id, id, "dead:"+id); err != nil || !ok {
		t.Fatalf("claim %s = %v, %v", id, ok, err)
	}
	if inProgress {
		if err := g.board.SetStatus(ctx, g.id, id, blackboard.StatusClaimed, blackboard.StatusInProgress); err != nil {
			t.Fatal(err)
		}
	}
}

// plantReal is the disk a worker killed after Swap.Enter leaves: the stub gone
// and the real file in its place.
func (g *rig) plantReal(t *testing.T, id, src string) {
	t.Helper()
	l := g.plan.Leaf(id)
	if err := os.Remove(g.stubPath(l)); err != nil {
		t.Fatal(err)
	}
	write(t, g.realPath(l), src)
}

// staleOne makes a claim stale after one second (the smallest setting).
func staleOne(o *rigOpts) {
	base := o.Settings
	o.Settings = func(c *settings.Config) {
		if base != nil {
			base(c)
		}
		c.Executor.StaleClaimSeconds = 1
	}
}

// age waits until a claim made now is older than stale_claim_seconds (1).
func age() { time.Sleep(1300 * time.Millisecond) }

func (g *rig) verifiedIDs(t *testing.T) []string {
	t.Helper()
	rows, err := g.board.List(context.Background(), g.id, blackboard.Filter{Statuses: []blackboard.Status{blackboard.StatusVerified}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		if g.plan.Leaf(r.NodeID) != nil {
			ids = append(ids, r.NodeID)
		}
	}
	return ids
}

func (g *rig) implementRows(t *testing.T, id string) []ledger.Call {
	t.Helper()
	rows, err := g.led.List(context.Background(), g.id, ledger.Filter{TaskType: "implement", NodeID: id})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestStaleClaimsReleased(t *testing.T) {
	t.Parallel()
	t.Run("stale claims are released and listed in one resume event", func(t *testing.T) {
		g := newRig(t, staleOne)
		g.started(t)
		g.plantDead(t, "fn-farewell", false)
		g.plantDead(t, "fn-greet", true)
		age()
		rc, err := startRun(context.Background(), g.options())
		if rc != nil {
			defer rc.close()
		}
		if err != nil {
			t.Fatalf("startRun: %v", err)
		}
		for _, id := range []string{"fn-farewell", "fn-greet"} {
			if row := g.row(t, id); row.Status != blackboard.StatusReady || row.Claim != nil {
				t.Errorf("%s = %s claim %v, want ready and unclaimed", id, row.Status, row.Claim)
			}
		}
		evs := g.sink.OfKind("resume")
		if len(evs) != 1 || !strings.Contains(evs[0].Message, "fn-farewell") || !strings.Contains(evs[0].Message, "fn-greet") {
			t.Fatalf("resume events = %+v, want one naming both released leaves", evs)
		}
	})
	t.Run("a fresh heartbeat is a live worker", func(t *testing.T) {
		g := newRig(t)
		g.started(t)
		g.plantDead(t, "fn-greet", true)
		rc, err := startRun(context.Background(), g.options())
		if rc != nil {
			defer rc.close()
		}
		if err == nil || !strings.Contains(err.Error(), "node fn-greet is held by a live worker") {
			t.Fatalf("startRun error = %v, want the live worker error naming fn-greet", err)
		}
		if _, isStop := stopOf(err); isStop {
			t.Error("a live worker is a harness fault, not a stop")
		}
		if row := g.row(t, "fn-greet"); row.Status != blackboard.StatusInProgress {
			t.Errorf("fn-greet = %s, want it left in_progress", row.Status)
		}
	})
}

func TestResumeRefusesChangedPlan(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, g *rig, want string) {
		t.Helper()
		before, err := g.board.List(context.Background(), g.id, blackboard.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		g.wire(goodScript(g))
		rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(g.fastChecker())})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("run = %s (%s), %v; want an error containing %q", rep.Status, rep.StopReason, err, want)
		}
		if rep.Status != "" {
			t.Errorf("a refused resume returned a report: %s", rep.Status)
		}
		if n := len(g.fake.Requests()); n != 0 {
			t.Errorf("provider calls = %d, want 0", n)
		}
		after, err := g.board.List(context.Background(), g.id, blackboard.Filter{})
		if err != nil || len(after) != len(before) {
			t.Fatalf("rows = %d, %v", len(after), err)
		}
		for i := range before {
			if before[i].Status != after[i].Status || before[i].UpdatedAt != after[i].UpdatedAt {
				t.Errorf("row %s changed", before[i].NodeID)
			}
		}
	}
	t.Run("a node file", func(t *testing.T) {
		g := newRig(t)
		g.started(t)
		var path string
		for k := range g.plan.Hashes {
			if strings.HasPrefix(k, "tree/") && strings.Contains(k, "fn-greet") {
				path = filepath.Join(g.runDir, strings.TrimPrefix(k, "tree/"))
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("node file: %v", err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		check(t, g, "plan files changed")
	})
	t.Run("the approval", func(t *testing.T) {
		g := newRig(t)
		g.started(t)
		p := filepath.Join(g.runDir, "approval.json")
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(raw), `"plan_hash": "`, `"plan_hash": "0`, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		check(t, g, "approval.json does not match")
	})
}

// spyGit records the calls that could discard work.
type spyGit struct {
	gitland.Repo
	mu    sync.Mutex
	calls []string
}

func (s *spyGit) Restore(paths []string) error {
	s.mu.Lock()
	s.calls = append(s.calls, "Restore")
	s.mu.Unlock()
	return s.Repo.Restore(paths)
}

func TestResumeRefusesForeignDirt(t *testing.T) {
	t.Parallel()
	t.Run("an unrelated file stops the run untouched", func(t *testing.T) {
		g := newRig(t)
		g.started(t)
		notes := filepath.Join(g.repo, "notes.txt")
		write(t, notes, "mine\n")
		spy := &spyGit{Repo: g.git}
		g.wire(goodScript(g))
		o := g.options()
		o.Git = spy
		rep, err := run(context.Background(), o, runFlags{skipAcceptance: true, afterStart: useChecker(g.fastChecker())})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if rep.Status != "failed" || rep.StopReason != "foreign_dirt" || rep.ExitCode != 1 {
			t.Fatalf("report = %s (%s) exit %d", rep.Status, rep.StopReason, rep.ExitCode)
		}
		if !strings.Contains(strings.Join(rep.Failures, "\n"), "notes.txt") {
			t.Errorf("failures %q do not name notes.txt", rep.Failures)
		}
		if n := len(g.fake.Requests()); n != 0 {
			t.Errorf("provider calls = %d, want 0", n)
		}
		if raw, err := os.ReadFile(notes); err != nil || string(raw) != "mine\n" {
			t.Errorf("notes.txt = %q, %v; want it untouched", raw, err)
		}
		if len(spy.calls) != 0 {
			t.Errorf("git calls %v, want none that discard work", spy.calls)
		}
		if out := g.gitCmd("for-each-ref", "refs/stash"); strings.TrimSpace(out) != "" {
			t.Errorf("a stash exists: %s", out)
		}
	})
	t.Run("the files of a released leaf are not foreign", func(t *testing.T) {
		g := newRig(t, staleOne)
		g.started(t)
		g.plantDead(t, "fn-greet", true)
		g.plantReal(t, "fn-greet", good("fn-greet"))
		age()
		rc, err := startRun(context.Background(), g.options())
		if rc != nil {
			defer rc.close()
		}
		if err != nil {
			t.Fatalf("a dirty contract file of a released leaf stopped the run: %v", err)
		}
	})
}

func TestRunModeFreshVsResume(t *testing.T) {
	t.Parallel()
	t.Run("a state file with no row past pending resumes but is not resumed", func(t *testing.T) {
		g := newRig(t)
		if st, err := LoadState(g.runDir); err != nil || st.StartedAt != "" {
			t.Fatalf("before any run: state %+v, %v", st, err)
		}
		g.started(t)
		rep, err := g.doRun(t, goodScript(g), g.fastChecker(), nil)
		if err != nil || rep.Status != "verified" || rep.Resumed {
			t.Fatalf("run = %s resumed %v, %v; want verified and not resumed", rep.Status, rep.Resumed, err)
		}
		if len(g.sink.OfKind("resume")) != 1 {
			t.Errorf("resume events = %d, want 1 (the resume path ran)", len(g.sink.OfKind("resume")))
		}
	})
	t.Run("a fresh run is not resumed, a later one is, and it stays so", func(t *testing.T) {
		g := newRig(t)
		fc := g.fastChecker()
		rep := g.interruptedRun(t, goodScript(g), fc, "leaf_verified", "fn-farewell")
		if rep.Resumed {
			t.Fatal("the first invocation says resumed")
		}
		if st, _ := LoadState(g.runDir); st.Resumed {
			t.Fatal("state.resumed is set after a fresh run")
		}
		rep = g.interruptedRun(t, goodScript(g), g.fastChecker(), "leaf_verified", "fn-greet")
		if !rep.Resumed {
			t.Fatal("the second invocation (a verified row exists) does not say resumed")
		}
		if st, _ := LoadState(g.runDir); !st.Resumed {
			t.Fatal("state.resumed was not saved")
		}
		rep, err := g.doRun(t, goodScript(g), g.fastChecker(), nil)
		if err != nil || rep.Status != "verified" || !rep.Resumed {
			t.Fatalf("third run = %s resumed %v, %v; want verified and resumed", rep.Status, rep.Resumed, err)
		}
		if rd := readReportFile(t, g); !rd.Resumed {
			t.Error("report.json lost the resumed flag")
		}
	})
}

func TestResumeNoDuplicateCalls(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	script := goodScript(g)
	script["implement:fn-bye"] = []step{{Text: good("fn-bye"), Delay: 100 * time.Millisecond}}
	g.wire(script)
	g.fake.Before = func(stage string) {
		if stage == "implement:fn-bye" {
			cancel()
		}
	}
	rep, err := run(ctx, g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(g.fastChecker())})
	if err != nil || rep.Status != "interrupted" {
		t.Fatalf("first run = %s (%s), %v", rep.Status, rep.StopReason, err)
	}
	verified := g.verifiedIDs(t)
	if len(verified) != 2 {
		t.Fatalf("verified before the resume = %v, want fn-farewell and fn-greet", verified)
	}
	for _, id := range verified {
		if n := len(g.implementRows(t, id)); n != 1 {
			t.Fatalf("%s has %d implement rows before the resume, want 1", id, n)
		}
	}

	g.wire(Script{
		"implement:fn-bye": {reply(good("fn-bye"))}, "implement:fn-hello": {reply(good("fn-hello"))}, "implement:fn-serve": {reply(good("fn-serve"))},
	})
	fc := newFakeChecker(g)
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	for _, id := range []string{"fn-bye", "fn-hello", "fn-serve"} {
		fc.LeafScript[id] = []runner.Verdict{passVerdict()}
	}
	rep, err = run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil || rep.Status != "verified" || !rep.Resumed {
		t.Fatalf("second run = %s (%s) resumed %v, %v, failures %v", rep.Status, rep.StopReason, rep.Resumed, err, rep.Failures)
	}
	stages := stagesOf(g.fake)
	for _, s := range stages {
		for _, id := range verified {
			if s == "implement:"+id {
				t.Errorf("the resume called %s for a leaf verified before the kill", s)
			}
		}
	}
	if len(stages) != 3 {
		t.Errorf("the resume made %d calls %v, want 3 (the in-flight leaf once, then the rest)", len(stages), stages)
	}
	for _, id := range verified {
		if n := len(g.implementRows(t, id)); n != 1 {
			t.Errorf("%s has %d implement ledger rows across both runs, want exactly 1", id, n)
		}
	}
	bye := g.implementRows(t, "fn-bye")
	if len(bye) != 2 {
		t.Fatalf("fn-bye has %d implement rows, want 2 (the cut-off call and its repeat)", len(bye))
	}
	if bye[0].ErrorKind != "cancelled" {
		t.Errorf("the cut-off call's error_kind = %q, want cancelled", bye[0].ErrorKind)
	}
	for _, l := range g.plan.Leaves {
		if g.leafCommits(l.ID) != 1 {
			t.Errorf("%s has %d commits, want 1", l.ID, g.leafCommits(l.ID))
		}
	}
}

func TestResumeVerifiesFileOnDiskFirst(t *testing.T) {
	t.Parallel()
	t.Run("a file that passes costs no model call and one commit", func(t *testing.T) {
		g := newRig(t, staleOne)
		g.started(t)
		g.plantDead(t, "fn-greet", true)
		g.plantReal(t, "fn-greet", good("fn-greet"))
		age()
		script := goodScript(g)
		delete(script, "implement:fn-greet") // a call for it would fail the test
		rep, err := g.doRun(t, script, g.fastChecker(), nil)
		if err != nil || rep.Status != "verified" || !rep.Resumed {
			t.Fatalf("run = %s (%s) resumed %v, %v, failures %v", rep.Status, rep.StopReason, rep.Resumed, err, rep.Failures)
		}
		if n := len(g.leafCalls("fn-greet")); n != 0 {
			t.Errorf("fn-greet was called %d times, want 0", n)
		}
		if g.leafCommits("fn-greet") != 1 {
			t.Errorf("fn-greet commits = %d, want 1", g.leafCommits("fn-greet"))
		}
	})
	t.Run("a file that fails seeds one call with the runner's failure", func(t *testing.T) {
		g := newRig(t, staleOne)
		g.started(t)
		g.plantDead(t, "fn-greet", true)
		g.plantReal(t, "fn-greet", bad("fn-greet", 1))
		age()
		script := goodScript(g)
		fc := g.fastChecker()
		fc.LeafScript["fn-greet"] = []runner.Verdict{failVerdict("test_fail", "ONDISK-FAILURE-LINE\nTOKEN "+canarySecret+"\n", "TestOnDisk"), passVerdict()}
		rep, err := g.doRun(t, script, fc, nil)
		if err != nil || rep.Status != "verified" {
			t.Fatalf("run = %s (%s), %v, failures %v", rep.Status, rep.StopReason, err, rep.Failures)
		}
		calls := g.leafCalls("fn-greet")
		if len(calls) != 1 {
			t.Fatalf("fn-greet calls = %d, want 1", len(calls))
		}
		if p := userText(calls[0]); !strings.Contains(p, "TestOnDisk") || !strings.Contains(p, "ONDISK-FAILURE-LINE") {
			t.Error("the request does not hold the runner-derived failure of the file on disk")
		}
		if strings.Contains(userText(calls[0]), canarySecret) {
			t.Error("a line holding a secret reached the prompt")
		}
		if n := len(attemptsOf(t, g.board, "fn-greet")); n != 1 {
			t.Errorf("attempts = %d, want 1 (checking the file on disk is not an attempt)", n)
		}
		for _, c := range []string{"ONDISK-FAILURE-LINE", canarySecret} {
			if hasCanary(t, g, c) {
				t.Errorf("%s reached a store", c)
			}
		}
	})
}

func TestReplyHashPersistedAcrossResume(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	r := bad("fn-greet", 1)
	script := goodScript(g)
	script["implement:fn-greet"] = []step{reply(r), {Text: good("fn-greet"), Delay: 100 * time.Millisecond}}
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = []runner.Verdict{failVerdict("test_fail", "x", "TestA")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.wire(script)
	calls := 0
	g.fake.Before = func(stage string) {
		if stage == "implement:fn-greet" {
			if calls++; calls == 2 {
				cancel()
			}
		}
	}
	rep, err := run(ctx, g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil || rep.Status != "interrupted" {
		t.Fatalf("first run = %s (%s), %v", rep.Status, rep.StopReason, err)
	}
	first := attemptsOf(t, g.board, "fn-greet")
	if len(first) != 1 || first[0].ReplySHA256 == "" {
		t.Fatalf("attempts after the first run = %+v, want one with a reply hash", first)
	}

	// The second invocation is a new runCtx: the hash can only come from the blackboard.
	second := goodScript(g)
	second["implement:fn-greet"] = []step{reply(r), reply(good("fn-greet"))}
	g.wire(second)
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	fc2 := g.fastChecker()
	fc2.LeafScript["fn-greet"] = nil // a check of the identical reply would fail the test
	_, err = run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc2)})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := len(g.leafCalls("fn-greet")); n != 1 {
		t.Errorf("fn-greet calls on the resume = %d, want 1 (the identical reply abandons the entry)", n)
	}
	if n := fc2.checks("fn-greet"); n != 0 {
		t.Errorf("the identical reply was checked %d times, want 0", n)
	}
	all := attemptsOf(t, g.board, "fn-greet")
	if len(all) != 2 || !strings.HasPrefix(all[1].FailureReason, "identical_reply") {
		t.Fatalf("attempts = %+v, want the old one kept and an identical_reply after it (numbering continues)", all)
	}
}

func TestResumeAllVerifiedGoesToFinish(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rep := g.interruptedRun(t, goodScript(g), g.fastChecker(), "leaf_verified", "fn-serve")
	if n := len(g.verifiedIDs(t)); n != 5 {
		t.Fatalf("verified after the first run = %d (%v), want 5", n, rep.Failures)
	}
	g.wire(Script{}) // any model call fails the test
	fc := newFakeChecker(g)
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil || rep.Status != "verified" || !rep.Resumed || rep.Landing == nil {
		t.Fatalf("second run = %s (%s) resumed %v landing %v, %v", rep.Status, rep.StopReason, rep.Resumed, rep.Landing, err)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("the resume made %d model calls, want 0", n)
	}
	for _, l := range g.plan.Leaves {
		if g.leafCommits(l.ID) != 1 {
			t.Errorf("%s has %d commits, want 1", l.ID, g.leafCommits(l.ID))
		}
	}
}

func TestResumeRefusesMovedRepo(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.interruptedRun(t, goodScript(g), g.fastChecker(), "leaf_verified", "fn-farewell")
	st, err := LoadState(g.runDir)
	if err != nil || st.Branch == "" {
		t.Fatalf("state %+v, %v", st, err)
	}
	// Someone moved the work branch back past the leaf commit.
	g.gitCmd("update-ref", "refs/heads/"+st.Branch, st.Branch+"~1")
	g.wire(goodScript(g))
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(g.fastChecker())})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Status != "failed" || rep.StopReason != "repo_moved" {
		t.Fatalf("report = %s (%s), want failed repo_moved", rep.Status, rep.StopReason)
	}
	if msg := strings.Join(rep.Failures, "\n"); !strings.Contains(msg, strings.TrimPrefix(resumeMovedMessage, "executor: ")) {
		t.Errorf("failures %q do not hold the fixed message", rep.Failures)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("provider calls = %d, want 0", n)
	}
}

// ---- the process kill tests ----

// greeterReplies is a correct reply for every leaf of the greeter, by stage.
func greeterReplies() Script {
	s := Script{}
	for _, id := range []string{"fn-farewell", "fn-greet", "fn-bye", "fn-hello", "fn-serve"} {
		s["implement:"+id] = []step{reply(good(id))}
	}
	return s
}

// blockHere is how a child "reaches" its kill point: it names the point to the
// parent and waits to be killed.
func blockHere(dir string) {
	_ = os.WriteFile(filepath.Join(dir, "in-flight"), []byte("x"), 0o600)
	time.Sleep(10 * time.Minute)
}

// killChecker blocks the child at a repository-wide check or at the first
// acceptance command, before the command runs.
type killChecker struct {
	Checker
	point string
	dir   string
	mu    sync.Mutex
	vets  int
	done  bool
}

func (k *killChecker) BuildVet(ctx context.Context, repo string, env []string) runner.Verdict {
	k.mu.Lock()
	k.vets++
	hit := k.point == "wavecheck" && k.vets == 2 // the check of wave 1, after fn-bye and fn-hello are committed
	k.mu.Unlock()
	if hit {
		blockHere(k.dir)
	}
	return k.Checker.BuildVet(ctx, repo, env)
}

func (k *killChecker) Run(ctx context.Context, s runner.Spec) runner.Result {
	if k.point == "acceptance" {
		for _, e := range s.Env {
			if strings.HasPrefix(e, "GM_ACCEPTANCE_ADDR=") {
				k.mu.Lock()
				first := !k.done
				k.done = true
				k.mu.Unlock()
				if first {
					blockHere(k.dir)
				}
			}
		}
	}
	return k.Checker.Run(ctx, s)
}

// killGit blocks the child right after the landing, before the report.
type killGit struct {
	gitland.Repo
	dir string
}

func (k *killGit) Finish(msg string) (string, error) {
	h, err := k.Repo.Finish(msg)
	if err == nil {
		blockHere(k.dir)
	}
	return h, err
}

// TestResumeChildProcess is the child of TestResumeAfterKill. It does nothing
// unless the parent started it: it runs the executor over the parent's disk
// rig until the kill point and then waits to be killed.
func TestResumeChildProcess(t *testing.T) {
	if os.Getenv("GM_RESUME_CHILD") != "1" {
		return
	}
	dir, point := os.Getenv("GM_RESUME_DIR"), os.Getenv("GM_RESUME_POINT")
	g := newRigIn(t, dir, greeterReplies())
	stage := map[string]string{"after_first_leaf": "implement:fn-greet", "mid_leaf": "implement:fn-bye"}[point]
	g.fake.Before = func(s string) {
		if stage != "" && s == stage {
			blockHere(dir)
		}
	}
	o := g.options()
	if point == "landed" {
		o.Git = &killGit{Repo: g.git, dir: dir}
	}
	rep, err := Run2(context.Background(), o, func(rc *runCtx) {
		rc.chk = &killChecker{Checker: rc.chk, point: point, dir: dir}
	})
	t.Fatalf("the child ran to its end (%s) without reaching the kill point %s: %v", rep.Status, point, err)
}

// Run2 is run with the one test seam, over the real checks and acceptance.
func Run2(ctx context.Context, o Options, after func(*runCtx)) (Report, error) {
	return run(ctx, o, runFlags{afterStart: after})
}

// treeAndSubjects is the end state of a run: main's tree and the subjects of
// its commits, which are what two runs must agree on.
func treeAndSubjects(g *rig) (string, []string) {
	tree := strings.TrimSpace(g.gitCmd("rev-parse", "main^{tree}"))
	return tree, strings.Split(strings.TrimSpace(g.gitCmd("log", "--format=%s", "main")), "\n")
}

func TestResumeAfterKill(t *testing.T) {
	t.Parallel()
	cache := goCacheOverride
	if cache == "" {
		t.Skip("no shared go cache")
	}

	// The uninterrupted run every killed-and-resumed run must equal.
	refDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	type refEnd struct {
		tree     string
		subjects []string
		err      error
	}
	refc := make(chan refEnd, 1)
	ref := newRigIn(t, refDir, greeterReplies())
	go func() {
		rep, err := Run(context.Background(), ref.options())
		if err == nil && rep.Status != "verified" {
			err = fmt.Errorf("the reference run is %s (%s): %v", rep.Status, rep.StopReason, rep.Failures)
		}
		tree, subjects := "", []string(nil)
		if err == nil {
			tree, subjects = treeAndSubjects(ref)
		}
		refc <- refEnd{tree, subjects, err}
	}()

	points := []struct {
		name  string
		leaf  string // the leaf in flight at the kill, if any
		stale bool   // a claim is left behind
	}{
		{"after_first_leaf", "fn-greet", true},
		{"mid_leaf", "fn-bye", true},
		{"wavecheck", "", false},
		{"acceptance", "", false},
		{"landed", "", false},
	}
	var refOnce sync.Once
	var want refEnd
	for _, pt := range points {
		pt := pt
		t.Run(pt.name, func(t *testing.T) {
			t.Parallel()
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			newRigIn(t, dir, Script{}).close() // plans once, in the parent

			cmd := exec.Command(os.Args[0], "-test.run=^TestResumeChildProcess$")
			cmd.Env = append(os.Environ(), "GM_RESUME_CHILD=1", "GM_RESUME_DIR="+dir, "GM_RESUME_POINT="+pt.name, "GM_TEST_GOCACHE="+cache)
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
					if !fileExists(marker) {
						t.Fatalf("the child ended before the kill point %s:\n%s", pt.name, tail(out.String()))
					}
					break wait
				case <-deadline:
					_ = cmd.Process.Kill()
					t.Fatalf("the kill point %s was not reached in time:\n%s", pt.name, tail(out.String()))
				case <-time.After(50 * time.Millisecond):
					if fileExists(marker) {
						break wait
					}
				}
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("kill: %v", err)
			}
			<-exited
			time.Sleep(1500 * time.Millisecond) // the dead process's claim is stale after 1 s

			// What the killed run left.
			pre := newRigIn(t, dir, Script{})
			ctx := context.Background()
			preRows := map[string]blackboard.Row{}
			rows, err := pre.board.List(ctx, pre.id, blackboard.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if pre.plan.Leaf(r.NodeID) != nil {
					preRows[r.NodeID] = r
				}
			}
			var finished []string
			for id, r := range preRows {
				if r.Status == blackboard.StatusVerified {
					finished = append(finished, id)
				}
			}
			if pt.stale {
				if r := preRows[pt.leaf]; r.Status != blackboard.StatusInProgress && r.Status != blackboard.StatusClaimed {
					t.Fatalf("after the kill %s is %s, want a claim left behind", pt.leaf, r.Status)
				}
			}
			preCalls := map[string]int{}
			for _, id := range finished {
				preCalls[id] = len(pre.implementRows(t, id))
			}
			pre.close()

			// The resume, in this process, over the same directory, with the real checks and acceptance.
			g := newRigIn(t, dir, greeterReplies())
			rep, err := Run(ctx, g.options())
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if rep.Status != "verified" || !rep.Resumed || rep.ExitCode != 0 {
				t.Fatalf("resume = %s (%s) resumed %v exit %d, failures %v", rep.Status, rep.StopReason, rep.Resumed, rep.ExitCode, rep.Failures)
			}
			for _, l := range g.plan.Leaves {
				if r := g.row(t, l.ID); r.Status != blackboard.StatusVerified {
					t.Errorf("%s is %s after the resume", l.ID, r.Status)
				}
			}

			// No duplicate commit: one per leaf, a unique node trailer each, one final commit.
			subjects := strings.Split(strings.TrimSpace(g.gitCmd("log", "--format=%s", "main")), "\n")
			perLeaf := map[string]int{}
			final := 0
			for _, s := range subjects {
				switch {
				case strings.HasPrefix(s, "gm(run): "):
					final++
				case strings.HasPrefix(s, "gm(fn-"):
					perLeaf[s[3:strings.Index(s, ")")]]++
				}
			}
			for _, l := range g.plan.Leaves {
				if perLeaf[l.ID] != 1 {
					t.Errorf("%s has %d commits, want 1", l.ID, perLeaf[l.ID])
				}
			}
			if final != 1 {
				t.Errorf("final commits = %d, want 1 (main fast-forwarded once)", final)
			}
			trailers := strings.Fields(g.gitCmd("log", "--format=%(trailers:key=GopherMind-Node,valueonly,unfold)", "main"))
			seen := map[string]bool{}
			for _, tr := range trailers {
				if seen[tr] {
					t.Errorf("the node trailer %s is on two commits", tr)
				}
				seen[tr] = true
			}

			// No repeated model call for a leaf verified before the kill.
			for _, id := range finished {
				if n := len(g.implementRows(t, id)); n != preCalls[id] {
					t.Errorf("%s has %d implement rows after the resume, %d before: a verified leaf was called again", id, n, preCalls[id])
				}
			}
			for _, s := range stagesOf(g.fake) {
				for _, id := range finished {
					if s == "implement:"+id {
						t.Errorf("the resume called %s for a leaf verified before the kill", s)
					}
				}
			}
			if pt.leaf != "" {
				if n := len(g.implementRows(t, pt.leaf)); n > 2 {
					t.Errorf("%s has %d implement rows, want at most 2 (the killed call and its repeat)", pt.leaf, n)
				}
			}

			// Attempts survive and numbering continues.
			for id, before := range preRows {
				after := g.row(t, id).Attempts
				if len(after) < len(before.Attempts) {
					t.Fatalf("%s lost attempts: %d before, %d after", id, len(before.Attempts), len(after))
				}
				for i, a := range before.Attempts {
					if after[i].ReplySHA256 != a.ReplySHA256 || after[i].Verdict != a.Verdict || after[i].Order != a.Order {
						t.Errorf("%s attempt %d changed across the resume", id, i+1)
					}
				}
			}

			// Stale claims were released and said so.
			for _, r := range g.mustRows(t) {
				if r.Status == blackboard.StatusClaimed || r.Status == blackboard.StatusInProgress || r.Claim != nil {
					t.Errorf("%s is still held after the resume", r.NodeID)
				}
			}
			if pt.stale {
				evs := g.sink.OfKind("resume")
				if len(evs) != 1 || !strings.Contains(evs[0].Message, pt.leaf) {
					t.Errorf("resume events = %+v, want one naming %s", evs, pt.leaf)
				}
			}
			if hasCanary(t, g, canarySecret) {
				t.Error("the secret reached a store")
			}

			// The same end state as the run nobody interrupted.
			refOnce.Do(func() { want = <-refc })
			if want.err != nil {
				t.Fatalf("reference: %v", want.err)
			}
			tree, subs := treeAndSubjects(g)
			if tree != want.tree {
				t.Errorf("final tree %s, uninterrupted %s", tree, want.tree)
			}
			if strings.Join(subs, "\n") != strings.Join(want.subjects, "\n") {
				t.Errorf("commits differ from the uninterrupted run:\n%s\nvs\n%s", strings.Join(subs, "\n"), strings.Join(want.subjects, "\n"))
			}
		})
	}
}

// tail is the last 2000 bytes of a child's output, for a failure message.
func tail(s string) string {
	if len(s) > 2000 {
		return s[len(s)-2000:]
	}
	return s
}

func (g *rig) mustRows(t *testing.T) []blackboard.Row {
	t.Helper()
	rows, err := g.board.List(context.Background(), g.id, blackboard.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// A kill during a repair: the reopened leaf is back to ready with its
// committed file intact. The resume adopts that file (no model call, no second
// commit), the wave check fails again, and exactly one repair is made.
func TestResumeMidRepair(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(2))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.wire(Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet")), {Text: variant(good("fn-greet"), 1), Delay: 100 * time.Millisecond}},
	})
	greetCalls := 0
	g.fake.Before = func(stage string) {
		if stage == "implement:fn-greet" {
			if greetCalls++; greetCalls == 2 { // the repair's call
				cancel()
			}
		}
	}
	fc := newFakeChecker(g)
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), passVerdict()}
	greetFails(fc, g, 1)
	rep, err := run(ctx, g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil || rep.Status != "interrupted" {
		t.Fatalf("first run = %s (%s), %v", rep.Status, rep.StopReason, err)
	}
	if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusReady || r.Revision != 1 {
		t.Fatalf("fn-greet after the cut-off repair = %s revision %d, want ready revision 1", r.Status, r.Revision)
	}

	g.wire(Script{
		"implement:fn-bye": {reply(good("fn-bye"))}, "implement:fn-hello": {reply(good("fn-hello"))}, "implement:fn-serve": {reply(good("fn-serve"))},
		"implement:fn-greet": {reply(variant(good("fn-greet"), 2))},
	})
	fc2 := newFakeChecker(g)
	fc2.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	for _, id := range []string{"fn-bye", "fn-hello", "fn-serve"} {
		fc2.LeafScript[id] = []runner.Verdict{passVerdict()}
	}
	fc2.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), passVerdict()} // the adopted file, then the repair
	greetFails(fc2, g, 1)
	rep, err = run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc2)})
	if err != nil || rep.Status != "verified" || !rep.Resumed {
		t.Fatalf("second run = %s (%s) resumed %v, %v, failures %v", rep.Status, rep.StopReason, rep.Resumed, err, rep.Failures)
	}
	if n := len(g.leafCalls("fn-greet")); n != 1 {
		t.Errorf("the resume called fn-greet %d times, want 1 (the repair only; the committed file was adopted)", n)
	}
	if n := len(g.leafCalls("fn-farewell")); n != 0 {
		t.Errorf("the resume called fn-farewell %d times", n)
	}
	repairs := strings.TrimSpace(g.gitCmd("log", "--format=%s", "--grep=^gm(fn-greet): repair round"))
	if repairs != "gm(fn-greet): repair round 1" {
		t.Errorf("repair commits = %q, want exactly one", repairs)
	}
	if n := g.leafCommits("fn-greet"); n != 2 { // the leaf's commit and one repair
		t.Errorf("fn-greet has %d commits, want 2 (leaf, repair)", n)
	}
}

// A leaf found with both its stub and a real file is normalized (the stub goes)
// and the file on disk is checked before any model call; a failing file is
// replaced through the ladder and the leaf ends committed once with no stub.
func TestResumeNormalizesHalfSwap(t *testing.T) {
	t.Parallel()
	g := newRig(t, staleOne)
	g.started(t)
	g.plantDead(t, "fn-greet", true)
	l := g.plan.Leaf("fn-greet")
	write(t, g.realPath(l), bad("fn-greet", 1)) // the real file exists next to the stub
	age()
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = []runner.Verdict{failVerdict("test_fail", "x", "TestA"), passVerdict()}
	rep, err := g.doRun(t, goodScript(g), fc, nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, failures %v", rep.Status, rep.StopReason, err, rep.Failures)
	}
	if n := len(g.leafCalls("fn-greet")); n != 1 {
		t.Errorf("fn-greet calls = %d, want 1: the file on disk failed its check", n)
	}
	if g.leafCommits("fn-greet") != 1 || fileExists(g.stubPath(l)) {
		t.Errorf("commits %d, stub present %v: want one commit and no stub", g.leafCommits("fn-greet"), fileExists(g.stubPath(l)))
	}
}

// No prompt text, reply text, runner output or secret reaches any store across
// an interruption and a resume.
func TestResumeLeavesNoCanaryInAnyStore(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.wire(Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet": {
			reply(variant(bad("fn-greet", 1), 1) + "\n// CANARY-REPLY-TEXT\n"),
			{Text: good("fn-greet"), Delay: 100 * time.Millisecond},
		},
	})
	calls := 0
	g.fake.Before = func(stage string) {
		if stage == "implement:fn-greet" {
			if calls++; calls == 2 {
				cancel()
			}
		}
	}
	fc := newFakeChecker(g)
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{failVerdict("test_fail", "CANARY-TEST-OUTPUT "+canarySecret, "TestA_"+canarySecret)}
	if rep, err := run(ctx, g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)}); err != nil || rep.Status != "interrupted" {
		t.Fatalf("first run = %s, %v", rep.Status, err)
	}
	second := goodScript(g)
	second["implement:fn-greet"] = []step{reply(variant(good("fn-greet"), 3))}
	fc2 := g.fastChecker()
	fc2.LeafScript["fn-farewell"] = nil
	rep, err := g.doRun(t, second, fc2, nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("second run = %s (%s), %v", rep.Status, rep.StopReason, err)
	}
	for _, c := range []string{canarySecret, "CANARY-REPLY-TEXT", "CANARY-TEST-OUTPUT"} {
		if hasCanary(t, g, c) {
			t.Errorf("%s reached a store", c)
		}
	}
	for _, name := range []string{"executor.json", "leaf-results.json", "escalations.json"} {
		if raw, err := os.ReadFile(filepath.Join(g.runDir, "_state", name)); err == nil && strings.Contains(string(raw), "CANARY") {
			t.Errorf("%s holds a canary", name)
		}
	}
}
