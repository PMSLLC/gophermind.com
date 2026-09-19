package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

// This file hosts the question round (question_round.go) inside the session:
// the "/questions" command, the writes to the question store, and the pass-2
// run that follows. M6 makes "/project" enter the same round; until then this
// command is how the round is reached, and it works on the plan tree in
// <cwd>/.planning/plan.

// qPhase is the step of the question round the model is in.
type qPhase int

const (
	qNone    qPhase = iota
	qAsking         // the round is showing and owns the keyboard
	qRunning        // the answers are written and pass 2 is running
)

// maxRoundQuestions bounds one round, so a pass that asked forty questions
// still produces a screen a person can work through. The rest stay open and
// the next "/questions" asks them.
const maxRoundQuestions = 20

// questionsProgressMsg is a line from the pass-2 goroutine.
type questionsProgressMsg string

// questionsDoneMsg carries the finished pass and what the plan needs next.
type questionsDoneMsg struct {
	res     plan.Result2
	actions plantree.Actions
}

// planRepo opens the plan tree of the current working directory.
func planRepo() (*plantree.Repo, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return plantree.Open(phaseflow.PlanningDir(root)), nil
}

// planCompleter returns the completer planning passes run on: the injected
// one if a test supplied it, otherwise this session's client in a fresh
// two-message conversation. It is nil when there is no session, which is what
// makes the round usable in a test with no agent.
func (m model) planCompleter() plan.Completer {
	if m.completer != nil {
		return m.completer
	}
	if m.agent == nil {
		return nil
	}
	return plan.ClientCompleter{Client: m.agent.LLM()}
}

// handleQuestionsCommand dispatches "/questions" and "/questions change".
// With open questions it shows them for answering; with none (or when change
// is asked for) it shows the answered ones so the owner can change their
// mind.
func (m model) handleQuestionsCommand(text string) (model, tea.Cmd) {
	repo, err := planRepo()
	if err != nil {
		return m.questionsError(err.Error())
	}
	if _, err := repo.Get(plantree.RootID); err != nil {
		if errors.Is(err, plantree.ErrNotFound) {
			return m.questionsError("no plan in " + repo.Dir() + " yet")
		}
		return m.questionsError("the plan root cannot be read: " + err.Error())
	}
	wantChange := len(strings.Fields(text)) > 1 && strings.EqualFold(strings.Fields(text)[1], "change")

	open, err := plan.OpenQuestions(repo)
	if err != nil {
		return m.questionsError(err.Error())
	}
	// Steps a changed answer flagged wait for a reconciling pass whatever
	// became of the run that flagged them (it may have failed or been
	// cancelled), so with nothing to ask, /questions runs that pass.
	if len(open) == 0 && !wantChange {
		if n, err := plan.NeedsReplan(repo); err != nil {
			return m.questionsError(err.Error())
		} else if n > 0 {
			c := m.planCompleter()
			if c == nil {
				return m.questionsError(fmt.Sprintf("%d step(s) wait to be re-planned, but there is no active session", n))
			}
			m.appendLine(fmt.Sprintf("questions: %d step(s) still wait to be re-planned; running that now", n))
			nm, cmd := m.startPass(repo, c)
			return nm.(model), cmd
		}
	}
	mode, qs := roundAnswer, open
	if wantChange || len(open) == 0 {
		answered, err := answeredQuestions(repo)
		if err != nil {
			return m.questionsError(err.Error())
		}
		if len(answered) == 0 {
			if wantChange {
				return m.questionsError("no answered question to change")
			}
			return m.questionsError("no open questions; nothing to answer")
		}
		mode, qs = roundChange, answered
	}
	if len(qs) > maxRoundQuestions {
		m.appendLine(fmt.Sprintf("questions: showing the first %d of %d; run /questions again for the rest", maxRoundQuestions, len(qs)))
		qs = qs[:maxRoundQuestions]
	}

	why := make([]string, len(qs))
	excerptFailed := false
	for i, q := range qs {
		// The excerpt is context, so a failure to read it must not stop the
		// round: the question is still answerable without it. It is said once.
		ex, err := plan.ExcerptsFor(repo, q.Affects, roundWhyBytes)
		if err != nil {
			if !excerptFailed {
				m.appendLine("questions: the brief excerpts could not be read (" + oneLine(err.Error()) + "); asking without them")
				excerptFailed = true
			}
			continue
		}
		why[i] = ex
	}
	// Before the first size arrives the width is 0; a round of width 0 is
	// unbounded, which a later WindowSizeMsg corrects.
	w := m.width - 4
	if w < 1 {
		w = 0
	}
	m.round = newQuestionRound(mode, qs, why, w)
	m.qphase = qAsking
	verb := "Answering"
	if mode == roundChange {
		verb = "Revisiting"
	}
	m.appendLine(roundTitleStyle.Render(fmt.Sprintf("%s %d question(s) from the plan", verb, len(qs))))
	m.sync()
	return m, nil
}

// answeredQuestions returns the answered questions, newest last.
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

func (m model) questionsError(detail string) (model, tea.Cmd) {
	m.appendLine("questions: " + detail)
	m.sync()
	return m, nil
}

// handleRoundKey gives one key to the round and acts on what it returns. It
// runs after the session's own keys (Ctrl-C, Esc, "/exit"), so none of those
// is taken over by the round.
func (m model) handleRoundKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r, res := m.round.Update(msg)
	m.round = r
	switch {
	case res.Cancelled:
		m.endRound()
		m.appendLine("questions: cancelled, nothing was written")
		m.sync()
		return m, nil
	case res.Submitted:
		return m.submitRound()
	}
	return m, nil
}

// endRound drops the round and its state.
func (m *model) endRound() {
	m.qphase = qNone
	m.round = questionRound{}
}

// submitRound writes the answers the round collected and starts the pass that
// acts on them. Writing is done here rather than in the goroutine because it
// is local, quick and must be finished before anything reads the store; the
// model pass that follows is the slow part.
//
// Whether the pass reconciles is read from the tree afterwards, not from what
// this submit wrote: steps flagged by an earlier change (whose pass failed or
// was cancelled) must be re-planned by whatever pass runs next.
func (m model) submitRound() (tea.Model, tea.Cmd) {
	repo, err := planRepo()
	if err != nil {
		// The round stays open, so what was typed is not lost.
		return m.questionsErrorModel(err.Error())
	}
	answers := m.round.answers()
	m.endRound()

	// The answers the store holds now, read once: an unchanged answer must
	// never reach ChangeAnswer, which re-flags the steps already specified
	// from it. The round drops the ones it can see are unchanged, and this is
	// the last word.
	stored := map[string]plan.Answer{}
	if all, err := plan.LoadQuestions(repo); err == nil {
		for _, q := range all {
			if q.Status == plan.QuestionAnswered && q.Answer != nil {
				stored[q.ID] = *q.Answer
			}
		}
	}

	written, failed := 0, 0
	for _, a := range answers {
		if !a.Change {
			if _, err := plan.AnswerQuestion(repo, a.ID, a.Answer); err != nil {
				m.refused(a, err)
				failed++
				continue
			}
			written++
			continue
		}
		if old, ok := stored[a.ID]; ok && plan.SameAnswer(old, a.Answer) {
			m.appendLine("questions: " + a.ID + ": answer unchanged, left alone")
			continue
		}
		q, rec, err := plan.ChangeAnswer(repo, a.ID, a.Answer)
		if err != nil {
			m.refused(a, err)
			failed++
			continue
		}
		written++
		line := fmt.Sprintf("%s changed: %d step(s) to re-plan", q.ID, len(rec.Flagged)+len(rec.Refreshed))
		if len(rec.Refreshed) > 0 {
			line += fmt.Sprintf(" (%d already waiting, their reason now names this change)", len(rec.Refreshed))
		}
		m.appendLine(line)
		if len(rec.Executed) > 0 {
			m.appendLine("  already running or finished, left alone: " + strings.Join(rec.Executed, ", "))
		}
	}
	if len(answers) == 0 {
		m.appendLine("questions: nothing was answered or changed")
	} else {
		m.appendLine(fmt.Sprintf("questions: %d written, %d refused", written, failed))
	}

	waiting, err := plan.NeedsReplan(repo)
	if err != nil {
		return m.questionsErrorModel(err.Error())
	}
	if written == 0 && waiting == 0 {
		m.sync()
		return m, nil
	}
	c := m.planCompleter()
	if c == nil {
		m.appendLine("questions: no active session, so nothing was re-planned; run /questions again with a session")
		m.sync()
		return m, nil
	}
	return m.startPass(repo, c)
}

// refused reports a write the store refused. The note the owner typed is
// printed with it, because the round is gone and the store kept nothing.
func (m *model) refused(a roundAnswerOut, err error) {
	m.appendLine("questions: " + a.ID + ": " + err.Error())
	if note := strings.TrimSpace(a.Answer.Text); note != "" {
		m.appendLine("  your note for " + a.ID + " was: " + oneLine(note))
	}
}

// startPass runs RunPass2 in the background, sized for the model's context
// window. It reconciles exactly when the tree has steps waiting to be
// re-planned. It takes no run lock of its own (RunPass2 does); the session
// serializes runs through m.st and qphase.
func (m model) startPass(repo *plantree.Repo, c plan.Completer) (tea.Model, tea.Cmd) {
	m.qphase = qRunning
	m.st = stateWorking
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	client, known := m.planClient(), m.planWindow
	go func() {
		sub <- questionsProgressMsg("specifying the steps the answers released…")
		waiting, err := plan.NeedsReplan(repo)
		if err != nil {
			sub <- errMsg{err: err}
			return
		}
		// The same sizes /project uses: a pass sized for the defaults would
		// overflow a small window that /project itself planned for.
		_, opt2 := plan.SizesFor(windowOf(ctx, client, known))
		opt2.Reconcile = waiting > 0
		opt2.Progress = func(done, total int) {
			sub <- questionsProgressMsg(fmt.Sprintf("specifying batch %d of %d", done, total))
		}
		res, err := plan.RunPass2(ctx, repo, c, opt2)
		if err != nil {
			sub <- errMsg{err: err}
			return
		}
		actions, err := plan.NextActions(repo)
		if err != nil {
			sub <- errMsg{err: err}
			return
		}
		sub <- questionsDoneMsg{res: res, actions: actions}
	}()
	m.sync()
	return m, nil
}

// questionsErrorModel is questionsError for the callers that return tea.Model.
func (m model) questionsErrorModel(detail string) (tea.Model, tea.Cmd) {
	nm, cmd := m.questionsError(detail)
	return nm, cmd
}

// renderQuestionsResult is the transcript summary of a finished pass.
func renderQuestionsResult(res plan.Result2) string {
	line := fmt.Sprintf("plan updated: %d step(s) specified in %d pass(es)", res.Steps, res.Passes)
	if res.Reconciled > 0 {
		line += fmt.Sprintf(", %d re-planned", res.Reconciled)
	}
	if res.Released > 0 {
		line += fmt.Sprintf(", %d released by an answer", res.Released)
	}
	if res.Questions > 0 {
		line += fmt.Sprintf(", %d new question(s)", res.Questions)
	}
	if res.Unspecified > 0 {
		line += fmt.Sprintf(", %d still unspecified", res.Unspecified)
	}
	return line
}

// renderNextActions is the transcript view of what the plan needs next, at
// most a few lines however large the plan is.
func renderNextActions(a plantree.Actions) string {
	if len(a.Runnable) == 0 && len(a.Blocked) == 0 {
		return "next: nothing, the plan is complete"
	}
	if len(a.Blocked) == 0 && len(a.Runnable) == 1 && a.Runnable[0].Kind == plantree.ActionApprove {
		return "next: approve the plan, every step is specified"
	}
	const shown = 5
	var lines []string
	add := func(prefix string, actions []plantree.Action) {
		for i, x := range actions {
			if i == shown {
				lines = append(lines, fmt.Sprintf("  (%d more %s actions)", len(actions)-shown, prefix))
				return
			}
			lines = append(lines, fmt.Sprintf("  %s %s %s: %s", prefix, x.Kind, x.NodeID, oneLine(x.Reason)))
		}
	}
	add("next", a.Runnable)
	add("blocked", a.Blocked)
	return strings.Join(lines, "\n")
}

// questionsDialogText is the line shown in the dialog panel while a round or
// its pass is active.
func questionsDialogText(p qPhase) string {
	if p == qRunning {
		return "questions · re-planning the affected steps · esc or ctrl-c to stop"
	}
	return ""
}
