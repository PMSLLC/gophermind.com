package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/planner"
	"gophermind/gophermind-lib/build/tree"
)

const statsPkg = "csvstat/internal/stats"

func csvstatSharedContract() *contract.Contracts {
	return &contract.Contracts{Module: "csvstat",
		Types: []contract.Type{{ID: "ty-column-state", Package: statsPkg, File: "internal/stats/stats.go", Decl: "type columnState struct{ n int }"}},
		Functions: []contract.Function{
			{ID: "fn-finalize-column-state", Package: statsPkg, File: "internal/stats/stats.go", Signature: "func finalizeColumnState(s *columnState) int"},
			{ID: "fn-update-column-state", Package: statsPkg, File: "stats.go", Signature: "func updateColumnState(s *columnState, v string)"},
			{ID: "fn-mean", Package: statsPkg, File: "internal/stats/stats.go", Signature: "func (s *columnState) Mean() float64"},
		}}
}

// A saved contract with the csvstat shape (several functions and a type in one
// file) gives every leaf its own file, and the type file stays the type's.
func TestLoadPlanSharedFilesGetDistinctLeafFiles(t *testing.T) {
	c := csvstatSharedContract()
	fns, moved := indexFunctionsMoved(c)
	if len(moved) != 3 {
		t.Fatalf("moved = %v", moved)
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "internal", "stats"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &Plan{Contracts: c, Classes: map[string]string{}, moved: moved}
	tests := map[string]planner.LeafTest{}
	var leaves []*Leaf
	for _, f := range c.Functions {
		tf := "internal/stats/" + strings.ReplaceAll(f.ID, "-", "_") + "_test.go"
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(tf)), []byte("package stats\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		tests[f.ID] = planner.LeafTest{TestFile: tf, TestFunc: "TestX", SHA256: "x"}
		// the node still carries the file the saved contract had
		node := fmt.Sprintf(`{"spec_version": "2.0", "id": %q, "kind": "function", "parent": "p", "title": "t", "description": "d",
  "brief_ref": "#x", "status": "pending", "wave": 0,
  "contract": {"file": %q, "package": %q, "signature": %q, "inputs": [], "outputs": []},
  "tests": [{"name": "t", "level": "unit", "given": "g", "expect": "e", "command": "go test ./internal/stats"}]}`,
			f.ID, oldFile(f.ID), f.Package, f.Signature)
		n, err := tree.ParseNode([]byte(node))
		if err != nil {
			t.Fatal(err)
		}
		l, err := p.leaf(n, nil, fns, tests, repo)
		if err != nil {
			t.Fatalf("%s: %v", f.ID, err)
		}
		if l.File == "internal/stats/stats.go" || l.Dir != "internal/stats" || l.Package != "stats" {
			t.Errorf("%s: File %q Dir %q Package %q", f.ID, l.File, l.Dir, l.Package)
		}
		leaves = append(leaves, l)
	}
	if err := checkLeafFiles(c, leaves); err != nil {
		t.Fatal(err)
	}
	if c.Types[0].File != "internal/stats/stats.go" {
		t.Errorf("type file = %q", c.Types[0].File)
	}
}

func oldFile(id string) string {
	if id == "fn-update-column-state" {
		return "stats.go"
	}
	return "internal/stats/stats.go"
}

// A node whose file matches neither the contract's nor the saved contract's
// file is still refused.
func TestLeafWithAForeignFileIsStillRefused(t *testing.T) {
	c := csvstatSharedContract()
	fns, moved := indexFunctionsMoved(c)
	p := &Plan{Contracts: c, Classes: map[string]string{}, moved: moved}
	node := `{"spec_version": "2.0", "id": "fn-mean", "kind": "function", "parent": "p", "title": "t", "description": "d",
  "brief_ref": "#x", "status": "pending", "wave": 0,
  "contract": {"file": "internal/stats/other.go", "package": "csvstat/internal/stats", "signature": "func (s *columnState) Mean() float64", "inputs": [], "outputs": []},
  "tests": [{"name": "t", "level": "unit", "given": "g", "expect": "e", "command": "go test ./internal/stats"}]}`
	n, err := tree.ParseNode([]byte(node))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.leaf(n, nil, fns, map[string]planner.LeafTest{}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "does not match contracts.json") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckLeafFilesRefusesSharing(t *testing.T) {
	c := csvstatSharedContract()
	a := &Leaf{ID: "fn-a", File: "internal/stats/a.go"}
	b := &Leaf{ID: "fn-b", File: "internal/stats/a.go"}
	if err := checkLeafFiles(c, []*Leaf{a, b}); err == nil || err.Error() != "executor: leaves fn-a and fn-b have the same file" {
		t.Errorf("shared leaf file: %v", err)
	}
	ty := &Leaf{ID: "fn-t", File: "internal/stats/stats.go"}
	if err := checkLeafFiles(c, []*Leaf{a, ty}); err == nil || err.Error() != "executor: leaf fn-t has the same file as type ty-column-state" {
		t.Errorf("type file: %v", err)
	}
	if err := checkLeafFiles(c, []*Leaf{a, {ID: "fn-c", File: "internal/stats/c.go"}}); err != nil {
		t.Errorf("distinct files: %v", err)
	}
}
