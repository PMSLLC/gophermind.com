package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/internal/gitenv"
)

func TestRepoContextListsTopLevel(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "internal"), 0o755)

	ctx := RepoContext(dir)
	if !strings.Contains(ctx, "main.go") || !strings.Contains(ctx, "internal/") {
		t.Errorf("context missing top-level entries:\n%s", ctx)
	}
	if !strings.Contains(ctx, "<repo_context") {
		t.Errorf("context not wrapped in a tag:\n%s", ctx)
	}
}

func TestRepoContextIncludesGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "trunk"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "T"}} {
		// Uses gitenv so an inherited GIT_DIR (every git hook exports one)
		// cannot redirect these calls at the caller's real repository.
		// Without this, "git init" reinitialized this repo as bare and test
		// commits landed on a real branch. See internal/gitenv.
		c := gitenv.Command(dir, args...)
		c.Run()
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)

	ctx := RepoContext(dir)
	if !strings.Contains(ctx, "trunk") {
		t.Errorf("context missing git branch:\n%s", ctx)
	}
	// An untracked file should show in the short status.
	if !strings.Contains(ctx, "f.txt") {
		t.Errorf("context missing dirty file:\n%s", ctx)
	}
}

func TestRepoContextNonGitDirDoesNotCrash(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	if ctx := RepoContext(dir); !strings.Contains(ctx, "a.txt") {
		t.Errorf("non-git dir should still list files:\n%s", ctx)
	}
}
