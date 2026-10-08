package briefv2_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBriefv2PackageImportsSQL pins decision 3 of the unification spec: run
// state is files, so nothing under briefv2 may import database/sql or the
// SQLite driver.
func TestNoBriefv2PackageImportsSQL(t *testing.T) {
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || p == "nosql_test.go" ||
			// typedecl.go holds "sql": "database/sql" as map data (a table of
			// import paths for generated code), not an import: false positive.
			p == filepath.Join("executor", "typedecl.go") {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		for _, bad := range []string{`"database/sql"`, `modernc.org/sqlite`, `briefv2/db"`} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s imports %s", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
