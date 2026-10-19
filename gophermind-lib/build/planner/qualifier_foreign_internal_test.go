package planner

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// foreignDoc is the csvstat shape: ColumnSummary is declared in internal/stats
// and used bare by functions in other packages.
func foreignDoc(pkg, sig string, extraTypes string) map[string]any {
	raw := `{"module": "github.com/example/csvstat",
	 "types": [{"id": "column-summary", "package": "internal/stats", "file": "internal/stats/types.go", "decl": "type ColumnSummary struct{ Name string }"},
	           {"id": "local", "package": "internal/render", "file": "internal/render/r.go", "decl": "type Local struct{}"}` + extraTypes + `],
	 "functions": [{"id": "fn-render-json", "component": "render", "package": ` + mustJSON(pkg) + `, "file": "x.go", "doc": "d", "uses": [], "signature": ` + mustJSON(sig) + `}]}`
	var d map[string]any
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		panic(err)
	}
	return d
}

func TestQualifyForeignTypeIsQualified(t *testing.T) {
	d := foreignDoc("internal/render", "func RenderJSON(summaries []ColumnSummary, m map[string]*ColumnSummary) ([]byte, error)", "")
	n, ex, amb := qualifyForeignTypes(d)
	want := "func RenderJSON(summaries []stats.ColumnSummary, m map[string]*stats.ColumnSummary) ([]byte, error)"
	if got := sigOf(d); got != want || n != 2 || len(amb) != 0 {
		t.Fatalf("signature %q, count %d, ambiguous %v", got, n, amb)
	}
	if len(ex) != 1 || !strings.Contains(ex[0], "fn-render-json") {
		t.Errorf("examples = %v", ex)
	}
	// The stub generator's own qualifier rule resolves it to the module path.
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+sigOf(d), 0)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveQualifier(Qualifiers(f)[0], sigOf(d), docImports(d), nil)
	if !ok || got != "github.com/example/csvstat/internal/stats" {
		t.Errorf("resolved %q %v", got, ok)
	}
}

func TestQualifyForeignTypeMainPackageAndParamNames(t *testing.T) {
	d := foreignDoc("cmd/csvstat", "func writeOutput(format string, summaries []ColumnSummary, stdout io.Writer) error", "")
	if n, _, _ := qualifyForeignTypes(d); n != 1 || !strings.Contains(sigOf(d), "[]stats.ColumnSummary") {
		t.Fatalf("signature %q, count %d", sigOf(d), n)
	}
}

func TestQualifyForeignTypeLeavesOwnPackageAndBuiltinsAlone(t *testing.T) {
	for _, tc := range []struct{ pkg, sig string }{
		{"internal/stats", "func f(s []ColumnSummary) error"},
		{"internal/render", "func f(l Local, e error, a any, b []byte, r rune) (int, bool)"},
		{"internal/render", "func f(already stats.ColumnSummary)"},
		{"internal/render", "func f[T any](x T, ColumnSummary int) T"},
	} {
		d := foreignDoc(tc.pkg, tc.sig, "")
		if n, _, amb := qualifyForeignTypes(d); n != 0 || sigOf(d) != tc.sig || len(amb) != 0 {
			t.Errorf("%q changed to %q (count %d, ambiguous %v)", tc.sig, sigOf(d), n, amb)
		}
	}
}

func TestQualifyForeignTypeAmbiguousIsLeftAlone(t *testing.T) {
	more := `, {"id": "cs2", "package": "internal/model", "file": "internal/model/m.go", "decl": "type ColumnSummary struct{}"}`
	sig := "func RenderJSON(s []ColumnSummary) error"
	d := foreignDoc("internal/render", sig, more)
	n, _, amb := qualifyForeignTypes(d)
	if n != 0 || sigOf(d) != sig {
		t.Fatalf("ambiguous signature rewritten to %q", sigOf(d))
	}
	if len(amb) != 1 || !strings.Contains(amb[0], "fn-render-json") {
		t.Errorf("ambiguous = %v", amb)
	}
}

func TestQualifyForeignTypeUnknownIsLeftAlone(t *testing.T) {
	sig := "func f(x Mystery) error"
	d := foreignDoc("internal/render", sig, "")
	if n, _, amb := qualifyForeignTypes(d); n != 0 || sigOf(d) != sig || len(amb) != 0 {
		t.Fatalf("changed %q (%d, %v)", sigOf(d), n, amb)
	}
}
