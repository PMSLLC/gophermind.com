package plan

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

// answeredRepo builds a tree with two steps under the task a question affects
// and one step under a task it does not, answers the question, and drafts
// every step, which is the state an owner is in when they change their mind.
func answeredRepo(t *testing.T) (*plantree.Repo, Question) {
	t.Helper()
	r := newRepo(t)
	for _, id := range []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001",
		"phase-001.task-001.step-002", "phase-001.task-002", "phase-001.task-002.step-001"} {
		n, err := newSkeleton(id, "node "+id, "why "+id, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	nq := twoOptions()
	nq.Affects = []string{"phase-001.task-001"}
	qs, err := AddQuestions(r, []NewQuestion{nq})
	if err != nil {
		t.Fatal(err)
	}
	q, err := AnswerQuestion(r, qs[0].ID, Answer{OptionIDs: []string{"opt-1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002", "phase-001.task-002.step-001"} {
		draft(t, r, id)
	}
	return r, q
}

// draft gives a step a complete specification and the drafted stage.
func draft(t *testing.T, r *plantree.Repo, id string) {
	t.Helper()
	cur, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(id, cur.NodeRevision, func(n *plantree.Node) error {
		n.Work = &plantree.Work{Description: "build " + id, AcceptanceCriteria: []string{"it works"}}
		n.Planning.Stage = plantree.StageDrafted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestChangeAnswerFlagsTheDraftedStepsItAffects(t *testing.T) {
	r, q := answeredRepo(t)
	got, rec, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}, Text: "on reflection"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer == nil || len(got.Answer.OptionIDs) != 1 || got.Answer.OptionIDs[0] != "opt-2" || got.Answer.Text != "on reflection" {
		t.Errorf("answer = %+v", got.Answer)
	}
	if len(got.PriorAnswers) != 1 || len(got.PriorAnswers[0].OptionIDs) != 1 || got.PriorAnswers[0].OptionIDs[0] != "opt-1" {
		t.Errorf("prior answers = %+v", got.PriorAnswers)
	}
	if got.Status != QuestionAnswered {
		t.Errorf("status = %q, want answered", got.Status)
	}
	want := []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"}
	if strings.Join(rec.Flagged, ",") != strings.Join(want, ",") {
		t.Errorf("Flagged = %v, want %v", rec.Flagged, want)
	}
	if len(rec.Executed) != 0 {
		t.Errorf("Executed = %v, want none", rec.Executed)
	}
	for _, id := range want {
		n, err := r.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if n.Planning.Stage != plantree.StageNeedsReconciliation {
			t.Errorf("%s is %s, want needs_reconciliation", id, n.Planning.Stage)
		}
		if !strings.Contains(n.ResumeNote, q.ID) || !strings.Contains(n.ResumeNote, "Postgres") {
			t.Errorf("%s resume note = %q, want the question and the new choice", id, n.ResumeNote)
		}
		if n.Work == nil {
			t.Errorf("%s lost its specification", id)
		}
	}
	// A step the question does not affect is untouched, and so is everything else.
	if n, _ := r.Get("phase-001.task-002.step-001"); n.Planning.Stage != plantree.StageDrafted || n.ResumeNote != "" {
		t.Errorf("an unaffected step changed: %s %q", n.Planning.Stage, n.ResumeNote)
	}
}

func TestChangeAnswerIsIdempotentForTheSameAnswer(t *testing.T) {
	r, q := answeredRepo(t)
	if _, _, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	// Put the flagged steps back so a second identical change would show up.
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		draft(t, r, id)
	}
	got, rec, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Flagged) != 0 || len(rec.Executed) != 0 {
		t.Errorf("an unchanged answer flagged %+v", rec)
	}
	if len(got.PriorAnswers) != 1 {
		t.Errorf("an unchanged answer grew the history to %d entries", len(got.PriorAnswers))
	}
	if n, _ := r.Get("phase-001.task-001.step-001"); n.Planning.Stage != plantree.StageDrafted {
		t.Errorf("an unchanged answer moved %s to %s", n.ID, n.Planning.Stage)
	}
}

func TestChangeAnswerLeavesExecutedStepsAlone(t *testing.T) {
	r, q := answeredRepo(t)
	cur, _ := r.Get("phase-001.task-001.step-002")
	if _, err := r.Update(cur.ID, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusCompleted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, rec, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rec.Flagged, ",") != "phase-001.task-001.step-001" {
		t.Errorf("Flagged = %v", rec.Flagged)
	}
	if strings.Join(rec.Executed, ",") != "phase-001.task-001.step-002" {
		t.Errorf("Executed = %v, want the completed step reported, not re-planned", rec.Executed)
	}
	if n, _ := r.Get("phase-001.task-001.step-002"); n.Planning.Stage != plantree.StageDrafted {
		t.Errorf("a completed step was re-planned: %s", n.Planning.Stage)
	}
}

func TestChangeAnswerValidatesLikeAnswerQuestion(t *testing.T) {
	r, q := answeredRepo(t)
	for _, bad := range []Answer{{}, {OptionIDs: []string{"opt-9"}}, {OptionIDs: []string{"opt-1", "opt-2"}}} {
		if _, _, err := ChangeAnswer(r, q.ID, bad); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("ChangeAnswer(%+v) = %v, want ErrInvalidAnswer", bad, err)
		}
	}
	if _, _, err := ChangeAnswer(r, "q-404", Answer{OptionIDs: []string{"opt-1"}}); !errors.Is(err, ErrNoSuchQuestion) {
		t.Errorf("unknown id: %v", err)
	}
	open, err := AddQuestions(r, []NewQuestion{{Question: "Still open?", Why: "w", Affects: []string{"phase-001"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ChangeAnswer(r, open[0].ID, Answer{Text: "yes"}); !errors.Is(err, ErrNotAnswered) {
		t.Errorf("open question: %v, want ErrNotAnswered", err)
	}
}

func TestChangeAnswerBoundsTheHistory(t *testing.T) {
	r, q := answeredRepo(t)
	long := strings.Repeat("y", 1500)
	for i := 0; i < maxAnswerHistory+3; i++ {
		if _, _, err := ChangeAnswer(r, q.ID, Answer{Text: long + strings.Repeat("z", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	qs, err := LoadQuestions(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs[0].PriorAnswers) != maxAnswerHistory {
		t.Fatalf("history = %d entries, want %d", len(qs[0].PriorAnswers), maxAnswerHistory)
	}
	for _, p := range qs[0].PriorAnswers {
		if len(p.Text) > historyTextBytes+3 {
			t.Errorf("a stored previous answer is %d bytes, want at most %d", len(p.Text), historyTextBytes)
		}
	}
	// The oldest entries are the ones dropped: the first kept entry is not the
	// original answer any more.
	if len(qs[0].PriorAnswers[0].OptionIDs) != 0 {
		t.Errorf("the oldest entry survived: %+v", qs[0].PriorAnswers[0])
	}
}

func TestQuestionsFileFromSchema1StillLoads(t *testing.T) {
	r := newRepo(t)
	const v1 = `{
  "schema_version": 1,
  "revision": 3,
  "questions": [
    {"id":"q-001","question":"Which database?","why":"the schema depends on it",
     "options":[{"id":"opt-1","label":"SQLite","description":"embedded"}],
     "multi_select":false,"allow_free_text":true,"recommended":null,
     "affects":["phase-001"],"source":"chunk 1 of the brief",
     "status":"answered","answer":{"option_ids":["opt-1"],"text":""},"answered_at":"2026-09-19T00:00:00Z"}
  ]
}`
	if err := os.WriteFile(questionsPath(r), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	qs, err := LoadQuestions(r)
	if err != nil || len(qs) != 1 || qs[0].Answer == nil || len(qs[0].PriorAnswers) != 0 {
		t.Fatalf("LoadQuestions = %+v, %v", qs, err)
	}
	if _, _, err := ChangeAnswer(r, "q-001", Answer{Text: "actually Postgres"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(questionsPath(r))
	if err != nil {
		t.Fatal(err)
	}
	var f questionFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.SchemaVersion != questionsSchema || f.Revision != 4 || len(f.Questions[0].PriorAnswers) != 1 {
		t.Errorf("after a change the file is schema %d revision %d with %d prior answers",
			f.SchemaVersion, f.Revision, len(f.Questions[0].PriorAnswers))
	}
}

func TestQuestionsFileFromAFutureSchemaIsRefused(t *testing.T) {
	r := newRepo(t)
	if err := os.WriteFile(questionsPath(r), []byte(`{"schema_version":99,"revision":1,"questions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuestions(r); err == nil || !strings.Contains(err.Error(), "schema_version 99") {
		t.Errorf("err = %v, want a refusal naming the version", err)
	}
}
