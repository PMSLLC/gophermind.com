package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ageFile(t *testing.T, dir, id string, age time.Duration) {
	t.Helper()
	p := filepath.Join(dir, id+".jsonl")
	old := time.Now().Add(-age)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestGCProtectingKeepsTaggedSessions(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "scratch", `{"role":"user","content":"x"}`)
	writeSessionLines(t, dir, "keeper", `{"role":"user","content":"y"}`)
	// Both are old enough to expire.
	ageFile(t, dir, "scratch", 48*time.Hour)
	ageFile(t, dir, "keeper", 48*time.Hour)
	// Protect "keeper" via a tag.
	if err := setTagsIn(dir, "keeper", []string{"important"}); err != nil {
		t.Fatal(err)
	}

	removed, err := gcProtectingIn(dir, 24*time.Hour, []string{"important"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "scratch" {
		t.Errorf("expected only 'scratch' removed, got %v", removed)
	}
	if !fileExists(filepath.Join(dir, "keeper.jsonl")) {
		t.Error("protected 'keeper' session was deleted")
	}
	if fileExists(filepath.Join(dir, "scratch.jsonl")) {
		t.Error("'scratch' session should have been removed")
	}
}

func TestGCProtectingNoProtectedTags(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "old", `{"role":"user","content":"x"}`)
	ageFile(t, dir, "old", 48*time.Hour)
	removed, err := gcProtectingIn(dir, 24*time.Hour, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Errorf("with no protected tags, old session should be removed: %v", removed)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
