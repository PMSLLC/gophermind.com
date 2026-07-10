package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstructionsExpandsIncludes(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".gophermind", "fragments"), 0o755)
	os.WriteFile(filepath.Join(dir, ".gophermind", "fragments", "style.md"), []byte("prefer small diffs"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Base rules.\n{{include: .gophermind/fragments/style.md}}\nEnd."), 0o644)

	got := Instructions(dir)
	if !strings.Contains(got, "prefer small diffs") {
		t.Errorf("include not expanded:\n%s", got)
	}
	if strings.Contains(got, "{{include") {
		t.Errorf("include directive left unexpanded:\n%s", got)
	}
	// Surrounding text is preserved.
	if !strings.Contains(got, "Base rules.") || !strings.Contains(got, "End.") {
		t.Errorf("surrounding text lost:\n%s", got)
	}
}

func TestIncludeMissingFileLeavesMarker(t *testing.T) {
	dir := t.TempDir()
	out := expandIncludes(dir, "before {{include: nope.md}} after", 0)
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Errorf("text damaged: %q", out)
	}
	if !strings.Contains(out, "missing") {
		t.Errorf("missing include should be noted: %q", out)
	}
}

func TestIncludeCannotEscapeRoot(t *testing.T) {
	dir := t.TempDir()
	out := expandIncludes(dir, "{{include: ../../etc/passwd}}", 0)
	if strings.Contains(out, "root:") {
		t.Errorf("include escaped the root: %q", out)
	}
}

func TestIncludeDepthLimit(t *testing.T) {
	dir := t.TempDir()
	// a.md includes b.md which includes a.md — must terminate.
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("A {{include: b.md}}"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("B {{include: a.md}}"), 0o644)
	out := expandIncludes(dir, "{{include: a.md}}", 0)
	if !strings.Contains(out, "A") || !strings.Contains(out, "B") {
		t.Errorf("expected partial expansion: %q", out)
	}
	// Must not hang or blow the stack; reaching here means it terminated.
}
