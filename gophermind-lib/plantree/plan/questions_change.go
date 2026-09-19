package plan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/plantree"
)

// reconcileNoteBytes bounds the resume note a changed answer leaves on a step.
const reconcileNoteBytes = 300

// Reconciled reports what a changed answer did to the tree.
type Reconciled struct {
	// Flagged lists, in tree order, the steps moved to
	// needs_reconciliation: they were specified from the old answer and must
	// be specified again (RunPass2 with Options2.Reconcile).
	Flagged []string
	// Executed lists steps the answer affects that are already in progress or
	// completed. They are left exactly as they are, because re-planning
	// finished work is not this function's decision to make; a caller should
	// show them to the owner.
	Executed []string
	// Refreshed lists steps that were already at needs_reconciliation from an
	// earlier change and whose resume note now names this change instead, so
	// the note and the decisions block of the next pass agree.
	Refreshed []string
}

// reconcileNotePrefix starts every resume note a changed answer leaves. Only
// a note with this prefix explains a reconcile action: a step's note can also
// hold the summary of a specification it lost, which explains nothing new.
const reconcileNotePrefix = "re-plan: the answer to "

// ChangeAnswer replaces the answer of an already answered question. The new
// answer is validated exactly like a first answer, the old one is kept in the
// question's bounded history, and every step that was specified from the old
// answer is moved to stage needs_reconciliation with a resume note naming the
// question, so the next reconciling pass 2 re-specifies it and nothing else.
//
// Changing an answer to what it already says adds no history entry and does
// not rewrite questions.json, but it still flags any step affected by the
// question that is not yet flagged. The answer is saved before the steps are
// flagged, so a call that died in between is repaired by repeating it.
//
// A question that is still open is refused with ErrNotAnswered; answer it
// with AnswerQuestion instead.
func ChangeAnswer(repo *plantree.Repo, id string, a Answer) (Question, Reconciled, error) {
	unlock, err := lockQuestions(repo)
	if err != nil {
		return Question{}, Reconciled{}, err
	}
	defer unlock()
	f, err := loadQuestionFile(repo)
	if err != nil {
		return Question{}, Reconciled{}, err
	}
	for i := range f.Questions {
		q := &f.Questions[i]
		if q.ID != id {
			continue
		}
		if q.Status != QuestionAnswered || q.Answer == nil {
			return Question{}, Reconciled{}, fmt.Errorf("%w: %s", ErrNotAnswered, id)
		}
		if err := checkAnswer(*q, a); err != nil {
			return Question{}, Reconciled{}, err
		}
		next := Answer{OptionIDs: append([]string{}, a.OptionIDs...), Text: strings.TrimSpace(a.Text)}
		if SameAnswer(*q.Answer, next) {
			// Nothing to record, but a previous call may have saved the answer
			// and died before flagging. Flagging is idempotent, so finish it.
			rec, err := flagForReconciliation(repo, *q)
			return *q, rec, err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		q.PriorAnswers = appendHistory(q.PriorAnswers, *q.Answer, q.AnsweredAt, now)
		q.Answer = &next
		q.AnsweredAt = now
		if err := saveQuestionFile(repo, f); err != nil {
			return Question{}, Reconciled{}, err
		}
		rec, err := flagForReconciliation(repo, *q)
		return *q, rec, err
	}
	return Question{}, Reconciled{}, fmt.Errorf("%w: %s", ErrNoSuchQuestion, id)
}

// SameAnswer reports whether two answers say the same thing. Option order is
// not part of an answer, so a re-ordered selection is the same answer.
func SameAnswer(a, b Answer) bool {
	if strings.TrimSpace(a.Text) != strings.TrimSpace(b.Text) || len(a.OptionIDs) != len(b.OptionIDs) {
		return false
	}
	x := append([]string{}, a.OptionIDs...)
	y := append([]string{}, b.OptionIDs...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// appendHistory adds the replaced answer to hist, keeping the newest
// maxAnswerHistory entries and cutting each stored text to historyTextBytes.
func appendHistory(hist []PriorAnswer, old Answer, answeredAt, replacedAt string) []PriorAnswer {
	hist = append(hist, PriorAnswer{
		OptionIDs:  append([]string{}, old.OptionIDs...),
		Text:       cutBytes(old.Text, historyTextBytes),
		AnsweredAt: answeredAt,
		ReplacedAt: replacedAt,
	})
	if len(hist) > maxAnswerHistory {
		hist = append([]PriorAnswer{}, hist[len(hist)-maxAnswerHistory:]...)
	}
	return hist
}

// flagForReconciliation moves every step the question affects that already
// carries a specification (drafted or approved, and not on hold) to stage
// needs_reconciliation, with a resume note saying which decision changed. A
// step that still waits for its first specification is left where it is: the
// new answer reaches it through the prompt anyway.
func flagForReconciliation(repo *plantree.Repo, q Question) (Reconciled, error) {
	steps, err := stepsUnder(repo, q.Affects)
	if err != nil {
		return Reconciled{}, err
	}
	note := cutBytes(reconcileNotePrefix+q.ID+" changed: "+oneLine(decisionLine(q)), reconcileNoteBytes)
	var rec Reconciled
	for _, s := range steps {
		if onHold(s) {
			continue
		}
		if s.Status == plantree.StatusInProgress || s.Status == plantree.StatusCompleted {
			rec.Executed = append(rec.Executed, s.ID)
			continue
		}
		if s.Planning.Stage == plantree.StageNeedsReconciliation {
			// Flagged by an earlier change and not yet re-planned: keep it
			// flagged, but make its note say the newest change.
			if s.ResumeNote == note {
				continue
			}
			if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
				n.ResumeNote = note
				return nil
			}); err != nil {
				return rec, fmt.Errorf("refreshing the note of %s: %w", s.ID, err)
			}
			rec.Refreshed = append(rec.Refreshed, s.ID)
			continue
		}
		if s.Planning.Stage != plantree.StageDrafted && s.Planning.Stage != plantree.StageApproved {
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageNeedsReconciliation
			n.ResumeNote = note
			return nil
		}); err != nil {
			return rec, fmt.Errorf("flagging %s for re-planning: %w", s.ID, err)
		}
		rec.Flagged = append(rec.Flagged, s.ID)
	}
	return rec, nil
}
