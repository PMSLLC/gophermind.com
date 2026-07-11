package phaseflow

import (
	"fmt"
	"os"
	"strings"
)

// Engine runs the PhaseFlow loop for a single project root. State operations
// (init, status, next, marking work done) are deterministic and run entirely in
// Go. Agentic steps (plan, execute, verify, milestone, roadmap) are turned into
// a state-seeded prompt via BuildStepPrompt for gophermind's agent to run.
type Engine struct {
	Root string
}

// New returns an Engine rooted at root (the project directory).
func New(root string) *Engine { return &Engine{Root: root} }

// Initialized reports whether the project has a .planning/ROADMAP.md.
func (e *Engine) Initialized() bool {
	_, err := os.Stat(RoadmapPath(e.Root))
	return err == nil
}

// Next returns the next incomplete phase, or nil when the roadmap is complete.
func (e *Engine) Next() (*Phase, error) {
	rm, err := LoadRoadmap(e.Root)
	if err != nil {
		return nil, err
	}
	return rm.NextPhase(), nil
}

// progressBar renders a fixed-width unicode bar for a 0..100 percentage.
func progressBar(pct int) string {
	const width = 10
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// Status renders a human-readable summary of the project's position: overall
// progress and a per-phase list marking the current phase.
func (e *Engine) Status() (string, error) {
	if !e.Initialized() {
		return "", fmt.Errorf("no PhaseFlow project here — run `phase init <name>` first")
	}
	rm, err := LoadRoadmap(e.Root)
	if err != nil {
		return "", err
	}
	done, total := rm.TotalPlans()
	next := rm.NextPhase()

	var b strings.Builder
	title := rm.Title
	if title == "" {
		title = "(untitled)"
	}
	fmt.Fprintf(&b, "Project: %s\n", title)
	fmt.Fprintf(&b, "Progress: %s %d%% (%d/%d plans)\n", progressBar(rm.Percent()), rm.Percent(), done, total)
	b.WriteString("Phases:\n")
	for i := range rm.Phases {
		p := &rm.Phases[i]
		pd, pt := 0, len(p.Plans)
		for _, pl := range p.Plans {
			if pl.Done {
				pd++
			}
		}
		mark := " "
		if p.Done {
			mark = "x"
		} else if next != nil && p.Number == next.Number {
			mark = ">"
		}
		name := p.Name
		if p.Inserted {
			name += " (inserted)"
		}
		fmt.Fprintf(&b, "  [%s] %-4s %-24s %d/%d plans\n", mark, p.Number.String(), name, pd, pt)
	}
	if next == nil {
		b.WriteString("\nAll phases complete. Run `phase milestone` to ship.\n")
	} else {
		fmt.Fprintf(&b, "\nCurrent: Phase %s — %s\n", next.Number, next.Name)
	}
	return b.String(), nil
}

// agenticSteps maps a loop step name to the embedded command asset that drives
// it. These are the steps BuildStepPrompt seeds for the agent.
var agenticSteps = map[string]string{
	"roadmap":   "new-project",
	"plan":      "plan-phase",
	"execute":   "execute-phase",
	"verify":    "verify-work",
	"milestone": "complete-milestone",
}

// IsAgenticStep reports whether step is one that BuildStepPrompt can seed.
func IsAgenticStep(step string) bool {
	_, ok := agenticSteps[step]
	return ok
}

// BuildStepPrompt constructs the prompt for an agentic loop step. It loads the
// upstream command asset for the step, substitutes $ARGUMENTS, and prepends a
// context block describing the project's current state and configuration. The
// returned string is what the caller hands to gophermind's agent to execute.
func (e *Engine) BuildStepPrompt(step, args string) (string, error) {
	cmd, ok := agenticSteps[step]
	if !ok {
		return "", fmt.Errorf("unknown step %q (want one of: roadmap, plan, execute, verify, milestone)", step)
	}
	// roadmap runs on a fresh project; the rest need an initialized one.
	if step != "roadmap" && !e.Initialized() {
		return "", fmt.Errorf("no PhaseFlow project here — run `phase init <name>` first")
	}
	asset, ok := Command(cmd)
	if !ok {
		return "", fmt.Errorf("embedded command %q missing", cmd)
	}

	ctx, err := e.contextBlock(step, args)
	if err != nil {
		return "", err
	}
	body := strings.ReplaceAll(asset.Body, "$ARGUMENTS", args)
	return ctx + "\n\n" + body, nil
}

// contextBlock builds the <phaseflow-context> preamble injected ahead of a
// step's command body, giving the agent the project state it needs.
func (e *Engine) contextBlock(step, args string) (string, error) {
	var b strings.Builder
	b.WriteString("<phaseflow-context>\n")
	fmt.Fprintf(&b, "You are running the PhaseFlow \"%s\" step via gophermind.\n", step)
	fmt.Fprintf(&b, "Workflow root: %s/\n", PlanningDirName)
	if args != "" {
		fmt.Fprintf(&b, "Arguments: %s\n", args)
	}

	cfg, _, err := LoadConfig(e.Root)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "Config: mode=%s granularity=%s research=%t plan_check=%t verifier=%t\n",
		cfg.Mode, cfg.Granularity, cfg.Workflow.Research, cfg.Workflow.PlanCheck, cfg.Workflow.Verifier)

	if e.Initialized() {
		if status, err := e.Status(); err == nil {
			b.WriteString("\nCurrent state:\n")
			b.WriteString(status)
		}
	}
	b.WriteString("</phaseflow-context>")
	return b.String(), nil
}
