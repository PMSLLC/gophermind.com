package main

import (
	"fmt"
	"os"
	"strings"

	"gophermind/internal/phaseflow"
)

// phaseUsage is printed for `gophermind phase help` and on unknown subcommands.
const phaseUsage = `PhaseFlow — spec-driven development workflow

Usage: gophermind phase <command> [args]

State commands (run locally, no agent):
  init <name>     scaffold .planning/ for a new project
  status          show progress and the current phase (alias: progress)
  next            print the next incomplete phase
  done <plan-id>  mark a plan complete and refresh STATE.md (e.g. 02-01)
  sync            recompute STATE.md position/progress from the roadmap
  archive <ver>   archive a shipped milestone (all phases must be complete)
  commands        list the embedded PhaseFlow command prompts

Loop steps (run the agent with a state-seeded prompt):
  roadmap [desc]  draft the project roadmap (Roadmap)
  plan <phase>    plan the given phase (Plan)
  execute <phase> execute a phase's plans (Execute)
  verify <phase>  verify a phase against its success criteria (Verify)
  milestone       archive completed phases and ship (Milestone)

The loop: Roadmap -> Phases -> Plan -> Execute -> Verify -> Milestone.
`

// runPhase handles the `gophermind phase ...` subcommand. args is the tail after
// "phase" (i.e. os.Args positional args with "phase" removed).
//
// It returns handled=true when it fully served the request locally (a state
// command), in which case the caller should return nil. For an agentic loop
// step it returns handled=false and a seeded task prompt; the caller runs that
// prompt through the standard agent path.
func runPhase(root string, args []string) (handled bool, task string, err error) {
	e := phaseflow.New(root)

	sub := "help"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	rest := strings.TrimSpace(strings.Join(args[1:], " "))

	switch sub {
	case "init":
		if rest == "" {
			return true, "", fmt.Errorf("usage: gophermind phase init <project-name>")
		}
		if err := e.Init(rest); err != nil {
			return true, "", err
		}
		fmt.Fprintf(os.Stderr, "✓ initialized PhaseFlow project %q under %s/\n", rest, phaseflow.PlanningDirName)
		fmt.Fprintln(os.Stderr, "next: gophermind phase roadmap")
		return true, "", nil

	case "status", "progress":
		out, err := e.Status()
		if err != nil {
			return true, "", err
		}
		fmt.Print(out)
		return true, "", nil

	case "next":
		p, err := e.Next()
		if err != nil {
			return true, "", err
		}
		if p == nil {
			fmt.Println("All phases complete — run `gophermind phase milestone` to ship.")
			return true, "", nil
		}
		fmt.Printf("Next: Phase %s — %s\n", p.Number, p.Name)
		return true, "", nil

	case "done":
		if rest == "" {
			return true, "", fmt.Errorf("usage: gophermind phase done <plan-id>")
		}
		if err := e.CompletePlan(rest); err != nil {
			return true, "", err
		}
		fmt.Fprintf(os.Stderr, "✓ marked plan %s complete\n", rest)
		out, err := e.Status()
		if err == nil {
			fmt.Print(out)
		}
		return true, "", nil

	case "sync":
		if err := e.SyncState("manual sync"); err != nil {
			return true, "", err
		}
		fmt.Fprintln(os.Stderr, "✓ STATE.md synced to the roadmap")
		return true, "", nil

	case "archive":
		if rest == "" {
			return true, "", fmt.Errorf("usage: gophermind phase archive <version> [name]")
		}
		version, name, _ := strings.Cut(rest, " ")
		summary, err := e.ArchiveMilestone(version, strings.TrimSpace(name))
		if err != nil {
			return true, "", err
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", summary)
		return true, "", nil

	case "commands", "list":
		for _, n := range phaseflow.CommandNames() {
			fmt.Println(n)
		}
		return true, "", nil

	case "help", "-h", "--help":
		fmt.Fprint(os.Stderr, phaseUsage)
		return true, "", nil

	case "roadmap", "plan", "execute", "verify", "milestone":
		prompt, err := e.BuildStepPrompt(sub, rest)
		if err != nil {
			return true, "", err
		}
		return false, prompt, nil

	default:
		// Any other embedded PhaseFlow command (map-codebase, code-review,
		// ship, …) runs agentically by name; unrecognized names error.
		if _, ok := phaseflow.Command(sub); ok {
			prompt, err := e.BuildCommandPrompt(sub, rest)
			if err != nil {
				return true, "", err
			}
			return false, prompt, nil
		}
		return true, "", fmt.Errorf("unknown phase subcommand %q\n\n%s", sub, phaseUsage)
	}
}
