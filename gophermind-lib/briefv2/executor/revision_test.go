package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/tree"
)

// greetTest is a valid revised test for fn-greet: the planned cases plus tabs and newlines.
const greetTest = `package greet

import "testing"

func TestGreet(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"greets a name", "Ada", "Hello, Ada!", false},
		{"trims the name", "  Ada  ", "Hello, Ada!", false},
		{"trims tabs and newlines", "\tAda\n", "Hello, Ada!", false},
		{"refuses an empty name", "   ", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Greet(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("Greet(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("Greet(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
`

// greetTestwriteJSON is a Test-writer reply carrying src for fn-greet.
func greetTestwriteJSON(t *testing.T, src string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"test_file": src, "tests": []map[string]string{
		{"name": "greets a name", "polarity": "success", "covers": "happy", "given": `"Ada"`, "expect": "Hello, Ada! and a nil error"},
		{"name": "trims the name", "polarity": "boundary", "covers": "input:name", "given": `"  Ada  "`, "expect": "Hello, Ada! and a nil error"},
		{"name": "trims tabs and newlines", "polarity": "boundary", "covers": "input:name", "given": `"\tAda\n"`, "expect": "Hello, Ada! and a nil error"},
		{"name": "refuses an empty name", "polarity": "negative", "covers": "error:1", "given": `"   "`, "expect": "an empty string and a *NameError"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// proposal builds the ReviseProposal the planner would return for the leaf:
// the node with a new description and revision n+1, and (newTest non-empty)
// that test file.
func (g *rig) proposal(t *testing.T, id, description, newTest string) planner.ReviseProposal {
	t.Helper()
	l := g.plan.Leaf(id)
	raw := readFile(t, filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(g.plan.nodeKey(id), "tree/"))))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["description"], doc["revision"] = description, l.Revision+1
	doc["assumptions"] = append(doc["assumptions"].([]any), "revision 1: because the test said so")
	enc, _ := json.Marshal(doc)
	n, err := tree.ParseNode(enc)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := n.Marshal()
	body := append(b, '\n')
	prop := planner.ReviseProposal{Outcome: planner.OutcomeRewritten, Body: body, BodySHA256: revlog.Hash(body),
		Changed: []string{"description"}, Assumptions: []string{"because the test said so"}, TestSHA256: l.TestSHA256, Model: "a/m1"}
	if newTest != "" {
		prop.TestChanged, prop.TestSource, prop.TestSHA256 = true, newTest, revlog.Hash([]byte(newTest))
		prop.Changed = append(prop.Changed, "tests")
	}
	return prop
}

func testPath(g *rig, l *Leaf) string { return filepath.Join(g.repo, filepath.FromSlash(l.TestFile)) }

func (g *rig) nodeFile(id string) string {
	return filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(g.plan.nodeKey(id), "tree/")))
}

func TestApplyRevisionCommitsEveryStep(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	origNode, origTest := readFile(t, g.nodeFile(leafID)), readFile(t, testPath(g, l))
	fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet")} // the red check
	prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)

	refusal, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted)
	if err != nil || refusal != "" {
		t.Fatalf("applyRevision = %q, %v", refusal, err)
	}
	if got := readFile(t, g.nodeFile(leafID)); revlog.Hash(got) != prop.BodySHA256 {
		t.Fatal("the node file is not the proposed body")
	}
	if got := readFile(t, testPath(g, l)); string(got) != greetTest {
		t.Fatal("the test file is not the proposed test")
	}
	lt, _ := planner.ReadLeafTests(g.runDir)
	if lt[leafID].SHA256 != prop.TestSHA256 {
		t.Fatalf("leaf_tests sha = %s, want %s", lt[leafID].SHA256, prop.TestSHA256)
	}
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	body0, test0, err := revlog.ReadStaged(dir, 0)
	if err != nil || string(body0) != string(origNode) || string(test0) != string(origTest) {
		t.Fatalf("revision 0 was not staged byte for byte: %v", err)
	}
	recs, _ := revlog.Read(dir)
	if len(recs) != 2 || recs[0].Kind != revlog.KindIntent || recs[1].Kind != revlog.KindCommit || recs[1].N != 1 || recs[1].Trigger != "ladder_exhausted" {
		t.Fatalf("records = %+v", recs)
	}
	if r := g.row(t, leafID); r.Revision != 1 {
		t.Fatalf("runtime revision = %d, want 1", r.Revision)
	}
	if l.Revision != 1 || l.Description != "REVISED-SENTENCE for the greeting." || l.TestSHA256 != prop.TestSHA256 {
		t.Fatalf("the leaf was not reloaded: %+v", l)
	}
	if tip, ok := rc.plan.tip(leafID); !ok || tip.N != 1 || tip.BodySHA256 != prop.BodySHA256 {
		t.Fatalf("tip = %+v %v", tip, ok)
	}
	if se := rc.planIntact(); se != nil {
		t.Fatalf("planIntact after a revision: %+v", se)
	}
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("approval after a revision: %v", err)
	}
	if fresh, err := LoadPlan(g.runDir, g.repo); err != nil || fresh.Revised[leafID].N != 1 {
		t.Fatalf("a fresh LoadPlan: %v %+v", err, fresh.Revised)
	}
	if files := fc.files[leafID]; len(files) != 1 || len(files[0]) != 1 || files[0][0] != l.StubFile {
		t.Fatalf("the red check ran on %v, want the stub only", files)
	}
	if dirty, _ := g.git.Dirty(); len(dirty) != 0 {
		t.Fatalf("dirty after the revision commit: %v", dirty)
	}
	if subjects := g.gitCmd("log", "--format=%s"); !strings.Contains(subjects, "gm(revise): fn-greet test, revision 1") {
		t.Fatalf("the revision commit is missing:\n%s", subjects)
	}
	escs, _ := LoadEscalations(g.runDir)
	n := 0
	for _, e := range escs {
		if e.Kind == "revision" && e.NodeID == leafID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("revision escalations = %d, want 1", n)
	}
	events := string(readFile(t, filepath.Join(g.runDir, "_state", "events.jsonl")))
	if !strings.Contains(events, `"kind":"revision"`) {
		t.Fatal("the revision event is missing from events.jsonl")
	}
	if lc := g.leafCommits(leafID); lc != 0 {
		t.Fatalf("a revision commit was counted as a leaf commit (%d)", lc)
	}
}

func TestApplyRevisionRefusesATestThatPassesTheStubOrDoesNotBuild(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		verdict runner.Verdict
		class   string
	}{
		"passes the stub": {passVerdict(), planner.ClassVacuousTest},
		"does not build":  {failVerdict(runner.ClassBuild, "x"), planner.ClassRedBuild},
		"does not vet":    {failVerdict(runner.ClassVet, "x"), planner.ClassRedBuild},
	} {
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			rc, fc := g.leafRC(t, Script{}, true)
			l := rc.plan.Leaf(leafID)
			origNode, origTest := readFile(t, g.nodeFile(leafID)), readFile(t, testPath(g, l))
			lt0, _ := planner.ReadLeafTests(g.runDir)
			fc.LeafScript[leafID] = []runner.Verdict{c.verdict}
			refusal, err := rc.applyRevision(context.Background(), l, g.proposal(t, leafID, "X", greetTest), planner.TriggerLadderExhausted)
			if err != nil || refusal != c.class {
				t.Fatalf("applyRevision = %q, %v; want %s", refusal, err, c.class)
			}
			if string(readFile(t, g.nodeFile(leafID))) != string(origNode) || string(readFile(t, testPath(g, l))) != string(origTest) {
				t.Fatal("a refused revision changed the node or the test file")
			}
			if lt, _ := planner.ReadLeafTests(g.runDir); lt[leafID] != lt0[leafID] {
				t.Fatal("a refused revision changed leaf_tests.json")
			}
			if r := g.row(t, leafID); r.Revision != 0 || l.Revision != 0 {
				t.Fatalf("revision moved: row %d leaf %d", r.Revision, l.Revision)
			}
			dir, _ := revlog.NodeDir(g.runDir, leafID)
			recs, _ := revlog.Read(dir)
			if len(recs) != 2 || recs[0].Kind != revlog.KindIntent || recs[1].Kind != revlog.KindRefused || recs[1].N != 1 || recs[1].Class != c.class {
				t.Fatalf("records = %+v", recs)
			}
			if _, err := os.Stat(filepath.Join(dir, revlog.BodyFile(1))); err == nil {
				t.Fatal("the staged body of a refused revision was left behind")
			}
			if _, err := LoadPlan(g.runDir, g.repo); err != nil {
				t.Fatalf("the plan does not load after a refusal: %v", err)
			}
			if strings.Contains(g.gitCmd("log", "--format=%s"), "gm(revise)") {
				t.Fatal("a refused revision was committed")
			}
		})
	}
}

func TestApplyRevisionWithTheTestUnchangedRunsNoCheckAndMakesNoCommit(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	commits := g.gitCmd("rev-list", "--count", "HEAD")
	refusal, err := rc.applyRevision(context.Background(), l, g.proposal(t, leafID, "Only the words change.", ""), planner.TriggerContractProblem)
	if err != nil || refusal != "" {
		t.Fatalf("applyRevision = %q, %v", refusal, err)
	}
	if fc.checks(leafID) != 0 {
		t.Fatal("a red check ran although the test did not change")
	}
	if g.gitCmd("rev-list", "--count", "HEAD") != commits {
		t.Fatal("a commit was made although the test did not change")
	}
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	recs, _ := revlog.Read(dir)
	if recs[1].TestSHA256Before != recs[1].TestSHA256After || recs[1].Trigger != "contract_problem" {
		t.Fatalf("commit record = %+v", recs[1])
	}
}

func TestApplyRevisionStopsWhenTheTestFileChangedAfterPlanning(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	p := testPath(g, l)
	os.WriteFile(p, append(readFile(t, p), []byte("\n// edited\n")...), 0o644)
	_, err := rc.applyRevision(context.Background(), l, g.proposal(t, leafID, "X", greetTest), planner.TriggerLadderExhausted)
	asStop(t, err, "failed", "test_file_changed")
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	if _, serr := os.Stat(dir); serr == nil {
		t.Fatal("something was staged before the test file was proved")
	}
}

func TestApplyRevisionRefusesALinkedTestFileAndAnUnsafeNodeID(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	// The test file replaced by a link to a file outside the repository.
	outside := filepath.Join(t.TempDir(), "outside_test.go")
	os.WriteFile(outside, []byte("package greet\n"), 0o644)
	p := testPath(g, l)
	os.Remove(p)
	os.Symlink(outside, p)
	if _, err := rc.applyRevision(context.Background(), l, g.proposal(t, leafID, "X", greetTest), planner.TriggerLadderExhausted); err == nil {
		t.Fatal("applyRevision followed a linked test file")
	}
	if got, _ := os.ReadFile(outside); string(got) != "package greet\n" {
		t.Fatal("the file behind the link was written")
	}
	// A leaf whose id carries a separator cannot name a revision folder.
	bad := *l
	bad.ID = "fn-greet/../../x"
	if _, err := rc.applyRevision(context.Background(), &bad, g.proposal(t, leafID, "X", ""), planner.TriggerLadderExhausted); err == nil {
		t.Fatal("a node id with a separator was accepted")
	}
}

func TestApplyRevisionBodyZeroMustBeThePlanAsLoaded(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	os.WriteFile(g.nodeFile(leafID), append(readFile(t, g.nodeFile(leafID)), ' '), 0o600)
	_, err := rc.applyRevision(context.Background(), l, g.proposal(t, leafID, "X", ""), planner.TriggerLadderExhausted)
	asStop(t, err, "failed", "plan_changed")
}

func TestApplyRevisionKilledAtAnyStepRollsForwardToTheSameFiles(t *testing.T) {
	t.Parallel()
	for _, step := range []int{3, 4, 5, 6} {
		step := step
		t.Run(string(rune('0'+step)), func(t *testing.T) {
			g := newRig(t)
			rc, fc := g.leafRC(t, Script{}, true)
			l := rc.plan.Leaf(leafID)
			fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet")}
			prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
			rc.afterRevStep = func(n int) error {
				if n == step {
					return errRevisionKilled
				}
				return nil
			}
			if _, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); err != errRevisionKilled {
				t.Fatalf("applyRevision = %v, want the kill", err)
			}
			// The open-intent window: the node file may already be the staged
			// body, and planIntact must still accept it (for this node only).
			if se := rc.planIntact(); se != nil {
				t.Fatalf("planIntact inside the open-intent window: %+v", se)
			}
			rc.afterRevStep = nil
			dir, _ := revlog.NodeDir(g.runDir, leafID)
			recs, _ := revlog.Read(dir)
			if len(recs) != 1 || recs[0].Kind != revlog.KindIntent {
				t.Fatalf("records = %+v", recs)
			}
			if refusal, err := rc.rollForward(context.Background(), l, dir, recs[0]); err != nil || refusal != "" {
				t.Fatalf("rollForward = %q, %v", refusal, err)
			}
			if got := readFile(t, g.nodeFile(leafID)); revlog.Hash(got) != prop.BodySHA256 {
				t.Fatal("the node file is not the proposed body")
			}
			if got := readFile(t, testPath(g, l)); string(got) != greetTest {
				t.Fatal("the test file is not the proposed test")
			}
			if l.Revision != 1 || g.row(t, leafID).Revision != 1 {
				t.Fatalf("revision: leaf %d row %d", l.Revision, g.row(t, leafID).Revision)
			}
			if se := rc.planIntact(); se != nil {
				t.Fatalf("planIntact after the roll forward: %+v", se)
			}
			if n := strings.Count(g.gitCmd("log", "--format=%s"), "gm(revise): fn-greet test, revision 1"); n != 1 {
				t.Fatalf("revision commits = %d, want 1", n)
			}
			if _, err := LoadPlan(g.runDir, g.repo); err != nil {
				t.Fatalf("LoadPlan: %v", err)
			}
		})
	}
}

func TestApplyRevisionOpenWindowAcceptsTheOpenBodyForItsOwnNodeOnly(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet")}
	rc.afterRevStep = func(n int) error {
		if n == 6 {
			return errRevisionKilled
		}
		return nil
	}
	prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	if _, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); err != errRevisionKilled {
		t.Fatalf("applyRevision = %v", err)
	}
	// Another node holding some other content is still plan_changed.
	other := ""
	for _, o := range rc.plan.Leaves {
		if o.ID != leafID {
			other = o.ID
			break
		}
	}
	p := g.nodeFile(other)
	orig := readFile(t, p)
	os.WriteFile(p, append(append([]byte{}, orig...), ' '), 0o600)
	if se := rc.planIntact(); se == nil {
		t.Fatal("a changed node passed planIntact")
	}
	os.WriteFile(p, orig, 0o600)
	// The staged body of the open intent in a different node's file is refused.
	os.WriteFile(p, readFile(t, g.nodeFile(leafID)), 0o600)
	if se := rc.planIntact(); se == nil {
		t.Fatal("the open body was accepted for another node")
	}
	os.WriteFile(p, orig, 0o600)
}

// R8: finishCommitted sets the chain tip BEFORE planIntact. The state here is
// what a restart or a stale tip leaves: the commit record is written, the node
// file holds the new body, and the in-memory tip is still the previous
// committed hash with no open intent. planIntact passes only if setTip ran first.
func TestFinishCommittedSetsTheTipBeforePlanIntact(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet")}
	prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	rc.afterRevStep = func(n int) error {
		if n == 6 {
			return errRevisionKilled
		}
		return nil
	}
	if _, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); err != errRevisionKilled {
		t.Fatalf("applyRevision = %v", err)
	}
	rc.afterRevStep = nil
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	recs, _ := revlog.Read(dir)
	commit := recs[0]
	commit.Kind = revlog.KindCommit
	if err := revlog.Append(dir, commit); err != nil {
		t.Fatal(err)
	}
	// Forget the open intent: the tip is the previous committed state.
	rc.plan.revMu.Lock()
	delete(rc.plan.Revised, leafID)
	rc.plan.revMu.Unlock()
	if se := rc.planIntact(); se == nil {
		t.Fatal("precondition: planIntact must reject the new body while the tip is stale")
	}
	if err := rc.finishCommitted(context.Background(), l, commit, false); err != nil {
		t.Fatalf("finishCommitted from a stale tip: %v", err)
	}
	if tip, ok := rc.plan.tip(leafID); !ok || tip.N != 1 || tip.Open != nil {
		t.Fatalf("tip = %+v %v", tip, ok)
	}
	if se := rc.planIntact(); se != nil {
		t.Fatalf("planIntact after finishCommitted: %+v", se)
	}
}

// A refusal closes the in-memory intent: the refused body is no longer
// accepted for the node file.
func TestAbortRevisionClearsTheOpenIntentSoTheRefusedBodyIsRejected(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	prop := g.proposal(t, leafID, "X", greetTest)
	if refusal, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); err != nil || refusal != planner.ClassVacuousTest {
		t.Fatalf("applyRevision = %q, %v", refusal, err)
	}
	if tip, ok := rc.plan.tip(leafID); ok {
		t.Fatalf("a tip is left after a refusal: %+v", tip)
	}
	orig := readFile(t, g.nodeFile(leafID))
	os.WriteFile(g.nodeFile(leafID), prop.Body, 0o600)
	if se := rc.planIntact(); se == nil {
		t.Fatal("the refused body passed planIntact")
	}
	os.WriteFile(g.nodeFile(leafID), orig, 0o600)
	if se := rc.planIntact(); se != nil {
		t.Fatalf("planIntact on the original: %+v", se)
	}
}

// If the refused record cannot be written the intent is still open on disk, so
// the in-memory intent stays open too and the error comes back.
func TestAbortRevisionKeepsTheIntentOpenWhenTheRefusedRecordFails(t *testing.T) {
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	fc.Hook = func(string) { os.Chmod(filepath.Join(dir, "log.jsonl"), 0o400) }
	defer os.Chmod(filepath.Join(dir, "log.jsonl"), 0o600)
	if os.Geteuid() == 0 {
		t.Skip("file modes do not bind root")
	}
	_, err := rc.applyRevision(context.Background(), l, g.proposal(t, leafID, "X", greetTest), planner.TriggerLadderExhausted)
	if err == nil {
		t.Fatal("a failed refused-record append was not reported")
	}
	if tip, ok := rc.plan.tip(leafID); !ok || tip.Open == nil {
		t.Fatalf("the open intent was cleared although it is open on disk: %+v %v", tip, ok)
	}
}
