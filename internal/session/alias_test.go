package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetAndResolveAlias(t *testing.T) {
	dir := t.TempDir()
	// Back the alias with a real session file so it can be resolved+used.
	writeSession(t, dir, "3f2a1b", `{"role":"system","content":"s"}`+"\n")

	if err := setAliasIn(dir, "my-refactor", "3f2a1b"); err != nil {
		t.Fatal(err)
	}
	if got := resolveIn(dir, "my-refactor"); got != "3f2a1b" {
		t.Errorf("alias resolved to %q, want 3f2a1b", got)
	}
	// A non-alias value passes through unchanged (it may be a real id).
	if got := resolveIn(dir, "3f2a1b"); got != "3f2a1b" {
		t.Errorf("plain id changed: %q", got)
	}
	if got := resolveIn(dir, "unknown"); got != "unknown" {
		t.Errorf("unknown value should pass through: %q", got)
	}
}

func TestAliasPersistsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "id1", `{"role":"system","content":"s"}`+"\n")
	writeSession(t, dir, "id2", `{"role":"system","content":"s"}`+"\n")
	setAliasIn(dir, "a", "id1")
	setAliasIn(dir, "b", "id2")

	if resolveIn(dir, "a") != "id1" || resolveIn(dir, "b") != "id2" {
		t.Error("aliases did not persist independently")
	}
	// Overwriting an alias updates it.
	setAliasIn(dir, "a", "id2")
	if resolveIn(dir, "a") != "id2" {
		t.Error("alias overwrite did not take")
	}
}

func TestSetAliasValidatesID(t *testing.T) {
	dir := t.TempDir()
	if err := setAliasIn(dir, "ok", "../escape"); err == nil {
		t.Error("invalid target id must be rejected")
	}
	if err := setAliasIn(dir, "", "id"); err == nil {
		t.Error("empty alias name must be rejected")
	}
}

func TestResolveMissingAliasesFile(t *testing.T) {
	// No aliases.json yet: resolve returns the input unchanged, no error/panic.
	if got := resolveIn(filepath.Join(t.TempDir(), "empty"), "x"); got != "x" {
		t.Errorf("got %q", got)
	}
	_ = os.Remove // keep os import
}
