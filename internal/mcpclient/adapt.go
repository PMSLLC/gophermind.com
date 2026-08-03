package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/tools"
)

// adaptTools converts a server's advertised tools into gophermind tools.
//
// Each is registered as "<server>__<tool>" so a remote server cannot shadow a
// builtin and two servers cannot collide. A remote tool name containing the
// separator is rejected rather than renamed: the namespace has to stay
// reversible, and safety.Gated keys off the separator to gate MCP tools.
func adaptTools(c *Client, server string, descs []ToolDesc) ([]tools.Tool, error) {
	out := make([]tools.Tool, 0, len(descs))
	for _, d := range descs {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			return nil, fmt.Errorf("server %q advertised a tool with no name", server)
		}
		if strings.Contains(name, NameSeparator) {
			return nil, fmt.Errorf("server %q tool %q must not contain %q", server, name, NameSeparator)
		}

		schema := d.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}

		description := strings.TrimSpace(d.Description)
		if description == "" {
			description = "Tool provided by the " + server + " MCP server."
		} else {
			// Name the origin so the model can tell whose tool this is.
			description = fmt.Sprintf("[mcp:%s] %s", server, description)
		}

		remote := name // capture per iteration for the closure
		out = append(out, tools.Tool{
			Name:        server + NameSeparator + remote,
			Description: description,
			Schema:      schema,
			Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
				return c.CallTool(ctx, remote, raw)
			},
		})
	}
	return out, nil
}
