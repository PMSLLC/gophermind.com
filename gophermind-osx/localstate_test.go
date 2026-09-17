package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// decodeLine/encodeLine are a minimal string codec for exercising
// loadState/saveState without depending on any of this package's real
// state types (PanelState, WindowState, ...) -- those get their own
// coverage through panelstate_test.go etc.; this file only needs to prove
// the shared open/create/best-effort-write plumbing itself.
func decodeLine(r io.Reader) (string, error) {
	s := bufio.NewScanner(r)
	if !s.Scan() {
		return "", s.Err()
	}
	return s.Text(), nil
}

func encodeLine(line string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := fmt.Fprintln(w, line)
		return err
	}
}

func TestLoadState_MissingFileReturnsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.txt")
	got := loadState(path, decodeLine, "default")
	if got != "default" {
		t.Errorf("loadState on a missing file = %q, want the zero value %q", got, "default")
	}
}

func TestSaveState_ThenLoadState_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.txt")

	saveState(path, encodeLine("hello"))

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saveState did not create the file: %v", err)
	}
	got := loadState(path, decodeLine, "default")
	if got != "hello" {
		t.Errorf("loadState after saveState = %q, want %q", got, "hello")
	}
}

func TestSaveState_CreatesParentDirectoryPrivately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "state.txt")

	saveState(path, encodeLine("x"))

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("parent directory not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("parent directory mode = %o, want 0700 (this directory can hold backends.json, which is user-private config)", perm)
	}
}

// TestMigrateOSXStateBetween_MovesFilesToNewDir covers the one-time move
// from gophermind-osx's pre-consolidation location (the OS's own per-app
// config directory) to the new one under gophermind-lib/config.Dir(), so
// upgrading the app doesn't silently reset an existing user's window
// position, panel state, and cache/history preferences back to defaults.
func TestMigrateOSXStateBetween_MovesFilesToNewDir(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "old")
	newDir := filepath.Join(t.TempDir(), "new")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "panel-state.json"), []byte(`{"collapsed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateOSXStateBetween(oldDir, newDir)

	got, err := os.ReadFile(filepath.Join(newDir, "panel-state.json"))
	if err != nil {
		t.Fatalf("panel-state.json was not moved to the new directory: %v", err)
	}
	if string(got) != `{"collapsed":true}` {
		t.Errorf("moved file content = %q, want unchanged", got)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "panel-state.json")); !os.IsNotExist(err) {
		t.Error("panel-state.json should no longer exist at the old location")
	}
}

// TestMigrateOSXStateBetween_NeverOverwritesExistingDestination covers
// the safety property: if the new location somehow already has its own
// file (e.g. a second launch after a partial migration, or the user
// already ran the new version once), migration must not clobber it with
// stale data from the old location.
func TestMigrateOSXStateBetween_NeverOverwritesExistingDestination(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "old")
	newDir := filepath.Join(t.TempDir(), "new")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "window-state.json"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "window-state.json"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateOSXStateBetween(oldDir, newDir)

	got, err := os.ReadFile(filepath.Join(newDir, "window-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("existing destination file was overwritten: got %q, want %q (unchanged)", got, "new")
	}
}

// TestMigrateOSXStateBetween_MissingOldDirIsNotAnError covers the common
// case (a fresh install, or a machine that never ran the pre-migration
// layout): nothing to move, and this must not panic or create anything.
func TestMigrateOSXStateBetween_MissingOldDirIsNotAnError(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "does-not-exist")
	newDir := filepath.Join(t.TempDir(), "new")

	migrateOSXStateBetween(oldDir, newDir) // must not panic

	if _, err := os.Stat(newDir); err == nil {
		t.Error("migrateOSXStateBetween created the new directory when there was nothing to migrate")
	}
}

func TestOSXStateDir_IsAnOSXSubdirectoryOfConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOPHERMIND_CONFIG_DIR", "")

	dir, err := osxStateDir()
	if err != nil {
		t.Fatalf("osxStateDir: %v", err)
	}
	want := filepath.Join(home, ".gophermind", "osx")
	if dir != want {
		t.Errorf("osxStateDir() = %q, want %q", dir, want)
	}
}
