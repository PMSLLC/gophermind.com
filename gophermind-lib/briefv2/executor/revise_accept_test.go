package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/tree"
)

// acceptPatch fixes the contradiction the planned test hid: tabs and newlines.
const acceptPatch = `{"description":"Greet a name. Leading and trailing spaces, tabs and newlines are trimmed first. An empty result is a *NameError.","rewrite_tests":true,"assumptions":["the planned test only used spaces"]}`

// hashedPlanFiles are the files the plan hash covers (planner.hashedFiles).
var hashedPlanFiles = []string{"contracts.json", "_state/decomposed.json", "_state/classes.json", "coverage.json", "answers.json", "dependencies.json", "_state/understanding.json", "_state/enriched.json"}

func planFileHashes(t *testing.T, g *rig) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range hashedPlanFiles {
		raw, err := os.ReadFile(filepath.Join(g.runDir, filepath.FromSlash(rel)))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		out[rel] = hex.EncodeToString(sum[:])
	}
	return out
}

// promptsOf is the prompt of every request for the stage, oldest first.
func promptsOf(g *rig, stage string) []string {
	var out []string
	for _, r := range g.fake.Requests() {
		if stageOf(r) == stage {
			out = append(out, userText(r))
		}
	}
	return out
}

func requestsFor(g *rig, stage string) int { return len(promptsOf(g, stage)) }

// TestReviseRewritesAndLeafVerifies is the first acceptance test of
// GOAL-UNIFICATION task 5: the scripted model fails the leaf on every ladder
// entry, the revise stage rewrites it, and the leaf then verifies (spec R0, R1, R2, R15).
func TestReviseRewritesAndLeafVerifies(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, _ := g.leafRC(t, Script{
		"implement:" + id:         {reply(variant(bad(id, 1), 1)), reply(variant(bad(id, 1), 2)), reply(good(id))},
		"revise:" + id:            {reply(acceptPatch)},
		"testwrite:" + id + "-r1": {reply(greetTestwriteJSON(t, greetTest))},
	}, false)
	hashesBefore := planFileHashes(t, g)
	_, planHashBefore, err := planner.RenderPlan(g.runDir)
	if err != nil {
		t.Fatal(err)
	}

	out := runOne(t, rc, id)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified after one rewrite", out)
	}
	// R0: the stages, the revision in three places, the log.
	if got := strings.Join(stagesOf(g.fake), " "); got != "implement:fn-greet implement:fn-greet revise:fn-greet testwrite:fn-greet-r1 implement:fn-greet" {
		t.Fatalf("stages = %s", got)
	}
	if row := g.row(t, id); row.Revision != 1 || row.Status != blackboard.StatusVerified {
		t.Fatalf("row revision %d status %s", row.Revision, row.Status)
	}
	var node struct {
		Revision int `json:"revision"`
	}
	if err := json.Unmarshal(readFile(t, g.nodeFile(id)), &node); err != nil || node.Revision != 1 {
		t.Fatalf("node revision = %d, %v", node.Revision, err)
	}
	dir, _ := revlog.NodeDir(g.runDir, id)
	recs, _ := revlog.Read(dir)
	if len(recs) != 2 || recs[0].Kind != revlog.KindIntent || recs[1].Kind != revlog.KindCommit || recs[1].N != 1 || recs[1].Trigger != planner.TriggerLadderExhausted {
		t.Fatalf("log = %+v", recs)
	}
	// R1: the first implement prompt after the revision is built from the revised node.
	implement := promptsOf(g, "implement:"+id)
	if len(implement) != 3 {
		t.Fatalf("implement prompts = %d", len(implement))
	}
	for _, want := range []string{"tabs and newlines are trimmed first", "trims tabs and newlines", "<behaviour>"} {
		if !strings.Contains(implement[2], want) {
			t.Errorf("the prompt after the rewrite lacks %q", want)
		}
		if strings.Contains(implement[0]+implement[1], want) {
			t.Errorf("a prompt before the rewrite carries %q", want)
		}
	}
	if strings.Contains(implement[2], "previous_failure") {
		t.Error("the first attempt of the new revision still carries the old failure")
	}
	// R2: the approval did not move.
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("approval: %v", err)
	}
	if _, h, err := planner.RenderPlan(g.runDir); err != nil || h != planHashBefore {
		t.Fatalf("plan hash %s -> %s (%v)", planHashBefore, h, err)
	}
	for rel, was := range hashesBefore {
		if planFileHashes(t, g)[rel] != was {
			t.Errorf("%s (hashed by the plan hash) changed", rel)
		}
	}
	if se := rc.planIntact(); se != nil {
		t.Fatalf("planIntact: %+v", se)
	}
	if fresh, err := LoadPlan(g.runDir, g.repo); err != nil || fresh.Revised[id].N != 1 {
		t.Fatalf("a fresh load: %v %+v", err, fresh.Revised)
	}
	// The ledger counts the revise and the revision's Test-writer call under their task types.
	// The rig's planning pass already holds one testwrite row of its own (stage testwrite:<id>,
	// written before the run), so the revision's call is counted by its stage name.
	for stage, want := range map[string]int{"revise:" + id: 1, "testwrite:" + id + "-r1": 1} {
		task := strings.SplitN(stage, ":", 2)[0]
		rows, err := g.led.List(context.Background(), g.id, ledger.Filter{TaskType: task, NodeID: id})
		if err != nil {
			t.Fatal(err)
		}
		got := 0
		for _, r := range rows {
			if r.Stage == stage {
				got++
			}
		}
		if got != want {
			t.Errorf("ledger %s rows = %d; want %d", stage, got, want)
		}
	}
	// The leaf was committed once and the revision commit is not it.
	if n := g.leafCommits(id); n != 1 {
		t.Fatalf("leaf commits = %d, want 1", n)
	}
	// R15: the report.
	rep, err := rc.buildReport(context.Background(), finishResult{Status: "failed", Reason: "leaves_not_verified"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rewritten != 1 || len(rep.Revised) != 1 || rep.Revised[0].Revisions != 1 || !rep.Revised[0].TestsRewritten {
		t.Fatalf("report revised = %+v", rep.Revised)
	}
	n := 0
	for _, e := range rep.ByTaskType {
		n += e.RevisionEscalations
	}
	if n != 1 {
		t.Fatalf("revision_escalations = %d, want 1", n)
	}
}

// TestLeafNeverVerifiesEscalatesAfterMaxRevisions is the second acceptance
// test: every call fails, the revise stage rewrites each time, and after
// max_revisions the leaf goes to the gate with the last attempt's class (spec R0b).
func TestLeafNeverVerifiesEscalatesAfterMaxRevisions(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: distinct(id, 6),
		"revise:" + id: {
			reply(`{"description":"Greet a name; this is the first rewrite."}`),
			reply(`{"description":"Greet a name; this is the second rewrite."}`),
		},
	}, true)
	failing(fc, id, 6)

	_, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")
	if n := requestsFor(g, "revise:"+id); n != 2 {
		t.Fatalf("revise requests = %d, want exactly 2", n)
	}
	seen := g.gate.Escalations()
	if len(seen) != 1 || seen[0].Reason != "test_fail" || seen[0].Revisions != 2 {
		t.Fatalf("gate saw %+v, want one escalation after 2 rewrites with the last attempt's class", seen)
	}
	row := g.row(t, id)
	if row.Status != blackboard.StatusEscalated || row.Revision != 2 || len(row.Attempts) != 6 {
		t.Fatalf("row %s revision %d attempts %d", row.Status, row.Revision, len(row.Attempts))
	}
	if res, _ := LoadLeafResults(g.runDir); res[id].Status != "escalated" || res[id].Reason != "test_fail" {
		t.Fatalf("leaf result = %+v", res[id])
	}
	dir, _ := revlog.NodeDir(g.runDir, id)
	recs, _ := revlog.Read(dir)
	var kinds []string
	for _, r := range recs {
		kinds = append(kinds, string(r.Kind))
	}
	if strings.Join(kinds, ",") != "intent,commit,intent,commit" {
		t.Fatalf("log = %v", kinds)
	}
	if se := rc.planIntact(); se != nil {
		t.Fatalf("planIntact after two rewrites: %+v", se)
	}
	// No third rewrite is made even if the gate asks again later: the budget is spent.
	if rc.maxRevisions(rc.plan.Leaf(id)) != 2 || rc.plan.Leaf(id).Revision != 2 {
		t.Fatal("the budget or the revision is not what the test set up")
	}
}

// TestRevisePromptCarriesNoReasoningAndNoSecret is spec R10 and R11 end to end
// on the executor side: node scope, canaries in every reasoning group, a secret
// in the failing test names and the failure text, and a secret in the first
// reply of the model.
func TestRevisePromptCarriesNoReasoningAndNoSecret(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(1))
	id := leafID
	// Canaries in every group a node-scope call must not see, written before the run loads the plan.
	store := tree.NewStore(g.runDir)
	nodes, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := nodes.Nodes[id].Marshal()
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	doc["rationale"] = "RATIONALE-CANARY the reason this function exists in the brief"
	doc["alternatives"] = []any{map[string]any{"approach": "ALT-CANARY", "rejected_because": "ALT-CANARY-WHY"}}
	doc["refactor_notes"] = []any{map[string]any{"what": "REFACTOR-CANARY", "why": "later", "when": "later"}}
	doc["assumptions"] = []any{"ASSUME-CANARY"}
	doc["decisions"] = []any{map[string]any{"id": "q1", "question": "DECISION-CANARY?", "kind": "decision", "answer": "yes", "answered_by": "human", "raised_by": "clarify"}}
	enc, _ := json.Marshal(doc)
	n, err := tree.ParseNode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(n); err != nil {
		t.Fatal(err)
	}

	rc, fc := g.leafRC(t, Script{
		"implement:" + id: {reply(variant(bad(id, 1), 1)), reply(variant(bad(id, 1), 2)), reply(good(id))},
		// The first reply holds the secret: it is refused, never saved, and the router's repair call gets the second.
		"revise:" + id: {reply(`{"description":"Greet a name using ` + canarySecret + `."}`), reply(`{"description":"Greet a name. Tabs and newlines are trimmed too."}`)},
	}, true)
	leakyNames := "TestGreet" + canarySecret
	fc.LeafScript[id] = []runner.Verdict{
		failVerdict("test_fail", "FAIL: line with "+canarySecret, leakyNames),
		failVerdict("test_fail", "FAIL: line with "+canarySecret, leakyNames),
		passVerdict(),
	}

	out := runOne(t, rc, id)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	prompts := promptsOf(g, "revise:"+id)
	if len(prompts) != 2 {
		t.Fatalf("revise requests = %d, want the refused reply and the repair", len(prompts))
	}
	for _, p := range prompts {
		for _, banned := range []string{"RATIONALE-CANARY", "ALT-CANARY", "REFACTOR-CANARY", "ASSUME-CANARY", "DECISION-CANARY", "brief_ref", "decision_ids", "open_questions", canarySecret} {
			if strings.Contains(p, banned) {
				t.Errorf("a node-scope revise prompt carries %q", banned)
			}
		}
	}
	if !strings.Contains(prompts[0], "attempt 1") || !strings.Contains(prompts[0], "test_fail") {
		t.Error("the revise prompt lacks the attempt history")
	}
	entries, _ := os.ReadDir(filepath.Join(g.runDir, "_state", "replies"))
	saved := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "revise_") {
			saved++
		}
	}
	if saved != 1 {
		t.Fatalf("saved revise replies = %d, want 1 (the reply with the secret is never saved)", saved)
	}
	if hasCanary(t, g, canarySecret) {
		t.Fatal("the secret reached a store: reply, log, decision record, report, ledger, event or commit")
	}
}

// TestAPatchCannotNameAPath is spec R12 end to end: a patch that tries to name
// a test file is refused, twice, and the leaf escalates with revise_invalid and
// no file outside the plan exists.
func TestAPatchCannotNameAPath(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: distinct(id, 2),
		"revise:" + id:    {reply(`{"description":"x","test_file":"../evil_test.go"}`)},
	}, true)
	g.fake.Loop = true // every revise request, repairs included, gets the same reply
	failing(fc, id, 2)
	_, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")
	if seen := g.gate.Escalations(); len(seen) != 1 || seen[0].Reason != planner.ClassReviseInvalid {
		t.Fatalf("gate saw %+v", seen)
	}
	if row := g.row(t, id); row.Revision != 0 {
		t.Fatalf("revision = %d: a refused patch consumes none", row.Revision)
	}
	for _, p := range []string{filepath.Join(filepath.Dir(g.repo), "evil_test.go"), filepath.Join(g.repo, "evil_test.go")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s exists", p)
		}
	}
	if se := rc.planIntact(); se != nil {
		t.Fatalf("planIntact: %+v", se)
	}
}
