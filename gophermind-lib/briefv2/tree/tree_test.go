package tree_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

func mustTree(t *testing.T, nodes ...tree.Node) *tree.Tree {
	t.Helper()
	tr, err := tree.NewTree(nodes)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestDuplicateIDs(t *testing.T) {
	if _, err := tree.NewTree([]tree.Node{fn(t, "a", "c", 0), fn(t, "a", "c", 0)}); err == nil {
		t.Fatal("duplicate IDs must error")
	}
}

func TestCycleRejectedWithBothIDs(t *testing.T) {
	tr := mustTree(t, fn(t, "fn-a", "c", 1, "fn-b"), fn(t, "fn-b", "c", 1, "fn-a"))
	err := tr.CycleCheck()
	if err == nil || !strings.Contains(err.Error(), "fn-a") || !strings.Contains(err.Error(), "fn-b") || !strings.Contains(err.Error(), "->") {
		t.Fatalf("want a cycle path naming both nodes, got %v", err)
	}
	if _, err := tr.ComputeWaves(); err == nil {
		t.Error("ComputeWaves must refuse a cyclic tree")
	}
}

func TestSelfCycle(t *testing.T) {
	if err := mustTree(t, fn(t, "fn-a", "c", 1, "fn-a")).CycleCheck(); err == nil {
		t.Fatal("self dependency is a cycle")
	}
}

func TestWavesAreOnePlusMaxOfDependencies(t *testing.T) {
	tr := mustTree(t,
		fn(t, "fn-a", "c", 0),
		fn(t, "fn-b", "c", 1, "fn-a"),
		fn(t, "fn-c", "c", 0),
		fn(t, "fn-d", "c", 2, "fn-b", "fn-c"),
	)
	w, err := tr.ComputeWaves()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"fn-a": 0, "fn-b": 1, "fn-c": 0, "fn-d": 2}
	for id, v := range want {
		if w[id] != v {
			t.Errorf("wave(%s) = %d, want %d", id, w[id], v)
		}
	}
	if err := tr.CheckWaves(); err != nil {
		t.Errorf("consistent waves flagged: %v", err)
	}
}

func TestHandSetWaveThatDisagreesIsAnError(t *testing.T) {
	tr := mustTree(t, fn(t, "fn-a", "c", 0), fn(t, "fn-b", "c", 5, "fn-a"))
	err := tr.CheckWaves()
	if err == nil || !strings.Contains(err.Error(), "fn-b") {
		t.Fatalf("want a wave disagreement naming fn-b, got %v", err)
	}
	if err := tr.AssignWaves(); err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckWaves(); err != nil {
		t.Errorf("AssignWaves should have repaired the wave: %v", err)
	}
}

func TestUnknownDependency(t *testing.T) {
	if _, err := mustTree(t, fn(t, "fn-a", "c", 1, "fn-missing")).ComputeWaves(); err == nil || !strings.Contains(err.Error(), "fn-missing") {
		t.Fatalf("want an unknown-dependency error, got %v", err)
	}
}

// The example tree is a partial excerpt, so only the nodes whose dependencies
// exist are checked (deviation D4). Their file waves must be reproduced.
func TestExampleSubsetWavesReproduced(t *testing.T) {
	tr := mustTree(t,
		readNode(t, "root.json"),
		readNode(t, "types/component.json"),
		readNode(t, "types/fn-validation-error-error.json"),
		readNode(t, "registration/component.json"),
		readNode(t, "registration/fn-validate-email.json"),
		readNode(t, "registration/fn-validate-username.json"),
	)
	if err := tr.CheckWaves(); err != nil {
		t.Fatalf("example waves not reproduced: %v", err)
	}
}

func TestReadiness(t *testing.T) {
	root := readNode(t, "root.json")
	comp := readNode(t, "types/component.json")
	leaf := readNode(t, "types/fn-validation-error-error.json")
	tr := mustTree(t, root, comp, leaf, readNode(t, "registration/fn-validate-email.json"))
	verified := map[string]bool{}
	is := func(id string) bool { return verified[id] }

	if !tr.Ready("fn-validation-error-error", is) {
		t.Error("a function with no dependencies is ready")
	}
	if tr.Ready("fn-validate-email", is) {
		t.Error("fn-validate-email needs fn-validation-error-error verified")
	}
	verified["fn-validation-error-error"] = true
	if !tr.Ready("fn-validate-email", is) || !tr.Ready("types", is) {
		t.Error("dependency and children verified should make both ready")
	}
	if tr.Ready("gm-2026-09-29-001", is) {
		t.Error("root needs every component verified")
	}
	if tr.Ready("nope", is) {
		t.Error("unknown node is never ready")
	}
}

func TestNewTreeRejectsPathCollision(t *testing.T) {
	// function "component" under "types" is written to types/component.json,
	// the same file as the component "types".
	_, err := tree.NewTree([]tree.Node{rootN(t, "rt"), comp(t, "types", "rt"), fn(t, "component", "types", 0)})
	if err == nil || !strings.Contains(err.Error(), "component") || !strings.Contains(err.Error(), "types") || !strings.Contains(err.Error(), "types/component.json") {
		t.Fatalf("want a path collision naming both ids and the path, got %v", err)
	}
}

func TestNewTreeRejectsTwoRoots(t *testing.T) {
	if _, err := tree.NewTree([]tree.Node{rootN(t, "rt-a"), rootN(t, "rt-b")}); err == nil {
		t.Fatal("two roots must be rejected")
	}
}

func TestCheckStructure(t *testing.T) {
	good := mustTree(t, rootN(t, "rt"), comp(t, "c", "rt"), fn(t, "fn-a", "c", 0))
	if err := good.CheckStructure(); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	for _, c := range []struct {
		name  string
		nodes []tree.Node
		want  string
	}{
		{"function under logs", []tree.Node{rootN(t, "rt"), fn(t, "fn-a", "logs", 0)}, "fn-a"},
		{"function parent absent", []tree.Node{rootN(t, "rt"), fn(t, "fn-a", "nope", 0)}, "fn-a"},
		{"component parent not the root", []tree.Node{rootN(t, "rt"), comp(t, "c1", "rt"), comp(t, "c2", "c1")}, "c2"},
		{"component parent absent", []tree.Node{rootN(t, "rt"), comp(t, "c", "gone")}, "c"},
		{"no root", []tree.Node{comp(t, "c", "rt")}, "c"},
		{"function parent is a function", []tree.Node{rootN(t, "rt"), comp(t, "c", "rt"), fn(t, "fn-a", "c", 0), fn(t, "fn-b", "fn-a", 0)}, "fn-b"},
	} {
		tr := mustTree(t, c.nodes...)
		if err := tr.CheckStructure(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error naming %q, got %v", c.name, c.want, err)
		}
	}
	if err := mustTree(t).CheckStructure(); err != nil {
		t.Errorf("empty tree in-memory should be accepted: %v", err)
	}
}
