package phaseflow

import (
	"context"
	"sync"
	"testing"
	"time"
)

// This file covers the contract flag (pipeline piece 3, task 3, see
// docs/superpowers/plans/2026-09-11-pipeline-piece3.md): a task that
// determines mid-execution that the contract is wrong raises
// StatusContractFlagged instead of improvising around it. That pauses the
// wave it occurred in - no new task in that wave starts, in-flight siblings
// finish normally - and stops the whole pass, so no later wave starts
// either.

// TestContractFlagStopsLaterWave: a flag raised in wave 2 must prevent
// wave 3 from ever starting, while the flagging wave's other task is still
// allowed to complete.
func TestContractFlagStopsLaterWave(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root,
		wavedTask("w1-a", 1),
		wavedTask("w2-a", 2), wavedTask("w2-b", 2),
		wavedTask("w3-a", 3),
	)

	r := &funcRunner{fn: func(ctx context.Context, t Task) (string, string, error) {
		if t.ID == "w2-a" {
			return StatusContractFlagged, "contract is missing field X", nil
		}
		return StatusDone, "", nil
	}}

	summary, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}
	if summary.ContractFlagged != 1 {
		t.Errorf("summary.ContractFlagged = %d, want 1", summary.ContractFlagged)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	w3, _ := reloaded.Task("w3-a")
	if w3.Status != StatusPending {
		t.Errorf("w3-a status = %q, want pending (wave 3 must never start)", w3.Status)
	}
	w2a, _ := reloaded.Task("w2-a")
	if w2a.Status != StatusContractFlagged {
		t.Errorf("w2-a status = %q, want %q", w2a.Status, StatusContractFlagged)
	}
	w2b, _ := reloaded.Task("w2-b")
	if w2b.Status != StatusDone {
		t.Errorf("w2-b status = %q, want done (sibling in the flagging wave must still complete)", w2b.Status)
	}
	w1a, _ := reloaded.Task("w1-a")
	if w1a.Status != StatusDone {
		t.Errorf("w1-a status = %q, want done (an earlier wave is unaffected by a later flag)", w1a.Status)
	}
}

// TestContractFlagProducesDistinctEvent: the flag must surface as its own
// status through both the persisted plan and the emit callback, never
// folded into a generic failure and never silently dropped.
func TestContractFlagProducesDistinctEvent(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, wavedTask("w1-a", 1))

	r := &funcRunner{fn: func(ctx context.Context, t Task) (string, string, error) {
		return StatusContractFlagged, "contract is missing field X", nil
	}}

	var emitted []TaskOutcome
	summary, err := ExecuteWithConcurrency(context.Background(), root, r, func(o TaskOutcome) {
		emitted = append(emitted, o)
	}, 1, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}
	if summary.Failed != 0 {
		t.Errorf("summary.Failed = %d, want 0 (a flag is not a generic failure)", summary.Failed)
	}
	if summary.ContractFlagged != 1 {
		t.Errorf("summary.ContractFlagged = %d, want 1", summary.ContractFlagged)
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted = %+v, want exactly 1 outcome", emitted)
	}
	if emitted[0].Status != StatusContractFlagged {
		t.Errorf("emitted status = %q, want %q", emitted[0].Status, StatusContractFlagged)
	}
	if emitted[0].Detail != "contract is missing field X" {
		t.Errorf("emitted detail = %q, want the flag's reason preserved", emitted[0].Detail)
	}
}

// TestContractFlagLetsInFlightSiblingFinish: a sibling already dispatched
// and genuinely running when a flag lands must be allowed to run to
// completion, not aborted.
func TestContractFlagLetsInFlightSiblingFinish(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, wavedTask("flag", 1), wavedTask("slow", 1))

	var mu sync.Mutex
	started := map[string]bool{}
	r := &funcRunner{fn: func(ctx context.Context, t Task) (string, string, error) {
		mu.Lock()
		started[t.ID] = true
		mu.Unlock()
		if t.ID == "flag" {
			return StatusContractFlagged, "contract is missing field X", nil
		}
		time.Sleep(80 * time.Millisecond)
		return StatusDone, "", nil
	}}

	if _, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, DefaultWaveConcurrency); err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}

	mu.Lock()
	bothStarted := started["flag"] && started["slow"]
	mu.Unlock()
	if !bothStarted {
		t.Fatalf("both tasks must have started concurrently for this test to exercise anything, started=%v", started)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	slow, _ := reloaded.Task("slow")
	if slow.Status != StatusDone {
		t.Errorf("slow status = %q, want done (an in-flight sibling must finish, not be aborted by the flag)", slow.Status)
	}
}

// TestNoContractFlagRunUnaffected: a run with no flag anywhere behaves
// exactly as an ordinary multi-wave run - every wave runs, nothing is
// paused.
func TestNoContractFlagRunUnaffected(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, wavedTask("w1-a", 1), wavedTask("w2-a", 2), wavedTask("w3-a", 3))

	r := &funcRunner{fn: func(ctx context.Context, t Task) (string, string, error) {
		return StatusDone, "", nil
	}}

	summary, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}
	if summary.ContractFlagged != 0 {
		t.Errorf("summary.ContractFlagged = %d, want 0", summary.ContractFlagged)
	}
	if summary.Done != 3 {
		t.Errorf("summary.Done = %d, want 3 (every wave ran)", summary.Done)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range reloaded.Tasks {
		if tk.Status != StatusDone {
			t.Errorf("task %s status = %q, want done", tk.ID, tk.Status)
		}
	}
}

// TestResetFailedToPendingLeavesContractFlaggedAlone mirrors the existing
// coverage for needs_revision/escalated (execute_needsrevision_test.go): a
// contract-flagged task must never be silently requeued by the retry-round
// mechanism, since that would work around the flag instead of pausing on
// it.
func TestResetFailedToPendingLeavesContractFlaggedAlone(t *testing.T) {
	root := t.TempDir()
	flagged := pendingTask("01-01")
	flagged.Status = StatusContractFlagged
	writeAssignments(t, root, flagged)

	if err := resetFailedToPending(root); err != nil {
		t.Fatalf("resetFailedToPending: %v", err)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := reloaded.Task("01-01")
	if tk.Status != StatusContractFlagged {
		t.Errorf("status = %q, want unchanged %q", tk.Status, StatusContractFlagged)
	}
}

// TestContractFlagStopsLaterRound: a flag must stop the RUN, not merely the
// pass it was raised in. The fixture deliberately includes a task that fails
// its first attempt, so the retry loop has an ordinary reason to start
// another round - which is exactly the shape
// TestContractFlagStopsLaterWave cannot catch, because its pass ends with no
// failures and the loop stops for an unrelated reason.
func TestContractFlagStopsLaterRound(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root,
		wavedTask("w1-a", 1),
		wavedTask("w2-a", 2), wavedTask("w2-b", 2),
		wavedTask("w3-a", 3),
	)

	var mu sync.Mutex
	attempts := map[string]int{}
	r := &funcRunner{fn: func(ctx context.Context, t Task) (string, string, error) {
		mu.Lock()
		attempts[t.ID]++
		n := attempts[t.ID]
		mu.Unlock()
		switch t.ID {
		case "w2-a":
			return StatusContractFlagged, "contract is missing field X", nil
		case "w2-b":
			if n == 1 {
				return StatusFailed, "transient", nil
			}
			return StatusDone, "", nil
		}
		return StatusDone, "", nil
	}}

	summary, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 3, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}
	if summary.ContractFlagged != 1 {
		t.Errorf("summary.ContractFlagged = %d, want 1", summary.ContractFlagged)
	}

	mu.Lock()
	w3Attempts := attempts["w3-a"]
	mu.Unlock()
	if w3Attempts != 0 {
		t.Errorf("w3-a ran %d times: no round may start work while a task is contract_flagged", w3Attempts)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	w3, _ := reloaded.Task("w3-a")
	if w3.Status != StatusPending {
		t.Errorf("w3-a status = %q, want pending (the flag must stop the whole run)", w3.Status)
	}
	w2a, _ := reloaded.Task("w2-a")
	if w2a.Status != StatusContractFlagged {
		t.Errorf("w2-a status = %q, want %q", w2a.Status, StatusContractFlagged)
	}
}
