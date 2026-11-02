package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAliasesMissingFileIsEmpty(t *testing.T) {
	a, err := LoadAliases(t.TempDir())
	if err != nil || len(a) != 0 {
		t.Fatalf("got %v, %v", a, err)
	}
	if got := a.Resolve("gm-2026-10-29-001"); got != "gm-2026-10-29-001" {
		t.Errorf("unknown resolved to %q", got)
	}
}

func TestAliasesSaveLoadResolve(t *testing.T) {
	dir := t.TempDir()
	in := Aliases{"gm-2026-10-29-001": "csvstat-v1-20261029-r001"}
	if err := SaveAliases(dir, in); err != nil {
		t.Fatal(err)
	}
	a, err := LoadAliases(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Resolve("gm-2026-10-29-001"); got != "csvstat-v1-20261029-r001" {
		t.Errorf("resolved to %q", got)
	}
	if got := a.Resolve("gm-2026-10-29-002"); got != "gm-2026-10-29-002" {
		t.Errorf("unknown resolved to %q", got)
	}
}

func TestAliasesBadFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, AliasesFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAliases(dir); err == nil {
		t.Error("bad JSON accepted")
	}
}
