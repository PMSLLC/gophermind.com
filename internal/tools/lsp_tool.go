package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/lsp"
)

// LSPDefinition returns a read-only tool that resolves a symbol's definition via
// a project Language Server (semantic go-to-definition, more precise than grep).
// argv is the LSP server command; an empty argv disables the tool.
func LSPDefinition(root string, argv []string) Tool {
	return Tool{
		Name:        "find_definition",
		Description: "Resolve where a symbol is defined via the project's Language Server (precise go-to-definition). Requires an LSP server to be configured.",
		Schema: object(map[string]any{
			"file":   str("File (relative to repo root) containing the reference."),
			"line":   map[string]any{"type": "integer", "description": "0-based line of the reference."},
			"column": map[string]any{"type": "integer", "description": "0-based column of the reference."},
		}, "file", "line", "column"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if len(argv) == 0 {
				return "", fmt.Errorf("no LSP server configured: set GOPHERMIND_LSP_CMD")
			}
			var a struct {
				File   string `json:"file"`
				Line   int    `json:"line"`
				Column int    `json:"column"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			locs, err := lsp.Definition(ctx, argv, root, a.File, a.Line, a.Column)
			if err != nil {
				return "", fmt.Errorf("lsp definition: %w", err)
			}
			if len(locs) == 0 {
				return "(no definition found)", nil
			}
			var b strings.Builder
			for _, l := range locs {
				fmt.Fprintf(&b, "%s:%d\n", l.File, l.Line)
			}
			return b.String(), nil
		},
	}
}
