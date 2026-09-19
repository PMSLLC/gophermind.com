package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

// failCompleter fails every call, which is what a cancelled or broken pass
// looks like to RunPass2: it returns an error and leaves the tree as it was.
type failCompleter struct{}

func (failCompleter) Complete(context.Context, string) (string, error) {
	return "", errors.New("model unreachable")
}

// runFailingPass runs the current model's pass to its error and delivers it.
func runFailingPass(t *testing.T, m model) model {
	t.Helper()
	deadline := 0
	for m.st != stateIdle {
		msg := <-m.sub
		next, _ := m.Update(msg)
		m = next.(model)
		if deadline++; deadline > 50 {
			t.Fatal("the pass did not end")
		}
	}
	return m
}

// flaggedModel answers the seeded question, then changes the answer while the
// completer fails, so both steps are left at needs_reconciliation.
func flaggedModel(t *testing.T) (model, *plantree.Repo, *specCompleter) {
	t.Helper()
	m, repo, good := roundModel(t)
	m = settle(t, keys(t, submit(t, m, "/questions"), key(tea.KeySpace), key(tea.KeyCtrlS)))
	m.completer = failCompleter{}
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeyRight), key(tea.KeySpace), key(tea.KeyCtrlS))
	m = runFailingPass(t, m)
	if n, err := plan.NeedsReplan(repo); err != nil || n != 2 {
		t.Fatalf("setup: %d steps flagged, %v", n, err)
	}
	m.completer = good
	return m, repo, good
}

func TestSlashQuestionsRePlansStepsAFailedPassLeftFlagged(t *testing.T) {
	m, repo, c := flaggedModel(t)
	before := len(c.seen())
	m = submit(t, m, "/questions")
	if m.qphase != qRunning {
		t.Fatalf("phase = %v, want the re-plan running; transcript:\n%s", m.qphase, m.content)
	}
	m = settle(t, m)
	if n, _ := plan.NeedsReplan(repo); n != 0 {
		t.Errorf("%d steps still flagged", n)
	}
	if len(c.seen()) != before+1 || !strings.Contains(m.content, "re-planned") {
		t.Errorf("no re-planning pass ran: %d prompts, transcript:\n%s", len(c.seen()), m.content)
	}
}

func TestAnsweringANewQuestionRePlansTheFlaggedStepsToo(t *testing.T) {
	m, repo, c := flaggedModel(t)
	if _, err := plan.AddQuestions(repo, []plan.NewQuestion{{
		Question: "Which cache?", Why: "speed",
		Options: []plan.NewOption{{Label: "none"}, {Label: "redis"}},
		Affects: []string{"phase-001.task-001.step-001"}, Source: "chunk 1",
	}}); err != nil {
		t.Fatal(err)
	}
	m = submit(t, m, "/questions")
	if m.qphase != qAsking || m.round.mode != roundAnswer {
		t.Fatalf("phase=%v mode=%v, want the new question offered", m.qphase, m.round.mode)
	}
	m = settle(t, keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS)))
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		n, err := repo.Get(id)
		if err != nil || n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s = %v, %v; want re-planned", id, n.Planning.Stage, err)
		}
	}
	last := c.seen()[len(c.seen())-1]
	if !strings.Contains(last, "previous specification:") {
		t.Errorf("the last pass was not a reconciling one")
	}
}

func TestSkippingEverythingStillRePlansFlaggedSteps(t *testing.T) {
	m, repo, _ := flaggedModel(t)
	if _, err := plan.AddQuestions(repo, []plan.NewQuestion{{
		Question: "Which cache?", Why: "speed",
		Options: []plan.NewOption{{Label: "none"}, {Label: "redis"}},
		Affects: []string{"phase-001.task-001.step-001"}, Source: "chunk 1",
	}}); err != nil {
		t.Fatal(err)
	}
	m = submit(t, m, "/questions")
	m = keys(t, m, runes("s"), key(tea.KeyCtrlS))
	if m.qphase != qRunning {
		t.Fatalf("nothing was written but flagged steps wait: phase=%v\n%s", m.qphase, m.content)
	}
	settle(t, m)
	if n, _ := plan.NeedsReplan(repo); n != 0 {
		t.Errorf("%d steps still flagged", n)
	}
}

func TestSecondChangeOfAFlaggedStepIsReportedTruthfully(t *testing.T) {
	m, repo, _ := flaggedModel(t)
	m.completer = failCompleter{}
	m = submit(t, m, "/questions change")
	if m.qphase != qAsking {
		t.Fatalf("phase = %v\n%s", m.qphase, m.content)
	}
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	if strings.Contains(m.content, "changed: 0 step(s)") {
		t.Errorf("the line claims nothing to re-plan:\n%s", m.content)
	}
	if !strings.Contains(m.content, "already waiting") {
		t.Errorf("transcript does not say the steps were already waiting:\n%s", m.content)
	}
	m = runFailingPass(t, m)
	n, _ := repo.Get("phase-001.task-001.step-001")
	if !strings.Contains(n.ResumeNote, "SQLite") {
		t.Errorf("the note = %q, want the newest answer", n.ResumeNote)
	}
}

func TestWindowResizeReachesTheRound(t *testing.T) {
	m, _, _ := roundModel(t)
	m.width, m.height = 100, 30
	m = submit(t, m, "/questions")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 30})
	m = next.(model)
	if m.round.width != 46 {
		t.Errorf("round width = %d after a resize to 50 columns, want 46", m.round.width)
	}
}

func TestRoundOpenedBeforeTheFirstSizeIsNotNegative(t *testing.T) {
	m, _, _ := roundModel(t)
	m.width = 0
	m = submit(t, m, "/questions")
	if m.round.width < 0 {
		t.Errorf("round width = %d", m.round.width)
	}
}

func TestKeepBottomRowsCountsWrappedLines(t *testing.T) {
	s := strings.Repeat("x", 100) + "\nlast line"
	got := keepBottomRows(s, 3, 20)
	if rows := strings.Count(got, "\n") + 1; rows > 3 {
		t.Errorf("kept %d rows, budget 3:\n%s", rows, got)
	}
	if !strings.Contains(got, "last line") {
		t.Errorf("the bottom row was dropped: %q", got)
	}
}

func TestQuestionRoundNeverOverflowsAtAnyHeight(t *testing.T) {
	for h := 1; h <= 30; h++ {
		m := testModel(t)
		m.height = h
		applyInputHeight(&m)
		qs := []plan.Question{roundQuestion("q-1", "Which?", false)}
		m.round = newQuestionRound(roundAnswer, qs, []string{"an excerpt"}, m.width-4)
		m.qphase = qAsking
		v := m.View()
		if h >= 8 && lipgloss.Height(v) > h {
			t.Errorf("height %d: frame is %d rows", h, lipgloss.Height(v))
		}
	}
}

func TestErrorWhileAskingDoesNotDropTheRound(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions")
	next, _ := m.Update(errMsg{err: errors.New("unrelated")})
	m = next.(model)
	if m.qphase != qAsking {
		t.Errorf("an unrelated error ended the round: phase=%v", m.qphase)
	}
}

func TestACorruptRootIsReportedNotCalledNoPlan(t *testing.T) {
	m, repo, _ := roundModel(t)
	if err := os.WriteFile(filepath.Join(repo.Dir(), "plan.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = submit(t, m, "/questions")
	if strings.Contains(m.content, "no plan in") {
		t.Errorf("a corrupt root was called a missing plan:\n%s", m.content)
	}
	if !strings.Contains(m.content, "questions: ") {
		t.Errorf("no error shown:\n%s", m.content)
	}
}

func TestExcerptFailureIsShownOnceAndTheRoundStillOpens(t *testing.T) {
	m, repo, _ := roundModel(t)
	dir := filepath.Join(repo.Dir(), "_state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provenance.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = submit(t, m, "/questions")
	if m.qphase != qAsking {
		t.Fatalf("the round did not open: %v", m.qphase)
	}
	if !strings.Contains(m.content, "brief excerpt") {
		t.Errorf("the failure was not shown:\n%s", m.content)
	}
}

func TestAFailedWriteKeepsTheTypedNote(t *testing.T) {
	m, repo, _ := roundModel(t)
	m = submit(t, m, "/questions")
	// Someone else answers the question while the round is open, so this
	// submit is refused.
	qs, _ := plan.LoadQuestions(repo)
	if _, err := plan.AnswerQuestion(repo, qs[0].ID, plan.Answer{OptionIDs: []string{"opt-1"}}); err != nil {
		t.Fatal(err)
	}
	m = keys(t, m, key(tea.KeySpace), runes("e"), runes("keep this thought"), key(tea.KeyEsc), key(tea.KeyCtrlS))
	if !strings.Contains(m.content, "keep this thought") {
		t.Errorf("the typed note was lost with the failed write:\n%s", m.content)
	}
}

func TestQuestionRoundShowsALongExcerptWrappedWithinItsWidth(t *testing.T) {
	long := strings.Repeat("alpha beta gamma ", 25) // 425 bytes, cut to 400
	r := newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-1", "Which?", false)}, []string{long}, 100)
	v := r.View()
	shown := 0
	rows := 0
	for _, l := range strings.Split(v, "\n") {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line is %d columns wide, limit 100: %q", w, l)
		}
		if strings.Contains(l, "from the brief:") || (rows > 0 && rows < 3 && strings.HasPrefix(l, strings.Repeat(" ", 18))) {
			rows++
			shown += len(strings.TrimSpace(strings.TrimPrefix(l, "  from the brief:")))
		}
	}
	if shown <= 160 {
		t.Errorf("only %d bytes of the excerpt are shown", shown)
	}
	if rows > 3 {
		t.Errorf("excerpt takes %d rows, want at most 3", rows)
	}
}
