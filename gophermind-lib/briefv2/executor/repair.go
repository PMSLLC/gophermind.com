package executor

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/runner"
)

// integrationKind is the word of the escalation reason for a failing check:
// build, vet or test. When several steps failed, build wins over vet and vet
// over test.
func integrationKind(res checkResult) string {
	kind := "test"
	for _, f := range res.Failures {
		switch f.Kind {
		case runner.ClassBuild:
			return "build"
		case runner.ClassVet:
			kind = "vet"
		}
	}
	return kind
}

// reopen is the explicit act that puts a verified leaf back to work after an
// integration failure named it: verified -> needs_revision -> ready, the row's
// revision plus one, and the leaf's revision allowance plus one, so a repair
// never spends the allowance the leaf started with. The reason is recorded in
// the leaf results and in a reopened event. Only a verified leaf is reopened.
func (rc *runCtx) reopen(ctx context.Context, l *Leaf, reason string) error {
	wc := context.WithoutCancel(ctx)
	runID := rc.plan.RunID
	row, err := rc.o.Board.Get(wc, runID, l.ID)
	if err != nil {
		return fmt.Errorf("executor: reading the row of %s failed", l.ID)
	}
	if row.Status != blackboard.StatusVerified {
		return fmt.Errorf("executor: leaf %s is %s, not verified: only a verified leaf is reopened", l.ID, row.Status)
	}
	for _, step := range [][2]blackboard.Status{
		{blackboard.StatusVerified, blackboard.StatusNeedsRevision},
		{blackboard.StatusNeedsRevision, blackboard.StatusReady},
	} {
		if err := rc.o.Board.SetStatus(wc, runID, l.ID, step[0], step[1]); err != nil {
			return fmt.Errorf("executor: reopening %s did not work", l.ID)
		}
	}
	if err := rc.o.Board.SetRevision(wc, runID, l.ID, row.Revision+1); err != nil {
		return fmt.Errorf("executor: raising the revision of %s failed", l.ID)
	}
	stateMu.Lock()
	rc.state.ExtraRevisions[l.ID]++
	err = rc.state.Save(rc.o.RunDir)
	stateMu.Unlock()
	if err != nil {
		return err
	}
	if err := rc.setResult(l.ID, blackboard.StatusNeedsRevision, reason); err != nil {
		return err
	}
	repMu.Lock()
	rc.rep.repairs++
	repMu.Unlock()
	rc.emit("reopened", l.ID, reason)
	return nil
}

// repairStop is a stop of the repair loop that names the leaf.
func repairStop(status, reason, format string, args ...any) *stopError {
	return &stopError{Status: status, Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// repairWave is the repair loop of spec 8.3. It is entered only with a failure
// that names at least one leaf (runWave stops on an unattributable one). For
// each round, up to executor.repair_rounds plus the rounds a person granted
// for this wave, it reopens exactly the attributed leaves, runs each back
// through the leaf loop with the integration failure lines as the previous
// failure, and checks the wave again. A pass returns nil. A failure no leaf
// owns stops the run (R10). When the rounds are used up each attributed leaf
// is escalated to a person (the same gate as a leaf's own ladder): retry grants
// one more round, stop and a missing or waiting gate stop the run, and skip
// ends it failed (S13), because the failing code is committed and every later
// wave would blame a terminal leaf. Nothing here papers over a failure: the
// run never continues past an unrepaired wave.
func (rc *runCtx) repairWave(ctx context.Context, w int, res checkResult, final bool) (*stopError, error) {
	wkey := strconv.Itoa(w)
	for round := 1; ; {
		stateMu.Lock()
		bound := rc.cfg.Executor.RepairRounds + rc.state.ExtraRepair[wkey]
		stateMu.Unlock()
		for ; round <= bound; round++ {
			att := rc.plan.Attribute(res)
			if len(att.Unattributed) > 0 {
				return unattributableStop(w, att.Unattributed), nil
			}
			kind := integrationKind(res)
			for _, id := range att.Nodes {
				l := rc.plan.Leaf(id)
				if l == nil {
					return nil, fmt.Errorf("executor: attribution named an unknown leaf")
				}
				row, err := rc.o.Board.Get(ctx, rc.plan.RunID, id)
				if err != nil {
					return rc.faultOrStop(fmt.Errorf("executor: reading the row of %s failed", id))
				}
				switch row.Status {
				case blackboard.StatusVerified:
					if err := rc.reopen(ctx, l, "integration: "+kind); err != nil {
						return rc.faultOrStop(err)
					}
				case blackboard.StatusReady:
					// a person chose retry at the gate: the leaf is already reopened
				default:
					return repairStop("failed", "repair_impossible",
						"executor: wave %d failed its integration checks in the files of leaf %s, which is %s and cannot be repaired", w, id, row.Status), nil
				}
				lines := append([]string{"integration check (" + kind + ") failed in this leaf's files:"}, att.Lines[id]...)
				prev := packer.NewFailure(nil, strings.Join(lines, "\n"), rc.secretValues())
				out, err := rc.runLeaf(ctx, l, leafIn{Previous: prev, Repair: round})
				if err != nil {
					return rc.faultOrStop(err)
				}
				switch {
				case out.Interrupted:
					return interruptStop(out.Reason), nil
				case out.Status == blackboard.StatusEscalated:
					return humanStop(id), nil
				case out.Status != blackboard.StatusVerified:
					return repairStop("failed", "repair_failed", "executor: leaf %s could not be repaired in round %d (%s)", id, round, out.Reason), nil
				}
			}
			var err error
			if res, err = rc.waveChecks(ctx, w, final); err != nil {
				return rc.faultOrStop(err)
			}
			if res.Pass() {
				return nil, nil
			}
			if un := rc.plan.Attribute(res).Unattributed; len(un) > 0 {
				return unattributableStop(w, un), nil
			}
		}

		att := rc.plan.Attribute(res)
		kind := integrationKind(res)
		for _, id := range att.Nodes {
			l := rc.plan.Leaf(id)
			row, err := rc.o.Board.Get(ctx, rc.plan.RunID, id)
			if err != nil {
				return rc.faultOrStop(fmt.Errorf("executor: reading the row of %s failed", id))
			}
			if row.Status != blackboard.StatusVerified {
				return repairStop("failed", "repair_impossible",
					"executor: wave %d still fails its integration checks in the files of leaf %s, which is %s", w, id, row.Status), nil
			}
			history := []string{fmt.Sprintf("integration %s check still failing after %d repair rounds", kind, bound)}
			for _, ln := range att.Lines[id] {
				history = append(history, "at "+ln)
			}
			r, err := rc.escalate(ctx, l, blackboard.StatusVerified, "integration: "+kind, history)
			if err != nil {
				if ctx.Err() != nil {
					if _, ok := stopOf(err); !ok {
						return interruptStop(""), nil
					}
				}
				return rc.faultOrStop(err)
			}
			if r.Action == human.ActionSkip {
				return repairStop("failed", "integration_skipped",
					"executor: wave %d fails its integration checks in leaf %s and a person skipped it; the run stops because its failing code is committed", w, id), nil
			}
		}
		stateMu.Lock()
		rc.state.ExtraRepair[wkey]++
		err := rc.state.Save(rc.o.RunDir)
		stateMu.Unlock()
		if err != nil {
			return nil, err
		}
	}
}
