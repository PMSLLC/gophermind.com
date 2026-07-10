// Package mcp exposes gophermind's tools over the Model Context Protocol as a
// stdio JSON-RPC server, so any MCP client (including Claude) can discover and
// call them. It implements the core methods: initialize, tools/list, tools/call.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"gophermind/internal/tools"
)

// request is a JSON-RPC 2.0 request/notification.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is a JSON-RPC 2.0 response.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server serves a tool registry over MCP.
type Server struct {
	reg  *tools.Registry
	name string
}

// NewServer builds an MCP server over the given tool registry.
func NewServer(reg *tools.Registry, name string) *Server {
	return &Server{reg: reg, name: name}
}

// Handle processes one JSON-RPC message and returns the response bytes, or nil
// for a notification (no id) that needs no reply.
func (s *Server) Handle(ctx context.Context, raw []byte) []byte {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return marshal(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
	}
	// Notifications (no id) get no response.
	notification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		return s.reply(req, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": "1"},
		})
	case "tools/list":
		var list []map[string]any
		for _, d := range s.reg.Definitions() {
			list = append(list, map[string]any{
				"name":        d.Function.Name,
				"description": d.Function.Description,
				"inputSchema": d.Function.Parameters,
			})
		}
		return s.reply(req, map[string]any{"tools": list})
	case "tools/call":
		return s.callTool(ctx, req)
	default:
		if notification {
			return nil
		}
		return marshal(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
	}
}

// callTool dispatches a tools/call request to the named tool.
func (s *Server) callTool(ctx context.Context, req request) []byte {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return marshal(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
	}
	t, ok := s.reg.Get(p.Name)
	if !ok {
		return marshal(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}})
	}
	args := p.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		// MCP convention: tool errors are returned as content with isError=true.
		return s.reply(req, map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		})
	}
	return s.reply(req, map[string]any{
		"content": []map[string]any{{"type": "text", "text": out}},
	})
}

// reply builds a result response (or nil for a notification).
func (s *Server) reply(req request, result any) []byte {
	if len(req.ID) == 0 {
		return nil
	}
	return marshal(response{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Serve runs the stdio loop: one JSON message per line in, one per line out,
// until EOF. This is the transport an MCP client speaks to.
func Serve(ctx context.Context, s *Server, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if resp := s.Handle(ctx, line); resp != nil {
			if _, err := fmt.Fprintf(out, "%s\n", resp); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}
