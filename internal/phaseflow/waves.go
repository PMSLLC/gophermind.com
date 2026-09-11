package phaseflow

import (
	"errors"
	"fmt"
	"strings"
)

// This file computes each task's execution wave from its DependsOn edges
// (pipeline piece 3, see docs/superpowers/specs
// /2026-09-11-harness-pipeline-design.md). AssignWaves is a pure function: it
// does no I/O and never mutates its input, so it is safe to call as often as
// a caller needs a fresh wave numbering (for example, once at plan-creation
// time, or again whenever a plan's DependsOn edges change).

// ErrCyclicDependency reports a dependency cycle, naming the tasks involved.
var ErrCyclicDependency = errors.New("phaseflow: cyclic task dependency")

// AssignWaves computes each task's wave from its DependsOn edges: a task's
// wave is one more than the highest wave among its dependencies, and a task
// with no dependencies is wave 1. Wave 0 is reserved for the contract step
// and is never assigned here; see Task.IsContract.
//
// A DependsOn id that does not name another task in tasks is an error, not a
// silently dropped edge - a typo in a plan must not quietly turn a dependent
// task into a wave-1 task that runs before the thing it needs exists. A
// dependency cycle is also an error, wrapping ErrCyclicDependency and naming
// the participating ids.
//
// AssignWaves returns a new slice; it does not mutate tasks.
func AssignWaves(tasks []Task) ([]Task, error) {
	index := make(map[string]int, len(tasks))
	for i, t := range tasks {
		index[t.ID] = i
	}

	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := index[dep]; !ok {
				return nil, fmt.Errorf("phaseflow: task %q depends on unknown task %q", t.ID, dep)
			}
		}
	}

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make([]int, len(tasks))
	wave := make([]int, len(tasks))
	var stack []string

	var visit func(i int) error
	visit = func(i int) error {
		switch state[i] {
		case visited:
			return nil
		case visiting:
			cycle := append(append([]string{}, stack...), tasks[i].ID)
			return fmt.Errorf("%w: %s", ErrCyclicDependency, strings.Join(cycle, " -> "))
		}

		state[i] = visiting
		stack = append(stack, tasks[i].ID)

		max := 0
		for _, dep := range tasks[i].DependsOn {
			di := index[dep]
			if err := visit(di); err != nil {
				return err
			}
			if wave[di] > max {
				max = wave[di]
			}
		}

		stack = stack[:len(stack)-1]
		wave[i] = max + 1
		state[i] = visited
		return nil
	}

	for i := range tasks {
		if err := visit(i); err != nil {
			return nil, err
		}
	}

	out := make([]Task, len(tasks))
	copy(out, tasks)
	for i := range out {
		out[i].Wave = wave[i]
	}
	return out, nil
}
