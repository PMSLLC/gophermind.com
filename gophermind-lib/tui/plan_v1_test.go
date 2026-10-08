package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanV1CommandsRenamed(t *testing.T) {
	have := map[string]bool{}
	for _, c := range slashCommands {
		have[c.Name] = true
	}
	for _, want := range []string{"/plan-v1", "/plan-v1-execute"} {
		if !have[want] {
			t.Errorf("registry lacks %s", want)
		}
		if !strings.Contains(helpLine(), want) {
			t.Errorf("helpLine lacks %s", want)
		}
	}
	if have["/project-execute"] {
		t.Error("registry still lists /project-execute")
	}

	// A bare /plan-v1 reaches handleProjectCommand, which asks for the name.
	t.Chdir(t.TempDir())
	m := testModel(t)
	m.input.SetValue("/plan-v1")
	m2, _ := m.handleSubmit()
	if m2.proj != projAwaitName {
		t.Errorf("/plan-v1 did not reach handleProjectCommand (proj = %v)", m2.proj)
	}

	// /plan-v1-execute reaches the execute handler, whose first gate is the approval check.
	m = testModel(t)
	m.input.SetValue("/plan-v1-execute")
	m3, _ := m.handleSubmit()
	if !strings.Contains(m3.content, "not approved") {
		t.Errorf("/plan-v1-execute did not reach the execute handler:\n%s", m3.content)
	}
}

func TestPlanV1PrintsDeprecation(t *testing.T) {
	first := func(s string) string { return strings.SplitN(s, "\n", 2)[0] }

	dir := t.TempDir()
	t.Chdir(dir)
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("a CLI tool"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m2, _ := m.startProject("demo", brief)
	if got := first(m2.content); got != planV1Deprecation {
		t.Errorf("startProject first line = %q", got)
	}

	m = testModel(t)
	m3, _ := m.handleProjectExecuteCommand()
	if got := first(m3.content); got != planV1Deprecation {
		t.Errorf("execute first line = %q", got)
	}
}
