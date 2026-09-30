package executor

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/runner"
)

func loc(file string, line int) runner.Location { return runner.Location{File: file, Line: line} }

func TestAttributeTable(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	p := g.plan
	a, b := p.Leaf("fn-greet"), p.Leaf("fn-farewell")
	if a.Dir != b.Dir {
		t.Fatalf("the fixture changed: %s and %s are not in one package", a.ID, b.ID)
	}

	for _, tc := range []struct {
		name  string
		res   checkResult
		nodes []string
		unatt []string
	}{
		{"build record in a leaf file",
			checkResult{[]checkFailure{{Kind: runner.ClassBuild, Locations: []runner.Location{loc(a.File, 7)}}}},
			[]string{a.ID}, nil},
		{"vet record in a stub path",
			checkResult{[]checkFailure{{Kind: runner.ClassVet, Locations: []runner.Location{loc(b.StubFile, 3)}}}},
			[]string{b.ID}, nil},
		{"build record in another leaf's test file",
			checkResult{[]checkFailure{{Kind: runner.ClassBuild, Locations: []runner.Location{loc(b.TestFile, 12)}}}},
			[]string{b.ID}, nil},
		{"test record by package and top-level name",
			checkResult{[]checkFailure{{Kind: runner.ClassTestFail, Dir: a.Dir, Names: []string{a.TestFunc + "/empty"}}}},
			[]string{a.ID}, nil},
		{"two records for one leaf are one node",
			checkResult{[]checkFailure{
				{Kind: runner.ClassBuild, Locations: []runner.Location{loc(a.File, 7), loc(a.File, 9), loc(a.TestFile, 2)}},
				{Kind: runner.ClassTestFail, Dir: a.Dir, Names: []string{a.TestFunc}}}},
			[]string{a.ID}, nil},
		{"two leaves, both named",
			checkResult{[]checkFailure{{Kind: runner.ClassBuild, Locations: []runner.Location{loc(b.File, 1), loc(a.File, 2)}}}},
			[]string{b.ID, a.ID}, nil},
		{"go.mod is nobody's",
			checkResult{[]checkFailure{{Kind: runner.ClassBuild, Locations: []runner.Location{loc("go.mod", 3)}}}},
			nil, []string{"go.mod:3"}},
		{"a type file is nobody's",
			checkResult{[]checkFailure{{Kind: runner.ClassBuild, Locations: []runner.Location{loc("internal/greet/errors.go", 5)}}}},
			nil, []string{"internal/greet/errors.go:5"}},
		{"a main package no node owns",
			checkResult{[]checkFailure{{Kind: runner.ClassVet, Locations: []runner.Location{loc("cmd/x/main.go", 4)}}}},
			nil, []string{"cmd/x/main.go:4"}},
		{"an owned record beside an unowned one still stops",
			checkResult{[]checkFailure{{Kind: runner.ClassBuild, Locations: []runner.Location{loc(a.File, 1), loc("go.mod", 2)}}}},
			[]string{a.ID}, []string{"go.mod:2"}},
		{"a test name nobody owns falls back to its file",
			checkResult{[]checkFailure{{Kind: runner.ClassTestPanic, Dir: a.Dir, Names: []string{"TestStranger"}, Locations: []runner.Location{loc(b.File, 6)}}}},
			[]string{b.ID}, nil},
		{"a test name nobody owns and no file",
			checkResult{[]checkFailure{{Kind: runner.ClassTestFail, Dir: a.Dir, Names: []string{"TestStranger"}}}},
			nil, []string{a.Dir + " " + runner.ClassTestFail}},
		{"no tests ran names no node",
			checkResult{[]checkFailure{{Kind: runner.ClassNoTestsRan, Dir: a.Dir}}},
			nil, []string{a.Dir + " " + runner.ClassNoTestsRan}},
		{"a timeout with no name and no file",
			checkResult{[]checkFailure{{Kind: runner.ClassTestTimeout, Dir: a.Dir}}},
			nil, []string{a.Dir + " " + runner.ClassTestTimeout}},
		{"a build failure with no location",
			checkResult{[]checkFailure{{Kind: runner.ClassBuild}}},
			nil, []string{runner.ClassBuild}},
		{"a same-named test in another package is not this leaf's",
			checkResult{[]checkFailure{{Kind: runner.ClassTestFail, Dir: "other/pkg", Names: []string{a.TestFunc}}}},
			nil, []string{"other/pkg " + runner.ClassTestFail}},
	} {
		got := p.Attribute(tc.res)
		if !reflect.DeepEqual(got.Nodes, sorted(tc.nodes)) {
			t.Errorf("%s: Nodes = %v, want %v", tc.name, got.Nodes, sorted(tc.nodes))
		}
		if !reflect.DeepEqual(got.Unattributed, tc.unatt) {
			t.Errorf("%s: Unattributed = %v, want %v", tc.name, got.Unattributed, tc.unatt)
		}
	}

	// Need to know: a leaf's lines never hold another leaf's locations or tests.
	got := p.Attribute(checkResult{[]checkFailure{
		{Kind: runner.ClassBuild, Locations: []runner.Location{loc(a.File, 7), loc(b.File, 9)}},
		{Kind: runner.ClassTestFail, Dir: a.Dir, Names: []string{a.TestFunc + "/x", b.TestFunc}},
	}})
	if !reflect.DeepEqual(got.Lines[a.ID], []string{a.File + ":7", a.TestFunc + "/x"}) {
		t.Errorf("Lines[a] = %v", got.Lines[a.ID])
	}
	if !reflect.DeepEqual(got.Lines[b.ID], []string{b.File + ":9", b.TestFunc}) {
		t.Errorf("Lines[b] = %v", got.Lines[b.ID])
	}
	for _, l := range got.Lines[a.ID] {
		if strings.Contains(l, b.File) || strings.Contains(l, b.TestFunc) {
			t.Errorf("a's lines hold b's material: %q", l)
		}
	}
}

func TestAttributeLinesCap(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	a := g.plan.Leaf("fn-greet")
	var locs []runner.Location
	for i := 1; i <= 60; i++ {
		locs = append(locs, loc(a.File, i))
	}
	got := g.plan.Attribute(checkResult{[]checkFailure{{Kind: runner.ClassBuild, Locations: locs}}})
	if len(got.Lines[a.ID]) != 30 {
		t.Fatalf("lines = %d, want 30", len(got.Lines[a.ID]))
	}
	if got.Lines[a.ID][0] != fmt.Sprintf("%s:1", a.File) {
		t.Errorf("first line = %q", got.Lines[a.ID][0])
	}
	if len(got.Nodes) != 1 || len(got.Unattributed) != 0 {
		t.Errorf("nodes %v unattributed %v", got.Nodes, got.Unattributed)
	}
}

func sorted(in []string) []string {
	if in == nil {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
