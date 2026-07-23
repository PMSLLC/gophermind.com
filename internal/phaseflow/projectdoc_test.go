package phaseflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readProjectDoc(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, ProjectDocName))
	if err != nil {
		t.Fatalf("read %s: %v", ProjectDocName, err)
	}
	return string(b)
}

// TestUpsertCreatesFileWhenAbsent: a fresh project gets a PROJECT.md holding
// just the managed block.
func TestUpsertCreatesFileWhenAbsent(t *testing.T) {
	root := t.TempDir()
	if err := UpsertProjectDoc(root, "BODY-ONE"); err != nil {
		t.Fatalf("UpsertProjectDoc: %v", err)
	}
	got := readProjectDoc(t, root)
	if !strings.Contains(got, "BODY-ONE") {
		t.Errorf("body missing: %q", got)
	}
	if !strings.Contains(got, specBeginMarker) || !strings.Contains(got, specEndMarker) {
		t.Errorf("markers missing: %q", got)
	}
}

// TestUpsertAppendsWhenMarkersAbsent is the case that protects hand-written
// conventions: an existing PROJECT.md with no markers keeps every byte and gains
// the block at the end.
func TestUpsertAppendsWhenMarkersAbsent(t *testing.T) {
	root := t.TempDir()
	const existing = "# PROJECT.md — myrepo\n\nHand-written conventions.\n"
	if err := os.WriteFile(filepath.Join(root, ProjectDocName), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpsertProjectDoc(root, "GENERATED"); err != nil {
		t.Fatalf("UpsertProjectDoc: %v", err)
	}
	got := readProjectDoc(t, root)
	if !strings.HasPrefix(got, existing) {
		t.Errorf("existing content was not preserved verbatim at the top:\n%q", got)
	}
	if !strings.Contains(got, "GENERATED") {
		t.Errorf("generated block missing: %q", got)
	}
}

// TestUpsertReplacesOnlyBetweenMarkers: regeneration must not touch anything
// outside the block, before or after it.
func TestUpsertReplacesOnlyBetweenMarkers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectDocName), []byte(
		"TOP MATTER\n"+specBeginMarker+"\nOLD BODY\n"+specEndMarker+"\nBOTTOM MATTER\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpsertProjectDoc(root, "NEW BODY"); err != nil {
		t.Fatalf("UpsertProjectDoc: %v", err)
	}
	got := readProjectDoc(t, root)
	if strings.Contains(got, "OLD BODY") {
		t.Errorf("old body survived: %q", got)
	}
	for _, keep := range []string{"TOP MATTER", "BOTTOM MATTER", "NEW BODY"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q missing from result: %q", keep, got)
		}
	}
	if strings.Index(got, "TOP MATTER") > strings.Index(got, "NEW BODY") {
		t.Error("top matter moved below the block")
	}
	if strings.Index(got, "BOTTOM MATTER") < strings.Index(got, "NEW BODY") {
		t.Error("bottom matter moved above the block")
	}
}

// TestUpsertIsIdempotent: regenerating the same body twice must not stack
// duplicate blocks.
func TestUpsertIsIdempotent(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := UpsertProjectDoc(root, "SAME"); err != nil {
			t.Fatalf("UpsertProjectDoc %d: %v", i, err)
		}
	}
	got := readProjectDoc(t, root)
	if n := strings.Count(got, specBeginMarker); n != 1 {
		t.Errorf("begin markers = %d, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, "SAME"); n != 1 {
		t.Errorf("body copies = %d, want 1:\n%s", n, got)
	}
}

// TestRenderProjectDocBody covers the rendered view: the spec summary, each
// phase, and every task with its live status.
func TestRenderProjectDocBody(t *testing.T) {
	a := &Assignments{Tasks: []Task{
		{ID: "01-01", Phase: "1", Title: "Scaffold the store", Status: StatusDone},
		{ID: "01-02", Phase: "1", Title: "Wire the handler", Status: StatusPending},
		{ID: "02-01", Phase: "2", Title: "Add the CLI", Status: StatusFailed},
	}}
	body := RenderProjectDocBody("myproj", "A short spec overview.", a)

	for _, want := range []string{"myproj", "A short spec overview.", "01-01", "Scaffold the store", "02-01", "Add the CLI"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, StatusDone) || !strings.Contains(body, StatusPending) {
		t.Errorf("statuses missing:\n%s", body)
	}
	// Phases must be grouped, not one flat list repeated per task.
	if n := strings.Count(body, "Phase 1"); n != 1 {
		t.Errorf("Phase 1 heading count = %d, want 1:\n%s", n, body)
	}
}
