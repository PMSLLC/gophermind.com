package planner

import (
	"encoding/json"
	"slices"
	"strings"
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

// Run gm-2026-10-30-043: Gemma wrote each kind as the key of an object,
// [{"exit_code": 1}] and [{"other": "error"}], on both repairs.
func TestErrorKindListTakesAKindWrittenAsAKey(t *testing.T) {
	var v any
	_ = json.Unmarshal([]byte(`["wrapped", {"exit_code": 1}, {"other": "error"}]`), &v)
	if got := errorKindList(v); strings.Join(got, ",") != "wrapped,exit_code,other" {
		t.Errorf("got %q", got)
	}
	for _, bad := range []string{`[{"exit_code": 1, "other": 2}]`, `[{"oops": 1}]`, `[3]`} {
		_ = json.Unmarshal([]byte(bad), &v)
		if got := errorKindList(v); len(got) != 0 {
			t.Errorf("%s: got %q, want nothing", bad, got)
		}
	}
}

// Run gm-2026-10-30-044: one kind for two errors, on both repairs. A missing
// kind is the error's own valid kind from the contract, else "other"; extra
// kinds are dropped.
func TestFitErrorKinds(t *testing.T) {
	errs := []map[string]any{{"kind": "sentinel"}, {"kind": "bogus"}, {}}
	if got := fitErrorKinds([]string{"wrapped"}, errs); strings.Join(got, ",") != "wrapped,other,other" {
		t.Errorf("short list: got %q", got)
	}
	if got := fitErrorKinds(nil, errs); strings.Join(got, ",") != "sentinel,other,other" {
		t.Errorf("no list: got %q", got)
	}
	if got := fitErrorKinds([]string{"a", "b", "c", "d"}, errs); strings.Join(got, ",") != "a,b,c" {
		t.Errorf("long list: got %q", got)
	}
}
