package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gophermind/gophermind-lib/briefv2/report"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestStateRoundTripAndNotes(t *testing.T) {
	runDir := t.TempDir()

	// A missing file is a fresh run, with usable maps.
	s, err := LoadState(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if s.StartedAt != "" || s.Wave0Done || s.Resumed {
		t.Errorf("fresh state = %+v, want zero values", s)
	}
	if s.PlanHashes == nil || s.ExtraRevisions == nil || s.CriticalStreak == nil || s.RedChecked == nil || s.ExtraRepair == nil {
		t.Errorf("fresh state maps are not initialized: %+v", s)
	}
	s.ExtraRevisions["x"] = 1 // must not panic on a nil map

	// Round trip.
	s.PlanHashes = map[string]string{"contracts.json": "abc"}
	s.StartedAt = "2026-09-30T10:00:00Z"
	s.Branch = "gm/gm-2026-09-30-901"
	s.Wave0Done, s.Resumed = true, true
	s.ExtraRevisions = map[string]int{"fn-greet": 2}
	s.CriticalStreak = map[string]int{"fn-serve": 1}
	s.RedChecked = map[string]bool{"fn-bye": true}
	s.ExtraRepair = map[string]int{"1": 3}
	if err := s.Save(runDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "_state", "executor.json")
	if got := mode(t, path); got != 0o600 {
		t.Errorf("executor.json mode = %o, want 600", got)
	}
	if got := mode(t, filepath.Join(runDir, "_state")); got != 0o700 {
		t.Errorf("_state mode = %o, want 700", got)
	}
	back, err := LoadState(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, s) {
		t.Errorf("round trip = %+v, want %+v", back, s)
	}
	if entries, _ := os.ReadDir(filepath.Join(runDir, "_state")); len(entries) != 1 {
		t.Errorf("_state holds %d entries after Save, want only executor.json (no temp file left)", len(entries))
	}

	// Notes: appended, only the last 5 per node kept, file mode 0600.
	for i := 1; i <= 7; i++ {
		if err := AddNote(runDir, "fn-greet", fmt.Sprintf("note %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddNote(runDir, "fn-bye", "only"); err != nil {
		t.Fatal(err)
	}
	notes, err := LoadNotes(runDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"note 3", "note 4", "note 5", "note 6", "note 7"}
	if !reflect.DeepEqual(notes["fn-greet"], want) {
		t.Errorf("notes = %v, want %v", notes["fn-greet"], want)
	}
	if !reflect.DeepEqual(notes["fn-bye"], []string{"only"}) {
		t.Errorf("fn-bye notes = %v", notes["fn-bye"])
	}
	if got := mode(t, filepath.Join(runDir, "_state", "notes.json")); got != 0o600 {
		t.Errorf("notes.json mode = %o, want 600", got)
	}
	none, err := LoadNotes(t.TempDir())
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("LoadNotes on a missing file = %v, %v; want an empty map", none, err)
	}

	// Escalations append in order.
	empty, err := LoadEscalations(runDir)
	if err != nil || len(empty) != 0 {
		t.Fatalf("LoadEscalations before any = %v, %v", empty, err)
	}
	e1 := report.Escalation{Kind: "leaf", TaskType: "implement", Model: "a/m1", NodeID: "fn-greet"}
	e2 := report.Escalation{Kind: "leaf", TaskType: "implement", Model: "b/m2", NodeID: "fn-bye"}
	for _, e := range []report.Escalation{e1, e2} {
		if err := AppendEscalation(runDir, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadEscalations(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []report.Escalation{e1, e2}) {
		t.Errorf("escalations = %+v", got)
	}
	if m := mode(t, filepath.Join(runDir, "_state", "escalations.json")); m != 0o600 {
		t.Errorf("escalations.json mode = %o, want 600", m)
	}
}

// TestStateRefusesUnreadableFile: a damaged state file is an error that does
// not quote its content, never a silent fresh start.
func TestStateRefusesUnreadableFile(t *testing.T) {
	runDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(runDir, "_state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "_state", "executor.json"), []byte(`{"started_at": CANARY-SECRET-VALUE`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadState(runDir)
	if err == nil {
		t.Fatal("LoadState accepted a damaged file")
	}
	if containsCanary(err.Error()) {
		t.Errorf("the error quotes the file: %v", err)
	}
}
