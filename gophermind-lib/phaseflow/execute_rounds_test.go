package phaseflow

import (
	"context"
	"testing"
)

// roundsRunner records how many times each task ran and decides each outcome
// from a caller-supplied function, so tests can script per-attempt behavior.
type roundsRunner struct {
	attempts map[string]int
	decide   func(id string, attempt int) (status string, detail string)
	seen     []string
}

func (r *roundsRunner) Run(ctx context.Context, t Task) (string, string, error) {
	if r.attempts == nil {
		r.attempts = map[string]int{}
	}
	r.attempts[t.ID]++
	r.seen = append(r.seen, t.ID)
	status, detail := r.decide(t.ID, r.attempts[t.ID])
	return status, detail, nil
}

// TestRetryRoundsFixesFailedTask: a task that fails its first attempt is
// retried in a later round and can still finish the run green.
func TestRetryRoundsFixesFailedTask(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"))

	r := &roundsRunner{decide: func(id string, attempt int) (string, string) {
		if id == "01-02" && attempt == 1 {
			return StatusFailed, "flaked"
		}
		return StatusDone, ""
	}}

	sum, err := Execute(context.Background(), root, r, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if sum.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (retry should have fixed it)", sum.Failed)
	}
	if r.attempts["01-02"] != 2 {
		t.Errorf("01-02 attempts = %d, want 2", r.attempts["01-02"])
	}

	a, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range a.Tasks {
		if task.Status != StatusDone {
			t.Errorf("task %s status = %q, want done", task.ID, task.Status)
		}
	}
}

// TestNoProgressRoundStopsTheRun: when a round fixes nothing, retrying again
// would just spin, so the loop stops and the failure stands.
func TestNoProgressRoundStopsTheRun(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	r := &roundsRunner{decide: func(string, int) (string, string) {
		return StatusFailed, "always broken"
	}}

	sum, err := Execute(context.Background(), root, r, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if sum.Failed != 1 {
		t.Errorf("Failed = %d, want 1", sum.Failed)
	}
	if got := r.attempts["01-01"]; got != 1 {
		t.Errorf("attempts = %d, want 1 (a round with no progress must not retry)", got)
	}

	a, _, _ := LoadAssignments(root)
	if a.Tasks[0].Status != StatusFailed {
		t.Errorf("final status = %q, want failed", a.Tasks[0].Status)
	}
}

// TestRetryStopsAtMaxRounds bounds the autonomy even when every round makes
// some progress.
func TestRetryStopsAtMaxRounds(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root,
		pendingTask("01-01"), pendingTask("01-02"),
		pendingTask("01-03"), pendingTask("01-04"))

	// One new task succeeds per round; the rest fail. Progress is made every
	// round, so only maxRounds can stop the loop.
	round := 0
	r := &roundsRunner{}
	r.decide = func(id string, attempt int) (string, string) {
		if attempt > round {
			round = attempt
		}
		if id == map[int]string{1: "01-01", 2: "01-02", 3: "01-03", 4: "01-04"}[attempt] {
			return StatusDone, ""
		}
		return StatusFailed, "not yet"
	}

	sum, err := ExecuteWithRounds(context.Background(), root, r, nil, 2)
	if err != nil {
		t.Fatalf("ExecuteWithRounds: %v", err)
	}
	if round > 2 {
		t.Errorf("ran %d rounds, want at most 2", round)
	}
	if sum.Done == 0 {
		t.Error("no task completed; the fixture is wrong")
	}
	if sum.Failed == 0 {
		t.Error("expected leftover failures after the round cap")
	}
}

// TestRetryFeedsFailureDetailBack: the retry attempt must tell the runner what
// went wrong last time, without persisting that note into the saved plan.
func TestRetryFeedsFailureDetailBack(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"))

	var secondAttemptAddendum string
	r := &roundsRunner{}
	r.decide = func(id string, attempt int) (string, string) {
		if id == "01-01" && attempt == 1 {
			return StatusFailed, "compile error: missing import"
		}
		return StatusDone, ""
	}
	capture := &addendumCapturingRunner{inner: r, onAttempt: func(t Task, attempt int) {
		if t.ID == "01-01" && attempt == 2 {
			secondAttemptAddendum = t.AgentAddendum
		}
	}}

	if _, err := Execute(context.Background(), root, capture, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if secondAttemptAddendum == "" {
		t.Fatal("retry attempt carried no failure context")
	}
	if !contains(secondAttemptAddendum, "compile error: missing import") {
		t.Errorf("retry addendum missing the failure detail: %q", secondAttemptAddendum)
	}

	// The note must not be written to disk: the saved plan stays clean.
	a, _, _ := LoadAssignments(root)
	for _, task := range a.Tasks {
		if contains(task.AgentAddendum, "compile error") {
			t.Errorf("failure note leaked into the persisted plan for %s: %q", task.ID, task.AgentAddendum)
		}
	}
}

type addendumCapturingRunner struct {
	inner     *roundsRunner
	onAttempt func(t Task, attempt int)
}

func (a *addendumCapturingRunner) Run(ctx context.Context, t Task) (string, string, error) {
	next := a.inner.attempts[t.ID] + 1
	a.onAttempt(t, next)
	return a.inner.Run(ctx, t)
}
