package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/human"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/runner"
)

func flaggedStep(class string) func(planner.ReviseRequest) (planner.ReviseProposal, error) {
	return answer(planner.ReviseProposal{Outcome: planner.OutcomeFlagged, Class: class, Paragraph: "The signature must return a second value."})
}

func TestAHumanDecisionAboutAFlaggedLeafIsRecordedWithoutTouchingTheTree(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 4)}, true)
	failing(fc, id, 4)
	g.gate.by = human.AnsweredByHuman
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "mind the tabs " + canarySecret}}
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){flaggedStep(planner.ClassContractFlagged), flaggedStep(planner.ClassContractFlagged)}}
	untouched := map[string][]byte{}
	for _, rel := range []string{"root.json", strings.TrimPrefix(rc.plan.nodeKey(id), "tree/"), "answers.json", "_state/clarify/questions.json"} {
		untouched[rel] = readFile(t, filepath.Join(g.runDir, rel))
	}

	_, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop") // the second escalation has an empty queue: stop

	store := string(readFile(t, filepath.Join(g.runDir, "_state", "revisions", "decisions.json")))
	for _, want := range []string{`"id": "rv-fn-greet-1"`, `"id": "rv-fn-greet-2"`, `"answered_by": "human"`, `"raised_by": "revise:fn-greet"`, `"answer": "retry: (redacted)"`, `"answer": "stop"`} {
		if !strings.Contains(store, want) {
			t.Errorf("the decision store lacks %s:\n%s", want, store)
		}
	}
	if md := string(readFile(t, filepath.Join(g.runDir, "decisions", "rv-fn-greet-1.md"))); !strings.Contains(md, "ended with contract_flagged") {
		t.Errorf("rv-fn-greet-1.md = %s", md)
	}
	for rel, was := range untouched {
		if string(readFile(t, filepath.Join(g.runDir, rel))) != string(was) {
			t.Errorf("%s changed: a revision decision must not touch the tree or the question store", rel)
		}
	}
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatal(err)
	}
	if hasCanary(t, g, canarySecret) {
		t.Fatal("a secret reached a store through the human's note")
	}
}

func TestASkipAndAStopAreRecordedToo(t *testing.T) {
	t.Parallel()
	for _, action := range []human.Action{human.ActionSkip, human.ActionStop} {
		action := action
		t.Run(string(action), func(t *testing.T) {
			t.Parallel()
			g := newRig(t, reviseRewrite(2))
			id := leafID
			rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
			failing(fc, id, 2)
			g.gate.queue = []human.Resolution{{Action: action}}
			rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){flaggedStep(planner.ClassSplitRequired)}}
			_, _ = rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
			store := string(readFile(t, filepath.Join(g.runDir, "_state", "revisions", "decisions.json")))
			if !strings.Contains(store, `"id": "rv-fn-greet-1"`) || !strings.Contains(store, `"answer": "`+string(action)+`"`) || !strings.Contains(store, "split_required") {
				t.Errorf("store = %s", store)
			}
		})
	}
}

func TestReportCountsRewritesFlagsAndRefusals(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: {reply(variant(bad(id, 1), 1)), reply(variant(bad(id, 1), 2)), reply(good(id))}}, true)
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		unusable(planner.ClassReviseInvalid), answer(g.proposal(t, id, "REVISED-SENTENCE for the greeting.", greetTest)),
	}}
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), passVerdict()}
	runOne(t, rc, id)

	rep, err := rc.buildReport(context.Background(), finishResult{Status: "failed", Reason: "leaves_not_verified"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rewritten != 1 || len(rep.Revised) != 1 {
		t.Fatalf("Rewritten %d Revised %+v", rep.Rewritten, rep.Revised)
	}
	v := rep.Revised[0]
	if v.NodeID != id || v.Revisions != 1 || !v.TestsRewritten || len(v.Refusals) != 1 || v.Refusals[0] != planner.ClassReviseInvalid || v.Flag != "" || strings.Join(v.Changed, ",") != "description,tests" {
		t.Fatalf("revised leaf = %+v", v)
	}
	committed := 0
	for _, e := range rep.ByTaskType {
		committed += e.RevisionEscalations
	}
	if committed != 1 {
		t.Fatalf("revision_escalations = %d, want the number of committed rewrites (1)", committed)
	}
	raw, _ := json.Marshal(rep)
	for _, banned := range []string{"REVISED-SENTENCE", "trims tabs", "Greet(", canarySecret} {
		if strings.Contains(string(raw)+rep.Summary(), banned) {
			t.Errorf("the report carries %q: it may hold ids, classes, counts and field names only", banned)
		}
	}
	if !strings.Contains(rep.Summary(), "Rewritten leaves: 1") {
		t.Errorf("summary:\n%s", rep.Summary())
	}
}

func TestReportNamesAFlaggedLeaf(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
	failing(fc, id, 2)
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){flaggedStep(planner.ClassContractFlagged)}}
	_, _ = rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
	rep, err := rc.buildReport(context.Background(), finishResult{Status: "escalated", Reason: "human_stop"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Revised) != 1 || rep.Revised[0].Flag != planner.ClassContractFlagged || rep.Revised[0].Revisions != 0 || rep.Rewritten != 0 {
		t.Fatalf("revised = %+v rewritten %d", rep.Revised, rep.Rewritten)
	}
	found := false
	for _, f := range rep.Failures {
		if strings.HasPrefix(f, id+":") && strings.Contains(f, planner.ClassContractFlagged) {
			found = true
		}
	}
	if !found {
		t.Fatalf("failures = %v: the leaf's reason class must be named", rep.Failures)
	}
}

func TestAHumanNoteWithASecretSplitByInvisibleCharactersIsNotRecorded(t *testing.T) {
	t.Parallel()
	for name, split := range map[string]string{"nul": "\x00", "zwsp": "​", "bidi": "‮", "zwj": "‍"} {
		split := split
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t, reviseRewrite(2))
			id := leafID
			rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
			failing(fc, id, 2)
			g.gate.queue = []human.Resolution{{Action: human.ActionSkip, Note: "key " + canarySecret[:7] + split + canarySecret[7:]}}
			rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){flaggedStep(planner.ClassContractFlagged)}}
			_, _ = rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
			store := string(readFile(t, filepath.Join(g.runDir, "_state", "revisions", "decisions.json")))
			md := string(readFile(t, filepath.Join(g.runDir, "decisions", "rv-fn-greet-1.md")))
			for _, s := range []string{store, md} {
				if strings.Contains(s, canarySecret) || strings.Contains(s, "CANARY") {
					t.Fatalf("the secret reached a decision record:\n%s", s)
				}
			}
		})
	}
}

func TestNoReviserMeansNoDecisionIsRecordedAndTheEscalationGoesOn(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
	failing(fc, id, 2)
	rc.rev = nil
	g.gate.queue = []human.Resolution{{Action: human.ActionStop}}
	l := rc.plan.Leaf(id)
	ctx := context.Background()
	if ok, cerr := g.board.Claim(ctx, g.id, id, "executor:"+id); cerr != nil || !ok {
		t.Fatalf("claim: %v %v", ok, cerr)
	}
	if err := g.board.SetStatus(ctx, g.id, id, blackboard.StatusClaimed, blackboard.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	_, err := rc.escalate(context.Background(), l, blackboard.StatusInProgress, planner.ClassContractFlagged, nil)
	asStop(t, err, "escalated", "human_stop")
	if len(g.gate.Escalations()) != 1 {
		t.Fatal("the gate was not asked")
	}
	for _, rel := range []string{"_state/revisions/decisions.json", "decisions"} {
		if _, serr := os.Stat(filepath.Join(g.runDir, rel)); serr == nil {
			t.Errorf("%s exists: with no reviser nothing is recorded", rel)
		}
	}
}
