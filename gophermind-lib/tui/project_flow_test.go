package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/export"
	"gophermind/gophermind-lib/plantree/plan"
)

// twoPartBrief has two headed sections, each large enough that the two cannot
// share one chunk at the default DefaultChunkBytes, so a run really does make
// two pass-1 calls. That is what lets a cancel land between them.
var twoPartBrief = "# Storage\nKeep the notes on disk.\n" + filler("s") +
	"\n# Interface\nA command line front end.\n" + filler("i")

// filler is a paragraph of plain prose, sized so one section fills more than
// half a chunk.
func filler(unit string) string {
	return strings.Repeat("Background detail "+unit+". ", 400) + "\n"
}

// planFake answers both passes. Pass 1 gets a phase per brief part, and asks
// one question on the first part; pass 2 specifies every step it is given.
type planFake struct {
	mu      sync.Mutex
	prompts []string
	// gate, when non-nil, blocks the first call until it is closed, and
	// started is closed as soon as that first call arrives. Together they let
	// a test cancel a run while it is genuinely in flight.
	gate    chan struct{}
	started chan struct{}
	// failPart, when non-empty, makes the call carrying that text fail once.
	failPart string
	failed   bool
}

func (f *planFake) Complete(_ context.Context, prompt string) (string, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	first := len(f.prompts) == 1
	fail := f.failPart != "" && !f.failed && strings.Contains(prompt, f.failPart)
	if fail {
		f.failed = true
	}
	f.mu.Unlock()
	if first && f.started != nil {
		close(f.started)
	}
	if first && f.gate != nil {
		<-f.gate
	}
	if fail {
		return "", errors.New("model unreachable")
	}
	switch {
	// A pass-2 prompt quotes the brief, so it is matched first: otherwise a
	// specification request carrying the brief's text would be answered with
	// a skeleton.
	case strings.Contains(prompt, "Steps to specify now:"):
		return specReply(prompt), nil
	case strings.Contains(prompt, "Keep the notes on disk"):
		return `{"phases":[{"title":"Storage","digest":"where notes live","objective":"put notes on disk",` +
			`"tasks":[{"title":"Write the store","digest":"the file format","objective":"one file per note",` +
			`"steps":[{"title":"Define the file","digest":"name and layout"},{"title":"Write it","digest":"the writer"}]}]}],` +
			`"overview":"Notes on disk, then a CLI.",` +
			`"questions":[{"question":"Which file format?","why":"the writer depends on it",` +
			`"options":[{"label":"JSON","description":"one object per note"},{"label":"Markdown","description":"plain text"}],` +
			`"multi_select":false,"recommended":["JSON"],"rationale":"easiest to parse","affects":["Write the store"]}]}`, nil
	case strings.Contains(prompt, "command line front end"):
		return `{"phases":[{"title":"Interface","digest":"how people use it","objective":"a CLI",` +
			`"tasks":[{"title":"Add the add command","digest":"the first verb","objective":"gophernote add",` +
			`"steps":[{"title":"Parse the arguments","digest":"flags"}]}]}],"overview":"Notes on disk, then a CLI."}`, nil
	}
	return "", errors.New("unexpected prompt")
}

func (f *planFake) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.prompts...)
}

// specReply specifies every step a pass-2 prompt asks for.
func specReply(prompt string) string {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	var steps []plan.StepSpecOut
	if start >= 0 && end > start {
		for _, line := range strings.Split(prompt[start:end], "\n") {
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			if id := roundStepID.FindString(line); id != "" {
				steps = append(steps, plan.StepSpecOut{
					ID: id, Description: "build " + id, TargetPaths: []string{"main.go"},
					AcceptanceCriteria: []string{id + " passes its test"},
					TestCommand:        []string{"go", "test", "./..."}, DependsOn: []string{},
				})
			}
		}
	}
	b, _ := json.Marshal(plan.Pass2Output{Steps: steps})
	return string(b)
}

// projectModel is a session in an empty working directory with a brief file
// in it, whose planning passes run on the given fake.
func projectModel(t *testing.T, f *planFake) (model, string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte(twoPartBrief), 0o644); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.completer = f
	return m, dir, brief
}

// TestSlashProjectRunsBothPassesAndOpensTheRound is Task 5's contract: the
// command reads the brief file, runs both passes on a goroutine, and lands in
// the question round the passes filled.
func TestSlashProjectRunsBothPassesAndOpensTheRound(t *testing.T) {
	f := &planFake{}
	m, dir, brief := projectModel(t, f)

	m = submit(t, m, "/project Gophernote "+brief)
	if m.proj != projRunning || m.st != stateWorking {
		t.Fatalf("proj = %v, state = %v, want the passes running; transcript:\n%s", m.proj, m.st, m.content)
	}
	m = settle(t, m)

	if m.qphase != qAsking {
		t.Fatalf("the round did not open (proj=%v qphase=%v):\n%s", m.proj, m.qphase, m.content)
	}
	for _, want := range []string{"Planning “Gophernote”", "reading the brief:", "skeleton: 2 phase(s), 2 task(s), 3 step(s)", "need an answer"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}
	if !strings.Contains(m.View(), "Which file format?") {
		t.Errorf("the question the pass asked is not on screen:\n%s", m.View())
	}

	repo := plantree.Open(phaseflow.PlanningDir(dir))
	if _, err := repo.Get("phase-002.task-001.step-001"); err != nil {
		t.Errorf("the tree is missing the second part's step: %v", err)
	}
	if got, err := plan.ReadBrief(repo); err != nil || got != twoPartBrief {
		t.Errorf("the brief was not stored for a resume: %v", err)
	}
	if !phaseflow.New(dir).Initialized() {
		t.Error(".planning was not scaffolded")
	}
}

// TestSlashProjectResumesAfterACancel: Esc mid-run stops the passes, and
// running /project with the name alone continues from the tree, with no
// brief path and no repeated work.
func TestSlashProjectResumesAfterACancel(t *testing.T) {
	f := &planFake{gate: make(chan struct{}), started: make(chan struct{})}
	m, dir, brief := projectModel(t, f)

	m = submit(t, m, "/project Gophernote "+brief)
	<-f.started // the first chunk's pass is in flight
	m.cancel()  // what Esc does
	close(f.gate)
	m = settle(t, m)

	if m.proj != projNone || !strings.Contains(m.content, "cancelled") {
		t.Fatalf("proj = %v after a cancel:\n%s", m.proj, m.content)
	}
	if !strings.Contains(m.content, "resumes it from the tree") {
		t.Errorf("the transcript does not say how to resume:\n%s", m.content)
	}
	repo := plantree.Open(phaseflow.PlanningDir(dir))
	if _, err := repo.Get("phase-001"); err != nil {
		t.Fatalf("the cancelled run kept nothing: %v", err)
	}
	if _, err := repo.Get("phase-002"); err == nil {
		t.Fatal("the run was not cancelled: the second chunk was processed too")
	}
	before := len(f.seen())

	// Resume with the name alone: the stored brief is re-supplied.
	m.completer = &planFake{}
	m = settle(t, submit(t, m, "/project Gophernote"))
	if !strings.Contains(m.content, "Resuming the plan") {
		t.Errorf("the resume was not announced:\n%s", m.content)
	}
	if _, err := repo.Get("phase-002.task-001.step-001"); err != nil {
		t.Errorf("the resume did not finish the brief: %v", err)
	}
	if got := len(f.seen()); got != before {
		t.Errorf("the first fake was called again (%d then %d): the resume redid finished work", before, got)
	}
	if m.qphase != qAsking && m.proj != projApprove {
		t.Errorf("the resumed run ended nowhere useful (qphase=%v proj=%v):\n%s", m.qphase, m.proj, m.content)
	}
}

// TestSlashProjectRefusesADifferentBriefOnAnExistingPlan: the pass-1 cursor
// is only valid against the same brief, so the mismatch is reported before
// anything runs rather than surfacing as a failed pass.
func TestSlashProjectRefusesADifferentBriefOnAnExistingPlan(t *testing.T) {
	f := &planFake{}
	m, dir, brief := projectModel(t, f)
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	if m.qphase != qAsking {
		t.Fatalf("no round:\n%s", m.content)
	}
	m = keys(t, m, key(tea.KeyEsc), key(tea.KeyEsc))

	other := filepath.Join(dir, "other.md")
	if err := os.WriteFile(other, []byte("# Something else\nentirely.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = submit(t, m, "/project Gophernote "+other)
	if m.proj != projNone || !strings.Contains(m.content, "different brief") {
		t.Errorf("proj = %v, transcript:\n%s", m.proj, m.content)
	}
}

// TestProjectApprovalRefusesWhileAQuestionIsOpen: the approval prompt is only
// reached with nothing open, but the approval itself re-checks, because the
// store can change between the prompt and the answer.
func TestProjectApprovalRefusesWhileAQuestionIsOpen(t *testing.T) {
	f := &planFake{}
	m, dir, brief := projectModel(t, f)
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	m = settle(t, keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS)))
	if m.proj != projApprove {
		t.Fatalf("no approval prompt:\n%s", m.content)
	}
	repo := plantree.Open(phaseflow.PlanningDir(dir))
	if _, err := plan.AddQuestions(repo, []plan.NewQuestion{{
		Question: "One more thing?", Why: "it came up",
		Options: []plan.NewOption{{Label: "yes"}, {Label: "no"}},
		Affects: []string{"phase-001.task-001"}, Source: "a later pass",
	}}); err != nil {
		t.Fatal(err)
	}
	m = submit(t, m, "y")
	if !strings.Contains(m.content, "still open") {
		t.Errorf("approval did not re-check the questions:\n%s", m.content)
	}
	if phaseflow.New(dir).Approved() {
		t.Error("a refused approval still wrote the marker")
	}
}

// TestProjectApprovalReviseDoesNotPretendToRePlan: nothing in this milestone
// turns free text into a changed plan, so the prompt says so and points at
// the path that does re-plan.
func TestProjectApprovalReviseDoesNotPretendToRePlan(t *testing.T) {
	m := testModel(t)
	t.Chdir(t.TempDir())
	m.proj = projApprove
	m.projName = "Gophernote"
	nm, _, handled := m.handleProjectInput("split phase 2")
	if !handled || nm.proj != projNone {
		t.Fatalf("handled=%v proj=%v", handled, nm.proj)
	}
	for _, want := range []string{"not wired to a re-planning pass", "/questions change"} {
		if !strings.Contains(nm.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, nm.content)
		}
	}
}

// TestProjectCancelLeavesThePlanUnapproved keeps "cancel" honest.
func TestProjectCancelLeavesThePlanUnapproved(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := testModel(t)
	m.proj = projApprove
	m.projName = "Gophernote"
	nm, _, handled := m.handleProjectInput("cancel")
	if !handled || nm.proj != projNone {
		t.Fatalf("handled=%v proj=%v", handled, nm.proj)
	}
	if phaseflow.New(dir).Approved() {
		t.Error("cancel must not approve")
	}
}

// TestExportRefusalsAreSpecific: each refusal is reported in its own words.
func TestExportRefusalsAreSpecific(t *testing.T) {
	for err, want := range map[error]string{
		export.ErrNotApproved:      "not approved",
		export.ErrNoAgent:          "agent catalog",
		export.ErrExecutionStarted: "already started",
		export.ErrNotValid:         "failed validation",
		plan.ErrRunBusy:            "another planning run",
	} {
		if got := exportRefusal(fmt.Errorf("wrapped: %w", err)); !strings.Contains(got, want) {
			t.Errorf("%v: %q is missing %q", err, got, want)
		}
	}
	if got := approveRefusal(plan.ErrRunBusy); !strings.Contains(got, "nothing was approved") {
		t.Errorf("busy approval: %q", got)
	}
}

// TestRenderExportReportAnnouncesReplacedFiles: overwriting is never silent.
func TestRenderExportReportAnnouncesReplacedFiles(t *testing.T) {
	got := renderExportReport(export.Report{Phases: 1, Tasks: 1, Steps: 1, Replaced: []string{".planning/ROADMAP.md"}})
	if !strings.Contains(got, "replaced 1 file(s)") || !strings.Contains(got, "replaced .planning/ROADMAP.md") {
		t.Errorf("replaced files not announced:\n%s", got)
	}
}

// TestProjectExportFailureSaysTheProjectIsUnapproved: with the tree approved
// but the catalog missing its agent, the export refuses after approval, and
// the owner is told execution is blocked and how to retry.
func TestProjectExportFailureSaysTheProjectIsUnapproved(t *testing.T) {
	f := &planFake{}
	m, dir, brief := projectModel(t, f)
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	m = settle(t, keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS)))
	if m.proj != projApprove {
		t.Fatalf("no approval prompt:\n%s", m.content)
	}
	agents := phaseflow.CatalogDir(dir)
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "other.prompt.md"), []byte("---\nname: other\n---\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = submit(t, m, "y")
	if phaseflow.New(dir).Approved() {
		t.Fatalf("the export should have refused:\n%s", m.content)
	}
	for _, want := range []string{"approved:", "agent catalog", "currently unapproved for execution", "/project Gophernote"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}
}

// TestSlashProjectEndToEnd is Task 7: brief in, answered question, approval,
// exported files that phaseflow validates, and pending tasks for
// /project-execute. The dir is clean, so no agent catalog exists and the
// export seeds one.
func TestSlashProjectEndToEnd(t *testing.T) {
	f := &planFake{}
	m, dir, brief := projectModel(t, f)

	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	if m.qphase != qAsking {
		t.Fatalf("no round:\n%s", m.content)
	}
	// Answer the open question (space selects, ctrl-s submits) and let the
	// pass that follows specify the steps it released.
	m = settle(t, keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS)))
	if m.proj != projApprove || !strings.Contains(m.content, approvalPrompt) {
		t.Fatalf("the round did not hand over to approval (proj=%v):\n%s", m.proj, m.content)
	}
	if !strings.Contains(m.content, "2 phase(s)") {
		t.Errorf("the summary does not size the plan:\n%s", m.content)
	}
	if phaseflow.New(dir).Approved() {
		t.Fatal("the plan was approved before the owner answered y")
	}

	// The owner changes their mind at the prompt: the answer is changed, the
	// two steps it invalidated are re-planned, and the same prompt returns.
	m = submit(t, m, "/questions")
	if m.qphase != qAsking || m.round.mode != roundChange {
		t.Fatalf("/questions at the prompt did not offer the answered question (phase=%v):\n%s", m.qphase, m.content)
	}
	m = settle(t, keys(t, m, key(tea.KeyRight), key(tea.KeySpace), key(tea.KeyCtrlS)))
	if m.proj != projApprove || !strings.Contains(m.content, "2 re-planned") {
		t.Fatalf("the changed answer did not return to the approval prompt (proj=%v):\n%s", m.proj, m.content)
	}
	if phaseflow.New(dir).Approved() {
		t.Fatal("changing an answer approved the plan")
	}

	m = submit(t, m, "y")
	if m.proj != projNone {
		t.Fatalf("proj = %v after approving", m.proj)
	}
	for _, want := range []string{"approved: 3 step(s) marked reviewed", "exported 2 phase(s) and 2 task(s)", "seeded", "/project-execute"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}

	e := phaseflow.New(dir)
	if !e.Approved() {
		t.Fatal("the approval marker /project-execute gates on was not written")
	}
	rep, err := e.ValidatePlan()
	if err != nil || !rep.Complete {
		t.Fatalf("phaseflow rejects the exported plan: %v %v", err, rep.Issues)
	}
	a, found, err := phaseflow.LoadAssignments(dir)
	if err != nil || !found {
		t.Fatalf("assignments: %v found=%v", err, found)
	}
	roadmap, err := os.ReadFile(filepath.Join(phaseflow.PlanningDir(dir), "ROADMAP.md"))
	if err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, tk := range a.Tasks {
		if tk.Status == phaseflow.StatusPending {
			pending++
		}
		if tk.Agent != export.DefaultAgent || tk.Model != export.DefaultModel {
			t.Errorf("task %s = agent %q model %q", tk.ID, tk.Agent, tk.Model)
		}
		if !strings.Contains(string(roadmap), tk.ID) {
			t.Errorf("task %s is in assignments.json but not in ROADMAP.md:\n%s", tk.ID, roadmap)
		}
	}
	if pending != 2 {
		t.Errorf("%d pending tasks, want the 2 /project-execute would run", pending)
	}
	// And the decision the owner ended on, the changed one, reached SPEC.md.
	spec, err := os.ReadFile(filepath.Join(phaseflow.PlanningDir(dir), export.SpecFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "Which file format?") || !strings.Contains(string(spec), "decided: Markdown") {
		t.Errorf("SPEC.md does not record the decision:\n%s", spec)
	}
}
