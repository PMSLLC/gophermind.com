package executor

import (
	"reflect"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

const depImportsContract = `{
 "spec_version": "2.0", "brief_id": "b", "revision": 0, "module": "example.com/m",
 "conventions": {"layout": ["x"], "naming": ["y"], "errors": "z"},
 "types": [
  {"id": "slug-opts", "package": "internal/slug", "file": "internal/slug/o.go", "decl": "// Opts.\ntype Opts struct{ Max int }\n\ntype Mode int", "uses": []},
  {"id": "local-t", "package": "internal/cli", "file": "internal/cli/t.go", "decl": "type Local struct{}", "uses": []}
 ],
 "functions": [
  {"id": "fn-make", "package": "internal/slug", "file": "internal/slug/make.go", "signature": "func Make(s string, max int) string", "doc": "Make.", "uses": [], "component": "slug"},
  {"id": "fn-parse", "package": "internal/cli", "file": "internal/cli/parse.go", "signature": "func parseArgs(a []string) (int, error)", "doc": "Parse.", "uses": [], "component": "cli"},
  {"id": "fn-run", "package": "internal/cli", "file": "internal/cli/run.go", "signature": "func Run() int", "doc": "Run.", "uses": ["fn-make", "fn-parse", "slug-opts", "local-t"], "component": "cli"}
 ],
 "components": []
}`

// fn-run is in internal/cli and uses Make and Opts from internal/slug. A model
// called Make bare, three times: the dependency list does not say where a name
// lives. The leaf's prompt gets the import path and qualifier of each one.
func TestDependencyImportsNameEveryDependencyInAnotherPackage(t *testing.T) {
	c, err := contract.Load([]byte(depImportsContract))
	if err != nil {
		t.Fatal(err)
	}
	got := dependencyImports(c, []string{"fn-make", "fn-parse", "slug-opts", "local-t"}, "internal/cli")
	want := []string{
		`slug.Make: import "example.com/m/internal/slug"`,
		`slug.Opts: import "example.com/m/internal/slug"`,
		`slug.Mode: import "example.com/m/internal/slug"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := dependencyImports(c, []string{"fn-parse", "local-t"}, "internal/cli"); len(got) != 0 {
		t.Errorf("same-package dependencies were listed: %q", got)
	}
	if got := dependencyImports(nil, []string{"fn-make"}, "internal/cli"); got != nil {
		t.Errorf("nil contracts gave %q", got)
	}
}
