package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchAtTurn(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "src",
		`{"role":"system","content":"s"}`+"\n"+
			`{"role":"user","content":"one"}`+"\n"+
			`{"role":"assistant","content":"1"}`+"\n"+
			`{"role":"user","content":"two"}`+"\n"+
			`{"role":"assistant","content":"2"}`+"\n")

	if err := branchIn(dir, "src", "fork", 3); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "fork.jsonl"))
	if err != nil {
		t.Fatalf("fork not created: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("fork has %d messages, want 3", len(lines))
	}
	if !strings.Contains(lines[2], `"1"`) || strings.Contains(string(data), "two") {
		t.Errorf("branch kept the wrong turns:\n%s", data)
	}
	// The source is untouched.
	src, _ := os.ReadFile(filepath.Join(dir, "src.jsonl"))
	if !strings.Contains(string(src), "two") {
		t.Error("source session was modified")
	}
}

func TestBranchFullCopyWhenTurnOutOfRange(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s", `{"role":"system","content":"s"}`+"\n"+`{"role":"user","content":"u"}`+"\n")
	// atTurn <= 0 or beyond length copies the whole session.
	if err := branchIn(dir, "s", "all", 0); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "all.jsonl"))
	if !strings.Contains(string(data), "u") {
		t.Errorf("full-copy branch missing content:\n%s", data)
	}
}

func TestBranchValidatesIDsAndSource(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "ok", `{"role":"system","content":"s"}`+"\n")
	if err := branchIn(dir, "ok", "../escape", 1); err == nil {
		t.Error("invalid new id must be rejected")
	}
	if err := branchIn(dir, "missing", "new", 1); err == nil {
		t.Error("missing source must error")
	}
}
