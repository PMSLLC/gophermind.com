package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/revlog"
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

// killedRun applies the revision in a first process, killed after step k,
// and returns the folder, the proposal and the run folder's rig for tampering.
func killedRun(t *testing.T, k int) (string, *rig, planner.ReviseProposal, *Leaf) {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	g1 := newRigIn(t, dir, Script{})
	prop := g1.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	rc1, err := startRun(context.Background(), g1.options())
	if err != nil {
		t.Fatal(err)
	}
	l := rc1.plan.Leaf(leafID)
	rc1.afterRevStep = killAt(k)
	_, err = rc1.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted)
	if k > 0 && !errors.Is(err, errRevisionKilled) {
		t.Fatalf("applyRevision: %v", err)
	}
	rc1.close()
	g1.close()
	return dir, g1, prop, l
}

func TestResumeStopsOnAHumanEditOfARevisedTestFile(t *testing.T) {
	t.Parallel()
	edit := func(t *testing.T, g *rig, l *Leaf) {
		p := testPath(g, l)
		if err := os.WriteFile(p, append(readFile(t, p), []byte("// a person's edit\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, k := range map[string]int{"committed revision": 8, "open intent": 3} {
		k := k
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, g1, _, l := killedRun(t, k)
			if k == 8 {
				// finish the whole transaction first, then edit
				g2 := newRigIn(t, dir, Script{})
				rc2, err := startRun(context.Background(), g2.options())
				if err != nil {
					t.Fatal(err)
				}
				rc2.close()
				g2.close()
			}
			edit(t, g1, l)
			// LoadPlan proves the chain against the test file before resume's
			// foreign-dirt check can run, so a person's edit stops there, with
			// nothing modified and no recovery.
			_, err := LoadPlan(g1.runDir, g1.repo)
			asStop(t, err, "failed", "plan_changed")
		})
	}
}

func TestOpenRevisionPathsListsATestFileOnlyWithTheRecordedContent(t *testing.T) {
	t.Parallel()
	dir, g1, prop, _ := killedRun(t, 7) // the commit record is written, the repository commit is not
	_ = g1
	g2 := newRigIn(t, dir, Script{})
	plan, err := LoadPlan(g2.runDir, g2.repo)
	if err != nil {
		t.Fatal(err)
	}
	rc := &runCtx{plan: plan, o: g2.options()}
	l := plan.Leaf(leafID)
	if got := rc.openRevisionPaths(); len(got) != 1 || got[0] != l.TestFile {
		t.Fatalf("recorded content: %v", got)
	}
	if revlog.Hash(readFile(t, testPath(g2, l))) != prop.TestSHA256 {
		t.Fatal("setup: the file is not the proposed test")
	}
	os.WriteFile(testPath(g2, l), []byte(greetTest+"// a person's edit\n"), 0o644)
	if got := rc.openRevisionPaths(); len(got) != 0 {
		t.Fatalf("an edited file is listed as the run's own: %v", got)
	}
}

func TestLoadPlanRefusesAStagedOnlyFolderThatIsNotAKillBeforeTheIntent(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, g *rig, dir string){
		"a staged body-1 changes a frozen field": func(t *testing.T, g *rig, dir string) {
			p := filepath.Join(dir, revlog.BodyFile(1))
			raw := string(readFile(t, p))
			if !strings.Contains(raw, `"kind": "function"`) {
				t.Fatal("setup: no kind field")
			}
			os.WriteFile(p, []byte(strings.Replace(raw, `"kind": "function"`, `"kind": "package"`, 1)), 0o600)
		},
		"the node file differs from the staged body-0": func(t *testing.T, g *rig, dir string) {
			p := g.nodeFile(leafID)
			os.WriteFile(p, append(readFile(t, p), ' '), 0o600)
		},
		"an unknown file in the folder": func(t *testing.T, g *rig, dir string) {
			os.WriteFile(filepath.Join(dir, revlog.BodyFile(2)), []byte("{}"), 0o600)
		},
		"the log removed after a commit": func(t *testing.T, g *rig, dir string) {
			os.Remove(filepath.Join(dir, "log.jsonl"))
		},
	}
	for name, mutate := range cases {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := 2
			if strings.HasPrefix(name, "the log removed") {
				k = 0 // not killed: applyRevision runs to the end
			}
			_, g, _, _ := killedRun(t, k)
			dir, _ := revlog.NodeDir(g.runDir, leafID)
			if k == 2 {
				if _, err := LoadPlan(g.runDir, g.repo); err != nil {
					t.Fatalf("setup: a kill before the intent must load: %v", err)
				}
			}
			mutate(t, g, dir)
			_, err := LoadPlan(g.runDir, g.repo)
			asStop(t, err, "failed", "plan_changed")
		})
	}
}

// noRevisionBoard drops SetRevision, as a board that lost the write would.
type noRevisionBoard struct{ blackboard.Blackboard }

func (noRevisionBoard) SetRevision(context.Context, string, string, int) error { return nil }

func TestResumeFaultsWhenRecoveryLeavesTheRuntimeRevisionBehindItsChain(t *testing.T) {
	t.Parallel()
	dir, g1, _, _ := killedRun(t, 0)
	if err := g1.board.SetRevision(context.Background(), g1.id, leafID, 0); err != nil {
		t.Fatal(err)
	}
	g2 := newRigIn(t, dir, Script{})
	o := g2.options()
	o.Board = noRevisionBoard{g2.board}
	_, err := startRun(context.Background(), o)
	asStop(t, err, "failed", "plan_changed")
}

// TestRecoveryAfterAHumanRetryDoesNotCountACommittedRewriteTwice is spec R15:
// one committed rewrite is one revision escalation, also when a person granted
// a retry (raising the round) before the run was resumed.
func TestRecoveryAfterAHumanRetryDoesNotCountACommittedRewriteTwice(t *testing.T) {
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
	// A person retries the leaf: the round goes up and is saved.
	stateMu.Lock()
	rc1.state.ExtraRevisions[leafID]++
	serr := rc1.state.Save(g1.runDir)
	stateMu.Unlock()
	if serr != nil {
		t.Fatal(serr)
	}
	rc1.close()
	g1.close()

	g2 := newRigIn(t, dir, Script{})
	rc2, err := startRun(context.Background(), g2.options())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer rc2.close()
	escs, _ := LoadEscalations(g2.runDir)
	n := 0
	for _, e := range escs {
		if e.Kind == "revision" && e.NodeID == leafID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("revision escalations = %d after one committed rewrite and a retry, want 1", n)
	}
}

// halfStaged leaves the folder as a kill between the two writes of Stage(0)
// would: body-0.json alone, no test-0.txt, no revision 1, no log.
func halfStaged(t *testing.T) (string, *rig, planner.ReviseProposal) {
	t.Helper()
	dir, g, prop, _ := killedRun(t, 2)
	d, _ := revlog.NodeDir(g.runDir, leafID)
	for _, n := range []string{revlog.TestFile(0), revlog.BodyFile(1), revlog.TestFile(1)} {
		if err := os.Remove(filepath.Join(d, n)); err != nil {
			t.Fatal(err)
		}
	}
	return dir, g, prop
}

func TestAHalfStagedRevisionZeroIsStagedAgainOnTheNextRevision(t *testing.T) {
	t.Parallel()
	dir, _, prop := halfStaged(t)
	g2 := newRigIn(t, dir, Script{})
	rc2, err := startRun(context.Background(), g2.options())
	if err != nil {
		t.Fatalf("resume over a half-staged revision 0: %v", err)
	}
	defer rc2.close()
	if refusal, err := rc2.applyRevision(context.Background(), rc2.plan.Leaf(leafID), prop, planner.TriggerLadderExhausted); err != nil || refusal != "" {
		t.Fatalf("applyRevision over a half-staged revision 0 = %q, %v", refusal, err)
	}
	revisionDone(t, g2, rc2, prop)
}

func TestAHalfStagedRevisionZeroThatIsNotTheNodeFileIsPlanChanged(t *testing.T) {
	t.Parallel()
	_, g, _ := halfStaged(t)
	d, _ := revlog.NodeDir(g.runDir, leafID)
	p := filepath.Join(d, revlog.BodyFile(0))
	if err := os.WriteFile(p, append(readFile(t, p), ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPlan(g.runDir, g.repo)
	asStop(t, err, "failed", "plan_changed")
}

// TestResumeRemovesAStrayReplaceTempOnlyUnderAnOpenIntent: a kill inside
// pathsafe.ReplaceTest leaves its temp file beside the test file. With the
// node's intent open it is this run's own residue and the resume removes it;
// with no open intent the same file is a person's and the resume stops.
func TestResumeRemovesAStrayReplaceTempOnlyUnderAnOpenIntent(t *testing.T) {
	t.Parallel()
	for name, k := range map[string]int{"open intent": 3, "no open intent": 0} {
		name, k := name, k
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, g1, prop, l := killedRun(t, k)
			stray := filepath.Join(g1.repo, filepath.FromSlash(path.Dir(l.TestFile)), ".gm-replace-"+path.Base(l.TestFile)+".0123456789abcdef")
			if err := os.WriteFile(stray, []byte(greetTest), 0o644); err != nil {
				t.Fatal(err)
			}
			g2 := newRigIn(t, dir, Script{})
			rc2, err := startRun(context.Background(), g2.options())
			if k == 0 {
				asStop(t, err, "failed", "foreign_dirt")
				return
			}
			if err != nil {
				t.Fatalf("resume with a stray temp file under an open intent: %v", err)
			}
			defer rc2.close()
			if _, serr := os.Lstat(stray); !os.IsNotExist(serr) {
				t.Fatalf("the stray temp file is still there: %v", serr)
			}
			revisionDone(t, g2, rc2, prop)
		})
	}
}
