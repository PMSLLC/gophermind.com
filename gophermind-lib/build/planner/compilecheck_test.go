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

// A helper field declared interface{} and passed to a function that takes an
// io.Writer: the model sees the types and the function, never its own names.
func TestCompileMessageInterfaceToWriter(t *testing.T) {
	c := compileCheckContract()
	c.Functions = append(c.Functions, contract.Function{ID: "fn_rt", Package: "cmd/csvstat", File: "cmd/csvstat/render.go", Signature: "func renderText(summaries []stats.ColumnSummary, maxRows int, w io.Writer) error"})
	diag := "cannot use w (variable of type interface{}) as io.Writer value in argument to renderText: interface{} does not implement io.Writer (missing method Write)"
	m := compileMessage(c, "cmd/csvstat", "package main\n", "x", 176, diag)
	for _, want := range []string{"type mismatch at line 176", "a value of type interface{} is passed where io.Writer is required in the call to renderText", "declare the field with the exact type the function takes"} {
		if !strings.Contains(m, want) {
			t.Errorf("lacks %q: %s", want, m)
		}
	}
	if strings.Contains(m, " w ") || strings.Contains(m, "missing method") {
		t.Errorf("leaks compiler text: %s", m)
	}
}

func TestCompileMessageMismatchPlaces(t *testing.T) {
	c := compileCheckContract()
	cases := map[string]string{
		"cannot use x (variable of type []string) as int value in assignment":                                    "a value of type []string is assigned where int is required",
		"cannot use s (variable of type map[string]int) as string value in return statement":                     "a value of type map[string]int is returned where string is required",
		"cannot use v (variable of type *stats.ColumnSummary) as stats.ColumnSummary value in struct literal":    "is given to a struct field that requires stats.ColumnSummary",
		"cannot use v (constant 1 of type int) as string value in variable declaration":                          "used in a variable declaration that requires string",
		"cannot use f (variable of type func(int) error) as io.Writer value in argument to (*bytes.Buffer).Grow": "a value of type func(int) error is passed where io.Writer is required",
	}
	for diag, want := range cases {
		m := compileMessage(c, "internal/stats", "package stats\n", "x", 5, diag)
		if !strings.Contains(m, want) {
			t.Errorf("%q: want %q, got %s", diag, want, m)
		}
	}
	m := compileMessage(c, "internal/stats", "package stats\n", "x", 5, "cannot use f (variable of type func(int) error) as io.Writer value in argument to (*bytes.Buffer).Grow")
	if strings.Contains(m, "Grow") || strings.Contains(m, "bytes") {
		t.Errorf("unsafe call name printed: %s", m)
	}
}

func TestCompileMessageModelTypeFallsBack(t *testing.T) {
	c := compileCheckContract()
	for _, diag := range []string{
		"cannot use h (variable of type secretHelper) as io.Writer value in argument to Render",
		"cannot use w (variable of type io.Writer) as secretHelper value in argument to Render",
		"cannot use w (variable of type struct{secretField int}) as int value in assignment",
		"cannot use w (variable of type func(secretParam int)) as int value in assignment",
		"cannot use w (variable of type interface{SecretMethod()}) as int value in assignment",
		"cannot use w (variable of type []secretHelper) as int value in assignment",
	} {
		m := compileMessage(c, "internal/stats", "package stats\n", "x", 7, diag)
		if m != "type mismatch at line 7" {
			t.Errorf("%q: want the generic text, got %s", diag, m)
		}
	}
}

func TestCompileMessageWrongArgumentCount(t *testing.T) {
	c := compileCheckContract()
	diag := "not enough arguments in call to Summarize\n\thave ()\n\twant ([][]string)"
	m := compileMessage(c, "internal/stats", "package stats\n", "x", 9, diag)
	want := "wrong number of arguments at line 9 in the call to Summarize: it takes ([][]string) and the call passes ()"
	if m != want {
		t.Errorf("got %s", m)
	}
	diag = "too many arguments in call to render.Render\n\thave (io.Writer, []stats.ColumnSummary, number)\n\twant (io.Writer, []stats.ColumnSummary)"
	m = compileMessage(c, "internal/stats", "package stats\n", "x", 9, diag)
	want = "wrong number of arguments at line 9 in the call to render.Render: it takes (io.Writer, []stats.ColumnSummary) and the call passes (io.Writer, []stats.ColumnSummary, number)"
	if m != want {
		t.Errorf("got %s", m)
	}
	// An unsafe type drops the lists; an unsafe callee drops its name.
	diag = "not enough arguments in call to h.secretMethod\n\thave (secretT)\n\twant (int, int)"
	m = compileMessage(c, "internal/stats", "package stats\n", "x", 9, diag)
	if m != "wrong number of arguments at line 9" {
		t.Errorf("got %s", m)
	}
	diag = "not enough arguments in call to Summarize\n\thave (secretT)\n\twant (int, int)"
	m = compileMessage(c, "internal/stats", "package stats\n", "x", 9, diag)
	if m != "wrong number of arguments at line 9 in the call to Summarize" {
		t.Errorf("got %s", m)
	}
}

func TestCompileMessageUnusedVariableMissingReturnUnknownField(t *testing.T) {
	c := compileCheckContract()
	if m := compileMessage(c, "internal/stats", "package stats\n", "x", 4, "declared and not used: secretVar"); m != "unused variable at line 4" {
		t.Errorf("got %s", m)
	}
	if m := compileMessage(c, "internal/stats", "package stats\n", "x", 4, "secretVar declared but not used"); m != "unused variable at line 4" {
		t.Errorf("got %s", m)
	}
	if m := compileMessage(c, "internal/stats", "package stats\n", "x", 6, "missing return"); m != "missing return at line 6" {
		t.Errorf("got %s", m)
	}
	m := compileMessage(c, "internal/stats", "package stats\n", "x", 8, "s.Total undefined (type *stats.ColumnSummary has no field or method Total)")
	if m != "unknown field or method at line 8 on a value of type *stats.ColumnSummary" {
		t.Errorf("got %s", m)
	}
	m = compileMessage(c, "internal/stats", "package stats\n", "x", 9, "r.String undefined (type *strings.Reader has no field or method String)")
	if !strings.HasPrefix(m, "unknown field or method at line 9 on a value of type *strings.Reader (") || !strings.Contains(m, "standard library") {
		t.Errorf("got %s", m)
	}
	m = compileMessage(c, "internal/stats", "package stats\n", "x", 8, "s.Total undefined (type secretHelper has no field or method Total)")
	if m != "unknown field or method at line 8" {
		t.Errorf("got %s", m)
	}
}

// End to end through the compiler: the exact failure of the csvstat smoke run.
func TestCompileCheckInterfaceFieldToWriter(t *testing.T) {
	src := `package render_test

import (
	"testing"

	"csvstat/internal/render"
)

type secretHelper struct{ w interface{} }

func TestFnRender(t *testing.T) {
	h := secretHelper{}
	_ = render.Render(h.w, nil)
}
`
	err := ccCheck(t, "internal/render/fn_render_test.go", src)
	if err == nil || !strings.Contains(err.Error(), "a value of type interface{} is passed where io.Writer is required in the call to render.Render") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "secretHelper") {
		t.Errorf("leaked: %v", err)
	}
}

// A composite literal that names a field the contract struct does not declare
// (the csvstat finalize smoke: &columnState{values: ...}). The message names
// the kind, the line, the type and the contract's declared fields; the invented
// field name is never copied. A wrong-case spelling of a declared field is the
// contract's own vocabulary and is named.
func TestCompileMessageUnknownFieldInLiteral(t *testing.T) {
	c := compileCheckContract()
	cases := []struct{ dir, diag, want string }{
		{"internal/stats", "unknown field secretValues in struct literal of type ColumnSummary",
			"unknown field at line 21 in a composite literal of type ColumnSummary; declared fields: Name, Count"},
		{"internal/stats", "unknown field count in struct literal of type ColumnSummary, but does have Count",
			"unknown field count at line 21 in a composite literal of type ColumnSummary; declared fields: Name, Count"},
		{"internal/render", "unknown field secretValues in struct literal of type stats.ColumnSummary",
			"unknown field at line 21 in a composite literal of type stats.ColumnSummary; declared fields: Name, Count"},
		{"internal/render", "unknown field Secret in struct literal of type stats.ColumnSummary, but does have unexported secret",
			"unknown field at line 21 in a composite literal of type stats.ColumnSummary; declared fields: Name, Count"},
		{"internal/stats", "unknown field secretValues in struct literal of type secretHelper",
			"unknown field at line 21 in a composite literal"},
		{"internal/stats", "unknown field 'secretValues' in struct literal",
			"unknown field at line 21 in a composite literal"},
	}
	for _, tc := range cases {
		m := compileMessage(c, tc.dir, "package stats\n", "x", 21, tc.diag)
		if m != tc.want {
			t.Errorf("%q: want %q, got %q", tc.diag, tc.want, m)
		}
	}
	c.Types = append(c.Types, contract.Type{ID: "t_emb", Package: "internal/stats", File: "internal/stats/stats.go", Decl: "type Wide struct {\n\tColumnSummary\n\tRows int\n}"})
	m := compileMessage(c, "internal/stats", "package stats\n", "x", 3, "unknown field secretValues in struct literal of type Wide")
	if m != "unknown field at line 3 in a composite literal of type Wide; declared fields: Rows; the type embeds other types" {
		t.Errorf("got %q", m)
	}
}

// End to end through the compiler, the shape of the csvstat finalize smoke.
func TestCompileCheckUnknownFieldInLiteral(t *testing.T) {
	src := `package stats

import "testing"

func TestFnSummarize(t *testing.T) {
	s := &ColumnSummary{
		Name:         "a",
		secretValues: []float64{1, 2},
	}
	_ = s
}
`
	err := ccCheck(t, "internal/stats/fn_summarize_test.go", src)
	if err == nil || !strings.Contains(err.Error(), "unknown field at line 8 in a composite literal of type ColumnSummary; declared fields: Name, Count") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "secretValues") {
		t.Errorf("leaked: %v", err)
	}
}

// End to end with a wrong-case spelling of a declared field, where the
// compiler appends its "but does have" hint. The declared fields still reach
// the message and the hint itself is not echoed.
func TestCompileCheckUnknownFieldWrongCase(t *testing.T) {
	src := `package stats

import "testing"

func TestFnSummarize(t *testing.T) {
	s := &ColumnSummary{
		name:  "a",
		count: 1,
	}
	_ = s
}
`
	err := ccCheck(t, "internal/stats/fn_summarize_test.go", src)
	if err == nil || !strings.Contains(err.Error(), "unknown field name at line 7 in a composite literal of type ColumnSummary; declared fields: Name, Count") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "does have") {
		t.Errorf("hint echoed: %v", err)
	}
}

func TestCompileMessageMissingStdImport(t *testing.T) {
	got := compileMessage(&contract.Contracts{}, "internal/render", "package render_test\n", "var w io.Writer", 18, "undefined: io")
	for _, want := range []string{"undefined name at line 18", `package io is used but not imported`, `add "io"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("message %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "unexported") {
		t.Fatalf("message %q gives the unexported advice for a missing import", got)
	}
	if other := compileMessage(&contract.Contracts{}, "internal/render", "package render_test\n", "x := secret.Foo()", 3, "undefined: secret"); strings.Contains(other, "secret") {
		t.Fatalf("message %q quotes a name outside the closed set", other)
	}
}

func TestCompileMessageUndeclaredNameListsTheDeclaredOnes(t *testing.T) {
	c := compileCheckContract()
	m := compileMessage(c, "internal/stats", "package stats\n", "x := pInt(s)", 126, "undefined: pInt")
	if !strings.Contains(m, "undefined name at line 126") || !strings.Contains(m, "not declared in the contract") || strings.Contains(m, "pInt") || strings.Contains(m, "external test package") {
		t.Errorf("message %q", m)
	}
}

func TestCompileMessageCannotAssignToAFunction(t *testing.T) {
	c := compileCheckContract()
	m := compileMessage(c, "internal/stats", "package stats\n", "stats.OpenInput = nil", 164, "cannot assign to stats.OpenInput (neither addressable nor a map index expression)")
	if !strings.HasPrefix(m, "cannot assign at line 164") || strings.Contains(m, "OpenInput") {
		t.Errorf("message %q", m)
	}
	m = compileMessage(c, "internal/stats", "package stats\n", "stats.OpenInput = nil", 166, "use of package stats not in selector")
	if !strings.HasPrefix(m, "cannot assign at line 166") {
		t.Errorf("message %q", m)
	}
}

func TestCompileMessageStdStreamAssignedAForeignType(t *testing.T) {
	c := compileCheckContract()
	m := compileMessage(c, "cmd/csvstat", "package main\n", "os.Stdin = &mockStdin{}", 131, "cannot use &mockStdin{} (value of type *mockStdin) as *os.File value in assignment")
	if !strings.HasPrefix(m, "type mismatch at line 131: os.Stdin, os.Stdout and os.Stderr are *os.File") || strings.Contains(m, "mockStdin") {
		t.Errorf("message %q", m)
	}
}
