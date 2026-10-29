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

func TestNotApplicableListFieldsBecomeEmpty(t *testing.T) {
	m := map[string]any{
		"observability": map[string]any{
			"log_events": []any{map[string]any{"level": "info", "msg": "start"}},
			"metrics":    map[string]any{"not_applicable": "main only delegates to Run and does no measurable work"},
		},
		"security": map[string]any{"not_applicable": "a pure function with no inputs from outside"},
	}
	got := notApplicableListFields(m)
	if !slices.Equal(got, []string{"observability.metrics"}) {
		t.Fatalf("changed %v", got)
	}
	if l, ok := m["observability"].(map[string]any)["metrics"].([]any); !ok || len(l) != 0 {
		t.Errorf("metrics = %v, want []", m["observability"].(map[string]any)["metrics"])
	}
	if _, ok := m["security"].(map[string]any)["not_applicable"]; !ok {
		t.Error("a group-level not_applicable was changed")
	}
}

func TestForbiddenObservabilityNABecomesNoLogs(t *testing.T) {
	m := map[string]any{"observability": map[string]any{"not_applicable": "main only wires the program together and logs nothing"}}
	if !observabilityNoLogs(m, "wiring") {
		t.Fatal("not changed")
	}
	o, _ := m["observability"].(map[string]any)
	if l, ok := o["log_events"].([]any); !ok || len(l) != 0 {
		t.Errorf("observability = %v", m["observability"])
	}
	pure := map[string]any{"observability": map[string]any{"not_applicable": "a pure function has nothing to log here"}}
	if observabilityNoLogs(pure, "pure") {
		t.Error("an allowed not applicable was changed")
	}
}
