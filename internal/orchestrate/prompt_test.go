package orchestrate

import (
	"strings"
	"testing"

	"gophermind/internal/phaseflow"
)

func TestBuildTaskPromptsSystemIncludesCatalogBodyAndAddendum(t *testing.T) {
	task := phaseflow.Task{
		ID:            "02-01",
		Phase:         "02",
		Title:         "Add login form",
		Description:   "Build the login form component.",
		AgentAddendum: "Use functional components only.",
	}
	system, _ := buildTaskPrompts(task, "You are the coder agent.")

	if !strings.Contains(system, "You are the coder agent.") {
		t.Errorf("system prompt missing catalog body: %q", system)
	}
	if !strings.Contains(system, "Use functional components only.") {
		t.Errorf("system prompt missing agent addendum: %q", system)
	}
}

func TestBuildTaskPromptsSystemOmitsAddendumWhenEmpty(t *testing.T) {
	task := phaseflow.Task{ID: "02-01", Title: "Add login form"}
	system, _ := buildTaskPrompts(task, "You are the coder agent.")

	if strings.Count(system, "You are the coder agent.") != 1 {
		t.Errorf("expected catalog body to appear exactly once, got: %q", system)
	}
}

func TestBuildTaskPromptsUserIncludesTitleDescriptionAndCriteria(t *testing.T) {
	task := phaseflow.Task{
		ID:          "02-01",
		Phase:       "02",
		Title:       "Add login form",
		Description: "Build the login form component.",
		AcceptanceCriteria: []string{
			"Form has email and password fields",
			"Submit button is disabled until valid",
		},
	}
	_, user := buildTaskPrompts(task, "catalog body")

	if !strings.Contains(user, task.Title) {
		t.Errorf("user prompt missing title: %q", user)
	}
	if !strings.Contains(user, task.Description) {
		t.Errorf("user prompt missing description: %q", user)
	}
	for _, c := range task.AcceptanceCriteria {
		if !strings.Contains(user, c) {
			t.Errorf("user prompt missing acceptance criterion %q: %q", c, user)
		}
	}
}
