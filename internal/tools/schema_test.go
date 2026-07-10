package tools

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func makeSchemaDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, active INTEGER);
		CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id),
			title TEXT
		);`); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDBSchemaListsTablesAndColumns(t *testing.T) {
	root := makeSchemaDB(t)
	out, err := run(t, DBSchema(root), `{"db":"app.db"}`)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Tables []struct {
			Name    string `json:"name"`
			Columns []struct {
				Name       string `json:"name"`
				Type       string `json:"type"`
				PrimaryKey bool   `json:"primary_key"`
				NotNull    bool   `json:"not_null"`
			} `json:"columns"`
			ForeignKeys []struct {
				Column     string `json:"column"`
				References string `json:"references"`
			} `json:"foreign_keys"`
		} `json:"tables"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(res.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d: %s", len(res.Tables), out)
	}

	byName := map[string]int{}
	for i, tbl := range res.Tables {
		byName[tbl.Name] = i
	}
	uIdx, ok := byName["users"]
	if !ok {
		t.Fatalf("users table missing: %s", out)
	}
	users := res.Tables[uIdx]
	if len(users.Columns) != 3 {
		t.Errorf("users should have 3 columns, got %d", len(users.Columns))
	}
	var idCol, nameCol bool
	for _, c := range users.Columns {
		if c.Name == "id" && c.PrimaryKey {
			idCol = true
		}
		if c.Name == "name" && c.NotNull {
			nameCol = true
		}
	}
	if !idCol {
		t.Errorf("id column should be primary key: %s", out)
	}
	if !nameCol {
		t.Errorf("name column should be NOT NULL: %s", out)
	}

	pIdx := byName["posts"]
	posts := res.Tables[pIdx]
	if len(posts.ForeignKeys) != 1 {
		t.Fatalf("posts should have 1 FK, got %d: %s", len(posts.ForeignKeys), out)
	}
	fk := posts.ForeignKeys[0]
	if fk.Column != "user_id" || !strings.Contains(fk.References, "users") {
		t.Errorf("unexpected FK %+v: %s", fk, out)
	}
}

func TestDBSchemaContainsDBPath(t *testing.T) {
	root := makeSchemaDB(t)
	if _, err := run(t, DBSchema(root), `{"db":"../../etc/passwd.db"}`); err == nil {
		t.Error("db path escaping the root should be rejected")
	}
}

func TestDBSchemaMissingDB(t *testing.T) {
	if _, err := run(t, DBSchema(t.TempDir()), `{"db":"nope.db"}`); err == nil {
		t.Error("missing db should error")
	}
}
