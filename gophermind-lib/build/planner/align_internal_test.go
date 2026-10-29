package planner

import (
	"reflect"
	"testing"
)

func comp(id, pkg string) map[string]any { return map[string]any{"id": id, "package": pkg} }

func fnFull(id, component, pkg, file string) map[string]any {
	return map[string]any{"id": id, "component": component, "package": pkg, "file": file}
}

func alignDoc(comps []any, fns ...map[string]any) map[string]any {
	var arr []any
	for _, f := range fns {
		arr = append(arr, f)
	}
	return map[string]any{"types": []any{}, "components": comps, "functions": arr}
}

// A model gave the entry point component the package cmd/slugify and then put
// its function in package "main" at main/main.go. No program is built from
// cmd/, so every acceptance command that runs it looked vacuous and planning
// stopped.
func TestAFunctionWithTheWrongPackageTakesItsComponentsPackageAndFolder(t *testing.T) {
	doc := alignDoc([]any{comp("entry-point", "cmd/slugify"), comp("make-a-slug", "internal/slug")},
		fnFull("fn-main", "entry-point", "main", "main/main.go"),
		fnFull("fn-slugify", "make-a-slug", "internal/slug", "slug/slugify.go"),
		fnFull("fn-ok", "make-a-slug", "internal/slug", "internal/slug/ok.go"),
	)
	got := alignFunctionsToComponents(doc)
	if want := []string{"fn-main"}; !reflect.DeepEqual(got, want) {
		t.Errorf("aligned = %v, want %v", got, want)
	}
	fns := objects(doc["functions"])
	if fns[0]["package"] != "cmd/slugify" || fns[0]["file"] != "cmd/slugify/main.go" {
		t.Errorf("fn-main = %v %v", fns[0]["package"], fns[0]["file"])
	}
	if fns[1]["package"] != "internal/slug" || fns[1]["file"] != "slug/slugify.go" {
		t.Errorf("a function whose package already matched was changed (a misplaced file is reported elsewhere): %v %v", fns[1]["package"], fns[1]["file"])
	}
	if fns[2]["file"] != "internal/slug/ok.go" {
		t.Errorf("a consistent function was changed: %v", fns[2])
	}
}

func TestAlignmentLeavesFunctionsWithoutAComponentAlone(t *testing.T) {
	doc := alignDoc([]any{comp("c", "")}, fnFull("fn-a", "", "x", "y/a.go"), fnFull("fn-b", "c", "x", "y/b.go"), fnFull("fn-c", "gone", "x", "y/c.go"))
	if got := alignFunctionsToComponents(doc); len(got) != 0 {
		t.Errorf("aligned %v with no component package to align to", got)
	}
}

func mainFn(component, pkg, file string) map[string]any {
	f := fnFull("fn-main", component, pkg, file)
	f["signature"] = "func main()"
	return f
}

// A model put func main() in the cli component, in internal/cli/main.go, and
// left the cmd/slugify entry-point component without a function. Only a main
// function under cmd/ builds into a program, so no acceptance command could run.
func TestMainOutsideCmdMovesToTheEntryPointComponent(t *testing.T) {
	doc := alignDoc([]any{comp("slug", "internal/slug"), comp("cli", "internal/cli"), comp("entry-point", "cmd/slugify")},
		mainFn("cli", "internal/cli", "internal/cli/main.go"))
	got := moveMainToEntryPoint(doc)
	if want := []string{"fn-main"}; !reflect.DeepEqual(got, want) {
		t.Errorf("moved = %v, want %v", got, want)
	}
	f := objects(doc["functions"])[0]
	if f["component"] != "entry-point" || f["package"] != "cmd/slugify" || f["file"] != "cmd/slugify/main.go" {
		t.Errorf("fn-main = %v %v %v", f["component"], f["package"], f["file"])
	}
}

func TestMainAlreadyUnderCmdOrWithoutAnEntryPointStaysPut(t *testing.T) {
	under := alignDoc([]any{comp("entry-point", "cmd/slugify")}, mainFn("entry-point", "cmd/slugify", "cmd/slugify/main.go"))
	if got := moveMainToEntryPoint(under); len(got) != 0 {
		t.Errorf("moved %v although main was already under cmd/", got)
	}
	none := alignDoc([]any{comp("cli", "internal/cli")}, mainFn("cli", "internal/cli", "internal/cli/main.go"))
	if got := moveMainToEntryPoint(none); len(got) != 0 {
		t.Errorf("moved %v with no cmd/ component to move it to", got)
	}
	other := alignDoc([]any{comp("cli", "internal/cli"), comp("entry-point", "cmd/x")},
		map[string]any{"id": "fn-run", "component": "cli", "package": "internal/cli", "file": "internal/cli/run.go", "signature": "func run() int"})
	if got := moveMainToEntryPoint(other); len(got) != 0 {
		t.Errorf("moved %v, which is not func main()", got)
	}
}
