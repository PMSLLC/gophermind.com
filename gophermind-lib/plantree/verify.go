package plantree

import (
	"fmt"
	"strings"
)

// Verify checks the rules that span nodes: every depends_on id exists, and
// the dependency graph has no cycle. Node-local rules are Validate's.
func (r *Repo) Verify() error {
	deps := map[string][]string{}
	var order []string
	err := r.Walk(func(n Node) error {
		deps[n.ID] = n.DependsOn
		order = append(order, n.ID)
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range order {
		for _, d := range deps[id] {
			if _, ok := deps[d]; !ok {
				return fmt.Errorf("plantree: %s depends on %s, which does not exist", id, d)
			}
		}
	}

	const (
		unseen = iota
		inStack
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(id string) error
	visit = func(id string) error {
		state[id] = inStack
		stack = append(stack, id)
		for _, d := range deps[id] {
			switch state[d] {
			case inStack:
				start := 0
				for i, s := range stack {
					if s == d {
						start = i
						break
					}
				}
				cycle := append(append([]string{}, stack[start:]...), d)
				return fmt.Errorf("plantree: dependency cycle: %s", strings.Join(cycle, " -> "))
			case unseen:
				if err := visit(d); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
		return nil
	}
	for _, id := range order {
		if state[id] == unseen {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}
