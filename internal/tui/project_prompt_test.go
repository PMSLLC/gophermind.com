package tui

import (
	"strings"
	"testing"

	"gophermind/internal/phaseflow"
)

// The generation prompt is the only thing that makes a planner emit the
// fields the wave scheduler reads. Without depends_on every task lands in
// wave 0 and the plan runs strictly one task at a time, however independent
// the work is. This pins that the prompt asks for them.
func TestGenerationPromptAsksForTheSchedulingFields(t *testing.T) {
	got := generationPrompt("demo", []phaseflow.CatalogAgent{{Name: "coder", DefaultModel: "speed", Description: "writes code"}})
	for _, want := range []string{
		"depends_on",
		"is_contract",
		"CONCURRENTLY",
		"No cycles",
		"exactly ONE task",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generation prompt does not mention %q", want)
		}
	}
	// And it still asks for everything it asked for before.
	for _, want := range []string{"acceptance_criteria", "agent_addendum", "SPEC.md", "ROADMAP.md", "assignments.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("generation prompt lost %q", want)
		}
	}
}
