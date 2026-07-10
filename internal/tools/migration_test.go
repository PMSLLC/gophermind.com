package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCreateMigration(t *testing.T) {
	dir := t.TempDir()
	out, err := run(t, CreateMigration(dir), `{"name":"Add Users Table"}`)
	if err != nil {
		t.Fatal(err)
	}
	// A file named <timestamp>_add_users_table.sql should exist under migrations/.
	entries, _ := os.ReadDir(filepath.Join(dir, "migrations"))
	if len(entries) != 1 {
		t.Fatalf("want 1 migration file, got %d", len(entries))
	}
	name := entries[0].Name()
	if !regexp.MustCompile(`^\d{14}_add_users_table\.sql$`).MatchString(name) {
		t.Errorf("unexpected migration filename %q", name)
	}
	if !strings.Contains(out, name) {
		t.Errorf("result should mention the created path: %q", out)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "migrations", name))
	if !strings.Contains(string(body), "-- +migrate Up") || !strings.Contains(string(body), "-- +migrate Down") {
		t.Errorf("migration missing up/down sections:\n%s", body)
	}
}

func TestCreateMigrationCustomDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, CreateMigration(dir), `{"name":"x","dir":"db/migrate"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "db", "migrate")); err != nil {
		t.Errorf("custom migration dir not created: %v", err)
	}
}

func TestCreateMigrationValidatesName(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, CreateMigration(dir), `{"name":""}`); err == nil {
		t.Error("empty name should error")
	}
}

func TestCreateMigrationContained(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, CreateMigration(dir), `{"name":"x","dir":"../../etc"}`); err == nil {
		t.Error("dir escaping the root should be rejected")
	}
}
