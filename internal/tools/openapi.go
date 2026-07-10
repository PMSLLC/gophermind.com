package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gophermind/internal/safety"
)

// openAPIMethods are the HTTP methods recognized as operations in a path item.
var openAPIMethods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

// OpenAPIOps returns a read-only tool that parses an OpenAPI 3 (JSON) spec file
// and lists its operations — method, path, operationId, summary, and parameters
// — plus the server base URL, so the model can discover the correct API calls to
// make (e.g. via http_request) instead of guessing.
func OpenAPIOps(root string) Tool {
	return Tool{
		Name:        "openapi_ops",
		Description: "Parse an OpenAPI 3 JSON spec file and list its operations (method, path, operationId, summary, parameters) and server URL. Read-only.",
		Schema: object(map[string]any{
			"spec": str("Path to an OpenAPI 3 JSON spec file, relative to the repo root."),
		}, "spec"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Spec string `json:"spec"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Spec)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("read spec: %w", err)
			}

			var doc struct {
				Servers []struct {
					URL string `json:"url"`
				} `json:"servers"`
				Paths map[string]map[string]struct {
					OperationID string `json:"operationId"`
					Summary     string `json:"summary"`
					Parameters  []struct {
						Name     string `json:"name"`
						In       string `json:"in"`
						Required bool   `json:"required"`
					} `json:"parameters"`
				} `json:"paths"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				return "", fmt.Errorf("parse spec: %w", err)
			}

			var b strings.Builder
			if len(doc.Servers) > 0 {
				fmt.Fprintf(&b, "server: %s\n", doc.Servers[0].URL)
			}
			b.WriteString("operations:\n")

			paths := make([]string, 0, len(doc.Paths))
			for p := range doc.Paths {
				paths = append(paths, p)
			}
			sort.Strings(paths)

			count := 0
			for _, p := range paths {
				item := doc.Paths[p]
				for _, m := range openAPIMethods {
					op, ok := item[m]
					if !ok {
						continue
					}
					count++
					fmt.Fprintf(&b, "  %-6s %s", strings.ToUpper(m), p)
					if op.OperationID != "" {
						fmt.Fprintf(&b, "  (%s)", op.OperationID)
					}
					if op.Summary != "" {
						fmt.Fprintf(&b, " — %s", op.Summary)
					}
					b.WriteString("\n")
					for _, prm := range op.Parameters {
						req := ""
						if prm.Required {
							req = ", required"
						}
						fmt.Fprintf(&b, "      param %s (in %s%s)\n", prm.Name, prm.In, req)
					}
				}
			}
			if count == 0 {
				b.WriteString("  (no operations found)\n")
			}
			return b.String(), nil
		},
	}
}
