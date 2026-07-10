package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run is a small helper: invoke a tool's Run with JSON args.
func run(t *testing.T, tool Tool, args string) (string, error) {
	t.Helper()
	return tool.Run(context.Background(), json.RawMessage(args))
}

func TestReadFileRange(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\nd\ne\n"), 0o644)
	tool := ReadFileRange(dir)

	// full read
	out, err := run(t, tool, `{"path":"f.txt"}`)
	if err != nil || !strings.Contains(out, "a\nb\nc") {
		t.Fatalf("full read: out=%q err=%v", out, err)
	}
	// range 2-4
	out, err = run(t, tool, `{"path":"f.txt","range_start":2,"range_end":4}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "b\nc\nd") || strings.Contains(out, "\na\n") {
		t.Errorf("range 2-4 = %q", out)
	}
	// line numbers
	out, _ = run(t, tool, `{"path":"f.txt","range_start":1,"range_end":1,"with_line_numbers":true}`)
	if !strings.Contains(out, "1: a") {
		t.Errorf("with_line_numbers = %q", out)
	}
	// binary guard
	os.WriteFile(filepath.Join(dir, "bin"), []byte{0, 1, 2}, 0o644)
	if _, err := run(t, tool, `{"path":"bin"}`); err == nil {
		t.Error("binary file should be rejected")
	}
}

func TestEditFileMultiReplaceAll(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("x x x"), 0o644)
	tool := EditFileMulti(dir)

	// ambiguous without replace_all
	if _, err := run(t, tool, `{"path":"f.txt","old":"x","new":"y"}`); err == nil {
		t.Error("ambiguous single edit should error")
	}
	// replace_all
	out, err := run(t, tool, `{"path":"f.txt","old":"x","new":"y","replace_all":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "3 occurrences") {
		t.Errorf("replace_all msg = %q", out)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "y y y" {
		t.Errorf("content = %q, want 'y y y'", b)
	}
}

func TestFileStat(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\n"), 0o644)
	out, err := run(t, FileStat(dir), `{"path":"f.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"size:", "mode:", "lines:"} {
		if !strings.Contains(out, want) {
			t.Errorf("stat missing %q in:\n%s", want, out)
		}
	}
}

func TestMoveDeleteMkdir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)

	if _, err := run(t, MoveFile(dir), `{"source":"a.txt","dest":"sub/b.txt"}`); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "b.txt")); err != nil {
		t.Error("moved file missing")
	}
	if _, err := run(t, Mkdir(dir), `{"path":"newdir"}`); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "newdir")); err != nil || !fi.IsDir() {
		t.Error("mkdir did not create dir")
	}
	if _, err := run(t, DeleteFile(dir), `{"path":"sub/b.txt"}`); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "b.txt")); !os.IsNotExist(err) {
		t.Error("file not deleted")
	}
}

func TestPatchApplySingleHunk(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0o644)

	patch := "diff --git a/a.txt b/a.txt\n" +
		"@@ -1,3 +1,3 @@\n" +
		" line1\n-line2\n+line2-modified\n line3\n"
	args, _ := json.Marshal(map[string]string{"patch": patch})
	out, err := run(t, PatchApply(dir), string(args))
	if err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	if !strings.Contains(out, "1 files") {
		t.Errorf("patch result = %q", out)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "line1\nline2-modified\nline3\n" {
		t.Errorf("patched content = %q", b)
	}
}

func TestWriteFileSecretWarning(t *testing.T) {
	dir := t.TempDir()
	out, err := run(t, WriteFile(dir), `{"path":"c.txt","content":"token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "secret") {
		t.Errorf("expected a secret warning, got %q", out)
	}
	out, _ = run(t, WriteFile(dir), `{"path":"d.txt","content":"just some ordinary text here"}`)
	if strings.Contains(out, "secret") {
		t.Errorf("clean write should not warn: %q", out)
	}
}
