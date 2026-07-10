package tools

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLQueryCacheHit(t *testing.T) {
	root := makeTestDB(t)
	t.Setenv("GOPHERMIND_SQL_CACHE_DIR", t.TempDir())
	dbPath := filepath.Join(root, "app.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	q := `{"db":"app.db","query":"SELECT count(*) FROM users"}`
	out1, err := run(t, SQLQuery(root), q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out1, "3") {
		t.Fatalf("expected count 3:\n%s", out1)
	}

	// Corrupt the db file but preserve its mtime; a cache miss would now fail to
	// open a valid database, so a matching result proves the cache was used.
	if err := os.WriteFile(dbPath, []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dbPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	out2, err := run(t, SQLQuery(root), q)
	if err != nil {
		t.Fatalf("second query should be served from cache, got error: %v", err)
	}
	if out2 != out1 {
		t.Errorf("cached result differs:\n%q\nvs\n%q", out1, out2)
	}
}

func TestSQLQueryCacheInvalidatedOnChange(t *testing.T) {
	root := makeTestDB(t)
	t.Setenv("GOPHERMIND_SQL_CACHE_DIR", t.TempDir())

	q := `{"db":"app.db","query":"SELECT count(*) FROM users"}`
	if out, err := run(t, SQLQuery(root), q); err != nil || !strings.Contains(out, "3") {
		t.Fatalf("first query: out=%q err=%v", out, err)
	}

	// Mutate the db (new row -> new mtime); the cache key must change.
	time.Sleep(10 * time.Millisecond)
	db, err := sql.Open("sqlite", filepath.Join(root, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (name, active) VALUES ('dave', 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out, err := run(t, SQLQuery(root), q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "4") {
		t.Errorf("expected fresh count 4 after mutation:\n%s", out)
	}
}
