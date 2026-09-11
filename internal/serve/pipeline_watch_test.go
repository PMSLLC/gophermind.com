package serve

import (
	"context"
	"testing"
	"time"

	"gophermind/internal/phaseflow"
)

// collect drains a hub subscription for up to d, returning the event names
// and payloads it saw.
func collect(t *testing.T, hub *PipelineHub, d time.Duration) []pipelineEvent {
	t.Helper()
	ch, unsub := hub.subscribe()
	defer unsub()

	var got []pipelineEvent
	deadline := time.After(d)
	for {
		select {
		case e := <-ch:
			got = append(got, e)
		case <-deadline:
			return got
		}
	}
}

func save(t *testing.T, root string, tasks ...phaseflow.Task) {
	t.Helper()
	a := phaseflow.Assignments{Tasks: tasks}
	if err := a.Save(root); err != nil {
		t.Fatalf("save assignments: %v", err)
	}
}

// The dashboard's whole premise is that a task changing state updates
// without a page reload. A run executes in a DIFFERENT process from the one
// serving the dashboard, so nothing in that run can call the hub directly;
// before this watcher existed, /pipeline/events opened a stream that stayed
// silent for the life of the page.
func TestPipelineWatcherPublishesAStatusChange(t *testing.T) {
	root := t.TempDir()
	save(t, root, phaseflow.Task{ID: "t1", Wave: 1, Status: phaseflow.StatusPending})

	hub := NewPipelineHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchPipeline(ctx, root, hub, 10*time.Millisecond)

	// Let the watcher take its baseline reading before anything changes.
	time.Sleep(60 * time.Millisecond)

	done := make(chan []pipelineEvent, 1)
	go func() { done <- collect(t, hub, 700*time.Millisecond) }()
	time.Sleep(30 * time.Millisecond)

	save(t, root, phaseflow.Task{ID: "t1", Wave: 1, Status: phaseflow.StatusRunning})

	var sawStatus bool
	for _, e := range <-done {
		if e.event == "task-status" {
			sawStatus = true
		}
	}
	if !sawStatus {
		t.Error("no task-status frame after the assignments file changed; the " +
			"dashboard would show stale state until the page was reloaded")
	}
}

// An attempt is the unit the run report is built from, and the source spec
// asks for attempts to appear "as they happen". Only newly appended attempts
// are published; the ones already on disk at startup are not replayed.
func TestPipelineWatcherPublishesOnlyNewAttempts(t *testing.T) {
	root := t.TempDir()
	save(t, root, phaseflow.Task{
		ID: "t1", Wave: 1, Status: phaseflow.StatusRunning,
		Attempts: []phaseflow.Attempt{{Model: "old", Verdict: "fail", Reason: "already on disk"}},
	})

	hub := NewPipelineHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchPipeline(ctx, root, hub, 10*time.Millisecond)
	time.Sleep(60 * time.Millisecond)

	done := make(chan []pipelineEvent, 1)
	go func() { done <- collect(t, hub, 700*time.Millisecond) }()
	time.Sleep(30 * time.Millisecond)

	save(t, root, phaseflow.Task{
		ID: "t1", Wave: 1, Status: phaseflow.StatusRunning,
		Attempts: []phaseflow.Attempt{
			{Model: "old", Verdict: "fail", Reason: "already on disk"},
			{Model: "new", Verdict: "pass", Reason: "6/6 passing"},
		},
	})

	var attempts int
	var payload string
	for _, e := range <-done {
		if e.event == "task-attempt" {
			attempts++
			payload = e.data
		}
	}
	if attempts != 1 {
		t.Fatalf("got %d task-attempt frames, want exactly 1 (the appended one)", attempts)
	}
	if want := `"model":"new"`; !contains(payload, want) {
		t.Errorf("attempt frame %q does not carry the new model", payload)
	}
}

// Connecting to a run that has already finished must not replay the whole
// history as if it were happening now.
func TestPipelineWatcherPublishesNothingOnItsFirstReading(t *testing.T) {
	root := t.TempDir()
	save(t, root,
		phaseflow.Task{ID: "t1", Wave: 1, Status: phaseflow.StatusDone},
		phaseflow.Task{ID: "t2", Wave: 2, Status: phaseflow.StatusDone},
	)

	hub := NewPipelineHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan []pipelineEvent, 1)
	go func() { done <- collect(t, hub, 400*time.Millisecond) }()
	time.Sleep(20 * time.Millisecond)
	go watchPipeline(ctx, root, hub, 10*time.Millisecond)

	if got := <-done; len(got) != 0 {
		t.Errorf("first reading published %d frames, want 0: the page already "+
			"fetched this state from /pipeline/state", len(got))
	}
}

// The run report is the end-of-run summary, and it goes out once.
func TestPipelineWatcherReportsWhenTheRunFinishes(t *testing.T) {
	root := t.TempDir()
	save(t, root, phaseflow.Task{ID: "t1", Wave: 1, Status: phaseflow.StatusRunning})

	hub := NewPipelineHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchPipeline(ctx, root, hub, 10*time.Millisecond)
	time.Sleep(60 * time.Millisecond)

	done := make(chan []pipelineEvent, 1)
	go func() { done <- collect(t, hub, 700*time.Millisecond) }()
	time.Sleep(30 * time.Millisecond)

	save(t, root, phaseflow.Task{
		ID: "t1", Wave: 1, Status: phaseflow.StatusDone,
		Attempts: []phaseflow.Attempt{{Model: "m", Verdict: "pass"}},
	})

	var reports int
	for _, e := range <-done {
		if e.event == "run-report" {
			reports++
		}
	}
	if reports != 1 {
		t.Errorf("got %d run-report frames, want exactly 1", reports)
	}
}

// A cancelled context must stop the goroutine; a watcher that outlived its
// server would keep a project directory busy for the life of the process.
func TestPipelineWatcherStopsOnContextCancel(t *testing.T) {
	root := t.TempDir()
	save(t, root, phaseflow.Task{ID: "t1", Status: phaseflow.StatusPending})

	hub := NewPipelineHub()
	ctx, cancel := context.WithCancel(context.Background())

	stopped := make(chan struct{})
	go func() {
		watchPipeline(ctx, root, hub, 10*time.Millisecond)
		close(stopped)
	}()

	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not return after its context was cancelled")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}()
}
