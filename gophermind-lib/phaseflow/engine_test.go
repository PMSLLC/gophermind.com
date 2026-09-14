package phaseflow

import (
	"os"
	"strings"
	"testing"
)

func TestInitScaffolds(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	if e.Initialized() {
		t.Fatal("fresh dir should not be initialized")
	}
	if err := e.Init("My Project"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !e.Initialized() {
		t.Fatal("should be initialized after Init")
	}
	for _, p := range []string{RoadmapPath(root), StatePath(root), ProjectPath(root), ConfigPath(root)} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", p, err)
		}
	}
	// Roadmap must carry the project name and parse cleanly.
	rm, err := LoadRoadmap(root)
	if err != nil {
		t.Fatalf("load roadmap: %v", err)
	}
	if rm.Title != "My Project" {
		t.Errorf("title = %q", rm.Title)
	}
}

func TestInitRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	if err := e.Init("A"); err != nil {
		t.Fatal(err)
	}
	if err := e.Init("B"); err == nil {
		t.Error("second init should refuse to overwrite")
	}
}

func TestInitRequiresName(t *testing.T) {
	if err := New(t.TempDir()).Init("  "); err == nil {
		t.Error("empty project name should error")
	}
}

func TestStatusRendersProgress(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RoadmapPath(root), []byte(sampleRoadmap), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := New(root).Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "Demo Project") {
		t.Error("status should name the project")
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("status should show 50%% progress:\n%s", out)
	}
	// Phase 2 is the current (first incomplete) phase.
	if !strings.Contains(out, "[>] 2") {
		t.Errorf("status should mark phase 2 current:\n%s", out)
	}
}

func TestStatusUninitialized(t *testing.T) {
	if _, err := New(t.TempDir()).Status(); err == nil {
		t.Error("status on uninitialized project should error")
	}
}

func TestBuildStepPromptSeedsContext(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(RoadmapPath(root), []byte(sampleRoadmap), 0o644)

	prompt, err := New(root).BuildStepPrompt("execute", "2 --tdd")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(prompt, "<phaseflow-context>") {
		t.Error("prompt should include context block")
	}
	if !strings.Contains(prompt, "Arguments: 2 --tdd") {
		t.Error("prompt should carry arguments")
	}
	// $ARGUMENTS in the command body should be substituted.
	if strings.Contains(prompt, "$ARGUMENTS") {
		t.Error("$ARGUMENTS placeholder should be substituted")
	}
	// The upstream command body should be present.
	if !strings.Contains(strings.ToLower(prompt), "wave") {
		t.Error("execute-phase body should be included")
	}
}

func TestBuildStepPromptUnknownStep(t *testing.T) {
	if _, err := New(t.TempDir()).BuildStepPrompt("bogus", ""); err == nil {
		t.Error("unknown step should error")
	}
}

func TestBuildStepPromptRoadmapAllowedUninitialized(t *testing.T) {
	// roadmap is the bootstrap step and must work before init.
	if _, err := New(t.TempDir()).BuildStepPrompt("roadmap", "greenfield app"); err != nil {
		t.Errorf("roadmap step should work on a fresh dir: %v", err)
	}
}

func TestBuildStepPromptNeedsInit(t *testing.T) {
	if _, err := New(t.TempDir()).BuildStepPrompt("execute", "1"); err == nil {
		t.Error("execute on uninitialized project should error")
	}
}

func TestBuildCommandPromptArbitrary(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	if err := e.Init("Demo"); err != nil {
		t.Fatal(err)
	}
	// A non-loop embedded command should seed by name.
	prompt, err := e.BuildCommandPrompt("map-codebase", "")
	if err != nil {
		t.Fatalf("map-codebase: %v", err)
	}
	if !strings.Contains(prompt, "<phaseflow-context>") {
		t.Error("arbitrary command should still get the context block")
	}
	// The "phase:" prefix is tolerated.
	if _, err := e.BuildCommandPrompt("phase:code-review", ""); err != nil {
		t.Errorf("phase: prefix should resolve: %v", err)
	}
	// An unknown command errors.
	if _, err := e.BuildCommandPrompt("definitely-not-a-command", ""); err == nil {
		t.Error("unknown command should error")
	}
}
