package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/gitenv"
)

// devBinaryScript is the guard script, relative to this package.
const devBinaryScript = "../../scripts/build-dev-binary.sh"

func requireBashAndGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func devGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command(dir, args...)
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// devRepo makes a clean temp repo with one commit and returns its path.
func devRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	devGit(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	devGit(t, repo, "add", "a.txt")
	devGit(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// runDevScript runs the script with GIT_* removed from its environment.
func runDevScript(t *testing.T, repo, bin string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(devBinaryScript)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = repo
	cmd.Env = append(gitenv.SanitizedEnv(), "GM_REPO="+repo, "GM_DEV_BIN="+bin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestDevBinaryScriptRefusesDirtyTree(t *testing.T) {
	requireBashAndGit(t)
	repo := devRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin", "gophermind-dev")
	out, err := runDevScript(t, repo, bin, "--dry-run")
	if err == nil {
		t.Fatalf("expected a non-zero exit, got success:\n%s", out)
	}
	if !strings.Contains(out, "refusing to build: the tree is dirty") {
		t.Errorf("output lacks the dirty message:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Dir(bin)); statErr == nil {
		t.Errorf("script created %s on a refused run", filepath.Dir(bin))
	}
}

func TestDevBinaryScriptRefusesUnpushedCommit(t *testing.T) {
	requireBashAndGit(t)
	repo := devRepo(t)
	bin := filepath.Join(t.TempDir(), "bin", "gophermind-dev")
	out, err := runDevScript(t, repo, bin, "--dry-run")
	if err == nil {
		t.Fatalf("expected a non-zero exit, got success:\n%s", out)
	}
	if !strings.Contains(out, "refusing to build: HEAD is not pushed") {
		t.Errorf("output lacks the unpushed message:\n%s", out)
	}
}

func TestDevBinaryScriptPrintsLdflags(t *testing.T) {
	requireBashAndGit(t)
	repo := devRepo(t)
	remote := t.TempDir()
	devGit(t, remote, "init", "-q", "--bare")
	devGit(t, repo, "remote", "add", "origin", remote)
	devGit(t, repo, "push", "-q", "origin", "main")
	sha := devGit(t, repo, "rev-parse", "--short", "HEAD")

	binDir := filepath.Join(t.TempDir(), "bin")
	bin := filepath.Join(binDir, "gophermind-dev")
	out, err := runDevScript(t, repo, bin, "--dry-run")
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Version=dev+" + sha,
		"version.Commit=" + sha,
		"version.Date=",
		bin,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if _, statErr := os.Stat(binDir); statErr == nil {
		t.Errorf("dry run created %s", binDir)
	}
}

func TestDevBinaryScriptRefusesSymlinkedBinDir(t *testing.T) {
	requireBashAndGit(t)
	repo := devRepo(t)
	remote := t.TempDir()
	devGit(t, remote, "init", "-q", "--bare")
	devGit(t, repo, "remote", "add", "origin", remote)
	devGit(t, repo, "push", "-q", "origin", "main")

	parent := t.TempDir()
	elsewhere := t.TempDir()
	link := filepath.Join(parent, "bin")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	out, err := runDevScript(t, repo, filepath.Join(link, "gophermind-dev"), "--dry-run")
	if err == nil {
		t.Fatalf("expected a refusal, got success:\n%s", out)
	}
	if !strings.Contains(out, "is a symlink") {
		t.Errorf("output lacks the symlink message:\n%s", out)
	}
}
