package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/tree"
)

func TestBriefStatusReadsTheRunFolderAndCreatesNoDatabase(t *testing.T) {
	id, repo := plannedGreeter(t)
	cfgDir := os.Getenv("GOPHERMIND_CONFIG_DIR")
	runDir := runDirInRepo(repo, id)
	tr, err := tree.NewStore(runDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	waves, err := tr.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}
	var nodeIDs []string
	for nid := range waves {
		nodeIDs = append(nodeIDs, nid)
	}
	board, led := briefBackends()
	ctx := context.Background()
	if err := board.InitRun(ctx, id, nodeIDs, waves); err != nil {
		t.Fatal(err)
	}
	rows, err := board.List(ctx, id, blackboard.Filter{})
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	if _, err := led.Summary(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "blackboard.db")); !os.IsNotExist(err) {
		t.Errorf("a database file was created: %v", err)
	}
}
