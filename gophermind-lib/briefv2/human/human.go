// Package human is where the engine stops and asks a person: clarifying
// questions, plan approval, and (for the executor plan) escalated leaves. The
// Gate interface has three adapters so the terminal, a pair of files, and
// later the desktop app can all answer the same calls.
package human

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrWaiting means a question or approval was written out and nobody has
// answered it yet. The command exits with code 3; `resume` asks again.
var ErrWaiting = errors.New("human: waiting for an answer")

// Question is one thing the planner needs to know.
type Question struct {
	ID      string
	Text    string
	Default string   // taken when the person leaves it alone; empty means an answer is required
	Options []string // suggestions; the terminal lets a number pick one
}

// Answer answers the question with the same ID. Assumed is true when the
// default was taken rather than typed.
type Answer struct {
	ID      string
	Text    string
	Assumed bool
}

// PlanSummary is the plan as shown for approval. Hash binds an approval to
// this exact plan.
type PlanSummary struct {
	Markdown string
	Hash     string
}

// Decision is the answer to Approve.
type Decision struct {
	Approved bool
	By       string // terminal, file, programmatic
	Note     string
}

// Escalation is a leaf that every model failed, handed to a person.
type Escalation struct {
	NodeID  string
	Reason  string
	History []string // one line per failed attempt
}

// Action is what to do about an escalated leaf.
type Action string

const (
	ActionRetry Action = "retry" // try again, with the note added to the leaf's context
	ActionSkip  Action = "skip"  // leave the leaf failed and continue
	ActionStop  Action = "stop"  // stop the run
)

// Resolution is the answer to Escalate.
type Resolution struct {
	Action Action
	Note   string
}

// Gate is the whole conversation with a person.
type Gate interface {
	// Ask blocks until every question is answered, in order.
	Ask(ctx context.Context, qs []Question) ([]Answer, error)
	Approve(ctx context.Context, plan PlanSummary) (Decision, error)
	// Escalate is used by the executor plan.
	Escalate(ctx context.Context, e Escalation) (Resolution, error)
}

// validateAnswers checks that as answers qs one for one, in order, with text.
func validateAnswers(qs []Question, as []Answer) error {
	if len(as) != len(qs) {
		return fmt.Errorf("human: %d answers for %d questions", len(as), len(qs))
	}
	for i, q := range qs {
		if as[i].ID != q.ID {
			return fmt.Errorf("human: answer %d is for %q, want %q", i+1, as[i].ID, q.ID)
		}
		if strings.TrimSpace(as[i].Text) == "" {
			return fmt.Errorf("human: question %q has an empty answer", q.ID)
		}
	}
	return nil
}

// parseDecision reads "approve", "yes", "reject: why", "no" and so on. ok is
// false for anything else, including empty text.
func parseDecision(text string) (approved bool, note string, ok bool) {
	word, rest := splitWord(text)
	switch word {
	case "approve", "approved", "yes", "y":
		return true, rest, true
	case "reject", "rejected", "no", "n":
		return false, rest, true
	}
	return false, "", false
}

// parseAction reads "retry check the test", "skip", "stop".
func parseAction(text string) (Action, string, bool) {
	word, rest := splitWord(text)
	switch Action(word) {
	case ActionRetry, ActionSkip, ActionStop:
		return Action(word), rest, true
	}
	return "", "", false
}

// splitWord returns the lower-cased first word (trailing colon removed) and
// the trimmed remainder.
func splitWord(text string) (word, rest string) {
	text = strings.TrimSpace(text)
	i := strings.IndexAny(text, " \t\r\n")
	if i < 0 {
		return strings.ToLower(strings.TrimRight(text, ":")), ""
	}
	return strings.ToLower(strings.TrimRight(text[:i], ":")), strings.TrimSpace(text[i+1:])
}
