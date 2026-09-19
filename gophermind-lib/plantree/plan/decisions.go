package plan

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	maxDecisions      = 6
	decisionLineBytes = 300
	decisionQuestion  = 200
	decisionNoteBytes = 150
)

// decisionsFor renders the answered questions that affect any of ids as short
// lines for a prompt: what was asked and what the owner chose. ids are the
// nodes the prompt is about (its phase, task and steps). At most maxDecisions
// lines are shown, each cut to decisionLineBytes, in the order the questions
// were asked. Each question is rendered as a quoted string, so text a model
// wrote reads as data, not as an instruction. It returns "" when there is
// nothing to show.
func decisionsFor(qs []Question, ids []string) string {
	want := setOf(ids)
	var lines []string
	omitted := 0
	for _, q := range qs {
		if q.Status != QuestionAnswered || q.Answer == nil || !affectsAny(q, want) {
			continue
		}
		if len(lines) == maxDecisions {
			omitted++
			continue
		}
		lines = append(lines, cutBytes(decisionLine(q), decisionLineBytes))
	}
	if len(lines) == 0 {
		return ""
	}
	out := "- " + strings.Join(lines, "\n- ")
	if omitted > 0 {
		out += fmt.Sprintf("\n  (%d more decisions not shown)", omitted)
	}
	return out
}

func affectsAny(q Question, want map[string]bool) bool {
	for _, id := range q.Affects {
		if want[id] {
			return true
		}
	}
	return false
}

func decisionLine(q Question) string {
	labels := map[string]string{}
	for _, o := range q.Options {
		labels[o.ID] = o.Label
	}
	var chosen []string
	for _, id := range q.Answer.OptionIDs {
		if l, ok := labels[id]; ok {
			chosen = append(chosen, oneLine(l))
		}
	}
	line := strconv.Quote(cutBytes(oneLine(q.Question), decisionQuestion)) + " -> "
	if len(chosen) > 0 {
		line += strings.Join(chosen, "; ")
	}
	if note := oneLine(q.Answer.Text); note != "" {
		if len(chosen) > 0 {
			line += " (note: " + cutBytes(note, decisionNoteBytes) + ")"
		} else {
			line += cutBytes(note, decisionNoteBytes)
		}
	}
	return line
}
