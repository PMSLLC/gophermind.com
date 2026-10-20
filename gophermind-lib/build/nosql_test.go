package build_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoBuildPackageImportsSQL pins decision 3 of the unification spec: run
// state is files, so nothing under build may import database/sql or the
// SQLite driver. It inspects real import declarations (go/parser ImportsOnly),
// so a string literal such as a qualifier-to-import-path table is not flagged.
func TestNoBuildPackageImportsSQL(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || p == "nosql_test.go" {
			return err
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				return uerr
			}
			if path == "database/sql" || strings.HasPrefix(path, "modernc.org/sqlite") || strings.HasSuffix(path, "build/db") {
				t.Errorf("%s imports %q", p, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
