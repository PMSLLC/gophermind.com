package tree

import (
	"fmt"
	"sort"
	"strings"
)

type Tree struct{ Nodes map[string]Node }

func NewTree(nodes []Node) (*Tree, error) {
	t := &Tree{Nodes: make(map[string]Node, len(nodes))}
	paths := map[string]string{}
	root := ""
	for _, n := range nodes {
		if _, dup := t.Nodes[n.ID]; dup {
			return nil, fmt.Errorf("tree: duplicate node id %q", n.ID)
		}
		if other, clash := paths[n.Path()]; clash {
			return nil, fmt.Errorf("tree: nodes %q and %q share the path %s", other, n.ID, n.Path())
		}
		if n.Kind == KindRoot {
			if root != "" {
				return nil, fmt.Errorf("tree: more than one root node (%q and %q)", root, n.ID)
			}
			root = n.ID
		}
		paths[n.Path()] = n.ID
		t.Nodes[n.ID] = n
	}
	return t, nil
}

// CheckStructure verifies the shape a complete tree must have: exactly one
// root (with no parent), every component's parent is that root, and every
// function's parent is a component in the tree. An empty tree is fine.
func (t *Tree) CheckStructure() error {
	if len(t.Nodes) == 0 {
		return nil
	}
	roots := 0
	for _, id := range t.ids() {
		n := t.Nodes[id]
		switch n.Kind {
		case KindRoot:
			roots++
			if n.Parent != "" {
				return fmt.Errorf("tree: root %q must not have a parent (has %q)", id, n.Parent)
			}
		case KindComponent:
			p, ok := t.Nodes[n.Parent]
			if !ok || p.Kind != KindRoot {
				return fmt.Errorf("tree: component %q: parent %q is not a root node in the tree", id, n.Parent)
			}
		default:
			p, ok := t.Nodes[n.Parent]
			if !ok || p.Kind != KindComponent {
				return fmt.Errorf("tree: function %q: parent %q is not a component in the tree", id, n.Parent)
			}
		}
	}
	if roots != 1 {
		return fmt.Errorf("tree: want exactly one root node, found %d", roots)
	}
	return nil
}

func (t *Tree) ids() []string {
	ids := make([]string, 0, len(t.Nodes))
	for id := range t.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// CycleCheck reports the first dependency cycle as "a -> b -> a". Edges to
// nodes not in the tree are ignored here (ComputeWaves reports those).
func (t *Tree) CycleCheck() error {
	const (
		white = iota
		grey
		black
	)
	state := map[string]int{}
	var stack []string
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case grey:
			i := 0
			for j, s := range stack {
				if s == id {
					i = j
				}
			}
			cycle := append(append([]string{}, stack[i:]...), id)
			return fmt.Errorf("tree: dependency cycle: %s", strings.Join(cycle, " -> "))
		case black:
			return nil
		}
		state[id] = grey
		stack = append(stack, id)
		for _, d := range t.Nodes[id].DependsOn {
			if _, ok := t.Nodes[d]; !ok {
				continue
			}
			if err := visit(d); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = black
		return nil
	}
	for _, id := range t.ids() {
		if state[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

// ComputeWaves returns wave(n) = 0 with no depends_on, else 1 + max over
// depends_on, for every node kind (deviation D2).
func (t *Tree) ComputeWaves() (map[string]int, error) {
	if err := t.CycleCheck(); err != nil {
		return nil, err
	}
	memo := map[string]int{}
	var wave func(id string) (int, error)
	wave = func(id string) (int, error) {
		if w, ok := memo[id]; ok {
			return w, nil
		}
		w := 0
		for _, d := range t.Nodes[id].DependsOn {
			if _, ok := t.Nodes[d]; !ok {
				return 0, fmt.Errorf("tree: node %q depends on unknown node %q", id, d)
			}
			dw, err := wave(d)
			if err != nil {
				return 0, err
			}
			if dw+1 > w {
				w = dw + 1
			}
		}
		memo[id] = w
		return w, nil
	}
	for _, id := range t.ids() {
		if _, err := wave(id); err != nil {
			return nil, err
		}
	}
	return memo, nil
}

// AssignWaves overwrites every node's wave with the computed value.
func (t *Tree) AssignWaves() error {
	w, err := t.ComputeWaves()
	if err != nil {
		return err
	}
	for id, v := range w {
		n := t.Nodes[id]
		n.SetWave(v)
		t.Nodes[id] = n
	}
	return nil
}

// CheckWaves errors when a node's recorded wave disagrees with the computed one.
func (t *Tree) CheckWaves() error {
	w, err := t.ComputeWaves()
	if err != nil {
		return err
	}
	for _, id := range t.ids() {
		n := t.Nodes[id]
		if n.Wave != nil && *n.Wave != w[id] {
			return fmt.Errorf("tree: node %q has wave %d but depends_on gives %d", id, *n.Wave, w[id])
		}
	}
	return nil
}

// Ready reports whether a node may start. A function needs every depends_on
// verified; a component needs every child verified; the root needs every
// component (its children) verified. Component and root readiness looks at
// children only and ignores the node's own depends_on (deviation D2). Unknown
// nodes are never ready.
func (t *Tree) Ready(id string, verified func(string) bool) bool {
	n, ok := t.Nodes[id]
	if !ok {
		return false
	}
	need := n.DependsOn
	if n.Kind != KindFunction {
		need = n.Children
	}
	for _, d := range need {
		if !verified(d) {
			return false
		}
	}
	return true
}
