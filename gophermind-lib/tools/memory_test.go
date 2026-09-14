package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/embed"
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

func TestRememberFactStampsValidFrom(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")

	if _, err := run(t, RememberFact(fakeEmbed{}, memPath), `{"text":"the parser lives in template.go"}`); err != nil {
		t.Fatal(err)
	}

	idx, err := embed.LoadIndex(memPath)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if len(idx.Vectors) != 1 {
		t.Fatalf("want 1 fact, got %d", len(idx.Vectors))
	}
	if idx.Vectors[0].ValidFrom == "" {
		t.Error("a remembered fact should be stamped with when it became true")
	}
	if idx.Vectors[0].ValidUntil != "" {
		t.Errorf("a newly remembered fact should be live, got ValidUntil %q", idx.Vectors[0].ValidUntil)
	}
}

func TestRememberFactSupersedesNearDuplicate(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")
	tool := RememberFact(fakeEmbed{}, memPath)

	if _, err := run(t, tool, `{"text":"the parser lives in template.go"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, tool, `{"text":"the database uses sqlite"}`); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, tool, `{"text":"the parser moved to parse.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "superseded") {
		t.Errorf("result should report what it retired, got %q", out)
	}

	idx, err := embed.LoadIndex(memPath)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	live := map[string]bool{}
	for _, v := range idx.Vectors {
		if v.ValidUntil == "" {
			live[v.Text] = true
		}
	}
	if live["the parser lives in template.go"] {
		t.Error("the superseded parser fact should no longer be live")
	}
	if !live["the parser moved to parse.go"] {
		t.Error("the new fact should be live")
	}
	if !live["the database uses sqlite"] {
		t.Error("an unrelated fact should stay live")
	}
	if len(idx.Vectors) != 3 {
		t.Errorf("superseded facts should stay on disk for history, got %d vectors", len(idx.Vectors))
	}
}

func TestRememberProfileSupersedesNearDuplicate(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	tool := RememberProfile(fakeEmbed{}, profilePath)

	if _, err := run(t, tool, `{"text":"the user prefers the parser in Go"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, tool, `{"text":"the user now prefers the parser in Rust"}`); err != nil {
		t.Fatal(err)
	}

	idx, err := embed.LoadIndex(profilePath)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if idx.Vectors[0].ValidUntil == "" {
		t.Error("profile memory should supersede near-duplicates too")
	}
}
