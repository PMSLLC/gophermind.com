package tools

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func makeMigrationDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMigrationDryRunReportsNewTable(t *testing.T) {
	root := makeMigrationDB(t)
	out, err := run(t, MigrationDryRun(root), `{"db":"app.db","sql":"CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT);"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "posts") {
		t.Errorf("dry-run should report the new posts table:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "add") && !strings.Contains(out, "+") {
		t.Errorf("dry-run should mark posts as added:\n%s", out)
	}
}

func TestMigrationDryRunDoesNotModifyOriginal(t *testing.T) {
	root := makeMigrationDB(t)
	if _, err := run(t, MigrationDryRun(root), `{"db":"app.db","sql":"CREATE TABLE temp1 (x INTEGER);"}`); err != nil {
		t.Fatal(err)
	}
	// The original db must be unchanged: temp1 should not exist.
	db, _ := sql.Open("sqlite", filepath.Join(root, "app.db"))
	defer db.Close()
	var n int
	db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='temp1'`).Scan(&n)
	if n != 0 {
		t.Error("dry-run modified the original database")
	}
}

func TestMigrationDryRunBadSQL(t *testing.T) {
	root := makeMigrationDB(t)
	if _, err := run(t, MigrationDryRun(root), `{"db":"app.db","sql":"CREATE TABLE ((("}`); err == nil {
		t.Error("invalid migration SQL should error")
	}
}

func TestMigrationDryRunContainsPath(t *testing.T) {
	root := makeMigrationDB(t)
	if _, err := run(t, MigrationDryRun(root), `{"db":"../../x.db","sql":"SELECT 1"}`); err == nil {
		t.Error("db path escaping the root should be rejected")
	}
}
