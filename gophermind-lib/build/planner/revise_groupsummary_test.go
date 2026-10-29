package planner

import (
	"encoding/json"
	"testing"
)

func TestMergePatchKeepsGroupGivenAsSummary(t *testing.T) {
	orig := map[string]any{
		"contract":    map[string]any{"inputs": []any{}, "outputs": []any{}},
		"security":    map[string]any{"trust_boundary": "none"},
		"performance": map[string]any{"complexity": "O(n)"},
	}
	p := revisePatch{Groups: map[string]json.RawMessage{
		"security":    json.RawMessage(`"none"`),
		"performance": json.RawMessage(`{"complexity": "O(n log n)"}`),
	}}
	m, err := mergePatch(orig, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := m["security"].(map[string]any); !ok || s["trust_boundary"] != "none" {
		t.Errorf("security = %v, want the original object kept", m["security"])
	}
	if pf, _ := m["performance"].(map[string]any); pf["complexity"] != "O(n log n)" {
		t.Errorf("performance = %v, want the replacement object", m["performance"])
	}
}
