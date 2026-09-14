// Package schemas ships a small library of reusable JSON schemas for the
// --schema-constrained output mode (diff, review, plan), so common machine-
// readable result shapes are available by name instead of a hand-written file.
package schemas

import (
	"encoding/json"
	"sort"
)

// library holds the built-in schemas as JSON source, parsed lazily on Get.
// Keeping them as source keeps the definitions readable and guarantees each
// Get returns an independent copy (no shared-map aliasing).
var library = map[string]string{
	"diff": `{
		"type": "object",
		"properties": {
			"files": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"path": {"type": "string"},
						"change": {"type": "string", "enum": ["add", "modify", "delete", "rename"]},
						"summary": {"type": "string"}
					},
					"required": ["path", "change"]
				}
			},
			"summary": {"type": "string"}
		},
		"required": ["files"]
	}`,
	"review": `{
		"type": "object",
		"properties": {
			"verdict": {"type": "string", "enum": ["approve", "request_changes", "comment"]},
			"summary": {"type": "string"},
			"findings": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"file": {"type": "string"},
						"line": {"type": "integer"},
						"severity": {"type": "string", "enum": ["blocker", "major", "minor", "nit"]},
						"message": {"type": "string"}
					},
					"required": ["severity", "message"]
				}
			}
		},
		"required": ["verdict", "findings"]
	}`,
	"plan": `{
		"type": "object",
		"properties": {
			"goal": {"type": "string"},
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {"type": "string"},
						"detail": {"type": "string"},
						"verify": {"type": "string"}
					},
					"required": ["title"]
				}
			}
		},
		"required": ["goal", "steps"]
	}`,
}

// Get returns a parsed copy of the named built-in schema, or (nil, false) if no
// such schema exists. The returned map is freshly parsed, so callers may mutate
// it without affecting the library.
func Get(name string) (map[string]any, bool) {
	src, ok := library[name]
	if !ok {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		// The library is a compile-time constant of valid JSON; a parse failure
		// here is a programming error, not a runtime condition.
		panic("schemas: invalid built-in schema " + name + ": " + err.Error())
	}
	return m, true
}

// Names returns the sorted names of the built-in schemas.
func Names() []string {
	names := make([]string, 0, len(library))
	for n := range library {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
