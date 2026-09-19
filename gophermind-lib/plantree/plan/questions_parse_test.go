package plan

import (
	"errors"
	"strings"
	"testing"
)

func goodQ() QuestionOut {
	return QuestionOut{
		Question:    "Which database?",
		Why:         "it shapes the schema",
		Options:     []OptionOut{{Label: "SQLite", Description: "embedded"}, {Label: "Postgres"}},
		Recommended: []string{"sqlite"},
		Rationale:   "simplest",
		Affects:     []string{"Repo layout"},
	}
}

func TestValidateQuestionOutsAccepts(t *testing.T) {
	free := QuestionOut{Question: "What is the deadline?"}
	multi := goodQ()
	multi.Question = "Which platforms?"
	multi.MultiSelect = true
	multi.Recommended = []string{"SQLite", "Postgres"}
	if err := validateQuestionOuts([]QuestionOut{goodQ(), free, multi}, nil); err != nil {
		t.Errorf("valid questions rejected: %v", err)
	}
	if err := validateQuestionOuts(nil, nil); err != nil {
		t.Errorf("no questions is valid: %v", err)
	}
}

func TestValidateQuestionOutsRejects(t *testing.T) {
	with := func(f func(*QuestionOut)) []QuestionOut {
		q := goodQ()
		f(&q)
		return []QuestionOut{q}
	}
	many := make([]QuestionOut, maxQuestionsPerPass+1)
	for i := range many {
		many[i] = QuestionOut{Question: strings.Repeat("q", i+1)}
	}
	cases := map[string]struct {
		qs   []QuestionOut
		want string
	}{
		"too many questions": {many, "too many questions"},
		"empty question":     {with(func(q *QuestionOut) { q.Question = " " }), "a question is empty"},
		"long question":      {with(func(q *QuestionOut) { q.Question = strings.Repeat("q", maxQuestionRunes+1) }), "longer than"},
		"long why":           {with(func(q *QuestionOut) { q.Why = strings.Repeat("w", maxWhyRunes+1) }), "why is longer"},
		"duplicate question": {[]QuestionOut{goodQ(), goodQ()}, "asked twice"},
		"one option":         {with(func(q *QuestionOut) { q.Options = q.Options[:1]; q.Recommended = nil }), "at least two options"},
		"too many options": {with(func(q *QuestionOut) {
			q.Options = nil
			for i := 0; i <= maxOptions; i++ {
				q.Options = append(q.Options, OptionOut{Label: strings.Repeat("o", i+1)})
			}
			q.Recommended = nil
		}), "too many options"},
		"empty label":         {with(func(q *QuestionOut) { q.Options[1].Label = "" }), "label is empty"},
		"long label":          {with(func(q *QuestionOut) { q.Options[1].Label = strings.Repeat("l", maxLabelRunes+1) }), "label is longer"},
		"duplicate label":     {with(func(q *QuestionOut) { q.Options[1].Label = " sqlite " }), "appears twice"},
		"long description":    {with(func(q *QuestionOut) { q.Options[0].Description = strings.Repeat("d", maxOptionDescRunes+1) }), "description is longer"},
		"unknown recommend":   {with(func(q *QuestionOut) { q.Recommended = []string{"Oracle"} }), "not one of its options"},
		"two on single":       {with(func(q *QuestionOut) { q.Recommended = []string{"SQLite", "Postgres"} }), "not multi_select"},
		"duplicate recommend": {with(func(q *QuestionOut) { q.MultiSelect = true; q.Recommended = []string{"SQLite", "sqlite"} }), "twice"},
		"long rationale":      {with(func(q *QuestionOut) { q.Rationale = strings.Repeat("r", maxRationaleRunes+1) }), "rationale is longer"},
		"empty affects":       {with(func(q *QuestionOut) { q.Affects = []string{""} }), "affects entry is empty"},
		"long affects":        {with(func(q *QuestionOut) { q.Affects = []string{strings.Repeat("a", maxAffectRunes+1)} }), "affects entry is longer"},
		"too many affects": {with(func(q *QuestionOut) {
			q.Affects = make([]string, maxAffects+1)
			for i := range q.Affects {
				q.Affects[i] = "x"
			}
		}), "too many affects"},
	}
	for name, c := range cases {
		err := validateQuestionOuts(c.qs, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, c.want)
		}
	}
}

func TestValidateQuestionOutsCallsTheAffectsCheckAndKeepsItsError(t *testing.T) {
	boom := errors.New("not a step of this batch")
	var seen []string
	err := validateQuestionOuts([]QuestionOut{goodQ()}, func(where, affect string) error {
		seen = append(seen, affect)
		return boom
	})
	if !errors.Is(err, boom) || len(seen) != 1 || seen[0] != "Repo layout" {
		t.Errorf("err=%v seen=%v", err, seen)
	}
}

func TestValidateQuestionOutsErrorsStayBounded(t *testing.T) {
	huge := strings.Repeat("x", 3_000_000)
	q := goodQ()
	q.Question = huge
	err := validateQuestionOuts([]QuestionOut{q}, nil)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("error length = %d", len(err.Error()))
	}
	q = goodQ()
	q.Recommended = []string{huge}
	err = validateQuestionOuts([]QuestionOut{q}, nil)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("recommended: error length = %d", len(err.Error()))
	}
}

func TestNewQuestionMapsOptionsAndKeepsResolvedAffects(t *testing.T) {
	nq := newQuestion(goodQ(), []string{"phase-001.task-001"})
	if nq.Question != "Which database?" || len(nq.Options) != 2 || nq.Options[0].Label != "SQLite" ||
		nq.RecommendedLabels[0] != "sqlite" || nq.Affects[0] != "phase-001.task-001" {
		t.Errorf("NewQuestion = %+v", nq)
	}
}
