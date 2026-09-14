package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// protocolVersion is the MCP revision this client asks for. The server answers
// with the version it will actually speak, which may be older — gophermind's
// own server replies "2024-11-05" — so the echoed value is adopted rather than
// required to match.
const protocolVersion = "2025-06-18"

// clientName and clientVersion identify gophermind in the initialize handshake.
const (
	clientName    = "gophermind"
	clientVersion = "1"
)

// Client speaks the MCP protocol over a Transport. It is safe for concurrent
// use: tool calls from parallel agent dispatch share one connection.
type Client struct {
	t Transport

	mu     sync.Mutex
	nextID int
}

// ToolDesc is a tool as advertised by a server's tools/list.
type ToolDesc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// NewClient wraps a transport. Call Initialize before anything else.
func NewClient(t Transport) *Client { return &Client{t: t} }

// Initialize performs the MCP handshake: initialize, then the initialized
// notification the spec requires before any other request.
func (c *Client) Initialize(ctx context.Context) error {
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	}, &res)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Adopt the server's version for the MCP-Protocol-Version header.
	if v := strings.TrimSpace(res.ProtocolVersion); v != "" {
		if va, ok := c.t.(versionAware); ok {
			va.setProtocolVersion(v)
		}
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}
	return nil
}

// ListTools returns the tools the server advertises.
func (c *Client) ListTools(ctx context.Context) ([]ToolDesc, error) {
	var res struct {
		Tools []ToolDesc `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	return res.Tools, nil
}

// CallTool invokes a remote tool and flattens its content blocks into the
// single string a gophermind tool returns.
//
// MCP reports tool failures in-band as isError with the message in content,
// rather than as a JSON-RPC error. That is converted to a Go error here so the
// agent sees a failed tool call exactly as it does for a builtin.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage("{}")
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}, &res); err != nil {
		return "", err
	}

	var b strings.Builder
	for _, blk := range res.Content {
		if blk.Text == "" {
			// Non-text blocks (images, embedded resources) have no string form
			// the agent can consume; name the type so the omission is visible.
			if blk.Type != "" && blk.Type != "text" {
				fmt.Fprintf(&b, "[%s content omitted]\n", blk.Type)
			}
			continue
		}
		b.WriteString(blk.Text)
		if !strings.HasSuffix(blk.Text, "\n") {
			b.WriteByte('\n')
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if res.IsError {
		if out == "" {
			out = "tool reported an error with no message"
		}
		return "", fmt.Errorf("%s", out)
	}
	return out, nil
}

// Close releases the transport.
func (c *Client) Close() error { return c.t.Close() }

// call sends a request and decodes its result into out.
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return err
	}
	reply, err := c.t.Send(ctx, body)
	if err != nil {
		return err
	}
	if len(reply) == 0 {
		return fmt.Errorf("empty response")
	}

	var resp rpcResponse
	if err := json.Unmarshal(reply, &resp); err != nil {
		return fmt.Errorf("malformed response: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out == nil || len(resp.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	return nil
}

// notify sends a notification, which by definition expects no reply.
func (c *Client) notify(ctx context.Context, method string, params any) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	_, err = c.t.Send(ctx, body)
	return err
}

// isNotification reports whether a marshalled message lacks an id, and so
// expects no reply. Both transports need this: stdio must not block reading a
// response that will never come, and HTTP must tolerate a 202 with no body.
func isNotification(msg []byte) bool {
	var probe struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(msg, &probe); err != nil {
		return false
	}
	return len(probe.ID) == 0
}
