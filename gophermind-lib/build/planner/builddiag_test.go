package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

const summarizeBuildOutput = `# csvstat/internal/stats
internal/stats/summarize.go:7:2: "math" imported and not used
internal/stats/summarize.go:9:2: "strings" imported and not used
internal/stats/summarize.go:43:4: unknown field DistinctValues in struct literal of type ColumnSummary
internal/stats/summarize.go:69:33: not enough arguments in call to Summarize
	have ([][]string)
	want ([][]string, int)
internal/stats/summarize.go:77:16: summaries[i].DistinctValues undefined (type ColumnSummary has no field or method DistinctValues)
internal/stats/summarize.go:80:3: not enough arguments in call to updateSummary
	have (*ColumnSummary, string)
	want (*ColumnSummary, string, string)
`

func TestBuildHintsNamesFieldsSignatureAndImports(t *testing.T) {
	got := strings.Join(BuildHints(compileCheckContract(), summarizeBuildOutput), "\n")
	for _, want := range []string{
		`ColumnSummary has no field DistinctValues. ColumnSummary declares only these fields: Name, Count.`,
		`The contract declares Summarize as: func Summarize(rows [][]string) []ColumnSummary.`,
		`updateSummary takes (*ColumnSummary, string, string)`,
		`Delete these imports, nothing in the file uses them: "math", "strings".`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hints lack %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "declares only these fields"); n != 1 {
		t.Errorf("the field hint is repeated %d times:\n%s", n, got)
	}
}

func TestBuildHintsSilentOnOtherOutput(t *testing.T) {
	if h := BuildHints(compileCheckContract(), "# csvstat\nx.go:3:1: syntax error: unexpected }\n"); len(h) != 0 {
		t.Fatalf("unexpected hints: %v", h)
	}
	if h := BuildHints(nil, summarizeBuildOutput); h != nil {
		t.Fatalf("hints without a contract: %v", h)
	}
	// A struct the contract does not declare gets no field list.
	if h := BuildHints(compileCheckContract(), "a.go:1:1: unknown field X in struct literal of type localThing\n"); len(h) != 0 {
		t.Fatalf("unexpected hints: %v", h)
	}
}

func undefinedDepContract(t *testing.T) *contract.Contracts {
	t.Helper()
	c, err := contract.Load([]byte(`{
 "spec_version": "2.0", "brief_id": "b", "revision": 0, "module": "hello",
 "conventions": {"layout": ["x"], "naming": ["y"], "errors": "z"},
 "types": [],
 "functions": [
  {"id": "fn-greeting", "package": "internal/greet", "file": "internal/greet/greet.go", "signature": "func Greeting(name string) string", "doc": "d", "uses": [], "component": "c"},
  {"id": "fn-run", "package": "internal/cli", "file": "internal/cli/run.go", "signature": "func Run(args []string) int", "doc": "d", "uses": ["fn-greeting"], "component": "c"}
 ],
 "components": []
}`))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBuildHintsNameTheImportForAnUndefinedDependency(t *testing.T) {
	c := undefinedDepContract(t)
	out := "# hello/internal/cli\ninternal/cli/run.go:12:24: undefined: Greeting\ninternal/cli/run.go:15:24: undefined: Greeting\n"
	want := `Greeting is declared in package greet (hello/internal/greet), not in internal/cli: add import "hello/internal/greet" and call greet.Greeting.`
	if h := BuildHints(c, out); len(h) != 1 || h[0] != want {
		t.Fatalf("hints = %q, want [%q]", h, want)
	}
	if h := BuildHints(c, "internal/greet/greet.go:5:2: undefined: Greeting\n"); len(h) != 0 {
		t.Errorf("same package: %q", h)
	}
	if h := BuildHints(c, "internal/cli/run.go:5:2: undefined: strings\n"); len(h) != 0 {
		t.Errorf("not in contracts: %q", h)
	}
}
