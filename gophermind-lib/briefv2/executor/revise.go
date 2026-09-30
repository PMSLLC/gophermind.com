package executor

import (
	"context"
	"errors"
	"fmt"

	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/runner"
)

// reviseMaxTokens caps the reply of a revise call: at most 5 notes of 200
// bytes as JSON.
const reviseMaxTokens = 1024

// reviseStop is how the revise rung ends without hints and without a contract
// problem: the ladder cannot continue. Interrupted means nothing was wrong
// with the leaf (the context ended, or no provider could answer): the claim is
// released and the run stops. Otherwise the leaf escalates with Reason, a class.
type reviseStop struct {
	Reason      string
	Interrupted bool
}

func (e *reviseStop) Error() string {
	return "executor: the revise call produced no usable notes (" + e.Reason + ")"
}

// revise is rung 5: one strong-tier call that reads the contract slice, the
// test file and the attempt history (classes and test names only) and answers
// with up to 5 hints, or CONTRACT_PROBLEM. The hints go to the notes overlay,
// the revision counter moves up by one and the escalation is counted. A
// CONTRACT_PROBLEM comes back as contractProblem. Contracts and node files are
// never written. The reply text is read and dropped: hints are the one thing
// kept, and only in _state/notes.json.
func (lr *leafRun) revise(ctx context.Context) (contractProblem bool, err error) {
	rc, l := lr.rc, lr.l
	testSrc, err := lr.testSource()
	if err != nil {
		return false, err
	}
	in := packer.Inputs{Budget: rc.contextBudget(l), Secrets: rc.secretValues()}
	packed, err := packer.PackRevise(rc.plan.View(l, testSrc), rc.plan.Contracts, lr.history, in)
	var floor *packer.ErrFloorOverBudget
	if errors.As(err, &floor) {
		return false, &reviseStop{Reason: ClassContextTooLong}
	}
	if err != nil {
		return false, fmt.Errorf("executor: leaf %s: the revise prompt could not be built: %w", l.ID, err)
	}

	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: packer.SystemPrefix + "revise:" + l.ID},
			{Role: provider.RoleUser, Content: packed.Text},
		},
		MaxTokens: reviseMaxTokens,
	}
	info := router.CallInfo{
		RunID: rc.plan.RunID, Stage: "revise:" + l.ID, NodeID: l.ID, Tier: router.TierStrong, Scope: router.ScopeNode,
		Revision: lr.rev, TaskType: "revise", NodeClass: l.Class,
	}
	var notes []string
	var problem string
	parse := func(text string) error {
		n, p, perr := packer.ParseRevise(text)
		notes, problem = n, p
		return perr
	}
	res, cerr := rc.o.Caller.CallParsed(ctx, info, req, parse)
	if cerr != nil {
		return false, lr.reviseFailure(ctx, cerr)
	}
	if problem != "" {
		lr.problem = problem
		return true, nil
	}

	for _, n := range notes {
		if err := AddNote(rc.o.RunDir, l.ID, n); err != nil {
			return false, err
		}
	}
	wc := context.WithoutCancel(ctx)
	if err := rc.o.Board.SetRevision(wc, rc.plan.RunID, l.ID, lr.rev+1); err != nil {
		return false, fmt.Errorf("executor: raising the revision of %s failed", l.ID)
	}
	lr.rev++
	if err := AppendEscalation(rc.o.RunDir, report.Escalation{Kind: "revision", TaskType: "revise", Model: res.Entry, NodeID: l.ID}); err != nil {
		return false, err
	}
	return false, nil
}

// reviseFailure says what a failed revise call means for the leaf.
func (lr *leafRun) reviseFailure(ctx context.Context, cerr error) error {
	var ce *router.ChainExhausted
	if ctx.Err() != nil || errors.Is(cerr, context.Canceled) {
		return &reviseStop{Interrupted: true}
	}
	if !errors.As(cerr, &ce) {
		return fmt.Errorf("executor: the revise call for leaf %s failed: %w", lr.l.ID, cerr)
	}
	switch {
	case ce.ParseErr != nil:
		return &reviseStop{Reason: runner.ClassMalformed}
	case len(ce.Reasons) > 0:
		switch kind := ce.Reasons[0].Kind; kind {
		case router.ReasonTooLong:
			return &reviseStop{Reason: ClassContextTooLong}
		case router.ReasonCooldown, router.ReasonFailed:
			return &reviseStop{Reason: "provider_unavailable", Interrupted: true}
		default:
			return &reviseStop{Reason: kind, Interrupted: true}
		}
	}
	return &reviseStop{Reason: "no_model", Interrupted: true}
}
