package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindSymbol(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(
		"package a\n\nfunc Widget() int { return 1 }\n\ntype Gadget struct{}\n\nfunc use() { Widget() }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.py"), []byte(
		"def widget():\n    pass\n\nclass Gadget:\n    pass\n"), 0o644)
	// ignored dir must not be searched
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "c.go"), []byte("func Widget() {}\n"), 0o644)

	tool := FindSymbol(dir)

	// Go func + method receiver style: find the definition, not the call site.
	out, err := run(t, tool, `{"name":"Widget"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "func Widget()") {
		t.Errorf("Widget definition not found: %q", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Errorf("should not search ignored dirs: %q", out)
	}
	// The bare call `Widget()` inside use() must not be reported as a definition.
	if strings.Contains(out, "func use()") {
		t.Errorf("call site wrongly reported: %q", out)
	}

	// type/class across languages
	out, _ = run(t, tool, `{"name":"Gadget"}`)
	if !strings.Contains(out, "type Gadget") || !strings.Contains(out, "class Gadget") {
		t.Errorf("Gadget defs (go type + py class) not both found: %q", out)
	}
}

func TestFindSymbolNoMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	out, err := run(t, FindSymbol(dir), `{"name":"Nope"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no definitions") {
		t.Errorf("expected no-match message, got %q", out)
	}
}

func TestFindSymbolValidatesName(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{`{"name":""}`, `{"name":"a b"}`, `{"name":"foo("}`} {
		if _, err := run(t, FindSymbol(dir), bad); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}
