package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readLines(t *testing.T, dir, id string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestMergeCombinesDivergentTails(t *testing.T) {
	dir := t.TempDir()
	// a and b share a 2-line prefix, then diverge.
	writeSessionLines(t, dir, "a",
		`{"role":"user","content":"shared 1"}`,
		`{"role":"assistant","content":"shared 2"}`,
		`{"role":"user","content":"a-only"}`)
	writeSessionLines(t, dir, "b",
		`{"role":"user","content":"shared 1"}`,
		`{"role":"assistant","content":"shared 2"}`,
		`{"role":"user","content":"b-only"}`)

	if err := mergeIn(dir, "a", "b", "merged"); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, dir, "merged")
	// shared prefix (2) + a tail (1) + b tail (1) = 4
	if len(lines) != 4 {
		t.Fatalf("expected 4 merged lines, got %d: %v", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "a-only") || !strings.Contains(joined, "b-only") {
		t.Errorf("both divergent tails should be present:\n%s", joined)
	}
	// The shared prefix must not be duplicated.
	if strings.Count(joined, "shared 1") != 1 {
		t.Errorf("shared prefix duplicated:\n%s", joined)
	}
}

func TestMergeIdenticalSessions(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "a", `{"role":"user","content":"x"}`)
	writeSessionLines(t, dir, "b", `{"role":"user","content":"x"}`)
	if err := mergeIn(dir, "a", "b", "m"); err != nil {
		t.Fatal(err)
	}
	if lines := readLines(t, dir, "m"); len(lines) != 1 {
		t.Errorf("identical sessions should merge to 1 line, got %v", lines)
	}
}

func TestMergeRejectsBadID(t *testing.T) {
	if err := mergeIn(t.TempDir(), "../a", "b", "m"); err == nil {
		t.Error("bad id should be rejected")
	}
}

func TestMergeMissingSession(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "a", `{"role":"user","content":"x"}`)
	if err := mergeIn(dir, "a", "nope", "m"); err == nil {
		t.Error("missing session should error")
	}
}
