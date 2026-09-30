package executor

import (
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/tree"
)

// TestGreeterFixturePlanIsApproved proves the fixture before any executor
// code relies on it: the real planner, run offline over the scripted replies,
// leaves an approved plan of five leaves in three waves.
func TestGreeterFixturePlanIsApproved(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	if err := planner.VerifyApproval(g.runDir); err != nil {
		t.Fatalf("VerifyApproval: %v", err)
	}

	tr, err := tree.NewStore(g.runDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	waves, err := tr.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}
	// The tree numbers waves from 0: the first leaves to run are wave 0.
	want := map[string]int{"fn-farewell": 0, "fn-greet": 0, "fn-bye": 1, "fn-hello": 1, "fn-serve": 2}
	got := map[string]int{}
	for id, n := range tr.Nodes {
		if n.Kind == tree.KindFunction {
			got[id] = waves[id]
		}
	}
	if len(got) != len(want) {
		t.Fatalf("function nodes = %v, want %v", got, want)
	}
	for id, w := range want {
		if gw, ok := got[id]; !ok || gw != w {
			t.Errorf("wave of %s = %d (present %v), want %d", id, gw, ok, w)
		}
	}

	tests, err := planner.ReadLeafTests(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 5 {
		t.Fatalf("leaf_tests has %d entries, want 5", len(tests))
	}
	var files []string
	for id, lt := range tests {
		if _, ok := want[id]; !ok {
			t.Errorf("leaf_tests names unexpected node %s", id)
		}
		if !fileExists(g.repo + "/" + lt.TestFile) {
			t.Errorf("test file %s of %s does not exist", lt.TestFile, id)
		}
		files = append(files, lt.TestFile)
	}

	reqs, err := planner.ReadRequirements(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	acceptance := 0
	for _, q := range reqs {
		if q.Kind == planner.ReqAcceptance {
			acceptance++
		}
	}
	if len(reqs) != 7 || acceptance != 2 {
		t.Errorf("requirements = %d with %d acceptance, want 7 and 2", len(reqs), acceptance)
	}
	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cov.Covered) != 7 {
		t.Errorf("coverage covers %d requirements, want 7", len(cov.Covered))
	}
	rootFor := map[string]bool{}
	for _, rt := range cov.RootTests {
		rootFor[rt.Requirement] = true
	}
	for _, id := range []string{"A1", "A2", "C1"} {
		if !rootFor[id] {
			t.Errorf("no root test for %s", id)
		}
	}

	var untracked []string
	for _, line := range strings.Split(strings.TrimSpace(g.gitCmd("status", "--porcelain", "--untracked-files=all")), "\n") {
		if !strings.HasPrefix(line, "?? ") {
			t.Errorf("git status line %q is not an untracked file", line)
		}
		untracked = append(untracked, strings.TrimPrefix(line, "?? "))
	}
	sort.Strings(untracked)
	sort.Strings(files)
	if strings.Join(untracked, ",") != strings.Join(files, ",") {
		t.Errorf("untracked files = %v, want exactly the test files %v", untracked, files)
	}
}
