package phaseflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// This file covers concurrent wave execution (pipeline piece 3, task 2, see
// docs/superpowers/plans/2026-09-11-pipeline-piece3.md). Tasks carry an
// explicit Wave (as a planner running AssignWaves would set it); executeOnce
// groups pending tasks by that field and runs each wave's tasks concurrently,
// waves in ascending order.
//
// wavedTask predates the wave field defaulting to "unassigned" (0): every
// task built by the older pendingTask helper still has Wave 0, which is why
// those pre-existing tests keep running one task at a time - see
// executeOnceConcurrency in execute.go.

func wavedTask(id string, wave int) Task {
	tk := pendingTask(id)
	tk.Wave = wave
	return tk
}

// timedRunner is a concurrency-safe TaskRunner double: every field access is
// behind a mutex, so it stays race-clean under real parallel dispatch, unlike
// the older fakeRunner (which is deliberately left as-is; see the "must pass
// unedited" pre-existing tests).
type timedRunner struct {
	mu        sync.Mutex
	intervals map[string][2]time.Time
	current   int
	maxSeen   int
	calls     []string
	sleep     time.Duration
	failIDs   map[string]bool
}

func newTimedRunner(sleep time.Duration) *timedRunner {
	return &timedRunner{intervals: map[string][2]time.Time{}, sleep: sleep}
}

func (r *timedRunner) Run(ctx context.Context, t Task) (string, string, error) {
	r.mu.Lock()
	r.current++
	if r.current > r.maxSeen {
		r.maxSeen = r.current
	}
	r.calls = append(r.calls, t.ID)
	start := time.Now()
	r.mu.Unlock()

	select {
	case <-time.After(r.sleep):
	case <-ctx.Done():
	}

	end := time.Now()

	r.mu.Lock()
	r.current--
	r.intervals[t.ID] = [2]time.Time{start, end}
	fail := r.failIDs[t.ID]
	r.mu.Unlock()

	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if fail {
		return "", "boom", errors.New("boom")
	}
	return StatusDone, "", nil
}

func (r *timedRunner) interval(id string) (time.Time, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	iv := r.intervals[id]
	return iv[0], iv[1]
}

func overlaps(startA, endA, startB, endB time.Time) bool {
	return startA.Before(endB) && startB.Before(endA)
}

// TestWaveTasksActuallyOverlap is the test the source spec names by name:
// two independent tasks in the same wave must actually execute concurrently,
// verified by their measured intervals overlapping - not merely by both
// having run. A sequential implementation cannot pass this: task B's start
// would always be at or after task A's end.
func TestWaveTasksActuallyOverlap(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, wavedTask("w1-a", 1), wavedTask("w1-b", 1))

	r := newTimedRunner(80 * time.Millisecond)
	if _, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, DefaultWaveConcurrency); err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}

	startA, endA := r.interval("w1-a")
	startB, endB := r.interval("w1-b")
	if startA.IsZero() || startB.IsZero() {
		t.Fatalf("both tasks must have run: a=%v b=%v", r.intervals["w1-a"], r.intervals["w1-b"])
	}
	if !overlaps(startA, endA, startB, endB) {
		t.Errorf("intervals do not overlap: a=[%v,%v] b=[%v,%v], want concurrent execution", startA, endA, startB, endB)
	}
}

// TestWaveTwoDoesNotStartUntilWaveOneFinishes: every wave-1 task's interval
// must end before the wave-2 task's interval starts.
func TestWaveTwoDoesNotStartUntilWaveOneFinishes(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, wavedTask("w1-a", 1), wavedTask("w1-b", 1), wavedTask("w2-a", 2))

	r := newTimedRunner(40 * time.Millisecond)
	if _, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, DefaultWaveConcurrency); err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}

	_, endA := r.interval("w1-a")
	_, endB := r.interval("w1-b")
	start2, _ := r.interval("w2-a")
	if start2.IsZero() {
		t.Fatal("wave-2 task never ran")
	}
	if start2.Before(endA) || start2.Before(endB) {
		t.Errorf("wave-2 task started (%v) before wave-1 finished (a end=%v, b end=%v)", start2, endA, endB)
	}
}

// TestWaveFailureDoesNotBlockSiblings: one task failing in a wave must not
// prevent its sibling in the same wave from completing.
func TestWaveFailureDoesNotBlockSiblings(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, wavedTask("w1-a", 1), wavedTask("w1-b", 1))

	r := newTimedRunner(20 * time.Millisecond)
	r.failIDs = map[string]bool{"w1-a": true}

	summary, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, DefaultWaveConcurrency)
	if err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}
	if summary.Failed != 1 || summary.Done != 1 {
		t.Errorf("summary = %+v, want Failed=1 Done=1", summary)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	ta, _ := reloaded.Task("w1-a")
	tb, _ := reloaded.Task("w1-b")
	if ta.Status != StatusFailed {
		t.Errorf("w1-a status = %q, want failed", ta.Status)
	}
	if tb.Status != StatusDone {
		t.Errorf("w1-b status = %q, want done (sibling must still complete)", tb.Status)
	}
}

// TestWaveConcurrencyLimitRespected: with 6 same-wave tasks and a limit of 2,
// no more than 2 may ever be in flight at once.
func TestWaveConcurrencyLimitRespected(t *testing.T) {
	root := t.TempDir()
	tasks := make([]Task, 0, 6)
	for i := 0; i < 6; i++ {
		tasks = append(tasks, wavedTask(string(rune('a'+i)), 1))
	}
	writeAssignments(t, root, tasks...)

	r := newTimedRunner(30 * time.Millisecond)
	if _, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, 2); err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}

	r.mu.Lock()
	maxSeen := r.maxSeen
	r.mu.Unlock()
	if maxSeen > 2 {
		t.Errorf("max simultaneous in-flight = %d, want at most 2", maxSeen)
	}
	if maxSeen < 2 {
		t.Errorf("max simultaneous in-flight = %d, want exactly 2 (concurrency never actually exercised)", maxSeen)
	}
}

// blockingRunner lets a test hold tasks open until it chooses to release
// them, and reports each Run call's start over a channel so the test can
// deterministically wait for a specific number of tasks to be in flight
// before acting (for example, cancelling).
type blockingRunner struct {
	started chan string
	release chan struct{}
}

func (r *blockingRunner) Run(ctx context.Context, t Task) (string, string, error) {
	r.started <- t.ID
	select {
	case <-r.release:
	case <-ctx.Done():
	}
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	return StatusDone, "", nil
}

// TestWaveCancellationRevertsInFlightAndStopsLaterWaves: cancelling the
// context mid-wave must revert every in-flight task to pending and must not
// start the next wave.
func TestWaveCancellationRevertsInFlightAndStopsLaterWaves(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, wavedTask("w1-a", 1), wavedTask("w1-b", 1), wavedTask("w2-a", 2))

	r := &blockingRunner{started: make(chan string, 2), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	var summary RunSummary
	var runErr error
	go func() {
		summary, runErr = ExecuteWithConcurrency(ctx, root, r, nil, 1, DefaultWaveConcurrency)
		close(done)
	}()

	<-r.started
	<-r.started
	cancel()
	close(r.release)
	<-done

	if runErr != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", runErr)
	}
	if summary.Done != 0 || summary.Failed != 0 {
		t.Errorf("summary = %+v, want nothing tallied (cancelled tasks are not counted)", summary)
	}

	reloaded, _, err := LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	ta, _ := reloaded.Task("w1-a")
	tb, _ := reloaded.Task("w1-b")
	tw2, _ := reloaded.Task("w2-a")
	if ta.Status != StatusPending {
		t.Errorf("w1-a status = %q, want pending (reverted)", ta.Status)
	}
	if tb.Status != StatusPending {
		t.Errorf("w1-b status = %q, want pending (reverted)", tb.Status)
	}
	if tw2.Status != StatusPending {
		t.Errorf("w2-a status = %q, want pending (never started)", tw2.Status)
	}
}

// TestWaveZeroTasksRunSolo pins that a plan with tasks at the "unassigned"
// wave (the zero value, exactly what every pre-existing execute_*_test.go
// fixture has) never batches them together - see executeOnceConcurrency.
// Two wave-0 tasks made deliberately slow must NOT overlap.
func TestWaveZeroTasksRunSolo(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"))

	r := newTimedRunner(40 * time.Millisecond)
	if _, err := ExecuteWithConcurrency(context.Background(), root, r, nil, 1, DefaultWaveConcurrency); err != nil {
		t.Fatalf("ExecuteWithConcurrency: %v", err)
	}

	startA, endA := r.interval("01-01")
	startB, endB := r.interval("01-02")
	if overlaps(startA, endA, startB, endB) {
		t.Errorf("wave-0 (unassigned) tasks overlapped: a=[%v,%v] b=[%v,%v], want strictly sequential", startA, endA, startB, endB)
	}
}
