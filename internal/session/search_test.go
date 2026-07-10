package session

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSessionLines(t *testing.T, dir, id string, lines ...string) {
	t.Helper()
	var body string
	for _, ln := range lines {
		body += ln + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSearchDirFindsMatches(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "s1",
		`{"role":"user","content":"help me fix the parser bug"}`,
		`{"role":"assistant","content":"sure, the parser is in template.go"}`)
	writeSessionLines(t, dir, "s2",
		`{"role":"user","content":"add a caching layer"}`)

	hits, err := searchDir(dir, "parser")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 matching session, got %d: %+v", len(hits), hits)
	}
	if hits[0].ID != "s1" {
		t.Errorf("wrong session matched: %s", hits[0].ID)
	}
	if hits[0].Matches == 0 {
		t.Errorf("expected match count > 0: %+v", hits[0])
	}
	if hits[0].Snippet == "" {
		t.Errorf("expected a snippet: %+v", hits[0])
	}
}

func TestSearchDirCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "s1", `{"role":"user","content":"The PARSER Broke"}`)
	hits, err := searchDir(dir, "parser")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Errorf("case-insensitive search failed: %+v", hits)
	}
}

func TestSearchDirSkipsEncrypted(t *testing.T) {
	dir := t.TempDir()
	// An encrypted session (magic prefix) must not be scanned as plaintext.
	if err := os.WriteFile(filepath.Join(dir, "enc.jsonl"), append([]byte(encMagic), []byte("parser")...), 0o600); err != nil {
		t.Fatal(err)
	}
	hits, err := searchDir(dir, "parser")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("encrypted session should be skipped, got %+v", hits)
	}
}

func TestSearchDirNoMatch(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "s1", `{"role":"user","content":"hello"}`)
	hits, err := searchDir(dir, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("expected no hits, got %+v", hits)
	}
}
