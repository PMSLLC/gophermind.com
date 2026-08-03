package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
)

// Transport carries one JSON-RPC message to an MCP server and returns its
// reply. Implementations are safe for concurrent use.
//
// A notification (a message with no id) expects no reply; Send returns
// (nil, nil) for it. This is the single seam between the protocol in client.go
// and the wire — stdio.go and http.go know nothing about each other.
type Transport interface {
	Send(ctx context.Context, msg []byte) ([]byte, error)
	Close() error
}

// rpcRequest is a JSON-RPC 2.0 request or notification. ID is a pointer so a
// notification omits it entirely rather than sending id:0, which a strict
// server would answer instead of treating as fire-and-forget.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
}

// versionAware is implemented by transports that must echo the negotiated
// protocol version on later requests (Streamable HTTP sends it as a header).
// Stdio does not, so it simply does not implement this.
type versionAware interface {
	setProtocolVersion(v string)
}
