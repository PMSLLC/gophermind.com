package planner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/planner"
)

func TestRecordDecisionWritesTheStoreAndTheMarkdownAndLeavesTheTreeAlone(t *testing.T) {
	g := planned(t, nil)
	untouched := map[string][]byte{}
	for _, rel := range []string{"root.json", "greeting/fn-greet.json", "answers.json", "_state/clarify/questions.json"} {
		untouched[rel] = fileBytes(t, g, rel)
	}
	rv := planner.NewReviser(g.deps)
	d := planner.RevisionDecision{RunDir: g.runDir, NodeID: "fn-greet", Revision: 1, Class: planner.ClassContractFlagged,
		Action: "retry", Note: "use runes", AnsweredBy: human.AnsweredByHuman, Round: 1}
	id1, err := rv.RecordDecision(d)
	if err != nil || id1 != "rv-fn-greet-1" {
		t.Fatalf("first decision = %q, %v", id1, err)
	}
	d2 := d
	d2.Revision, d2.Action, d2.Note, d2.AnsweredBy = 2, "stop", "", "something unknown"
	if id2, err := rv.RecordDecision(d2); err != nil || id2 != "rv-fn-greet-2" {
		t.Fatalf("second decision = %q, %v", id2, err)
	}
	// The same answer again is the same record.
	if again, err := rv.RecordDecision(d); err != nil || again != id1 {
		t.Fatalf("repeat = %q, %v; want %q", again, err, id1)
	}
	md := string(fileBytes(t, g, "decisions/rv-fn-greet-1.md"))
	for _, want := range []string{"# Decision rv-fn-greet-1", "Leaf fn-greet ended with contract_flagged after 1 revision(s)", "retry: use runes", "Answered by: human", "Raised by: revise:fn-greet", "Shaped these nodes: fn-greet"} {
		if !strings.Contains(md, want) {
			t.Errorf("the decision record lacks %q:\n%s", want, md)
		}
	}
	store := string(fileBytes(t, g, "_state/revisions/decisions.json"))
	if strings.Count(store, `"id": "rv-fn-greet-`) != 2 || !strings.Contains(store, `"answered_by": "programmatic"`) {
		t.Errorf("store = %s: want two records and an unknown source recorded as programmatic", store)
	}
	for _, rel := range []string{"_state/revisions/decisions.json", "decisions/rv-fn-greet-1.md"} {
		if fi, err := os.Stat(filepath.Join(g.runDir, rel)); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v, want mode 0600", rel, fi.Mode().Perm(), err)
		}
	}
	// The question store, the root's decisions, the node and the approval did not move.
	for rel, was := range untouched {
		if string(fileBytes(t, g, rel)) != string(was) {
			t.Errorf("%s changed", rel)
		}
	}
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatal(err)
	}
}

func TestRecordDecisionKeepsALaterAnswerForTheSameQuestionAndRound(t *testing.T) {
	g := planned(t, nil)
	rv := planner.NewReviser(g.deps)
	stop := planner.RevisionDecision{RunDir: g.runDir, NodeID: "fn-greet", Revision: 1, Class: planner.ClassContractFlagged,
		Action: "stop", AnsweredBy: human.AnsweredByHuman, Round: 0}
	id1, err := rv.RecordDecision(stop)
	if err != nil || id1 != "rv-fn-greet-1" {
		t.Fatalf("stop = %q, %v", id1, err)
	}
	retry := stop
	retry.Action, retry.Note = "retry", "now use runes"
	id2, err := rv.RecordDecision(retry)
	if err != nil || id2 != "rv-fn-greet-2" {
		t.Fatalf("a retry after a stop in the same round = %q, %v: the second answer must be kept", id2, err)
	}
	if again, err := rv.RecordDecision(retry); err != nil || again != id2 {
		t.Fatalf("repeating the retry = %q, %v; want %q", again, err, id2)
	}
	if again, err := rv.RecordDecision(stop); err != nil || again != id1 {
		t.Fatalf("repeating the stop = %q, %v; want %q", again, err, id1)
	}
}

func TestRecordDecisionWritesTheNoteOnOneBoundedLine(t *testing.T) {
	g := planned(t, nil)
	rv := planner.NewReviser(g.deps)
	note := "first line\nsecond\x1b[31m line\u202e " + strings.Repeat("x", 2000)
	id, err := rv.RecordDecision(planner.RevisionDecision{RunDir: g.runDir, NodeID: "fn-greet", Revision: 1, Class: "test_fail",
		Action: "retry", Note: note, AnsweredBy: human.AnsweredByHuman})
	if err != nil {
		t.Fatal(err)
	}
	store := string(fileBytes(t, g, "_state/revisions/decisions.json"))
	md := string(fileBytes(t, g, "decisions/"+id+".md"))
	for _, s := range []string{store, md} {
		if strings.ContainsAny(s, "\x1b\u202e") || strings.Contains(s, strings.Repeat("x", 700)) {
			t.Errorf("an unbounded or unclean note reached a record:\n%s", s)
		}
	}
	if !strings.Contains(md, "retry: first line second") {
		t.Errorf("the note is not on one line:\n%s", md)
	}
}

func TestRecordDecisionRefusesWhatItCannotRecordSafely(t *testing.T) {
	g := planned(t, nil)
	rv := planner.NewReviser(g.deps)
	ok := planner.RevisionDecision{RunDir: g.runDir, NodeID: "fn-greet", Revision: 1, Class: "test_fail", Action: "retry", AnsweredBy: human.AnsweredByHuman}
	for name, mod := range map[string]func(*planner.RevisionDecision){
		"node id with a separator": func(d *planner.RevisionDecision) { d.NodeID = "../fn-greet" },
		"unknown action":           func(d *planner.RevisionDecision) { d.Action = "explode" },
		"relative run folder":      func(d *planner.RevisionDecision) { d.RunDir = "relative" },
	} {
		d := ok
		mod(&d)
		if _, err := rv.RecordDecision(d); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	d := ok
	d.Class = "../x"
	if id, err := rv.RecordDecision(d); err != nil || id == "" {
		t.Fatalf("an odd class is recorded as other, not refused: %q %v", id, err)
	}
	if !strings.Contains(string(fileBytes(t, g, "decisions/rv-fn-greet-1.md")), "ended with other") {
		t.Error("an unusable class was written into the record")
	}
}
