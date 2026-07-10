package agent

import (
	"context"
	"testing"
)

func TestTopoSort(t *testing.T) {
	// c depends on a,b; d depends on c. Valid order: a,b before c before d.
	nodes := []TaskNode{
		{ID: "d", Deps: []string{"c"}},
		{ID: "c", Deps: []string{"a", "b"}},
		{ID: "a"},
		{ID: "b"},
	}
	order, err := TopoSort(nodes)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if pos["a"] > pos["c"] || pos["b"] > pos["c"] || pos["c"] > pos["d"] {
		t.Errorf("dependency order violated: %v", order)
	}
	if len(order) != 4 {
		t.Errorf("expected all 4 nodes, got %v", order)
	}
}

func TestTopoSortCycle(t *testing.T) {
	nodes := []TaskNode{
		{ID: "a", Deps: []string{"b"}},
		{ID: "b", Deps: []string{"a"}},
	}
	if _, err := TopoSort(nodes); err == nil {
		t.Error("a cycle should be detected")
	}
}

func TestTopoSortUnknownDep(t *testing.T) {
	if _, err := TopoSort([]TaskNode{{ID: "a", Deps: []string{"ghost"}}}); err == nil {
		t.Error("an unknown dependency should error")
	}
}

func TestRunGraphExecutesInOrder(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	var seq []string
	run := func(_ context.Context, task string) (string, error) {
		seq = append(seq, task)
		return "did " + task, nil
	}
	nodes := []TaskNode{
		{ID: "build", Deps: []string{"compile"}, Description: "build"},
		{ID: "compile", Description: "compile"},
	}
	results, err := a.RunGraph(context.Background(), nodes, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(seq) != 2 || seq[0] != "compile" || seq[1] != "build" {
		t.Errorf("execution order wrong: %v", seq)
	}
	if results["build"] != "did build" {
		t.Errorf("results wrong: %v", results)
	}
}
