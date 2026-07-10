package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestRunResumableSkipsCompleted(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	progress := filepath.Join(t.TempDir(), "progress.json")
	steps := []string{"step1", "step2", "step3"}

	// First run: fail at step3 (after 1 and 2 complete + checkpoint).
	var ran []string
	run := func(_ context.Context, s string) (string, error) {
		ran = append(ran, s)
		if s == "step3" {
			return "", errors.New("interrupted")
		}
		return "ok", nil
	}
	if err := a.RunResumable(context.Background(), steps, run, progress); err == nil {
		t.Fatal("expected the interrupted run to error")
	}
	if len(ran) != 3 {
		t.Fatalf("first pass should attempt through step3, got %v", ran)
	}

	// Second run: steps 1 and 2 are already checkpointed, so only step3 re-runs.
	ran = nil
	run2 := func(_ context.Context, s string) (string, error) {
		ran = append(ran, s)
		return "ok", nil
	}
	if err := a.RunResumable(context.Background(), steps, run2, progress); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "step3" {
		t.Errorf("resume should only re-run step3, got %v", ran)
	}
}
