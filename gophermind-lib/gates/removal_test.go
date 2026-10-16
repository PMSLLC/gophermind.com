// Package gates holds tests that fail while something that was removed (or
// renamed) still exists. They read the whole repository, so they live in their
// own package instead of beside any one package.
package gates

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
