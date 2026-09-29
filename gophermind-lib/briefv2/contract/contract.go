// Package contract loads the Wave 0 contracts.json artifact and derives each
// node's dependency_signatures from it. Models never write signatures; the
// harness slices them here.
package contract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/schema"
)

type Type struct {
	ID      string   `json:"id"`
	Package string   `json:"package"`
	File    string   `json:"file"`
	Decl    string   `json:"decl"`
	Uses    []string `json:"uses"`
}

type Function struct {
	ID        string   `json:"id"`
	Package   string   `json:"package"`
	File      string   `json:"file"`
	Signature string   `json:"signature"`
	Doc       string   `json:"doc"`
	Uses      []string `json:"uses"`
	Component string   `json:"component"`
}

type Component struct {
	ID      string   `json:"id"`
	Package string   `json:"package"`
	Exports []string `json:"exports"`
}

type Contracts struct {
	BriefID    string      `json:"brief_id"`
	Revision   int         `json:"revision"`
	Module     string      `json:"module"`
	Types      []Type      `json:"types"`
	Functions  []Function  `json:"functions"`
	Components []Component `json:"components"`

	typeIdx map[string]int
	fnIdx   map[string]int
	compSet map[string]bool
}

// Load validates raw against the contract schema and checks that every uses
// and exports entry resolves to a declared type or function.
func Load(raw []byte) (*Contracts, error) {
	if err := schema.Validate(schema.KindContract, raw); err != nil {
		return nil, err
	}
	var c Contracts
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	c.typeIdx, c.fnIdx, c.compSet = map[string]int{}, map[string]int{}, map[string]bool{}
	for i, t := range c.Types {
		if _, dup := c.typeIdx[t.ID]; dup {
			return nil, fmt.Errorf("contract: duplicate id %q", t.ID)
		}
		c.typeIdx[t.ID] = i
	}
	for i, f := range c.Functions {
		if _, dup := c.typeIdx[f.ID]; dup {
			return nil, fmt.Errorf("contract: duplicate id %q", f.ID)
		}
		if _, dup := c.fnIdx[f.ID]; dup {
			return nil, fmt.Errorf("contract: duplicate id %q", f.ID)
		}
		c.fnIdx[f.ID] = i
	}
	for _, comp := range c.Components {
		c.compSet[comp.ID] = true
	}
	for _, t := range c.Types {
		for _, u := range t.Uses {
			if !c.declared(u) {
				return nil, fmt.Errorf("contract: %s uses unknown id %q", t.ID, u)
			}
		}
	}
	for _, f := range c.Functions {
		for _, u := range f.Uses {
			if !c.declared(u) {
				return nil, fmt.Errorf("contract: %s uses unknown id %q", f.ID, u)
			}
		}
	}
	for _, comp := range c.Components {
		for _, e := range comp.Exports {
			if _, ok := c.fnIdx[e]; !ok {
				return nil, fmt.Errorf("contract: component %s exports unknown function %q", comp.ID, e)
			}
		}
	}
	return &c, nil
}

func (c *Contracts) declared(id string) bool {
	_, t := c.typeIdx[id]
	_, f := c.fnIdx[id]
	return t || f
}

func (c *Contracts) uses(id string) []string {
	if i, ok := c.typeIdx[id]; ok {
		return c.Types[i].Uses
	}
	if i, ok := c.fnIdx[id]; ok {
		return c.Functions[i].Uses
	}
	return nil
}

// Closure returns every type and function ID reachable from dependsOn through
// uses, including the dependsOn entries themselves. Component IDs are skipped;
// any other unknown ID is an error.
func (c *Contracts) Closure(dependsOn []string) (map[string]bool, error) {
	seen := map[string]bool{}
	stack := append([]string{}, dependsOn...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		if c.compSet[id] && !c.declared(id) {
			continue
		}
		if !c.declared(id) {
			return nil, fmt.Errorf("contract: unknown id %q", id)
		}
		seen[id] = true
		stack = append(stack, c.uses(id)...)
	}
	return seen, nil
}

// Slice returns the dependency_signatures for a node with the given
// depends_on. self is removed from the result.
func (c *Contracts) Slice(dependsOn []string, self string) ([]string, error) {
	closure, err := c.Closure(dependsOn)
	if err != nil {
		return nil, err
	}
	delete(closure, self)

	var typeIDs, fnIDs []string
	for _, t := range c.Types {
		if closure[t.ID] {
			typeIDs = append(typeIDs, t.ID)
		}
	}
	for _, f := range c.Functions {
		if closure[f.ID] {
			fnIDs = append(fnIDs, f.ID)
		}
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, id := range order(typeIDs, c.uses) {
		add(c.Types[c.typeIdx[id]].Decl)
	}
	for _, id := range order(fnIDs, c.uses) {
		f := c.Functions[c.fnIdx[id]]
		add(docComment(f.Doc) + f.Signature)
	}
	return out, nil
}

func docComment(doc string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		lines[i] = "// " + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// order sorts ids (given in contracts.json order) so a decl follows the
// members of ids it uses. Ties keep file order. A cycle is broken by taking
// the first remaining id in file order rather than failing.
func order(ids []string, uses func(string) []string) []string {
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	done := map[string]bool{}
	var out []string
	for len(out) < len(ids) {
		pick := ""
		for _, id := range ids {
			if done[id] {
				continue
			}
			ready := true
			for _, u := range uses(id) {
				if in[u] && !done[u] && u != id {
					ready = false
					break
				}
			}
			if ready {
				pick = id
				break
			}
		}
		if pick == "" {
			for _, id := range ids {
				if !done[id] {
					pick = id
					break
				}
			}
		}
		done[pick] = true
		out = append(out, pick)
	}
	return out
}

// Diff lists IDs whose declaration changed, was added, or was removed.
func Diff(old, nw *Contracts) []string {
	sig := func(c *Contracts) map[string]string {
		m := map[string]string{}
		for _, t := range c.Types {
			m[t.ID] = "T\x00" + t.Decl
		}
		for _, f := range c.Functions {
			m[f.ID] = "F\x00" + f.Signature + "\x00" + f.Doc
		}
		return m
	}
	a, b := sig(old), sig(nw)
	set := map[string]bool{}
	for id, v := range a {
		if b[id] != v {
			set[id] = true
		}
	}
	for id := range b {
		if _, ok := a[id]; !ok {
			set[id] = true
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Affected returns the node IDs (keys of deps, mapping node ID to its
// depends_on) whose closure includes any changed ID.
func (c *Contracts) Affected(changed []string, deps map[string][]string) ([]string, error) {
	chg := map[string]bool{}
	for _, id := range changed {
		chg[id] = true
	}
	var out []string
	for node, d := range deps {
		cl, err := c.Closure(d)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node, err)
		}
		for id := range cl {
			if chg[id] {
				out = append(out, node)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}
