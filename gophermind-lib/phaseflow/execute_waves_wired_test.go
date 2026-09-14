package phaseflow

import (
	"context"
	"testing"
)

// AssignWaves was a pure function nothing in production ever called, so
// every task kept Wave 0, every wave-0 group runs at an effective
// concurrency of 1, and the whole of piece 3 was unreachable: a plan could
// declare depends_on and still execute strictly one task at a time in ID
// order.
//
// Executing a plan that declares dependencies must derive its waves and
// persist them, so the run is ordered by the dependency graph and the
// dashboard (which reads the same file) can show it.
func TestExecuteDerivesWavesFromDependencies(t *testing.T) {
	root := t.TempDir()
	a := Assignments{Tasks: []Task{
		{ID: "c", Agent: "x", Model: "m", Status: StatusPending, DependsOn: []string{"b"}},
		{ID: "b", Agent: "x", Model: "m", Status: StatusPending, DependsOn: []string{"a"}},
		{ID: "a", Agent: "x", Model: "m", Status: StatusPending},
	}}
	if err := a.Save(root); err != nil {
		t.Fatal(err)
	}

	if _, err := Execute(context.Background(), root, okRunner{}, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got, found, err := LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("reload: %v found=%v", err, found)
	}
	want := map[string]int{"a": 1, "b": 2, "c": 3}
	for _, task := range got.Tasks {
		if w := want[task.ID]; task.Wave != w {
			t.Errorf("task %s wave = %d, want %d", task.ID, task.Wave, w)
		}
	}
}

// A plan that declares no dependencies keeps every task in wave 0, which is
// what makes it run one at a time exactly as it always has. Deriving waves
// for such a plan would silently turn every existing sequential plan into a
// four-way concurrent one.
func TestExecuteLeavesADependencyFreePlanInWaveZero(t *testing.T) {
	root := t.TempDir()
	a := Assignments{Tasks: []Task{
		{ID: "a", Agent: "x", Model: "m", Status: StatusPending},
		{ID: "b", Agent: "x", Model: "m", Status: StatusPending},
	}}
	if err := a.Save(root); err != nil {
		t.Fatal(err)
	}

	if _, err := Execute(context.Background(), root, okRunner{}, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got, _, _ := LoadAssignments(root)
	for _, task := range got.Tasks {
		if task.Wave != 0 {
			t.Errorf("task %s wave = %d, want 0 for a plan with no depends_on",
				task.ID, task.Wave)
		}
	}
}

// A dependency on a task id that does not exist is a typo in the plan, and
// running it anyway would start a task before the input it names exists.
func TestExecuteRefusesAPlanWithAnUnknownDependency(t *testing.T) {
	root := t.TempDir()
	a := Assignments{Tasks: []Task{
		{ID: "a", Agent: "x", Model: "m", Status: StatusPending, DependsOn: []string{"typo"}},
	}}
	if err := a.Save(root); err != nil {
		t.Fatal(err)
	}

	r := &countingRunner{}
	if _, err := Execute(context.Background(), root, r, nil); err == nil {
		t.Error("expected an error for a dependency on an unknown task id")
	}
	if r.calls != 0 {
		t.Errorf("runner was called %d times; a plan with a broken dependency "+
			"graph must not run at all", r.calls)
	}
}

type okRunner struct{}

func (okRunner) Run(context.Context, Task) (string, string, error) {
	return StatusDone, "ok", nil
}

type countingRunner struct{ calls int }

func (r *countingRunner) Run(context.Context, Task) (string, string, error) {
	r.calls++
	return StatusDone, "ok", nil
}
