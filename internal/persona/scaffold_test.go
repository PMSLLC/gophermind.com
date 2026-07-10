package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldWritesTemplate(t *testing.T) {
	root := t.TempDir()
	path, err := Scaffold(root, "mentor")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, filepath.Join(".gophermind", "personas", "mentor.md")) {
		t.Errorf("unexpected scaffold path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("scaffold file not written: %v", err)
	}
	if !strings.Contains(string(data), "mentor") {
		t.Errorf("template should mention the persona name:\n%s", data)
	}
}

func TestScaffoldRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	if _, err := Scaffold(root, "dup"); err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(root, "dup"); err == nil {
		t.Error("scaffolding over an existing persona should error")
	}
}

func TestScaffoldRejectsBadName(t *testing.T) {
	if _, err := Scaffold(t.TempDir(), "../evil"); err == nil {
		t.Error("bad persona name should be rejected")
	}
}

func TestResolvePrefersBuiltinThenCustom(t *testing.T) {
	root := t.TempDir()

	// Built-in still resolves.
	if _, ok := Resolve(root, "reviewer"); !ok {
		t.Error("built-in reviewer should resolve")
	}

	// A custom persona resolves after scaffolding + editing.
	path, _ := Scaffold(root, "mentor")
	if err := os.WriteFile(path, []byte("Be a patient mentor."), 0o644); err != nil {
		t.Fatal(err)
	}
	text, ok := Resolve(root, "mentor")
	if !ok {
		t.Fatal("custom mentor persona should resolve")
	}
	if !strings.Contains(text, "patient mentor") {
		t.Errorf("custom persona content wrong: %q", text)
	}

	// Unknown resolves to nothing.
	if _, ok := Resolve(root, "nope"); ok {
		t.Error("unknown persona should not resolve")
	}
}
