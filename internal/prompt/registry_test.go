package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrySaveListShow(t *testing.T) {
	dir := t.TempDir()
	if err := saveNamedIn(dir, "reviewer", "You are a reviewer."); err != nil {
		t.Fatal(err)
	}
	if err := saveNamedIn(dir, "planner", "You are a planner."); err != nil {
		t.Fatal(err)
	}

	names, err := listNamedIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "planner" || names[1] != "reviewer" {
		t.Errorf("expected sorted [planner reviewer], got %v", names)
	}

	body, err := showNamedIn(dir, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "reviewer") {
		t.Errorf("show returned wrong content: %q", body)
	}
}

func TestRegistrySaveVersions(t *testing.T) {
	dir := t.TempDir()
	// Saving the same name twice keeps a timestamped backup, so rollback is possible.
	if err := saveNamedIn(dir, "p", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := saveNamedIn(dir, "p", "v2"); err != nil {
		t.Fatal(err)
	}
	body, _ := showNamedIn(dir, "p")
	if body != "v2" {
		t.Errorf("current should be v2, got %q", body)
	}
	// A backup of v1 should exist.
	backups, err := filepath.Glob(filepath.Join(dir, "p.*.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Error("expected a versioned backup of the previous prompt")
	}
	data, _ := os.ReadFile(backups[0])
	if string(data) != "v1" {
		t.Errorf("backup content = %q, want v1", data)
	}
}

func TestRegistryBadName(t *testing.T) {
	if err := saveNamedIn(t.TempDir(), "../evil", "x"); err == nil {
		t.Error("bad prompt name should be rejected")
	}
}

func TestRegistryShowMissing(t *testing.T) {
	if _, err := showNamedIn(t.TempDir(), "nope"); err == nil {
		t.Error("missing prompt should error")
	}
}
