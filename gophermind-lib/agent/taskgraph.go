package agent

import (
	"context"
	"fmt"
	"sort"
)

// TaskNode is one node in a task graph: an id, its dependency ids, and an
// optional human description of the work.
type TaskNode struct {
	ID          string   `json:"id"`
	Deps        []string `json:"deps,omitempty"`
	Description string   `json:"description,omitempty"`
}

// TopoSort returns the node ids in a valid dependency order (each node after all
// its deps), using Kahn's algorithm. It errors on a cycle or an unknown
// dependency. Ties are broken by id for deterministic output.
func TopoSort(nodes []TaskNode) ([]string, error) {
	deps := map[string][]string{}
	indeg := map[string]int{}
	for _, n := range nodes {
		if _, dup := deps[n.ID]; dup {
			return nil, fmt.Errorf("duplicate task id %q", n.ID)
		}
		deps[n.ID] = n.Deps
		indeg[n.ID] = 0
	}
	for _, n := range nodes {
		for _, d := range n.Deps {
			if _, ok := deps[d]; !ok {
				return nil, fmt.Errorf("task %q depends on unknown task %q", n.ID, d)
			}
			indeg[n.ID]++
		}
	}
	// Ready set: nodes with no remaining deps, processed in id order.
	var ready []string
	for id, d := range indeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	var order []string
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		// Decrement dependents.
		var newly []string
		for _, n := range nodes {
			for _, d := range n.Deps {
				if d == id {
					indeg[n.ID]--
					if indeg[n.ID] == 0 {
						newly = append(newly, n.ID)
					}
				}
			}
		}
		sort.Strings(newly)
		ready = append(ready, newly...)
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("task graph has a cycle")
	}
	return order, nil
}

// RunGraph executes a task graph's nodes in dependency order, invoking run for
// each node's Description (or ID) and returning each node's result. It stops and
// returns the first error. Nodes are validated (cycles/unknown deps) up front.
func (a *Agent) RunGraph(ctx context.Context, nodes []TaskNode, run TurnFunc) (map[string]string, error) {
	order, err := TopoSort(nodes)
	if err != nil {
		return nil, err
	}
	byID := map[string]TaskNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	results := make(map[string]string, len(order))
	for _, id := range order {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		n := byID[id]
		task := n.Description
		if task == "" {
			task = n.ID
		}
		a.onEvent(Event{Type: "assistant", Text: "▶ executing task: " + id})
		out, err := run(ctx, task)
		if err != nil {
			return results, fmt.Errorf("task %q failed: %w", id, err)
		}
		results[id] = out
	}
	return results, nil
}
