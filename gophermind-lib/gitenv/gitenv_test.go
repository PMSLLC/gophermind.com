package gitenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizedEnvDropsGitVars checks that every GIT_* variable is removed
// and every other variable survives.
func TestSanitizedEnvDropsGitVars(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere")
	t.Setenv("GIT_INDEX_FILE", "/somewhere/index")
	t.Setenv("GIT_WORK_TREE", "/somewhere/worktree")
	t.Setenv("GITENV_KEEP_ME", "1")

	env := SanitizedEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") {
			t.Errorf("SanitizedEnv kept a GIT_ variable: %q", kv)
		}
	}
	found := false
	for _, kv := range env {
		if kv == "GITENV_KEEP_ME=1" {
			found = true
		}
	}
	if !found {
		t.Error("SanitizedEnv dropped a non-GIT_ variable it should have kept")
	}
}

// TestCommandSetsDirAndSanitizedEnv checks that Command sets Dir to the given
// directory and that its Env carries no GIT_* variable.
func TestCommandSetsDirAndSanitizedEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere")

	cmd := Command("/some/dir", "status")
	if cmd.Dir != "/some/dir" {
		t.Errorf("Dir = %q, want /some/dir", cmd.Dir)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "GIT_") {
			t.Errorf("Command env kept a GIT_ variable: %q", kv)
		}
	}
}

// TestCommandIgnoresInheritedGitDir is the decisive regression test for the
// incident described in the package comment. With GIT_DIR (and
// GIT_INDEX_FILE) pointed at a decoy directory, a real "git init" + commit
// run through Command in a different directory must land in that directory,
// and the decoy must never be written to. This test fails if the env
// sanitizing in Command is removed; that was verified by hand (see
// task-7-report.md).
func TestCommandIgnoresInheritedGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// A decoy that must remain untouched. If Command stopped stripping
	// GIT_*, git would write here instead of into the intended repo.
	decoy := t.TempDir()
	t.Setenv("GIT_DIR", decoy)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoy, "index"))

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
	} {
		if out, err := Command(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "initial commit"}} {
		if out, err := Command(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// The commit landed in the intended repo.
	out, err := Command(dir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log in the intended repo: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "initial commit") {
		t.Errorf("intended repo has no commit; got %q", out)
	}

	// The decoy was never turned into a repository.
	if _, err := os.Stat(filepath.Join(decoy, "HEAD")); err == nil {
		t.Error("decoy was written to: HEAD appeared under the inherited GIT_DIR")
	}
}
