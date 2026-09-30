package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/report"
)

// reasonSkipped is the reason of a leaf a person chose to skip.
const reasonSkipped = "skipped by human"

// humanStop is the run ending at the human gate: a stop answer, no gate, or a
// gate that failed. Spec 7.4 and R14: no answer means stop.
func humanStop(id string) *stopError {
	return &stopError{Status: "escalated", Reason: "human_stop",
		Message: fmt.Sprintf("executor: leaf %s is escalated and the run stopped at the human gate", id)}
}

// waitingOnHuman is the run ending because the gate has no answer yet (the
// file gate, ruling S8): exit 3, the leaf stays escalated and the next run asks again.
func waitingOnHuman(id string) *stopError {
	return &stopError{Status: "escalated", Reason: "waiting_on_human",
		Message: fmt.Sprintf("executor: leaf %s is escalated and waits for an answer at the human gate", id)}
}

// scrubHistory drops the history lines that hold a secret value: the gate is
// shown ids, a class and class-and-name lines, and never a secret.
func (rc *runCtx) scrubHistory(lines []string) []string {
	secrets := rc.secretValues()
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		clean := true
		for _, s := range secrets {
			if s != "" && strings.Contains(ln, s) {
				clean = false
				break
			}
		}
		if clean {
			out = append(out, ln)
		}
	}
	return out
}

// rowRevision is the revision of the leaf's row, 0 when it cannot be read.
func (rc *runCtx) rowRevision(ctx context.Context, id string) int {
	row, err := rc.o.Board.Get(ctx, rc.plan.RunID, id)
	if err != nil {
		return 0
	}
	return row.Revision
}

// round is how many retries a person has granted the leaf so far.
func (rc *runCtx) round(id string) int {
	stateMu.Lock()
	defer stateMu.Unlock()
	return rc.state.ExtraRevisions[id]
}

// lastModel is the provider/model of the last attempt of the leaf, "" when it
// has none.
func (rc *runCtx) lastModel(ctx context.Context, id string) string {
	row, err := rc.o.Board.Get(ctx, rc.plan.RunID, id)
	if err != nil || len(row.Attempts) == 0 {
		return ""
	}
	a := row.Attempts[len(row.Attempts)-1]
	return a.Provider + "/" + a.Model
}

// escalate is rung 7: the leaf leaves status from (in_progress for the ladder,
// verified for an integration failure, escalated when a resumed run asks
// again) for needs_revision and escalated, the person is asked, and the answer
// is applied to the blackboard:
//
//	retry  note saved, revision allowance raised by one, escalated -> ready
//	skip   escalated -> failed, reason "skipped by human"
//	stop   a *stopError (human_stop), also for a nil gate or a gate that fails
//
// A gate with no answer yet (human.ErrWaiting) is a *stopError with reason
// waiting_on_human and the leaf stays escalated. A context that ends while the
// gate waits comes back as the context's error, the leaf stays escalated. The
// gate is given the id, the reason class and one line per attempt: never a
// reply, command output or a secret value.
func (rc *runCtx) escalate(ctx context.Context, l *Leaf, from blackboard.Status, reason string, history []string) (human.Resolution, error) {
	runID, wc := rc.plan.RunID, context.WithoutCancel(ctx)
	if from != blackboard.StatusEscalated {
		if from != blackboard.StatusNeedsRevision {
			if err := rc.o.Board.SetStatus(wc, runID, l.ID, from, blackboard.StatusNeedsRevision); err != nil {
				return human.Resolution{}, fmt.Errorf("executor: escalating %s did not work", l.ID)
			}
		}
		if err := rc.o.Board.SetStatus(wc, runID, l.ID, blackboard.StatusNeedsRevision, blackboard.StatusEscalated); err != nil {
			return human.Resolution{}, fmt.Errorf("executor: escalating %s did not work", l.ID)
		}
	}
	rec := report.Escalation{Kind: "human", TaskType: "implement", Model: rc.lastModel(wc, l.ID), NodeID: l.ID, Revision: rc.rowRevision(wc, l.ID), Round: rc.round(l.ID)}
	if rc.o.Gate == nil {
		rec.AnsweredBy = human.AnsweredByGateAbsent
	}
	if err := AppendEscalation(rc.o.RunDir, rec); err != nil {
		return human.Resolution{}, err
	}
	if err := rc.setResult(l.ID, blackboard.StatusEscalated, reason); err != nil {
		return human.Resolution{}, err
	}
	rc.emit("escalated", l.ID, reason)

	if rc.o.Gate == nil {
		return human.Resolution{}, humanStop(l.ID)
	}
	res, err := rc.o.Gate.Escalate(ctx, human.Escalation{NodeID: l.ID, Reason: reason, History: rc.scrubHistory(history)})
	if err == nil {
		by := res.AnsweredBy
		if !human.ValidAnsweredBy(by) {
			by = human.AnsweredByProgrammatic // a gate that does not say is not a person
		}
		res.AnsweredBy = by
		rec.AnsweredBy = by
		if aerr := AppendEscalation(rc.o.RunDir, rec); aerr != nil {
			return human.Resolution{}, aerr
		}
	}
	switch {
	case errors.Is(err, human.ErrWaiting):
		return human.Resolution{}, waitingOnHuman(l.ID)
	case err != nil && ctx.Err() != nil:
		return human.Resolution{}, ctx.Err()
	case err != nil:
		return human.Resolution{}, humanStop(l.ID)
	}

	switch res.Action {
	case human.ActionRetry:
		if note := strings.TrimSpace(res.Note); note != "" {
			if err := AddNote(rc.o.RunDir, l.ID, note); err != nil {
				return res, err
			}
		}
		stateMu.Lock()
		rc.state.ExtraRevisions[l.ID]++
		err := rc.state.Save(rc.o.RunDir)
		stateMu.Unlock()
		if err != nil {
			return res, err
		}
		if err := rc.o.Board.SetStatus(wc, runID, l.ID, blackboard.StatusEscalated, blackboard.StatusReady); err != nil {
			return res, fmt.Errorf("executor: re-opening %s did not work", l.ID)
		}
		return res, nil
	case human.ActionSkip:
		if err := rc.o.Board.SetStatus(wc, runID, l.ID, blackboard.StatusEscalated, blackboard.StatusFailed); err != nil {
			return res, fmt.Errorf("executor: skipping %s did not work", l.ID)
		}
		if err := rc.setResult(l.ID, blackboard.StatusFailed, reasonSkipped); err != nil {
			return res, err
		}
		return res, nil
	}
	return human.Resolution{}, humanStop(l.ID)
}
