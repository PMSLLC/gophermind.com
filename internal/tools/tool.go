// Package tools defines the agent's tool abstraction and the built-in tool set.
package tools

import (
	"context"
	"encoding/json"
	"sort"

	"gophermind/internal/llm"
)

// Tool is a single capability the model can invoke. Run receives the raw JSON
// arguments from the model and returns a result string that becomes the
// role:"tool" message content. A returned error is formatted into the result
// by the agent so the model can see and recover from it.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON Schema for the parameters object
	Run         func(ctx context.Context, args json.RawMessage) (string, error)
}

// Registry holds the available tools, keyed by name.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds a registry from the given tools.
func NewRegistry(ts ...Tool) *Registry {
	m := make(map[string]Tool, len(ts))
	for _, t := range ts {
		m[t.Name] = t
	}
	return &Registry{tools: m}
}

// Get returns the named tool.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Definitions renders the registry as the wire-format tool list sent to the
// model on each request, in stable (sorted) order.
func (r *Registry) Definitions() []llm.Tool {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]llm.Tool, 0, len(names))
	for _, name := range names {
		t := r.tools[name]
		defs = append(defs, llm.Tool{
			Type: "function",
			Function: llm.Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}
	return defs
}

// object is a small helper for declaring a JSON-Schema object parameter set.
func object(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

// str declares a string property with a description.
func str(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
