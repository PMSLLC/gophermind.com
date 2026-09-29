package rundir_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/rundir"
)

func TestCreateLayoutAndExclude(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := rundir.Create(repo, "gm-2026-09-29-001", []byte("# brief"))
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(repo, ".gophermind", "gm-2026-09-29-001") {
		t.Errorf("dir = %s", dir)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "brief.md")); string(b) != "# brief" {
		t.Error("brief.md not copied")
	}
	if fi, err := os.Stat(filepath.Join(dir, "logs")); err != nil || !fi.IsDir() {
		t.Error("logs/ missing")
	}
	ex, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex), ".gophermind/") != 1 {
		t.Errorf("exclude = %q", ex)
	}
	if _, err := rundir.Create(repo, "gm-2026-09-29-001", []byte("x")); !errors.Is(err, rundir.ErrExists) {
		t.Errorf("second Create = %v, want ErrExists", err)
	}
	ex, _ = os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex), ".gophermind/") != 1 {
		t.Error("exclude entry duplicated")
	}
}

func TestCreateRejectsBadID(t *testing.T) {
	for _, id := range []string{"", "../x", "gm-1", "gm-2026-09-29-001/../../x"} {
		if _, err := rundir.Create(t.TempDir(), id, nil); err == nil {
			t.Errorf("id %q should be rejected", id)
		}
	}
}

func TestCreateWithoutGitDirIsFine(t *testing.T) {
	if _, err := rundir.Create(t.TempDir(), "gm-2026-09-29-001", []byte("x")); err != nil {
		t.Fatal(err)
	}
}

const rid = "gm-2026-09-29-001"

func TestWorktreeExcludeGoesToCommonDir(t *testing.T) {
	tmp := t.TempDir()
	wtGit := filepath.Join(tmp, "main", ".git", "worktrees", "wt")
	if err := os.MkdirAll(wtGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "wt")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rundir.Create(repo, rid, []byte("x")); err != nil {
		t.Fatal(err)
	}
	ex, err := os.ReadFile(filepath.Join(tmp, "main", ".git", "info", "exclude"))
	if err != nil || string(ex) != ".gophermind/\n" {
		t.Fatalf("exclude = %q, %v", ex, err)
	}
}

func TestWorktreeRelativeGitdir(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "wt")
	gd := filepath.Join(repo, "gd") // relative to the repo root, no commondir
	if err := os.MkdirAll(gd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: gd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rundir.Create(repo, rid, nil); err != nil {
		t.Fatal(err)
	}
	if ex, _ := os.ReadFile(filepath.Join(gd, "info", "exclude")); string(ex) != ".gophermind/\n" {
		t.Fatalf("exclude = %q", ex)
	}
}

func TestUnparsableGitFileErrorsAndLeavesNothing(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rundir.Create(repo, rid, []byte("x")); err == nil {
		t.Fatal("unparsable .git file must error")
	}
	if _, err := os.Stat(filepath.Join(repo, ".gophermind", rid)); err == nil {
		t.Fatal("run dir must not exist after a failed exclude")
	}
}

func TestExcludeAddsMissingNewlineAndIgnoresComments(t *testing.T) {
	repo := t.TempDir()
	info := filepath.Join(repo, ".git", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(info, "exclude")
	if err := os.WriteFile(p, []byte("*.log\n# .gophermind/"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rundir.Create(repo, rid, nil); err != nil {
		t.Fatal(err)
	}
	ex, _ := os.ReadFile(p)
	if string(ex) != "*.log\n# .gophermind/\n.gophermind/\n" {
		t.Fatalf("exclude = %q", ex)
	}
}

func TestRunDirModes(t *testing.T) {
	repo := t.TempDir()
	dir, err := rundir.Create(repo, rid, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "logs"): 0o700, filepath.Join(dir, "brief.md"): 0o600} {
		fi, err := os.Stat(p)
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s mode = %v, %v; want %v", p, fi.Mode().Perm(), err, want)
		}
	}
	if _, err := rundir.Create(repo, rid, nil); !errors.Is(err, rundir.ErrExists) {
		t.Errorf("second Create = %v, want ErrExists", err)
	}
}
