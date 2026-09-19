package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// Pass-2 defaults, sized so the worst-case prompt stays near 27,000 bytes.
const (
	defaultStepsPerPass = 6
	defaultBriefBytes   = 4000
)

// siblingListCapBytes bounds the list of every step of the task in a prompt.
const siblingListCapBytes = 3000

// fit makes a node field safe for a prompt: one line, at most n bytes.
func fit(s string, n int) string { return cutBytes(oneLine(s), n) }

// stepTag tells the model whether a sibling can be depended on.
func stepTag(s plantree.Node) string {
	switch {
	case onHold(s):
		return "on hold: " + string(s.Status)
	case s.Planning.Stage == plantree.StageDrafted || s.Planning.Stage == plantree.StageApproved:
		return "specified"
	case s.Planning.Stage == plantree.StageAwaitingAnswers:
		return "waiting for an answer"
	}
	return "to specify"
}

func stepList(steps []plantree.Node) string {
	var b strings.Builder
	omitted := 0
	for _, s := range steps {
		line := "- " + s.ID + ": " + fit(s.Title, 200) + " [" + stepTag(s) + "]\n"
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

// Pass2Input is everything one specification pass shows the model. Facts and
// Excerpts may be empty. Nothing about any other task belongs here.
type Pass2Input struct {
	Project   string
	Overview  string
	Facts     string // project facts: language, build and test commands, layout
	Decisions string // answers already given that affect this task
	Excerpts  string // brief excerpts that produced the task
	Phase     plantree.Node
	Task      plantree.Node
	Siblings  []plantree.Node // every step of the task
	Batch     []plantree.Node // the steps to specify now
}

// Pass2Prompt builds the prompt for one specification pass. It carries the
// overview, the project facts, the phase and task the steps belong to, the
// list of the task's steps, the steps to specify now, and optionally excerpts
// of the brief. It carries nothing about any other task.
func Pass2Prompt(in Pass2Input) string {
	phase, task, siblings, batch := in.Phase, in.Task, in.Siblings, in.Batch
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing the work specification for some steps of ONE task in a project plan for %q. You see only this task.\n\n", fit(in.Project, 100))
	b.WriteString("Running overview of the whole project:\n")
	b.WriteString(orNone(in.Overview))
	b.WriteString("\n\nRepository facts (language, build and test commands, layout):\n")
	if strings.TrimSpace(in.Facts) == "" {
		b.WriteString("(not provided)\n")
	} else {
		b.WriteString(cutBytes(strings.TrimSpace(in.Facts), FactsCapBytes) + "\n")
	}
	b.WriteString("\nDecisions already made by the project owner (follow them; do not ask again):\n")
	if strings.TrimSpace(in.Decisions) == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(in.Decisions + "\n")
	}
	fmt.Fprintf(&b, "\nPhase: %s\nWhy: %s\nObjective: %s\n", fit(phase.Title, 200), fit(phase.ContextDigest, 500), orNone(fit(phase.Objective, 1000)))
	fmt.Fprintf(&b, "\nTask: %s\nWhy: %s\nObjective: %s\n", fit(task.Title, 200), fit(task.ContextDigest, 500), orNone(fit(task.Objective, 1000)))
	b.WriteString("\nAll steps of this task, in order. A step may depend only on an EARLIER step in this list:\n")
	b.WriteString(stepList(siblings))
	b.WriteString("\nSteps to specify now:\n")
	for _, s := range batch {
		fmt.Fprintf(&b, "- %s: %s. Why: %s\n", s.ID, fit(s.Title, 200), fit(s.ContextDigest, 500))
	}
	b.WriteString("\nBrief excerpts that produced this task (context only, may be partial):\n")
	if strings.TrimSpace(in.Excerpts) == "" {
		b.WriteString("(not available)\n")
	} else {
		fmt.Fprintf(&b, "<<<BRIEF EXCERPTS\n%s\nBRIEF EXCERPTS>>>\n", in.Excerpts)
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Return exactly the steps listed under \"Steps to specify now\", each once, using its id.\n")
	b.WriteString("- description: what to build or change, concrete enough that an agent can start without asking.\n")
	b.WriteString("- target_paths: repository-relative files or directories the step touches. Never absolute, never containing \"..\".\n")
	b.WriteString("- acceptance_criteria: 1 to 10 checks a reviewer can verify.\n")
	b.WriteString("- test_command: the command as an array of arguments that verifies the step, taken from the repository facts; if the facts do not say, use an empty array rather than guessing.\n")
	b.WriteString("- depends_on: ids of EARLIER steps of this task that must be done first, or an empty array.\n")
	b.WriteString("- description: at most 2000 characters. Each acceptance criterion: at most 300 characters, and at most 10 criteria.\n")
	b.WriteString("- target_paths: at most 20 paths of at most 300 characters each. test_command: at most 20 arguments of at most 200 characters each.\n")
	b.WriteString("- In every array, never put an empty string.\n")
	b.WriteString("- Do not depend on a step marked on hold.\n")
	b.WriteString("- If you cannot specify a step because something is unknown that only the project owner can decide, do not guess: leave that step out of \"steps\" and ask a question whose \"affects\" lists its id (affects are ids of steps under \"Steps to specify now\", never empty). At most 5 questions, each with 2 to 8 options (or none for a free-text question) and \"recommended\" (option labels) if you have one. Never ask what the decisions above or the brief already answer.\n")
	b.WriteString("- Do not invent scope the task does not need. Do not call tools. Reply with ONE JSON object and nothing else, in this shape:\n")
	b.WriteString(`{"steps":[{"id":"<step id>","description":"<what to build>","target_paths":["<path/to/file>"],"acceptance_criteria":["<a check a reviewer can verify>"],"test_command":["<command>","<arg>"],"depends_on":[]}],"questions":[]}`)
	b.WriteString("\n")
	return b.String()
}
