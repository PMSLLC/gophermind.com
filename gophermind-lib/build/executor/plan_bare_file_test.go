package executor

import (
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/tree"
)

const bareLeafNode = `{
  "spec_version": "2.0", "id": "fn-format-summary", "kind": "function", "parent": "output-formats",
  "title": "formatSummary", "description": "d", "brief_ref": "#x", "status": "pending", "wave": 0,
  "contract": {"file": "json.go", "package": "csvstat/internal/render", "signature": "func formatSummary(n int) string", "inputs": [], "outputs": []},
  "tests": [{"name": "t", "level": "unit", "given": "g", "expect": "e", "command": "go test ./internal/render -run ^TestFormatSummary$"}]
}`

// A function whose file is a bare name (an older contracts.json) becomes a
// leaf with a repo-relative file, directory and stub path, and the recorded
// test file beside it.
func TestLeafOfABareFileNameIsRepoRelative(t *testing.T) {
	c := &contract.Contracts{Module: "csvstat", Functions: []contract.Function{
		{ID: "fn-format-summary", Package: "csvstat/internal/render", File: "json.go", Signature: "func formatSummary(n int) string"}}}
	fns := indexFunctions(c)

	repo := t.TempDir()
	testFile := "internal/render/fn_format_summary_test.go"
	if err := os.MkdirAll(filepath.Join(repo, "internal", "render"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(testFile)), []byte("package render\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := tree.ParseNode([]byte(bareLeafNode))
	if err != nil {
		t.Fatal(err)
	}
	p := &Plan{Contracts: c, Classes: map[string]string{}}
	tests := map[string]planner.LeafTest{"fn-format-summary": {TestFile: testFile, TestFunc: "TestFormatSummary", SHA256: "x"}}
	l, err := p.leaf(n, nil, fns, tests, repo)
	if err != nil {
		t.Fatal(err)
	}
	if l.File != "internal/render/json.go" || l.Dir != "internal/render" ||
		l.StubFile != "internal/render/zz_gm_stub_fn-format-summary.go" || l.Package != "render" {
		t.Errorf("File %q Dir %q StubFile %q Package %q", l.File, l.Dir, l.StubFile, l.Package)
	}
	if filepath.Dir(l.TestFile) != filepath.Dir(l.File) {
		t.Errorf("test file %q is not beside %q", l.TestFile, l.File)
	}
}
