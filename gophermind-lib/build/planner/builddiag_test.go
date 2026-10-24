package planner

import (
	"strings"
	"testing"
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
