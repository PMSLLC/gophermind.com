package gitland

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFilesListsTrackedAndUntrackedButNotIgnored(t *testing.T) {
	c := newRepo(t) // README.md and go.mod committed
	put(t, c, ".gitignore", "*.log\n")
	put(t, c, "pkg/new.go", "package pkg\n")
	put(t, c, "run.log", "ignored\n")
	put(t, c, ".gophermind/r/_state/x.json", "{}\n")
	if err := os.Remove(filepath.Join(c.dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	got, err := c.Files()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".gitignore", "go.mod", "pkg/new.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Files = %q, want %q", got, want)
	}
}

func TestNewScratchMakesABaselineWithTheGuard(t *testing.T) {
	useTestGit(t)
	dir := t.TempDir()
	for rel, body := range map[string]string{"go.mod": "module x\n", "a/a.go": "package a\n", ".gitignore": "*.log\n"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewScratch(dir, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	home := s.home
	if d, err := s.Dirty(); err != nil || len(d) != 0 {
		t.Fatalf("Dirty after the baseline = %q, %v", d, err)
	}
	if subj := gx(t, s, "log", "-1", "--format=%s"); subj != "gm(baseline)" {
		t.Fatalf("baseline subject = %q", subj)
	}
	put(t, s, "a/stray.go", "package a\n")
	put(t, s, "x.log", "ignored\n")
	if d, _ := s.Dirty(); !reflect.DeepEqual(d, []string{"a/stray.go"}) {
		t.Fatalf("Dirty = %q", d)
	}
	if ig, _ := s.Ignored(); !reflect.DeepEqual(ig, []string{"x.log"}) {
		t.Fatalf("Ignored = %q", ig)
	}
	if err := s.Restore([]string{"a/stray.go", "x.log"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "stray.go")); !os.IsNotExist(err) {
		t.Fatal("Restore left the untracked file")
	}
	for _, args := range [][]string{{"push"}, {"reset", "--hard"}, {"checkout", "main"}, {"clean", "-f"}, {"gc"}} {
		if _, _, err := s.runCode(args...); !errors.Is(err, ErrForbiddenGitArgs) {
			t.Errorf("git %v in a scratch tree: %v, want the guard", args, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("Close left the private home")
	}
	if _, err := NewScratch(dir, "run-1"); err == nil || !strings.Contains(err.Error(), "already holds a repository") {
		t.Fatalf("a second NewScratch over the same directory: %v", err)
	}
	if _, err := NewScratch(t.TempDir(), "bad id!"); err == nil {
		t.Fatal("an invalid run id was accepted")
	}
}

// A scratch tree under the repository's .gophermind is a repository of its
// own: the outer repository neither sees its files nor gains a commit.
func TestNewScratchInsideARepositoryIsItsOwnRepository(t *testing.T) {
	outer := newRepo(t)
	if err := os.WriteFile(filepath.Join(outer.dir, ".git", "info", "exclude"), []byte(".gophermind/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(outer.dir, ".gophermind", "r-scratch", "trees", "fn-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewScratch(dir, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	put(t, s, "b.go", "package x\n")
	if d, _ := s.Dirty(); !reflect.DeepEqual(d, []string{"b.go"}) {
		t.Fatalf("the tree's Dirty = %q", d)
	}
	if d, _ := outer.Dirty(); len(d) != 0 {
		t.Fatalf("the outer repository sees the tree: %q", d)
	}
	if n := gx(t, outer, "rev-list", "--count", "HEAD"); n != "1" {
		t.Fatalf("the outer repository has %s commits, want 1", n)
	}
}
