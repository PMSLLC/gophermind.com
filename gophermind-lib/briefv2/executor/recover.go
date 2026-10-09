package executor

import (
	"context"
	"sort"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/revlog"
)

// openRevisionPaths are the test files of every node that has a revision chain
// (ruling R1). A kill after the commit record and before the repository commit
// leaves the revised test dirty with no open intent, so an open intent alone is
// not enough: during a resume such a file is this run's own dirt.
func (rc *runCtx) openRevisionPaths() []string {
	rc.plan.revMu.Lock()
	defer rc.plan.revMu.Unlock()
	var out []string
	for id := range rc.plan.Revised {
		if l := rc.plan.byID[id]; l != nil {
			out = append(out, l.TestFile)
		}
	}
	sort.Strings(out)
	return out
}

// lastCommit is the commit record of revision n.
func lastCommit(recs []revlog.Record, n int) (revlog.Record, bool) {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Kind == revlog.KindCommit && recs[i].N == n {
			return recs[i], true
		}
	}
	return revlog.Record{}, false
}

// recoverRevisions finishes what a kill left half done (spec section 10).
// LoadPlan already proved every chain; this only rolls forward. An open intent
// goes through its remaining steps, each skipped when its target already has
// the "after" hash; a committed revision whose runtime revision, repository
// commit or in-memory state is behind is brought up to it. It calls no model.
func (rc *runCtx) recoverRevisions(ctx context.Context) error {
	rc.plan.revMu.Lock()
	tips := make(map[string]RevisedTip, len(rc.plan.Revised))
	for id, t := range rc.plan.Revised {
		tips[id] = t
	}
	rc.plan.revMu.Unlock()
	for _, l := range rc.plan.Leaves {
		tip, ok := tips[l.ID]
		if !ok {
			continue
		}
		dir, err := revlog.NodeDir(rc.o.RunDir, l.ID)
		if err != nil {
			return errPlanChanged()
		}
		if tip.Open != nil {
			refusal, rerr := rc.rollForward(ctx, l, dir, *tip.Open)
			if rerr != nil {
				return rerr
			}
			if refusal != "" {
				rc.emit("revise_recovered", l.ID, "the open revision was refused: "+refusal)
			}
			continue
		}
		if tip.N >= 1 {
			recs, rerr := revlog.Read(dir)
			if rerr != nil {
				return errPlanChanged()
			}
			c, found := lastCommit(recs, tip.N)
			if !found {
				return errPlanChanged()
			}
			if ferr := rc.finishCommitted(ctx, l, c, false); ferr != nil {
				return ferr
			}
		}
	}
	return nil
}

// checkRuntimeRevisions is the runtime cross-check, run after recovery. The
// row's revision is a shared counter (the notes rung and a repair round raise
// it too), so it can be above a node's chain length but never below it: a row
// behind its own chain is a harness fault with a fixed message.
func (rc *runCtx) checkRuntimeRevisions(ctx context.Context) error {
	rows, err := rc.o.Board.List(ctx, rc.plan.RunID, blackboard.Filter{})
	if err != nil {
		return errPlanChanged()
	}
	return rc.runtimeRevisionsOK(rows)
}

func (rc *runCtx) runtimeRevisionsOK(rows []blackboard.Row) error {
	for _, r := range rows {
		if rc.plan.Leaf(r.NodeID) == nil {
			continue
		}
		if t, ok := rc.plan.tip(r.NodeID); ok && r.Revision < t.N {
			return errPlanChanged()
		}
	}
	return nil
}
