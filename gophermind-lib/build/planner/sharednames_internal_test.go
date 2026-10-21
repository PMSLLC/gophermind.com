package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

const sharedReaderSrc = `package stats

import "testing"

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) { return 0, nil }

func TestSummarize(t *testing.T) {}
`

func sharedSrc(clause, body string) string { return "package " + clause + "\n\n" + body + "\n" }

func sharedWriteFile(t *testing.T, repo, rel, src string) {
	t.Helper()
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sharedCollide runs the same composition the stage uses.
func sharedCollide(repo, dir, self, src string, st *testwriterState, c *contract.Contracts) error {
	return checkSharedNames(src, otherTestSources(repo, dir, self, st), stubSources(c, dir))
}

func TestSharedNamesSameHelperRefusedWithoutSource(t *testing.T) {
	repo := t.TempDir()
	sharedWriteFile(t, repo, "internal/stats/fn_summarize_test.go", sharedReaderSrc)
	st := &testwriterState{Nodes: map[string]writtenTests{"fn-summarize": {TestFile: "internal/stats/fn_summarize_test.go"}}}
	second := sharedSrc("stats", "type errorReader struct{}\nfunc TestSummarizeFile() {}")
	err := sharedCollide(repo, "internal/stats", "fn-summarize-file", second, st, &contract.Contracts{})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(err.Error(), "errorReader") || !strings.Contains(err.Error(), "prefix every helper") {
		t.Fatalf("message = %q", err)
	}
}

func TestSharedNamesDuplicateTestFuncRefused(t *testing.T) {
	repo := t.TempDir()
	sharedWriteFile(t, repo, "internal/stats/a_test.go", sharedReaderSrc)
	st := &testwriterState{Nodes: map[string]writtenTests{"fn-a": {TestFile: "internal/stats/a_test.go"}}}
	if err := sharedCollide(repo, "internal/stats", "fn-b", sharedSrc("stats", "func TestSummarize() {}"), st, &contract.Contracts{}); err == nil {
		t.Fatal("expected a refusal")
	}
}

func TestSharedNamesDifferentDirFine(t *testing.T) {
	repo := t.TempDir()
	sharedWriteFile(t, repo, "internal/stats/fn_summarize_test.go", sharedReaderSrc)
	st := &testwriterState{Nodes: map[string]writtenTests{"fn-summarize": {TestFile: "internal/stats/fn_summarize_test.go"}}}
	other := sharedSrc("report", "type errorReader struct{}")
	if err := sharedCollide(repo, "internal/report", "fn-render", other, st, &contract.Contracts{}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedNamesHelperInsideTestFuncFine(t *testing.T) {
	repo := t.TempDir()
	sharedWriteFile(t, repo, "internal/stats/fn_summarize_test.go", sharedReaderSrc)
	st := &testwriterState{Nodes: map[string]writtenTests{"fn-summarize": {TestFile: "internal/stats/fn_summarize_test.go"}}}
	inner := sharedSrc("stats", "func TestSummarizeFile() {\n\ttype errorReader struct{}\n\t_ = errorReader{}\n}")
	if err := sharedCollide(repo, "internal/stats", "fn-summarize-file", inner, st, &contract.Contracts{}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedNamesPrefixedHelperFine(t *testing.T) {
	repo := t.TempDir()
	sharedWriteFile(t, repo, "internal/stats/fn_summarize_test.go", sharedReaderSrc)
	st := &testwriterState{Nodes: map[string]writtenTests{"fn-summarize": {TestFile: "internal/stats/fn_summarize_test.go"}}}
	pre := sharedSrc("stats", "type summarizeFileErrorReader struct{}\nfunc TestSummarizeFile() {}")
	if err := sharedCollide(repo, "internal/stats", "fn-summarize-file", pre, st, &contract.Contracts{}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedNamesRewriteDoesNotCollideWithOwnFile(t *testing.T) {
	repo := t.TempDir()
	sharedWriteFile(t, repo, "internal/stats/fn_summarize_test.go", sharedReaderSrc)
	st := &testwriterState{Nodes: map[string]writtenTests{"fn-summarize": {TestFile: "internal/stats/fn_summarize_test.go"}}}
	if err := sharedCollide(repo, "internal/stats", "fn-summarize", sharedReaderSrc, st, &contract.Contracts{}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedNamesStubCollisionRefused(t *testing.T) {
	c := &contract.Contracts{
		Types:     []contract.Type{{ID: "t-row", Package: "internal/stats", File: "internal/stats/row.go", Decl: "type Row struct{ N int }"}},
		Functions: []contract.Function{{ID: "fn-mean", Package: "internal/stats", File: "internal/stats/mean.go", Signature: "func Mean(xs []float64) float64"}},
	}
	st := &testwriterState{}
	for _, body := range []string{"type Row struct{}", "func Mean() {}"} {
		if err := sharedCollide(t.TempDir(), "internal/stats", "fn-x", sharedSrc("stats", body), st, c); err == nil {
			t.Fatalf("stub collision not refused for %q", body)
		}
	}
	// An external test package does not share names with the package under test.
	if err := sharedCollide(t.TempDir(), "internal/stats", "fn-x", sharedSrc("stats_test", "type Row struct{}"), st, c); err != nil {
		t.Fatal(err)
	}
}

func TestSharedNamesContractCollisionGetsDeclaredMessage(t *testing.T) {
	c := &contract.Contracts{
		Functions: []contract.Function{{ID: "fn-parse-flags", Package: "internal/stats", File: "internal/stats/flags.go", Signature: "func parseFlags(args []string) int"}},
	}
	err := sharedCollide(t.TempDir(), "internal/stats", "fn-parse-flags", sharedSrc("stats", "func parseFlags(args []string) int { return 0 }"), &testwriterState{}, c)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if err != errDeclaredName {
		t.Fatalf("message = %q", err)
	}
	for _, bad := range []string{"parseFlags", "return 0", "prefix every helper"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("message leaks %q: %q", bad, err)
		}
	}
	if !strings.Contains(err.Error(), "do not define or stub it") {
		t.Fatalf("message = %q", err)
	}
}

func TestSharedNamesBothKindsGiveDeclaredMessage(t *testing.T) {
	repo := t.TempDir()
	sharedWriteFile(t, repo, "internal/stats/fn_summarize_test.go", sharedReaderSrc)
	st := &testwriterState{Nodes: map[string]writtenTests{"fn-summarize": {TestFile: "internal/stats/fn_summarize_test.go"}}}
	c := &contract.Contracts{
		Functions: []contract.Function{{ID: "fn-mean", Package: "internal/stats", File: "internal/stats/mean.go", Signature: "func Mean(xs []float64) float64"}},
	}
	src := sharedSrc("stats", "type errorReader struct{}\nfunc Mean() {}")
	if err := sharedCollide(repo, "internal/stats", "fn-x", src, st, c); err != errDeclaredName {
		t.Fatalf("err = %v", err)
	}
}

func richDeclContract() *contract.Contracts {
	return &contract.Contracts{
		Types: []contract.Type{
			{ID: "t-col", Package: "internal/stats", File: "internal/stats/col.go", Decl: "type columnState struct {\n\tname string\n\tcount int\n\tdistinct map[string]struct{}\n}"},
			{ID: "t-kind", Package: "internal/stats", File: "internal/stats/col.go", Decl: "type kind int"},
		},
		Functions: []contract.Function{{ID: "fn-fin", Package: "internal/stats", File: "internal/stats/col.go", Signature: "func finalizeAll(n int) int"}},
	}
}

func TestRichDeclaredNameStruct(t *testing.T) {
	c := richDeclContract()
	src := sharedSrc("stats", "import \"testing\"\n\nfunc TestX(t *testing.T) {}\n\ntype columnState struct {\n\tname string\n}")
	err := richDeclaredName(src, c, "internal/stats", errDeclaredName)
	want := "test file declares columnState at line 7, which the package already declares (fields: name, count, distinct); do not define or stub it, build it with a composite literal of those fields and write only tests and test doubles"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
}

func TestRichDeclaredNameFunctionAndType(t *testing.T) {
	c := richDeclContract()
	err := richDeclaredName(sharedSrc("stats", "func finalizeAll(n int) int { return n }"), c, "internal/stats", errDeclaredName)
	if err == nil || !strings.Contains(err.Error(), "finalizeAll at line 3, a function the package already declares") || !strings.Contains(err.Error(), "call it as the package provides it") {
		t.Fatalf("err = %v", err)
	}
	err = richDeclaredName(sharedSrc("stats", "type kind int"), c, "internal/stats", errDeclaredName)
	if err == nil || !strings.Contains(err.Error(), "kind at line 3") || strings.Contains(err.Error(), "fields:") {
		t.Fatalf("err = %v", err)
	}
}

func TestRichDeclaredNameFallsBackWhenNotContractDeclared(t *testing.T) {
	c := richDeclContract()
	for _, src := range []string{
		sharedSrc("stats", "type helperThing struct{}"),
		sharedSrc("stats", "type columnState struct{"),
	} {
		if err := richDeclaredName(src, c, "internal/stats", errDeclaredName); err != errDeclaredName {
			t.Fatalf("err = %v", err)
		}
	}
	if err := richDeclaredName(sharedSrc("stats", "type columnState struct{}"), c, "other/dir", errDeclaredName); err != errDeclaredName {
		t.Fatalf("err = %v", err)
	}
}
