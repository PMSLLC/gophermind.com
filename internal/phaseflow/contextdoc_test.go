package phaseflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestContextDocRecordsProgress: the doc is what a cleared task context reads
// to learn where the run is, so it must carry the last outcome and what is next.
func TestContextDocRecordsProgress(t *testing.T) {
	a := &Assignments{Tasks: []Task{
		{ID: "01-01", Phase: "1", Title: "Scaffold the store", Status: StatusDone},
		{ID: "01-02", Phase: "1", Title: "Wire the handler", Status: StatusFailed},
		{ID: "02-01", Phase: "2", Title: "Add the CLI", Status: StatusPending},
	}}
	body := RenderContextDocBody("myproj", a, TaskOutcome{ID: "01-02", Status: StatusFailed, Detail: "compile error"})

	for _, want := range []string{"myproj", "01-01", "01-02", "02-01", "compile error"} {
		if !strings.Contains(body, want) {
			t.Errorf("context doc missing %q:\n%s", want, body)
		}
	}
	// The next pending task must be called out, not just listed.
	if !strings.Contains(body, "Next") {
		t.Errorf("context doc does not identify what runs next:\n%s", body)
	}
}

// TestContextDocCountsRemaining gives a cleared context a sense of scale.
func TestContextDocCountsRemaining(t *testing.T) {
	a := &Assignments{Tasks: []Task{
		{ID: "01-01", Status: StatusDone}, {ID: "01-02", Status: StatusPending},
		{ID: "01-03", Status: StatusPending},
	}}
	body := RenderContextDocBody("p", a, TaskOutcome{})
	if !strings.Contains(body, "2 pending") {
		t.Errorf("pending count missing:\n%s", body)
	}
}

// TestUpsertContextDocPreservesHandwrittenContent: CONTEXT.md is a
// human-authored handoff file; the generated block must not eat it.
func TestUpsertContextDocPreservesHandwrittenContent(t *testing.T) {
	root := t.TempDir()
	const existing = "# Context Handoff\n\nMy own notes about the work.\n"
	if err := os.WriteFile(filepath.Join(root, ContextDocName), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpsertContextDoc(root, "GENERATED STATE"); err != nil {
		t.Fatalf("UpsertContextDoc: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, ContextDocName))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, existing) {
		t.Errorf("hand-written content was not preserved:\n%s", got)
	}
	if !strings.Contains(got, "GENERATED STATE") {
		t.Errorf("generated block missing:\n%s", got)
	}
}

// TestUpsertContextDocIsIdempotent keeps per-task rewrites from stacking.
func TestUpsertContextDocIsIdempotent(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := UpsertContextDoc(root, "STATE"); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, ContextDocName))
	if n := strings.Count(string(b), contextBeginMarker); n != 1 {
		t.Errorf("begin markers = %d, want 1", n)
	}
}

// TestExecuteWritesContextDocPerTask is the behavior asked for: state is
// refreshed between steps so the next cleared context can read it. The check
// runs from inside the runner, i.e. while the second task is starting, so it
// proves the doc is current mid-run rather than only at the end.
func TestExecuteWritesContextDocPerTask(t *testing.T) {
	root := t.TempDir()
	writeAssignments(t, root, pendingTask("01-01"), pendingTask("01-02"))

	var sawFirstTaskRecorded bool
	r := &funcRunner{fn: func(ctx context.Context, task Task) (string, string, error) {
		if task.ID == "01-02" {
			b, err := os.ReadFile(filepath.Join(root, ContextDocName))
			if err == nil && strings.Contains(string(b), "01-01") && strings.Contains(string(b), StatusDone) {
				sawFirstTaskRecorded = true
			}
		}
		return StatusDone, "", nil
	}}

	if _, err := Execute(context.Background(), root, r, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !sawFirstTaskRecorded {
		t.Error("CONTEXT.md did not reflect task 01-01 by the time 01-02 started")
	}
}
