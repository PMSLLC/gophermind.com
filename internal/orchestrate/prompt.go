package orchestrate

import (
	"fmt"
	"strings"

	"gophermind/internal/phaseflow"
)

// buildTaskPrompts assembles the system and user prompts for one task's fresh
// agent: system is the catalog agent's body plus the task's AgentAddendum (if
// any); user is a rendered instruction from the task's title/phase/description
// plus its acceptance criteria as a bullet list.
func buildTaskPrompts(t phaseflow.Task, catalogBody string) (system, user string) {
	system = catalogBody
	if strings.TrimSpace(t.AgentAddendum) != "" {
		system = system + "\n\n" + t.AgentAddendum
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (phase %s): %s\n\n%s\n", t.ID, t.Phase, t.Title, t.Description)
	b.WriteString("\nAcceptance criteria:\n")
	for _, c := range t.AcceptanceCriteria {
		fmt.Fprintf(&b, "- %s\n", c)
	}
	user = b.String()
	return system, user
}
