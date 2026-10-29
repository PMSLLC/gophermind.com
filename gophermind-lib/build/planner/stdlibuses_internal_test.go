package planner

import (
	"reflect"
	"testing"
)

func fnDoc(fns ...map[string]any) map[string]any {
	var arr []any
	for _, f := range fns {
		arr = append(arr, f)
	}
	return map[string]any{"types": []any{}, "functions": arr}
}

func fn(id string, uses ...string) map[string]any {
	var u []any
	for _, s := range uses {
		u = append(u, s)
	}
	return map[string]any{"id": id, "uses": u}
}

// A model lists the packages a function imports in uses. They are not contract
// ids, and asking it to declare them made it write empty functions named after
// the packages (func fmt(), func os()), which shadow the imports and stop the
// first wave compiling.
func TestStandardLibraryNamesAreDroppedFromUsesAtTheFinalCheck(t *testing.T) {
	doc := fnDoc(
		fn("fn-a", "fn-fmt", "strconv", "fn-encoding-json", "fn-net-http", "fn-b"),
		fn("fn-b"),
	)
	dropped := dropStdlibUses(doc)
	if want := []string{"fn-fmt", "strconv", "fn-encoding-json", "fn-net-http"}; !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
	if got := strList(objects(doc["functions"])[0]["uses"]); !reflect.DeepEqual(got, []string{"fn-b"}) {
		t.Errorf("uses = %v, want only the real function", got)
	}
	if un := unresolvedFinal(doc); len(un) != 0 {
		t.Errorf("unresolved after the drop = %v, want none, so the repair step is not asked for them", un)
	}
}

func TestADeclaredFunctionKeepsItsNameEvenIfItMatchesAPackage(t *testing.T) {
	doc := fnDoc(fn("fn-log"), fn("fn-c", "fn-log"), fn("fn-d", "fn-sort"))
	dropped := dropStdlibUses(doc)
	if got := strList(objects(doc["functions"])[1]["uses"]); !reflect.DeepEqual(got, []string{"fn-log"}) {
		t.Errorf("a declared function was dropped from uses: %v", got)
	}
	if want := []string{"fn-sort"}; !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want only the undeclared package name %v", dropped, want)
	}
}

func TestDropStdlibUsesLeavesOtherIDsAlone(t *testing.T) {
	doc := fnDoc(fn("fn-a", "fn-parse-args", "internal-session"))
	if dropped := dropStdlibUses(doc); len(dropped) != 0 {
		t.Errorf("dropped %v from a doc with no standard library names", dropped)
	}
	if got := strList(objects(doc["functions"])[0]["uses"]); len(got) != 2 {
		t.Errorf("uses = %v", got)
	}
}
