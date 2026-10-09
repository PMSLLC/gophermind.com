package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/settings"
)

func TestApprovalShowsTheRevisionNoticeOutsideTheHash(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	g.mustPlan(planner.Options{})
	if len(gate.plans) != 1 || !strings.Contains(gate.plans[0].Notice, "rewritten by the planner") {
		t.Fatalf("plans = %+v", gate.plans)
	}
	_, hash, err := planner.RenderPlan(g.runDir)
	if err != nil || hash != gate.plans[0].Hash {
		t.Fatalf("the shown hash %s is not the plan hash %s (%v)", gate.plans[0].Hash, hash, err)
	}
	if strings.Contains(gate.plans[0].Markdown, "rewritten by the planner") {
		t.Fatal("the notice is inside the hashed text")
	}
}

func TestNoNoticeInNotesMode(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	g.cfg.Revise.Mode = settings.ReviseModeNotes
	g.mustPlan(planner.Options{})
	if len(gate.plans) != 1 || gate.plans[0].Notice != "" {
		t.Fatalf("plans = %+v", gate.plans)
	}
}
