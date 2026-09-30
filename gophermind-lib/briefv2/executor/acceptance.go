package executor

// Task 12b stub. Task 13 replaces this whole file with the acceptance stage
// (built binary, root tests, proof file, repair loop) and keeps the names below.
// Until then nothing can reach `verified` without runFlags.skipAcceptance, which
// only the package tests set.

import (
	"context"
	"errors"
	"fmt"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/report"
)

// accResult is what the acceptance stage proved.
type accResult struct{ Passed, Total, ConstraintsPassed, ConstraintsTotal int }

// finishResult is how the run ended once every wave was done: the final status,
// a reason class, the acceptance counts, extra failure lines (acceptance
// bullets, never output text) and the landing.
type finishResult struct {
	Status, Reason string
	Acc            accResult
	Failures       []string
	Landing        *report.Landing
}

// decide is the one place the final status is decided: verified only when every
// leaf row is verified. A row that is not verified, or cannot be read, never
// yields verified: escalated when a leaf is escalated, else failed with the
// first reason.
func (rc *runCtx) decide() (status, reason string) {
	ctx := context.Background()
	escalated, first := false, ""
	for _, l := range rc.plan.Leaves {
		row, err := rc.o.Board.Get(ctx, rc.plan.RunID, l.ID)
		if err != nil {
			return "failed", "state_unreadable"
		}
		if row.Status == blackboard.StatusVerified {
			continue
		}
		if row.Status == blackboard.StatusEscalated {
			escalated = true
		}
		if first == "" {
			repMu.Lock()
			first = rc.rep.reasons[l.ID]
			repMu.Unlock()
			if first == "" {
				first = "leaves_not_verified"
			}
		}
	}
	switch {
	case escalated:
		return "escalated", "human_stop"
	case first != "":
		return "failed", first
	}
	return "verified", ""
}

// finish decides the final status and lands the work. The stub refuses
// `verified` (failed, acceptance_pending) unless the package tests skip
// acceptance; Task 13 replaces it. A *stopError or a cancelled context from a
// step becomes the result; any other error is a harness fault.
func (rc *runCtx) finish(ctx context.Context, f runFlags) (finishResult, error) {
	status, reason := rc.decide()
	if status != "verified" {
		return finishResult{Status: status, Reason: reason}, nil
	}
	if !f.skipAcceptance {
		return finishResult{Status: "failed", Reason: "acceptance_pending"}, nil
	}
	if err := rc.verifyModules(ctx); err != nil {
		return rc.finishFromError(ctx, err)
	}
	acc := accResult{}
	landing, stop, err := rc.land(ctx, acc)
	if err != nil {
		return rc.finishFromError(ctx, err)
	}
	if stop != nil {
		return finishResult{Status: stop.Status, Reason: stop.Reason}, nil
	}
	return finishResult{Status: "verified", Acc: acc, Landing: landing}, nil
}

// finishFromError turns the error of a finishing step into a result when it is
// a stop or a cancelled context, and leaves a fault as an error.
func (rc *runCtx) finishFromError(ctx context.Context, err error) (finishResult, error) {
	if se, ok := stopOf(err); ok {
		return finishResult{Status: se.Status, Reason: se.Reason}, nil
	}
	if ctx.Err() != nil {
		return finishResult{Status: "interrupted", Reason: "cancelled"}, nil
	}
	return finishResult{}, err
}

// land ends a verified run. diff_only writes <run>/changes.patch (the working
// tree against the base) and commits nothing; otherwise the empty final commit
// on the work branch and a fast-forward of the base through gitland, the only
// git calls of the package. A failure is failed/landing_blocked with the work
// branch left as it is.
func (rc *runCtx) land(ctx context.Context, acc accResult) (*report.Landing, *stopError, error) {
	if se := rc.planIntact(); se != nil {
		return nil, se, nil
	}
	blocked := func(msg string) (*report.Landing, *stopError, error) {
		rc.emit("landing", "", "landing_blocked")
		return nil, &stopError{Status: "failed", Reason: "landing_blocked", Message: "executor: " + msg}, nil
	}
	if rc.diffOnly {
		patch, err := rc.git.Diff(rc.baseBranch())
		if err != nil {
			return blocked("the patch could not be made")
		}
		if err := report.WriteFile(rc.o.RunDir, "changes.patch", patch, report.WriteOptions{RepoRoot: rc.o.Repo}); err != nil {
			return blocked("changes.patch could not be written")
		}
		rc.emit("landing", "", "diff_only: changes.patch")
		return nil, nil, nil
	}
	msg := fmt.Sprintf("%s built, Acceptance passed %d of %d", rc.plan.Brief.Front.Title, acc.Passed, acc.Total)
	hash, err := rc.git.Finish(msg)
	if err != nil {
		if errors.Is(err, gitland.ErrLandingBlocked) {
			return blocked("landing is blocked: the base branch moved, and the work branch is left as it is")
		}
		return blocked("landing failed, and the work branch is left as it is")
	}
	rc.emit("landing", "", hash)
	return &report.Landing{Branch: rc.state.Branch, Commit: hash, MergedInto: rc.baseBranch()}, nil, nil
}
