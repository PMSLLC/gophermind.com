package planner

import (
	"reflect"
	"testing"
)

// The conventions block has list fields (layout, naming) and text fields
// (errors, logging, testing). A model wrote naming as an object and was
// refused by the schema three times.
func TestNormalizeConventionsFixesTheShapeOfEachField(t *testing.T) {
	c := map[string]any{
		"layout":  "cmd/ and internal/",
		"naming":  map[string]any{"package": "pkg", "function": "fn-<name>"},
		"errors":  []any{"return error", "wrap with %w"},
		"logging": map[string]any{"level": "none"},
		"testing": true,
	}
	if !normalizeConventions(c) {
		t.Fatal("reported no change")
	}
	if got := c["layout"]; !reflect.DeepEqual(got, []any{"cmd/ and internal/"}) {
		t.Errorf("layout = %#v", got)
	}
	if got := c["naming"]; !reflect.DeepEqual(got, []any{"function: fn-<name>", "package: pkg"}) {
		t.Errorf("naming = %#v", got)
	}
	if got := c["errors"]; got != "return error; wrap with %w" {
		t.Errorf("errors = %#v", got)
	}
	if got := c["logging"]; got != "level: none" {
		t.Errorf("logging = %#v", got)
	}
	if got := c["testing"]; got != "true" {
		t.Errorf("testing = %#v", got)
	}
}

func TestNormalizeConventionsLeavesAGoodBlockAlone(t *testing.T) {
	c := map[string]any{"layout": []any{"a"}, "naming": []any{}, "errors": "e", "logging": "l", "testing": "t"}
	if normalizeConventions(c) {
		t.Error("changed a block that was already right")
	}
	if got := c["naming"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("an empty naming list became %#v", got)
	}
	if normalizeConventions(nil) {
		t.Error("reported a change for nil")
	}
}

func TestAnEmptyNamingObjectBecomesAnEmptyList(t *testing.T) {
	c := map[string]any{"layout": []any{"a"}, "naming": map[string]any{}, "errors": "e"}
	normalizeConventions(c)
	if got := c["naming"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("naming = %#v, want []", got)
	}
}
