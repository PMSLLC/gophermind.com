package planner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/runfs"
)

// takesRecommendations is true when the brief says no human is available:
// every question takes its recommended answer, marked assumed.
func (r *run) takesRecommendations() bool { return r.brief.Front.OnAmbiguity == "assume_and_document" }

func clarifyDone(r *run) bool {
	s, err := loadQStore(r)
	return err == nil && s.Complete
}

// clarify is the Clarify stage: the questions whose answers would change the
// code, asked as a dependency tree in rounds. The harness probes the facts
// first, asks the model for the questions, puts only the frontier (the
// questions whose prerequisites are settled) to the owner each round, and asks
// the model again what the settled answers unblocked, until nothing is left or
// a cap is reached. Everything is kept in _state/clarify/ so a resume after the
// file gate stopped the run never asks the model again for what it has.
func (p *Planner) clarify(ctx context.Context, r *run) error {
	s, err := loadQStore(r)
	if err != nil {
		return err
	}
	facts, err := p.ensureFacts(r)
	if err != nil {
		return err
	}
	cfg := p.d.Settings.Defaults
	unattended := r.takesRecommendations()
	for !s.Complete {
		if len(s.unsettled()) == 0 {
			if s.Calls >= cfg.ClarifyMaxCalls || len(s.Questions) >= cfg.ClarifyMaxQuestions {
				s.Complete = true
				break
			}
			added, err := p.clarifyCall(ctx, r, &s, facts, unattended)
			if err != nil {
				return err
			}
			if err := s.save(r); err != nil {
				return err
			}
			if added == 0 {
				s.Complete = true
			}
			continue
		}
		if err := p.clarifyRound(ctx, r, &s, facts, unattended); err != nil {
			return err
		}
	}
	if err := s.save(r); err != nil {
		return err
	}
	return writeAnswersView(r, s)
}

// clarifyCall asks the model for questions: the first call from the brief and
// the facts, later calls from what the settled answers unblocked. It returns
// how many questions it added.
func (p *Planner) clarifyCall(ctx context.Context, r *run, s *qstore, facts factsFile, unattended bool) (int, error) {
	cfg := p.d.Settings.Defaults
	stage, tmpl := "clarify", "clarify"
	data := map[string]string{
		"Brief": string(r.src), "Facts": factsPrompt(facts), "FactKeys": strings.Join(factKeys, ", "),
		"MaxQuestions": fmt.Sprint(cfg.ClarifyMaxQuestions - len(s.Questions)),
	}
	if s.Calls > 0 {
		stage, tmpl = "clarify:more", "clarify_more"
		data["Settled"] = settledText(*s)
		data["Open"] = openText(*s)
		data["NextID"] = fmt.Sprintf("q%d", s.nextNumber())
	}
	prompt, err := render(tmpl, data)
	if err != nil {
		return 0, err
	}
	var got []qrec
	cs := callSpec{stage: stage, taskType: "clarify", scope: router.ScopeBrief, maxTokens: maxTokensClarify}
	if err := p.call(ctx, r, cs, prompt, func(text string) error {
		qs, perr := parseQuestionList(StripReply(text), *s, qparseOpts{
			MaxTotal: cfg.ClarifyMaxQuestions, RequireRecommended: unattended, RaisedBy: "clarify"})
		if perr != nil {
			return perr
		}
		got = qs
		return nil
	}); err != nil {
		return 0, err
	}
	s.Calls++
	s.Questions = append(s.Questions, got...)
	return len(got), nil
}

// clarifyRound settles one round: facts the probe can answer, then the
// frontier, put to the owner or answered by recommendation.
func (p *Planner) clarifyRound(ctx context.Context, r *run, s *qstore, facts factsFile, unattended bool) error {
	cfg := p.d.Settings.Defaults
	now := p.d.Now().UTC()
	if done := s.answerFacts(facts, now); len(done) > 0 {
		if err := s.save(r); err != nil {
			return err
		}
		for _, id := range done {
			if err := writeDecision(r, *s.get(id), nil); err != nil {
				return err
			}
		}
		if len(s.unsettled()) == 0 {
			return nil
		}
	}
	capped := s.Calls >= cfg.ClarifyMaxCalls || len(s.Questions) >= cfg.ClarifyMaxQuestions
	qs := s.frontier()
	if capped {
		// At a cap every open question is the last round, whatever it waits for.
		qs = s.unsettled()
	}
	if len(qs) == 0 {
		if open := s.unsettled(); len(open) > 0 {
			// Parsing forbids an unknown or cyclic depends_on, so only a hand-edited
			// store gets here. Finishing would drop the question unasked.
			return fmt.Errorf("question %s is open but can never be asked: it waits on %s, which is unknown or cyclic; fix its depends_on in %s",
				open[0].ID, oneLine(strings.Join(open[0].DependsOn, ", "), 200), fileQuestions)
		}
		return nil
	}
	round := s.Round + 1
	for _, q := range qs {
		s.get(q.ID).Round = round
	}
	if err := s.save(r); err != nil {
		return err
	}
	type result struct{ answer, by string }
	results := make(map[string]result, len(qs))
	if unattended {
		for _, q := range qs {
			if strings.TrimSpace(q.Recommended) == "" {
				return fmt.Errorf("question %s has no recommended answer and this run takes recommendations", q.ID)
			}
			results[q.ID] = result{q.Recommended, byUnattended}
		}
	} else {
		ask := make([]human.Question, len(qs))
		for i, q := range qs {
			ask[i] = human.Question{ID: q.ID, Text: q.Text, Why: q.Why, Kind: q.Kind, DependsOn: q.DependsOn, Options: q.Options,
				Recommended: q.Recommended, RecommendedWhy: q.RecommendedWhy, Round: round}
		}
		got, err := p.ask(ctx, ask)
		if err != nil {
			return err
		}
		if len(got) != len(qs) {
			return fmt.Errorf("the human gate returned %d answers for %d questions", len(got), len(qs))
		}
		for i, q := range qs {
			by := byHuman
			switch {
			case got[i].Accepted:
				by = byAccepted
			case got[i].Assumed:
				by = byUnattended
			}
			results[q.ID] = result{got[i].Text, by}
		}
	}
	for _, q := range qs {
		s.settle(q.ID, results[q.ID].answer, results[q.ID].by, now)
	}
	s.Round = round
	if err := s.save(r); err != nil {
		return err
	}
	ids := make([]string, len(qs))
	for i, q := range qs {
		ids[i] = q.ID
		if err := writeDecision(r, *s.get(q.ID), nil); err != nil {
			return err
		}
	}
	line := fmt.Sprintf(`{"round":%d,"questions":%q,"at":%q}`, round, strings.Join(ids, ","), stamp(now))
	if err := runfs.AppendLine(r.path(fileRounds), []byte(line)); err != nil {
		return err
	}
	return writeAnswersView(r, *s)
}

// settledText shows the settled decisions to the model, bounded.
func settledText(s qstore) string {
	var b strings.Builder
	for _, q := range s.Questions {
		if q.Status == qSettled && b.Len() < 6000 {
			fmt.Fprintf(&b, "%s: %s\nAnswer: %s\n", q.ID, oneLine(q.Text, 300), oneLine(q.Answer, 300))
		}
	}
	if b.Len() == 0 {
		return "(none)"
	}
	return strings.TrimRight(b.String(), "\n")
}

// openText lists the questions still open, bounded.
func openText(s qstore) string {
	var b strings.Builder
	for _, q := range s.unsettled() {
		if b.Len() < 3000 {
			fmt.Fprintf(&b, "%s: %s\n", q.ID, oneLine(q.Text, 200))
		}
	}
	if b.Len() == 0 {
		return "(none)"
	}
	return strings.TrimRight(b.String(), "\n")
}

var errNotConfirmed = errors.New("the understanding was not confirmed")
