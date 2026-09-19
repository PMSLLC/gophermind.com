package plantree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRoundTripSurvivesReopen(t *testing.T) {
	_, dir := newRepo(t)
	r2 := Open(dir) // a new process would do exactly this
	got, err := r2.Get(step1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "title "+step1 || got.Status != StatusUntouched {
		t.Errorf("reopened node = %+v", got)
	}
	root, err := r2.Get(RootID)
	if err != nil || root.Objective != "ship it" {
		t.Errorf("root = %+v, %v", root, err)
	}
}

func TestInitTwiceFails(t *testing.T) {
	r, _ := newRepo(t)
	if err := r.Init(mk(t, RootID)); !errors.Is(err, ErrExists) {
		t.Errorf("second Init = %v, want ErrExists", err)
	}
}

func TestCreateRejects(t *testing.T) {
	r, _ := newRepo(t)
	if err := r.Create(mk(t, step1)); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate Create = %v, want ErrExists", err)
	}
	if err := r.Create(mk(t, "phase-002.task-001")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Create under a missing parent = %v, want ErrNotFound", err)
	}
	n := mk(t, "phase-001.task-001.step-002")
	n.NodeRevision = 2
	if err := r.Create(n); err == nil {
		t.Error("a new node must start at revision 1")
	}
	bad := mk(t, "phase-001.task-001.step-003")
	bad.Title = ""
	if err := r.Create(bad); err == nil {
		t.Error("an invalid node must be rejected")
	}
	if err := r.Create(mk(t, RootID)); err == nil {
		t.Error("Create must not accept the root")
	}
}

func TestGetMissing(t *testing.T) {
	r, _ := newRepo(t)
	if _, err := r.Get("phase-009"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func TestUpdateBumpsRevisionAndPersists(t *testing.T) {
	r, dir := newRepo(t)
	got, err := r.Update(step1, 1, func(n *Node) error {
		n.ResumeNote = "half done"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeRevision != 2 || got.ResumeNote != "half done" {
		t.Errorf("Update returned %+v", got)
	}
	again, _ := Open(dir).Get(step1)
	if again.NodeRevision != 2 || again.ResumeNote != "half done" {
		t.Errorf("persisted %+v", again)
	}
}

func TestUpdateRejectsStaleRevisionAndKeepsNewerData(t *testing.T) {
	r, _ := newRepo(t)
	if _, err := r.Update(step1, 1, func(n *Node) error { n.ResumeNote = "first"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err := r.Update(step1, 1, func(n *Node) error { n.ResumeNote = "second"; return nil })
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Expected != 1 || ce.Actual != 2 {
		t.Fatalf("stale Update = %v, want ConflictError{1,2}", err)
	}
	got, _ := r.Get(step1)
	if got.ResumeNote != "first" {
		t.Errorf("stale write overwrote newer data: %+v", got)
	}
}

func TestUpdateRejectsIdentityChangeAndInvalidResult(t *testing.T) {
	r, _ := newRepo(t)
	if _, err := r.Update(step1, 1, func(n *Node) error { n.ID = "phase-001.task-001.step-002"; return nil }); err == nil {
		t.Error("changing the id must be rejected")
	}
	if _, err := r.Update(step1, 1, func(n *Node) error { n.Status = StatusBlocked; return nil }); err == nil {
		t.Error("a blocked step without a reason must be rejected")
	}
	mutateErr := errors.New("boom")
	if _, err := r.Update(step1, 1, func(n *Node) error { return mutateErr }); !errors.Is(err, mutateErr) {
		t.Errorf("a mutate error must be returned, got %v", err)
	}
	got, _ := r.Get(step1)
	if got.NodeRevision != 1 {
		t.Errorf("a failed Update changed the node: %+v", got)
	}
}

func TestConcurrentUpdatesExactlyOneWins(t *testing.T) {
	r, _ := newRepo(t)
	const workers = 8
	var wg sync.WaitGroup
	var wins, conflicts int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := r.Update(step1, 1, func(n *Node) error {
				n.ResumeNote = fmt.Sprintf("worker %d", i)
				return nil
			})
			var ce *ConflictError
			switch {
			case err == nil:
				atomic.AddInt32(&wins, 1)
			case errors.As(err, &ce):
				atomic.AddInt32(&conflicts, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != workers-1 {
		t.Errorf("wins=%d conflicts=%d, want 1 and %d", wins, conflicts, workers-1)
	}
}

func TestChildrenNumericOrderAndMembership(t *testing.T) {
	r, dir := newRepo(t)
	for _, id := range []string{"phase-001.task-001.step-010", "phase-001.task-001.step-002"} {
		if err := r.Create(mk(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	steps := filepath.Join(dir, "plan", "phases", "phase-001", "tasks", "task-001", "steps")
	if err := os.MkdirAll(filepath.Join(steps, "step-003"), 0o755); err != nil { // no meta.json
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(steps, "notes"), 0o755); err != nil { // stray directory
		t.Fatal(err)
	}
	kids, err := r.Children("phase-001.task-001")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, k := range kids {
		got = append(got, k.ID)
	}
	want := []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002", "phase-001.task-001.step-010"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Children = %v, want %v", got, want)
	}
	if kids, err := r.Children(step1); err != nil || len(kids) != 0 {
		t.Errorf("Children(step) = %v, %v", kids, err)
	}
}

func TestWalkIsPreOrder(t *testing.T) {
	r, _ := newRepo(t)
	if err := r.Create(mk(t, "phase-002")); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := r.Walk(func(n Node) error { got = append(got, n.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	want := []string{"plan", "phase-001", "phase-001.task-001", step1, "phase-002"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Walk = %v, want %v", got, want)
	}
}

func TestReadRejectsHandEditedID(t *testing.T) {
	r, dir := newRepo(t)
	p := filepath.Join(dir, "plan", "phases", "phase-001", "tasks", "task-001", "steps", "step-001", "meta.json")
	other := mk(t, "phase-001.task-001.step-002")
	b, _ := Encode(other)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(step1); err == nil {
		t.Error("a file holding a different id must be rejected")
	}
}

func TestChildrenSkipsDirectoriesThatAreNotMembers(t *testing.T) {
	r, dir := newRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "plan", "phases", "backup-001"), 0o755); err != nil {
		t.Fatal(err)
	}
	kids, err := r.Children(RootID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 1 || kids[0].ID != "phase-001" {
		t.Errorf("Children = %v", kids)
	}
	if _, err := r.NextActions(); err != nil {
		t.Errorf("NextActions: %v", err)
	}
	if _, err := r.Summarize(RootID); err != nil {
		t.Errorf("Summarize: %v", err)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}
