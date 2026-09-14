package tools

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func makeExplainDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT, name TEXT);
		CREATE INDEX idx_users_email ON users(email);
		INSERT INTO users (email, name) VALUES ('a@x','a'),('b@x','b');`); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDBExplainFlagsFullScan(t *testing.T) {
	root := makeExplainDB(t)
	// name is not indexed -> full table scan expected.
	out, err := run(t, DBExplain(root), `{"db":"app.db","query":"SELECT * FROM users WHERE name = ?","params":["a"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "scan") {
		t.Errorf("expected a SCAN in the plan:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "full") && !strings.Contains(strings.ToLower(out), "warning") {
		t.Errorf("expected a full-scan warning:\n%s", out)
	}
}

func TestDBExplainIndexedNoWarning(t *testing.T) {
	root := makeExplainDB(t)
	// email is indexed -> should use the index, no full-scan warning.
	out, err := run(t, DBExplain(root), `{"db":"app.db","query":"SELECT * FROM users WHERE email = ?","params":["a@x"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(out), "full-table scan") {
		t.Errorf("indexed query should not warn about full-table scan:\n%s", out)
	}
}

func TestDBExplainRejectsWrites(t *testing.T) {
	root := makeExplainDB(t)
	if _, err := run(t, DBExplain(root), `{"db":"app.db","query":"DELETE FROM users"}`); err == nil {
		t.Error("non-read-only query should be rejected")
	}
}

func TestDBExplainMissingDB(t *testing.T) {
	if _, err := run(t, DBExplain(t.TempDir()), `{"db":"nope.db","query":"SELECT 1"}`); err == nil {
		t.Error("missing db should error")
	}
}
