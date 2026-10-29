package planner

import (
	"encoding/json"
	"reflect"
	"testing"
)

// depends_on holds ids of contract functions. A model lists the Go identifiers a
// function uses (os.File, io.Reader, strconv.Atoi) there and re-sent them
// through two repair passes. A dotted name cannot be a contract id.
func TestNormalizeShapeDropsGoIdentifiersFromDependsOn(t *testing.T) {
	d := map[string]any{"id": "fn-run", "depends_on": []any{"fn-parse-args", "os.File", "io.Reader", "errors.Error", "fn-make"}}
	var notes []string
	normalizeShape(d, "run", func(field, what string) { notes = append(notes, field+":"+what) })
	got, _ := d["depends_on"].([]any)
	if !reflect.DeepEqual(got, []any{"fn-parse-args", "fn-make"}) {
		t.Errorf("depends_on = %v, want only the contract ids", got)
	}
	if len(notes) == 0 {
		t.Error("the drop was not noted")
	}
}

// A model wrote an error's returns as a number, an exit code: {"returns": 2}.
// The check wants text, so it called the entry incomplete and stopped planning.
func TestNormalizeShapeWritesScalarErrorFieldsAsText(t *testing.T) {
	var asNumber any
	if err := json.Unmarshal([]byte(`2`), &asNumber); err != nil {
		t.Fatal(err)
	}
	d := map[string]any{"contract": map[string]any{"errors": []any{
		map[string]any{"when": "parseArgs fails", "returns": asNumber},
		map[string]any{"when": "write fails", "returns": true},
		map[string]any{"when": "ok", "returns": "nil"},
	}}}
	normalizeShape(d, "run", func(string, string) {})
	errs := d["contract"].(map[string]any)["errors"].([]any)
	want := []string{"2", "true", "nil"}
	for i, e := range errs {
		if got, _ := e.(map[string]any)["returns"].(string); got != want[i] {
			t.Errorf("errors[%d].returns = %v, want %q", i, e.(map[string]any)["returns"], want[i])
		}
	}
}

func TestNormalizeShapeKeepsAMissingErrorFieldMissing(t *testing.T) {
	d := map[string]any{"contract": map[string]any{"errors": []any{map[string]any{"when": "x"}}}}
	normalizeShape(d, "run", func(string, string) {})
	if _, has := d["contract"].(map[string]any)["errors"].([]any)[0].(map[string]any)["returns"]; has {
		t.Error("a missing returns was invented")
	}
}
