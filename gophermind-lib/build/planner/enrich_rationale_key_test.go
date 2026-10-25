package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// structureWithKey is a structure reply whose rationale sits under key.
func structureWithKey(key string) string {
	b, _ := json.Marshal(map[string]any{key: "This part exists to serve one piece of the brief for its callers.", "assumptions": []any{}, "open_questions": []any{}, "decision_ids": []any{}})
	return string(b)
}

// A model that mistypes the key (ratione, rationne) is not asked again with a
// message that says only "missing": the near miss is taken and a warning says so.
func TestAMisspelledRationaleKeyIsTakenWithAWarning(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, nil))
	calls := withEnrichmentErr(g, func(stage string, call int, nodes []enrichNode, prompt string) (string, error) {
		return goodEnrichmentJSON(nodes), nil
	}, func(stage, prompt string) string { return structureWithKey("ratione") })
	if out, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	if n := calls.count("enrich_root"); n != 1 {
		t.Errorf("the root call ran %d times, want 1 (a mistyped key is not a retry)", n)
	}
	raw, _ := os.ReadFile(filepath.Join(g.runDir, "_state", "enriched.json"))
	var st struct {
		Warnings []string       `json:"warnings"`
		Root     map[string]any `json:"root"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	if rat, _ := st.Root["rationale"].(string); len(rat) < 20 {
		t.Errorf("root rationale = %q", rat)
	}
	found := false
	for _, w := range st.Warnings {
		if strings.Contains(w, "rationale_key_normalized") && strings.Contains(w, "root") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want rationale_key_normalized for the root", st.Warnings)
	}
}

// A reply with no key near rationale gets a retry that names the valid keys
// and never quotes the key the model wrote.
func TestAMissingRationaleRetryNamesTheValidKeys(t *testing.T) {
	g := newRig(t, approving(), fixtureDir(t, nil))
	calls := withEnrichmentErr(g, func(stage string, call int, nodes []enrichNode, prompt string) (string, error) {
		return goodEnrichmentJSON(nodes), nil
	}, func(stage, prompt string) string {
		if stage == "enrich_root" && !strings.Contains(prompt, "could not be used") {
			return structureWithKey("zebrasummary")
		}
		return goodStructureJSON
	})
	if out, err := g.plan(planner.Options{StopAfter: "enrich"}); err != nil || out != planner.Done {
		t.Fatalf("plan = %v, %v", out, err)
	}
	found := false
	for i, s := range calls.stages {
		p := calls.prompts[i]
		if s == "enrich_root" && strings.Contains(p, "rationale, assumptions, open_questions, decision_ids") {
			found = true
			if strings.Contains(p, "zebrasummary") {
				t.Error("the retry prompt quotes the key the model wrote")
			}
		}
	}
	if !found {
		t.Error("the retry prompt did not name the valid keys")
	}
}
