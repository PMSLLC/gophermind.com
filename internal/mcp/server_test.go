package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gophermind/internal/tools"
)

func testServer() *Server {
	reg := tools.NewRegistry(tools.Tool{
		Name:        "echo",
		Description: "echoes text",
		Schema:      map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Text string `json:"text"`
			}
			json.Unmarshal(raw, &a)
			return "echo:" + a.Text, nil
		},
	})
	return NewServer(reg, "gophermind")
}

func TestInitialize(t *testing.T) {
	out := testServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	var r map[string]any
	json.Unmarshal(out, &r)
	res, _ := r["result"].(map[string]any)
	if res == nil || res["protocolVersion"] == nil {
		t.Fatalf("initialize result malformed: %s", out)
	}
}

func TestToolsList(t *testing.T) {
	out := testServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if !strings.Contains(string(out), `"echo"`) || !strings.Contains(string(out), "inputSchema") {
		t.Errorf("tools/list should include echo + inputSchema:\n%s", out)
	}
}

func TestToolsCall(t *testing.T) {
	out := testServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`))
	if !strings.Contains(string(out), "echo:hi") {
		t.Errorf("tools/call result missing output:\n%s", out)
	}
}

func TestToolsCallUnknown(t *testing.T) {
	out := testServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope"}}`))
	if !strings.Contains(string(out), "unknown tool") {
		t.Errorf("unknown tool should error:\n%s", out)
	}
}

func TestNotificationNoReply(t *testing.T) {
	// A request without an id (notification) gets no response.
	out := testServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if out != nil {
		t.Errorf("notification should yield no reply, got %s", out)
	}
}

func TestServeLoop(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"x"}}}` + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), testServer(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "echo:x") {
		t.Errorf("serve loop output:\n%s", out.String())
	}
}
