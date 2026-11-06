package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/tree"
)

func TestBriefResumeFromRefusesAnUnknownStep(t *testing.T) {
	id, repo := plannedGreeter(t)
	code, out, errs := runBriefCmd(t, "", "resume", id, "--from", "bogus", "--fake", filepath.Join(greeterExecDir, "planner"))
	if code != 1 || !strings.Contains(errs, "unknown step") {
		t.Fatalf("code=%d out=%q err=%q, want 1 and unknown step", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(runDirInRepo(repo, id), "approval.json")); err != nil {
		t.Errorf("a refused resume moved the run: %v", err)
	}
}

func TestBriefResumeFromNodeResetsAndExits0(t *testing.T) {
	id, repo := plannedGreeter(t)
	runDir := runDirInRepo(repo, id)
	tr, err := tree.NewStore(runDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	var node string
	var ids []string
	waves := map[string]int{}
	for nid := range tr.Nodes {
		ids = append(ids, nid)
		waves[nid] = 0
		node = nid
	}
	ctx := context.Background()
	board, _ := briefBackends()
	if err := board.InitRun(ctx, id, ids, waves); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][2]blackboard.Status{{blackboard.StatusPending, blackboard.StatusReady}} {
		if err := board.SetStatus(ctx, id, node, step[0], step[1]); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := board.Claim(ctx, id, node, "w"); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	for _, step := range [][2]blackboard.Status{
		{blackboard.StatusClaimed, blackboard.StatusInProgress},
		{blackboard.StatusInProgress, blackboard.StatusNeedsRevision},
		{blackboard.StatusNeedsRevision, blackboard.StatusEscalated},
	} {
		if err := board.SetStatus(ctx, id, node, step[0], step[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(runDir, "_state", "executor.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runBriefCmd(t, "", "resume", id, "--from", "node:"+node)
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	want := "resumed " + node + "; run: gophermind brief run " + id + " --repo " + repo
	if !strings.Contains(out, want) {
		t.Errorf("out=%q lacks %q", out, want)
	}
	row, err := board.Get(ctx, id, node)
	if err != nil || row.Status != blackboard.StatusReady {
		t.Errorf("row %+v %v, want ready", row, err)
	}
}

func TestBriefResumeRejectsResetRepoWithoutFrom(t *testing.T) {
	id, _ := plannedGreeter(t)
	for _, args := range [][]string{
		{"resume", id, "--reset-repo"},
		{"resume", id, "--from", "node:fn-greet", "--reset-repo"},
	} {
		code, out, errs := runBriefCmd(t, "", args...)
		if code != 1 || !strings.Contains(errs, "error: --reset-repo needs --from <stage>") {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
	}
	for _, args := range [][]string{{"resume", "--from", "confirm"}, {"plan", "x.md", "--from", "confirm"}} {
		code, _, errs := runBriefCmd(t, "", args...)
		if code != 1 || !strings.Contains(errs, "usage:") {
			t.Errorf("%v: code=%d err=%q, want usage", args, code, errs)
		}
	}
}
