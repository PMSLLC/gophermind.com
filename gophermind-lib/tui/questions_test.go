package tui

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

var roundStepID = regexp.MustCompile(`phase-\d{3}\.task-\d{3}\.step-\d{3}`)

// specCompleter answers a pass-2 prompt with a specification for every step
// it asks for, and records what it was asked.
type specCompleter struct {
	mu      sync.Mutex
	prompts []string
}

func (c *specCompleter) Complete(_ context.Context, prompt string) (string, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, prompt)
	c.mu.Unlock()
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
					AcceptanceCriteria: []string{"it works"}, TestCommand: []string{"go", "test", "./..."},
					DependsOn: []string{},
				})
			}
		}
	}
	b, err := json.Marshal(plan.Pass2Output{Steps: steps})
	return string(b), err
}

func (c *specCompleter) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.prompts...)
}

// seedPlan writes a two-step plan into dir/.planning/plan with one open
// question holding both steps, which is the state a pass leaves behind.
func seedPlan(t *testing.T, dir string) *plantree.Repo {
	t.Helper()
	repo := plantree.Open(phaseflow.PlanningDir(dir))
	node := func(id, title string) plantree.Node {
		ref, err := plantree.ParentRef(id)
		if err != nil {
			t.Fatal(err)
		}
		n := plantree.Node{
			SchemaVersion: plantree.SchemaVersion, ID: id, Title: title, NodeRevision: 1,
			ContextDigest: "why " + id, DependsOn: []string{},
			Planning: plantree.Planning{Stage: plantree.StageSkeleton},
		}
		if id != plantree.RootID {
			n.ParentRef = &ref
		}
		if n.Kind() == plantree.KindStep {
			n.Status = plantree.StatusUntouched
		}
		return n
	}
	if err := repo.Init(node(plantree.RootID, "demo")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		if err := repo.Create(node(id, "node "+id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := plan.AddQuestions(repo, []plan.NewQuestion{{
		Question: "Which database?", Why: "the schema depends on it",
		Options:           []plan.NewOption{{Label: "SQLite", Description: "embedded"}, {Label: "Postgres", Description: "server"}},
		RecommendedLabels: []string{"SQLite"}, Rationale: "simplest to run",
		Affects: []string{"phase-001.task-001"}, Source: "chunk 1 of the brief",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.HoldOpen(repo); err != nil { // a real pass holds the steps it asked about
		t.Fatal(err)
	}
	return repo
}

// roundModel is a session whose planning passes run on a fake completer, in a
// temporary working directory holding a seeded plan.
func roundModel(t *testing.T) (model, *plantree.Repo, *specCompleter) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	repo := seedPlan(t, dir)
	m := testModel(t)
	c := &specCompleter{}
	m.completer = c
	return m, repo, c
}

// submit runs one line through handleSubmit.
func submit(t *testing.T, m model, line string) model {
	t.Helper()
	m.input.SetValue(line)
	next, _ := m.handleSubmit()
	return next
}

// keys drives the model's key handler, which is what the round is reached
// through in a real session.
func keys(t *testing.T, m model, msgs ...tea.KeyMsg) model {
	t.Helper()
	for _, k := range msgs {
		next, _ := m.handleKey(k)
		m = next.(model)
	}
	return m
}

// settle delivers whatever the background pass posts until the model is idle
// again, so a test sees the transcript a user would.
func settle(t *testing.T, m model) model {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for m.st != stateIdle {
		select {
		case msg := <-m.sub:
			next, _ := m.Update(msg)
			m = next.(model)
		case <-deadline:
			t.Fatalf("the pass did not finish; transcript:\n%s", m.content)
		}
	}
	return m
}

// waitPass reads messages until the background pass has reported, so nothing
// is still running when a test returns.
func waitPass(t *testing.T, m model) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-m.sub:
			switch msg.(type) {
			case questionsDoneMsg, errMsg:
				return
			}
		case <-deadline:
			t.Fatal("the pass did not report")
		}
	}
}

func TestSlashQuestionsNeedsAPlan(t *testing.T) {
	t.Chdir(t.TempDir())
	m := submit(t, testModel(t), "/questions")
	if m.qphase != qNone || !strings.Contains(m.content, "no plan in") {
		t.Errorf("phase = %v, transcript = %q", m.qphase, m.content)
	}
}

func TestSlashQuestionsOpensTheRoundAndShowsTheBriefBehindIt(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions")
	if m.qphase != qAsking || m.round.count() != 1 {
		t.Fatalf("phase = %v with %d questions", m.qphase, m.round.count())
	}
	if !strings.Contains(m.content, "Answering 1 question(s)") {
		t.Errorf("transcript = %q", m.content)
	}
	v := m.View()
	if !strings.Contains(v, "Which database?") || !strings.Contains(v, "(recommended)") {
		t.Errorf("the round is not on screen:\n%s", v)
	}
	// There is no brief here, so the excerpt is simply absent, not an error.
	if strings.Contains(v, "from the brief:") {
		t.Errorf("an excerpt appeared without a brief:\n%s", v)
	}
}

func TestSlashQuestionsChangeNeedsAnAnsweredQuestion(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions change")
	if m.qphase != qNone || !strings.Contains(m.content, "no answered question to change") {
		t.Errorf("phase = %v, transcript = %q", m.qphase, m.content)
	}
}

func TestQuestionRoundEscapeWritesNothing(t *testing.T) {
	m, repo, c := roundModel(t)
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeyEsc))
	if m.qphase != qNone || !strings.Contains(m.content, "cancelled") {
		t.Errorf("phase = %v, transcript = %q", m.qphase, m.content)
	}
	open, err := plan.OpenQuestions(repo)
	if err != nil || len(open) != 1 {
		t.Errorf("the question was written after a cancel: %+v, %v", open, err)
	}
	if len(c.seen()) != 0 {
		t.Error("a cancelled round ran a pass")
	}
}

func TestQuestionRoundAnswerReleasesTheStepsAndSpecifiesThem(t *testing.T) {
	m, repo, c := roundModel(t)
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qRunning || m.st != stateWorking {
		t.Fatalf("phase = %v, state = %v, want a pass running", m.qphase, m.st)
	}
	m = settle(t, m)

	if m.qphase != qNone {
		t.Errorf("phase = %v after the pass", m.qphase)
	}
	for _, want := range []string{"questions: 1 written, 0 refused", "2 step(s) specified", "2 released by an answer", "next: approve the plan, every step is specified"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}
	qs, err := plan.LoadQuestions(repo)
	if err != nil || len(qs) != 1 || qs[0].Status != plan.QuestionAnswered || qs[0].Answer.OptionIDs[0] != "opt-1" {
		t.Fatalf("stored question = %+v, %v", qs, err)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		n, err := repo.Get(id)
		if err != nil || n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s = %v, %v", id, n.Planning.Stage, err)
		}
	}
	if seen := c.seen(); len(seen) != 1 || !strings.Contains(seen[0], "Which database?") {
		t.Errorf("the pass did not see the decision: %d prompts", len(seen))
	}
}

func TestQuestionRoundWithoutASessionWritesTheAnswersAnyway(t *testing.T) {
	m, repo, _ := roundModel(t)
	m.completer = nil // no injected completer and no agent: nothing can run
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qNone || m.st != stateIdle {
		t.Fatalf("phase = %v, state = %v", m.qphase, m.st)
	}
	if !strings.Contains(m.content, "no active session") {
		t.Errorf("transcript = %q", m.content)
	}
	qs, _ := plan.LoadQuestions(repo)
	if len(qs) != 1 || qs[0].Status != plan.QuestionAnswered {
		t.Errorf("the answer was not written: %+v", qs)
	}
}

// TestQuestionRoundLeavesTheSessionKeysAlone covers the ordering in
// update.go: Ctrl-C and Esc keep their meaning while the round is showing.
func TestQuestionRoundLeavesTheSessionKeysAlone(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions")

	// Ctrl-C while the round is showing quits, exactly as it does when idle.
	_, cmd := m.handleKey(key(tea.KeyCtrlC))
	if cmd == nil {
		t.Fatal("ctrl-c did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl-c produced %T, want tea.QuitMsg", cmd())
	}

	// Typing goes to the round, not to the input line, so nothing is left
	// behind in the prompt when the round ends.
	m = keys(t, m, runes("e"), runes("some note"), key(tea.KeyEsc), key(tea.KeyEsc))
	if m.qphase != qNone {
		t.Fatalf("phase = %v, want the round gone", m.qphase)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("input = %q, want the round to have kept the keys", got)
	}
	// With the round gone, "/exit" works as it always has.
	m.input.SetValue("/exit")
	_, cmd = m.handleKey(key(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("/exit did not quit after the round")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("/exit produced %T, want tea.QuitMsg", cmd())
	}
}

func TestSlashQuestionsIsRegistered(t *testing.T) {
	found := false
	for _, name := range commandNames() {
		if name == "/questions" {
			found = true
		}
	}
	if !found {
		t.Fatal("/questions missing from commandNames()")
	}
}

// TestQuestionRoundCancelledPassEndsThePhase covers the path a Ctrl-C during
// the pass takes: the cancelled turn arrives as an errMsg, and the session
// must come back to an ordinary idle prompt rather than a phase whose keys
// nothing handles.
func TestQuestionRoundCancelledPassEndsThePhase(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qRunning {
		t.Fatalf("phase = %v", m.qphase)
	}
	// The pass runs on a goroutine. Let it finish before the cancelled
	// message is delivered, so it is not still writing to the temporary
	// directory when the test ends.
	waitPass(t, m)
	next, _ := m.Update(errMsg{err: context.Canceled})
	m = next.(model)
	if m.qphase != qNone || m.st != stateIdle || m.cancel != nil {
		t.Errorf("after a cancelled pass: phase=%v state=%v cancel=%v", m.qphase, m.st, m.cancel != nil)
	}
	if !strings.Contains(m.content, "cancelled") {
		t.Errorf("transcript = %q", m.content)
	}
}

// TestQuestionRoundNeverRechangesAnUnchangedAnswer covers the host's own guard.
// The round drops an unchanged answer when it can see the stored one, but a
// stale round (built before the answer was stored) cannot, and an unchanged
// call to plan.ChangeAnswer re-flags the steps already specified from it.
func TestQuestionRoundNeverRechangesAnUnchangedAnswer(t *testing.T) {
	m, repo, c := roundModel(t)
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	m = settle(t, m)
	before := len(c.seen())

	stale, err := plan.LoadQuestions(repo)
	if err != nil || len(stale) != 1 {
		t.Fatalf("questions = %+v, %v", stale, err)
	}
	q := stale[0]
	q.Answer = nil // a round built before the answer was stored has not seen it, yet knows it is a change
	m.round = newQuestionRound(roundAnswer, []plan.Question{q}, nil, 76)
	m.qphase = qAsking
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS)) // the same option again

	if !strings.Contains(m.content, "answer unchanged") {
		t.Errorf("transcript = %q", m.content)
	}
	if m.qphase != qNone || m.st != stateIdle {
		t.Errorf("phase = %v, state = %v, want no pass for an unchanged answer", m.qphase, m.st)
	}
	if got := len(c.seen()); got != before {
		t.Errorf("an unchanged answer ran a pass: %d prompts, was %d", got, before)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		n, err := repo.Get(id)
		if err != nil || n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s = %v, %v: an unchanged answer re-flagged it", id, n.Planning.Stage, err)
		}
	}

	// A different answer still goes through ChangeAnswer and re-flags.
	m.round = newQuestionRound(roundAnswer, []plan.Question{q}, nil, 76)
	m.qphase = qAsking
	m = keys(t, m, key(tea.KeyRight), key(tea.KeySpace), key(tea.KeyCtrlS))
	if !strings.Contains(m.content, "changed: ") {
		t.Errorf("a changed answer was not written as a change: %q", m.content)
	}
	if m.qphase != qRunning {
		t.Fatalf("phase = %v, want the pass for a changed answer running", m.qphase)
	}
	waitPass(t, m)
}

// TestQuestionRoundIsCappedToTheTerminalHeight checks the round cannot push
// the input and status off a short screen, however many options it lists.
func TestQuestionRoundIsCappedToTheTerminalHeight(t *testing.T) {
	m := testModel(t)
	m.height = 14
	applyInputHeight(&m)
	var opts []plan.Option
	for i := 0; i < 15; i++ {
		opts = append(opts, plan.Option{ID: "opt-" + strings.Repeat("x", i+1), Label: "option"})
	}
	qs := []plan.Question{{ID: "q-1", Question: "Which?", Why: "because", Options: opts, Status: plan.QuestionOpen}}
	m.round = newQuestionRound(roundAnswer, qs, []string{"an excerpt"}, m.width-4)
	m.qphase = qAsking

	v := m.View()
	if h := lipgloss.Height(v); h > m.height {
		t.Errorf("frame is %d rows on a %d row terminal:\n%s", h, m.height, v)
	}
	lines := strings.Split(v, "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "ready") {
		t.Errorf("the status line is not last: %q", last)
	}
	if !strings.Contains(v, "╭") || !strings.Contains(v, "╰") {
		t.Errorf("the input box is missing:\n%s", v)
	}
}

// TestQuestionRoundEndToEnd is the whole M5 loop in one session: answer the
// open question, let pass 2 specify the steps, then change that answer, let
// the reconciling pass re-specify exactly the steps it invalidated, and end
// with nothing left but approval.
func TestQuestionRoundEndToEnd(t *testing.T) {
	m, repo, c := roundModel(t)

	m = settle(t, keys(t, submit(t, m, "/questions"), key(tea.KeySpace), key(tea.KeyCtrlS)))
	if !strings.Contains(m.content, "next: approve the plan, every step is specified") {
		t.Fatalf("after answering, the plan is not complete:\n%s", m.content)
	}

	m = submit(t, m, "/questions")
	if m.qphase != qAsking || m.round.mode != roundChange {
		t.Fatalf("with nothing open, /questions must offer the answered ones: phase=%v mode=%v", m.qphase, m.round.mode)
	}
	// Choose the other option and submit: this invalidates both specifications.
	m = keys(t, m, key(tea.KeyRight), key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qRunning {
		t.Fatalf("a changed answer did not start a pass: %v, %q", m.qphase, m.content)
	}
	m = settle(t, m)

	for _, want := range []string{"q-001 changed: 2 step(s) to re-plan", "2 re-planned", "next: approve the plan, every step is specified"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}
	qs, err := plan.LoadQuestions(repo)
	if err != nil || len(qs) != 1 || len(qs[0].PriorAnswers) != 1 || qs[0].Answer.OptionIDs[0] != "opt-2" {
		t.Fatalf("stored question = %+v, %v", qs, err)
	}
	actions, err := plan.NextActions(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions.Blocked) != 0 || len(actions.Runnable) != 1 || actions.Runnable[0].Kind != plantree.ActionApprove {
		t.Errorf("NextActions = %+v, want only approval", actions)
	}
	// Two passes: one for the first two steps, then the re-planning pass.
	if seen := c.seen(); len(seen) != 2 {
		t.Errorf("%d passes, want 2", len(seen))
	} else if !strings.Contains(seen[1], "previous specification: build phase-001.task-001.step-001") {
		t.Error("the re-planning pass was not shown what it was replacing")
	}
}
