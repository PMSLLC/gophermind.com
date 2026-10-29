package planner

import (
	"reflect"
	"testing"
)

func fnIn(id, pkg, sig string, uses ...string) map[string]any {
	m := fn(id, uses...)
	m["package"], m["signature"] = pkg, sig
	return m
}

func signatureOfFn(doc map[string]any, id string) string {
	for _, f := range objects(doc["functions"]) {
		if f["id"] == id {
			return f["signature"].(string)
		}
	}
	return ""
}

// main (package main) used run in internal/cli. Go cannot call an unexported
// function of another package, so the program and its test could never compile.
func TestAFunctionUsedFromAnotherPackageIsExported(t *testing.T) {
	doc := fnDoc(
		fnIn("fn-main", "main", "func main()", "fn-run"),
		fnIn("fn-run", "internal/cli", "func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int", "fn-open"),
		fnIn("fn-open", "internal/cli", "func open(name string) (io.Reader, error)"),
		fnIn("fn-slug", "internal/slug", "func (b *Builder) build(s string) string", "fn-esc"),
	)
	// fn-main calls run from another package: exported. fn-run calls open in the
	// same package: left alone.
	renamed := exportCrossPackageUses(doc)
	if want := []string{"fn-run"}; !reflect.DeepEqual(renamed, want) {
		t.Errorf("renamed = %v, want %v", renamed, want)
	}
	if got := signatureOfFn(doc, "fn-run"); got != "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int" {
		t.Errorf("signature = %q", got)
	}
	if got := signatureOfFn(doc, "fn-open"); got != "func open(name string) (io.Reader, error)" {
		t.Errorf("a same-package callee was renamed: %q", got)
	}
}

func TestExportingKeepsExportedNamesAndMethods(t *testing.T) {
	doc := fnDoc(
		fnIn("fn-a", "main", "func main()", "fn-b", "fn-c", "fn-d"),
		fnIn("fn-b", "x", "func Already() int"),
		fnIn("fn-c", "y", "func (s *Store) get(k string) string"),
		fnIn("fn-d", "z", "func run() int"),
		fnIn("fn-e", "z", "func Run() int"), // would collide with fn-d's new name
	)
	renamed := exportCrossPackageUses(doc)
	if got := signatureOfFn(doc, "fn-b"); got != "func Already() int" {
		t.Errorf("an exported name changed: %q", got)
	}
	if got := signatureOfFn(doc, "fn-c"); got != "func (s *Store) Get(k string) string" {
		t.Errorf("a method was not exported: %q", got)
	}
	if got := signatureOfFn(doc, "fn-d"); got != "func run() int" {
		t.Errorf("a rename that would collide with an existing name went ahead: %q", got)
	}
	if want := []string{"fn-c"}; !reflect.DeepEqual(renamed, want) {
		t.Errorf("renamed = %v, want %v", renamed, want)
	}
}
