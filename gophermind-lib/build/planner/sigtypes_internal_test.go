package planner

import (
	"reflect"
	"testing"
)

func undeclDoc(sigs ...string) map[string]any {
	fns := []any{}
	for i, s := range sigs {
		fns = append(fns, map[string]any{"id": "fn-" + string(rune('a'+i)), "component": "c", "package": "p", "signature": s})
	}
	return map[string]any{
		"module":    "m",
		"types":     []any{map[string]any{"id": "decl-t", "package": "p", "decl": "type T struct{}"}},
		"functions": fns,
	}
}

func undeclNames(l []sigType) []string {
	var out []string
	for _, s := range l {
		out = append(out, s.Name)
	}
	return out
}

func TestUndeclaredSignatureTypes(t *testing.T) {
	doc := undeclDoc(
		"func A() *state",                  // undeclared result type
		"func B(t T, n int) error",         // declared, builtin
		"func (s *state) C(x other) error", // undeclared receiver and parameter
		"func D(w zzqual.Thing) error",     // a qualifier is not a type of the plan
		"func E(b strings.Builder) error",  // standard library
	)
	got := undeclNames(undeclaredSignatureTypes(doc))
	want := []string{"state", "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("undeclared = %v, want %v", got, want)
	}
	if u := undeclaredSignatureTypes(doc)[0].Users; !reflect.DeepEqual(u, []string{"fn-a", "fn-c"}) {
		t.Errorf("users of state = %v", u)
	}
}

func TestTypeIDFor(t *testing.T) {
	for in, want := range map[string]string{"columnState": "column-state", "greetState": "greet-state", "state": "state"} {
		if got := typeIDFor(in); got != want {
			t.Errorf("typeIDFor(%q) = %q, want %q", in, got, want)
		}
	}
}
