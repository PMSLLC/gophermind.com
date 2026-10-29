package planner

import (
	"slices"
	"testing"
)

func TestEmptyGroupsBecomeNotApplicable(t *testing.T) {
	m := map[string]any{"refactor_notes": []any{}, "alternatives": []any{"x"}, "construction": []any{}}
	got := emptyGroupsNotApplicable(m)
	if !slices.Equal(got, []string{"refactor_notes"}) {
		t.Fatalf("changed %v, want [refactor_notes]", got)
	}
	na, _ := m["refactor_notes"].(map[string]any)
	if s, _ := na["not_applicable"].(string); len(s) < 20 {
		t.Errorf("refactor_notes = %v", m["refactor_notes"])
	}
	if _, ok := m["construction"].([]any); !ok {
		t.Error("construction may never be not applicable and must be left for the check")
	}
}

func TestContractErrorKinds(t *testing.T) {
	errs := []map[string]any{{"when": "a", "kind": "wrapped"}, {"when": "b", "kind": "sentinel"}}
	if k, ok := contractErrorKinds(errs); !ok || !slices.Equal(k, []string{"wrapped", "sentinel"}) {
		t.Fatalf("kinds = %v %v", k, ok)
	}
	if _, ok := contractErrorKinds([]map[string]any{{"when": "a"}}); ok {
		t.Error("an error without a kind gave kinds")
	}
	if _, ok := contractErrorKinds([]map[string]any{{"when": "a", "kind": "made_up"}}); ok {
		t.Error("an unknown kind was accepted")
	}
}
