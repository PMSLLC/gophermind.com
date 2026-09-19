package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// Pass-2 defaults, sized so the worst-case prompt stays near 25,000 bytes.
const (
	defaultStepsPerPass = 6
	defaultBriefBytes   = 6000
)

// siblingListCapBytes bounds the list of every step of the task in a prompt.
const siblingListCapBytes = 4000

func stepList(steps []plantree.Node) string {
	var b strings.Builder
	omitted := 0
	for _, s := range steps {
		line := "- " + s.ID + ": " + oneLine(s.Title) + "\n"
		if b.Len()+len(line) > siblingListCapBytes {
			omitted++
			continue
		}
		b.WriteString(line)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  (%d more steps not shown)\n", omitted)
	}
	return b.String()
}

// Pass2Prompt builds the prompt for one specification pass. It carries the
// overview, the phase and task the steps belong to, the list of the task's
// steps, the steps to specify now, and optionally excerpts of the brief. It
// carries nothing about any other task.
func Pass2Prompt(project, overview string, phase, task plantree.Node, siblings, batch []plantree.Node, excerpts string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing the work specification for some steps of ONE task in a project plan for %q. You see only this task.\n\n", project)
	b.WriteString("Running overview of the whole project:\n")
	b.WriteString(orNone(overview))
	fmt.Fprintf(&b, "\n\nPhase: %s\nWhy: %s\nObjective: %s\n", oneLine(phase.Title), oneLine(phase.ContextDigest), orNone(oneLine(phase.Objective)))
	fmt.Fprintf(&b, "\nTask: %s\nWhy: %s\nObjective: %s\n", oneLine(task.Title), oneLine(task.ContextDigest), orNone(oneLine(task.Objective)))
	b.WriteString("\nAll steps of this task, in order. A step may depend only on an EARLIER step in this list:\n")
	b.WriteString(stepList(siblings))
	b.WriteString("\nSteps to specify now:\n")
	for _, s := range batch {
		fmt.Fprintf(&b, "- %s: %s. Why: %s\n", s.ID, oneLine(s.Title), oneLine(s.ContextDigest))
	}
	b.WriteString("\nBrief excerpts that produced this task (context only, may be partial):\n")
	if strings.TrimSpace(excerpts) == "" {
		b.WriteString("(not available)\n")
	} else {
		fmt.Fprintf(&b, "<<<BRIEF EXCERPTS\n%s\nBRIEF EXCERPTS>>>\n", excerpts)
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Return exactly the steps listed under \"Steps to specify now\", each once, using its id.\n")
	b.WriteString("- description: what to build or change, concrete enough that an agent can start without asking.\n")
	b.WriteString("- target_paths: repository-relative files or directories the step touches. Never absolute, never containing \"..\".\n")
	b.WriteString("- acceptance_criteria: 1 to 10 checks a reviewer can verify.\n")
	b.WriteString("- test_command: the command as an array of arguments that verifies the step, or an empty array if there is none.\n")
	b.WriteString("- depends_on: ids of EARLIER steps of this task that must be done first, or an empty array.\n")
	b.WriteString("- Do not invent scope the task does not need. Do not call tools. Reply with ONE JSON object and nothing else, in this shape:\n")
	b.WriteString(`{"steps":[{"id":"","description":"","target_paths":[""],"acceptance_criteria":[""],"test_command":[""],"depends_on":[""]}]}`)
	b.WriteString("\n")
	return b.String()
}
