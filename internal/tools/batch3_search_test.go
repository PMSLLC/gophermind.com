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

func TestRunShellEnhanced(t *testing.T) {
	dir := t.TempDir()
	out, err := run(t, RunShellEnhanced(dir, 10_000_000_000), `{"command":"echo hello-shell"}`)
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	if !strings.Contains(out, "hello-shell") {
		t.Errorf("shell output = %q", out)
	}
}
