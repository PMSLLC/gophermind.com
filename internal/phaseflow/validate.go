package phaseflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// This file gates `/project`: the plan is only approvable once it is fully
// populated — real phases and tasks (no template placeholders), and every task
// carrying acceptance criteria, an assigned agent, and a model. Approval is a
// simple on-disk marker the TUI checks before letting a phase run.

// approvedMarker is the file whose presence means the plan has been approved.
const approvedMarker = ".outline-approved"

// approvedPath returns the approval marker path for a project root.
func approvedPath(root string) string {
	return filepath.Join(PlanningDir(root), approvedMarker)
}

// Approved reports whether the project's plan has been approved.
func (e *Engine) Approved() bool {
	_, err := os.Stat(approvedPath(e.Root))
	return err == nil
}

// Approve marks the plan approved, unlocking the phase commands behind the gate.
func (e *Engine) Approve() error {
	if err := os.MkdirAll(PlanningDir(e.Root), 0o755); err != nil {
		return err
	}
	return os.WriteFile(approvedPath(e.Root), []byte("approved\n"), 0o644)
}

// Unapprove clears the approval, re-locking the gate. A missing marker is fine.
func (e *Engine) Unapprove() error {
	err := os.Remove(approvedPath(e.Root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// PlanReport summarizes whether a plan is complete enough to approve.
type PlanReport struct {
	Phases   int
	Tasks    int
	Issues   []string
	Complete bool
}

// rePlaceholder matches template placeholder tokens like "[Name]" or
// "[One-line description]" that must not survive into a real plan.
var rePlaceholder = regexp.MustCompile(`\[[^\]]*\]`)

// hasPlaceholder reports whether s still contains a template placeholder or a
// literal "TBD".
func hasPlaceholder(s string) bool {
	return rePlaceholder.MatchString(s) || strings.Contains(strings.ToUpper(s), "TBD")
}

// ValidatePlan checks that the roadmap and assignments form an approvable plan:
// at least one phase and task, no leftover placeholders, and every task with
// acceptance criteria, a known catalog agent, and a model. It returns a report
// listing every issue found; Complete is true only when there are none.
func (e *Engine) ValidatePlan() (PlanReport, error) {
	var rep PlanReport
	rm, err := LoadRoadmap(e.Root)
	if err != nil {
		return rep, err
	}
	rep.Phases = len(rm.Phases)
	if rep.Phases == 0 {
		rep.Issues = append(rep.Issues, "roadmap has no phases")
	}
	for i := range rm.Phases {
		p := &rm.Phases[i]
		if hasPlaceholder(p.Name) || p.Name == "" {
			rep.Issues = append(rep.Issues, fmt.Sprintf("phase %s has a placeholder name", p.Number))
		}
		if hasPlaceholder(p.Goal) {
			rep.Issues = append(rep.Issues, fmt.Sprintf("phase %s has a placeholder goal", p.Number))
		}
		if len(p.Plans) == 0 {
			rep.Issues = append(rep.Issues, fmt.Sprintf("phase %s has no plans", p.Number))
		}
		rep.Tasks += len(p.Plans)
	}
	if rep.Tasks == 0 {
		rep.Issues = append(rep.Issues, "plan has no tasks")
	}

	// Known catalog agents (if a catalog exists) to validate assignments against.
	known := map[string]bool{}
	if cat, found, err := LoadCatalog(e.Root); err == nil && found {
		for _, a := range cat {
			known[a.Name] = true
		}
	}

	assign, found, err := LoadAssignments(e.Root)
	if err != nil {
		return rep, err
	}
	if !found {
		rep.Issues = append(rep.Issues, "no assignments.json (tasks not assigned to agents)")
	}
	byID := map[string]Task{}
	for _, tk := range assign.Tasks {
		byID[tk.ID] = tk
	}
	// Every roadmap plan must have a well-formed assignment.
	for i := range rm.Phases {
		for _, pl := range rm.Phases[i].Plans {
			tk, ok := byID[pl.ID]
			if !ok {
				rep.Issues = append(rep.Issues, fmt.Sprintf("task %s has no assignment", pl.ID))
				continue
			}
			if len(tk.AcceptanceCriteria) == 0 {
				rep.Issues = append(rep.Issues, fmt.Sprintf("task %s has no acceptance criteria", pl.ID))
			}
			if strings.TrimSpace(tk.Agent) == "" {
				rep.Issues = append(rep.Issues, fmt.Sprintf("task %s has no agent", pl.ID))
			} else if len(known) > 0 && !known[tk.Agent] {
				rep.Issues = append(rep.Issues, fmt.Sprintf("task %s assigns unknown agent %q", pl.ID, tk.Agent))
			}
			if strings.TrimSpace(tk.Model) == "" {
				rep.Issues = append(rep.Issues, fmt.Sprintf("task %s has no model", pl.ID))
			}
		}
	}

	rep.Complete = len(rep.Issues) == 0
	return rep, nil
}
