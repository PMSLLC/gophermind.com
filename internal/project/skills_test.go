package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsInjectsFiles(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, ".gophermind", "skills")
	os.MkdirAll(skills, 0o755)
	os.WriteFile(filepath.Join(skills, "deploy.md"), []byte("# Deploy\nRun make release."), 0o644)
	os.WriteFile(filepath.Join(skills, "review.md"), []byte("# Review\nCheck for N+1 queries."), 0o644)

	got := Skills(dir)
	if !strings.Contains(got, "Run make release.") || !strings.Contains(got, "Check for N+1 queries.") {
		t.Errorf("skills content missing:\n%s", got)
	}
	if !strings.Contains(got, `name="deploy"`) || !strings.Contains(got, `name="review"`) {
		t.Errorf("skills not tagged with names:\n%s", got)
	}
	// Sorted: deploy before review.
	if strings.Index(got, "deploy") > strings.Index(got, "review") {
		t.Error("skills not in sorted order")
	}
}

func TestSkillsEmptyWhenNoDir(t *testing.T) {
	if got := Skills(t.TempDir()); got != "" {
		t.Errorf("no skills dir should give empty, got %q", got)
	}
}

func TestSkillsSkipsBlankAndNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, ".gophermind", "skills")
	os.MkdirAll(skills, 0o755)
	os.WriteFile(filepath.Join(skills, "blank.md"), []byte("   \n"), 0o644)
	os.WriteFile(filepath.Join(skills, "notes.txt"), []byte("ignore me"), 0o644)
	if got := Skills(dir); got != "" {
		t.Errorf("blank/non-md should yield empty, got %q", got)
	}
}
