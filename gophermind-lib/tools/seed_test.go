package tools

import (
	"strings"
	"testing"
)

func TestSeedDataGeneratesInserts(t *testing.T) {
	root := makeSchemaDB(t) // users(id PK, name NOT NULL, active), posts(...)
	out, err := run(t, SeedData(root), `{"db":"app.db","table":"users","rows":3}`)
	if err != nil {
		t.Fatal(err)
	}
	// Three INSERT statements targeting users, omitting the autoincrement PK.
	if n := strings.Count(out, `INSERT INTO "users"`); n != 3 {
		t.Errorf("expected 3 INSERT statements, got %d:\n%s", n, out)
	}
	if strings.Contains(out, "id") {
		t.Errorf("integer primary key should be omitted (autoincrement):\n%s", out)
	}
	// The NOT NULL text column must be present with a quoted value.
	if !strings.Contains(out, "name") || !strings.Contains(out, "'") {
		t.Errorf("expected quoted text value for name:\n%s", out)
	}
}

func TestSeedDataDefaultRows(t *testing.T) {
	root := makeSchemaDB(t)
	out, err := run(t, SeedData(root), `{"db":"app.db","table":"users"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, `INSERT INTO "users"`) == 0 {
		t.Errorf("default row count should generate at least one insert:\n%s", out)
	}
}

func TestSeedDataUnknownTable(t *testing.T) {
	root := makeSchemaDB(t)
	if _, err := run(t, SeedData(root), `{"db":"app.db","table":"nope"}`); err == nil {
		t.Error("unknown table should error")
	}
}

func TestSeedDataContainsPath(t *testing.T) {
	root := makeSchemaDB(t)
	if _, err := run(t, SeedData(root), `{"db":"../../x.db","table":"users"}`); err == nil {
		t.Error("db path escaping the root should be rejected")
	}
}
