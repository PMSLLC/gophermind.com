package planner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/human"
)

// understandingRec is _state/understanding.json: what was confirmed, by whom,
// and the hash of the text that was confirmed.
type understandingRec struct {
	ConfirmedAt string `json:"confirmed_at"`
	ConfirmedBy string `json:"confirmed_by"`
	Hash        string `json:"understanding_hash"`
}

func readUnderstanding(r *run) (understandingRec, bool) {
	var u understandingRec
	found, err := readJSON(r.path(stateUnderstanding), &u)
	return u, err == nil && found
}

// understandingMarkdown renders the understanding from the store and the
// facts. It is deterministic: the same store gives the same text and hash.
// Only Clarify-phase decisions are listed: a question raised later by a stage
// is bound by the plan hash through answers.json.
func understandingMarkdown(s qstore, f factsFile) string {
	var b strings.Builder
	b.WriteString("# Understanding\n\nThis is what the plan will be built on. Confirm it, or reject it with a reason.\n\n")
	b.WriteString("## Facts established by the harness\n\n" + factsPrompt(f) + "\n\n")
	b.WriteString("## Decisions\n\n")
	n := 0
	var assumed []string
	open := 0
	for _, q := range s.Questions {
		if q.RaisedBy != "clarify" {
			continue
		}
		if q.Status != qSettled {
			open++
			continue
		}
		n++
		fmt.Fprintf(&b, "%d. %s (%s)\n   Answer: %s\n   Decided by: %s\n", n, oneLine(q.Text, 400), q.ID, oneLine(q.Answer, 400), q.AnsweredBy)
		switch q.AnsweredBy {
		case byUnattended:
			assumed = append(assumed, fmt.Sprintf("%s: %s [assumed, nobody was asked]", q.ID, oneLine(q.Answer, 300)))
		case byAccepted:
			assumed = append(assumed, fmt.Sprintf("%s: %s [recommendation accepted]", q.ID, oneLine(q.Answer, 300)))
		}
	}
	if n == 0 {
		b.WriteString("None. No question needed asking.\n")
	}
	b.WriteString("\n## Assumptions taken\n\n")
	if len(assumed) == 0 {
		b.WriteString("None.\n")
	}
	for _, a := range assumed {
		b.WriteString("- " + a + "\n")
	}
	b.WriteString("\n## Open questions\n\n")
	if open == 0 {
		b.WriteString("None.\n\nFrontier empty: every branch visited.\n")
	} else {
		fmt.Fprintf(&b, "%d question(s) are still open.\n", open)
	}
	return neutralize(b.String())
}

// neutralize makes the text safe to embed in the file gate's UNDERSTANDING.md:
// no code fence (which could open or close a decision block) and no line that
// starts "Understanding hash:" (which the gate reads as the hash). Words come
// from the model and the owner, so both are rewritten rather than trusted.
func neutralize(md string) string {
	md = strings.ReplaceAll(md, "```", "'''")
	md = strings.ReplaceAll(md, "~~~", "---")
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "Understanding hash:") {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n")
}

// currentUnderstanding renders the understanding as it stands and its hash.
func currentUnderstanding(r *run) (md, hash string, err error) {
	s, err := loadQStore(r)
	if err != nil {
		return "", "", err
	}
	f, err := loadFacts(r)
	if err != nil {
		return "", "", err
	}
	md = understandingMarkdown(s, f)
	return md, hashHex([]byte(md)), nil
}

// confirmDone is true when the understanding was confirmed and nothing it
// shows has changed since: the text, and answers.json against the store (an
// edit to either puts the understanding to the owner again).
func confirmDone(r *run) bool {
	u, ok := readUnderstanding(r)
	if !ok {
		return false
	}
	_, hash, err := currentUnderstanding(r)
	if err != nil || hash != u.Hash {
		return false
	}
	s, err := loadQStore(r)
	if err != nil {
		return false
	}
	as, err := loadAnswers(r)
	return err == nil && sameJSON(as, answersView(s))
}

// confirm is the confirm stage: the owner confirms the shared understanding
// before Contract starts. An unattended run confirms by its stated rule; --yes
// records flag.
func (p *Planner) confirm(ctx context.Context, r *run) error {
	s, err := loadQStore(r)
	if err != nil {
		return err
	}
	md, hash, err := currentUnderstanding(r)
	if err != nil {
		return err
	}
	by := "flag"
	switch {
	case r.opts.Yes:
	case r.takesRecommendations():
		if len(s.unsettled()) > 0 || !s.Complete {
			return errors.New("the understanding still has open questions")
		}
		by = "unattended"
	default:
		if p.d.Gate == nil {
			return errors.New("the understanding needs confirmation and no human gate is configured; pass --yes to confirm it unseen")
		}
		d, err := p.d.Gate.Confirm(ctx, human.Understanding{Markdown: md, Hash: hash})
		if err != nil {
			return err
		}
		if !d.Approved {
			if d.Note != "" {
				return fmt.Errorf("%w: %s", errNotConfirmed, d.Note)
			}
			return errNotConfirmed
		}
		if by = d.By; by == "" {
			by = "gate"
		}
	}
	// answers.json is the store's view: bring it in line so the confirmation holds.
	if err := writeAnswersView(r, s); err != nil {
		return err
	}
	// The browsable copy is written only now. The file gate keeps its own
	// UNDERSTANDING.md (the text, the hash and the decision block) in the same
	// place while it waits, and a resume must find that file as the owner left
	// it, so the planner overwrites it only once the answer has been consumed.
	if err := writeFileAtomic(r.path(fileUnderstanding), []byte(md), 0o600); err != nil {
		return err
	}
	return writeJSON(r.path(stateUnderstanding), understandingRec{ConfirmedAt: stamp(p.d.Now()), ConfirmedBy: by, Hash: hash})
}
