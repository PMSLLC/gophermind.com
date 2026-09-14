package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListFilesGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644)
	tool := ListFilesGlob(dir)

	all, err := run(t, tool, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, "a.go") || !strings.Contains(all, "b.txt") {
		t.Errorf("list all = %q", all)
	}
	only, err := run(t, tool, `{"include":"*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(only, "a.go") || strings.Contains(only, "b.txt") {
		t.Errorf("include *.go = %q", only)
	}
}

func TestSearchEnhanced(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("func foo() {}\nfunc bar() {}\n"), 0o644)
	out, err := run(t, SearchEnhanced(dir), `{"pattern":"func bar"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "func bar") {
		t.Errorf("search result = %q", out)
	}
}

// TestSearchEnhancedPathDirectory reproduces a bug hit live in production: a
// model passed "path" as a plain directory ("gophermind-lib/serve") expecting
// it to scope the search there, matching the tool's own schema description
// ("Optional path/glob filter (e.g. '*.go', 'src/')"). The old code always
// passed path through rg's -g flag, which is a filename-glob filter -- a bare
// directory with no wildcard never matches anything via -g, so rg exited 2
// with "No files were searched", and the search tool failed on every call
// that scoped by directory rather than by glob.
func TestSearchEnhancedPathDirectory(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "code.go"), []byte("type Deps struct{}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("type Deps struct{}\n"), 0o644)

	out, err := run(t, SearchEnhanced(dir), `{"pattern":"type Deps struct","path":"sub"}`)
	if err != nil {
		t.Fatalf("search scoped to directory: %v", err)
	}
	if !strings.Contains(out, "sub/code.go") {
		t.Errorf("expected a match under sub/, got %q", out)
	}
	if strings.Count(out, "type Deps struct") != 1 {
		t.Errorf("expected exactly the sub/ match, not the root one too: %q", out)
	}
}

// TestSearchEnhancedPathGlob confirms a real glob (containing a wildcard)
// still filters by filename via -g, unaffected by the directory-path fix.
func TestSearchEnhancedPathGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("needle\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("needle\n"), 0o644)

	out, err := run(t, SearchEnhanced(dir), `{"pattern":"needle","path":"*.go"}`)
	if err != nil {
		t.Fatalf("search with glob path: %v", err)
	}
	if !strings.Contains(out, "a.go") || strings.Contains(out, "b.txt") {
		t.Errorf("glob path filter = %q, want only a.go", out)
	}
}

func TestRunShellEnhanced(t *testing.T) {
	dir := t.TempDir()
	out, err := run(t, RunShellEnhanced(dir, 10_000_000_000, ShellLimits{}), `{"command":"echo hello-shell"}`)
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	if !strings.Contains(out, "hello-shell") {
		t.Errorf("shell output = %q", out)
	}
}
