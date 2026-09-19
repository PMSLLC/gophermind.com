package export

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree/plan"
)

// roadmapMarkdown renders ROADMAP.md exactly as phaseflow's own parser reads
// it: a title, a phase list of summary checkboxes, and a detail section per
// phase with a **Goal** and a Plans list of "NN-MM" checkboxes.
func roadmapMarkdown(p legacyPlan, overview string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Roadmap: %s\n\n", p.Project)
	b.WriteString("Generated from the plan tree in .planning/plan by /project. Edit the\n")
	b.WriteString("plan through that flow, not by hand: the next export overwrites this file.\n\n")

	b.WriteString("## Overview\n\n")
	if q := quote(cutBytes(strings.TrimSpace(overview), overviewBytes)); q != "" {
		b.WriteString(q)
		b.WriteString("\n\n")
	} else {
		b.WriteString("> No overview was recorded.\n\n")
	}

	b.WriteString("## Phases\n\n")
	for _, ph := range p.Phases {
		fmt.Fprintf(&b, "- [ ] **Phase %d: %s** - %s\n", ph.Number, ph.Name, ph.Goal)
	}
	b.WriteString("\n## Phase Details\n")
	for _, ph := range p.Phases {
		fmt.Fprintf(&b, "\n### Phase %d: %s\n", ph.Number, ph.Name)
		fmt.Fprintf(&b, "**Goal**: %s\n", ph.Goal)
		if ph.Number == 1 {
			b.WriteString("**Depends on**: Nothing (first phase)\n")
		} else {
			fmt.Fprintf(&b, "**Depends on**: Phase %d\n", ph.Number-1)
		}
		b.WriteString("\nPlans:\n")
		for _, t := range ph.Tasks {
			fmt.Fprintf(&b, "- [ ] %s: %s\n", t.ID, fit(t.Title, planDescriptionBytes))
		}
	}

	b.WriteString("\n## Progress\n\n")
	b.WriteString("| Phase | Plans Complete | Status | Completed |\n")
	b.WriteString("|-------|----------------|--------|-----------|\n")
	for _, ph := range p.Phases {
		fmt.Fprintf(&b, "| %d. %s | 0/%d | Not started | - |\n", ph.Number, strings.ReplaceAll(ph.Name, "|", "/"), len(ph.Tasks))
	}
	return b.String()
}

// specMarkdown renders SPEC.md: what the plan is for, what the repository is,
// what the owner decided, and the scope as phases and tasks. It is prose for
// a person and for the executor's context, not a file anything parses, so
// nothing here has to match a format.
func specMarkdown(p legacyPlan, overview, facts string, decisions []plan.Question) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: specification\n\n", p.Project)
	b.WriteString("Generated from the plan tree in .planning/plan by /project. The tree is\n")
	b.WriteString("canonical; this file is rewritten by the next export.\n\n")

	b.WriteString("## Overview\n\n")
	if o := cutBytes(strings.TrimSpace(overview), overviewBytes); o != "" {
		b.WriteString(o + "\n\n")
	} else {
		b.WriteString("No overview was recorded.\n\n")
	}

	b.WriteString("## Repository facts\n\n")
	if f := cutBytes(strings.TrimSpace(facts), factsBytes); f != "" {
		b.WriteString(f + "\n\n")
	} else {
		b.WriteString("None recorded. Every test command below came from the plan alone.\n\n")
	}

	b.WriteString("## Decisions the owner made\n\n")
	if len(decisions) == 0 {
		b.WriteString("None: nothing in the brief needed a decision.\n\n")
	} else {
		for _, q := range decisions {
			fmt.Fprintf(&b, "- **%s** %s\n", q.ID, fit(q.Question, 300))
			fmt.Fprintf(&b, "  - decided: %s\n", fit(answerText(q), 300))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Scope: %d phase(s), %d task(s), %d step(s)\n", len(p.Phases), countTasks(p), p.Steps)
	for _, ph := range p.Phases {
		fmt.Fprintf(&b, "\n### Phase %d: %s\n\n%s\n\n", ph.Number, ph.Name, ph.Goal)
		for _, t := range ph.Tasks {
			fmt.Fprintf(&b, "- %s %s (%d step(s), %d acceptance criteria)\n", t.ID, t.Title, len(t.StepIDs), len(t.Criteria))
		}
	}
	return b.String()
}

// answerText renders a question's answer as one readable phrase: the labels
// chosen, then the note, whichever of them there is.
func answerText(q plan.Question) string {
	if q.Answer == nil {
		return "(not answered)"
	}
	labels := map[string]string{}
	for _, o := range q.Options {
		labels[o.ID] = o.Label
	}
	var parts []string
	for _, id := range q.Answer.OptionIDs {
		if l := labels[id]; l != "" {
			parts = append(parts, l)
		} else {
			parts = append(parts, id)
		}
	}
	chosen := strings.Join(parts, ", ")
	note := strings.TrimSpace(q.Answer.Text)
	switch {
	case chosen != "" && note != "":
		return chosen + " (" + note + ")"
	case chosen != "":
		return chosen
	case note != "":
		return note
	}
	return "(no choice and no note)"
}

func countTasks(p legacyPlan) int {
	n := 0
	for _, ph := range p.Phases {
		n += len(ph.Tasks)
	}
	return n
}
