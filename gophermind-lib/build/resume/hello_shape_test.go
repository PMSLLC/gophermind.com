package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gophermind/gophermind-lib/build/blackboard"
)

// TestHelloR003ShapeResumesAtFnRun builds the run folder the recorded hello run
// r003 left behind (every planner stage through testwriter, the executor's
// state, fn-greeting verified, fn-run escalated after two attempts, fn-main
// pending) and resumes it at fn-run.
func TestHelloR003ShapeResumesAtFnRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var planner []string
	for _, s := range Stages {
		for _, f := range stageFiles(s) {
			if f == "decisions" {
				f = "decisions/q1.md"
			}
			put(t, dir, f, "planner output "+f)
			planner = append(planner, f)
		}
	}
	put(t, dir, "_state/executor.json", `{}`)
	b := nodeBoard(t, dir, map[string]blackboard.Status{
		"fn-greeting": blackboard.StatusVerified,
		"fn-run":      blackboard.StatusEscalated,
		"fn-main":     blackboard.StatusPending,
	})
	for _, m := range []string{"small", "strong"} {
		if err := b.AppendAttempt(ctx, "r", "fn-run", blackboard.Attempt{Model: m, Verdict: blackboard.VerdictFail}); err != nil {
			t.Fatal(err)
		}
	}
	put(t, dir, "_state/leaf-results.json", `{"fn-greeting":{"status":"verified"},"fn-run":{"status":"escalated","reason":"ladder_exhausted"}}`)
	put(t, dir, "ESCALATION-fn-run.md", "escalated")
	put(t, dir, "report.json", "{}")

	pts, err := Points(ctx, dir, b, "r")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pts {
		got = append(got, p.Step)
	}
	want := append(append([]string{}, Stages...), "node:fn-run")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("points %v, want %v", got, want)
	}

	rowBytes := func(id string) []byte {
		row, err := b.Get(ctx, "r", id)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := map[string][]byte{}
	for _, f := range planner {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		before[f] = raw
	}
	greeting, mainRow := rowBytes("fn-greeting"), rowBytes("fn-main")

	res, err := FromNode(ctx, dir, b, "r", "fn-run", fixed)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range planner {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil || !bytes.Equal(raw, before[f]) {
			t.Errorf("planner file %s changed: %v", f, err)
		}
	}
	if !bytes.Equal(rowBytes("fn-greeting"), greeting) {
		t.Error("fn-greeting row changed")
	}
	if !bytes.Equal(rowBytes("fn-main"), mainRow) {
		t.Error("fn-main row changed")
	}
	run, err := b.Get(ctx, "r", "fn-run")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != blackboard.StatusReady || len(run.Attempts) != 0 {
		t.Errorf("fn-run: %+v", run)
	}
	for _, name := range []string{"attempts-fn-run.json", "report.json", "ESCALATION-fn-run.md", "leaf-result-fn-run.json"} {
		if !present(dir, res.DiscardDir+"/"+name) {
			t.Errorf("discard dir lacks %s", name)
		}
	}
	for _, rel := range []string{"report.json", "ESCALATION-fn-run.md"} {
		if present(dir, rel) {
			t.Errorf("%s still in place", rel)
		}
	}
}
