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
	others := append(otherTestSources(repo, dir, self, st), stubSources(c, dir)...)
	return checkSharedNames(src, others)
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
