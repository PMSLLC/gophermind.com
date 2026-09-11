package phaseflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gophermind/internal/lockfile"
)

// This file defines the machine-readable plan produced by `/project` and
// consumed by orchestrated execution (Spec 2). ROADMAP.md stays the
// human-facing view; assignments.json is the structured contract: for each
// task, which agent runs it, on which model, tailored how, and the acceptance
// criteria the verifier checks it against.

// Task is one unit of work in the plan, keyed to a ROADMAP plan id (e.g.
// "02-01"). Agent names a catalog agent type; Model is a tier ("speed"/"strong")
// or a concrete model name; Status tracks execution progress (owned by Spec 2).
//
// Wave, DependsOn, CandidateModels, Attempts and RevisionRounds are additive
// fields for the pipeline runner (see docs/superpowers/specs
// /2026-09-11-harness-pipeline-design.md). Every one is omitempty, so an
// assignments.json written before these existed loads unchanged and gains no
// new keys when it round-trips.
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

	// Wave is this task's execution wave, computed from DependsOn. 0 means
	// unassigned.
	Wave int `json:"wave,omitempty"`
	// DependsOn lists the ids of tasks that must complete before this one
	// starts.
	DependsOn []string `json:"depends_on,omitempty"`
	// CandidateModels is the ordered list of models to try for this task.
	// An empty list falls back to Model.
	CandidateModels []string `json:"candidate_models,omitempty"`
	// Attempts is the history of every model's try at this task, whether it
	// passed or failed. See RecordAttempt for the retention rule.
	Attempts []Attempt `json:"attempts,omitempty"`
	// RevisionRounds is the history of the planner rewriting this task after
	// every candidate model failed it.
	RevisionRounds []Revision `json:"revision_rounds,omitempty"`
	// IsContract marks the task that produces the contract every other task
	// must conform to: schemas, interface signatures, file/module layout,
	// naming conventions. A contract task is written with Wave 0 directly
	// (it has no dependencies to compute a wave from - it is the root every
	// later wave depends on), and Wave 0 always runs at concurrency 1 (see
	// executeOnce), which is what gives it "runs alone, before everything".
	// IsContract itself carries no further behavior in execute.go; it exists
	// so a plan's JSON records which task that solo wave-0 task actually was.
	IsContract bool `json:"is_contract,omitempty"`
}

// Task status values. Planning writes StatusPending; execution advances them.
// StatusNeedsRevision marks a task whose candidate models were all
// exhausted without a pass, awaiting a planner rewrite. StatusEscalated
// marks a task that failed revision too, and needs a human. StatusContractFlagged
// marks a task that determined mid-execution that the contract is wrong or
// incomplete: it does not improvise around that, it raises the flag instead.
// See executeOnce for what a flag does to the wave it occurred in.
const (
	StatusPending         = "pending"
	StatusRunning         = "running"
	StatusDone            = "done"
	StatusFailed          = "failed"
	StatusCorrected       = "corrected"
	StatusNeedsRevision   = "needs_revision"
	StatusEscalated       = "escalated"
	StatusContractFlagged = "contract_flagged"
)

// Attempt is one model's try at a task, recorded whether it passed or
// failed.
type Attempt struct {
	Model     string    `json:"model"`
	StartedAt time.Time `json:"started_at"`
	Duration  string    `json:"duration"`
	// Verdict is "pass" or "fail".
	Verdict string `json:"verdict"`
	// Reason is what specifically failed, never just "failed". The
	// revision circuit breaker (piece 4) needs a specific reason to notice
	// when several models fail the same way.
	Reason string `json:"reason"`
}

// Revision is one round of the planner rewriting a task after every
// candidate model failed.
type Revision struct {
	Round int       `json:"round"`
	At    time.Time `json:"at"`
	// Note describes what changed and why.
	Note        string   `json:"note"`
	Deliverable string   `json:"deliverable,omitempty"`
	Test        []string `json:"test,omitempty"`
	// Attempts are the attempts that led to this revision, moved here when
	// ApplyRevision cleared the task's live list for a fresh count. Without
	// this they would be lost, and the end-of-run report would under-report
	// every model tried before a task was revised: a model that failed three
	// times would show zero attempts, which is exactly the "wins and losses
	// attributed to every model actually tried" the report exists to provide.
	Attempts []Attempt `json:"attempts,omitempty"`
}

// maxAttemptsPerTask caps how many Attempt records a task keeps. An
// unattended run with repeated revisions could otherwise grow
// assignments.json without bound; this ring-style cap is the same precedent
// as the odometer's event ring.
const maxAttemptsPerTask = 50

// RecordAttempt appends a to the task's attempt history, dropping the oldest
// entries once the history exceeds maxAttemptsPerTask so the record stays
// bounded on a long unattended run.
func (t *Task) RecordAttempt(a Attempt) {
	t.Attempts = append(t.Attempts, a)
	if len(t.Attempts) > maxAttemptsPerTask {
		t.Attempts = t.Attempts[len(t.Attempts)-maxAttemptsPerTask:]
	}
}

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
// directory if needed. Output is indented for human diffability. The write
// itself is atomic (temp file, fsync, rename) via internal/lockfile, so a
// crash mid-write cannot leave a torn assignments.json - but Save alone does
// not serialize against another concurrent Save or Update; callers that read
// before they write must use Update instead.
func (a Assignments) Save(root string) error {
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	// Take the same lock Update takes. An atomic write alone stops a torn
	// file, not a lost update: a Save built from a stale read would otherwise
	// overwrite a concurrent Update's result wholesale. Relying on callers to
	// prefer Update would work only until someone reasonably calls Save.
	//
	// Update does its own write inline rather than calling Save, so this lock
	// is never taken twice on one path; flock is not reentrant and that would
	// deadlock.
	release, err := lockfile.Acquire(AssignmentsPath(root) + ".lock")
	if err != nil {
		return fmt.Errorf("phaseflow: acquire assignments lock: %w", err)
	}
	defer release()

	return lockfile.WriteAtomic(AssignmentsPath(root), out, 0o644)
}

// Update applies mutate to the assignments under an exclusive lock and writes
// the result atomically. It is the only safe way to change a task's state
// when more than one task may be running: the load, the mutation and the
// save all happen while holding the same lock, so a concurrent Update cannot
// interleave a stale read between them the way independent
// load-mutate-Save calls can. A missing assignments.json is a clear error
// rather than a silent empty write, since Update always has an existing plan
// to modify.
func Update(root string, mutate func(*Assignments) error) error {
	lockPath := AssignmentsPath(root) + ".lock"
	if err := os.MkdirAll(PlanningDir(root), 0o755); err != nil {
		return err
	}
	release, err := lockfile.Acquire(lockPath)
	if err != nil {
		return fmt.Errorf("phaseflow: acquire assignments lock: %w", err)
	}
	defer release()

	a, found, err := LoadAssignments(root)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("phaseflow: no assignments to update")
	}
	if err := mutate(&a); err != nil {
		return err
	}
	out, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return lockfile.WriteAtomic(AssignmentsPath(root), out, 0o644)
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
