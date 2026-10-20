package packer

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func stripExpect(t *testing.T, sig string, declared ...string) Expect {
	t.Helper()
	e, err := NewExpect("stats", sig)
	if err != nil {
		t.Fatal(err)
	}
	e.Declared = declared
	return e
}

// contractSrc is what the package already holds, so a stripped reply can be
// type-checked together with it.
const contractSrc = `package stats

type ColumnSummary struct{ N int }

type columnState struct{ n int }

func (c *columnState) add(v float64) { c.n++ }

func otherLeaf() int { return 1 }
`

func typeCheck(t *testing.T, files ...string) {
	t.Helper()
	fset := token.NewFileSet()
	var fs []*ast.File
	for i, s := range files {
		f, err := parser.ParseFile(fset, string(rune('a'+i))+".go", s, 0)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		fs = append(fs, f)
	}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("stats", fset, fs, nil); err != nil {
		t.Fatalf("type check: %v", err)
	}
}

const wholeFile = `package stats

import (
	"fmt"
	"strings"
)

// ColumnSummary is shared.
type ColumnSummary struct{ N int }

type columnState struct{ n int }

func (c *columnState) add(v float64) { c.n++ }

func otherLeaf() int { return 1 }

// Finalize builds the summary.
func Finalize(c *columnState) ColumnSummary {
	return ColumnSummary{N: c.n + len(helperName(c))}
}

func helperName(c *columnState) string { return strings.Repeat("x", 1) }

func unusedAfterStrip() string { return strings.ToUpper("a") }
`

func TestStripSharedTypesMethodsAndImports(t *testing.T) {
	e := stripExpect(t, "func Finalize(c *columnState) ColumnSummary", "ColumnSummary", "columnState", "otherLeaf")
	// A reply whose only user of fmt is a removed function.
	text := strings.Replace(wholeFile, "func otherLeaf() int { return 1 }", "func otherLeaf() int { fmt.Println(); return 1 }", 1)
	r, err := ParseReply(text, e)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	want := []string{"ColumnSummary", "columnState", "columnState.add", "otherLeaf"}
	if strings.Join(r.Stripped, ",") != strings.Join(want, ",") {
		t.Errorf("Stripped = %v, want %v", r.Stripped, want)
	}
	src := string(r.Source)
	for _, gone := range []string{"type ColumnSummary", "type columnState", ") add(", "func otherLeaf", `"fmt"`, "ColumnSummary is shared"} {
		if strings.Contains(src, gone) {
			t.Errorf("source still has %q:\n%s", gone, src)
		}
	}
	for _, kept := range []string{"func Finalize(", "func helperName(", `"strings"`, "func unusedAfterStrip"} {
		if !strings.Contains(src, kept) {
			t.Errorf("source lost %q:\n%s", kept, src)
		}
	}
	if len(r.Imports) != 1 || r.Imports[0] != "strings" {
		t.Errorf("Imports = %v", r.Imports)
	}
	typeCheck(t, contractSrc, src)
}

func TestStripKeepsTargetMethodOfContractType(t *testing.T) {
	e := stripExpect(t, "func (c *columnState) finish() ColumnSummary", "ColumnSummary", "columnState", "add")
	text := "package stats\n\ntype ColumnSummary struct{ N int }\n\ntype columnState struct{ n int }\n\nfunc (c *columnState) add(v float64) { c.n++ }\n\nfunc (c *columnState) finish() ColumnSummary { return ColumnSummary{N: c.n} }\n"
	r, err := ParseReply(text, e)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	src := string(r.Source)
	if !strings.Contains(src, "finish()") || strings.Contains(src, "add(") || strings.Contains(src, "type columnState") {
		t.Errorf("unexpected source:\n%s", src)
	}
	typeCheck(t, "package stats\n\ntype ColumnSummary struct{ N int }\n\ntype columnState struct{ n int }\n\nfunc (c *columnState) add(v float64) { c.n++ }\n", src)
}

func TestStripCollisionOfHelperWithContractFunction(t *testing.T) {
	e := stripExpect(t, "func Finalize(c *columnState) ColumnSummary", "ColumnSummary", "columnState", "otherLeaf")
	text := "package stats\n\nfunc otherLeaf() int { return 2 }\n\nfunc Finalize(c *columnState) ColumnSummary { return ColumnSummary{N: otherLeaf()} }\n"
	r, err := ParseReply(text, e)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	if strings.Contains(string(r.Source), "return 2") || len(r.Stripped) != 1 || r.Stripped[0] != "otherLeaf" {
		t.Errorf("Stripped = %v\n%s", r.Stripped, r.Source)
	}
}

func TestStripValueSpecsAndGroups(t *testing.T) {
	e := stripExpect(t, "func Finalize() int", "limit", "ErrBad")
	text := "package stats\n\nimport \"errors\"\n\nvar ErrBad = errors.New(\"bad\")\n\nconst (\n\tlimit = 3\n\tmine = 4\n)\n\nfunc Finalize() int { return mine }\n"
	r, err := ParseReply(text, e)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	src := string(r.Source)
	if strings.Contains(src, "limit") || strings.Contains(src, "ErrBad") || strings.Contains(src, "errors") || !strings.Contains(src, "mine = 4") {
		t.Errorf("unexpected source:\n%s", src)
	}
	if len(r.Imports) != 0 {
		t.Errorf("Imports = %v", r.Imports)
	}
}

func TestStripLeavesUsedImportAndBlankImports(t *testing.T) {
	e := stripExpect(t, "func Finalize() string", "T")
	text := "package stats\n\nimport (\n\t_ \"embed\"\n\t\"strings\"\n)\n\ntype T struct{ s strings.Builder }\n\nfunc Finalize() string { return strings.ToUpper(\"a\") }\n"
	r, err := ParseReply(text, e)
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	src := string(r.Source)
	if !strings.Contains(src, `"strings"`) || !strings.Contains(src, `_ "embed"`) || strings.Contains(src, "type T") {
		t.Errorf("unexpected source:\n%s", src)
	}
}

func TestNoStripWhenDeclaredNil(t *testing.T) {
	e := stripExpect(t, "func Finalize() int")
	e.Declared = nil
	_, err := ParseReply("package stats\n\nfunc helper() int { return 1 }\n\nfunc Finalize() int { return helper() }\n", e)
	if err == nil || ErrorClass(err) != ClassDeclaration {
		t.Errorf("err = %v", err)
	}
}

func TestStripStillRefusesWhatRemains(t *testing.T) {
	e := stripExpect(t, "func Finalize() int", "T")
	// Exported helper is not in Declared: stripping T does not make it allowed.
	_, err := ParseReply("package stats\n\ntype T int\n\nfunc Extra() int { return 1 }\n\nfunc Finalize() int { return 1 }\n", e)
	if err == nil || ErrorClass(err) != ClassDeclaration {
		t.Errorf("err = %v", err)
	}
}

func TestStripSignatureMismatchStillRefused(t *testing.T) {
	e := stripExpect(t, "func Finalize() int", "T")
	_, err := ParseReply("package stats\n\ntype T int\n\nfunc Finalize() string { return \"\" }\n", e)
	if err == nil || ErrorClass(err) != ClassSignature {
		t.Errorf("err = %v", err)
	}
}

func TestErrorClasses(t *testing.T) {
	e := stripExpect(t, "func Finalize() int")
	cases := []struct{ name, text, class string }{
		{"not found", "package stats\n\nfunc Other() {}\n", ClassNotFound},
		{"package", "package other\n\nfunc Finalize() int { return 1 }\n", ClassPackage},
		{"syntax", "package stats\n\nfunc Finalize( {\n", ClassSyntax},
		{"empty", "", ""},
		{"init", "package stats\n\nfunc init() {}\n\nfunc Finalize() int { return 1 }\n", ""},
	}
	for _, c := range cases {
		_, err := ParseReply(c.text, e)
		if err == nil || ErrorClass(err) != c.class {
			t.Errorf("%s: class %q err %v", c.name, ErrorClass(err), err)
		}
	}
}
