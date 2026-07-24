package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleIndexCommandWritesIndex: /index rebuilds INDEX.md and reports the
// symbol count.
func TestHandleIndexCommandWritesIndex(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// A tiny Go file so the index has a symbol to find.
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n\n// F does a thing.\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := testModel(t)
	m = m.handleIndexCommand()

	if !strings.Contains(m.content, "index: wrote INDEX.md") {
		t.Errorf("no success line in output:\n%s", m.content)
	}
	if _, err := os.Stat(filepath.Join(dir, "INDEX.md")); err != nil {
		t.Errorf("INDEX.md not written: %v", err)
	}
}

// TestHandleOptimizeCommandWritesEnv: /optimize with a valid profile writes .env
// and reports the profile applied.
func TestHandleOptimizeCommandWritesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := testModel(t)
	m = m.handleOptimizeCommand("/optimize aggressive")

	if !strings.Contains(m.content, "aggressive") {
		t.Errorf("profile name not reported:\n%s", m.content)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf(".env not written: %v", err)
	}
	if !strings.Contains(string(b), "GOPHERMIND_MAX_ITER=60") {
		t.Errorf(".env missing the aggressive tuning:\n%s", b)
	}
}

// TestHandleOptimizeDefaultsToAggressive: a bare /optimize picks a profile
// rather than erroring.
func TestHandleOptimizeDefaultsToAggressive(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := testModel(t)
	m = m.handleOptimizeCommand("/optimize")

	if !strings.Contains(m.content, "aggressive") {
		t.Errorf("bare /optimize did not default to aggressive:\n%s", m.content)
	}
}

// TestHandleOptimizeUnknownProfileListsChoices: a typo must not write anything;
// it should show the valid profiles.
func TestHandleOptimizeUnknownProfileListsChoices(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := testModel(t)
	m = m.handleOptimizeCommand("/optimize turbo")

	if !strings.Contains(m.content, "unknown profile") {
		t.Errorf("unknown profile not reported:\n%s", m.content)
	}
	if !strings.Contains(m.content, "safe") || !strings.Contains(m.content, "unattended") {
		t.Errorf("valid profiles not listed:\n%s", m.content)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Error(".env was written for an invalid profile")
	}
}

// TestHandleOptimizeUnattendedWarns: the profile that removes the approval gate
// must say so.
func TestHandleOptimizeUnattendedWarns(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := testModel(t)
	m = m.handleOptimizeCommand("/optimize unattended")

	if !strings.Contains(m.content, "APPROVAL=auto") {
		t.Errorf("unattended profile did not warn about auto-approval:\n%s", m.content)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(b), "GOPHERMIND_APPROVAL=auto") {
		t.Errorf(".env missing auto-approval for unattended:\n%s", b)
	}
}
