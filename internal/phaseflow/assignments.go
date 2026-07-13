package phaseflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// This file defines the machine-readable plan produced by `/project` and
// consumed by orchestrated execution (Spec 2). ROADMAP.md stays the
// human-facing view; assignments.json is the structured contract: for each
// task, which agent runs it, on which model, tailored how, and the acceptance
// criteria the verifier checks it against.

// Task is one unit of work in the plan, keyed to a ROADMAP plan id (e.g.
// "02-01"). Agent names a catalog agent type; Model is a tier ("speed"/"strong")
// or a concrete model name; Status tracks execution progress (owned by Spec 2).
type Task struct {
	ID                 string   `json:"id"`
	Phase              string   `json:"phase"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Agent              string   `json:"agent"`
	AgentAddendum      string   `json:"agent_addendum,omitempty"`
	Model              string   `json:"model"`
	Status             string   `json:"status"`
}

// Task status values. Planning writes StatusPending; execution advances them.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCorrected = "corrected"
)

// Assignments is the full set of task assignments for a project.
type Assignments struct {
	Tasks []Task `json:"tasks"`
}

// AssignmentsPath returns the path to .planning/assignments.json.
func AssignmentsPath(root string) string {
	return filepath.Join(PlanningDir(root), "assignments.json")
}

// LoadAssignments reads assignments.json. A missing file yields an empty
// Assignments and found=false rather than an error, so callers can treat an
// unplanned project uniformly.
func LoadAssignments(root string) (a Assignments, found bool, err error) {
	data, err := os.ReadFile(AssignmentsPath(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Assignments{}, false, nil
		}
		return Assignments{}, false, err
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return Assignments{}, false, err
	}
	return a, true, nil
}

// Save writes the assignments to .planning/assignments.json, creating the
// directory if needed. Output is indented for human diffability.
func (a Assignments) Save(root string) error {
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(AssignmentsPath(root), out, 0o644)
}

// Task returns the task with the given id and whether it was found.
func (a Assignments) Task(id string) (Task, bool) {
	for _, t := range a.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}
