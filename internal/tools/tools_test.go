package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/internal/safety"
)

func args(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return b
}

func TestSafeJoinRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	// "../" escapes must be rejected.
	for _, rel := range []string{"../escape", "../../etc/passwd"} {
		if _, err := safety.SafeJoin(root, rel); err == nil {
			t.Errorf("SafeJoin(%q) should have failed", rel)
		}
	}
	// Ordinary relative paths are allowed.
	if _, err := safety.SafeJoin(root, "sub/ok.go"); err != nil {
		t.Errorf("SafeJoin(sub/ok.go) failed: %v", err)
	}
	// A leading-slash path is reinterpreted as relative to root and contained.
	got, err := safety.SafeJoin(root, "/etc/passwd")
	if err != nil {
		t.Errorf("SafeJoin(/etc/passwd) failed: %v", err)
	}
	if !strings.HasPrefix(got, root) {
		t.Errorf("SafeJoin(/etc/passwd) = %q, not contained in root", got)
	}
}

func TestWriteAndReadFile(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	_, err := WriteFile(root).Run(ctx, args(t, map[string]string{"path": "pkg/a.txt", "content": "hello"}))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "pkg/a.txt")); string(b) != "hello" {
		t.Errorf("file content = %q", b)
	}
	out, err := ReadFile(root).Run(ctx, args(t, map[string]string{"path": "pkg/a.txt"}))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out != "hello" {
		t.Errorf("read = %q", out)
	}
}

func TestEditFileMatchCounts(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	edit := EditFile(root)

	os.WriteFile(filepath.Join(root, "f.txt"), []byte("one two one"), 0o644)

	// Zero matches -> error.
	if _, err := edit.Run(ctx, args(t, map[string]string{"path": "f.txt", "old": "zzz", "new": "x"})); err == nil {
		t.Error("expected error for zero matches")
	}
	// Multiple matches -> error.
	if _, err := edit.Run(ctx, args(t, map[string]string{"path": "f.txt", "old": "one", "new": "x"})); err == nil {
		t.Error("expected error for multiple matches")
	}
	// Exactly one -> success.
	if _, err := edit.Run(ctx, args(t, map[string]string{"path": "f.txt", "old": "two", "new": "TWO"})); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "f.txt")); string(b) != "one TWO one" {
		t.Errorf("after edit = %q", b)
	}
}

func TestListFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), nil, 0o644)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, ".git", "config"), nil, 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "b.go"), nil, 0o644)

	out, err := ListFiles(root).Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "sub/b.go") {
		t.Errorf("missing files: %q", out)
	}
	if strings.Contains(out, ".git") {
		t.Errorf(".git should be skipped: %q", out)
	}
}

// A pattern beginning with "-" must be treated as a literal search pattern,
// not parsed as a command-line flag (argument injection — e.g. rg's --pre
// preprocessor). With the "--" terminator the search runs and matches the
// literal text rather than erroring on an "unknown flag".
func TestSearchPatternNotTreatedAsFlag(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("this line needs --pre review\n"), 0o644)

	out, err := Search(root).Run(context.Background(), args(t, map[string]string{"pattern": "--pre"}))
	if err != nil {
		t.Fatalf("search with dash-pattern should not error (arg injection guard): %v", err)
	}
	if !strings.Contains(out, "--pre") {
		t.Errorf("expected literal match for %q, got %q", "--pre", out)
	}
}

func TestRunShell(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	sh := RunShell(root, 5*time.Second)

	out, err := sh.Run(ctx, args(t, map[string]string{"command": "echo hi"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hi") || !strings.Contains(out, "[exit 0]") {
		t.Errorf("unexpected output: %q", out)
	}

	// Non-zero exit is reported, not errored.
	out, err = sh.Run(ctx, args(t, map[string]string{"command": "exit 3"}))
	if err != nil {
		t.Fatalf("run exit 3: %v", err)
	}
	if !strings.Contains(out, "[exit 3]") {
		t.Errorf("missing exit code: %q", out)
	}
}

func TestRunShellBlocked(t *testing.T) {
	sh := RunShell(t.TempDir(), time.Second)
	if _, err := sh.Run(context.Background(), args(t, map[string]string{"command": "rm -rf /tmp/x"})); err == nil {
		t.Error("expected blocked command to error")
	}
}

func TestRunShellTimeout(t *testing.T) {
	sh := RunShell(t.TempDir(), 200*time.Millisecond)
	out, err := sh.Run(context.Background(), args(t, map[string]string{"command": "sleep 5"}))
	if err != nil {
		t.Fatalf("timeout run: %v", err)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("expected timeout note: %q", out)
	}
}

func TestDefinitionsShape(t *testing.T) {
	reg := NewRegistry(ReadFile("."), WriteFile("."))
	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2", len(defs))
	}
	for _, d := range defs {
		if d.Type != "function" {
			t.Errorf("type = %q, want function", d.Type)
		}
		if d.Function.Parameters["type"] != "object" {
			t.Errorf("parameters not an object schema: %v", d.Function.Parameters)
		}
	}
	// Must marshal cleanly to JSON.
	if _, err := json.Marshal(defs); err != nil {
		t.Errorf("marshal defs: %v", err)
	}
}
