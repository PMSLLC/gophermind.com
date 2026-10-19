package planner

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

const renameFirstSrc = `package render

import "testing"

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, nil }

func TestRenderJSON(t *testing.T) {}
`

const renameSecondSrc = `package render

import (
	"io"
	"testing"
)

type failingWriter struct{ n int }

func (w *failingWriter) Write(p []byte) (int, error) { w.n++; return 0, io.ErrClosedPipe }

var _ io.Writer = (*failingWriter)(nil)

func newW() io.Writer { return &failingWriter{n: 1} }

func TestRenderText(t *testing.T) {
	var w io.Writer = &failingWriter{}
	if fw, ok := w.(*failingWriter); ok {
		_ = fw
	}
	_ = []failingWriter{{}}
	_ = struct{ f failingWriter }{}
	_ = newW()
}
`

func TestResolveSharedNamesRenamesHelperEverywhere(t *testing.T) {
	out, renamed, err := resolveSharedNames(renameSecondSrc, "renderText", []string{renameFirstSrc}, nil)
	if err != nil || !renamed {
		t.Fatalf("renamed=%v err=%v", renamed, err)
	}
	if strings.Contains(out, "failingWriter") {
		t.Fatalf("old name left:\n%s", out)
	}
	if got := strings.Count(out, "renderTextFailingWriter"); got != 8 {
		t.Fatalf("renamed idents = %d:\n%s", got, out)
	}
	checkTypes(t, out, renameFirstSrc)
}

func TestResolveSharedNamesLowerFirstLetterKeepsCase(t *testing.T) {
	src := "package render\n\ntype Helper struct{}\nvar failingWriter = 1\nfunc TestX() {}\n"
	other := "package render\n\ntype Helper struct{}\nvar failingWriter = 2\n"
	out, _, err := resolveSharedNames(src, "RenderText", []string{other}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "type RenderTextHelper struct") || !strings.Contains(out, "var renderTextFailingWriter") {
		// lower-case original gives lowerCamel prefix of the node name
		t.Fatalf("out:\n%s", out)
	}
}

func TestResolveSharedNamesLeavesShadowingLocalAlone(t *testing.T) {
	src := `package render

type failingWriter struct{}

func TestRenderText() {
	failingWriter := 3
	_ = failingWriter
}

func useType() {
	var v failingWriter
	_ = v
}

func helperUsesLocal() int {
	failingWriter := 1
	return failingWriter
}
`
	out, renamed, err := resolveSharedNames(src, "renderText", []string{renameFirstSrc}, nil)
	if err != nil || !renamed {
		t.Fatalf("renamed=%v err=%v", renamed, err)
	}
	if !strings.Contains(out, "failingWriter := 3") || !strings.Contains(out, "_ = failingWriter") || !strings.Contains(out, "return failingWriter") {
		t.Fatalf("local changed:\n%s", out)
	}
	if !strings.Contains(out, "type renderTextFailingWriter struct{}") || !strings.Contains(out, "var v renderTextFailingWriter") {
		t.Fatalf("type not renamed:\n%s", out)
	}
}

func TestResolveSharedNamesTestFuncCollisionStillRefused(t *testing.T) {
	src := "package render\n\nfunc TestRenderJSON() {}\n"
	_, renamed, err := resolveSharedNames(src, "renderJSON", []string{renameFirstSrc}, nil)
	if err != errSharedName || renamed {
		t.Fatalf("err=%v renamed=%v", err, renamed)
	}
}

func TestResolveSharedNamesDeclaredCollisionStillRefused(t *testing.T) {
	c := &contract.Contracts{
		Functions: []contract.Function{{ID: "fn-mean", Package: "render", File: "render/mean.go", Signature: "func Mean(xs []float64) float64"}},
	}
	src := "package render\n\ntype failingWriter struct{}\nfunc Mean() {}\n"
	_, renamed, err := resolveSharedNames(src, "mean", []string{renameFirstSrc}, stubSources(c, "render"))
	if err != errDeclaredName || renamed {
		t.Fatalf("err=%v renamed=%v", err, renamed)
	}
}

func TestResolveSharedNamesRenamedNameCollidesFallsBack(t *testing.T) {
	other := renameFirstSrc + "\ntype renderTextFailingWriter struct{}\n"
	_, renamed, err := resolveSharedNames(renameSecondSrc, "renderText", []string{other}, nil)
	if err != errSharedName || renamed {
		t.Fatalf("err=%v renamed=%v", err, renamed)
	}
}

func TestResolveSharedNamesNoCollisionUnchanged(t *testing.T) {
	out, renamed, err := resolveSharedNames("package render\n\ntype a struct{}\n", "x", []string{renameFirstSrc}, nil)
	if err != nil || renamed || out != "package render\n\ntype a struct{}\n" {
		t.Fatalf("out=%q renamed=%v err=%v", out, renamed, err)
	}
}

func TestResolveSharedNamesSecondNodeDoesNotCollide(t *testing.T) {
	first, _, err := resolveSharedNames(renameSecondSrc, "renderText", []string{renameFirstSrc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	third := strings.ReplaceAll(renameSecondSrc, "TestRenderText", "TestRenderYAML")
	out, renamed, err := resolveSharedNames(third, "renderYAML", []string{renameFirstSrc, first}, nil)
	if err != nil || !renamed {
		t.Fatalf("renamed=%v err=%v", renamed, err)
	}
	if !strings.Contains(out, "renderYAMLFailingWriter") {
		t.Fatalf("out:\n%s", out)
	}
	// A node whose own helper is already prefixed is not touched.
	if _, renamed, err := resolveSharedNames(first, "renderText", []string{renameFirstSrc}, nil); err != nil || renamed {
		t.Fatalf("renamed=%v err=%v", renamed, err)
	}
	checkTypes(t, out, renameFirstSrc, first)
}

// checkTypes type-checks the files as one package; any error fails the test.
func checkTypes(t *testing.T, srcs ...string) {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for i, s := range srcs {
		f, err := parser.ParseFile(fset, string(rune('a'+i))+".go", s, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check("render", fset, files, nil); err != nil {
		t.Fatalf("type check: %v", err)
	}
}
