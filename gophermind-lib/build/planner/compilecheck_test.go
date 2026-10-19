package planner

import (
	"errors"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

func compileCheckContract() *contract.Contracts {
	return &contract.Contracts{
		Module: "csvstat",
		Types: []contract.Type{
			{ID: "t_summary", Package: "internal/stats", File: "internal/stats/stats.go", Decl: "type ColumnSummary struct {\n\tName string\n\tCount int\n}"},
		},
		Functions: []contract.Function{
			{ID: "fn_detect", Package: "internal/stats", File: "internal/stats/detect.go", Signature: "func detectType(values []string) string"},
			{ID: "fn_summarize", Package: "internal/stats", File: "internal/stats/summarize.go", Signature: "func Summarize(rows [][]string) []ColumnSummary"},
			{ID: "fn_render", Package: "internal/render", File: "internal/render/render.go", Signature: "func Render(w io.Writer, s []stats.ColumnSummary) error"},
		},
	}
}

const ccGood = `package stats

import "testing"

func TestFnDetect(t *testing.T) {
	if detectType([]string{"1"}) == "x" {
		t.Fatal("x")
	}
}
`

func ccCheck(t *testing.T, rel, src string) error {
	t.Helper()
	if _, err := lookGo("go"); err != nil {
		t.Skip("no go tool")
	}
	return compileCheckTestFile(compileCheckContract(), rel, src, nil)
}

func TestCompileCheckAcceptsCorrectFile(t *testing.T) {
	if err := ccCheck(t, "internal/stats/fn_detect_test.go", ccGood); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

func TestCompileCheckExternalPackageUnexported(t *testing.T) {
	src := `package stats_test

import (
	"testing"

	"csvstat/internal/stats"
)

func TestFnDetect(t *testing.T) {
	_ = stats.detectType([]string{"1"})
}
`
	err := ccCheck(t, "internal/stats/fn_detect_test.go", src)
	if err == nil {
		t.Fatal("want a refusal")
	}
	msg := err.Error()
	for _, want := range []string{"undefined name at line 10", "unexported", "package stats"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q: %s", want, msg)
		}
	}
}

func TestCompileCheckUnusedImport(t *testing.T) {
	src := `package stats

import (
	"encoding/json"
	"testing"
)

func TestFnDetect(t *testing.T) {
	_ = detectType(nil)
}
`
	err := ccCheck(t, "internal/stats/fn_detect_test.go", src)
	if err == nil || !strings.Contains(err.Error(), "unused import at line 4") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "encoding/json") {
		t.Errorf("message quotes the import: %v", err)
	}
}

func TestCompileCheckTypeMismatch(t *testing.T) {
	src := `package render_test

import (
	"io"
	"testing"

	"csvstat/internal/render"
)

func TestFnRender(t *testing.T) {
	_ = render.Render(nil, []io.Writer{nil})
}
`
	err := ccCheck(t, "internal/render/fn_render_test.go", src)
	if err == nil || !strings.Contains(err.Error(), "type mismatch at line 11") {
		t.Fatalf("got %v", err)
	}
}

func TestCompileCheckOtherFilesAreCompiledToo(t *testing.T) {
	other := "package stats\n\nimport \"testing\"\n\nfunc TestFnOther(t *testing.T) { _ = Summarize(nil) }\n"
	if _, err := lookGo("go"); err != nil {
		t.Skip("no go tool")
	}
	if err := compileCheckTestFile(compileCheckContract(), "internal/stats/fn_detect_test.go", ccGood, []string{other}); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

func TestCompileCheckSkipsWithoutGo(t *testing.T) {
	old := lookGo
	defer func() { lookGo = old }()
	lookGo = func(string) (string, error) { return "", errors.New("not found") }
	err := compileCheckTestFile(compileCheckContract(), "internal/stats/fn_detect_test.go", "package stats\nfunc (", nil)
	if !errors.Is(err, errNoGo) {
		t.Fatalf("got %v", err)
	}
}

func TestCompileCheckUnresolvableImportIsSkipped(t *testing.T) {
	out := "x_test.go:3:8: no required module provides package github.com/a/b; to add it:\n"
	if err := refuseCompile(compileCheckContract(), "internal/stats", "x_test.go", "package stats\n", out); err != nil {
		t.Fatalf("want skip, got %v", err)
	}
}

func TestCompileMessageNeverQuotesDiagnostic(t *testing.T) {
	c := compileCheckContract()
	for _, diag := range []string{`"SECRETPKG" imported and not used`, "undefined: SECRETNAME", "cannot use SECRETVAL (variable)", "weird SECRET"} {
		m := compileMessage(c, "internal/stats", "package stats\n", "x := 1", 3, diag)
		if strings.Contains(m, "SECRET") {
			t.Errorf("%q leaked: %s", diag, m)
		}
	}
}
