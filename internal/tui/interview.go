package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file implements the structured one-question-at-a-time interview used by
// the `/project` flow. The model is asked for exactly one JSON object per
// round, so a weak model cannot emit a wall of questions: the shape forbids it.
// See docs/superpowers/specs/2026-07-23-project-autonomy-design.md.

// interviewStep is one round of the interview as returned by the model.
type interviewStep struct {
	Question string `json:"question"`
	Why      string `json:"why"`
	Done     bool   `json:"done"`
}

// interviewQA is a single asked-and-answered pair.
type interviewQA struct {
	Q string
	A string
}

// interviewTranscript accumulates the interview so far. It is held in model
// state and replayed into every prompt, so answers survive context trimming and
// the generation step sees the whole conversation.
type interviewTranscript struct {
	pairs []interviewQA
}

func (t *interviewTranscript) add(q, a string) {
	t.pairs = append(t.pairs, interviewQA{Q: q, A: a})
}

func (t interviewTranscript) count() int { return len(t.pairs) }

// String renders the transcript in the order asked.
func (t interviewTranscript) String() string {
	var b strings.Builder
	for i, qa := range t.pairs {
		fmt.Fprintf(&b, "Q%d: %s\nA%d: %s\n\n", i+1, qa.Q, i+1, qa.A)
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseInterviewStep extracts the step object from a model reply. Models
// routinely wrap JSON in prose or a fenced block, so the first balanced object
// in the reply is used rather than requiring a bare object.
func parseInterviewStep(reply string) (interviewStep, error) {
	raw, ok := firstJSONObject(reply)
	if !ok {
		return interviewStep{}, fmt.Errorf("no JSON object in reply")
	}
	var step interviewStep
	if err := json.Unmarshal([]byte(raw), &step); err != nil {
		return interviewStep{}, fmt.Errorf("parse interview step: %w", err)
	}
	if !step.Done && strings.TrimSpace(step.Question) == "" {
		return interviewStep{}, fmt.Errorf("interview step has neither a question nor done=true")
	}
	step.Question = strings.TrimSpace(step.Question)
	return step, nil
}

// firstJSONObject returns the first brace-balanced JSON object in s. Braces
// inside string literals are ignored, so a question containing "{a, b}" does
// not truncate the object.
func firstJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	var depth int
	var inString, escaped bool
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// brace inside a string literal: not structural
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

// interviewStepPrompt asks for the next single question, replaying everything
// answered so far so the model does not repeat itself.
func interviewStepPrompt(name string, tr interviewTranscript) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are scoping a new software project called %q for a spec-driven workflow.\n\n", name)

	if tr.count() == 0 {
		b.WriteString("No questions have been asked yet.\n\n")
	} else {
		b.WriteString("The interview so far:\n\n")
		b.WriteString(tr.String())
		b.WriteString("\n\n")
	}

	b.WriteString("Ask EXACTLY ONE next question — the single most useful thing you still do not know. ")
	b.WriteString("Cover, over the course of the interview: goals, target users, core features, scope, ")
	b.WriteString("explicit non-goals, constraints, measurable success criteria, and the exact test command ")
	b.WriteString("for the project (e.g. \"go test ./...\"), which is required before you may finish.\n\n")

	b.WriteString("Reply with ONE JSON object and nothing else:\n")
	b.WriteString(`{"question":"<your single question>","why":"<why you need it>","done":false}` + "\n\n")
	b.WriteString("When you have everything needed to write a thorough spec, reply instead with:\n")
	b.WriteString(`{"done":true}` + "\n\n")
	b.WriteString("Do not ask multiple questions. Do not write any files. Do not call any tools.")
	return b.String()
}
