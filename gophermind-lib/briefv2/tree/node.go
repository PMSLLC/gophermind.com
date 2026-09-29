// Package tree holds the v2 task tree: node documents, the dependency graph
// (cycle check, waves, readiness), and the one-file-per-node store.
package tree

import (
	"encoding/json"

	"gophermind/gophermind-lib/briefv2/schema"
)

type Kind string

const (
	KindRoot      Kind = "root"
	KindComponent Kind = "component"
	KindFunction  Kind = "function"
)

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
	return n, nil
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
