package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

// atApproval runs /project to the approval prompt: the question answered,
// every step specified.
func atApproval(t *testing.T) (model, string, *planFake) {
	t.Helper()
	f := &planFake{}
	m, dir, brief := projectModel(t, f)
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	m = settle(t, keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS)))
	if m.proj != projApprove {
		t.Fatalf("no approval prompt:\n%s", m.content)
	}
	return m, dir, f
}

// blockCatalog gives the project a catalog with no executor agent, which
// makes the export refuse; it returns the file to remove to fix that.
func blockCatalog(t *testing.T, dir string) string {
	t.Helper()
	agents := phaseflow.CatalogDir(dir)
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(agents, "other.prompt.md")
	if err := os.WriteFile(p, []byte("---\nname: other\n---\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// After a successful plan.Approve and a failed export, the message tells the
// owner to run /project again and answer y. That has to reach the prompt.
func TestRerunAfterAFailedExportReachesTheApprovalPromptAgain(t *testing.T) {
	m, dir, _ := atApproval(t)
	blocker := blockCatalog(t, dir)
	m = submit(t, m, "y")
	if phaseflow.New(dir).Approved() {
		t.Fatalf("the export should have refused:\n%s", m.content)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(phaseflow.CatalogDir(dir)); err != nil {
		t.Fatal(err)
	}

	m = settle(t, submit(t, m, "/project Gophernote"))
	if m.proj != projApprove {
		t.Fatalf("the rerun did not offer the approval prompt again (proj=%v):\n%s", m.proj, m.content)
	}
	if !strings.Contains(m.content, "already approved") {
		t.Errorf("the prompt does not say the plan is already approved:\n%s", m.content)
	}
	m = submit(t, m, "y")
	if !phaseflow.New(dir).Approved() {
		t.Fatalf("answering y after the cause was fixed did not approve:\n%s", m.content)
	}
	if m.proj != projNone || !strings.Contains(m.content, "/project-execute") {
		t.Errorf("proj=%v, transcript:\n%s", m.proj, m.content)
	}
}

// A rerun after a fully successful export has nothing to do, and says so
// instead of re-exporting or claiming the plan is complete.
func TestRerunAfterASuccessfulExportSaysItIsReady(t *testing.T) {
	m, dir, _ := atApproval(t)
	m = submit(t, m, "y")
	if !phaseflow.New(dir).Approved() {
		t.Fatalf("setup: the export failed:\n%s", m.content)
	}
	before, err := os.ReadFile(phaseflow.AssignmentsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	m = settle(t, submit(t, m, "/project Gophernote"))
	if m.proj != projNone {
		t.Errorf("proj = %v, want the flow ended", m.proj)
	}
	if !strings.Contains(m.content, "already approved and exported") || !strings.Contains(m.content, "/project-execute") {
		t.Errorf("the rerun does not say the plan is exported and ready:\n%s", m.content)
	}
	after, _ := os.ReadFile(phaseflow.AssignmentsPath(dir))
	if string(before) != string(after) {
		t.Error("the rerun rewrote the exported plan")
	}
}

// startPass sizes its pass for the model's window, like the /project run.
func TestStartPassUsesTheSizesForTheWindow(t *testing.T) {
	stepsPerPrompt := func(window int) []int {
		f := &planFake{}
		m, _, brief := projectModel(t, f)
		m = settle(t, submit(t, m, "/project Gophernote "+brief))
		m.planWindow = window // what the probe reports; read by the pass the answer starts
		before := len(f.seen())
		m = settle(t, keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS)))
		var out []int
		for _, p := range f.seen()[before:] {
			if strings.Contains(p, "Steps to specify now:") {
				out = append(out, len(stepsToSpecifyIn(p)))
			}
		}
		return out
	}
	if got := stepsPerPrompt(0); len(got) != 1 || got[0] != 2 {
		t.Errorf("with an unknown window the answer's pass sent %v step(s) per prompt, want one prompt of 2", got)
	}
	if got := stepsPerPrompt(8400); len(got) != 2 || got[0] != 1 || got[1] != 1 {
		t.Errorf("with an 8400 token window the answer's pass sent %v step(s) per prompt, want two prompts of 1", got)
	}
}

func stepsToSpecifyIn(prompt string) []string {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	if start < 0 || end <= start {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(prompt[start:end], "\n") {
		if strings.HasPrefix(line, "- ") {
			if id := roundStepID.FindString(line); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func TestPlanningWarningWhenNoSettingFits(t *testing.T) {
	if got := planningWarning(0); got != "" {
		t.Errorf("an unknown window warned: %q", got)
	}
	if got := planningWarning(32768); got != "" {
		t.Errorf("a 32768 token window warned: %q", got)
	}
	got := planningWarning(4096)
	for _, want := range []string{"no setting is safe", "context error"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q is missing %q", got, want)
		}
	}
}

func TestProjectSaysWhenTheWindowIsTooSmall(t *testing.T) {
	f := &planFake{}
	m, _, brief := projectModel(t, f)
	m.planWindow = 4096
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	if !strings.Contains(m.content, "no setting is safe") {
		t.Errorf("the transcript does not warn before the run:\n%s", m.content)
	}
	if strings.Index(m.content, "no setting is safe") > strings.Index(m.content, "skeleton:")+1 && strings.Contains(m.content, "skeleton:") {
		t.Errorf("the warning must come before the run's results:\n%s", m.content)
	}
}

var chunkProgress = regexp.MustCompile(`planning chunk (\d+) of (\d+)`)

func TestProjectShowsChunkProgress(t *testing.T) {
	f := &planFake{}
	m, _, brief := projectModel(t, f)
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	all := chunkProgress.FindAllStringSubmatch(m.content, -1)
	if len(all) < 2 {
		t.Fatalf("the transcript shows %d chunk progress line(s), want one per chunk:\n%s", len(all), m.content)
	}
	for i, a := range all {
		done, _ := strconv.Atoi(a[1])
		total, _ := strconv.Atoi(a[2])
		if done != i+1 || total != len(all) {
			t.Errorf("progress line %d says %d of %d, want %d of %d", i, done, total, i+1, len(all))
		}
	}
}

func TestOneLineStripsTerminalControls(t *testing.T) {
	cases := map[string]string{
		"plain text": "plain text",
		"a\x1b]8;;http://evil\x1b\\click\x1b]8;;\x1b\\ b": "a]8;;http://evil\\click]8;;\\ b",
		"red \x1b[31mtext\x1b[0m":                         "red [31mtext[0m",
		"bell\x07 nul\x00 del\x7f c1\u0085\u009b x":       "bell nul del c1 x",
		"tab\tand\nnewline":                               "tab and newline",
		"héllo wörld":                                     "héllo wörld",
	}
	for in, want := range cases {
		got := oneLine(in)
		if got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("oneLine(%q) kept control rune %U", in, r)
			}
		}
	}
	long := strings.Repeat("é", 200)
	if got := oneLine(long); len(got) > 160+len("…") || !strings.HasSuffix(got, "…") {
		t.Errorf("the cap is not kept: %d bytes", len(got))
	}
}

func TestScaffoldIsAnnouncedOnlyOnAFreshProject(t *testing.T) {
	f := &planFake{}
	m, _, brief := projectModel(t, f)
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	if !strings.Contains(m.content, "scaffolded") || !strings.Contains(m.content, "placeholder ROADMAP.md") {
		t.Errorf("a fresh project does not say .planning was scaffolded with placeholders:\n%s", m.content)
	}
	m = keys(t, m, key(tea.KeyEsc), key(tea.KeyEsc))
	m.content = ""
	m = settle(t, submit(t, m, "/project Gophernote"))
	if strings.Contains(m.content, "scaffolded") {
		t.Errorf("a resume announced a scaffold:\n%s", m.content)
	}
	if !strings.Contains(m.content, "Resuming the plan") {
		t.Errorf("the resume was not announced:\n%s", m.content)
	}
}

func TestLeavingTheApprovalPromptWithACommandSaysSo(t *testing.T) {
	m, _, _ := atApproval(t)
	m = submit(t, m, "/help")
	if m.proj != projNone {
		t.Fatalf("proj = %v", m.proj)
	}
	if !strings.Contains(m.content, "left unapproved") {
		t.Errorf("no line says the plan was left unapproved:\n%s", m.content)
	}
}

func TestProjectSaysInPlainWordsWhenAnotherRunHoldsThePlan(t *testing.T) {
	f := &planFake{}
	m, dir, brief := projectModel(t, f)
	repoDir := phaseflow.PlanningDir(dir)
	if err := os.MkdirAll(filepath.Join(plantree.Open(repoDir).Dir(), "_state"), 0o755); err != nil {
		t.Fatal(err)
	}
	other, err := lockfile.TryAcquire(filepath.Join(plantree.Open(repoDir).Dir(), "_state", "run.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer other()
	m = settle(t, submit(t, m, "/project Gophernote "+brief))
	for _, want := range []string{"another planning run is working on this plan", "nothing was changed"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}
	if len(f.seen()) != 0 {
		t.Error("a refused run still called the model")
	}
	_ = plan.ErrRunBusy
}
