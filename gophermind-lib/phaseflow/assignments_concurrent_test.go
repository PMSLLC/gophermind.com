package phaseflow

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestUpdateDoesNotLoseConcurrentTaskUpdates is the central concurrency
// guarantee this store must hold once waves run tasks in parallel: N
// goroutines each flip a DIFFERENT task's status through Update, and every
// one of them must stick. Under a plain load-mutate-Save built from a stale
// read, the last writer wins and the rest are silently lost.
func TestUpdateDoesNotLoseConcurrentTaskUpdates(t *testing.T) {
	root := t.TempDir()
	const n = 20

	var a Assignments
	for i := 0; i < n; i++ {
		a.Tasks = append(a.Tasks, Task{ID: fmt.Sprintf("t%02d", i), Status: StatusPending})
	}
	if err := a.Save(root); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("t%02d", i)
			if err := Update(root, func(a *Assignments) error {
				for j := range a.Tasks {
					if a.Tasks[j].ID == id {
						a.Tasks[j].Status = StatusDone
						return nil
					}
				}
				return fmt.Errorf("task %s missing", id)
			}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	final, found, err := LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("load after concurrent updates: %v found=%v", err, found)
	}
	for _, tk := range final.Tasks {
		if tk.Status != StatusDone {
			t.Errorf("task %s is %q: a concurrent update was lost", tk.ID, tk.Status)
		}
	}
}

// TestUpdatePropagatesMutateErrorWithoutWriting confirms a failing mutate
// leaves the stored assignments untouched, so a bug in the caller's mutate
// function cannot corrupt state that a concurrent reader might observe.
func TestUpdatePropagatesMutateErrorWithoutWriting(t *testing.T) {
	root := t.TempDir()
	var a Assignments
	a.Tasks = append(a.Tasks, Task{ID: "t00", Status: StatusPending})
	if err := a.Save(root); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("mutate failed")
	err := Update(root, func(a *Assignments) error {
		a.Tasks[0].Status = StatusDone
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Update err = %v, want %v", err, wantErr)
	}

	final, found, err := LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("load after failed update: %v found=%v", err, found)
	}
	if final.Tasks[0].Status != StatusPending {
		t.Errorf("status = %q after failed mutate, want unchanged %q", final.Tasks[0].Status, StatusPending)
	}
}

// TestUpdateOnMissingAssignmentsIsAClearError confirms Update against a
// project with no assignments.json yet fails loudly rather than silently
// writing an empty file, which would look indistinguishable from a real but
// empty plan.
func TestUpdateOnMissingAssignmentsIsAClearError(t *testing.T) {
	root := t.TempDir()
	err := Update(root, func(a *Assignments) error {
		return nil
	})
	if err == nil {
		t.Fatal("Update against a missing assignments.json returned nil error, want a clear failure")
	}
}

// Save must not clobber a concurrent Update. An atomic write alone prevents a
// torn file, not a lost update: without a shared lock, Save's write can land
// in the middle of Update's read-modify-write window, and the write Update
// then makes from its already-stale read erases it. This pins the fix so a
// later refactor cannot quietly drop the lock from Save.
//
// The interleaving is forced rather than raced for, because racing for it is
// exactly what the previous version of this test failed to do: an Update's
// mutate callback is held open, so the Update provably holds the lock and has
// provably already read the file, and the Save runs against it in that state.
// With the lock, Save blocks until the Update commits and its snapshot
// survives as the final state. Without it, Save writes immediately and the
// Update's stale write wipes it out.
func TestSaveDoesNotClobberConcurrentUpdate(t *testing.T) {
	root := t.TempDir()
	base := Assignments{Tasks: []Task{{ID: "t00", Status: StatusPending}}}
	if err := base.Save(root); err != nil {
		t.Fatal(err)
	}

	holdingLock := make(chan struct{})
	releaseMutate := make(chan struct{})

	updateDone := make(chan error, 1)
	go func() {
		updateDone <- Update(root, func(a *Assignments) error {
			// Update has taken the lock and read the file by the time mutate
			// runs, so everything after this signal is inside its window.
			close(holdingLock)
			<-releaseMutate
			a.Tasks[0].Status = StatusDone
			return nil
		})
	}()
	<-holdingLock

	// The Save's snapshot deliberately carries a marker the Update's stale read
	// cannot contain, so a lost Save is unambiguous in the final file.
	saveDone := make(chan error, 1)
	go func() {
		saveDone <- Assignments{Tasks: []Task{{ID: "t00", Status: StatusCorrected, Title: "from Save"}}}.Save(root)
	}()

	// Give the unlocked Save every chance to commit before the Update writes.
	// A locked Save is still blocked on the lock here and commits afterwards.
	time.Sleep(250 * time.Millisecond)
	close(releaseMutate)

	if err := <-updateDone; err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := <-saveDone; err != nil {
		t.Fatalf("Save: %v", err)
	}

	final, found, err := LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("load: %v found=%v", err, found)
	}
	if len(final.Tasks) != 1 {
		t.Fatalf("final tasks = %+v, want exactly 1", final.Tasks)
	}
	if final.Tasks[0].Title != "from Save" {
		t.Errorf("final task = %+v: the Save landed inside the Update's window and was lost", final.Tasks[0])
	}
}

// A locked Save must still complete rather than deadlocking against itself.
func TestSaveIsNotReentrantDeadlocked(t *testing.T) {
	root := t.TempDir()
	a := Assignments{Tasks: []Task{{ID: "t1", Status: StatusPending}}}
	done := make(chan error, 1)
	go func() { done <- a.Save(root) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Save did not return within 10s: the lock is likely reentrant-deadlocked")
	}
}
