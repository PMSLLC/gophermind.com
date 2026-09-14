package phaseflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// legacyAssignmentsJSON is a hand-written JSON literal in the pre-pipeline
// on-disk shape: no wave, depends_on, candidate_models, attempts or
// revision_rounds keys anywhere. It is a literal rather than a marshalled
// struct on purpose, so this test keeps exercising the actual old shape even
// after Task grows further - the same technique the odometer's compat test
// uses to protect its on-disk format.
const legacyAssignmentsJSON = `{
  "tasks": [
    {
      "id": "01-01",
      "phase": "setup",
      "title": "Scaffold project",
      "description": "Create the initial layout",
      "acceptance_criteria": ["go build succeeds"],
      "agent": "builder",
      "model": "strong",
      "status": "done"
    },
    {
      "id": "01-02",
      "phase": "setup",
      "title": "Add CI",
      "description": "Wire up the test workflow",
      "acceptance_criteria": ["CI runs on push"],
      "agent": "builder",
      "model": "speed",
      "status": "pending"
    }
  ]
}`

// TestLegacyAssignmentsLoadAndRoundTripWithoutNewKeys confirms a
// pre-pipeline assignments.json loads correctly and, once re-marshalled,
// gains none of the new fields - proving they are all omitempty and a plan
// written before this change keeps working unchanged.
func TestLegacyAssignmentsLoadAndRoundTripWithoutNewKeys(t *testing.T) {
	var a Assignments
	if err := json.Unmarshal([]byte(legacyAssignmentsJSON), &a); err != nil {
		t.Fatalf("unmarshal legacy assignments.json: %v", err)
	}
	if len(a.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(a.Tasks))
	}
	if a.Tasks[0].ID != "01-01" || a.Tasks[0].Status != StatusDone {
		t.Errorf("task 0 = %+v, want id 01-01 status done", a.Tasks[0])
	}
	for _, tk := range a.Tasks {
		if tk.Wave != 0 || tk.DependsOn != nil || tk.CandidateModels != nil ||
			tk.Attempts != nil || tk.RevisionRounds != nil {
			t.Errorf("task %s: new fields not zero-valued on legacy load: %+v", tk.ID, tk)
		}
	}

	out, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"wave"`, `"depends_on"`, `"candidate_models"`, `"attempts"`, `"revision_rounds"`,
	} {
		if strings.Contains(string(out), key) {
			t.Errorf("round-tripped output contains %s, want it absent (omitempty)", key)
		}
	}
}

// TestRecordAttemptPreservesOrder confirms attempts accumulate in the order
// they were recorded.
func TestRecordAttemptPreservesOrder(t *testing.T) {
	var tk Task
	for i := 0; i < 3; i++ {
		tk.RecordAttempt(Attempt{Model: modelName(i), Verdict: "fail"})
	}
	if len(tk.Attempts) != 3 {
		t.Fatalf("got %d attempts, want 3", len(tk.Attempts))
	}
	for i, a := range tk.Attempts {
		if a.Model != modelName(i) {
			t.Errorf("attempt %d model = %q, want %q: order not preserved", i, a.Model, modelName(i))
		}
	}
}

// TestRecordAttemptCapsAndDropsOldest confirms the history is bounded at
// maxAttemptsPerTask and keeps the newest entries when it overflows.
func TestRecordAttemptCapsAndDropsOldest(t *testing.T) {
	var tk Task
	total := maxAttemptsPerTask + 10
	for i := 0; i < total; i++ {
		tk.RecordAttempt(Attempt{Model: modelName(i)})
	}
	if len(tk.Attempts) != maxAttemptsPerTask {
		t.Fatalf("got %d attempts, want cap of %d", len(tk.Attempts), maxAttemptsPerTask)
	}
	if got, want := tk.Attempts[0].Model, modelName(total-maxAttemptsPerTask); got != want {
		t.Errorf("oldest kept attempt = %q, want %q: cap should drop the oldest first", got, want)
	}
	if got, want := tk.Attempts[len(tk.Attempts)-1].Model, modelName(total-1); got != want {
		t.Errorf("newest attempt = %q, want %q", got, want)
	}
}

func modelName(i int) string {
	return fmt.Sprintf("model-%d", i)
}

// TestNewStatusValuesExist confirms the two new status constants exist and
// are distinct from every existing status.
func TestNewStatusValuesExist(t *testing.T) {
	if StatusNeedsRevision == "" {
		t.Error("StatusNeedsRevision is empty")
	}
	if StatusEscalated == "" {
		t.Error("StatusEscalated is empty")
	}
	seen := map[string]bool{}
	for _, s := range []string{
		StatusPending, StatusRunning, StatusDone, StatusFailed, StatusCorrected,
		StatusNeedsRevision, StatusEscalated,
	} {
		if seen[s] {
			t.Errorf("status value %q is not unique", s)
		}
		seen[s] = true
	}
}
