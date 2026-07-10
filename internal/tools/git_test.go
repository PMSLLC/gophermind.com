package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644)
	commit(t, dir, "initial commit")
	return dir
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestGitInfoLog(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("two\n"), 0o644)
	commit(t, dir, "second commit")

	tool := GitInfo(dir)
	out, err := run(t, tool, `{"op":"log","limit":10}`)
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("log output is not JSON array: %v\n%s", err, out)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 commits, got %d: %s", len(entries), out)
	}
	if entries[0]["subject"] != "second commit" {
		t.Errorf("newest subject = %v", entries[0]["subject"])
	}
	if entries[0]["author"] != "Tester" || entries[0]["hash"] == "" {
		t.Errorf("missing author/hash: %v", entries[0])
	}
}

func TestGitInfoStatus(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	out, err := run(t, GitInfo(dir), `{"op":"status"}`)
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, out)
	}
	paths := map[string]bool{}
	for _, e := range entries {
		paths[e["path"].(string)] = true
	}
	if !paths["f.txt"] || !paths["new.txt"] {
		t.Errorf("status missing changed files: %v", entries)
	}
}

func TestGitInfoDiff(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("mutated\n"), 0o644)
	out, err := run(t, GitInfo(dir), `{"op":"diff"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mutated") || !strings.Contains(out, "f.txt") {
		t.Errorf("diff missing content: %q", out)
	}
}

func TestGitInfoRejectsBadOp(t *testing.T) {
	dir := initRepo(t)
	if _, err := run(t, GitInfo(dir), `{"op":"push"}`); err == nil {
		t.Error("unknown/mutating op should be rejected")
	}
}

func TestGitInfoOutsideRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	if _, err := run(t, GitInfo(dir), `{"op":"log"}`); err == nil {
		t.Error("expected error outside a git repository")
	}
}
