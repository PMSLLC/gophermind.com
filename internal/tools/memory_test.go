package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRememberFactAppends(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "mem.json")

	if _, err := run(t, RememberFact(fakeEmbed{}, memPath), `{"text":"the parser lives in template.go"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, RememberFact(fakeEmbed{}, memPath), `{"text":"the database uses sqlite"}`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("memory store not written: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "template.go") || !strings.Contains(s, "sqlite") {
		t.Errorf("both facts should be persisted:\n%s", s)
	}
}

func TestRememberFactNilProvider(t *testing.T) {
	if _, err := run(t, RememberFact(nil, filepath.Join(t.TempDir(), "m.json")), `{"text":"x"}`); err == nil {
		t.Error("nil provider should error with configuration guidance")
	}
}

func TestRememberFactEmpty(t *testing.T) {
	if _, err := run(t, RememberFact(fakeEmbed{}, filepath.Join(t.TempDir(), "m.json")), `{"text":"  "}`); err == nil {
		t.Error("empty fact should error")
	}
}

func TestRememberProfileUsesOwnStore(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.json")
	tool := RememberProfile(fakeEmbed{}, profilePath)
	if tool.Name != "remember_profile" {
		t.Errorf("tool name = %q, want remember_profile", tool.Name)
	}
	if _, err := run(t, tool, `{"text":"the user prefers Go"}`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("profile store not written: %v", err)
	}
	if !strings.Contains(string(data), "prefers Go") {
		t.Errorf("profile fact not persisted:\n%s", data)
	}
}
