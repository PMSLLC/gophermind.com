package planner

import (
	"reflect"
	"testing"
)

func namesOfParams(ct map[string]any, field string) []string {
	var out []string
	for _, x := range ct[field].([]any) {
		out = append(out, x.(map[string]any)["name"].(string))
	}
	return out
}

// A model wrote an output as {"type": "string"} with no name. The schema needs
// a name, two repair passes did not get one, and planning stopped. Go results
// are often unnamed, so the harness gives a plain name rather than fail.
func TestNormalizeShapeNamesAnUnnamedParam(t *testing.T) {
	d := map[string]any{"id": "fn-make", "contract": map[string]any{
		"inputs":  []any{map[string]any{"type": "string"}, map[string]any{"name": "max", "type": "int"}, map[string]any{"type": "bool"}},
		"outputs": []any{map[string]any{"type": "string", "description": "the slug"}},
	}}
	var notes []string
	normalizeShape(d, "Make", func(field, what string) { notes = append(notes, field+":"+what) })
	ct := d["contract"].(map[string]any)
	if got := namesOfParams(ct, "outputs"); !reflect.DeepEqual(got, []string{"result"}) {
		t.Errorf("outputs named %v, want [result]", got)
	}
	if got := namesOfParams(ct, "inputs"); !reflect.DeepEqual(got, []string{"arg1", "max", "arg3"}) {
		t.Errorf("inputs named %v, want [arg1 max arg3]", got)
	}
	if len(notes) == 0 {
		t.Error("the change was not noted")
	}
}

func TestNormalizeShapeNumbersSeveralUnnamedOutputs(t *testing.T) {
	d := map[string]any{"contract": map[string]any{
		"outputs": []any{map[string]any{"type": "int"}, map[string]any{"type": "error"}},
	}}
	normalizeShape(d, "F", func(string, string) {})
	if got := namesOfParams(d["contract"].(map[string]any), "outputs"); !reflect.DeepEqual(got, []string{"result1", "result2"}) {
		t.Errorf("outputs named %v, want [result1 result2]", got)
	}
}

func TestNormalizeShapeKeepsNamedParamsAndNonObjects(t *testing.T) {
	d := map[string]any{"contract": map[string]any{
		"inputs":  []any{map[string]any{"name": "s", "type": "string"}},
		"outputs": []any{"not an object"},
	}}
	normalizeShape(d, "F", func(string, string) {})
	ct := d["contract"].(map[string]any)
	if got := namesOfParams(ct, "inputs"); !reflect.DeepEqual(got, []string{"s"}) {
		t.Errorf("inputs = %v", got)
	}
	if ct["outputs"].([]any)[0] != "not an object" {
		t.Errorf("a non-object output was changed: %v", ct["outputs"])
	}
}
