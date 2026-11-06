package resume

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo makes a repo on main with one commit holding a.txt and keep.txt.
func newRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "T")
	put(t, dir, "a.txt", "base a\n")
	put(t, dir, "keep.txt", "base keep\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	return dir
}

func TestOwnedCloneIsResetToBase(t *testing.T) {
	repo := newRepo(t)
	base := runGit(t, repo, "rev-parse", "main")
	runGit(t, repo, "checkout", "-b", "gm/x")
	put(t, repo, "extra.txt", "x\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "extra")
	put(t, repo, "a.txt", "dirty\n")
	put(t, repo, "untracked.txt", "u\n")

	acts, err := ResetRepo(t.TempDir(), repo, RepoOwnedClone, filepath.Dir(repo), RepoState{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repo, "rev-parse", "HEAD"); got != base {
		t.Errorf("HEAD %s, want %s", got, base)
	}
	if st := runGit(t, repo, "status", "--porcelain"); st != "" {
		t.Errorf("tree not clean: %q", st)
	}
	if len(acts) != 2 || !strings.Contains(acts[0], repo) || !strings.Contains(acts[1], repo) {
		t.Errorf("actions %v", acts)
	}
}

func TestOwnedCloneRefusesAPathOutsideTheOwner(t *testing.T) {
	repo := newRepo(t)
	put(t, repo, "a.txt", "dirty\n")
	other := t.TempDir()
	_, err := ResetRepo(t.TempDir(), repo, RepoOwnedClone, other, RepoState{Base: "main"})
	want := "resume: refusing to reset " + repo + ": not under " + other
	if err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "dirty\n" {
		t.Error("the repository was touched")
	}
	// the owner itself and a sibling prefix are not inside it
	if _, err := ResetRepo(t.TempDir(), repo, RepoOwnedClone, repo, RepoState{Base: "main"}); err == nil {
		t.Error("owner == repo must be refused")
	}
	if _, err := ResetRepo(t.TempDir(), repo, RepoOwnedClone, repo[:len(repo)-1], RepoState{Base: "main"}); err == nil {
		t.Error("a name prefix is not a path prefix")
	}
	if _, err := ResetRepo(t.TempDir(), "relative", RepoOwnedClone, filepath.Dir(repo), RepoState{Base: "main"}); err == nil {
		t.Error("a relative repo must be refused")
	}
}

func TestRestoreFilesDeletesOnlyARunOwnedBranch(t *testing.T) {
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "_state/test_files.json", `[]`)

	// A branch holding only the run's own commit is deleted.
	repo := newRepo(t)
	runGit(t, repo, "checkout", "-b", "gm/x")
	put(t, repo, "new.txt", "n\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "gm(leaf): fn-a\n\nGopherMind-Run: run1")
	acts, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main", Started: true})
	if err != nil {
		t.Fatal(err)
	}
	if runGit(t, repo, "branch", "--list", "gm/x") != "" {
		t.Error("run-owned branch kept")
	}
	if runGit(t, repo, "branch", "--show-current") != "main" {
		t.Error("base not checked out")
	}
	if len(acts) != 1 || acts[0] != "deleted branch gm/x" {
		t.Errorf("actions %v", acts)
	}

	// A branch with a foreign commit is refused and kept.
	repo2 := newRepo(t)
	runGit(t, repo2, "checkout", "-b", "gm/x")
	put(t, repo2, "new.txt", "n\n")
	runGit(t, repo2, "add", ".")
	runGit(t, repo2, "commit", "-m", "gm(leaf): fn-a\n\nGopherMind-Run: run1")
	put(t, repo2, "person.txt", "p\n")
	runGit(t, repo2, "add", ".")
	runGit(t, repo2, "commit", "-m", "a person's commit")
	_, err = ResetRepo(run, repo2, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main", Started: true})
	if err == nil || !strings.Contains(err.Error(), "1 commit(s) this run did not make") {
		t.Fatalf("err %v", err)
	}
	if runGit(t, repo2, "branch", "--list", "gm/x") == "" {
		t.Error("branch with a foreign commit was deleted")
	}
}

func TestRestoreFilesRestoresListedFilesOnly(t *testing.T) {
	repo := newRepo(t)
	run := t.TempDir()
	put(t, run, "_state/test_files.json", `["a.txt","gen/new_test.go","../escape.txt"]`)
	put(t, repo, "a.txt", "dirty\n")
	put(t, repo, "keep.txt", "dirty keep\n")
	put(t, repo, "gen/new_test.go", "x\n")

	// A listed path that leaves the repository refuses the whole reset.
	if _, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"}); err == nil {
		t.Fatal("a path outside the repository must be refused")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "dirty\n" {
		t.Fatal("a refusal must change nothing")
	}

	put(t, run, "_state/test_files.json", `["a.txt","gen/new_test.go"]`)
	acts, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "base a\n" {
		t.Errorf("a.txt %q", b)
	}
	if _, err := os.Lstat(filepath.Join(repo, "gen/new_test.go")); err == nil {
		t.Error("listed untracked file kept")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "keep.txt")); string(b) != "dirty keep\n" {
		t.Errorf("unlisted file touched: %q", b)
	}
	if strings.Join(acts, "|") != "restored a.txt|removed gen/new_test.go" {
		t.Errorf("actions %v", acts)
	}
}

func TestFromStageRefusesWithoutARepoModeWhenTheExecutorRan(t *testing.T) {
	dir := fullRun(t)
	put(t, dir, "_state/executor.json", `{"started_at":"2026-11-05T10:00:00Z","branch":"gm/x"}`)
	_, err := FromStage(dir, "coverage", RepoRequest{Mode: RepoKeep}, fixed)
	want := "resume: executor state exists; pass --reset-repo to discard the run's branch and files, or resume from node:<id>"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v", err)
	}
	if !present(dir, "coverage.json") || present(dir, "_state/discarded") {
		t.Error("a refusal must move nothing")
	}
}

func TestKeepRefusesWhenRunTestFilesExistAndRestoreClearsThem(t *testing.T) {
	repo := newRepo(t)
	dir := fullRun(t)
	put(t, dir, "_state/test_files.json", `["pkg/a_test.go"]`)
	put(t, repo, "pkg/a_test.go", "package pkg\n")

	_, err := FromStage(dir, "testwriter", RepoRequest{Repo: repo, Mode: RepoKeep}, fixed)
	want := "resume: test files from this run exist in the repo; pass --reset-repo to remove them"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v", err)
	}
	if !present(dir, "_state/testwriter.json") {
		t.Error("a refusal must move nothing")
	}

	acts, err := ResetRepo(dir, repo, RepoRestoreFiles, "", RepoState{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0] != "removed pkg/a_test.go" {
		t.Errorf("actions %v", acts)
	}
	if _, err := FromStage(dir, "testwriter", RepoRequest{Repo: repo, Mode: RepoKeep}, fixed); err != nil {
		t.Fatalf("after the restore: %v", err)
	}
}

func TestReadExecutorState(t *testing.T) {
	dir := t.TempDir()
	if b, base, started, err := ReadExecutorState(dir); err != nil || b != "" || base != "" || started {
		t.Fatalf("empty run: %q %q %v %v", b, base, started, err)
	}
	put(t, dir, "_state/executor.json", `{"started_at":"2026-11-05T10:00:00Z","branch":"gm/x"}`)
	if b, _, started, err := ReadExecutorState(dir); err != nil || b != "gm/x" || !started {
		t.Fatalf("started: %q %v %v", b, started, err)
	}
}
