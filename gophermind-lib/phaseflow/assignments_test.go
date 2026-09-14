package phaseflow

import (
	"os"
	"testing"
)

func TestAssignmentsRoundTrip(t *testing.T) {
	root := t.TempDir()
	in := Assignments{Tasks: []Task{
		{
			ID: "01-01", Phase: "1", Title: "Scaffold", Description: "set up",
			AcceptanceCriteria: []string{"builds", "tests pass"},
			Agent:              "coder", AgentAddendum: "use Go", Model: "strong", Status: StatusPending,
		},
		{
			ID: "01-02", Phase: "1", Title: "Docs", Description: "write docs",
			AcceptanceCriteria: []string{"README updated"},
			Agent:              "docs", Model: "speed", Status: StatusPending,
		},
	}}
	if err := in.Save(root); err != nil {
		t.Fatalf("save: %v", err)
	}

	out, found, err := LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("load: %v found=%v", err, found)
	}
	if len(out.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(out.Tasks))
	}
	tk, ok := out.Task("01-01")
	if !ok {
		t.Fatal("task 01-01 missing")
	}
	if tk.Agent != "coder" || tk.Model != "strong" || len(tk.AcceptanceCriteria) != 2 {
		t.Errorf("task 01-01 round-tripped wrong: %+v", tk)
	}
	// omitempty: the docs task had no addendum.
	if d, _ := out.Task("01-02"); d.AgentAddendum != "" {
		t.Errorf("expected empty addendum, got %q", d.AgentAddendum)
	}
}

func TestLoadAssignmentsMissing(t *testing.T) {
	a, found, err := LoadAssignments(t.TempDir())
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if found {
		t.Error("found should be false for a missing file")
	}
	if len(a.Tasks) != 0 {
		t.Error("missing file should yield no tasks")
	}
}

func TestLoadAssignmentsInvalidJSON(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(PlanningDir(root), 0o755)
	_ = os.WriteFile(AssignmentsPath(root), []byte("{not json"), 0o644)
	if _, _, err := LoadAssignments(root); err == nil {
		t.Error("expected an error for malformed json")
	}
}
