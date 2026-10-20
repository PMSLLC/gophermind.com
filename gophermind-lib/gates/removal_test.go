// Package gates holds tests that fail while something that was removed (or
// renamed) still exists. They read the whole repository, so they live in their
// own package instead of beside any one package.
package gates

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/gitenv"
)

// repoRoot is the folder holding go.work, two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("no go.work at %s: the gates package moved?", root)
	}
	return root
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "testdata": true, "libui-ng": true, "dist": true}

// eachGoFile calls fn with the repo-relative path and the import paths of
// every .go file outside the skipped folders. A file that does not parse is
// skipped: the compiler is the judge of that.
func eachGoFile(t *testing.T, fn func(rel string, imports []string)) {
	t.Helper()
	root := repoRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		var imps []string
		for _, s := range f.Imports {
			imps = append(imps, strings.Trim(s.Path.Value, `"`))
		}
		rel, _ := filepath.Rel(root, p)
		fn(filepath.ToSlash(rel), imps)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// removedImports are the packages that no longer exist. Add a line here in the
// same commit that deletes a package.
var removedImports = []string{
	"gophermind/gophermind-lib/plantree",
	"gophermind/gophermind-lib/orchestrate",
	"gophermind/gophermind-lib/phaseflow",
}

// removedDirs are the folders that must not exist.
var removedDirs = []string{
	"gophermind-lib/plantree",
	"gophermind-lib/orchestrate",
	"gophermind-lib/phaseflow",
}

func TestNoPackageImportsARemovedPackage(t *testing.T) {
	eachGoFile(t, func(rel string, imports []string) {
		for _, imp := range imports {
			for _, gone := range removedImports {
				if imp == gone || strings.HasPrefix(imp, gone+"/") {
					t.Errorf("%s imports %s, which was removed", rel, imp)
				}
			}
		}
	})
}

func TestRemovedPackageDirectoriesAreGone(t *testing.T) {
	root := repoRoot(t)
	for _, d := range removedDirs {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err == nil {
			t.Errorf("%s still exists", d)
		}
	}
}

// historicalPrefixes are dated records that keep the old names. Keep this list
// equal to the exclusions of the rename command in the task brief.
var historicalPrefixes = []string{
	"docs/superpowers/", "docs/reviews/", "docs/goals/", "docs/GOAL.md", "docs/GOAL-UNIFICATION.md",
	"docs/unification-overview.md", "CHANGELOG.md", ".planning/", ".gophermind/skills-packaging/references/",
	"PLAN.md", "todo.md", "todo-2.md", "todo-3.md", "todo-4.md", "BUILD_AUDIT_LOG.md", "BUILD_AUDIT_REPORT.md",
	"DEPLOY_QUEUE.md", "CONTEXT.md", "gophermind-lib/gates/",
}

// TestNoBriefv2Anywhere: no tracked path, import path or live text names the
// old version-named folder. It reads the tracked files only (git ls-files), so
// git-ignored scratch such as .superpowers/ is never read.
func TestNoBriefv2Anywhere(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	cmd.Env = gitenv.SanitizedEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	files := splitTracked(out)
	requireTracked(t, files)
	hist := func(rel string) bool {
		for _, p := range historicalPrefixes {
			if rel == p || strings.HasPrefix(rel, p) {
				return true
			}
		}
		return false
	}
	const old = "briefv2"
	skipped := 0
	defer func() {
		if skipped > 0 {
			t.Logf("skipped %d tracked files over 1 MiB", skipped)
		}
	}()
	for _, rel := range files {
		if hist(rel) {
			continue
		}
		if strings.Contains(strings.ToLower(rel), old) {
			t.Errorf("path %s still names %s", rel, old)
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		fi, ierr := os.Lstat(p)
		if ierr != nil || !fi.Mode().IsRegular() {
			continue
		}
		if fi.Size() > 1<<20 {
			skipped++
			continue
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil || bytes.IndexByte(b, 0) >= 0 {
			continue
		}
		if strings.Contains(strings.ToLower(string(b)), old) {
			t.Errorf("%s still mentions %s", rel, old)
		}
	}
}

// splitTracked turns `git ls-files -z` output into paths, dropping empties.
func splitTracked(out []byte) []string {
	var files []string
	for _, rel := range strings.Split(string(bytes.TrimRight(out, "\x00")), "\x00") {
		if rel != "" {
			files = append(files, rel)
		}
	}
	return files
}

// requireTracked fails unless the list proves the tracked files were read: it
// must be non-empty and contain the go.work entry.
func requireTracked(t *testing.T, files []string) {
	t.Helper()
	if len(files) == 0 {
		t.Fatal("git ls-files returned no files")
	}
	for _, f := range files {
		if f == "go.work" {
			return
		}
	}
	t.Fatal("git ls-files has no go.work entry")
}

func TestSplitTracked(t *testing.T) {
	if got := splitTracked(nil); len(got) != 0 {
		t.Fatalf("empty input gave %v", got)
	}
	got := splitTracked([]byte("go.work\x00a/b.go\x00"))
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	requireTracked(t, got)
}
