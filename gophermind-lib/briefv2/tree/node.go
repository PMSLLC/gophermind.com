// Package tree holds the v2 task tree: node documents, the dependency graph
// (cycle check, waves, readiness), and the one-file-per-node store.
package tree

import (
	"encoding/json"
	"fmt"
	"regexp"

	"gophermind/gophermind-lib/briefv2/schema"
)

type Kind string

const (
	KindRoot      Kind = "root"
	KindComponent Kind = "component"
	KindFunction  Kind = "function"
)

// idPattern is the schema's id pattern; parent, children and depends_on
// entries must match it too so a reference can never carry a path.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// reservedComponentID collides with the run's logs/ directory.
const reservedComponentID = "logs"

// Node exposes the fields the tree logic needs and keeps the full decoded
// document so nothing else is lost on a round trip.
type Node struct {
	ID        string
	Kind      Kind
	Parent    string
	Children  []string
	DependsOn []string
	Wave      *int

	doc map[string]any
}

// ParseNode validates raw against the node schema and decodes it.
func ParseNode(raw []byte) (Node, error) {
	if err := schema.Validate(schema.KindNode, raw); err != nil {
		return Node{}, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Node{}, err
	}
	n := Node{doc: doc}
	n.ID, _ = doc["id"].(string)
	k, _ := doc["kind"].(string)
	n.Kind = Kind(k)
	n.Parent, _ = doc["parent"].(string)
	n.Children = strList(doc["children"])
	n.DependsOn = strList(doc["depends_on"])
	if f, ok := doc["wave"].(float64); ok {
		w := int(f)
		n.Wave = &w
	}
	if err := n.checkRefs(); err != nil {
		return Node{}, err
	}
	return n, nil
}

func (n Node) checkRefs() error {
	if n.Parent != "" && !idPattern.MatchString(n.Parent) {
		return fmt.Errorf("tree: node %q: invalid parent %q", n.ID, n.Parent)
	}
	for _, c := range n.Children {
		if !idPattern.MatchString(c) {
			return fmt.Errorf("tree: node %q: invalid children entry %q", n.ID, c)
		}
	}
	for _, d := range n.DependsOn {
		if !idPattern.MatchString(d) {
			return fmt.Errorf("tree: node %q: invalid depends_on entry %q", n.ID, d)
		}
	}
	if n.Kind == KindComponent && n.ID == reservedComponentID {
		return fmt.Errorf("tree: node %q: component id %q is reserved", n.ID, n.ID)
	}
	return nil
}

func strList(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// SetWave records w on both the struct and the document.
func (n *Node) SetWave(w int) {
	n.Wave = &w
	n.doc["wave"] = w
}

// Marshal renders the document as indented JSON.
func (n Node) Marshal() ([]byte, error) { return json.MarshalIndent(n.doc, "", "  ") }

// Path is the node's slash-separated path relative to the run directory.
func (n Node) Path() string {
	switch n.Kind {
	case KindRoot:
		return "root.json"
	case KindComponent:
		return n.ID + "/component.json"
	default:
		return n.Parent + "/" + n.ID + ".json"
	}
}
