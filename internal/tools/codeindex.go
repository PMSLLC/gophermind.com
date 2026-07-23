package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/codeindex"
)

// CodeIndex returns the index tool: a symbol lookup over the Go tree, for
// answering "where is X defined" without grepping.
//
// It builds the index from source on each call rather than reading INDEX.md.
// The file on disk can lag behind an in-progress edit; a fresh parse cannot.
func CodeIndex(root string) Tool {
	return Tool{
		Name: "index",
		Description: "Look up Go symbols (funcs, methods, types, interfaces, consts, vars) by name and get their file:line locations and signatures. " +
			"Prefer this over search when you want to find where something is DEFINED. Omit 'query' to list everything.",
		Schema: object(map[string]any{
			"query": str("Case-insensitive substring of the symbol or receiver name. Omit to list all symbols."),
			"kind":  str("Optional filter: func, method, type, interface, const, or var."),
			"limit": map[string]any{"type": "integer", "description": "Maximum results (default 50)."},
		}),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Query string `json:"query"`
				Kind  string `json:"kind"`
				Limit int    `json:"limit"`
			}
			// Arguments are all optional; an empty payload lists everything.
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			idx, err := codeindex.Build(root)
			if err != nil {
				return "", fmt.Errorf("build index: %w", err)
			}
			hits := idx.Lookup(a.Query, strings.TrimSpace(a.Kind), a.Limit)
			if len(hits) == 0 {
				return fmt.Sprintf("no symbols match %q (indexed %d)", a.Query, len(idx.Entries)), nil
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d match(es) of %d indexed symbols:\n", len(hits), len(idx.Entries))
			for _, e := range hits {
				name := e.Name
				if e.Recv != "" {
					name = "(" + e.Recv + ")." + e.Name
				}
				fmt.Fprintf(&b, "%s:%d  %s %s.%s  %s\n", e.File, e.Line, e.Kind, e.Package, name, e.Signature)
				if e.Doc != "" {
					fmt.Fprintf(&b, "    %s\n", e.Doc)
				}
			}
			return b.String(), nil
		},
	}
}
