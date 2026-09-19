package plan

import (
	"strings"
	"testing"
)

func answered(question string, affects []string, ids []string, text string) Question {
	return Question{
		ID: "q-001", Question: question, Status: QuestionAnswered, Affects: affects,
		Options: []Option{{ID: "opt-1", Label: "SQLite"}, {ID: "opt-2", Label: "Postgres"}},
		Answer:  &Answer{OptionIDs: ids, Text: text},
	}
}

func TestDecisionsForShowsOnlyAnsweredQuestionsThatAffectTheNodes(t *testing.T) {
	open := answered("Open one?", []string{"phase-001.task-001"}, nil, "")
	open.Status, open.Answer = QuestionOpen, nil
	qs := []Question{
		answered("Which database?", []string{"phase-001.task-001"}, []string{"opt-2"}, ""),
		answered("Elsewhere?", []string{"phase-009"}, []string{"opt-1"}, ""),
		open,
		answered("Deadline?", []string{"phase-001"}, nil, "end of the quarter"),
		answered("Both?", []string{"phase-001.task-001.step-001"}, []string{"opt-1", "opt-2"}, "keep both in sync"),
	}
	got := decisionsFor(qs, []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001"})
	want := "- Which database? -> Postgres\n- Deadline? -> end of the quarter\n- Both? -> SQLite; Postgres (note: keep both in sync)"
	if got != want {
		t.Errorf("decisionsFor =\n%q\nwant\n%q", got, want)
	}
	if got := decisionsFor(qs, []string{"phase-777"}); got != "" {
		t.Errorf("nothing affects phase-777, got %q", got)
	}
	if got := decisionsFor(nil, []string{"phase-001"}); got != "" {
		t.Errorf("no questions: %q", got)
	}
}

func TestDecisionsForIsBounded(t *testing.T) {
	var qs []Question
	for i := 0; i < maxDecisions+3; i++ {
		qs = append(qs, answered(strings.Repeat("q", 900), []string{"phase-001"}, []string{"opt-1"}, strings.Repeat("n", 900)))
	}
	got := decisionsFor(qs, []string{"phase-001"})
	lines := strings.Split(got, "\n")
	if len(lines) != maxDecisions+1 || !strings.Contains(lines[len(lines)-1], "3 more decisions not shown") {
		t.Fatalf("%d lines, last %q", len(lines), lines[len(lines)-1])
	}
	for _, l := range lines[:maxDecisions] {
		if len(l) > decisionLineBytes+3+3 { // "- " prefix and the cut marker
			t.Errorf("a decision line is %d bytes, cap %d", len(l), decisionLineBytes)
		}
	}
}
