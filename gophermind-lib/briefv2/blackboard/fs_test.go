package blackboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

func newBB(t *testing.T) *FS {
	t.Helper()
	return NewFS(runfs.Fixed(t.TempDir()), WithFlatLayout())
}

// initReady creates the nodes and moves them to ready.
func initReady(t *testing.T, b *FS, run string, ids ...string) {
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
	b := newBB(t)
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
	b := newBB(t)
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
	b := newBB(t)
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
			b := newBB(t)
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
	b := newBB(t)
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
	b := newBB(t)
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
	b := newBB(t)
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
	b := newBB(t)
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
	b := newBB(t)
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
	b := newBB(t)
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
	b := newBB(t)
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

func TestAttemptReplySHA256RoundTrips(t *testing.T) {
	b := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	sum := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if err := b.AppendAttempt(ctx, "r", "n", Attempt{Model: "m1", Order: 1, StartedAt: at, Verdict: VerdictPass, ReplySHA256: sum}); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendAttempt(ctx, "r", "n", Attempt{Model: "m2", Order: 2, StartedAt: at, Verdict: VerdictError}); err != nil {
		t.Fatal(err)
	}
	row, err := b.Get(ctx, "r", "n")
	if err != nil {
		t.Fatal(err)
	}
	if len(row.Attempts) != 2 || row.Attempts[0].ReplySHA256 != sum || row.Attempts[1].ReplySHA256 != "" {
		t.Fatalf("attempts = %+v", row.Attempts)
	}
}

func TestFSReadsDoNotWrite(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	if _, err := b.List(ctx, "r", Filter{}); err != nil {
		t.Fatalf("List of an empty run: %v", err)
	}
	if _, err := b.Get(ctx, "r", "nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of a missing node: %v", err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Errorf("reads created %d entries in the run folder", len(ents))
	}
}

func TestFSRefusesAnUnsafeNodeID(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	for _, id := range []string{"../escape", "/abs", "a/b", "", "UPPER", ".hidden"} {
		if err := b.InitRun(ctx, "r", []string{id}, map[string]int{id: 0}); err == nil {
			t.Errorf("InitRun accepted node id %q", id)
		}
	}
	ents, _ := os.ReadDir(filepath.Dir(dir))
	for _, e := range ents {
		if e.Name() == "escape" {
			t.Error("a node id escaped the run folder")
		}
	}
}

func TestFSNeverReadsATornFile(t *testing.T) {
	b := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = b.AppendAttempt(ctx, "r", "n", Attempt{Model: "m", Verdict: VerdictFail, FailureReason: strings.Repeat("x", 500)})
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			wg.Wait()
			return
		default:
			if _, err := b.Get(ctx, "r", "n"); err != nil {
				t.Fatalf("a read during a write failed: %v", err)
			}
		}
	}
}

func TestFSRuntimeFileSitsBesideTheNodeFile(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	for _, raw := range []string{
		`{"spec_version":"2.0","id":"rt","kind":"root","title":"t","description":"d","brief_ref":"#x","status":"pending","children":["comp"]}`,
		`{"spec_version":"2.0","id":"comp","kind":"component","parent":"rt","title":"t","description":"d","brief_ref":"#x","status":"pending","children":["fn-a"]}`,
		`{"spec_version":"2.0","id":"fn-a","kind":"function","parent":"comp","title":"t","description":"d","brief_ref":"#x","status":"pending","wave":0,"depends_on":[],
"contract":{"package":"p","file":"p/fn-a.go","signature":"func F()","inputs":[],"outputs":[]},
"tests":[{"name":"n","level":"unit","given":"g","expect":"e","command":"go test ./p"}]}`,
	} {
		n, err := tree.ParseNode([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Write(n); err != nil {
			t.Fatal(err)
		}
	}
	b := NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	if err := b.InitRun(ctx, "run1", []string{"rt", "comp", "fn-a"}, map[string]int{"rt": 2, "comp": 1, "fn-a": 0}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"root.runtime.json", "comp/component.runtime.json", "comp/fn-a.runtime.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("expected %s beside the node file: %v", want, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "comp", "fn-a.runtime.json"))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil || doc["node_id"] != "fn-a" || doc["status"] != "pending" {
		t.Errorf("runtime file = %s (%v)", raw, err)
	}
	if _, err := b.Get(ctx, "run1", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a node that is not in the tree: err = %v, want ErrNotFound", err)
	}
	if loaded, err := s.Load(); err != nil || len(loaded.Nodes) != 3 {
		t.Errorf("Load must still see only the 3 plan nodes: %d, %v", len(loaded.Nodes), err)
	}
}

func TestFSEventsAreOneJSONLinePerChange(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	initReady(t, b, "r", "n")
	if ok, _ := b.Claim(ctx, "r", "n", "w"); !ok {
		t.Fatal("claim")
	}
	lines, _, err := runfs.ReadLines(filepath.Join(dir, "_state", "events.jsonl"), 0)
	if err != nil || len(lines) != 2 {
		t.Fatalf("event lines = %d, %v; want 2 (ready, claimed)", len(lines), err)
	}
	var e struct {
		RunID  string `json:"run_id"`
		NodeID string `json:"node_id"`
		Kind   string `json:"kind"`
		At     string `json:"at"`
	}
	if err := json.Unmarshal(lines[1], &e); err != nil || e.Kind != "status" || e.NodeID != "n" {
		t.Errorf("last event = %s (%v)", lines[1], err)
	}
}

func TestFSLeavesAStaleLockBehindNothing(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	initReady(t, b, "r", "n")
	lock := filepath.Join(dir, "nodes", "n.runtime.json.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(lock, old, old)
	if ok, err := b.Claim(ctx, "r", "n", "w"); err != nil || !ok {
		t.Fatalf("claim past a dead holder's lock: %v %v", ok, err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("lock still present after the claim: %v", err)
	}
}

func TestFSListSeesANodeFolderNamedAttempts(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	for _, raw := range []string{
		`{"spec_version":"2.0","id":"rt","kind":"root","title":"t","description":"d","brief_ref":"#x","status":"pending","children":["attempts"]}`,
		`{"spec_version":"2.0","id":"attempts","kind":"component","parent":"rt","title":"t","description":"d","brief_ref":"#x","status":"pending","children":["fn-a"]}`,
		`{"spec_version":"2.0","id":"fn-a","kind":"function","parent":"attempts","title":"t","description":"d","brief_ref":"#x","status":"pending","wave":0,"depends_on":[],
"contract":{"package":"p","file":"p/fn-a.go","signature":"func F()","inputs":[],"outputs":[]},
"tests":[{"name":"n","level":"unit","given":"g","expect":"e","command":"go test ./p"}]}`,
	} {
		n, err := tree.ParseNode([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Write(n); err != nil {
			t.Fatal(err)
		}
	}
	b := NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return clock }
	if err := b.InitRun(ctx, "r", []string{"rt", "attempts", "fn-a"}, map[string]int{"rt": 2, "attempts": 1, "fn-a": 0}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetStatus(ctx, "r", "fn-a", StatusPending, StatusReady); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.Claim(ctx, "r", "fn-a", "w"); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	rows, err := b.List(ctx, "r", Filter{})
	if err != nil || len(rows) != 3 {
		t.Fatalf("List = %d rows, %v; want 3 including fn-a under attempts/", len(rows), err)
	}
	clock = clock.Add(time.Minute)
	ids, err := b.ReleaseStale(ctx, "r", 0)
	if err != nil || len(ids) != 1 || ids[0] != "fn-a" {
		t.Fatalf("ReleaseStale = %v, %v; want [fn-a]", ids, err)
	}
}

func TestFSListOfAMissingRunFolder(t *testing.T) {
	b := NewFS(runfs.Fixed(filepath.Join(t.TempDir(), "nope")), WithFlatLayout())
	rows, err := b.List(context.Background(), "r", Filter{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("List = %d rows, %v; want none and no error", len(rows), err)
	}
}
