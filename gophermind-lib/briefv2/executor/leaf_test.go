package executor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

const leafID = "fn-greet"

func reply(text string) step { return step{Text: text} }

// runOne runs the leaf once and fails the test on a harness error.
func runOne(t *testing.T, rc *runCtx, id string) leafOutcome {
	t.Helper()
	out, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
	if err != nil {
		t.Fatalf("runLeaf(%s): %v", id, err)
	}
	return out
}

func TestLeafFailTwiceThenPass(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(bad(leafID, 1)), reply(bad(leafID, 2)), reply(good(leafID))}}, false)
	l := g.plan.Leaf(leafID)

	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified || out.Interrupted {
		t.Fatalf("outcome = %+v, want verified", out)
	}
	if got := verdictsOf(t, g.board, leafID); got != "fail,fail,pass" {
		t.Fatalf("verdicts = %s, want fail,fail,pass", got)
	}
	row := g.row(t, leafID)
	if row.Status != blackboard.StatusVerified || row.Result == nil || row.Result.Commit == "" {
		t.Fatalf("row = %+v, want verified with a commit", row)
	}
	if n := g.leafCommits(leafID); n != 1 {
		t.Fatalf("leaf commits = %d, want 1", n)
	}
	if fileExists(g.stubPath(l)) {
		t.Fatal("the stub is still on disk after the leaf verified")
	}
	if !fileExists(g.realPath(l)) {
		t.Fatal("the real file is missing after the leaf verified")
	}
	reqs := g.leafCalls(leafID)
	if len(reqs) != 3 {
		t.Fatalf("calls = %d, want 3", len(reqs))
	}
	for i := 1; i < 3; i++ {
		p := userText(reqs[i])
		if !strings.Contains(p, "Failing tests:") || !strings.Contains(p, l.TestFunc) {
			t.Errorf("prompt %d does not carry the failed test names", i+1)
		}
	}
	if strings.Contains(userText(reqs[0]), "Failing tests:") {
		t.Error("the first prompt carries a failure")
	}
	if reqs[0].Temperature != 0 || reqs[1].Temperature != 0 || reqs[2].Temperature != 0.3 {
		t.Errorf("temperatures = %v %v %v, want 0 0 0.3", reqs[0].Temperature, reqs[1].Temperature, reqs[2].Temperature)
	}
	for _, r := range g.leafCalls(leafID) {
		if !strings.HasPrefix(r.Messages[0].Content, "GopherMind executor. Stage: implement:"+leafID) {
			t.Errorf("system message = %q", r.Messages[0].Content)
		}
	}
	for _, c := range implementCalls(t, g.led, leafID) {
		if c.Scope != "node" || c.TaskType != "implement" || c.NodeID != leafID {
			t.Errorf("ledger row scope %q task %q node %q, want node, implement, %s", c.Scope, c.TaskType, c.NodeID, leafID)
		}
	}
	for _, a := range attemptsOf(t, g.board, leafID) {
		if a.ReplySHA256 == "" || a.Provider != "a" || a.Model != "m1" || a.Order != 1 {
			t.Errorf("attempt = %+v", a)
		}
	}
	if dirty, err := g.git.Dirty(); err != nil || len(dirty) != 0 {
		t.Fatalf("dirty after the leaf = %v, %v", dirty, err)
	}
}

func TestNoTestsRanIsFailure(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.FixAttempts = 1 } })
	var steps []step
	for i := 1; i <= 4; i++ {
		steps = append(steps, reply(variant(bad(leafID, 1), i)))
	}
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: steps}, true)
	for i := 0; i < 4; i++ {
		fc.LeafScript[leafID] = append(fc.LeafScript[leafID], runner.Verdict{Events: 0}) // a pass that ran no test
	}
	out := runOne(t, rc, leafID)
	if out.Status == blackboard.StatusVerified {
		t.Fatal("a check with zero test events verified the leaf")
	}
	for i, r := range reasonsOf(t, g.board, leafID) {
		if r != "no_tests_ran" {
			t.Errorf("attempt %d reason = %q, want no_tests_ran", i, r)
		}
	}
	if g.leafCommits(leafID) != 0 {
		t.Fatal("a leaf with no test events was committed")
	}
	if !fileExists(g.stubPath(g.plan.Leaf(leafID))) {
		t.Fatal("the stub was not restored")
	}
}

func TestMalformedIsCheapRepair(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply("Sure, here you go! I would do it like this."), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified", out)
	}
	calls := implementCalls(t, g.led, leafID)
	if len(calls) != 2 || calls[0].Outcome != "malformed" {
		t.Fatalf("ledger implement rows = %+v, want two with the first malformed", calls)
	}
	if got := verdictsOf(t, g.board, leafID); got != "pass" {
		t.Fatalf("verdicts = %s, want only the pass: a repair charges no blackboard attempt", got)
	}
}

func TestForbiddenWriteRejected(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	fileHeader := "// FILE: internal/greet/other.go\npackage greet\n\nfunc NEVERWRITTEN() {}\n"
	diffHeader := "diff --git a/internal/greet/greet.go b/internal/greet/greet.go\n--- a/internal/greet/greet.go\n+++ b/internal/greet/greet.go\n@@ -1 +1 @@\n-NEVERWRITTEN\n"
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(fileHeader), reply(diffHeader), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified", out)
	}
	if got := verdictsOf(t, g.board, leafID); got != "fail,fail,pass" {
		t.Fatalf("verdicts = %s", got)
	}
	if r := reasonsOf(t, g.board, leafID); r[0] != "forbidden_write" || r[1] != "forbidden_write" {
		t.Fatalf("reasons = %v, want forbidden_write twice", r)
	}
	if fc.checks(leafID) != 1 {
		t.Fatalf("checks = %d, want 1: a forbidden reply is never written or checked", fc.checks(leafID))
	}
	raw, err := os.ReadFile(g.realPath(g.plan.Leaf(leafID)))
	if err != nil || strings.Contains(string(raw), "NEVERWRITTEN") {
		t.Fatalf("the real file holds refused text (err %v)", err)
	}
	reqs := g.leafCalls(leafID)
	if !strings.Contains(userText(reqs[1]), "refused before anything was written") || strings.Contains(userText(reqs[1]), "NEVERWRITTEN") {
		t.Error("the next prompt must carry the fixed sentence and none of the refused text")
	}
	if dirty, _ := g.git.Dirty(); len(dirty) != 0 {
		t.Fatalf("git status is not clean: %v", dirty)
	}
}

func TestImportNotAllowedFailsAttempt(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	execImport := strings.Replace(good(leafID), `import "strings"`, "import (\n\t\"os/exec\"\n\t\"strings\"\n)", 1)
	execImport = strings.Replace(execImport, "name = strings.TrimSpace(name)", "_ = exec.Command\n\tname = strings.TrimSpace(name)", 1)
	if !strings.Contains(execImport, "os/exec") {
		t.Fatal("test setup: the reply does not import os/exec")
	}
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(execImport), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if r := reasonsOf(t, g.board, leafID); r[0] != "import_not_allowed" {
		t.Fatalf("reasons = %v, want import_not_allowed first", r)
	}
	if fc.checks(leafID) != 1 {
		t.Fatalf("checks = %d, want 1: a reply with a forbidden import is never written", fc.checks(leafID))
	}
	p := userText(g.leafCalls(leafID)[1])
	if !strings.Contains(p, "Allowed imports:") || !strings.Contains(p, "example.com/greeter") {
		t.Error("the next prompt does not list the allowed modules")
	}
	if strings.Contains(p, "os/exec") {
		t.Error("the next prompt repeats the offending import")
	}
}

func TestStrayWriteFailsAttempt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, file string }{
		{"untracked", "notes.txt"},
		{"ignored", "artifact.out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newRig(t)
			write(t, g.repo+"/.gitignore", ".gophermind/\n*.out\n")
			g.gitCmd("add", ".gitignore")
			g.gitCmd("commit", "-q", "-m", "ignore outputs")
			rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID)), reply(variant(good(leafID), 2))}}, true)
			fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), passVerdict()} // the checker passes both times
			var calls atomic.Int32
			fc.Hook = func(repo string) {
				if calls.Add(1) == 1 {
					write(t, repo+"/"+tc.file, "stray")
				}
			}
			out := runOne(t, rc, leafID)
			if out.Status != blackboard.StatusVerified {
				t.Fatalf("outcome = %+v, want verified on the second attempt", out)
			}
			if got := verdictsOf(t, g.board, leafID); got != "fail,pass" {
				t.Fatalf("verdicts = %s, want fail,pass: the stray write fails the attempt although the checker passed", got)
			}
			if r := reasonsOf(t, g.board, leafID); r[0] != "forbidden_write" {
				t.Fatalf("reasons = %v", r)
			}
			if fileExists(g.repo + "/" + tc.file) {
				t.Fatal("the stray file was not removed")
			}
			if g.leafCommits(leafID) != 1 {
				t.Fatalf("leaf commits = %d, want 1", g.leafCommits(leafID))
			}
		})
	}
}

func TestExecutorSignatureMismatchIsRepair(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	wrong := strings.Replace(good(leafID), "func Greet(name string) (string, error)", "func Greet(name string) string", 1)
	wrong = strings.Replace(wrong, `return "", &NameError{Reason: "name is empty"}`, `return ""`, 1)
	wrong = strings.Replace(wrong, `return "Hello, " + name + "!", nil`, `return "Hello, " + name + "!"`, 1)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(wrong), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	calls := implementCalls(t, g.led, leafID)
	if len(calls) != 2 || calls[0].Outcome != "malformed" {
		t.Fatalf("ledger rows = %+v, want a malformed row then a good one", calls)
	}
	if fc.checks(leafID) != 1 {
		t.Fatalf("checks = %d, want 1: the changed signature was never written", fc.checks(leafID))
	}
	raw, _ := os.ReadFile(g.realPath(g.plan.Leaf(leafID)))
	if !strings.Contains(string(raw), "(string, error)") {
		t.Fatal("the committed file does not have the contract signature")
	}
}

func TestForgedPassReplyNeverCommitted(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	forged := "package greet\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc init() {\n\tfmt.Println(\"--- PASS: TestGreet (0.00s)\")\n\tfmt.Println(\"PASS\")\n\tos.Exit(0)\n}\n\nfunc Greet(name string) (string, error) { return \"\", nil }\n"
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(forged), reply(forged), reply(forged), reply(forged)}}, true)
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusFailed || out.Reason == "" {
		t.Fatalf("outcome = %+v, want failed with a reason: a forged-pass reply never verifies", out)
	}
	if len(attemptsOf(t, g.board, leafID)) == 0 {
		t.Fatal("no attempt was recorded for the forged replies")
	}
	if fc.checks(leafID) != 0 {
		t.Fatalf("checks = %d, want 0: the reply gate refuses the forgery before anything is written", fc.checks(leafID))
	}
	if g.leafCommits(leafID) != 0 {
		t.Fatal("a forged-pass reply reached a commit")
	}
	if !fileExists(g.stubPath(g.plan.Leaf(leafID))) || fileExists(g.realPath(g.plan.Leaf(leafID))) {
		t.Fatal("the stub was not kept and the real file absent")
	}
	for _, a := range attemptsOf(t, g.board, leafID) {
		if a.Verdict != blackboard.VerdictError || !strings.HasPrefix(a.FailureReason, "malformed") {
			t.Errorf("attempt = %+v, want an error attempt of class malformed", a)
		}
	}
}

func TestProviderErrorDoesNotConsumeFix(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {{Err: context.DeadlineExceeded}, reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if got := verdictsOf(t, g.board, leafID); got != "error,pass" {
		t.Fatalf("verdicts = %s, want error,pass: a provider error consumes no fix attempt", got)
	}
	if r := reasonsOf(t, g.board, leafID); r[0] != "timeout" {
		t.Fatalf("reasons = %v, want timeout first", r)
	}
	if g.leafCommits(leafID) != 1 {
		t.Fatalf("leaf commits = %d, want 1", g.leafCommits(leafID))
	}
}

func TestChainExhaustedCooldownInterrupts(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {{Err: provider.ErrRateLimited{}}, {Err: provider.ErrRateLimited{}}}}, true)
	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatalf("runLeaf: %v", err)
	}
	if !out.Interrupted {
		t.Fatalf("outcome = %+v, want Interrupted", out)
	}
	row := g.row(t, leafID)
	if row.Status != blackboard.StatusReady || row.Claim != nil {
		t.Fatalf("row = status %s claim %v, want ready and unclaimed", row.Status, row.Claim)
	}
	for _, a := range row.Attempts {
		if a.Verdict == blackboard.VerdictFail || a.FailureReason != "rate_limited" {
			t.Errorf("attempt = %+v, want only uncharged rate_limited errors", a)
		}
	}
	if !fileExists(g.stubPath(g.plan.Leaf(leafID))) || fileExists(g.realPath(g.plan.Leaf(leafID))) {
		t.Fatal("the stub is not in place")
	}
}

func TestHeartbeatWhileHeld(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	rc.heartbeat = 20 * time.Millisecond
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	var before, after blackboard.Row
	fc.Hook = func(string) {
		before = g.row(t, leafID)
		time.Sleep(300 * time.Millisecond)
		after = g.row(t, leafID)
	}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if before.Status != blackboard.StatusInProgress || after.Status != blackboard.StatusInProgress {
		t.Fatalf("status during the check = %s / %s, want in_progress", before.Status, after.Status)
	}
	if !after.HeartbeatAt.After(before.HeartbeatAt) {
		t.Fatalf("heartbeat did not advance while the leaf was held: %v then %v", before.HeartbeatAt, after.HeartbeatAt)
	}
	if row := g.row(t, leafID); row.Claim != nil {
		t.Fatalf("claim %+v is not cleared after the leaf returned", row.Claim)
	}
}

func TestStubRestoredOnCancel(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc.LeafScript[leafID] = []runner.Verdict{{Class: runner.ClassCancelled, Err: errors.New("runner: the run was cancelled")}}
	fc.Hook = func(string) { cancel() }
	out, err := rc.runLeaf(ctx, g.plan.Leaf(leafID), leafIn{})
	if err != nil || !out.Interrupted {
		t.Fatalf("runLeaf = %+v, %v; want Interrupted and no error", out, err)
	}
	l := g.plan.Leaf(leafID)
	if !fileExists(g.stubPath(l)) || fileExists(g.realPath(l)) {
		t.Fatal("the stub was not restored on cancel")
	}
	row := g.row(t, leafID)
	if row.Status != blackboard.StatusReady || row.Claim != nil || len(row.Attempts) != 0 {
		t.Fatalf("row = %+v, want ready, unclaimed, no attempt charged", row)
	}
}

func TestHarnessFaultConsumesNoFix(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{{Class: runner.ClassHarness, Err: errors.New("runner: go not found")}}
	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err == nil {
		t.Fatalf("a harness fault returned no error (outcome %+v)", out)
	}
	var se *stopError
	if errors.As(err, &se) {
		t.Fatalf("a harness fault is an error, not a stop: %v", err)
	}
	row := g.row(t, leafID)
	if row.Status != blackboard.StatusReady || row.Claim != nil || len(row.Attempts) != 0 {
		t.Fatalf("row = %+v, want ready, unclaimed, no attempt charged", row)
	}
	if !fileExists(g.stubPath(g.plan.Leaf(leafID))) {
		t.Fatal("the stub was not restored after a harness fault")
	}
}

func TestLeafAdoptsFileOnDisk(t *testing.T) {
	t.Parallel()
	put := func(g *rig, l *Leaf, src string) {
		t.Helper()
		if err := os.Remove(g.stubPath(l)); err != nil {
			t.Fatal(err)
		}
		write(t, g.realPath(l), src)
	}
	t.Run("uncommitted", func(t *testing.T) {
		g := newRig(t)
		rc, fc := g.leafRC(t, Script{}, true)
		l := g.plan.Leaf(leafID)
		put(g, l, good(leafID))
		fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
		out := runOne(t, rc, leafID)
		if out.Status != blackboard.StatusVerified {
			t.Fatalf("outcome = %+v", out)
		}
		if n := len(g.fake.Requests()); n != 0 {
			t.Fatalf("provider calls = %d, want 0", n)
		}
		if g.leafCommits(leafID) != 1 || fileExists(g.stubPath(l)) {
			t.Fatalf("commits = %d stub present %v, want one commit and no stub", g.leafCommits(leafID), fileExists(g.stubPath(l)))
		}
	})
	t.Run("committed", func(t *testing.T) {
		g := newRig(t)
		rc, fc := g.leafRC(t, Script{}, true)
		l := g.plan.Leaf(leafID)
		put(g, l, good(leafID))
		if _, err := g.git.CommitLeaf(leafID, l.Title, []string{l.File}, []string{l.StubFile}); err != nil {
			t.Fatal(err)
		}
		hash, ok, err := g.git.LeafCommit(leafID)
		if err != nil || !ok {
			t.Fatalf("LeafCommit = %q, %v, %v", hash, ok, err)
		}
		fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
		out := runOne(t, rc, leafID)
		if out.Status != blackboard.StatusVerified {
			t.Fatalf("outcome = %+v", out)
		}
		if n := len(g.fake.Requests()); n != 0 {
			t.Fatalf("provider calls = %d, want 0", n)
		}
		if g.leafCommits(leafID) != 1 {
			t.Fatalf("leaf commits = %d, want still 1 (no second commit)", g.leafCommits(leafID))
		}
		if row := g.row(t, leafID); row.Result == nil || row.Result.Commit != hash {
			t.Fatalf("result = %+v, want the hash %s from LeafCommit", row.Result, hash)
		}
	})
	t.Run("failing file seeds the ladder", func(t *testing.T) {
		g := newRig(t)
		rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
		l := g.plan.Leaf(leafID)
		put(g, l, bad(leafID, 1))
		fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "ONDISK-FAILURE-LINE\n", "TestOnDisk"), passVerdict()}
		out := runOne(t, rc, leafID)
		if out.Status != blackboard.StatusVerified {
			t.Fatalf("outcome = %+v", out)
		}
		first := userText(g.leafCalls(leafID)[0])
		if !strings.Contains(first, "ONDISK-FAILURE-LINE") || !strings.Contains(first, "TestOnDisk") {
			t.Fatal("the first prompt does not carry the failure of the file on disk")
		}
		if got := verdictsOf(t, g.board, leafID); got != "pass" {
			t.Fatalf("verdicts = %s: checking the file on disk is not a model attempt", got)
		}
	})
	t.Run("committed file that fails is restored from git", func(t *testing.T) {
		g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.FixAttempts = 1 } })
		rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(variant(bad(leafID, 2), 1)), reply(variant(bad(leafID, 2), 2)), reply(variant(bad(leafID, 2), 3)), reply(variant(bad(leafID, 2), 4))}}, true)
		l := g.plan.Leaf(leafID)
		put(g, l, bad(leafID, 1))
		if _, err := g.git.CommitLeaf(leafID, l.Title, []string{l.File}, []string{l.StubFile}); err != nil {
			t.Fatal(err)
		}
		committed, _ := os.ReadFile(g.realPath(l))
		for i := 0; i < 5; i++ {
			fc.LeafScript[leafID] = append(fc.LeafScript[leafID], failVerdict("test_fail", "x", "TestX"))
		}
		out := runOne(t, rc, leafID)
		if out.Status == blackboard.StatusVerified {
			t.Fatal("a failing committed file verified")
		}
		after, _ := os.ReadFile(g.realPath(l))
		if string(after) != string(committed) {
			t.Fatal("a failed attempt did not restore the committed file")
		}
		if dirty, _ := g.git.Dirty(); len(dirty) != 0 {
			t.Fatalf("dirty = %v, want a clean tree after the failed attempts", dirty)
		}
	})
}

func TestNoReplyOrOutputInPersistedFailure(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	runCanary := "package greet\n\nimport \"fmt\"\n\n// CANARY-reply\nfunc Greet(name string) (string, error) {\n\tfmt.Println(\"CANARY-output\")\n\treturn \"Hello, \" + name + \"!\", nil\n}\n"
	buildCanary := "package greet\n\nfunc Greet(name string) (string, error) {\n\tvar n int = \"CANARY-compile\"\n\treturn \"\", nil\n}\n"
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(runCanary), reply(buildCanary), reply(good(leafID))}}, false)
	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("runLeaf = %+v, %v; want verified", out, err)
	}
	if got := verdictsOf(t, g.board, leafID); got != "fail,fail,pass" {
		t.Fatalf("verdicts = %s", got)
	}
	reqs := g.leafCalls(leafID)
	if !strings.Contains(userText(reqs[1]), "CANARY-output") {
		t.Error("the runner output did not reach the next prompt, so the canary test proves nothing")
	}
	if !strings.Contains(userText(reqs[2]), "CANARY-compile") {
		t.Error("the compiler output did not reach the next prompt, so the canary test proves nothing")
	}
	for _, canary := range []string{"CANARY-reply", "CANARY-output", "CANARY-compile"} {
		if hasCanary(t, g, canary) {
			t.Errorf("%s reached a persisted store", canary)
		}
	}
}

func TestNoSecretInPrompt(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(bad(leafID, 1)), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{
		failVerdict("test_fail", "token is "+canarySecret+"\nan ordinary line\n", "TestX"),
		passVerdict(),
	}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	for i, r := range g.fake.Requests() {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, canarySecret) {
				t.Errorf("request %d carries the secret value", i)
			}
		}
	}
	if !strings.Contains(userText(g.leafCalls(leafID)[1]), "an ordinary line") {
		t.Error("the ordinary output line was not fed back")
	}
}

// withNetwork gives the brief a critical and an optional host.
func withNetwork(o *rigOpts) {
	o.BriefEdit = func(s string) string {
		return strings.Replace(s, "secrets:\n", "network:\n  - host: critical.invalid\n    purpose: \"a host the leaves must reach\"\n    critical: true\n  - host: ok.invalid\n    purpose: \"an optional host\"\n    critical: false\nsecrets:\n", 1)
	}
}

// viaProxy sends one request to host through the harness proxy as the node.
func viaProxy(t *testing.T, rc *runCtx, id, host string) {
	t.Helper()
	u, err := url.Parse(rc.prox.URL(id))
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), DisableKeepAlives: true}, Timeout: 20 * time.Second}
	resp, err := c.Get("http://" + host + "/x")
	if err == nil {
		resp.Body.Close()
	}
}

func TestCriticalHostFailure(t *testing.T) {
	t.Parallel()
	g := newRig(t, withNetwork)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID)), reply(variant(good(leafID), 2))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), passVerdict()}
	var calls atomic.Int32
	fc.Hook = func(string) {
		if calls.Add(1) == 1 {
			viaProxy(t, rc, leafID, "critical.invalid")
		}
	}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified on the second attempt", out)
	}
	if got := verdictsOf(t, g.board, leafID); got != "fail,pass" {
		t.Fatalf("verdicts = %s, want fail,pass: a critical failure fails a passing check", got)
	}
	if r := reasonsOf(t, g.board, leafID); r[0] != "network_critical: critical.invalid" {
		t.Fatalf("reasons = %v, want network_critical: critical.invalid", r)
	}
	st, err := LoadState(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.CriticalStreak[leafID] != 0 {
		t.Fatalf("critical streak = %d after a clean attempt, want it reset", st.CriticalStreak[leafID])
	}
	if !strings.Contains(userText(g.leafCalls(leafID)[1]), "critical network host") {
		t.Error("the next prompt does not say a critical request failed")
	}
}

func TestThreeCriticalFailuresTerminal(t *testing.T) {
	t.Parallel()
	g := newRig(t, withNetwork)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID)), reply(variant(good(leafID), 2)), reply(variant(good(leafID), 3))}}, true)
	for i := 0; i < 3; i++ {
		fc.LeafScript[leafID] = append(fc.LeafScript[leafID], passVerdict())
	}
	fc.Hook = func(string) { viaProxy(t, rc, leafID, "critical.invalid") }
	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(leafID), leafIn{})
	var se *stopError
	if !errors.As(err, &se) || se.Status != "failed" || se.Reason != "network_critical" {
		t.Fatalf("err = %v, want a stopError{failed, network_critical}", err)
	}
	if out.Status != blackboard.StatusFailed || out.Reason != "network_critical" {
		t.Fatalf("outcome = %+v", out)
	}
	row := g.row(t, leafID)
	if row.Status != blackboard.StatusFailed || len(row.Attempts) != 3 {
		t.Fatalf("row = status %s, %d attempts; want failed after 3", row.Status, len(row.Attempts))
	}
	st, _ := LoadState(g.runDir)
	if st.CriticalStreak[leafID] != 3 {
		t.Fatalf("persisted streak = %d, want 3", st.CriticalStreak[leafID])
	}
	if g.leafCommits(leafID) != 0 || !fileExists(g.stubPath(g.plan.Leaf(leafID))) {
		t.Fatal("a leaf that failed on the network was committed or lost its stub")
	}
}

func TestNonCriticalWarns(t *testing.T) {
	t.Parallel()
	g := newRig(t, withNetwork)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	fc.Hook = func(string) { viaProxy(t, rc, leafID, "ok.invalid") }
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified: a non-critical failure changes nothing", out)
	}
	var warned int
	for _, e := range g.sink.OfKind("warning") {
		if e.NodeID == leafID && strings.Contains(e.Message, "ok.invalid") {
			warned++
		}
	}
	if warned != 1 {
		t.Fatalf("warning events = %d, want 1", warned)
	}
	if st, _ := LoadState(g.runDir); st.CriticalStreak[leafID] != 0 {
		t.Fatal("a non-critical failure counted toward the critical streak")
	}
}

func TestEveryEntryTooLongIsNotSilent(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) {
		o.Settings = func(c *settings.Config) {
			c.Providers[0].Models[0].ContextTokens = 600
			c.Providers[1].Models[0].ContextTokens = 600
		}
	})
	rc, _ := g.leafRC(t, Script{}, true)
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusFailed || out.Reason == "" {
		t.Fatalf("outcome = %+v, want failed with a reason", out)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("provider calls = %d, want none for a prompt that cannot fit", n)
	}
	r := reasonsOf(t, g.board, leafID)
	if len(r) != 2 || !strings.HasPrefix(r[0], "context_too_long") {
		t.Fatalf("reasons = %v, want one context_too_long per entry", r)
	}
	lr, err := rc.newLeafRun(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := lr.oneRevision(context.Background()); err != nil || res != revTooLong {
		t.Fatalf("oneRevision = %v, %v; want revTooLong", res, err)
	}
}

func TestLeafNeverLeftWithoutTerminalStatus(t *testing.T) {
	t.Parallel()
	// The Task 11a afterRevision stub ends an exhausted ladder as failed with a
	// named reason: the row is terminal, the reason is recorded, and an event says so.
	g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.FixAttempts = 1 } })
	var steps []step
	for i := 1; i <= 4; i++ {
		steps = append(steps, reply(variant(bad(leafID, 1), i)))
	}
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: steps}, true)
	for i := 0; i < 4; i++ {
		fc.LeafScript[leafID] = append(fc.LeafScript[leafID], failVerdict("test_fail", "x", "TestX"))
	}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusFailed || out.Reason != "ladder_exhausted" {
		t.Fatalf("outcome = %+v, want failed ladder_exhausted", out)
	}
	if row := g.row(t, leafID); row.Status != blackboard.StatusFailed || row.Claim != nil {
		t.Fatalf("row = %+v, want failed and unclaimed", row)
	}
	if rc.rep.reasons[leafID] != "ladder_exhausted" {
		t.Fatalf("recorded reason = %q", rc.rep.reasons[leafID])
	}
	if len(g.sink.OfKind("leaf_failed")) != 1 {
		t.Fatalf("leaf_failed events = %d, want 1", len(g.sink.OfKind("leaf_failed")))
	}
	if !fileExists(g.stubPath(g.plan.Leaf(leafID))) {
		t.Fatal("the stub was not restored")
	}
}

func TestContractProblemEndsRevision(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply("CONTRACT_PROBLEM: the signature cannot return an error for this case")}}, true)
	lr, err := rc.newLeafRun(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lr.oneRevision(context.Background())
	if err != nil || res != revContractProblem {
		t.Fatalf("oneRevision = %v, %v; want revContractProblem", res, err)
	}
	if lr.cp[0] != 1 {
		t.Fatalf("contract problems of revision 0 = %d, want 1", lr.cp[0])
	}
	if r := reasonsOf(t, g.board, leafID); len(r) != 1 || r[0] != "contract_problem" {
		t.Fatalf("reasons = %v, want contract_problem", r)
	}
	if fc.checks(leafID) != 0 {
		t.Fatal("a contract problem was checked")
	}
	if hasCanary(t, g, "the signature cannot return an error") {
		t.Fatal("the model's contract-problem sentence reached a persisted store")
	}
}

func TestMaxRevisions(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	l := *g.plan.Leaf(leafID)
	rc.cfg.Defaults.MaxRevisions = 3
	l.MaxRevisions = -1
	if got := rc.maxRevisions(&l); got != 3 {
		t.Errorf("default = %d, want 3", got)
	}
	l.MaxRevisions = 1
	if got := rc.maxRevisions(&l); got != 1 {
		t.Errorf("node budget = %d, want 1", got)
	}
	l.MaxRevisions = 0
	if got := rc.maxRevisions(&l); got != 0 {
		t.Errorf("explicit zero = %d, want 0", got)
	}
	rc.state.ExtraRevisions[leafID] = 2
	l.MaxRevisions = 1
	if got := rc.maxRevisions(&l); got != 3 {
		t.Errorf("with extra = %d, want 3", got)
	}
}
