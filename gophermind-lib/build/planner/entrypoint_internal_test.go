package planner

import (
	"testing"

	"gophermind/gophermind-lib/build/brief"
)

func TestRunnableBinaryFromAcceptanceCommand(t *testing.T) {
	reqs := []Requirement{
		{ID: "F1", Kind: ReqFeature, Name: "Summarize", Text: "`csvstat data.csv` prints one block per column."},
		{ID: "A1", Kind: ReqAcceptance, Text: "`go build ./...` succeeds."},
	}
	name, ok := RunnableBinary(reqs, brief.Frontmatter{ID: "gm-1", Title: "csvstat"}, "")
	if !ok || name != "csvstat" {
		t.Fatalf("RunnableBinary = %q, %v", name, ok)
	}
}

func TestRunnableBinaryIgnoresToolCommandsAndCodeSpans(t *testing.T) {
	reqs := []Requirement{
		{ID: "A1", Kind: ReqAcceptance, Text: "`go test ./...` passes and `gofmt -l .` prints nothing."},
		{ID: "A2", Kind: ReqAcceptance, Text: "`greet.Hello(\"a\")` returns `Hello, a`."},
		{ID: "A3", Kind: ReqAcceptance, Text: "`--format json` is accepted."},
	}
	if name, ok := RunnableBinary(reqs, brief.Frontmatter{Title: "Greeter library"}, "A library of pure functions."); ok {
		t.Fatalf("a library brief is runnable as %q", name)
	}
}

func TestRunnableBinaryFromKeywordsUsesTheTitle(t *testing.T) {
	reqs := []Requirement{{ID: "A1", Kind: ReqAcceptance, Text: "The exit code is 0 on success."}}
	f := brief.Frontmatter{ID: "gm-2026-09-29-003", Title: "Venture Server"}
	name, ok := RunnableBinary(reqs, f, "It is a command-line tool.")
	if !ok || name != "venture-server" {
		t.Fatalf("RunnableBinary = %q, %v", name, ok)
	}
	f.Title = "!!"
	if name, _ := RunnableBinary(reqs, f, "It is a command-line tool."); name != "gm-2026-09-29-003" {
		t.Fatalf("fallback name = %q, want the brief id", name)
	}
}

func TestHasEntryPoint(t *testing.T) {
	doc := func(pkgs ...string) map[string]any {
		var cs []any
		for _, p := range pkgs {
			cs = append(cs, map[string]any{"id": "c", "package": p})
		}
		return map[string]any{"components": cs}
	}
	for _, c := range []struct {
		pkgs []string
		want bool
	}{
		{[]string{"internal/stats", "cmd/csvstat"}, true},
		{[]string{"internal/stats", "internal/render"}, false},
		{[]string{"cmd"}, false},
		{nil, false},
	} {
		if got := hasEntryPoint(doc(c.pkgs...)); got != c.want {
			t.Errorf("hasEntryPoint(%v) = %v", c.pkgs, got)
		}
	}
}

func TestPackageClauseOfAnEntryPointIsMain(t *testing.T) {
	for pkg, want := range map[string]string{"cmd/csvstat": "main", "cmd/a/b": "main", ".": "main", "internal/stats": "stats"} {
		if got := PackageClause(pkg); got != want {
			t.Errorf("PackageClause(%q) = %q, want %q", pkg, got, want)
		}
	}
}
