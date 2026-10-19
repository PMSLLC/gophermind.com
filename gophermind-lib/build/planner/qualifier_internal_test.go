package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

// qualDoc is the shape csvstat produced: a module-prefixed package, a bare
// file name, and a same-package type written with the module name.
func qualDoc(sig string) map[string]any {
	raw := `{"module": "csvstat",
	 "components": [{"id": "stats", "package": "csvstat/internal/stats", "exports": []}],
	 "types": [{"id": "column-summary", "package": "csvstat/internal/stats", "file": "internal/stats/stats.go", "decl": "type ColumnSummary struct{ Name string }"}],
	 "functions": [{"id": "fn-finalize-columns", "component": "stats", "package": "csvstat/internal/stats", "file": "summarize.go", "doc": "d", "uses": [], "signature": ` + mustJSON(sig) + `}]}`
	var d map[string]any
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		panic(err)
	}
	return d
}

func sigOf(d map[string]any) string {
	s, _ := objects(d["functions"])[0]["signature"].(string)
	return s
}

func TestNormalizeQualifierRewritesAModuleQualifiedSamePackageType(t *testing.T) {
	d := qualDoc("func finalizeColumns(states []*columnState) []csvstat.ColumnSummary")
	n, ex := normalizeSignatureQualifiers(d)
	if got := sigOf(d); got != "func finalizeColumns(states []*columnState) []ColumnSummary" || n != 1 {
		t.Fatalf("signature %q, count %d", got, n)
	}
	if len(ex) != 1 || !strings.Contains(ex[0], "fn-finalize-columns") {
		t.Errorf("examples = %v", ex)
	}
}

func TestNormalizeQualifierRewritesThePackageNameQualifier(t *testing.T) {
	d := qualDoc("func f(a stats.ColumnSummary, b stats.ColumnSummary) (stats.ColumnSummary, error)")
	n, _ := normalizeSignatureQualifiers(d)
	if got := sigOf(d); got != "func f(a ColumnSummary, b ColumnSummary) (ColumnSummary, error)" || n != 3 {
		t.Fatalf("signature %q, count %d", got, n)
	}
}

func TestNormalizeQualifierLeavesStdlibAndForeignQualifiersAlone(t *testing.T) {
	for _, sig := range []string{
		"func f(r io.Reader) error",
		"func f(d time.Duration) csvstat.Other",
		"func f(s model.ColumnSummary)",
	} {
		d := qualDoc(sig)
		if n, _ := normalizeSignatureQualifiers(d); n != 0 || sigOf(d) != sig {
			t.Errorf("%q changed to %q (count %d)", sig, sigOf(d), n)
		}
	}
}

func TestQualifierIssuesStdlibIsFineAndUnknownIsADefaultByKind(t *testing.T) {
	deps := []string{"github.com/spf13/cobra"}
	for _, sig := range []string{"func f(r io.Reader, d time.Duration) error", "func f(c *cobra.Command)", "func f(x ColumnSummary)"} {
		if is := qualifierIssues(qualDoc(sig), deps); len(is) != 0 {
			t.Errorf("%q: issues %v", sig, is)
		}
	}
	d := qualDoc("func f(x csvstat.Other, y secretword.Thing)")
	is := qualifierIssues(d, deps)
	if len(is) != 1 || !is[0].repairable() || is[0].id != "fn-finalize-columns" {
		t.Fatalf("issues = %+v", is)
	}
	s := is[0].String()
	if !strings.Contains(s, "qualifier") || !strings.Contains(s, "fn-finalize-columns") || strings.Contains(s, "secretword") || strings.Contains(s, "csvstat") {
		t.Errorf("defect text = %q", s)
	}
}

func TestFileDirInStripsTheModulePrefix(t *testing.T) {
	cases := []struct{ module, pkg, file, want string }{
		{"csvstat", "csvstat/internal/stats", "summarize.go", "internal/stats"},
		{"example.com/csvstat", "example.com/csvstat/internal/stats", "summarize.go", "internal/stats"},
		{"csvstat", "internal/stats", "summarize.go", "internal/stats"},
		{"csvstat", "csvstat", "main.go", "."},
		{"csvstat", "csvstat/cmd/csvstat", "main.go", "cmd/csvstat"},
		{"csvstat", "csvstat/internal/stats", "internal/stats/a.go", "internal/stats"},
	}
	for _, c := range cases {
		if got := FileDirIn(c.module, c.pkg, c.file); got != c.want {
			t.Errorf("FileDirIn(%q,%q,%q) = %q, want %q", c.module, c.pkg, c.file, got, c.want)
		}
	}
}
