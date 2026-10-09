package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/revlog"
)

// killAt stops a revision after step k, as a kill would.
func killAt(k int) func(int) error {
	return func(step int) error {
		if step == k {
			return errRevisionKilled
		}
		return nil
	}
}

// vacuousTest compiles and never calls the function: it passes against the stub.
const vacuousTest = "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {}\n"

// revisionDone proves the leaf is at revision 1 with every file the proposal names.
func revisionDone(t *testing.T, g *rig, rc *runCtx, prop planner.ReviseProposal) {
	t.Helper()
	l := rc.plan.Leaf(leafID)
	if got := readFile(t, g.nodeFile(leafID)); revlog.Hash(got) != prop.BodySHA256 {
		t.Error("the node file is not the proposed body")
	}
	if got := string(readFile(t, testPath(g, l))); got != greetTest {
		t.Error("the test file is not the proposed test")
	}
	if lt, _ := planner.ReadLeafTests(g.runDir); lt[leafID].SHA256 != prop.TestSHA256 {
		t.Errorf("leaf_tests sha = %s, want %s", lt[leafID].SHA256, prop.TestSHA256)
	}
	if l.Revision != 1 || l.TestSHA256 != prop.TestSHA256 {
		t.Errorf("the leaf was not reloaded: revision %d", l.Revision)
	}
	if r := g.row(t, leafID); r.Revision != 1 {
		t.Errorf("runtime revision = %d, want 1", r.Revision)
	}
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	recs, _ := revlog.Read(dir)
	var kinds []string
	for _, r := range recs {
		kinds = append(kinds, string(r.Kind))
	}
	if strings.Join(kinds, ",") != "intent,commit" {
		t.Errorf("records = %v, want one intent and one commit", kinds)
	}
	fresh, err := LoadPlan(g.runDir, g.repo)
	if err != nil || fresh.Revised[leafID].N != 1 || fresh.Revised[leafID].Open != nil {
		t.Errorf("a fresh LoadPlan: %v %+v", err, fresh.Revised)
	}
	if dirty, _ := g.git.Dirty(); len(dirty) != 0 {
		t.Errorf("dirty after recovery: %v", dirty)
	}
	if n := strings.Count(g.gitCmd("log", "--format=%s"), "gm(revise): fn-greet test, revision 1"); n != 1 {
		t.Errorf("revision commits = %d, want 1", n)
	}
	escs, _ := LoadEscalations(g.runDir)
	n := 0
	for _, e := range escs {
		if e.Kind == "revision" && e.NodeID == leafID {
			n++
		}
	}
	if n != 1 {
		t.Errorf("revision escalations = %d, want 1", n)
	}
	if se := rc.planIntact(); se != nil {
		t.Errorf("planIntact: %+v", se)
	}
}

func TestRevisionKilledAtEveryStepConvergesWithoutAModelCall(t *testing.T) {
	t.Parallel()
	for k := 1; k <= 8; k++ {
		k := k
		t.Run(fmt.Sprintf("kill after step %d", k), func(t *testing.T) {
			t.Parallel()
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			g1 := newRigIn(t, dir, Script{})
			prop := g1.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
			rc1, err := startRun(context.Background(), g1.options())
			if err != nil {
				t.Fatal(err)
			}
			rc1.afterRevStep = killAt(k)
			if refusal, err := rc1.applyRevision(context.Background(), rc1.plan.Leaf(leafID), prop, planner.TriggerLadderExhausted); !errors.Is(err, errRevisionKilled) || refusal != "" {
				t.Fatalf("applyRevision = %q, %v; want the kill", refusal, err)
			}
			rc1.close()
			g1.close()

			// A second process: it reopens the folder and resumes.
			g2 := newRigIn(t, dir, Script{})
			rc2, err := startRun(context.Background(), g2.options())
			if err != nil {
				t.Fatalf("resume after a kill at step %d: %v", k, err)
			}
			defer rc2.close()
			if k <= 2 {
				// Nothing was recorded yet: the node is as approved, and the revision is applied again.
				if rc2.plan.Leaf(leafID).Revision != 0 {
					t.Fatal("a revision that was never recorded was recovered")
				}
				if refusal, err := rc2.applyRevision(context.Background(), rc2.plan.Leaf(leafID), prop, planner.TriggerLadderExhausted); err != nil || refusal != "" {
					t.Fatalf("applying again: %q, %v", refusal, err)
				}
			}
			revisionDone(t, g2, rc2, prop)
			if n := len(g2.fake.Requests()); n != 0 {
				t.Fatalf("%d model call(s) were made by recovery", n)
			}
		})
	}
}

func TestRecoveryRefusesAVacuousTestAndPutsTheOldOneBack(t *testing.T) {
	t.Parallel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	g1 := newRigIn(t, dir, Script{})
	prop := g1.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", vacuousTest)
	rc1, err := startRun(context.Background(), g1.options())
	if err != nil {
		t.Fatal(err)
	}
	l := rc1.plan.Leaf(leafID)
	origNode, origTest := readFile(t, g1.nodeFile(leafID)), readFile(t, testPath(g1, l))
	rc1.afterRevStep = killAt(3) // the intent is written, the test is not placed
	if _, err := rc1.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); !errors.Is(err, errRevisionKilled) {
		t.Fatal(err)
	}
	rc1.close()
	g1.close()

	g2 := newRigIn(t, dir, Script{})
	rc2, err := startRun(context.Background(), g2.options())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer rc2.close()
	l2 := rc2.plan.Leaf(leafID)
	if string(readFile(t, g2.nodeFile(leafID))) != string(origNode) || string(readFile(t, testPath(g2, l2))) != string(origTest) {
		t.Fatal("a refused recovery changed the node or the test file")
	}
	d, _ := revlog.NodeDir(g2.runDir, leafID)
	recs, _ := revlog.Read(d)
	if len(recs) != 2 || recs[1].Kind != revlog.KindRefused || recs[1].Class != planner.ClassVacuousTest {
		t.Fatalf("records = %+v", recs)
	}
	if r := g2.row(t, leafID); r.Revision != 0 || l2.Revision != 0 {
		t.Fatalf("revision moved to %d / %d", r.Revision, l2.Revision)
	}
	if dirty, _ := g2.git.Dirty(); len(dirty) != 0 {
		t.Fatalf("dirty after a refused recovery: %v", dirty)
	}
}

func TestRecoveryRaisesAStaleRuntimeRevisionAndFinishesTheCommit(t *testing.T) {
	t.Parallel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	g1 := newRigIn(t, dir, Script{})
	prop := g1.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	rc1, err := startRun(context.Background(), g1.options())
	if err != nil {
		t.Fatal(err)
	}
	if refusal, err := rc1.applyRevision(context.Background(), rc1.plan.Leaf(leafID), prop, planner.TriggerLadderExhausted); err != nil || refusal != "" {
		t.Fatalf("applyRevision = %q, %v", refusal, err)
	}
	// Undo what a kill between the commit record and the rest would not have done.
	if err := g1.board.SetRevision(context.Background(), g1.id, leafID, 0); err != nil {
		t.Fatal(err)
	}
	rc1.close()
	g1.close()

	g2 := newRigIn(t, dir, Script{})
	rc2, err := startRun(context.Background(), g2.options())
	if err != nil {
		t.Fatal(err)
	}
	defer rc2.close()
	revisionDone(t, g2, rc2, prop)
}

func TestLoadPlanStopsWhenAStagedFileOfAnOpenIntentIsMissing(t *testing.T) {
	t.Parallel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	g1 := newRigIn(t, dir, Script{})
	prop := g1.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	rc1, err := startRun(context.Background(), g1.options())
	if err != nil {
		t.Fatal(err)
	}
	l := rc1.plan.Leaf(leafID)
	rc1.afterRevStep = killAt(3)
	if _, err := rc1.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); !errors.Is(err, errRevisionKilled) {
		t.Fatal(err)
	}
	node, test := readFile(t, g1.nodeFile(leafID)), readFile(t, testPath(g1, l))
	rc1.close()
	g1.close()
	d, _ := revlog.NodeDir(g1.runDir, leafID)
	if err := os.Remove(filepath.Join(d, revlog.BodyFile(1))); err != nil {
		t.Fatal(err)
	}
	_, err = LoadPlan(g1.runDir, g1.repo)
	asStop(t, err, "failed", "plan_changed")
	if string(readFile(t, g1.nodeFile(leafID))) != string(node) || string(readFile(t, testPath(g1, l))) != string(test) {
		t.Fatal("a refused load modified the plan or the repository")
	}
}

func TestRuntimeRevisionBehindItsChainIsAHarnessFault(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	rc.plan.setTip(leafID, RevisedTip{Key: rc.plan.nodeKey(leafID), N: 1})
	rows := []blackboard.Row{{NodeID: leafID, Revision: 0}}
	asStop(t, rc.runtimeRevisionsOK(rows), "failed", "plan_changed")
	// Above its chain is normal: the notes rung and a repair round raise the same counter.
	if err := rc.runtimeRevisionsOK([]blackboard.Row{{NodeID: leafID, Revision: 3}}); err != nil {
		t.Fatalf("a counter above the chain: %v", err)
	}
	rc.plan.setTip(leafID, RevisedTip{})
	if err := rc.runtimeRevisionsOK([]blackboard.Row{{NodeID: leafID, Revision: 2}}); err != nil {
		t.Fatalf("a counter with no chain: %v", err)
	}
}

func TestResumeFailsWhenTheRuntimeRevisionStaysBehindItsChain(t *testing.T) {
	t.Parallel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	g1 := newRigIn(t, dir, Script{})
	prop := g1.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	rc1, err := startRun(context.Background(), g1.options())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rc1.applyRevision(context.Background(), rc1.plan.Leaf(leafID), prop, planner.TriggerLadderExhausted); err != nil {
		t.Fatal(err)
	}
	rc1.close()
	g1.close()
	g2 := newRigIn(t, dir, Script{})
	rc2, err := startRun(context.Background(), g2.options())
	if err != nil {
		t.Fatal(err)
	}
	defer rc2.close()
	// Recovery repaired nothing here: the row is already at 1. Put it behind after the fact.
	if err := g2.board.SetRevision(context.Background(), g2.id, leafID, 0); err != nil {
		t.Fatal(err)
	}
	asStop(t, rc2.checkRuntimeRevisions(context.Background()), "failed", "plan_changed")
}
