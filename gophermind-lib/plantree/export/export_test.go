package export

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if _, err := ExportLegacy(repo, root); err == nil || !strings.Contains(err.Error(), "acceptance criteria") {
		t.Fatalf("ExportLegacy = %v, want a refusal naming the missing criteria", err)
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
