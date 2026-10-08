package planner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const canaryQ = "CANARY-q-8841"

func TestParseQuestionListAcceptsAValidTree(t *testing.T) {
	text := `[
 {"id":"q1","question":"Which store?","why_it_matters":"types","options":["memory","postgres"],"recommended":"memory","recommended_why":"none declared"},
 {"id":"q2","question":"Pool size?","depends_on":["q1"],"recommended":"10"}]`
	got, err := parseQuestionList(text, qstore{}, qparseOpts{MaxTotal: 30, RequireRecommended: true, RaisedBy: "clarify"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != "decision" || got[0].Status != qOpen || got[0].RaisedBy != "clarify" ||
		!reflect.DeepEqual(got[1].DependsOn, []string{"q1"}) || got[0].Recommended != "memory" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseQuestionListFillsIdsAliasesAndDowngradesFacts(t *testing.T) {
	text := `[{"question":"A?","default_if_unanswered":"yes"},
 {"id":"q9","kind":"fact","question":"Module?"},
 {"id":"q10","kind":"fact","fact_key":"go_module","question":"Module path?"}]`
	have := qstore{Questions: []qrec{{ID: "q4", Status: qSettled}}}
	got, err := parseQuestionList(text, have, qparseOpts{MaxTotal: 30, RaisedBy: "clarify"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "q5" || got[0].Recommended != "yes" {
		t.Errorf("id/alias: %+v", got[0])
	}
	if got[1].Kind != "decision" || got[1].FactKey != "" {
		t.Errorf("a fact with no fact_key must become a decision: %+v", got[1])
	}
	if got[2].Kind != "fact" || got[2].FactKey != "go_module" {
		t.Errorf("a fact with a key stays a fact: %+v", got[2])
	}
}

func TestParseQuestionListRejects(t *testing.T) {
	have := qstore{Questions: []qrec{{ID: "q1", Status: qSettled}}}
	cases := []struct {
		name string
		text string
		opts qparseOpts
		want string
	}{
		{"not an array", `{"a":1}`, qparseOpts{}, "not a JSON array"},
		{"null", `null`, qparseOpts{}, "null"},
		{"empty text", `[{"id":"q2","question":"  "}]`, qparseOpts{}, "no text"},
		{"bad id", `[{"id":"x2","question":"` + canaryQ + `?"}]`, qparseOpts{}, "id like"},
		{"repeats an existing id", `[{"id":"q1","question":"` + canaryQ + `?"}]`, qparseOpts{}, "repeats an id"},
		{"repeats an id in the reply", `[{"id":"q2","question":"a?"},{"id":"q2","question":"` + canaryQ + `?"}]`, qparseOpts{}, "repeats an id"},
		{"forward reference", `[{"id":"q2","question":"` + canaryQ + `?","depends_on":["q3"]},{"id":"q3","question":"b?"}]`, qparseOpts{}, "not earlier"},
		{"self dependency", `[{"id":"q2","question":"` + canaryQ + `?","depends_on":["q2"]}]`, qparseOpts{}, "itself"},
		{"unknown dependency", `[{"id":"q2","question":"` + canaryQ + `?","depends_on":["q77"]}]`, qparseOpts{}, "not earlier"},
		{"bad kind", `[{"id":"q2","kind":"opinion","question":"` + canaryQ + `?"}]`, qparseOpts{}, "kind"},
		{"one option", `[{"id":"q2","question":"` + canaryQ + `?","options":["only"]}]`, qparseOpts{}, "2 to 8"},
		{"nine options", `[{"id":"q2","question":"a?","options":["1","2","3","4","5","6","7","8","` + canaryQ + `"]}]`, qparseOpts{}, "2 to 8"},
		{"duplicate options", `[{"id":"q2","question":"a?","options":["` + canaryQ + `","` + canaryQ + `"]}]`, qparseOpts{}, "distinct"},
		{"recommended is not an option", `[{"id":"q2","question":"a?","options":["a","b"],"recommended":"` + canaryQ + `"}]`, qparseOpts{}, "recommended"},
		{"decision without a recommendation", `[{"id":"q2","question":"` + canaryQ + `?"}]`, qparseOpts{RequireRecommended: true}, "recommended"},
		{"over the cap", `[{"id":"q2","question":"` + canaryQ + `?"},{"id":"q3","question":"b?"}]`, qparseOpts{MaxTotal: 2}, "most important"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseQuestionList(tc.text, have, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), canaryQ) {
				t.Errorf("the error quotes the reply: %v", err)
			}
		})
	}
}

func chain() qstore {
	return qstore{Questions: []qrec{
		{ID: "q1", Status: qOpen},
		{ID: "q2", Status: qOpen, DependsOn: []string{"q1"}},
		{ID: "q3", Status: qOpen, DependsOn: []string{"q2"}},
		{ID: "q4", Status: qOpen},
	}}
}

func qids(qs []qrec) []string {
	out := []string{}
	for _, q := range qs {
		out = append(out, q.ID)
	}
	return out
}

func TestFrontierFollowsTheTree(t *testing.T) {
	s := chain()
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if got := qids(s.frontier()); !reflect.DeepEqual(got, []string{"q1", "q4"}) {
		t.Fatalf("round 1 frontier = %v", got)
	}
	s.settle("q1", "a", byHuman, at)
	s.settle("q4", "d", byHuman, at)
	if got := qids(s.frontier()); !reflect.DeepEqual(got, []string{"q2"}) {
		t.Fatalf("round 2 frontier = %v", got)
	}
	s.settle("q2", "b", byAccepted, at)
	if got := qids(s.frontier()); !reflect.DeepEqual(got, []string{"q3"}) {
		t.Fatalf("round 3 frontier = %v", got)
	}
	if s.settle("q3", "c", byUnattended, at); len(s.unsettled()) != 0 || len(s.frontier()) != 0 {
		t.Fatalf("everything is settled: %v %v", s.unsettled(), s.frontier())
	}
	if s.settle("q3", "again", byHuman, at) {
		t.Error("a settled question must not settle twice")
	}
	if s.get("q2").Answer != "b" || s.get("q2").AnsweredBy != byAccepted || s.get("q2").SettledAt != "2026-10-08T00:00:00Z" {
		t.Errorf("q2 = %+v", s.get("q2"))
	}
}

func TestNextNumberSkipsStageIds(t *testing.T) {
	s := qstore{Questions: []qrec{{ID: "q3"}, {ID: "decompose-greeting-q2"}, {ID: "q1"}}}
	if n := s.nextNumber(); n != 4 {
		t.Errorf("nextNumber = %d, want 4", n)
	}
}

func TestAnswersViewIsDerivedFromTheStore(t *testing.T) {
	r := &run{dir: t.TempDir()}
	at := time.Now()
	s := qstore{Questions: []qrec{
		{ID: "q1", Text: "A?", RaisedBy: "clarify", Status: qOpen},
		{ID: "q2", Text: "B?", RaisedBy: "clarify", Status: qOpen},
		{ID: "decompose-g-q1", Text: "C?", RaisedBy: "decompose:g", Status: qOpen},
	}}
	s.settle("q1", "x", byUnattended, at)
	s.settle("q2", "y", byHuman, at)
	s.settle("decompose-g-q1", "z", byHuman, at)
	if err := writeAnswersView(r, s); err != nil {
		t.Fatal(err)
	}
	as, err := loadAnswers(r)
	if err != nil || len(as.Answers) != 3 {
		t.Fatalf("answers = %+v, %v", as, err)
	}
	if !as.Answers[0].Assumed || as.Answers[1].Assumed || as.Answers[2].Stage != "decompose:g" || as.Answers[0].Stage != "clarify" {
		t.Errorf("answers = %+v", as.Answers)
	}
}

func TestAnswersViewKeepsALegacyFileWhenTheStoreIsEmpty(t *testing.T) {
	r := &run{dir: t.TempDir()}
	if err := writeJSON(r.path(fileAnswers), answersFile{Answers: []answer{{ID: "q1", Stage: "clarify", Question: "old?", Answer: "kept"}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeAnswersView(r, qstore{}); err != nil {
		t.Fatal(err)
	}
	if as, _ := loadAnswers(r); len(as.Answers) != 1 || as.Answers[0].Answer != "kept" {
		t.Errorf("a run planned before the store lost its answers: %+v", as)
	}
}

func TestDecisionRecordIsABrowsableFile(t *testing.T) {
	r := &run{dir: t.TempDir()}
	q := qrec{ID: "q2", Text: "Which store?", Why: "types", Kind: "decision", Options: []string{"memory", "postgres"}, Recommended: "memory",
		RecommendedWhy: "none declared", RaisedBy: "clarify", Round: 1, Status: qSettled, Answer: "postgres", AnsweredBy: byHuman,
		SettledAt: "2026-10-08T00:00:00Z", DependsOn: []string{"q1"}, History: []qhistory{{At: "2026-10-07T00:00:00Z", Answer: "memory"}}}
	if err := writeDecision(r, q, []string{"fn-b", "fn-a"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(r.dir, "decisions", "q2.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Decision q2", "Which store?", "Answered by: human", "postgres", "Recommended: memory", "Earlier answers", "fn-a, fn-b"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("decision record lacks %q:\n%s", want, raw)
		}
	}
	bad := q
	bad.ID = "../escape"
	if err := writeDecision(r, bad, nil); err == nil {
		t.Error("a question id that is not a plain name must be refused")
	}
}

func settledStore() qstore {
	return qstore{Questions: []qrec{
		{ID: "q1", Text: "A?", Kind: "decision", RaisedBy: "clarify", Round: 1, Status: qSettled, Answer: "x", AnsweredBy: byHuman},
		{ID: "q2", Text: "B?", Kind: "decision", RaisedBy: "clarify", Round: 1, Status: qSettled, Answer: "y", AnsweredBy: byAccepted, Recommended: "y"},
		{ID: "q3", Text: "C?", Kind: "decision", RaisedBy: "decompose:g", Status: qSettled, Answer: "z", AnsweredBy: byHuman},
	}}
}

func TestEmbedDecisionsPutsTheConversationInTheTree(t *testing.T) {
	s := settledStore()
	root := map[string]any{"id": "run"}
	fnA := map[string]any{"id": "fn-a", "decision_ids": []any{"q3", "q1"}}
	fnB := map[string]any{"id": "fn-b", "decision_ids": []any{}}
	embedDecisions(s, root, []map[string]any{fnA, fnB})
	if got := len(root["decisions"].([]any)); got != 3 {
		t.Fatalf("root carries %d records, want all 3", got)
	}
	a := fnA["decisions"].([]any)
	if len(a) != 2 || a[0].(map[string]any)["id"] != "q1" || a[1].(map[string]any)["id"] != "q3" {
		t.Errorf("fn-a carries %v, want q1 then q3 (store order)", a)
	}
	if b := fnB["decisions"].([]any); len(b) != 0 {
		t.Errorf("fn-b cites nothing but carries %v", b)
	}
	rec := a[0].(map[string]any)
	if rec["question"] != "A?" || rec["answer"] != "x" || rec["answered_by"] != "human" || rec["raised_by"] != "clarify" {
		t.Errorf("record = %v", rec)
	}
	if problems := checkEmbedded(s, root, []map[string]any{fnA, fnB}); len(problems) != 0 {
		t.Errorf("a freshly embedded tree reported %v", problems)
	}
}

func TestCheckEmbeddedCatchesAHandEdit(t *testing.T) {
	s := settledStore()
	root := map[string]any{"id": "run"}
	fnA := map[string]any{"id": "fn-a", "decision_ids": []any{"q1"}}
	embedDecisions(s, root, []map[string]any{fnA})

	fnA["decisions"].([]any)[0].(map[string]any)["answer"] = "tampered"
	if p := checkEmbedded(s, root, []map[string]any{fnA}); len(p) != 1 || !strings.Contains(p[0], "fn-a") {
		t.Errorf("edited answer: %v", p)
	}
	embedDecisions(s, root, []map[string]any{fnA})
	root["decisions"] = root["decisions"].([]any)[:2]
	if p := checkEmbedded(s, root, []map[string]any{fnA}); len(p) != 1 || !strings.Contains(p[0], "root") {
		t.Errorf("a record missing from the root: %v", p)
	}
	embedDecisions(s, root, []map[string]any{fnA})
	fnA["decision_ids"] = []any{"q1", "q2"}
	if p := checkEmbedded(s, root, []map[string]any{fnA}); len(p) != 1 {
		t.Errorf("decision_ids changed without the records: %v", p)
	}
}

func TestEmbeddedRecordsCarryHistoryAndOmitEmptyFields(t *testing.T) {
	q := qrec{ID: "q1", Text: "A?", Kind: "decision", RaisedBy: "clarify", Status: qSettled, Answer: "x", AnsweredBy: byHuman,
		History: []qhistory{{At: "t", Answer: "w"}}}
	m := recordOf(q)
	if _, has := m["options"]; has {
		t.Error("empty options must be omitted")
	}
	if h := m["history"].([]any); len(h) != 1 || h[0].(map[string]any)["answer"] != "w" {
		t.Errorf("history = %v", m["history"])
	}
}

func TestDecisionRecordAllowsUnderscoreIds(t *testing.T) {
	r := &run{dir: t.TempDir()}
	q := qrec{ID: "enrich-_fix-q1", Text: "A?", Kind: "decision", RaisedBy: "enrich:_fix", Status: qSettled, Answer: "x", AnsweredBy: byHuman}
	if err := writeDecision(r, q, nil); err != nil {
		t.Fatalf("an enrich repair question id was refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "decisions", "enrich-_fix-q1.md")); err != nil {
		t.Error(err)
	}
}

func TestLoadQStoreBuildsACompleteStoreFromALegacyAnswersFile(t *testing.T) {
	r := &run{dir: t.TempDir()}
	as := answersFile{Answers: []answer{
		{ID: "q1", Stage: "clarify", Question: "old?", Answer: "kept"},
		{ID: "q2", Stage: "clarify", Question: "guess?", Answer: "g", Assumed: true},
	}}
	if err := writeJSON(r.path(fileAnswers), as); err != nil {
		t.Fatal(err)
	}
	s, err := loadQStore(r)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Complete || len(s.Questions) != 2 || len(s.unsettled()) != 0 {
		t.Fatalf("store = %+v", s)
	}
	if q := s.Questions[0]; q.Status != qSettled || q.Answer != "kept" || q.AnsweredBy != byHuman || q.RaisedBy != "clarify" || q.Text != "old?" || q.Kind != "decision" {
		t.Errorf("q1 = %+v", q)
	}
	if s.Questions[1].AnsweredBy != byUnattended {
		t.Errorf("q2 = %+v", s.Questions[1])
	}
	if got := answersView(s); !reflect.DeepEqual(got, as) {
		t.Errorf("view = %+v, want %+v", got, as)
	}
}

func TestLoadQStoreWithNothingOnDiskIsEmptyAndOpen(t *testing.T) {
	s, err := loadQStore(&run{dir: t.TempDir()})
	if err != nil || s.Complete || len(s.Questions) != 0 || s.Questions == nil {
		t.Fatalf("store = %+v, %v", s, err)
	}
}

func TestQStoreSaveRoundTripsAndIsPrivate(t *testing.T) {
	r := &run{dir: t.TempDir()}
	s := qstore{Calls: 2, Round: 1, Complete: true, Questions: []qrec{{ID: "q1", Text: "A?", Status: qOpen}}}
	if err := s.save(r); err != nil {
		t.Fatal(err)
	}
	got, err := loadQStore(r)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if fi, _ := os.Stat(r.path(fileQuestions)); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
}

func TestEmbeddedCopiesAreIndependent(t *testing.T) {
	s := qstore{Questions: []qrec{{ID: "q1", Text: "A?", Kind: "decision", RaisedBy: "clarify", Status: qSettled, Answer: "x", AnsweredBy: byHuman,
		Options: []string{"x", "y"}, DependsOn: []string{"q0"}, History: []qhistory{{At: "t", Answer: "w"}}}}}
	root := map[string]any{"id": "run"}
	a := map[string]any{"id": "fn-a", "decision_ids": []any{"q1"}}
	b := map[string]any{"id": "fn-b", "decision_ids": []any{"q1"}}
	embedDecisions(s, root, []map[string]any{a, b})

	rec := a["decisions"].([]any)[0].(map[string]any)
	rec["answer"] = "tampered"
	rec["options"].([]any)[0] = "tampered"
	rec["depends_on"].([]any)[0] = "tampered"
	rec["history"].([]any)[0].(map[string]any)["answer"] = "tampered"

	for name, doc := range map[string][]any{"root": root["decisions"].([]any), "fn-b": b["decisions"].([]any)} {
		got := doc[0].(map[string]any)
		if got["answer"] != "x" || got["options"].([]any)[0] != "x" || got["depends_on"].([]any)[0] != "q0" ||
			got["history"].([]any)[0].(map[string]any)["answer"] != "w" {
			t.Errorf("%s changed when fn-a's record was edited: %v", name, got)
		}
	}
	if s.Questions[0].Answer != "x" || s.Questions[0].Options[0] != "x" {
		t.Error("the store changed")
	}
}
