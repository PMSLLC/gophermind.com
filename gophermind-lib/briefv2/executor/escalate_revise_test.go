package executor

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/settings"
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
