package planner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

// A run approved before the understanding existed keeps its approval.json (the
// approve stage is skipped), but its owner is asked to confirm the
// understanding once. Because understanding.json is part of the plan hash, the
// old approval no longer matches until it is removed and the plan approved again.
func TestLegacyApprovedRunIsAskedToConfirmOnceAndNeedsReapproval(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	g.mustPlan(planner.Options{StopAfter: "approve"})
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("fresh approval: %v", err)
	}
	if err := os.Remove(filepath.Join(g.runDir, "_state", "understanding.json")); err != nil {
		t.Fatal(err)
	}
	before := len(gate.understandings)
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "approve"})
	if got := len(gate.understandings) - before; got != 1 {
		t.Errorf("the owner was asked to confirm %d times, want once", got)
	}
	if !g.has("approval.json") {
		t.Error("the old approval.json was removed")
	}
	// A real clock stamps a new confirmed_at; model that by rewriting the record.
	up := filepath.Join(g.runDir, "_state", "understanding.json")
	raw, err := os.ReadFile(up)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(up, []byte(strings.Replace(string(raw), `"confirmed_at": "`, `"confirmed_at": "later-`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := planner.VerifyApproval(g.runDir); err == nil {
		t.Error("the old approval still matches after the understanding changed")
	}
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "approve"})
	if got := len(gate.understandings) - before; got != 1 {
		t.Errorf("a second resume asked again (%d total)", got)
	}
}
