package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

// fakeCompleter specifies every step a pass-2 prompt asks for.
type fakeCompleter struct{}

func (fakeCompleter) Complete(_ context.Context, prompt string) (string, error) {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	var steps []plan.StepSpecOut
	if start >= 0 && end > start {
		for _, line := range strings.Split(prompt[start:end], "\n") {
			id, ok := strings.CutPrefix(line, "- ")
			if !ok {
				continue
			}
			id, _, _ = strings.Cut(id, ":")
			steps = append(steps, plan.StepSpecOut{
				ID: id, Description: "build " + id, TargetPaths: []string{"main.go"},
				AcceptanceCriteria: []string{id + " passes its test"},
				TestCommand:        []string{"go", "test", "./..."}, DependsOn: []string{},
			})
		}
	}
	b, err := json.Marshal(plan.Pass2Output{Steps: steps})
	return string(b), err
}

const brief = "# Storage\nKeep the notes on disk.\n\n# Interface\nA command line front end.\n"

// approvedProject builds a real project root with an approved plan tree in
// it: two passes over a two-section brief, then plan.Approve.
func approvedProject(t *testing.T) (root string, repo *plantree.Repo) {
	t.Helper()
	root = t.TempDir()
	repo = plantree.Open(phaseflow.PlanningDir(root))
	reply := func(n int, prompt string) (string, error) {
		switch {
		case strings.Contains(prompt, "Keep the notes on disk"):
			return `{"phases":[{"title":"Storage","digest":"where notes live","objective":"put notes on disk","tasks":[{"title":"Write the store","digest":"the file format","objective":"one file per note","steps":[{"title":"Define the file","digest":"name and layout"},{"title":"Write it","digest":"the writer"}]}]}],"overview":"Notes on disk, then a CLI."}`, nil
		case strings.Contains(prompt, "command line front end"):
			return `{"phases":[{"title":"Interface","digest":"how people use it","objective":"a CLI","tasks":[{"title":"Add the add command","digest":"the first verb","objective":"gophernote add","steps":[{"title":"Parse the arguments","digest":"flags"}]}]}],"overview":"Notes on disk, then a CLI."}`, nil
		}
		return "", errors.New("unexpected prompt")
	}
	if _, err := plan.RunPass1(context.Background(), repo, brief, completerFunc(reply), plan.Options{ProjectName: "Gophernote", ChunkBytes: 40}); err != nil {
		t.Fatal(err)
	}
	if err := plan.WriteFacts(repo, "Go. Build with go build ./... and test with go test ./...."); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.RunPass2(context.Background(), repo, fakeCompleter{}, plan.Options2{}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Approve(repo); err != nil {
		t.Fatal(err)
	}
	return root, repo
}

type completerFunc func(n int, prompt string) (string, error)

func (f completerFunc) Complete(_ context.Context, prompt string) (string, error) {
	return f(0, prompt)
}

// TestExportLegacyProducesAPlanPhaseflowAccepts is the contract: the files
// this package writes are read back by phaseflow's own parsers and pass its
// own validator, and the approval marker lets /project-execute run.
func TestExportLegacyProducesAPlanPhaseflowAccepts(t *testing.T) {
	root, repo := approvedProject(t)
	rep, err := ExportLegacy(repo, root)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Phases != 2 || rep.Tasks != 2 || rep.Steps != 3 {
		t.Errorf("Report = %+v, want 2 phases, 2 tasks, 3 steps", rep)
	}
	if rep.SeededAgents == 0 {
		t.Error("a project with no agent catalog must get one")
	}

	e := phaseflow.New(root)
	got, err := e.ValidatePlan()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete {
		t.Fatalf("phaseflow rejects the exported plan: %s", strings.Join(got.Issues, "; "))
	}
	if got.Phases != 2 || got.Tasks != 2 {
		t.Errorf("ValidatePlan saw %d phases and %d tasks", got.Phases, got.Tasks)
	}
	if !e.Approved() {
		t.Error("the approval marker was not written")
	}

	rm, err := phaseflow.LoadRoadmap(root)
	if err != nil {
		t.Fatal(err)
	}
	if rm.Title != "Gophernote" {
		t.Errorf("roadmap title = %q", rm.Title)
	}
	var ids []string
	for _, p := range rm.Phases {
		for _, pl := range p.Plans {
			ids = append(ids, pl.ID)
		}
		if p.Goal == "" {
			t.Errorf("phase %s has no goal", p.Number)
		}
	}
	if strings.Join(ids, ",") != "01-01,02-01" {
		t.Errorf("plan ids = %v, want 01-01 and 02-01 assigned by phase and task order", ids)
	}

	a, found, err := phaseflow.LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("assignments: %v found=%v", err, found)
	}
	for _, tk := range a.Tasks {
		if tk.Agent != DefaultAgent || tk.Model != DefaultModel || tk.Status != phaseflow.StatusPending {
			t.Errorf("task %s = agent %q model %q status %q", tk.ID, tk.Agent, tk.Model, tk.Status)
		}
		if len(tk.DependsOn) != 0 || tk.Wave != 0 {
			t.Errorf("task %s carries dependencies or a wave, which this milestone does not export: %+v", tk.ID, tk)
		}
		if len(tk.AcceptanceCriteria) == 0 {
			t.Errorf("task %s has no acceptance criteria", tk.ID)
		}
		if !strings.Contains(tk.Description, "Verify with: go test ./...") {
			t.Errorf("task %s does not carry its steps' test command:\n%s", tk.ID, tk.Description)
		}
	}
	first, _ := a.Task("01-01")
	if !strings.Contains(first.Description, "Define the file") || !strings.Contains(first.Description, "Write it") {
		t.Errorf("a task's description must fold in its steps:\n%s", first.Description)
	}
	if len(first.AcceptanceCriteria) != 2 {
		t.Errorf("acceptance criteria = %v, want the union of both steps' criteria", first.AcceptanceCriteria)
	}

	spec, err := os.ReadFile(filepath.Join(phaseflow.PlanningDir(root), SpecFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Notes on disk", "go build ./...", "Scope: 2 phase(s), 2 task(s), 3 step(s)"} {
		if !strings.Contains(string(spec), want) {
			t.Errorf("SPEC.md is missing %q", want)
		}
	}
	doc, err := os.ReadFile(phaseflow.ProjectDocPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "gophermind:spec:begin") || !strings.Contains(string(doc), "01-01") {
		t.Errorf("PROJECT.md's managed block was not written:\n%s", doc)
	}
}

// TestExportLegacyIsReRunnable: exporting twice before anything executes
// replaces the files and still validates.
func TestExportLegacyIsReRunnable(t *testing.T) {
	root, repo := approvedProject(t)
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	firstRoadmap, err := os.ReadFile(phaseflow.RoadmapPath(root))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ExportLegacy(repo, root)
	if err != nil {
		t.Fatalf("a second export before any execution must be accepted: %v", err)
	}
	if rep.SeededAgents != 0 {
		t.Error("the second export re-seeded a catalog that already existed")
	}
	secondRoadmap, err := os.ReadFile(phaseflow.RoadmapPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstRoadmap) != string(secondRoadmap) {
		t.Error("two exports of one tree produced different roadmaps")
	}
	if got, err := phaseflow.New(root).ValidatePlan(); err != nil || !got.Complete {
		t.Errorf("the re-exported plan is not valid: %v %v", err, got.Issues)
	}
	// And exactly one managed block survives in PROJECT.md.
	doc, err := os.ReadFile(phaseflow.ProjectDocPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(doc), "gophermind:spec:begin"); n != 1 {
		t.Errorf("PROJECT.md holds %d managed blocks, want 1", n)
	}
}

// TestExportLegacyRefusesOnceExecutionStarted is the guard: a task that is no
// longer pending means a run recorded something, so the export stops.
func TestExportLegacyRefusesOnceExecutionStarted(t *testing.T) {
	root, repo := approvedProject(t)
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	if err := phaseflow.Update(root, func(a *phaseflow.Assignments) error {
		a.Tasks[0].Status = phaseflow.StatusDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(phaseflow.AssignmentsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExportLegacy(repo, root)
	if !errors.Is(err, ErrExecutionStarted) {
		t.Fatalf("ExportLegacy = %v, want ErrExecutionStarted", err)
	}
	if !strings.Contains(err.Error(), "01-01") {
		t.Errorf("the refusal does not name the task: %v", err)
	}
	after, err := os.ReadFile(phaseflow.AssignmentsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a refused export still rewrote assignments.json")
	}
}

// TestExportLegacyRefusesAnUnspecifiedStep: the tree is the source, so a step
// with no acceptance criteria stops the export rather than producing a task
// phaseflow will reject later.
func TestExportLegacyRefusesAnUnspecifiedStep(t *testing.T) {
	root := t.TempDir()
	repo := plantree.Open(phaseflow.PlanningDir(root))
	if _, err := plan.RunPass1(context.Background(), repo, brief, completerFunc(func(_ int, p string) (string, error) {
		return `{"phases":[{"title":"Only","digest":"d","objective":"o","tasks":[{"title":"T","digest":"d","objective":"o","steps":[{"title":"S","digest":"d"}]}]}],"overview":"o"}`, nil
	}), plan.Options{ProjectName: "Half", ChunkBytes: 4000}); err != nil {
		t.Fatal(err)
	}
	// The tree validates an approved step, so an unspecified one is never
	// approved: the export refuses the whole tree first.
	if _, err := ExportLegacy(repo, root); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("ExportLegacy = %v, want ErrNotApproved", err)
	}
	// And the row-level guard, reached directly, still names the task.
	steps := []plantree.Node{{ID: "phase-001.task-001.step-001", Title: "S", ContextDigest: "d",
		Work: &plantree.Work{Description: "d"}}}
	if _, err := renderTask("01-01", plantree.Node{ID: "phase-001.task-001", Title: "T"}, steps); err == nil || !strings.Contains(err.Error(), "acceptance criteria") || !strings.Contains(err.Error(), "phase-001.task-001") {
		t.Fatalf("renderTask = %v, want a refusal naming the task and the missing criteria", err)
	}
	if phaseflow.New(root).Approved() {
		t.Error("a refused export must not approve anything")
	}
}

// TestExportedTextSurvivesThePlaceholderCheck: a phase name or goal with
// brackets or "TBD" in it would fail phaseflow's placeholder check, so the
// export rewrites them rather than producing a plan that cannot be approved.
func TestExportedTextSurvivesThePlaceholderCheck(t *testing.T) {
	for _, in := range []string{"Storage [maybe]", "Decide TBD later", "**bold**"} {
		got := sanitize(in)
		if strings.ContainsAny(got, "[]*") || strings.Contains(strings.ToUpper(got), "TBD") {
			t.Errorf("sanitize(%q) = %q, still a placeholder", in, got)
		}
		if got == "" {
			t.Errorf("sanitize(%q) emptied the text", in)
		}
	}
}

// TestRoadmapOverviewCannotForgeAPhase: the overview is model output and
// ROADMAP.md is parsed by line, so an overview containing a phase heading or
// a plan checkbox must not become part of the plan.
func TestRoadmapOverviewCannotForgeAPhase(t *testing.T) {
	p := legacyPlan{Project: "Demo", Steps: 1, Phases: []legacyPhase{{
		Number: 1, Name: "Real", Goal: "ship it",
		Tasks: []legacyTask{{ID: "01-01", Title: "Do the thing", Criteria: []string{"it works"}}},
	}}}
	hostile := "### Phase 9: Injected\n**Goal**: take over\n\nPlans:\n- [ ] 09-09: not a real task\n"
	rm, err := phaseflow.ParseRoadmap(roadmapMarkdown(p, hostile))
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Phases) != 1 {
		t.Fatalf("the overview forged %d extra phase(s): %+v", len(rm.Phases)-1, rm.Phases)
	}
	if len(rm.Phases[0].Plans) != 1 || rm.Phases[0].Plans[0].ID != "01-01" {
		t.Errorf("plans = %+v, want only the real one", rm.Phases[0].Plans)
	}
}

// builtTree writes an approved tree directly: phases phases, each with tasks
// tasks of one specified, reviewed step. It is how a test reaches sizes the
// two passes would take too long to produce.
func builtTree(t *testing.T, title string, phases, tasks int) (string, *plantree.Repo) {
	t.Helper()
	root := t.TempDir()
	repo := plantree.Open(phaseflow.PlanningDir(root))
	mk := func(id, title string) plantree.Node {
		ref, _ := plantree.ParentRef(id)
		n := plantree.Node{
			SchemaVersion: plantree.SchemaVersion, ID: id, Title: title, NodeRevision: 1,
			ContextDigest: "digest " + id, DependsOn: []string{}, Objective: "objective of " + id,
			Planning: plantree.Planning{Stage: plantree.StageSkeleton},
		}
		if ref != "" {
			n.ParentRef = &ref
		}
		return n
	}
	if err := repo.Init(mk(plantree.RootID, title)); err != nil {
		t.Fatal(err)
	}
	for p := 1; p <= phases; p++ {
		pid := fmt.Sprintf("phase-%03d", p)
		if err := repo.Create(mk(pid, "Phase title "+pid)); err != nil {
			t.Fatal(err)
		}
		for k := 1; k <= tasks; k++ {
			tid := fmt.Sprintf("%s.task-%03d", pid, k)
			if err := repo.Create(mk(tid, "Task "+tid)); err != nil {
				t.Fatal(err)
			}
			st := mk(tid+".step-001", "Step")
			st.Status = plantree.StatusReviewed
			st.Planning.Stage = plantree.StageApproved
			st.Work = &plantree.Work{Description: "d", TargetPaths: []string{"a.go"},
				AcceptanceCriteria: []string{"criterion of " + tid}, TestCommand: []string{"go", "test"}}
			if err := repo.Create(st); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, repo
}

// draftedProject runs both passes but does not approve.
func draftedProject(t *testing.T) (string, *plantree.Repo) {
	t.Helper()
	root, repo := approvedProject(t)
	if err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindStep {
			return nil
		}
		_, err := repo.Update(n.ID, n.NodeRevision, func(m *plantree.Node) error {
			m.Planning.Stage = plantree.StageDrafted
			m.Status = plantree.StatusUntouched
			return nil
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return root, repo
}

func TestExportLegacyRefusesAnUnapprovedTree(t *testing.T) {
	root, repo := draftedProject(t)
	_, err := ExportLegacy(repo, root)
	if !errors.Is(err, ErrNotApproved) {
		t.Fatalf("ExportLegacy = %v, want ErrNotApproved", err)
	}
	if !strings.Contains(err.Error(), "approve the plan first") {
		t.Errorf("the refusal does not say what to do: %v", err)
	}
	for _, p := range []string{phaseflow.RoadmapPath(root), phaseflow.AssignmentsPath(root), filepath.Join(phaseflow.PlanningDir(root), SpecFileName), phaseflow.ProjectDocPath(root)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("a refused export wrote %s", p)
		}
	}
	if phaseflow.New(root).Approved() {
		t.Error("a refused export approved something")
	}
	if _, found, _ := phaseflow.LoadCatalog(root); found {
		t.Error("a refused export seeded a catalog")
	}
}

func TestExportLegacyRefusesAnInvalidTree(t *testing.T) {
	root, repo := approvedProject(t)
	dep := "phase-001.task-001.step-999"
	if err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindStep {
			return nil
		}
		_, err := repo.Update(n.ID, n.NodeRevision, func(m *plantree.Node) error {
			m.DependsOn = []string{dep}
			return nil
		})
		if err != nil {
			return err
		}
		return errStop
	}); err != nil && !errors.Is(err, errStop) {
		t.Fatal(err)
	}
	if _, err := ExportLegacy(repo, root); err == nil {
		t.Fatal("a tree with a dangling dependency was exported")
	}
	if _, err := os.Stat(phaseflow.RoadmapPath(root)); err == nil {
		t.Error("a refused export wrote ROADMAP.md")
	}
}

var errStop = errors.New("stop")

// TestExportLegacyDropsTheMarkerBeforeReplacingFiles: an approved project
// re-exported with a tree that fails phaseflow's gate must end with no marker,
// or /project-execute would run the replaced, unvalidated pair.
func TestExportLegacyDropsTheMarkerWhenTheGateFails(t *testing.T) {
	root, repo := approvedProject(t)
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	if !phaseflow.New(root).Approved() {
		t.Fatal("setup: the first export must approve")
	}
	// A phase with no tasks passes the tree's own checks (every step is
	// reviewed) but leaves a roadmap phase with no plans.
	pid := "phase-003"
	ref, _ := plantree.ParentRef(pid)
	if err := repo.Create(plantree.Node{
		SchemaVersion: plantree.SchemaVersion, ID: pid, Title: "Empty", NodeRevision: 1,
		ContextDigest: "d", ParentRef: &ref, DependsOn: []string{}, Objective: "nothing",
		Planning: plantree.Planning{Stage: plantree.StageSkeleton},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := ExportLegacy(repo, root)
	if !errors.Is(err, ErrNotValid) {
		t.Fatalf("ExportLegacy = %v, want ErrNotValid", err)
	}
	if phaseflow.New(root).Approved() {
		t.Error("a stale approval marker survived a failed re-export")
	}
}

// TestExportLegacyLeavesNoMarkerAfterACrash: a write that fails between the
// ROADMAP write and the assignments write must leave no approval behind, must
// not have reached the assignments write, and must say the project is
// currently unapproved.
func TestExportLegacyLeavesNoMarkerAfterACrash(t *testing.T) {
	root, repo := approvedProject(t)
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(phaseflow.PlanningDir(root), SpecFileName)
	if err := os.Remove(spec); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(spec, 0o755); err != nil { // the SPEC.md write now fails
		t.Fatal(err)
	}
	// An old modification time is the sentinel: a re-export writes identical
	// bytes, so only a changed time shows whether a file was written again.
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []string{phaseflow.RoadmapPath(root), phaseflow.AssignmentsPath(root)} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := ExportLegacy(repo, root)
	if err == nil {
		t.Fatal("the export did not fail")
	}
	if !strings.Contains(err.Error(), "currently unapproved") || !strings.Contains(err.Error(), "may still be intact") {
		t.Errorf("error = %q, want it to say the project is currently unapproved and old files may be intact", err)
	}
	if phaseflow.New(root).Approved() {
		t.Error("a crashed export left the approval marker in place")
	}
	mtime := func(p string) time.Time {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return fi.ModTime()
	}
	if !mtime(phaseflow.RoadmapPath(root)).After(old) {
		t.Error("the crash simulation never reached the ROADMAP write")
	}
	if !mtime(phaseflow.AssignmentsPath(root)).Equal(old) {
		t.Error("the crash simulation did not stop before the assignments write")
	}
	// Only what was really replaced before the crash is reported.
	if len(rep.Replaced) != 1 || rep.Replaced[0] != phaseflow.RoadmapPath(root) {
		t.Errorf("Replaced = %v, want just the ROADMAP written before the crash", rep.Replaced)
	}
}

// A failure before the first write replaced nothing, so Report.Replaced is empty.
func TestExportLegacyReplacedIsEmptyWhenTheFirstWriteFails(t *testing.T) {
	root, repo := approvedProject(t)
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	rm := phaseflow.RoadmapPath(root)
	if err := os.Remove(rm); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(rm, 0o755); err != nil { // the very first write fails
		t.Fatal(err)
	}
	rep, err := ExportLegacy(repo, root)
	if err == nil {
		t.Fatal("the export did not fail")
	}
	if len(rep.Replaced) != 0 || len(rep.Paths) != 0 {
		t.Errorf("Replaced = %v, Paths = %v after a failure before any write", rep.Replaced, rep.Paths)
	}
}

// A gate failure after every write also says the project is unapproved.
func TestExportLegacyGateFailureSaysTheProjectIsUnapproved(t *testing.T) {
	root, repo := approvedProject(t)
	pid := "phase-003"
	ref, _ := plantree.ParentRef(pid)
	if err := repo.Create(plantree.Node{
		SchemaVersion: plantree.SchemaVersion, ID: pid, Title: "Empty", NodeRevision: 1,
		ContextDigest: "d", ParentRef: &ref, DependsOn: []string{}, Objective: "nothing",
		Planning: plantree.Planning{Stage: plantree.StageSkeleton},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := ExportLegacy(repo, root)
	if !errors.Is(err, ErrNotValid) || !strings.Contains(err.Error(), "currently unapproved") {
		t.Errorf("ExportLegacy = %v, want ErrNotValid naming the unapproved state", err)
	}
}

func TestExportLegacyRefusesWhileAnotherRunHoldsTheLock(t *testing.T) {
	root, repo := approvedProject(t)
	lock := filepath.Join(repo.Dir(), "_state", "run.lock")
	other, err := lockfile.TryAcquire(lock)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExportLegacy(repo, root)
	if !errors.Is(err, plan.ErrRunBusy) {
		t.Errorf("ExportLegacy = %v, want plan.ErrRunBusy", err)
	}
	if _, statErr := os.Stat(phaseflow.RoadmapPath(root)); statErr == nil {
		t.Error("a busy refusal wrote files")
	}
	other()
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatalf("export after the lock was released: %v", err)
	}
	again, err := lockfile.TryAcquire(lock)
	if err != nil {
		t.Fatalf("the export did not release its hold: %v", err)
	}
	again()
}

func TestExportLegacyReportsWhatItReplaced(t *testing.T) {
	root, repo := approvedProject(t)
	// A hand-written PROJECT.md without a block: nothing to replace there.
	if err := os.WriteFile(phaseflow.ProjectDocPath(root), []byte("# Mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := ExportLegacy(repo, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Replaced) != 0 {
		t.Errorf("first export Replaced = %v, want none", first.Replaced)
	}
	if len(first.Paths) != 4 {
		t.Errorf("Paths = %v, want ROADMAP, SPEC, assignments, PROJECT.md", first.Paths)
	}
	for _, p := range first.Paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("Paths lists %s which is not on disk", p)
		}
	}
	second, err := ExportLegacy(repo, root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		phaseflow.RoadmapPath(root): true, phaseflow.AssignmentsPath(root): true,
		filepath.Join(phaseflow.PlanningDir(root), SpecFileName): true, phaseflow.ProjectDocPath(root): true,
	}
	if len(second.Replaced) != len(want) {
		t.Fatalf("second export Replaced = %v, want %d files", second.Replaced, len(want))
	}
	for _, p := range second.Replaced {
		if !want[p] {
			t.Errorf("Replaced lists unexpected %s", p)
		}
	}
	doc, _ := os.ReadFile(phaseflow.ProjectDocPath(root))
	if !strings.Contains(string(doc), "# Mine") {
		t.Error("the hand-written PROJECT.md text was lost")
	}
}

func TestExportLegacyRefusesACatalogWithoutTheDefaultAgent(t *testing.T) {
	root, repo := approvedProject(t)
	dir := phaseflow.CatalogDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "reviewer.prompt.md")
	if err := os.WriteFile(mine, []byte("---\nname: reviewer\n---\nreview\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportLegacy(repo, root)
	if !errors.Is(err, ErrNoAgent) {
		t.Fatalf("ExportLegacy = %v, want ErrNoAgent", err)
	}
	if !strings.Contains(err.Error(), DefaultAgent) || !strings.Contains(err.Error(), dir) {
		t.Errorf("the refusal must name the agent and the catalog dir: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the owner's catalog was modified: %d files", len(entries))
	}
	if _, statErr := os.Stat(phaseflow.RoadmapPath(root)); statErr == nil {
		t.Error("a refused export wrote ROADMAP.md")
	}
	// With the agent present it goes through and the catalog stays theirs.
	if err := os.WriteFile(filepath.Join(dir, DefaultAgent+".prompt.md"), []byte("---\nname: executor\n---\nmine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := ExportLegacy(repo, root)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SeededAgents != 0 {
		t.Error("an existing catalog was seeded over")
	}
}

func TestSanitizeNeutralizesInserted(t *testing.T) {
	for _, in := range []string{"Fix (INSERTED)", "Fix [Inserted]", "Fix (inserted) now"} {
		got := sanitize(in)
		if strings.Contains(strings.ToUpper(got), "(INSERTED)") || got == "" {
			t.Errorf("sanitize(%q) = %q", in, got)
		}
		rm, err := phaseflow.ParseRoadmap("# Roadmap: X\n\n### Phase 1: " + got + "\n**Goal**: g\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(rm.Phases) != 1 || rm.Phases[0].Inserted {
			t.Errorf("%q still parses as an inserted phase: %+v", got, rm.Phases)
		}
	}
}

func TestRootTitleCannotInjectRoadmapLines(t *testing.T) {
	root, repo := builtTree(t, "Demo\n### Phase 9: Injected\n- [ ] 09-09: forged", 1, 1)
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	rm, err := phaseflow.LoadRoadmap(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Phases) != 1 || len(rm.Phases[0].Plans) != 1 {
		t.Fatalf("the title forged structure: %+v", rm.Phases)
	}
	if strings.Contains(rm.Title, "\n") {
		t.Errorf("title = %q", rm.Title)
	}
}

func TestACriterionLiterallyCmdDoesNotSuppressTheCommand(t *testing.T) {
	steps := []plantree.Node{{ID: "phase-001.task-001.step-001", Title: "S", ContextDigest: "d",
		Work: &plantree.Work{Description: "d", AcceptanceCriteria: []string{"cmd:make x"}, TestCommand: []string{"make", "x"}}}}
	got, err := renderTask("01-01", plantree.Node{ID: "phase-001.task-001", Title: "T"}, steps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Description, "Verify with: make x") {
		t.Errorf("the command was suppressed:\n%s", got.Description)
	}
}

// TestHostileTitlesSurviveEndToEnd puts every parser trap into a phase title
// and a goal and checks phaseflow's real parser and validator still accept it.
func TestHostileTitlesSurviveEndToEnd(t *testing.T) {
	root, repo := approvedProject(t)
	phases, _ := repo.Children(plantree.RootID)
	hostile := "Storage [x] **b** TBD (INSERTED)\n### Phase 9: forged\n- [ ] 09-09: forged"
	if _, err := repo.Update(phases[0].ID, phases[0].NodeRevision, func(n *plantree.Node) error {
		n.Title = hostile
		n.Objective = hostile
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	rm, err := phaseflow.LoadRoadmap(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Phases) != 2 {
		t.Fatalf("%d phases parsed, want 2: %+v", len(rm.Phases), rm.Phases)
	}
	if rm.Phases[0].Inserted {
		t.Error("the title made the phase inserted")
	}
	if got, err := phaseflow.New(root).ValidatePlan(); err != nil || !got.Complete {
		t.Errorf("hostile title made the plan invalid: %v %v", err, got.Issues)
	}
}

func TestIdsBeyondNinetyNineParseAndMatch(t *testing.T) {
	root, repo := builtTree(t, "Big", 101, 1)
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	rm, err := phaseflow.LoadRoadmap(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Phases) != 101 || rm.Phases[100].Plans[0].ID != "101-01" {
		t.Fatalf("phases = %d, last plan = %+v", len(rm.Phases), rm.Phases[len(rm.Phases)-1].Plans)
	}
	a, _, err := phaseflow.LoadAssignments(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Task("101-01"); !ok || len(a.Tasks) != 101 {
		t.Errorf("assignments hold %d tasks, 101-01 present=%v", len(a.Tasks), ok)
	}

	root2, repo2 := builtTree(t, "Wide", 1, 101)
	if _, err := ExportLegacy(repo2, root2); err != nil {
		t.Fatal(err)
	}
	rm2, _ := phaseflow.LoadRoadmap(root2)
	if n := len(rm2.Phases[0].Plans); n != 101 || rm2.Phases[0].Plans[100].ID != "01-101" {
		t.Errorf("plans = %d, last %q", n, rm2.Phases[0].Plans[n-1].ID)
	}
}

func TestCJKTitlesStayValidUTF8(t *testing.T) {
	long := strings.Repeat("数据存储层", 80) // 1200 bytes, over every bound
	root, repo := builtTree(t, "笔记应用", 1, 1)
	phases, _ := repo.Children(plantree.RootID)
	if _, err := repo.Update(phases[0].ID, phases[0].NodeRevision, func(n *plantree.Node) error {
		n.Title = long
		n.Objective = long
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportLegacy(repo, root); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{phaseflow.RoadmapPath(root), phaseflow.AssignmentsPath(root)} {
		b, _ := os.ReadFile(p)
		if !utf8.Valid(b) {
			t.Errorf("%s is not valid UTF-8", p)
		}
	}
	if got, err := phaseflow.New(root).ValidatePlan(); err != nil || !got.Complete {
		t.Errorf("CJK plan invalid: %v %v", err, got.Issues)
	}
}

// sanitize runs before the cap, so rewriting TBD to the longer "undecided"
// cannot push a field past its bound.
func TestSanitizedTextStaysWithinItsCap(t *testing.T) {
	_, repo := builtTree(t, strings.Repeat("TBD ", 200), 1, 1)
	p, err := read(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Project) > planDescriptionBytes+len("...") {
		t.Errorf("project name is %d bytes, cap %d", len(p.Project), planDescriptionBytes)
	}
	if strings.Contains(strings.ToUpper(p.Project), "TBD") {
		t.Errorf("project name still holds TBD: %q", p.Project)
	}
}

// A pipe in a phase name would add a column to the Progress table.
func TestPhaseNameCannotBreakTheProgressTable(t *testing.T) {
	md := roadmapMarkdown(legacyPlan{Project: "P", Phases: []legacyPhase{{Number: 1, Name: "a | b", Goal: "g",
		Tasks: []legacyTask{{ID: "01-01", Title: "t"}}}}}, "")
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, "| 1.") && strings.Count(l, "|") != 5 {
			t.Errorf("progress row %q has %d pipes, want 5", l, strings.Count(l, "|"))
		}
	}
}
