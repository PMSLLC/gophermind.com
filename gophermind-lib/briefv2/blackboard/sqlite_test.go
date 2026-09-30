package blackboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
)

func newBB(t *testing.T) (*SQLite, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewSQLite(d), d
}

// initReady creates the nodes and moves them to ready.
func initReady(t *testing.T, b *SQLite, run string, ids ...string) {
	t.Helper()
	ctx := context.Background()
	waves := map[string]int{}
	for _, id := range ids {
		waves[id] = 0
	}
	if err := b.InitRun(ctx, run, ids, waves); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := b.SetStatus(ctx, run, id, StatusPending, StatusReady); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitRunIsIdempotent(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	if err := b.InitRun(ctx, "r", []string{"a", "b"}, map[string]int{"a": 0, "b": 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetStatus(ctx, "r", "a", StatusPending, StatusReady); err != nil {
		t.Fatal(err)
	}
	if err := b.InitRun(ctx, "r", []string{"a", "b", "c"}, map[string]int{"a": 0, "b": 1, "c": 2}); err != nil {
		t.Fatal(err)
	}
	rows, err := b.List(ctx, "r", Filter{})
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	a, _ := b.Get(ctx, "r", "a")
	if a.Status != StatusReady {
		t.Errorf("a was reset to %s; InitRun must leave existing rows alone", a.Status)
	}
}

func TestClaimRaceHasExactlyOneWinner(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "leaf")
	var wins atomic.Int32
	winner := make(chan string, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := fmt.Sprintf("worker-%d", i)
			ok, err := b.Claim(ctx, "r", "leaf", w)
			if err != nil {
				t.Errorf("Claim returned an error for a lost race: %v", err)
				return
			}
			if ok {
				wins.Add(1)
				winner <- w
			}
		}(i)
	}
	wg.Wait()
	close(winner)
	if wins.Load() != 1 {
		t.Fatalf("%d workers won the claim, want exactly 1", wins.Load())
	}
	row, _ := b.Get(ctx, "r", "leaf")
	if row.Status != StatusClaimed || row.Claim == nil || row.Claim.Worker != <-winner {
		t.Errorf("row after claim = %+v", row)
	}
}

func TestClaimOnlyWorksOnReadyNodes(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	if err := b.InitRun(ctx, "r", []string{"a"}, map[string]int{"a": 0}); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.Claim(ctx, "r", "a", "w"); ok || err != nil {
		t.Errorf("claiming a pending node = %v, %v; want false, nil", ok, err)
	}
	if ok, err := b.Claim(ctx, "r", "missing", "w"); ok || err != nil {
		t.Errorf("claiming an unknown node = %v, %v; want false, nil", ok, err)
	}
}

func TestSetStatusTransitions(t *testing.T) {
	// path lists the statuses to walk through starting from pending.
	cases := []struct {
		name string
		path []Status
		ok   bool
	}{
		{"pending to ready", []Status{StatusReady}, true},
		{"ready to claimed to in_progress to verified", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusVerified}, true},
		{"in_progress released back to ready", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusReady}, true},
		{"in_progress to needs_revision to escalated", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusNeedsRevision, StatusEscalated}, true},
		{"escalated to failed", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusNeedsRevision, StatusEscalated, StatusFailed}, true},
		{"verified back to needs_revision", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusVerified, StatusNeedsRevision}, true},
		{"pending straight to verified", []Status{StatusVerified}, false},
		{"ready straight to verified", []Status{StatusReady, StatusVerified}, false},
		{"failed is terminal", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusFailed, StatusReady}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, _ := newBB(t)
			ctx := context.Background()
			if err := b.InitRun(ctx, "r", []string{"n"}, map[string]int{"n": 0}); err != nil {
				t.Fatal(err)
			}
			from := StatusPending
			for i, to := range c.path {
				err := b.SetStatus(ctx, "r", "n", from, to)
				last := i == len(c.path)-1
				if last && !c.ok {
					if !errors.Is(err, ErrBadTransition) {
						t.Fatalf("%s -> %s: err = %v, want ErrBadTransition", from, to, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("%s -> %s: %v", from, to, err)
				}
				from = to
			}
			row, _ := b.Get(ctx, "r", "n")
			if row.Status != from {
				t.Errorf("final status = %s, want %s", row.Status, from)
			}
		})
	}
}

func TestSetStatusIsCompareAndSet(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	// The row is ready; claiming to move from pending must fail even though pending->ready is a legal edge.
	if err := b.SetStatus(ctx, "r", "n", StatusPending, StatusReady); !errors.Is(err, ErrBadTransition) {
		t.Errorf("stale from-status: err = %v, want ErrBadTransition", err)
	}
	if err := b.SetStatus(ctx, "r", "nope", StatusPending, StatusReady); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown node: err = %v, want ErrNotFound", err)
	}
}

func TestHeartbeatAndReleaseRequireTheOwner(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	if ok, _ := b.Claim(ctx, "r", "n", "alice"); !ok {
		t.Fatal("alice should win an uncontested claim")
	}
	if err := b.Heartbeat(ctx, "r", "n", "bob"); !errors.Is(err, ErrNotOwner) {
		t.Errorf("bob heartbeat: %v, want ErrNotOwner", err)
	}
	if err := b.Release(ctx, "r", "n", "bob"); !errors.Is(err, ErrNotOwner) {
		t.Errorf("bob release: %v, want ErrNotOwner", err)
	}
	if err := b.Heartbeat(ctx, "r", "n", "alice"); err != nil {
		t.Errorf("alice heartbeat: %v", err)
	}
	if err := b.Release(ctx, "r", "n", "alice"); err != nil {
		t.Fatal(err)
	}
	row, _ := b.Get(ctx, "r", "n")
	if row.Status != StatusReady || row.Claim != nil {
		t.Errorf("after release: %+v", row)
	}
	if err := b.Heartbeat(ctx, "r", "missing", "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("heartbeat on unknown node: %v, want ErrNotFound", err)
	}
}

func TestReleaseStaleReleasesOnlyOldClaims(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "old", "fresh", "idle")
	base := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return base }
	if ok, _ := b.Claim(ctx, "r", "old", "w1"); !ok {
		t.Fatal("claim old")
	}
	b.now = func() time.Time { return base.Add(19 * time.Minute) }
	if ok, _ := b.Claim(ctx, "r", "fresh", "w2"); !ok {
		t.Fatal("claim fresh")
	}
	b.now = func() time.Time { return base.Add(20 * time.Minute) }
	released, err := b.ReleaseStale(ctx, "r", 15*time.Minute)
	if err != nil || len(released) != 1 || released[0] != "old" {
		t.Fatalf("released = %v, %v; want [old]", released, err)
	}
	if row, _ := b.Get(ctx, "r", "old"); row.Status != StatusReady || row.Claim != nil {
		t.Errorf("old after release: %+v", row)
	}
	if row, _ := b.Get(ctx, "r", "fresh"); row.Status != StatusClaimed {
		t.Errorf("fresh should stay claimed, is %s", row.Status)
	}
	if row, _ := b.Get(ctx, "r", "idle"); row.Status != StatusReady {
		t.Errorf("idle should stay ready, is %s", row.Status)
	}
}

func TestAttemptsAndResultRoundTrip(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	started := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	first := Attempt{Model: "m1", Provider: "p", Revision: 0, Order: 1, StartedAt: started, DurationMS: 1200,
		Verdict: VerdictFail, TestsPassed: 2, TestsTotal: 4, FailedTests: []string{"a", "b"}, FailureReason: "off_by_one: boundary"}
	second := Attempt{Model: "m2", Provider: "q", Order: 2, StartedAt: started.Add(time.Minute), Verdict: VerdictPass, TestsPassed: 4, TestsTotal: 4}
	if err := b.AppendAttempt(ctx, "r", "n", first); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendAttempt(ctx, "r", "n", second); err != nil {
		t.Fatal(err)
	}
	if err := b.SetResult(ctx, "r", "n", Result{FilesChanged: []string{"x.go"}, Commit: "abc1234"}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetRevision(ctx, "r", "n", 2); err != nil {
		t.Fatal(err)
	}
	row, err := b.Get(ctx, "r", "n")
	if err != nil {
		t.Fatal(err)
	}
	if len(row.Attempts) != 2 || row.Attempts[0].Model != "m1" || row.Attempts[1].Model != "m2" {
		t.Fatalf("attempts = %+v", row.Attempts)
	}
	if got := row.Attempts[0]; got.FailureReason != "off_by_one: boundary" || len(got.FailedTests) != 2 || !got.StartedAt.Equal(started) || got.Verdict != VerdictFail {
		t.Errorf("first attempt lost data: %+v", got)
	}
	if row.Result == nil || row.Result.Commit != "abc1234" || row.Revision != 2 {
		t.Errorf("result/revision = %+v / %d", row.Result, row.Revision)
	}
}

func TestConcurrentAppendAttemptLosesNothing(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := b.AppendAttempt(ctx, "r", "n", Attempt{Model: fmt.Sprintf("m%d", i), Provider: "p", Verdict: VerdictError}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	row, _ := b.Get(ctx, "r", "n")
	if len(row.Attempts) != 12 {
		t.Errorf("stored %d attempts, want 12", len(row.Attempts))
	}
}

func TestListFilters(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	if err := b.InitRun(ctx, "r", []string{"a", "b", "c"}, map[string]int{"a": 0, "b": 1, "c": 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetStatus(ctx, "r", "b", StatusPending, StatusReady); err != nil {
		t.Fatal(err)
	}
	wave1 := 1
	rows, _ := b.List(ctx, "r", Filter{Wave: &wave1})
	if len(rows) != 2 {
		t.Errorf("wave 1 rows = %d, want 2", len(rows))
	}
	rows, _ = b.List(ctx, "r", Filter{Statuses: []Status{StatusReady}})
	if len(rows) != 1 || rows[0].NodeID != "b" {
		t.Errorf("ready rows = %+v", rows)
	}
	rows, _ = b.List(ctx, "r", Filter{Statuses: []Status{StatusReady, StatusPending}, Wave: &wave1})
	if len(rows) != 2 {
		t.Errorf("combined filter rows = %d, want 2", len(rows))
	}
	if rows, _ := b.List(ctx, "other", Filter{}); len(rows) != 0 {
		t.Errorf("another run's rows leaked: %d", len(rows))
	}
}

func TestWatchDeliversChanges(t *testing.T) {
	b, _ := newBB(t)
	b.poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.InitRun(ctx, "r", []string{"n"}, map[string]int{"n": 0}); err != nil {
		t.Fatal(err)
	}
	ch, err := b.Watch(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetStatus(ctx, "r", "n", StatusPending, StatusReady); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Kind != EventStatus || ev.Row.NodeID != "n" || ev.Row.Status != StatusReady {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3 seconds")
	}
	cancel()
	select {
	case _, open := <-ch:
		for open {
			_, open = <-ch
		}
	case <-time.After(3 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}
