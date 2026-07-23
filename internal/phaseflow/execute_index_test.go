package phaseflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gophermind/internal/codeindex"
)

// TestExecuteRefreshesIndexAfterEachTask: an autonomous run edits code as it
// goes, so the symbol index the agent searches must be rebuilt per task rather
// than reflecting the tree as it was when the run started.
func TestExecuteRefreshesIndexAfterEachTask(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	// A runner that adds a new symbol, standing in for a task that writes code.
	r := &funcRunner{fn: func(ctx context.Context, task Task) (string, string, error) {
		src := "package sample\n\n// AddedByTask is written while the run is in flight.\nfunc AddedByTask() {}\n"
		if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte(src), 0o644); err != nil {
			return StatusFailed, err.Error(), nil
		}
		return StatusDone, "", nil
	}}

	if _, err := Execute(context.Background(), root, r, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, codeindex.FileName))
	if err != nil {
		t.Fatalf("index not written: %v", err)
	}
	if !contains(string(b), "AddedByTask") {
		t.Errorf("index does not reflect the code the task wrote:\n%s", b)
	}
}

// TestIndexFailureDoesNotFailTheTask: the index is a convenience, never a gate.
func TestIndexFailureDoesNotFailTheTask(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"))

	// A directory named INDEX.md makes the write fail without affecting the task.
	if err := os.MkdirAll(filepath.Join(root, codeindex.FileName), 0o755); err != nil {
		t.Fatal(err)
	}

	r := &funcRunner{fn: func(context.Context, Task) (string, string, error) {
		return StatusDone, "", nil
	}}

	sum, err := Execute(context.Background(), root, r, nil)
	if err != nil {
		t.Fatalf("Execute returned an error because the index could not be written: %v", err)
	}
	if sum.Done != 1 {
		t.Errorf("Done = %d, want 1; a failed index write must not fail the task", sum.Done)
	}
}

type funcRunner struct {
	fn func(ctx context.Context, t Task) (string, string, error)
}

func (f *funcRunner) Run(ctx context.Context, t Task) (string, string, error) { return f.fn(ctx, t) }
