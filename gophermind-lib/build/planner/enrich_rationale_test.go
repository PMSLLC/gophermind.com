package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

func longRationale() string {
	return strings.Repeat("This part exists to serve one piece of the brief for its callers. ", 12) // ~800 characters
}

func structureWith(rationale string) string {
	b, _ := json.Marshal(map[string]any{"rationale": rationale, "assumptions": []any{}, "open_questions": []any{}, "decision_ids": []any{}})
	return string(b)
}

func TestAnOverlongStructureRationaleIsTrimmedNotFailed(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, nil))
	calls := withEnrichmentErr(g, func(stage string, call int, nodes []enrichNode, prompt string) (string, error) {
		return goodEnrichmentJSON(nodes), nil
	}, func(stage, prompt string) string { return structureWith(longRationale()) })
	if out, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	if n := calls.count("enrich_root"); n != 1 {
		t.Errorf("the root call ran %d times, want 1 (a trim is not a retry)", n)
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "_state", "enriched.json"))
	var st struct {
		Warnings   []string                  `json:"warnings"`
		Components map[string]map[string]any `json:"components"`
		Root       map[string]any            `json:"root"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(st.Root["rationale"].(string))); n > 600 || n < 20 {
		t.Errorf("root rationale has %d characters", n)
	}
	var root, comp bool
	for _, w := range st.Warnings {
		if strings.Contains(w, "rationale_trimmed") {
			root = root || strings.Contains(w, "root")
			comp = comp || strings.Contains(w, "greeting")
		}
	}
	if !root || !comp {
		t.Errorf("warnings = %v, want rationale_trimmed for the root and a component", st.Warnings)
	}
}

func TestAShortStructureRationaleRetryNamesTheLengthAndTarget(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, nil))
	calls := withEnrichmentErr(g, func(stage string, call int, nodes []enrichNode, prompt string) (string, error) {
		return goodEnrichmentJSON(nodes), nil
	}, func(stage, prompt string) string {
		if stage == "enrich_root" && !strings.Contains(prompt, "could not be used") {
			return structureWith("tiny one")
		}
		return goodStructureJSON
	})
	if out, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	found := false
	for i, s := range calls.stages {
		if s == "enrich_root" && strings.Contains(calls.prompts[i], "8 characters") && strings.Contains(calls.prompts[i], "at least 20") {
			found = true
		}
	}
	if !found {
		t.Error("the retry prompt did not state the rationale length and the minimum")
	}
}
