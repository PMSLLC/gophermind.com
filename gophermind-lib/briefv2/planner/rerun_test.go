package planner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

type boardRow struct {
	Status string
	Wave   int
}

func (g *rig) boardSnapshot() map[string]boardRow {
	g.t.Helper()
	out := map[string]boardRow{}
	for w := 0; w < 30; w++ {
		w := w
		rows, err := g.board.List(context.Background(), greeterID, blackboard.Filter{Wave: &w})
		if err != nil {
			g.t.Fatal(err)
		}
		for _, r := range rows {
			out[r.NodeID] = boardRow{Status: string(r.Status), Wave: w}
		}
	}
	return out
}

func (g *rig) ledgerCount() int {
	g.t.Helper()
	rows, err := g.led.List(context.Background(), greeterID, ledger.Filter{})
	if err != nil {
		g.t.Fatal(err)
	}
	return len(rows)
}

// The goal's clear procedure is git reset --hard, git clean -fdx and removing
// the project's state. It cannot reach ~/.gophermind, so the run record, the
// ledger and the blackboard rows of the reused run id survive it. A rerun
// must plan from nothing anyway.
func TestARerunAfterCleaningTheRepoPlansFromScratch(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})
	calls1, board1 := g.ledgerCount(), g.boardSnapshot()
	if calls1 == 0 || len(board1) == 0 {
		t.Fatalf("first run left %d ledger rows and %d board rows", calls1, len(board1))
	}

	// Stale state a dead run could have left behind in its run folder, then
	// the clean. The state lives in the folder, so a fresh folder starts clean.
	stale := filepath.Join(g.runDir, "stale.runtime.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(g.runDir); err != nil {
		t.Fatal(err)
	}
	for _, f := range g.repoFiles() {
		os.Remove(filepath.Join(g.repo, f))
	}
	g.wire()

	g.mustPlan(planner.Options{BriefPath: g.briefPath})

	if got := g.ledgerCount(); got != calls1 {
		t.Errorf("ledger has %d rows after the rerun, want %d (no stale rows)", got, calls1)
	}
	board2 := g.boardSnapshot()
	if len(board2) != len(board1) {
		t.Fatalf("%d board rows, want %d", len(board2), len(board1))
	}
	for id, want := range board1 {
		if got := board2[id]; got.Status != want.Status || got.Wave != want.Wave {
			t.Errorf("row %s = %s wave %d, want %s wave %d", id, got.Status, got.Wave, want.Status, want.Wave)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale file of the old run folder survived the rerun: %v", err)
	}
	for _, f := range greeterTestFiles {
		if _, err := os.Stat(filepath.Join(g.repo, f)); err != nil {
			t.Errorf("rerun did not write %s: %v", f, err)
		}
	}
}

// A run folder left over from a plan that died before any stage finished is
// not an error: it holds nothing to resume.
func TestALeftoverUntouchedRunFolderIsReplaced(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load"})
	g.wire()
	g.mustPlan(planner.Options{BriefPath: g.briefPath})
}

// A run folder that already holds stage output is not silently replaced.
func TestARunFolderWithProgressStillRefusesAFreshPlan(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	g.wire()
	if _, err := g.plan(planner.Options{BriefPath: g.briefPath}); err == nil {
		t.Fatal("a fresh plan over a run with progress was accepted")
	}
}

// A plan that died during its first stage leaves a ledger, an events file and
// saved replies under _state. None of that is progress: the folder is replaced.
func TestALeftoverRunFolderWithOnlyLedgerAndRepliesIsReplaced(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load"})
	err := g.led.Record(context.Background(), &ledger.Call{RunID: greeterID, Stage: "clarify", Provider: "p", Outcome: ledger.Outcome("ok")})
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(g.runDir, "_state")
	if err := os.MkdirAll(filepath.Join(state, "replies"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		filepath.Join("replies", "clarify-1.txt"): "reply",
		"events.jsonl": "{}\n",
		"calls.lock":   "",
	} {
		if err := os.WriteFile(filepath.Join(state, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	g.wire()
	g.mustPlan(planner.Options{BriefPath: g.briefPath})
}

// Real stage output next to a ledger is still progress and is still refused.
func TestARunFolderWithStageOutputAndLedgerStillRefuses(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load"})
	err := g.led.Record(context.Background(), &ledger.Call{RunID: greeterID, Stage: "clarify", Provider: "p", Outcome: ledger.Outcome("ok")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.runDir, "answers.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	g.wire()
	if _, err := g.plan(planner.Options{BriefPath: g.briefPath}); err == nil {
		t.Fatal("a fresh plan over a run with stage output was accepted")
	}
}
