package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstructionsReadsAndTagsFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("use tabs, not spaces"), 0o644)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("run go test before finishing"), 0o644)

	got := Instructions(dir)
	for _, want := range []string{
		`source="CLAUDE.md"`, "use tabs, not spaces",
		`source="AGENTS.md"`, "run go test before finishing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Instructions missing %q; got:\n%s", want, got)
		}
	}
	// CLAUDE.md comes before AGENTS.md.
	if strings.Index(got, "CLAUDE.md") > strings.Index(got, "AGENTS.md") {
		t.Error("CLAUDE.md should precede AGENTS.md")
	}
}

func TestInstructionsIncludesRepoOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("base rule"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".gophermind"), 0o755)
	os.WriteFile(filepath.Join(dir, ".gophermind", "prompt.md"), []byte("repo override rule"), 0o644)

	got := Instructions(dir)
	if !strings.Contains(got, "repo override rule") {
		t.Errorf("override not included; got:\n%s", got)
	}
	if !strings.Contains(got, `source=".gophermind/prompt.md"`) {
		t.Errorf("override not tagged with its source; got:\n%s", got)
	}
	// Override applies last so it can refine earlier files.
	if strings.Index(got, "base rule") > strings.Index(got, "repo override rule") {
		t.Error("repo override should come after CLAUDE.md")
	}
}

func TestInstructionsEmptyWhenNoneOrBlank(t *testing.T) {
	dir := t.TempDir()
	if got := Instructions(dir); got != "" {
		t.Errorf("no files should give empty, got %q", got)
	}
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("   \n\t"), 0o644)
	if got := Instructions(dir); got != "" {
		t.Errorf("blank file should be skipped, got %q", got)
	}
}
