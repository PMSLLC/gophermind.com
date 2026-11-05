package planner

import (
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

const pkgMapArch = "- `cmd/slugify` (package `main`): calls `cli.Run` with `os.Args[1:]`. Nothing else.\n" +
	"- `internal/slug`: `Make(s string, max int) string` is pure.\n" +
	"- `internal/cli`: `Run(args []string) int` parses the arguments and calls `slug.Make`.\n"

func pkgDoc(module string, pkgs ...[2]string) map[string]any {
	var comps []any
	for _, p := range pkgs {
		comps = append(comps, map[string]any{"id": p[0], "package": p[1]})
	}
	return map[string]any{"module": module, "components": comps}
}

func docPackages(doc map[string]any) []string {
	var out []string
	for _, c := range objects(doc["components"]) {
		out = append(out, c["package"].(string))
	}
	return out
}

func TestMapComponentPackagesToArchitectureDirs(t *testing.T) {
	doc := pkgDoc("slugify", [2]string{"slug-logic", "slug"}, [2]string{"cli-logic", "cli"}, [2]string{"entry-point", "main"})
	changes := mapComponentPackages(doc, pkgMapArch, "slugify")
	if got, want := docPackages(doc), []string{"internal/slug", "internal/cli", "cmd/slugify"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("packages = %v, want %v", got, want)
	}
	if len(changes) != 3 || changes[0] != "slug-logic: slug -> internal/slug" {
		t.Fatalf("changes = %v", changes)
	}
}

func TestMapComponentPackagesAmbiguousMain(t *testing.T) {
	arch := "- `cmd/a`: one.\n- `cmd/b`: two.\n"
	doc := pkgDoc("m", [2]string{"entry", "main"})
	if changes := mapComponentPackages(doc, arch, "m"); len(changes) != 0 {
		t.Fatalf("changes = %v, want none", changes)
	}
	if got := docPackages(doc); got[0] != "main" {
		t.Fatalf("packages = %v, want main unchanged", got)
	}
}

func TestMapComponentPackagesStripsModule(t *testing.T) {
	doc := pkgDoc("example.com/slugify", [2]string{"cli-logic", "example.com/slugify/internal/cli"})
	mapComponentPackages(doc, pkgMapArch, "example.com/slugify")
	if got := docPackages(doc); got[0] != "internal/cli" {
		t.Fatalf("packages = %v", got)
	}
}

func TestMapComponentPackagesLeavesUnknownNames(t *testing.T) {
	doc := pkgDoc("greeter", [2]string{"greeter", "greet"})
	if changes := mapComponentPackages(doc, pkgMapArch, "greeter"); len(changes) != 0 {
		t.Fatalf("changes = %v", changes)
	}
	if got := docPackages(doc); got[0] != "greet" {
		t.Fatalf("packages = %v", got)
	}
}

func TestLocalChecksRefuseMainAsAComponentPackage(t *testing.T) {
	c := &contract.Contracts{BriefID: "b", Components: []contract.Component{{ID: "entry", Package: "main"}}}
	err := localChecks(c, "b")
	if err == nil || !strings.Contains(err.Error(), "package main is a Go package name, not a directory") {
		t.Fatalf("localChecks = %v", err)
	}
}
