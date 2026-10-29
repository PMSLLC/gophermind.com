package planner

import (
	"reflect"
	"testing"
)

// profile_hooks is either {"not_applicable": reason} or a non-empty list of
// bench, pprof and trace. A model wrote ["not_applicable"] and [] (it meant
// "none"), the schema refused both, and two repairs did not change that.
func TestNormalizeProfileHooks(t *testing.T) {
	na := func(m map[string]any) bool {
		_, ok := m["profile_hooks"].(map[string]any)["not_applicable"].(string)
		return ok
	}
	for name, in := range map[string]any{
		"empty list":             []any{},
		"not_applicable in list": []any{"not_applicable"},
		"none":                   []any{"none"},
		"only unknown names":     []any{"cpu", "memory"},
		"a plain string":         "not_applicable",
		"null":                   nil,
	} {
		d := map[string]any{"profile_hooks": in}
		if !normalizeProfileHooks(d) || !na(d) {
			t.Errorf("%s: profile_hooks = %#v, want a not_applicable object", name, d["profile_hooks"])
		}
		if reason := d["profile_hooks"].(map[string]any)["not_applicable"].(string); !naReasonOK(reason) {
			t.Errorf("%s: the reason %q is not accepted as a real reason", name, reason)
		}
	}
	d := map[string]any{"profile_hooks": []any{"bench", "none", "trace", "bench"}}
	if !normalizeProfileHooks(d) || !reflect.DeepEqual(d["profile_hooks"], []any{"bench", "trace"}) {
		t.Errorf("mixed list = %#v, want the valid hooks once each", d["profile_hooks"])
	}
}

func TestNormalizeProfileHooksLeavesGoodValuesAndAbsenceAlone(t *testing.T) {
	for name, d := range map[string]map[string]any{
		"valid list":  {"profile_hooks": []any{"bench"}},
		"na object":   {"profile_hooks": map[string]any{"not_applicable": "a pure helper is never profiled on its own"}},
		"not present": {},
	} {
		before := map[string]any{}
		for k, v := range d {
			before[k] = v
		}
		if normalizeProfileHooks(d) {
			t.Errorf("%s: reported a change", name)
		}
		if !reflect.DeepEqual(d, before) {
			t.Errorf("%s: changed to %#v", name, d)
		}
	}
}
