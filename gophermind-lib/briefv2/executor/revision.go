package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/tree"
)

// errRevisionKilled is what a test hook returns to stop a revision between two steps.
var errRevisionKilled = errors.New("executor: a test stopped the revision between two steps")

func (rc *runCtx) revStep(n int) error {
	if rc.afterRevStep != nil {
		return rc.afterRevStep(n)
	}
	return nil
}

func (rc *runCtx) stamp() string { return runfs.TS(rc.o.Now()) }

func (rc *runCtx) scrubAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = rc.scrubText(s)
	}
	return out
}

// applyRevision is spec section 10 for a rewritten proposal. Steps: 1 check,
// 2 stage, 3 intent, then rollForward (4 test + red check, 5 leaf_tests, 6 node
// file, 7 commit + runtime + git, 8 reload). A refusal class comes back with a
// nil error and leaves the node, the test and leaf_tests.json as they were.
func (rc *runCtx) applyRevision(ctx context.Context, l *Leaf, prop planner.ReviseProposal, trigger string) (string, error) {
	n := l.Revision + 1
	dir, err := revlog.NodeDir(rc.o.RunDir, l.ID)
	if err != nil {
		return "", errPlanChanged()
	}
	key := rc.plan.nodeKey(l.ID)
	rel := strings.TrimPrefix(key, "tree/")
	curBody, err := readPlanFile(rc.o.RunDir, rel)
	if err != nil {
		return "", err
	}
	want := rc.plan.Hashes[key]
	if tip, ok := rc.plan.tip(l.ID); ok {
		want = tip.BodySHA256
	}
	if revlog.Hash(curBody) != want {
		return "", errPlanChanged()
	}
	testAbs, err := pathsafe.ResolveTest(rc.o.Repo, l.TestFile)
	if err != nil {
		return "", fmt.Errorf("executor: leaf %s: its test path is not allowed", l.ID)
	}
	curTest, err := readNoFollow(testAbs)
	if err != nil {
		return "", fmt.Errorf("executor: the test file of leaf %s is not readable", l.ID)
	}
	if revlog.Hash(curTest) != l.TestSHA256 {
		return "", &stopError{Status: "failed", Reason: "test_file_changed",
			Message: fmt.Sprintf("executor: the test file of leaf %s changed since it was planned", l.ID)}
	}
	newTest := curTest
	if prop.TestChanged {
		newTest = []byte(prop.TestSource)
	}
	if revlog.Hash(prop.Body) != prop.BodySHA256 || revlog.Hash(newTest) != prop.TestSHA256 {
		return "", fmt.Errorf("executor: the proposal for leaf %s does not match its own hashes", l.ID)
	}
	if err := rc.revStep(1); err != nil {
		return "", err
	}
	if n == 1 {
		// Revision 0 is create-only. A refused first revision leaves it staged,
		// and a second attempt may keep it only if it is still the same bytes.
		b0, t0, rerr := revlog.ReadStaged(dir, 0)
		switch {
		case rerr != nil:
			if err := rc.plan.chainWrite(func() error { return revlog.Stage(dir, 0, curBody, curTest) }); err != nil {
				return "", err
			}
		case revlog.Hash(b0) != revlog.Hash(curBody) || revlog.Hash(t0) != revlog.Hash(curTest):
			return "", errPlanChanged()
		}
	}
	if err := rc.plan.chainWrite(func() error { return revlog.Stage(dir, n, prop.Body, newTest) }); err != nil {
		return "", err
	}
	if err := rc.revStep(2); err != nil {
		return "", err
	}
	in := revlog.Record{Kind: revlog.KindIntent, N: n, Trigger: trigger, BaseSHA256: revlog.Hash(curBody), BodySHA256: prop.BodySHA256,
		TestSHA256Before: l.TestSHA256, TestSHA256After: prop.TestSHA256, Changed: prop.Changed, Assumptions: rc.scrubAll(prop.Assumptions),
		Model: prop.Model, At: rc.stamp()}
	if err := rc.plan.chainWrite(func() error { return revlog.Append(dir, in) }); err != nil {
		return "", err
	}
	// The intent is open from here: planIntact must accept this node's staged
	// body while step 6 and step 7 are apart (Plan.accepts).
	rc.openTip(l, in)
	if err := rc.revStep(3); err != nil {
		return "", err
	}
	return rc.rollForward(ctx, l, dir, in)
}

// openTip records the open intent on the in-memory chain tip of the node, as
// LoadPlan does for an intent it finds, so Plan.accepts allows the staged body.
func (rc *runCtx) openTip(l *Leaf, in revlog.Record) {
	rc.plan.revMu.Lock() // read and write in one critical section: no other writer can slip between them
	defer rc.plan.revMu.Unlock()
	t, ok := rc.plan.Revised[l.ID]
	if !ok {
		t = RevisedTip{Key: rc.plan.nodeKey(l.ID), BodySHA256: in.BaseSHA256, TestSHA256: in.TestSHA256Before}
	}
	rec := in
	t.Open = &rec
	if rc.plan.Revised == nil {
		rc.plan.Revised = map[string]RevisedTip{}
	}
	rc.plan.Revised[l.ID] = t
}

// closeTip undoes openTip when the intent is refused.
func (rc *runCtx) closeTip(l *Leaf) {
	rc.plan.revMu.Lock()
	defer rc.plan.revMu.Unlock()
	if t, ok := rc.plan.Revised[l.ID]; ok {
		if t.N == 0 {
			delete(rc.plan.Revised, l.ID)
		} else {
			t.Open = nil
			rc.plan.Revised[l.ID] = t
		}
	}
}

// redCheckRevision runs the new test against the stub, as the Wave 0 red check
// does, and says why it is not red: a pass is a vacuous test, a build or vet
// failure a broken one.
func (rc *runCtx) redCheckRevision(ctx context.Context, l *Leaf) (string, error) {
	v := rc.leafCheck(ctx, l, []string{l.StubFile})
	switch {
	case v.Faulted():
		return "", faultOf(v, "the red check of the revised test of "+l.ID)
	case v.Pass():
		return planner.ClassVacuousTest, nil
	case v.Class == runner.ClassBuild || v.Class == runner.ClassVet:
		return planner.ClassRedBuild, nil
	}
	return "", nil
}

// rollForward is steps 4 to 8 of an open intent. Every step checks whether its
// target already has the "after" hash and skips if so, so a recovery that
// replays it converges to the same files. No model is called.
func (rc *runCtx) rollForward(ctx context.Context, l *Leaf, dir string, in revlog.Record) (string, error) {
	body, test, err := revlog.ReadStaged(dir, in.N)
	if err != nil || revlog.Hash(body) != in.BodySHA256 || revlog.Hash(test) != in.TestSHA256After {
		return "", errPlanChanged()
	}
	_, oldTest, err := revlog.ReadStaged(dir, in.N-1)
	if err != nil || revlog.Hash(oldTest) != in.TestSHA256Before {
		return "", errPlanChanged()
	}
	testAbs, err := pathsafe.ResolveTest(rc.o.Repo, l.TestFile)
	if err != nil {
		return "", fmt.Errorf("executor: leaf %s: its test path is not allowed", l.ID)
	}

	// Step 4: place the new test, prove it red against the stub.
	if in.TestSHA256After != in.TestSHA256Before {
		cur, rerr := readNoFollow(testAbs)
		if rerr != nil {
			return "", fmt.Errorf("executor: the test file of leaf %s is not readable", l.ID)
		}
		switch revlog.Hash(cur) {
		case in.TestSHA256After:
		case in.TestSHA256Before:
			if perr := rc.plan.chainWrite(func() error { return pathsafe.ReplaceTest(rc.o.Repo, l.TestFile, test) }); perr != nil {
				return "", fmt.Errorf("executor: the revised test of leaf %s could not be written", l.ID)
			}
		default:
			return "", &stopError{Status: "failed", Reason: "test_file_changed",
				Message: fmt.Sprintf("executor: the test file of leaf %s changed since it was planned", l.ID)}
		}
		class, cerr := rc.redCheckRevision(ctx, l)
		if cerr != nil {
			return "", cerr
		}
		if class != "" {
			return rc.abortRevision(l, dir, in, oldTest, class)
		}
	}
	if err := rc.revStep(4); err != nil {
		return "", err
	}

	// Step 5: leaf_tests.json.
	if err := rc.plan.chainWrite(func() error { return planner.SetLeafTestSHA(rc.o.RunDir, l.ID, in.TestSHA256After) }); err != nil {
		return "", fmt.Errorf("executor: leaf_tests.json could not be updated for %s", l.ID)
	}
	if err := rc.revStep(5); err != nil {
		return "", err
	}

	// Step 6: the node file.
	rel := strings.TrimPrefix(rc.plan.nodeKey(l.ID), "tree/")
	cur, err := readPlanFile(rc.o.RunDir, rel)
	if err != nil {
		return "", err
	}
	switch revlog.Hash(cur) {
	case in.BodySHA256:
	case in.BaseSHA256:
		node, perr := tree.ParseNode(body)
		if perr != nil {
			return "", errPlanChanged()
		}
		if werr := rc.plan.chainWrite(func() error { return tree.NewStore(rc.o.RunDir).Write(node) }); werr != nil {
			return "", fmt.Errorf("executor: the revised node %s could not be written", l.ID)
		}
		if written, rerr := readPlanFile(rc.o.RunDir, rel); rerr != nil || revlog.Hash(written) != in.BodySHA256 {
			return "", fmt.Errorf("executor: the revised node %s was not written as staged", l.ID)
		}
	default:
		return "", errPlanChanged()
	}
	if err := rc.revStep(6); err != nil {
		return "", err
	}

	// Step 7: the commit record, the runtime revision, the repository commit.
	commit := in
	commit.Kind, commit.At, commit.Round = revlog.KindCommit, rc.stamp(), rc.round(l.ID)
	if err := rc.plan.chainWrite(func() error { return revlog.Append(dir, commit) }); err != nil {
		return "", err
	}
	if err := rc.revStep(7); err != nil { // 7a: the commit record is on disk
		return "", err
	}
	// 7b (the repository commit, hook 8) and the reload are part of finishCommitted.
	if err := rc.finishCommitted(ctx, l, commit, true); err != nil {
		return "", err
	}
	return "", nil
}

// abortRevision undoes the part of a transaction that a rejected test touched:
// the old test file goes back, a refused record closes the intent and the staged
// files of that revision are removed.
func (rc *runCtx) abortRevision(l *Leaf, dir string, in revlog.Record, oldTest []byte, class string) (string, error) {
	// The in-memory open intent is cleared on every way out once the refused
	// record is on disk. If that record could not be written the intent is still
	// open on disk, so the memory keeps agreeing with the disk and the tip stays open.
	closed := false
	defer func() {
		if closed {
			rc.closeTip(l)
		}
	}()
	testAbs, err := pathsafe.ResolveTest(rc.o.Repo, l.TestFile)
	if err != nil {
		return "", fmt.Errorf("executor: leaf %s: its test path is not allowed", l.ID)
	}
	if cur, rerr := readNoFollow(testAbs); rerr != nil || revlog.Hash(cur) != in.TestSHA256Before {
		if perr := rc.plan.chainWrite(func() error { return pathsafe.ReplaceTest(rc.o.Repo, l.TestFile, oldTest) }); perr != nil {
			return "", fmt.Errorf("executor: the test file of leaf %s could not be restored", l.ID)
		}
	}
	refused := revlog.Record{Kind: revlog.KindRefused, N: in.N, Class: class, Trigger: in.Trigger, At: rc.stamp()}
	if err := rc.plan.chainWrite(func() error { return revlog.Append(dir, refused) }); err != nil {
		return "", err
	}
	closed = true
	if err := rc.plan.chainWrite(func() error { return revlog.RemoveStaged(dir, in.N) }); err != nil {
		return "", err
	}
	rc.emit("revise_refused", l.ID, class)
	return class, nil
}

// finishCommitted does everything that follows a commit record and is safe to
// repeat. The order is fixed (ruling R8): the chain tip in memory first, then
// planIntact (the spec requires it before every commit), then the runtime
// revision, the repository commit of a rewritten test, the escalation count and
// the reloaded leaf. The revision numbers here are the node's (in.N), never the
// row counter, which the notes rung and a repair round also raise. The
// escalation is keyed by the commit record (its N and Round), never by the
// current round, so a recovery after a human retry does not count it again.
func (rc *runCtx) finishCommitted(ctx context.Context, l *Leaf, in revlog.Record, announce bool) error {
	wc := context.WithoutCancel(ctx)
	rc.plan.setTip(l.ID, RevisedTip{Key: rc.plan.nodeKey(l.ID), N: in.N, BodySHA256: in.BodySHA256, TestSHA256: in.TestSHA256After})
	if se := rc.planIntact(); se != nil {
		return se
	}
	row, err := rc.o.Board.Get(wc, rc.plan.RunID, l.ID)
	if err != nil {
		return fmt.Errorf("executor: reading the row of %s failed", l.ID)
	}
	if row.Revision < in.N {
		if err := rc.o.Board.SetRevision(wc, rc.plan.RunID, l.ID, in.N); err != nil {
			return fmt.Errorf("executor: raising the revision of %s failed", l.ID)
		}
	}
	if in.TestSHA256After != in.TestSHA256Before && !rc.diffOnly {
		dirty, derr := rc.git.Dirty()
		if derr != nil {
			return errors.New("executor: the repository state could not be read")
		}
		for _, p := range dirty {
			if p != l.TestFile {
				continue
			}
			abs, rerr := pathsafe.ResolveTest(rc.o.Repo, l.TestFile)
			if rerr != nil {
				return fmt.Errorf("executor: leaf %s: its test path is not allowed", l.ID)
			}
			cur, rerr := readNoFollow(abs)
			if rerr != nil || revlog.Hash(cur) != in.TestSHA256After {
				return &stopError{Status: "failed", Reason: "test_file_changed",
					Message: fmt.Sprintf("executor: the test file of leaf %s changed since it was planned", l.ID)}
			}
			if _, cerr := rc.git.CommitRevision(l.ID, in.N, []string{l.TestFile}); cerr != nil && !errors.Is(cerr, gitland.ErrNothingToCommit) {
				return fmt.Errorf("executor: committing the revised test of %s failed", l.ID)
			}
			if terr := rc.recordTip(); terr != nil {
				return terr
			}
			break
		}
	}
	if err := rc.revStep(8); err != nil { // 7b: the repository commit is made
		return err
	}
	if err := AppendEscalation(rc.o.RunDir, report.Escalation{Kind: "revision", TaskType: "revise", Model: in.Model, NodeID: l.ID, Revision: in.N - 1, Round: in.Round}); err != nil {
		return err
	}
	if err := rc.plan.ReloadLeaf(l.ID); err != nil {
		return err
	}
	if announce {
		rc.emit("node_revised", l.ID, fmt.Sprintf("revision %d: %s", in.N, strings.Join(in.Changed, ", ")))
	}
	return nil
}
