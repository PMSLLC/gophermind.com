package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/router"
)

type clarifyQuestion struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Why      string `json:"why_it_matters"`
	Default  string `json:"default_if_unanswered"`
}

// parseClarify decodes the Clarify reply. Ids are renumbered q1, q2, ... so
// they are unique whatever the model wrote.
func parseClarify(text string) ([]clarifyQuestion, error) {
	qs := []clarifyQuestion{}
	if err := json.Unmarshal([]byte(text), &qs); err != nil {
		return nil, fmt.Errorf("clarify reply is not a JSON array of questions: %w", err)
	}
	for i := range qs {
		if strings.TrimSpace(qs[i].Question) == "" {
			return nil, fmt.Errorf("clarify reply: question %d has no text", i+1)
		}
		qs[i].ID = fmt.Sprintf("q%d", i+1)
	}
	return qs, nil
}

func clarifyDone(r *run) bool { return exists(r.path(fileAnswers)) }

// clarify asks the model what it needs to know before planning, then gets the
// answers from a person, or takes the defaults when the brief says to assume.
// The questions are kept in _state/clarify.json so a resume after the file
// gate stopped the run never asks the model again.
func (p *Planner) clarify(ctx context.Context, r *run) error {
	var qs []clarifyQuestion
	found, err := readJSON(r.path(stateClarify), &qs)
	if err != nil {
		return err
	}
	if !found {
		prompt, err := render("clarify", map[string]string{"Brief": string(r.src)})
		if err != nil {
			return err
		}
		cs := callSpec{stage: "clarify", taskType: "clarify", scope: router.ScopeBrief, maxTokens: maxTokensClarify}
		if err := p.call(ctx, r, cs, prompt, func(text string) error {
			parsed, err := parseClarify(StripReply(text))
			if err != nil {
				return err
			}
			qs = parsed
			return nil
		}); err != nil {
			return err
		}
		if err := writeJSON(r.path(stateClarify), qs); err != nil {
			return err
		}
	}

	as := answersFile{Answers: []answer{}}
	switch {
	case len(qs) == 0:
	case r.brief.Front.OnAmbiguity == "assume_and_document":
		for _, q := range qs {
			text := q.Default
			if strings.TrimSpace(text) == "" {
				text = "No answer was given; take the most conservative option."
			}
			as.Answers = append(as.Answers, answer{ID: q.ID, Stage: "clarify", Question: q.Question, Answer: text, Assumed: true})
		}
	default:
		ask := make([]human.Question, len(qs))
		for i, q := range qs {
			text := q.Question
			if q.Why != "" {
				text += " (" + q.Why + ")"
			}
			ask[i] = human.Question{ID: q.ID, Text: text, Default: q.Default}
		}
		got, err := p.ask(ctx, ask)
		if err != nil {
			return err
		}
		for i, q := range qs {
			as.Answers = append(as.Answers, answer{ID: q.ID, Stage: "clarify", Question: q.Question, Answer: got[i].Text, Assumed: got[i].Assumed})
		}
	}
	return writeJSON(r.path(fileAnswers), as)
}
