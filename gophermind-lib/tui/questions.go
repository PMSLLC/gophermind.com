package tui

import (
	"context"
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
		return m.questionsError("no plan in " + repo.Dir() + " yet")
	}
	wantChange := len(strings.Fields(text)) > 1 && strings.EqualFold(strings.Fields(text)[1], "change")

	open, err := plan.OpenQuestions(repo)
	if err != nil {
		return m.questionsError(err.Error())
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
	for i, q := range qs {
		// The excerpt is context, so a failure to read it must not stop the
		// round: the question is still answerable without it.
		if ex, err := plan.ExcerptsFor(repo, q.Affects, roundWhyBytes); err == nil {
			why[i] = ex
		}
	}
	m.round = newQuestionRound(mode, qs, why, m.width-4)
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
func (m model) submitRound() (tea.Model, tea.Cmd) {
	repo, err := planRepo()
	if err != nil {
		m.endRound()
		return m.questionsErrorModel(err.Error())
	}
	answers := m.round.answers()
	m.endRound()
	if len(answers) == 0 {
		m.appendLine("questions: nothing was answered or changed")
		m.sync()
		return m, nil
	}

	written, changed, failed := 0, false, 0
	for _, a := range answers {
		if !a.Change {
			if _, err := plan.AnswerQuestion(repo, a.ID, a.Answer); err != nil {
				m.appendLine("questions: " + a.ID + ": " + err.Error())
				failed++
				continue
			}
			written++
			continue
		}
		// An unchanged answer must never reach ChangeAnswer: it re-flags the
		// steps already specified from it. The round drops the ones it can
		// see are unchanged, and this checks the store itself as the last
		// word.
		if stored, ok := storedAnswer(repo, a.ID); ok && sameRoundAnswer(stored, a.Answer) {
			m.appendLine("questions: " + a.ID + ": answer unchanged, left alone")
			continue
		}
		q, rec, err := plan.ChangeAnswer(repo, a.ID, a.Answer)
		if err != nil {
			m.appendLine("questions: " + a.ID + ": " + err.Error())
			failed++
			continue
		}
		written++
		changed = true
		m.appendLine(fmt.Sprintf("%s changed: %d step(s) to re-plan", q.ID, len(rec.Flagged)))
		if len(rec.Executed) > 0 {
			m.appendLine("  already running or finished, left alone: " + strings.Join(rec.Executed, ", "))
		}
	}
	m.appendLine(fmt.Sprintf("questions: %d written, %d refused", written, failed))
	if written == 0 {
		m.sync()
		return m, nil
	}

	c := m.planCompleter()
	if c == nil {
		m.appendLine("questions: no active session, so nothing was re-planned; run /questions again with a session")
		m.sync()
		return m, nil
	}
	m.qphase = qRunning
	m.st = stateWorking
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	reconcile := changed
	go func() {
		// RunPass2 reports nothing until it returns (its progress counters are
		// per call, not per step), so this is the one line the run can promise
		// before its result.
		sub <- questionsProgressMsg("specifying the steps the answers released…")
		res, err := plan.RunPass2(ctx, repo, c, plan.Options2{Reconcile: reconcile})
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

// storedAnswer returns the answer the store holds for question id, if any.
func storedAnswer(repo *plantree.Repo, id string) (plan.Answer, bool) {
	all, err := plan.LoadQuestions(repo)
	if err != nil {
		return plan.Answer{}, false
	}
	for _, q := range all {
		if q.ID == id && q.Status == plan.QuestionAnswered && q.Answer != nil {
			return *q.Answer, true
		}
	}
	return plan.Answer{}, false
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
				lines = append(lines, fmt.Sprintf("  (%d more %s)", len(actions)-shown, strings.TrimSpace(prefix)))
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
