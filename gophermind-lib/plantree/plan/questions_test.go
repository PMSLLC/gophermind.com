package plan

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func twoOptions() NewQuestion {
	return NewQuestion{
		Question: "Which database?",
		Why:      "the schema depends on it",
		Options: []NewOption{
			{Label: "SQLite", Description: "embedded"},
			{Label: "Postgres", Description: "server"},
		},
		RecommendedLabels: []string{"sqlite"},
		Rationale:         "simplest to run",
		Affects:           []string{"phase-001.task-001"},
		Source:            "chunk 2 of the brief",
	}
}

func TestAddQuestionsAssignsIdsAndKeepsRecommendationsApart(t *testing.T) {
	r := newRepo(t)
	got, err := AddQuestions(r, []NewQuestion{twoOptions()})
	if err != nil || len(got) != 1 {
		t.Fatalf("AddQuestions = %+v, %v", got, err)
	}
	q := got[0]
	if q.ID != "q-001" || q.Status != QuestionOpen || !q.AllowFreeText || q.Answer != nil ||
		len(q.Options) != 2 || q.Options[0].ID != "opt-1" || q.Options[1].ID != "opt-2" {
		t.Errorf("question = %+v", q)
	}
	if q.Recommended == nil || len(q.Recommended.OptionIDs) != 1 || q.Recommended.OptionIDs[0] != "opt-1" || q.Recommended.Rationale != "simplest to run" {
		t.Errorf("recommended = %+v", q.Recommended)
	}
	if q.Answer != nil {
		t.Error("a recommendation is never an answer")
	}
	if q.Source != "chunk 2 of the brief" || len(q.Affects) != 1 || q.Affects[0] != "phase-001.task-001" {
		t.Errorf("source=%q affects=%v", q.Source, q.Affects)
	}
	open, _ := OpenQuestions(r)
	if len(open) != 1 {
		t.Errorf("OpenQuestions = %d", len(open))
	}
}

func TestAddQuestionsIsIdempotentByQuestionText(t *testing.T) {
	r := newRepo(t)
	first, _ := AddQuestions(r, []NewQuestion{twoOptions()})
	dup := twoOptions()
	dup.Question = "  which DATABASE?  "
	second, err := AddQuestions(r, []NewQuestion{dup})
	if err != nil || len(second) != 1 || second[0].ID != first[0].ID {
		t.Fatalf("a repeated question must return the existing record: %+v, %v", second, err)
	}
	all, _ := LoadQuestions(r)
	if len(all) != 1 {
		t.Errorf("%d questions stored, want 1", len(all))
	}
	// Even after it is answered, asking it again does not reopen it.
	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-1"}}); err != nil {
		t.Fatal(err)
	}
	again, _ := AddQuestions(r, []NewQuestion{twoOptions()})
	if again[0].Status != QuestionAnswered {
		t.Error("an answered question must stay answered")
	}
}

func TestAddQuestionsRejectsAnUnknownRecommendation(t *testing.T) {
	r := newRepo(t)
	bad := twoOptions()
	bad.RecommendedLabels = []string{"Oracle"}
	if _, err := AddQuestions(r, []NewQuestion{bad}); err == nil || !strings.Contains(err.Error(), "not one of its options") {
		t.Errorf("err = %v", err)
	}
	if all, _ := LoadQuestions(r); len(all) != 0 {
		t.Error("a rejected question must not be stored")
	}
}

func TestAnswerQuestionRules(t *testing.T) {
	r := newRepo(t)
	multi := twoOptions()
	multi.Question = "Which platforms?"
	multi.MultiSelect = true
	multi.RecommendedLabels = nil
	if _, err := AddQuestions(r, []NewQuestion{twoOptions(), multi}); err != nil {
		t.Fatal(err)
	}

	bad := map[string]struct {
		id string
		a  Answer
	}{
		"nothing":              {"q-001", Answer{}},
		"blank text":           {"q-001", Answer{Text: "  "}},
		"unknown option":       {"q-001", Answer{OptionIDs: []string{"opt-9"}}},
		"two on single select": {"q-001", Answer{OptionIDs: []string{"opt-1", "opt-2"}}},
		"duplicate on multi":   {"q-002", Answer{OptionIDs: []string{"opt-1", "opt-1"}}},
		"text too long":        {"q-001", Answer{Text: strings.Repeat("x", maxAnswerTextRunes+1)}},
	}
	for name, c := range bad {
		if _, err := AnswerQuestion(r, c.id, c.a); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("%s: err = %v, want ErrInvalidAnswer", name, err)
		}
	}
	if open, _ := OpenQuestions(r); len(open) != 2 {
		t.Error("a rejected answer must leave the question open")
	}

	q, err := AnswerQuestion(r, "q-002", Answer{OptionIDs: []string{"opt-1", "opt-2"}, Text: " both, plus a note "})
	if err != nil || q.Status != QuestionAnswered || q.Answer.Text != "both, plus a note" || q.AnsweredAt == "" || len(q.Answer.OptionIDs) != 2 {
		t.Errorf("multi-select answer = %+v, %v", q, err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{Text: "whatever fits"}); err != nil {
		t.Errorf("free text alone must be accepted: %v", err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{Text: "again"}); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("second answer: err = %v, want ErrAlreadyAnswered", err)
	}
	if _, err := AnswerQuestion(r, "q-099", Answer{Text: "x"}); !errors.Is(err, ErrNoSuchQuestion) {
		t.Errorf("unknown id: err = %v, want ErrNoSuchQuestion", err)
	}
}

func TestAnswerSurvivesReopeningTheStore(t *testing.T) {
	r := newRepo(t)
	if _, err := AddQuestions(r, []NewQuestion{twoOptions()}); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	all, err := LoadQuestions(r)
	if err != nil || len(all) != 1 || all[0].Answer == nil || all[0].Answer.OptionIDs[0] != "opt-2" {
		t.Errorf("reloaded = %+v, %v", all, err)
	}
	open, _ := OpenQuestions(r)
	if len(open) != 0 {
		t.Error("an answered question is not open")
	}
}

func TestQuestionFileIsStrict(t *testing.T) {
	r := newRepo(t)
	if err := os.WriteFile(questionsPath(r), []byte(`{"schema_version":1,"revision":1,"questions":[],"extra":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuestions(r); err == nil {
		t.Error("an unknown field must be rejected")
	}
	if err := os.WriteFile(questionsPath(r), []byte(`{"schema_version":7,"revision":1,"questions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuestions(r); err == nil || !strings.Contains(err.Error(), "schema_version 7") {
		t.Errorf("an unsupported version must be reported: %v", err)
	}
}

func TestConcurrentAddsDoNotLoseQuestions(t *testing.T) {
	r := newRepo(t)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := twoOptions()
			q.Question = strings.Repeat("q", i+1) + "?"
			if _, err := AddQuestions(r, []NewQuestion{q}); err != nil {
				t.Errorf("add %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	all, _ := LoadQuestions(r)
	ids := map[string]bool{}
	for _, q := range all {
		ids[q.ID] = true
	}
	if len(all) != 6 || len(ids) != 6 {
		t.Errorf("%d questions and %d distinct ids, want 6 and 6", len(all), len(ids))
	}
}
