package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/human"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/settings"
)

func TestEscalateWithGivesTheGateTheRewritesAndAScrubbedProposal(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(2))
	rc, _ := g.leafRC(t, Script{}, true)
	ctx := context.Background()
	l := rc.plan.Leaf(leafID)
	if ok, err := g.board.Claim(ctx, g.id, leafID, "w"); err != nil || !ok {
		t.Fatal("claim", err)
	}
	if err := g.board.SetStatus(ctx, g.id, leafID, blackboard.StatusClaimed, blackboard.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := g.board.SetRevision(ctx, g.id, leafID, 5); err != nil {
		t.Fatal(err)
	}
	rc.cfg.Revise.Mode = settings.ReviseModeRewrite
	l.Revision = 2 // the node revision; the row counter below is deliberately different
	_, err := rc.escalateWith(ctx, l, blackboard.StatusInProgress, "contract_flagged", []string{"attempt 1 a/m1 fail test_fail"},
		escalateExtra{Proposal: "change the signature " + canarySecret})
	asStop(t, err, "escalated", "human_stop")
	seen := g.gate.Escalations()
	if len(seen) != 1 || seen[0].Reason != "contract_flagged" || seen[0].Revisions != 2 {
		t.Fatalf("gate saw %+v", seen)
	}
	if strings.Contains(seen[0].Proposal, canarySecret) || seen[0].Proposal == "" {
		t.Fatalf("proposal = %q, want it scrubbed", seen[0].Proposal)
	}
	// In notes mode the gate is told no rewrites.
	rc.cfg.Revise.Mode = settings.ReviseModeNotes
	_, _ = rc.escalate(ctx, l, blackboard.StatusEscalated, "test_fail", nil)
	if seen = g.gate.Escalations(); len(seen) != 2 || seen[1].Revisions != 0 {
		t.Fatalf("gate saw %+v", seen)
	}
}

// decisionErrReviser fails RecordDecision, as a full disk would.
type decisionErrReviser struct{ fakeReviser }

var errDecisionStore = errors.New("test: the decision store could not be written")

func (*decisionErrReviser) RecordDecision(planner.RevisionDecision) (string, error) {
	return "", errDecisionStore
}

// A decision about a revise-stage leaf that cannot be recorded aborts the
// escalation: the error comes back and the answer (here a retry) is not applied.
func TestEscalateWithAbortsWhenTheDecisionCannotBeRecorded(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	rc, _ := g.leafRC(t, Script{}, true)
	ctx := context.Background()
	l := rc.plan.Leaf(leafID)
	if ok, err := g.board.Claim(ctx, g.id, leafID, "w"); err != nil || !ok {
		t.Fatal("claim", err)
	}
	if err := g.board.SetStatus(ctx, g.id, leafID, blackboard.StatusClaimed, blackboard.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	g.gate.by = human.AnsweredByHuman
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "try again"}}
	rc.rev = &decisionErrReviser{}
	res, err := rc.escalateWith(ctx, l, blackboard.StatusInProgress, planner.ClassContractFlagged, nil, escalateExtra{})
	if !errors.Is(err, errDecisionStore) {
		t.Fatalf("escalateWith = %+v, %v; want the decision store error", res, err)
	}
	if res.Action != "" {
		t.Fatalf("a resolution came back: %+v", res)
	}
	if rc.round(leafID) != 0 {
		t.Fatal("the retry was applied: the revision allowance was raised")
	}
	if r := g.row(t, leafID); r.Status != blackboard.StatusEscalated {
		t.Fatalf("status = %s, want escalated (the retry must not reopen the leaf)", r.Status)
	}
	if notes, _ := LoadNotes(g.runDir); len(notes[leafID]) != 0 {
		t.Fatalf("the retry note was saved: %v", notes[leafID])
	}
}
