package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, r *run, rel, body string) {
	t.Helper()
	if err := writeFileAtomic(r.path(rel), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// changeRun is a run folder that has been planned up to approval.
func changeRun(t *testing.T) *run {
	t.Helper()
	r := &run{id: "gm-2026-10-08-001", dir: t.TempDir()}
	write(t, r, "contracts.json", `{"components":[{"id":"greeting"},{"id":"types"}]}`)
	write(t, r, "root.json", `{}`)
	write(t, r, "greeting/component.json", `{}`)
	write(t, r, "types/component.json", `{}`)
	write(t, r, "_state/contract.json", `{}`)
	write(t, r, "_state/understanding.json", `{}`)
	write(t, r, "_state/classes.json", `{"fn-greet":"validation","fn-name":"pure"}`)
	write(t, r, "_state/decomposed.json", `{"components":{"greeting":[{"id":"fn-greet","rationale":"r","security":"s","contract":{"signature":"func Greet()"}}],"types":[{"id":"fn-name","rationale":"r"}]},"done":true}`)
	write(t, r, "_state/enriched.json", `{"components":{"greeting":{"rationale":"x"},"types":{"rationale":"y"}},"root":{"rationale":"z"},"done":true}`)
	write(t, r, "coverage.json", `{}`)
	write(t, r, "approval.json", `{}`)
	s := qstore{Complete: true, Calls: 2, Round: 1, Questions: []qrec{
		{ID: "q1", Text: "Trim names?", Kind: "decision", RaisedBy: "clarify", Round: 1, Status: qSettled, Answer: "yes", AnsweredBy: byHuman, SettledAt: "2026-10-08T00:00:00Z"},
		{ID: "q2", Text: "Module path?", Kind: "fact", FactKey: "go_module", RaisedBy: "clarify", Round: 1, Status: qSettled, Answer: "example.com/x", AnsweredBy: byProbe},
		{ID: "decompose-greeting-q3", Text: "Return a pointer?", Kind: "decision", RaisedBy: "decompose:greeting", Status: qSettled, Answer: "no", AnsweredBy: byHuman},
	}}
	if err := s.save(r); err != nil {
		t.Fatal(err)
	}
	if err := writeAnswersView(r, s); err != nil {
		t.Fatal(err)
	}
	return r
}

var changeNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func TestChangingAClarifyAnswerRestartsFromContract(t *testing.T) {
	r := changeRun(t)
	rep, err := changeAnswerIn(r, "q1", "no, keep spaces", changeNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reset != "contract" {
		t.Errorf("reset = %q", rep.Reset)
	}
	for _, gone := range []string{"_state/understanding.json", "contracts.json", "root.json", "greeting/component.json", "types/component.json", "_state/decomposed.json", "_state/enriched.json", "_state/classes.json", "coverage.json", "approval.json"} {
		if exists(r.path(gone)) {
			t.Errorf("%s survived the reset", gone)
		}
	}
	if !exists(r.path(fileQuestions)) || !exists(r.path(fileAnswers)) {
		t.Error("the question store and answers.json must survive")
	}
	s, _ := loadQStore(r)
	q := s.get("q1")
	if q.Answer != "no, keep spaces" || q.AnsweredBy != byHuman || len(q.History) != 1 || q.History[0].Answer != "yes" {
		t.Errorf("q1 = %+v", q)
	}
	as, _ := loadAnswers(r)
	if as.Answers[0].Answer != "no, keep spaces" {
		t.Errorf("answers.json = %+v", as)
	}
	md, _ := os.ReadFile(filepath.Join(r.dir, "decisions", "q1.md"))
	if !strings.Contains(string(md), "no, keep spaces") || !strings.Contains(string(md), "Earlier answers") {
		t.Errorf("the decision record was not rewritten:\n%s", md)
	}
}

func TestChangingAMidStageAnswerResetsOnlyThatComponent(t *testing.T) {
	r := changeRun(t)
	rep, err := changeAnswerIn(r, "decompose-greeting-q3", "yes, a pointer", changeNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reset != "greeting" {
		t.Fatalf("reset = %q, want greeting", rep.Reset)
	}
	if !exists(r.path("contracts.json")) || !exists(r.path("types/component.json")) || !exists(r.path("greeting/component.json")) {
		t.Error("a component reset must not touch the contract or the other components")
	}
	var dec decomposed
	if _, err := readJSON(r.path(stateDecomposed), &dec); err != nil {
		t.Fatal(err)
	}
	if len(dec.Components["greeting"]) != 0 || len(dec.Components["types"]) != 1 || dec.Done {
		t.Errorf("decomposed = %+v", dec)
	}
	st, _ := loadEnriched(r)
	if st.Components["greeting"] != nil || st.Components["types"] == nil || st.Done {
		t.Errorf("enriched = %+v", st)
	}
	classes, _ := loadClasses(r)
	if _, has := classes["fn-greet"]; has || classes["fn-name"] != "pure" {
		t.Errorf("classes = %v", classes)
	}
	if exists(r.path("coverage.json")) || exists(r.path("approval.json")) {
		t.Error("coverage and approval depend on the plan and must be removed")
	}
}

func TestChangeAnswerRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		prep func(r *run)
		id   string
		text string
		want string
	}{
		"tests already written":  {func(r *run) { write(t, r, "_state/testwriter.json", `{}`) }, "q1", "x", "written tests"},
		"the executor started":   {func(r *run) { write(t, r, "_state/executor.json", `{}`) }, "q1", "x", "written tests or started building"},
		"an unknown question":    {nil, "q99", "x", "no settled question"},
		"an answer by the probe": {nil, "q2", "x", "repository"},
		"the same answer":        {nil, "q1", "yes", "already the answer"},
		"an empty answer":        {nil, "q1", "   ", "1 to"},
		"an answer that is huge": {nil, "q1", strings.Repeat("a", maxAnswerBytes+1), "1 to"},
	} {
		t.Run(name, func(t *testing.T) {
			r := changeRun(t)
			if tc.prep != nil {
				tc.prep(r)
			}
			if _, err := changeAnswerIn(r, tc.id, tc.text, changeNow); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if !exists(r.path("contracts.json")) {
				t.Error("a refused change must not reset anything")
			}
		})
	}
}

func TestComponentOfStage(t *testing.T) {
	for in, want := range map[string]string{
		"decompose:greeting": "greeting", "enrich:greeting": "greeting", "enrich_comp:types": "types",
		"decompose:_fix": "", "enrich:_fix": "", "contract:_repair": "", "contract:greeting": "", "clarify": "", "enrich_root": "", "coverage": "",
	} {
		if got := componentOfStage(in); got != want {
			t.Errorf("componentOfStage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHistoryIsBounded(t *testing.T) {
	r := changeRun(t)
	for i := 0; i < maxHistory+3; i++ {
		if _, err := changeAnswerIn(r, "q1", "answer "+string(rune('a'+i)), changeNow); err != nil {
			t.Fatal(err)
		}
		// restore a planned state so the next change is not blocked by anything
		write(t, r, "contracts.json", `{"components":[]}`)
	}
	s, _ := loadQStore(r)
	if n := len(s.get("q1").History); n != maxHistory {
		t.Errorf("history has %d entries, want the last %d", n, maxHistory)
	}
}

func TestChangingAnEnrichAnswerResetsOnlyTheEnrichOutput(t *testing.T) {
	r := changeRun(t)
	s, _ := loadQStore(r)
	s.Questions = append(s.Questions, qrec{ID: "enrich-greeting-q1", Text: "Log the name?", Kind: "decision", RaisedBy: "enrich:greeting", Status: qSettled, Answer: "no", AnsweredBy: byHuman})
	if err := s.save(r); err != nil {
		t.Fatal(err)
	}
	if err := writeAnswersView(r, s); err != nil {
		t.Fatal(err)
	}
	rep, err := changeAnswerIn(r, "enrich-greeting-q1", "yes, log it", changeNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reset != "greeting" {
		t.Fatalf("reset = %q", rep.Reset)
	}
	dec, _ := loadDecomposed(r)
	g := dec.Components["greeting"]
	if len(g) != 1 || g[0]["id"] != "fn-greet" || g[0]["contract"] == nil {
		t.Fatalf("the Decompose drafts must be kept: %+v", g)
	}
	if _, has := g[0]["rationale"]; has {
		t.Error("the enrichment groups of the component's drafts must be removed")
	}
	if _, has := g[0]["security"]; has {
		t.Error("security must be removed")
	}
	if _, has := dec.Components["types"][0]["rationale"]; !has {
		t.Error("another component's enrichment must stay")
	}
	classes, _ := loadClasses(r)
	if classes["fn-greet"] != "validation" {
		t.Errorf("classes = %v", classes)
	}
	st, _ := loadEnriched(r)
	if st.Components["greeting"] != nil || st.Components["types"] == nil || st.Done {
		t.Errorf("enriched = %+v", st)
	}
	if !dec.Done {
		t.Error("Decompose must stay done")
	}
	if !exists(r.path("_state/understanding.json")) || !exists(r.path("contracts.json")) {
		t.Error("an Enrich answer must not touch the confirmation or the contract")
	}
	if exists(r.path("coverage.json")) || exists(r.path("approval.json")) {
		t.Error("coverage and approval depend on the plan and must be removed")
	}
}

func TestChangingARootEnrichAnswerResetsOnlyTheRootGroups(t *testing.T) {
	r := changeRun(t)
	s, _ := loadQStore(r)
	s.Questions = append(s.Questions, qrec{ID: "enrich-root-q1", Text: "Scope?", Kind: "decision", RaisedBy: "enrich_root", Status: qSettled, Answer: "a", AnsweredBy: byHuman})
	_ = s.save(r)
	rep, err := changeAnswerIn(r, "enrich-root-q1", "b", changeNow)
	if err != nil || rep.Reset != "root" {
		t.Fatalf("rep = %+v, err = %v", rep, err)
	}
	st, _ := loadEnriched(r)
	if st.Root != nil || st.Components["greeting"] == nil || st.Done {
		t.Errorf("enriched = %+v", st)
	}
}

func TestChangingARepairStageAnswerRestartsFromContract(t *testing.T) {
	for _, stage := range []string{"decompose:_fix", "enrich:_fix", "contract:_repair"} {
		r := changeRun(t)
		s, _ := loadQStore(r)
		s.Questions = append(s.Questions, qrec{ID: "fix-q1", Text: "Which?", Kind: "decision", RaisedBy: stage, Status: qSettled, Answer: "a", AnsweredBy: byHuman})
		_ = s.save(r)
		rep, err := changeAnswerIn(r, "fix-q1", "b", changeNow)
		if err != nil || rep.Reset != "contract" || exists(r.path("contracts.json")) {
			t.Errorf("%s: rep = %+v, err = %v", stage, rep, err)
		}
	}
}

func TestChangedAnswerInvalidatesTheApprovalHash(t *testing.T) {
	r := changeRun(t)
	before, err := os.ReadFile(r.path(fileAnswers))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changeAnswerIn(r, "q1", "no", changeNow); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(r.path(fileAnswers))
	if string(before) == string(after) {
		t.Fatal("answers.json is hashed into the approval and must change with the answer")
	}
	found := false
	for _, f := range hashedFiles {
		found = found || f == fileAnswers
	}
	if !found {
		t.Fatal("answers.json is not part of the approval hash")
	}
}

// The record of a changed answer lists the nodes the answer shaped, read before
// the reset removes them; it never falls back to "none recorded" for a question
// that nodes cite.
func TestChangedAnswerRecordListsTheNodesItShaped(t *testing.T) {
	r := changeRun(t)
	write(t, r, "_state/decomposed.json", `{"components":{"greeting":[{"id":"fn-greet","decision_ids":["q1"]}],"types":[{"id":"fn-name"}]},"done":true}`)
	write(t, r, "_state/enriched.json", `{"components":{"greeting":{"decision_ids":["q1"]}},"root":{"decision_ids":["q1"]},"done":true}`)
	if _, err := changeAnswerIn(r, "q1", "no, keep spaces", changeNow); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(r.dir, "decisions", "q1.md"))
	if strings.Contains(string(md), "none recorded") || !strings.Contains(string(md), "Shaped these nodes: fn-greet, greeting, root") {
		t.Errorf("the record does not list the nodes the answer shaped:\n%s", md)
	}
}

// A change that cannot reset the run leaves the old answer in place, so the
// same command can be run again: nothing is half done.
func TestAChangeThatCannotResetKeepsTheOldAnswerAndCanBeRepeated(t *testing.T) {
	r := changeRun(t)
	good, err := os.ReadFile(r.path("_state/decomposed.json"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, r, "_state/decomposed.json", `{not json`)
	if _, err := changeAnswerIn(r, "decompose-greeting-q3", "yes, a pointer", changeNow); err == nil {
		t.Fatal("a reset that cannot read the drafts must fail")
	}
	s, _ := loadQStore(r)
	if q := s.get("decompose-greeting-q3"); q.Answer != "no" || len(q.History) != 0 {
		t.Fatalf("the answer changed although the reset failed: %+v", q)
	}
	write(t, r, "_state/decomposed.json", string(good))
	if _, err := changeAnswerIn(r, "decompose-greeting-q3", "yes, a pointer", changeNow); err != nil {
		t.Fatalf("repeating the command: %v", err)
	}
	s, _ = loadQStore(r)
	if q := s.get("decompose-greeting-q3"); q.Answer != "yes, a pointer" {
		t.Errorf("answer = %q", q.Answer)
	}
}
