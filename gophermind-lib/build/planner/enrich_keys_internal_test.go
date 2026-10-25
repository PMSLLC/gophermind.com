package planner

import "testing"

func TestNearKey(t *testing.T) {
	cases := []struct {
		name string
		x    map[string]any
		want string
		ok   bool
	}{
		{"one typo", map[string]any{"ratione": "x"}, "ratione", true},
		{"another typo", map[string]any{"rationne": "x"}, "rationne", true},
		{"exact key present", map[string]any{"rationale": "x", "ratione": "y"}, "", false},
		{"two near keys is ambiguous", map[string]any{"ratione": "x", "rationne": "y"}, "", false},
		{"too far", map[string]any{"summary": "x"}, "", false},
		{"short keys are never taken", map[string]any{"rat": "x"}, "", false},
	}
	for _, tc := range cases {
		got, ok := nearKey(tc.x, "rationale")
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: nearKey = %q, %v; want %q, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNormalizeStructureKeysLeavesOtherKeysAlone(t *testing.T) {
	x := map[string]any{"ratione": "x", "assumptions": []any{}, "open_questions": []any{}, "decision_ids": []any{}}
	warns := normalizeStructureKeys(x, "root")
	if _, ok := x["rationale"]; !ok || len(warns) != 1 || warns[0] != "root: rationale_key_normalized" {
		t.Fatalf("x = %v, warns = %v", x, warns)
	}
	if _, still := x["ratione"]; still {
		t.Error("the mistyped key was kept")
	}
}
