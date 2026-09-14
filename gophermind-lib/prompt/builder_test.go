package prompt

import (
	"strings"
	"testing"
)

func TestDefaultBuilder(t *testing.T) {
	b, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	out := b.Build()
	for _, want := range []string{"<role>", "GopherMind", "<workflow>", "<safety>", "<rules>"} {
		if !strings.Contains(out, want) {
			t.Errorf("default prompt missing %q", want)
		}
	}
	// Empty context: project_context/skills sections should be dropped.
	if strings.Contains(out, "<project_context>") {
		t.Errorf("empty project_context should be omitted:\n%s", out)
	}
}

func TestBuilderInjectsProjectContextAndSkills(t *testing.T) {
	b, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	out := b.WithContext("ProjectInstructions", "use tabs").WithContext("Skills", "deploy skill").Build()
	if !strings.Contains(out, "<project_context>") || !strings.Contains(out, "use tabs") {
		t.Errorf("project context not injected:\n%s", out)
	}
	if !strings.Contains(out, "<skills>") || !strings.Contains(out, "deploy skill") {
		t.Errorf("skills not injected:\n%s", out)
	}
}

func TestLoadBuilderMissingFile(t *testing.T) {
	if _, err := LoadBuilder("/nonexistent/template.md"); err == nil {
		t.Error("missing template file should error")
	}
}

func TestLoadBuilderCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/custom.md"
	if err := writeFile(path, "<role>\nCustom agent.\n</role>"); err != nil {
		t.Fatal(err)
	}
	b, err := LoadBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.Build(), "Custom agent.") {
		t.Errorf("custom template not loaded: %s", b.Build())
	}
}
