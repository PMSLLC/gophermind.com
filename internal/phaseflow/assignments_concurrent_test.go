package phaseflow

import (
	"errors"
	"fmt"
	"sync"
	"testing"
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
