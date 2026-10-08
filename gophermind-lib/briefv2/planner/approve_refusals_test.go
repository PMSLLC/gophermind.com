package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

// planUntilCoverage plans the greeter up to and including Coverage: everything
// the approve stage reads exists, and nothing is approved yet.
func planUntilCoverage(t *testing.T) *rig {
	t.Helper()
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	return g
}

// editJSON rewrites a run-folder JSON file through change.
func editJSON(t *testing.T, g *rig, name string, change func(doc map[string]any)) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(g.read(name), &doc); err != nil {
		t.Fatal(err)
	}
	change(doc)
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.runDir, filepath.FromSlash(name)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestApproveRefusesWhileANodeListsAnOpenQuestion(t *testing.T) {
	g := planUntilCoverage(t)
	editJSON(t, g, "_state/decomposed.json", func(doc map[string]any) {
		for _, drafts := range doc["components"].(map[string]any) {
			d := drafts.([]any)[0].(map[string]any)
			d["open_questions"] = []any{"Which store?"}
			return
		}
	})
	err := planner.ApproveStage(planner.New(g.deps), greeterID)
	if err == nil || !strings.Contains(err.Error(), "open questions") {
		t.Fatalf("err = %v, want a refusal naming the open questions", err)
	}
	if g.has("approval.json") {
		t.Error("a plan with an open question was approved")
	}
}

func TestApproveRefusesWithoutAConfirmedUnderstanding(t *testing.T) {
	g := planUntilCoverage(t)
	if err := os.Remove(filepath.Join(g.runDir, "_state", "understanding.json")); err != nil {
		t.Fatal(err)
	}
	err := planner.ApproveStage(planner.New(g.deps), greeterID)
	if err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("err = %v, want 'not confirmed'", err)
	}
	if g.has("approval.json") {
		t.Error("a plan was approved without a confirmed understanding")
	}
}

func TestApproveRefusesAStaleUnderstanding(t *testing.T) {
	g := planUntilCoverage(t)
	editJSON(t, g, "_state/understanding.json", func(doc map[string]any) { doc["understanding_hash"] = "0000" })
	err := planner.ApproveStage(planner.New(g.deps), greeterID)
	if err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("err = %v, want 'not confirmed'", err)
	}
	if g.has("approval.json") {
		t.Error("a plan was approved on a stale understanding")
	}
}

func TestEditingEnrichedJSONAfterApprovalFailsVerification(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "approve"})
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("fresh approval: %v", err)
	}
	editJSON(t, g, "_state/enriched.json", func(doc map[string]any) {
		doc["root"].(map[string]any)["rationale"] = "edited after the approval"
	})
	if err := planner.VerifyApproval(g.runDir); err == nil {
		t.Fatal("an edit of enriched.json after the approval was not noticed")
	}
}

func TestAPlanWithoutEnrichedJSONStillHashes(t *testing.T) {
	g := planUntilCoverage(t)
	_, with, err := planner.RenderPlan(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(g.runDir, "_state", "enriched.json")); err != nil {
		t.Fatal(err)
	}
	_, without, err := planner.RenderPlan(g.runDir)
	if err != nil {
		t.Fatalf("a run folder without enriched.json: %v", err)
	}
	if with == without {
		t.Error("enriched.json is not part of the hash")
	}
}

func TestAnApprovalThatPredatesTheUnderstandingSaysSo(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "approve"})
	editJSON(t, g, "approval.json", func(doc map[string]any) { delete(doc, "understanding_hash") })
	err := planner.VerifyApproval(g.runDir)
	if err == nil || !strings.Contains(err.Error(), "predates the confirmed understanding") {
		t.Fatalf("err = %v, want the message about the understanding", err)
	}
}
