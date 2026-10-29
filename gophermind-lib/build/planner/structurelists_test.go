package planner

import "testing"

func TestStructureListsFillsMissingLists(t *testing.T) {
	x := map[string]any{"assumptions": []any{"a"}, "open_questions": nil}
	if err := structureLists(x); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"open_questions", "decision_ids"} {
		if v, ok := x[k].([]any); !ok || len(v) != 0 {
			t.Errorf("%s = %#v, want []", k, x[k])
		}
	}
	if err := structureLists(map[string]any{"assumptions": "none", "open_questions": []any{}, "decision_ids": []any{}}); err == nil {
		t.Error("a list given as a string was accepted")
	}
}
