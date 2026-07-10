package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGCDir(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "fresh", `{"role":"system","content":"x"}`+"\n")
	writeSession(t, dir, "stale", `{"role":"system","content":"x"}`+"\n")

	now := time.Now()
	// Age the stale session to 40 days old.
	old := now.Add(-40 * 24 * time.Hour)
	os.Chtimes(filepath.Join(dir, "stale.jsonl"), old, old)

	removed, err := gcDir(dir, 30*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "stale" {
		t.Fatalf("removed = %v, want [stale]", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.jsonl")); !os.IsNotExist(err) {
		t.Error("stale session should be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "fresh.jsonl")); err != nil {
		t.Error("fresh session should survive")
	}
}

func TestGCDirNothingStale(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "a", `{"role":"system","content":"x"}`+"\n")
	removed, err := gcDir(dir, time.Hour, time.Now())
	if err != nil || len(removed) != 0 {
		t.Errorf("removed = %v err = %v, want none", removed, err)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	store := t.TempDir()
	writeSession(t, store, "orig", `{"role":"system","content":"s"}`+"\n"+`{"role":"user","content":"hi"}`+"\n")

	out := filepath.Join(t.TempDir(), "dump.jsonl")
	if err := exportIn(store, "orig", out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 {
		t.Fatalf("export produced no file: %v", err)
	}

	// Import into a fresh store under a new id.
	store2 := t.TempDir()
	if err := importIn(store2, out, "copied"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(store2, "copied.jsonl"))
	if err != nil {
		t.Fatalf("imported session missing: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("round-trip mismatch:\n got %q\nwant %q", got, data)
	}
}

func TestImportValidatesIDAndFile(t *testing.T) {
	store := t.TempDir()
	good := filepath.Join(t.TempDir(), "g.jsonl")
	os.WriteFile(good, []byte(`{"role":"system","content":"s"}`+"\n"), 0o600)

	if err := importIn(store, good, "../escape"); err == nil {
		t.Error("path-escape id must be rejected")
	}
	if err := importIn(store, filepath.Join(t.TempDir(), "missing.jsonl"), "ok"); err == nil {
		t.Error("missing source file must error")
	}
	// A file whose first line isn't a JSON object is rejected.
	bad := filepath.Join(t.TempDir(), "b.jsonl")
	os.WriteFile(bad, []byte("not json\n"), 0o600)
	if err := importIn(store, bad, "ok"); err == nil {
		t.Error("non-JSONL file must be rejected")
	}
}

func TestExportMissingSession(t *testing.T) {
	if err := exportIn(t.TempDir(), "nope", filepath.Join(t.TempDir(), "o.jsonl")); err == nil {
		t.Error("exporting a missing session must error")
	}
}
