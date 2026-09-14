package codeindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes a small Go tree and returns its root.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("store/store.go", `// Package store persists widgets.
package store

import "errors"

// ErrMissing is returned when a widget is absent.
var ErrMissing = errors.New("missing")

// MaxItems caps the store. It is a hard limit.
const MaxItems = 100

// Widget is a thing that is stored.
type Widget struct {
	Name string
}

// Keeper reads and writes widgets.
type Keeper interface {
	Get(name string) (Widget, error)
}

// Save writes w to disk and returns any error.
func Save(w Widget) error { return nil }

// Rename changes the widget's name.
func (w *Widget) Rename(to string) { w.Name = to }

func unexportedHelper(x int) int { return x }
`)
	write("store/store_test.go", `package store

import "testing"

func TestNotIndexed(t *testing.T) {}
`)
	write("vendor/other/lib.go", `package other

func VendoredFunc() {}
`)
	write("dist/build.go", `package dist

func DistFunc() {}
`)
	write("store/testdata/sample.go", `package testdata

func TestdataFunc() {}
`)
	return root
}

func names(idx *Index) map[string]Entry {
	m := map[string]Entry{}
	for _, e := range idx.Entries {
		m[e.Name] = e
	}
	return m
}

// TestBuildExtractsEveryKind covers funcs, methods, types, interfaces, consts
// and vars, including unexported ones.
func TestBuildExtractsEveryKind(t *testing.T) {
	idx, err := Build(fixture(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	by := names(idx)

	for _, want := range []string{"Save", "Rename", "Widget", "Keeper", "MaxItems", "ErrMissing", "unexportedHelper"} {
		if _, ok := by[want]; !ok {
			t.Errorf("index missing %q; have %v", want, by)
		}
	}

	if got := by["Save"].Kind; got != KindFunc {
		t.Errorf("Save kind = %q, want %q", got, KindFunc)
	}
	if got := by["Rename"]; got.Kind != KindMethod || !strings.Contains(got.Recv, "Widget") {
		t.Errorf("Rename = %+v, want a method on Widget", got)
	}
	if got := by["Keeper"].Kind; got != KindInterface {
		t.Errorf("Keeper kind = %q, want %q", got, KindInterface)
	}
	if got := by["Widget"].Kind; got != KindType {
		t.Errorf("Widget kind = %q, want %q", got, KindType)
	}
	if got := by["MaxItems"].Kind; got != KindConst {
		t.Errorf("MaxItems kind = %q, want %q", got, KindConst)
	}
	if got := by["ErrMissing"].Kind; got != KindVar {
		t.Errorf("ErrMissing kind = %q, want %q", got, KindVar)
	}
}

// TestBuildRecordsLocationAndDoc: the point of the index is jump targets plus a
// one-line hint, so both must be populated.
func TestBuildRecordsLocationAndDoc(t *testing.T) {
	idx, err := Build(fixture(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	save := names(idx)["Save"]

	if save.File != filepath.Join("store", "store.go") {
		t.Errorf("File = %q, want a repo-relative path", save.File)
	}
	if save.Line <= 0 {
		t.Errorf("Line = %d, want > 0", save.Line)
	}
	if !strings.Contains(save.Signature, "func(w Widget) error") {
		t.Errorf("Signature = %q", save.Signature)
	}
	if !strings.HasPrefix(save.Doc, "Save writes w to disk") {
		t.Errorf("Doc = %q", save.Doc)
	}
	// Only the first sentence, so the table stays one line per symbol.
	if strings.Contains(names(idx)["MaxItems"].Doc, "hard limit") {
		t.Errorf("Doc kept more than the first sentence: %q", names(idx)["MaxItems"].Doc)
	}
}

// TestBuildSkipsExcludedTrees keeps generated, vendored and test material out.
func TestBuildSkipsExcludedTrees(t *testing.T) {
	idx, err := Build(fixture(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	by := names(idx)
	for _, unwanted := range []string{"TestNotIndexed", "VendoredFunc", "DistFunc", "TestdataFunc"} {
		if _, ok := by[unwanted]; ok {
			t.Errorf("%q should not be indexed", unwanted)
		}
	}
}

// TestRenderIsDeterministic is what makes a per-task hook tolerable: no
// timestamp, stable ordering, so an unchanged tree yields an identical file.
func TestRenderIsDeterministic(t *testing.T) {
	root := fixture(t)
	a, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if a.Render() != b.Render() {
		t.Error("two builds of the same tree rendered differently")
	}
	if strings.Contains(strings.ToLower(a.Render()), "generated at") {
		t.Error("render contains a timestamp; that would churn the file on every run")
	}
}

// TestRenderGroupsByPackage: the file is meant to be read, not just grepped.
func TestRenderGroupsByPackage(t *testing.T) {
	idx, err := Build(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	out := idx.Render()
	if !strings.Contains(out, "store") {
		t.Error("package name missing from render")
	}
	for _, want := range []string{"Save", "Widget", "store/store.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

// TestWriteCreatesIndexFile checks the artifact lands where the agent expects.
func TestWriteCreatesIndexFile(t *testing.T) {
	root := fixture(t)
	idx, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Write(root); err != nil {
		t.Fatalf("Write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		t.Fatalf("read %s: %v", FileName, err)
	}
	if !strings.Contains(string(b), "Save") {
		t.Error("written index is missing content")
	}
}

// TestLookupMatchesCaseInsensitiveSubstring is how the tool answers
// "where is X".
func TestLookupMatchesCaseInsensitiveSubstring(t *testing.T) {
	idx, err := Build(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	hits := idx.Lookup("save", "", 10)
	if len(hits) != 1 || hits[0].Name != "Save" {
		t.Fatalf("Lookup(save) = %+v, want just Save", hits)
	}

	if got := idx.Lookup("widget", "", 10); len(got) < 2 {
		t.Errorf("Lookup(widget) = %d hits, want the type and its method", len(got))
	}
}

// TestLookupFiltersByKind narrows a noisy query.
func TestLookupFiltersByKind(t *testing.T) {
	idx, err := Build(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	hits := idx.Lookup("widget", KindMethod, 10)
	for _, h := range hits {
		if h.Kind != KindMethod {
			t.Errorf("kind filter leaked %+v", h)
		}
	}
	if len(hits) == 0 {
		t.Error("kind filter dropped everything")
	}
}

// TestLookupRespectsLimit keeps a broad query from flooding the model's context.
func TestLookupRespectsLimit(t *testing.T) {
	idx, err := Build(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.Lookup("", "", 2); len(got) != 2 {
		t.Errorf("len = %d, want the limit of 2", len(got))
	}
}
