package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/gitland"
)

// overrideBoard shows one node with a status the test chose, for the reads
// finish and the report make; it never writes.
type overrideBoard struct {
	blackboard.Blackboard
	node   string
	status blackboard.Status
}

func (b overrideBoard) Get(ctx context.Context, runID, nodeID string) (blackboard.Row, error) {
	r, err := b.Blackboard.Get(ctx, runID, nodeID)
	if nodeID == b.node {
		r.Status = b.status
	}
	return r, err
}

func (b overrideBoard) List(ctx context.Context, runID string, f blackboard.Filter) ([]blackboard.Row, error) {
	rows, err := b.Blackboard.List(ctx, runID, f)
	for i := range rows {
		if rows[i].NodeID == b.node {
			rows[i].Status = b.status
		}
	}
	return rows, err
}

// recGit logs the commit-making calls of the git layer and can fail Finish.
type recGit struct {
	gitland.Repo
	log       *orderLog
	finishErr error
}

func (r *recGit) CommitLeaf(nodeID, title string, add, remove []string) (string, error) {
	r.log.add("CommitLeaf")
	return r.Repo.CommitLeaf(nodeID, title, add, remove)
}

func (r *recGit) CommitRepair(nodeID string, round int, add []string) (string, error) {
	r.log.add("CommitRepair")
	return r.Repo.CommitRepair(nodeID, round, add)
}

func (r *recGit) Finish(msg string) (string, error) {
	r.log.add("Finish")
	if r.finishErr != nil {
		return "", r.finishErr
	}
	return r.Repo.Finish(msg)
}

func withRecGit(g *rig, finishErr error, rec **recGit) func(*Options, *orderLog) {
	return func(o *Options, log *orderLog) {
		*rec = &recGit{Repo: g.git, log: log, finishErr: finishErr}
		o.Git = *rec
	}
}

func TestFinishVerifiedImpliesEveryLeafVerified(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	rows, err := g.board.List(context.Background(), g.id, blackboard.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if g.plan.Leaf(r.NodeID) != nil && r.Status != blackboard.StatusVerified {
			t.Errorf("a verified run has %s at %s", r.NodeID, r.Status)
		}
	}
	if rep.Nodes.Verified != rep.Nodes.Total {
		t.Errorf("nodes = %+v", rep.Nodes)
	}

	g2 := newRig(t)
	fc := g2.fastChecker()
	rc, _ := g2.leafRC(t, goodScript(g2), false)
	rc.chk = fc
	if stop, err := rc.runWaves(context.Background(), func() string { return "" }); err != nil || stop != nil {
		t.Fatalf("runWaves = %v, %v", stop, err)
	}
	real := rc.o.Board
	cases := []struct {
		name         string
		status       blackboard.Status
		reason       string
		wantStatus   string
		wantReason   string
		wantFailures []string
	}{
		{"pending", blackboard.StatusPending, "", "failed", "leaf_not_verified",
			[]string{"fn-greet: not_run: leaf_not_verified"}},
		{"failed", blackboard.StatusFailed, "test_fail", "failed", "test_fail", []string{"fn-greet: test_fail"}},
		{"escalated", blackboard.StatusEscalated, "ladder_exhausted", "escalated", "human_stop", []string{"fn-greet: ladder_exhausted"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc.o.Board = overrideBoard{Blackboard: real, node: "fn-greet", status: tc.status}
			defer func() { rc.o.Board = real }()
			if tc.reason != "" {
				if err := rc.setResult("fn-greet", tc.status, tc.reason); err != nil {
					t.Fatal(err)
				}
			}
			fin, err := rc.finish(context.Background(), runFlags{skipAcceptance: true})
			if err != nil {
				t.Fatal(err)
			}
			if fin.Status == "verified" || fin.Status != tc.wantStatus || fin.Reason != tc.wantReason {
				t.Fatalf("finish = %s (%s), want %s (%s)", fin.Status, fin.Reason, tc.wantStatus, tc.wantReason)
			}
			if fin.Acc.Passed != 0 || fin.Acc.Total != 2 {
				t.Errorf("acceptance = %+v, want 0 of 2", fin.Acc)
			}
			rep, err := rc.buildReport(context.Background(), fin, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(rep.Failures, "\n")
			for _, w := range tc.wantFailures {
				if !strings.Contains(got, w) {
					t.Errorf("failures %q lack %q", got, w)
				}
			}
			if rep.Status == "verified" {
				t.Error("the report says verified")
			}
		})
	}
}

func TestFinishOrder(t *testing.T) {
	t.Parallel()
	t.Run("acceptance, go mod verify, scan, Finish", func(t *testing.T) {
		g := newRig(t)
		var rec *recGit
		rep, err, log := g.accRun(t, goodScript(g), g.fastChecker(), nil, withRecGit(g, nil, &rec))
		if err != nil || rep.Status != "verified" {
			t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
		lastAccept, mod, scan, fin := log.index("accept", true), log.index("modverify", false), log.index("event:scan", true), log.index("Finish", false)
		if lastAccept < 0 || !(lastAccept < mod && mod < scan && scan < fin) {
			t.Fatalf("order = %v", log.list())
		}
		if log.count("accept") != 3 {
			t.Errorf("%d acceptance commands ran, want 3 (A1, A2, C1)", log.count("accept"))
		}
	})
	t.Run("a failing bullet: no go mod verify, no Finish", func(t *testing.T) {
		g := newRig(t)
		var rec *recGit
		edit := func(rc *runCtx, h *hybridChecker) {
			setCommand(t, rc, "A2", "exit 1")
			bullet(t, rc, "A2").nodes = []string{"root"}
		}
		rep, err, log := g.accRun(t, goodScript(g), g.fastChecker(), edit, withRecGit(g, nil, &rec))
		if err != nil || rep.Status != "failed" {
			t.Fatalf("run = %s (%s), %v", rep.Status, rep.StopReason, err)
		}
		if log.count("Finish") != 0 || log.count("modverify") != 0 {
			t.Errorf("order = %v", log.list())
		}
	})
	t.Run("go mod verify failing: no Finish", func(t *testing.T) {
		g := newRig(t)
		var rec *recGit
		rep, err, log := g.accRun(t, goodScript(g), g.fastChecker(), func(rc *runCtx, h *hybridChecker) { h.failMod = true }, withRecGit(g, nil, &rec))
		if err != nil || rep.Status != "failed" || rep.StopReason != "mod_verify" {
			t.Fatalf("run = %s (%s), %v", rep.Status, rep.StopReason, err)
		}
		if log.count("Finish") != 0 {
			t.Errorf("Finish was called after a failed go mod verify: %v", log.list())
		}
	})
}

func TestFinishDiffOnly(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string { return strings.Replace(s, "landing: commit", "landing: diff_only", 1) }
	})
	var rec *recGit
	rep, err, log := g.accRun(t, goodScript(g), g.fastChecker(), nil, withRecGit(g, nil, &rec))
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	if log.count("Finish") != 0 || log.count("CommitLeaf") != 0 || log.count("CommitRepair") != 0 {
		t.Errorf("diff_only committed: %v", log.list())
	}
	fi, err := os.Stat(filepath.Join(g.runDir, "changes.patch"))
	if err != nil || fi.Size() == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("changes.patch: %v, %v", fi, err)
	}
	if rep.Acceptance.Passed != 2 || rep.Landing != nil {
		t.Errorf("acceptance %+v landing %+v", rep.Acceptance, rep.Landing)
	}
}

func TestLandingBlockedIsFailedNotError(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	var rec *recGit
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, withRecGit(g, gitland.ErrLandingBlocked, &rec))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if rep.Status != "failed" || rep.StopReason != "landing_blocked" {
		t.Fatalf("report = %s (%s)", rep.Status, rep.StopReason)
	}
	if rep.Landing == nil || rep.Landing.Branch == "" || rep.Landing.Commit != "" {
		t.Errorf("landing = %+v, want the work branch and no commit", rep.Landing)
	}
}

func TestAcceptanceInterruptedIsNotAPass(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.wire(goodScript(g))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := filepath.Join(g.repo, ".gophermind", g.id+"-scratch", "started")
	go func() {
		for i := 0; i < 600; i++ {
			if fileExists(started) {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
	}()
	fc := g.fastChecker()
	hook := func(rc *runCtx) {
		rc.chk = &hybridChecker{fakeChecker: fc, real: rc.chk, log: &orderLog{}}
		setCommand(t, rc, "A1", `echo started > "$TMPDIR/started"; sleep 300`)
	}
	rep, err := run(ctx, g.options(), runFlags{afterStart: hook})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "interrupted" || rep.ExitCode != 5 {
		t.Fatalf("report = %s (%s) exit %d", rep.Status, rep.StopReason, rep.ExitCode)
	}
	if rep.Acceptance.Passed != 0 {
		t.Errorf("acceptance = %+v, want nothing passed", rep.Acceptance)
	}
	if fileExists(filepath.Join(g.runDir, "acceptance.json")) {
		t.Error("acceptance.json was written for a cut-off run")
	}
}
