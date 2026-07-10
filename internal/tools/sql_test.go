package tools

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func makeTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);
		INSERT INTO users (name, active) VALUES ('alice', 1), ('bob', 0), ('carol', 1);`); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSQLQuerySelect(t *testing.T) {
	root := makeTestDB(t)
	out, err := run(t, SQLQuery(root), `{"db":"app.db","query":"SELECT name, active FROM users ORDER BY id"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name", "active", "alice", "bob", "carol"} {
		if !strings.Contains(out, want) {
			t.Errorf("result missing %q:\n%s", want, out)
		}
	}
}

func TestSQLQueryParameterized(t *testing.T) {
	root := makeTestDB(t)
	out, err := run(t, SQLQuery(root), `{"db":"app.db","query":"SELECT name FROM users WHERE active = ?","params":[1]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "carol") || strings.Contains(out, "bob") {
		t.Errorf("parameterized filter wrong:\n%s", out)
	}
}

func TestSQLQueryRejectsWrites(t *testing.T) {
	root := makeTestDB(t)
	for _, q := range []string{
		`{"db":"app.db","query":"INSERT INTO users(name) VALUES('x')"}`,
		`{"db":"app.db","query":"UPDATE users SET name='x'"}`,
		`{"db":"app.db","query":"DELETE FROM users"}`,
		`{"db":"app.db","query":"DROP TABLE users"}`,
	} {
		if _, err := run(t, SQLQuery(root), q); err == nil {
			t.Errorf("write query should be rejected: %s", q)
		}
	}
	// The table is still intact (nothing was written).
	out, _ := run(t, SQLQuery(root), `{"db":"app.db","query":"SELECT count(*) FROM users"}`)
	if !strings.Contains(out, "3") {
		t.Errorf("data was modified: %s", out)
	}
}

func TestSQLQueryContainsDBPath(t *testing.T) {
	root := makeTestDB(t)
	if _, err := run(t, SQLQuery(root), `{"db":"../../etc/passwd.db","query":"SELECT 1"}`); err == nil {
		t.Error("db path escaping the root should be rejected")
	}
}

func TestSQLQueryMissingDB(t *testing.T) {
	if _, err := run(t, SQLQuery(t.TempDir()), `{"db":"nope.db","query":"SELECT 1"}`); err == nil {
		t.Error("missing db should error")
	}
}
