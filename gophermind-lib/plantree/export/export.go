package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

// The agent and model every exported task gets. The tree has no home for
// either (its node schema is closed: unknown fields are rejected and the
// version is pinned at 4), so they are assigned here, at export, from one
// default rather than stored per node. Choosing an agent per task, and a
// model per task, is a later layer.
//
// "executor" is the catalog agent that implements a task; it is what
// SeedCatalog writes as executor.prompt.md, and phaseflow's validator refuses
// an agent the catalog does not have.
const (
	DefaultAgent = "executor"
	DefaultModel = phaseflow.ModelStrong
)

// SpecFileName is the generated specification, written beside ROADMAP.md.
const SpecFileName = "SPEC.md"

// ErrExecutionStarted is returned when the existing assignments.json holds a
// task that is no longer pending. Overwriting it would throw away what a run
// has already recorded, so the export refuses instead.
var ErrExecutionStarted = errors.New("export: the existing plan is already being executed")

// ErrNotValid is returned when the generated files do not pass phaseflow's
// own validator. The message lists every issue.
var ErrNotValid = errors.New("export: the generated plan is not valid")

// ErrNotApproved is returned when the plan tree has not been approved. The
// export never approves anything itself: the flow is plan.Approve, then
// ExportLegacy.
var ErrNotApproved = errors.New("export: the plan has not been approved; approve the plan first (every step specified and reviewed), then export")

// ErrNoAgent is returned when the project's agent catalog cannot supply
// DefaultAgent, so every exported task would fail phaseflow's validator. The
// owner's catalog is never overwritten to fix it.
var ErrNoAgent = errors.New("export: the agent catalog has no " + DefaultAgent + " agent")

// Report is what one ExportLegacy call produced.
type Report struct {
	Phases int
	Tasks  int
	Steps  int
	// SeededAgents is how many catalog agent files the export had to write
	// because the project had no catalog yet.
	SeededAgents int
	// Paths lists the files written, for the transcript.
	Paths []string
	// Replaced lists the files that already existed and were overwritten by
	// this export, so the caller can announce it. PROJECT.md appears only when
	// it already held a managed block, which is what was replaced; any
	// hand-written text around the block is left untouched.
	Replaced []string
}

// ExportLegacy writes the approved plan tree out as the legacy planning
// artifacts under root: .planning/ROADMAP.md, .planning/assignments.json,
// .planning/SPEC.md, the PROJECT.md managed block, and the approval marker
// that lets /project-execute run. repo must be the tree of that same root,
// which is plantree.Open(phaseflow.PlanningDir(root)).
//
// Every task is exported with no dependencies. The legacy executor schedules
// from depends_on, so a plan without it lands entirely in wave 0 and runs one
// task at a time: correct, and slower than it could be. Task dependencies and
// waves are a later layer; the tree records dependencies only between the
// steps of one task, which do not survive the fold into a task row.
//
// It requires an approved tree and never approves one itself: it returns
// ErrNotApproved unless every step is approved and reviewed, and it verifies
// the tree before any write. It holds the plan's run lock for the whole export,
// so a planning pass in another process yields plan.ErrRunBusy.
//
// It is re-runnable: exporting again before anything has executed overwrites
// the files cleanly, ids and all, and lists what it replaced in
// Report.Replaced. Once a task is no longer pending it refuses with
// ErrExecutionStarted rather than discarding a run's record.
//
// The order is: take the lock, check the tree, render, refuse early if
// anything is missing (including the catalog agent), remove any earlier
// approval marker, write the files, run phaseflow's validator as the gate, and
// only then write the approval marker. The marker is removed before the first
// write, so a failed or crashed export never leaves an approval standing over
// a replaced, unvalidated pair of files; it leaves the generated files and no
// marker, and the next export replaces them.
func ExportLegacy(repo *plantree.Repo, root string) (Report, error) {
	release, err := plan.AcquireRun(repo)
	if err != nil {
		return Report{}, err
	}
	defer release()
	if err := repo.Verify(); err != nil {
		return Report{}, err
	}
	sum, err := repo.Summarize(plantree.RootID)
	if err != nil {
		return Report{}, err
	}
	if sum.Leaves == 0 || sum.Status != plantree.StatusReviewed {
		return Report{}, ErrNotApproved
	}
	if err := refuseIfRunning(root); err != nil {
		return Report{}, err
	}
	p, err := read(repo)
	if err != nil {
		return Report{}, err
	}
	if len(p.Phases) == 0 {
		return Report{}, errors.New("export: the plan has no phases")
	}
	if countTasks(p) == 0 {
		return Report{}, errors.New("export: the plan has no tasks")
	}
	overview, err := plan.ReadOverview(repo.Dir())
	if err != nil {
		return Report{}, err
	}
	facts, err := plan.ReadFacts(repo)
	if err != nil {
		return Report{}, err
	}
	decisions, err := answeredQuestions(repo)
	if err != nil {
		return Report{}, err
	}
	seed, err := checkCatalog(root)
	if err != nil {
		return Report{}, err
	}

	rep := Report{Phases: len(p.Phases), Tasks: countTasks(p), Steps: p.Steps}
	specPath := filepath.Join(phaseflow.PlanningDir(root), SpecFileName)
	for _, path := range []string{phaseflow.RoadmapPath(root), specPath, phaseflow.AssignmentsPath(root)} {
		if _, err := os.Stat(path); err == nil {
			rep.Replaced = append(rep.Replaced, path)
		}
	}
	if doc, err := os.ReadFile(phaseflow.ProjectDocPath(root)); err == nil {
		begin := strings.Index(string(doc), "gophermind:spec:begin")
		end := strings.Index(string(doc), "gophermind:spec:end")
		if begin >= 0 && end > begin {
			rep.Replaced = append(rep.Replaced, phaseflow.ProjectDocPath(root))
		}
	}

	e := phaseflow.New(root)
	if err := e.Unapprove(); err != nil {
		return rep, err
	}
	if seed {
		n, err := phaseflow.SeedCatalog(root)
		if err != nil {
			return rep, err
		}
		rep.SeededAgents = n
	}

	if err := os.MkdirAll(phaseflow.PlanningDir(root), 0o755); err != nil {
		return rep, err
	}
	if err := writeFile(phaseflow.RoadmapPath(root), roadmapMarkdown(p, overview)); err != nil {
		return rep, err
	}
	rep.Paths = append(rep.Paths, phaseflow.RoadmapPath(root))

	if err := writeFile(specPath, specMarkdown(p, overview, facts, decisions)); err != nil {
		return rep, err
	}
	rep.Paths = append(rep.Paths, specPath)

	assignments := assignmentsOf(p)
	if err := assignments.Save(root); err != nil {
		return rep, err
	}
	rep.Paths = append(rep.Paths, phaseflow.AssignmentsPath(root))

	if err := phaseflow.UpsertProjectDoc(root, phaseflow.RenderProjectDocBody(p.Project, specMarkdownOverview(overview), &assignments)); err != nil {
		return rep, err
	}
	rep.Paths = append(rep.Paths, phaseflow.ProjectDocPath(root))

	report, err := e.ValidatePlan()
	if err != nil {
		return rep, err
	}
	if !report.Complete {
		return rep, fmt.Errorf("%w: %s", ErrNotValid, strings.Join(report.Issues, "; "))
	}
	if err := e.Approve(); err != nil {
		return rep, err
	}
	return rep, nil
}

// checkCatalog resolves the agent catalog before anything is written and
// reports whether it must be seeded. An existing catalog is the owner's and is
// never touched; if it lacks DefaultAgent the export refuses, since every task
// would fail the validator. A missing catalog is seeded from the embedded
// agents, which must include DefaultAgent.
func checkCatalog(root string) (seed bool, err error) {
	agents, found, err := phaseflow.LoadCatalog(root)
	if err != nil {
		return false, err
	}
	if found {
		for _, a := range agents {
			if a.Name == DefaultAgent {
				return false, nil
			}
		}
		return false, fmt.Errorf("%w: add %s.prompt.md to %s, or export into a project without a catalog", ErrNoAgent, DefaultAgent, phaseflow.CatalogDir(root))
	}
	for _, full := range phaseflow.AgentNames() {
		if strings.TrimPrefix(full, "phase-") == DefaultAgent {
			return true, nil
		}
	}
	return false, fmt.Errorf("%w: the built-in catalog does not provide it either", ErrNoAgent)
}

// assignmentsOf builds the legacy task rows. Every row is pending, assigned
// to DefaultAgent on DefaultModel, with no dependencies (see ExportLegacy).
func assignmentsOf(p legacyPlan) phaseflow.Assignments {
	var a phaseflow.Assignments
	for _, ph := range p.Phases {
		for _, t := range ph.Tasks {
			a.Tasks = append(a.Tasks, phaseflow.Task{
				ID:                 t.ID,
				Phase:              strconv.Itoa(ph.Number),
				Title:              t.Title,
				Description:        t.Description,
				AcceptanceCriteria: t.Criteria,
				Agent:              DefaultAgent,
				Model:              DefaultModel,
				Status:             phaseflow.StatusPending,
			})
		}
	}
	return a
}

// refuseIfRunning stops an export that would overwrite a plan a run has
// already started. Everything pending is fine: that is a plan nothing has
// touched, and replacing it is the whole point of re-exporting.
func refuseIfRunning(root string) error {
	a, found, err := phaseflow.LoadAssignments(root)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for _, t := range a.Tasks {
		// An empty status is a row written before statuses existed; it has
		// recorded nothing, so it counts as pending.
		if t.Status != "" && t.Status != phaseflow.StatusPending {
			return fmt.Errorf("%w: task %s is %s. Re-exporting would discard what that run recorded; finish or reset the run first",
				ErrExecutionStarted, t.ID, t.Status)
		}
	}
	return nil
}

// answeredQuestions returns the decisions the owner made, in store order.
func answeredQuestions(repo *plantree.Repo) ([]plan.Question, error) {
	all, err := plan.LoadQuestions(repo)
	if err != nil {
		return nil, err
	}
	var out []plan.Question
	for _, q := range all {
		if q.Status == plan.QuestionAnswered {
			out = append(out, q)
		}
	}
	return out, nil
}

// specMarkdownOverview is the overview PROJECT.md's managed block quotes. It
// is the running overview, bounded; the block's own generator adds the phase
// and task table.
func specMarkdownOverview(overview string) string {
	return cutBytes(strings.TrimSpace(overview), overviewBytes)
}

func writeFile(path, content string) error {
	return lockfile.WriteAtomic(path, []byte(content), 0o644)
}
