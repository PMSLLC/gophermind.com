package ui

import (
	"strings"
	"testing"
)

func TestBreakdownSeedPrompt_IncludesBriefContent(t *testing.T) {
	prompt := BreakdownSeedPrompt("my-project", "Build a todo app with auth.")
	if !strings.Contains(prompt, "Build a todo app with auth.") {
		t.Errorf("prompt does not contain the brief content:\n%s", prompt)
	}
}

func TestBreakdownSeedPrompt_NamesTheProject(t *testing.T) {
	prompt := BreakdownSeedPrompt("my-project", "brief text")
	if !strings.Contains(prompt, "my-project") {
		t.Errorf("prompt does not name the project:\n%s", prompt)
	}
}

// TestBreakdownSeedPrompt_NamesThePlanFiles covers "seeds session with
// structured prompt" -- the produced plan must land in the same three
// files (.planning/SPEC.md, ROADMAP.md, assignments.json) the pipeline
// endpoints (GET /pipeline/state, GET /pipeline/report) already read, or
// the panel this task builds would have nothing to display.
func TestBreakdownSeedPrompt_NamesThePlanFiles(t *testing.T) {
	prompt := BreakdownSeedPrompt("my-project", "brief text")
	for _, want := range []string{"SPEC.md", "ROADMAP.md", "assignments.json"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not mention %q:\n%s", want, prompt)
		}
	}
}

func TestBreakdownSeedPrompt_AsksForInterviewBeforePlanning(t *testing.T) {
	prompt := BreakdownSeedPrompt("my-project", "brief text")
	if !strings.Contains(prompt, "question") {
		t.Errorf("prompt does not ask a scoping question before planning:\n%s", prompt)
	}
}
