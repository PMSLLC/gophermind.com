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
	runGit(t, repo, "checkout", "-b", "gm/x") // the run's branch: its edits are the run's leftovers
	run := t.TempDir()
	put(t, run, "_state/test_files.json", `["a.txt","gen/new_test.go","../escape.txt"]`)
	put(t, repo, "a.txt", "dirty\n")
	put(t, repo, "keep.txt", "dirty keep\n")
	put(t, repo, "gen/new_test.go", "x\n")

	// A listed path that leaves the repository refuses the whole reset.
	if _, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main"}); err == nil {
		t.Fatal("a path outside the repository must be refused")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "dirty\n" {
		t.Fatal("a refusal must change nothing")
	}

	put(t, run, "_state/test_files.json", `["a.txt","gen/new_test.go"]`)
	acts, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main"})
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
	if strings.Join(acts, "|") != "restored a.txt|removed gen/new_test.go|deleted branch gm/x" {
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
	if err := os.Remove(filepath.Join(dir, "approval.json")); err != nil { // fullRun's empty approval is not a plan
		t.Fatal(err)
	}
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

const trailerMsg = "gm(leaf): fn-a\n\nGopherMind-Run: run1"

func commitFile(t *testing.T, repo, rel, body, msg string) {
	t.Helper()
	put(t, repo, rel, body)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", msg)
}

func TestForeignCommitRefusalLeavesHeadWhereItWas(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "_state/test_files.json", `[]`)
	runGit(t, repo, "checkout", "-b", "gm/x")
	commitFile(t, repo, "p.txt", "p\n", "a person's commit")
	runGit(t, repo, "checkout", "-b", "other", "main")
	_, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main"})
	if err == nil || !strings.Contains(err.Error(), "did not make") {
		t.Fatalf("err %v", err)
	}
	if got := runGit(t, repo, "branch", "--show-current"); got != "other" {
		t.Errorf("HEAD moved to %q", got)
	}
	// a person on another branch stays there when the run's own branch is deleted
	runGit(t, repo, "branch", "-D", "gm/x")
	runGit(t, repo, "checkout", "-b", "gm/x", "main")
	commitFile(t, repo, "n.txt", "n\n", trailerMsg)
	runGit(t, repo, "checkout", "other")
	if _, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repo, "branch", "--show-current"); got != "other" {
		t.Errorf("HEAD moved to %q", got)
	}
}

func TestStoppedRunOnTheWorkBranchResetsCleanly(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "_state/test_files.json", `["a.txt","gen/t_test.go"]`)
	runGit(t, repo, "checkout", "-b", "gm/x")
	commitFile(t, repo, "a.txt", "run version\n", trailerMsg)
	put(t, repo, "a.txt", "uncommitted run edit\n")
	put(t, repo, "gen/t_test.go", "x\n")
	acts, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main", Started: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repo, "branch", "--show-current"); got != "main" {
		t.Errorf("HEAD on %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "base a\n" {
		t.Errorf("a.txt %q", b)
	}
	if st := runGit(t, repo, "status", "--porcelain"); st != "" {
		t.Errorf("tree not clean: %q", st)
	}
	if got := strings.Join(acts, "|"); got != "restored a.txt|removed gen/t_test.go|deleted branch gm/x" {
		t.Errorf("actions %s", got)
	}
}

func TestResetRefusesAFolderThatIsNotARepositoryRoot(t *testing.T) {
	repo := newRepo(t)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, repo, "a.txt", "dirty\n")
	run := filepath.Join(t.TempDir(), "run1")
	want := "resume: " + sub + " is not a repository root"
	if _, err := ResetRepo(run, sub, RepoOwnedClone, repo, RepoState{Base: "main"}); err == nil || err.Error() != want {
		t.Errorf("owned: %v", err)
	}
	if _, err := ResetRepo(run, sub, RepoRestoreFiles, "", RepoState{Base: "main"}); err == nil || err.Error() != want {
		t.Errorf("restore: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "dirty\n" {
		t.Error("the enclosing repository was touched")
	}
}

func TestStageResumeChecksTheRunFolderBeforeTheRepository(t *testing.T) {
	repo := newRepo(t)
	dir := fullRun(t)
	put(t, dir, "brief.md", string(testBrief("main")))
	if _, err := FromStage(dir, "approve", RepoRequest{Mode: RepoKeep}, fixed); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "approval.json", "") // the stage is reached again, in the same second
	put(t, repo, "a.txt", "dirty\n")
	_, err := FromStage(dir, "approve", RepoRequest{Repo: repo, Mode: RepoOwnedClone, OwnedUnder: filepath.Dir(repo)}, fixed)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "dirty\n" {
		t.Error("the repository was reset although the run folder refused")
	}
}

func TestRestoreRefusesWhenTheApprovedPlanCannotBeRead(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "approval.json", "{}")
	put(t, run, "_state/test_files.json", `["a.txt"]`)
	put(t, repo, "a.txt", "dirty\n")
	_, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"})
	if err == nil || !strings.Contains(err.Error(), "resume: the approved plan could not be read: ") || !strings.HasSuffix(err.Error(), "; not resetting") {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "dirty\n" {
		t.Error("a file was restored")
	}
}

func TestRestoreRefusesAPlanPathThatIsAFolder(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(t.TempDir(), "run1")
	commitFile(t, repo, "dir/f.txt", "base\n", "add dir")
	put(t, repo, "dir/f.txt", "unlisted dirty\n")
	for _, listed := range []string{"dir", "newdir"} {
		put(t, repo, "newdir/x.txt", "x\n")
		put(t, run, "_state/test_files.json", `["`+listed+`"]`)
		_, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"})
		if err == nil || !strings.Contains(err.Error(), "plan path "+listed+" is a ") {
			t.Fatalf("%s: err %v", listed, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "dir/f.txt")); string(b) != "unlisted dirty\n" {
		t.Error("a file under a listed folder was overwritten")
	}
	if _, err := os.Lstat(filepath.Join(repo, "newdir/x.txt")); err != nil {
		t.Error("a file under a listed folder was removed")
	}
}

func TestRestoreRefusesAParentLinkThatLeavesTheRepository(t *testing.T) {
	repo := newRepo(t)
	outside := t.TempDir()
	put(t, outside, "x.go", "outside\n")
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "_state/test_files.json", `["link/x.go"]`)
	_, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"})
	if err == nil || !strings.Contains(err.Error(), "not a path inside the repository") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "x.go")); err != nil {
		t.Error("a file outside the repository was removed")
	}
}

func TestResetRefusesARunThatLanded(t *testing.T) {
	repo := newRepo(t)
	commitFile(t, repo, "n.txt", "n\n", trailerMsg)
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "_state/test_files.json", `[]`)
	put(t, repo, "a.txt", "dirty\n")
	want := "resume: run run1 landed on main; a resume cannot undo landed commits"
	if _, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"}); err == nil || err.Error() != want {
		t.Errorf("restore: %v", err)
	}
	if _, err := ResetRepo(run, repo, RepoOwnedClone, filepath.Dir(repo), RepoState{Base: "main"}); err == nil || err.Error() != want {
		t.Errorf("owned: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "dirty\n" {
		t.Error("the repository was touched")
	}
}

func TestResetRefusesABaseThatIsNotABranchName(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(t.TempDir(), "run1")
	for _, base := range []string{"-x", "a..b", "no-such-branch"} {
		if _, err := ResetRepo(run, repo, RepoOwnedClone, filepath.Dir(repo), RepoState{Base: base}); err == nil {
			t.Errorf("owned accepted base %q", base)
		}
		if _, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: base}); err == nil {
			t.Errorf("restore accepted base %q", base)
		}
	}
}

func TestKeepCountsOnlyTestFilesThatDifferFromBase(t *testing.T) {
	repo := newRepo(t)
	commitFile(t, repo, "pkg/a_test.go", "package pkg\n", "tracked test")
	dir := fullRun(t)
	if err := os.Remove(filepath.Join(dir, "approval.json")); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "_state/test_files.json", `["pkg/a_test.go"]`)
	put(t, dir, "brief.md", string(testBrief("main")))
	if _, err := FromStage(dir, "testwriter", RepoRequest{Repo: repo, Mode: RepoKeep}, fixed); err != nil {
		t.Fatalf("a file identical to base counted: %v", err)
	}
	dir = fullRun(t)
	os.Remove(filepath.Join(dir, "approval.json"))
	put(t, dir, "_state/test_files.json", `["pkg/a_test.go"]`)
	put(t, dir, "brief.md", string(testBrief("main")))
	put(t, repo, "pkg/a_test.go", "package pkg // changed\n")
	if _, err := FromStage(dir, "testwriter", RepoRequest{Repo: repo, Mode: RepoKeep}, fixed); err == nil {
		t.Fatal("a changed test file was not counted")
	}
}

func TestOwnedCloneKeepsARunFolderInsideTheClone(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(repo, ".gophermind", "run1")
	put(t, run, "_state/executor.json", "{}")
	put(t, repo, "untracked.txt", "u\n")
	if _, err := ResetRepo(run, repo, RepoOwnedClone, filepath.Dir(repo), RepoState{Base: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(run, "_state/executor.json")); err != nil {
		t.Errorf("run folder lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "untracked.txt")); err == nil {
		t.Error("untracked file kept")
	}
}

func testBrief(base string) []byte {
	return []byte("---\nspec_version: \"2.0\"\nid: gm-2026-10-28-060\ntitle: t\nlanguage: go\nbase_branch: " + base + "\nlanding: commit\non_ambiguity: halt\n---\n\n" +
		"## Overview\n\no\n\n## Features\n\n### F\n\n- f\n\n## Architecture\n\n- `cmd/tool` (package `main`): wires it up. `Run` takes an `io.Reader`.\n\n## Data\n\nd\n\n## Constraints\n\n- c\n\n## Out of scope\n\n- o\n\n## Acceptance\n\n- `go build ./...` succeeds.\n")
}

func TestReadExecutorStateReadsTheBaseFromTheBrief(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "brief.md", string(testBrief("develop")))
	if _, base, _, err := ReadExecutorState(dir); err != nil || base != "develop" {
		t.Fatalf("base %q %v", base, err)
	}
}

func TestRestoreRefusesAPersonsUncommittedChangeByName(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "_state/test_files.json", `["a.txt","keep.txt"]`)
	put(t, repo, "keep.txt", "person edit\n")
	_, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"})
	want := "resume: keep.txt has uncommitted changes; commit or stash them, then pass --reset-repo again"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "keep.txt")); string(b) != "person edit\n" {
		t.Error("the edit was overwritten")
	}
	// an untracked file at a path base holds is a person's too
	runGit(t, repo, "checkout", "--", "keep.txt")
	runGit(t, repo, "checkout", "-b", "other")
	runGit(t, repo, "rm", "-q", "keep.txt")
	runGit(t, repo, "commit", "-m", "drop keep")
	put(t, repo, "keep.txt", "untracked person file\n")
	if _, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Base: "main"}); err == nil || err.Error() != want {
		t.Fatalf("untracked: %v", err)
	}
	// on the work branch the same edit is the run's leftover and is reset
	repo2 := newRepo(t)
	runGit(t, repo2, "checkout", "-b", "gm/x")
	put(t, repo2, "keep.txt", "run leftover\n")
	if _, err := ResetRepo(run, repo2, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo2, "keep.txt")); string(b) != "base keep\n" {
		t.Errorf("keep.txt %q", b)
	}
}

func TestOwnedCloneRefusesAGitDirectoryOutsideTheClone(t *testing.T) {
	owned := t.TempDir()
	outside := newRepo(t)
	clone := filepath.Join(owned, "clone")
	put(t, clone, ".git", "gitdir: "+filepath.Join(outside, ".git")+"\n")
	put(t, outside, "a.txt", "dirty\n")
	run := filepath.Join(t.TempDir(), "run1")
	_, err := ResetRepo(run, clone, RepoOwnedClone, owned, RepoState{Base: "main"})
	want := "resume: refusing to reset " + clone + ": its git directory is outside the clone"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "a.txt")); string(b) != "dirty\n" {
		t.Error("the repository behind the gitfile was reset")
	}
}

func TestRestoreSaysFilesWereRestoredWhenTheBranchSwitchRefuses(t *testing.T) {
	repo := newRepo(t)
	run := filepath.Join(t.TempDir(), "run1")
	put(t, run, "_state/test_files.json", `["a.txt"]`)
	runGit(t, repo, "checkout", "-b", "gm/x")
	commitFile(t, repo, "u.txt", "run\n", trailerMsg)
	runGit(t, repo, "checkout", "main")
	commitFile(t, repo, "u.txt", "main version\n", "main adds u")
	runGit(t, repo, "checkout", "gm/x")
	put(t, repo, "u.txt", "person edit\n") // conflicts with the switch, not a plan file
	_, err := ResetRepo(run, repo, RepoRestoreFiles, "", RepoState{Branch: "gm/x", Base: "main"})
	if err == nil || !strings.Contains(err.Error(), "the listed files were already restored; the switch to main was refused") {
		t.Fatalf("err %v", err)
	}
}
