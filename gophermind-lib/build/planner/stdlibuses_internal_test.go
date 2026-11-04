package planner

import (
	"encoding/json"
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

// The model also lists identifiers from those packages as functions: io.Writer
// as fn-io-writer, os.File as fn-os-file. The repair step then asked it to
// write a function with that id, and the signature it invented did not parse.
func TestStandardLibraryIdentifiersAreDroppedFromUses(t *testing.T) {
	doc := fnDoc(
		fn("fn-a", "fn-io-writer", "fn-os-file", "io-reader", "fn-bufio-scanner", "fn-strings-builder", "fn-errors-new", "fn-b", "fn-io-handler"),
		fn("fn-b"),
	)
	dropped := dropStdlibUses(doc)
	if want := []string{"fn-io-writer", "fn-os-file", "io-reader", "fn-bufio-scanner", "fn-strings-builder", "fn-errors-new"}; !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
	if got := strList(objects(doc["functions"])[0]["uses"]); !reflect.DeepEqual(got, []string{"fn-b", "fn-io-handler"}) {
		t.Errorf("uses = %v: fn-io-handler is not a standard library name and must stay for the repair step", got)
	}
}

// Before the planner knows whether an id is a type or a function it keeps both
// forms in one entry: "io-reader|fn-io-reader". That is still io.Reader.
func TestPendingReferencesToStandardLibraryNamesAreDropped(t *testing.T) {
	doc := fnDoc(
		fn("fn-a", "io-reader|fn-io-reader", "os-file|fn-os-file", "slug|fn-slug", "fn-b"),
		fn("fn-b"),
	)
	dropped := dropStdlibUses(doc)
	if want := []string{"io-reader|fn-io-reader", "os-file|fn-os-file"}; !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
	if got := strList(objects(doc["functions"])[0]["uses"]); !reflect.DeepEqual(got, []string{"slug|fn-slug", "fn-b"}) {
		t.Errorf("uses = %v, want the non-standard-library pending reference kept", got)
	}
}

// unresolved skips a bare qualifier (http, rand, context), so dropStdlibUses
// must remove it too, or contract validation refuses an entry nothing repairs.
func TestBareStandardLibraryQualifiersPassTheFinalValidation(t *testing.T) {
	var d map[string]any
	fnUsing := func(uses ...string) string {
		m := map[string]any{"id": "fn-a", "component": "greeting", "package": "x", "file": "internal/x/a.go", "signature": "func Run(a int) error", "doc": "d", "uses": uses}
		b, _ := json.Marshal(m)
		return string(b)
	}
	d = docWith(fnUsing("http", "rand", "context"))
	if _, err := validateContractDoc(d, testRunID); err == nil {
		t.Fatal("setup: a doc with bare qualifiers in uses validates before the drop")
	}
	if un := unresolvedFinal(d); len(un) != 0 {
		t.Errorf("unresolved = %v, want none", un)
	}
	if dropped := dropStdlibUses(d); !reflect.DeepEqual(dropped, []string{"http", "rand", "context"}) {
		t.Errorf("dropped = %v", dropped)
	}
	if _, err := validateContractDoc(d, testRunID); err != nil {
		t.Errorf("validation after the drop: %v", err)
	}
}
