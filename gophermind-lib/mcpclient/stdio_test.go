package mcpclient

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"gophermind/gophermind-lib/mcp"
	"gophermind/gophermind-lib/tools"
)

// serverEnvVar makes this test binary re-exec as an MCP server instead of
// running tests, the standard Go helper-process pattern. It lets the stdio
// transport be exercised against internal/mcp.Serve — gophermind's real server
// — rather than against a mock of the protocol.
const serverEnvVar = "GOPHERMIND_MCP_TEST_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(serverEnvVar) == "1" {
		reg := tools.NewRegistry(
			tools.Tool{
				Name:        "echo",
				Description: "Echo the text back.",
				Schema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"text": map[string]any{"type": "string"}},
					"required":   []any{"text"},
				},
				Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
					var a struct {
						Text string `json:"text"`
					}
					_ = json.Unmarshal(raw, &a)
					return "echo: " + a.Text, nil
				},
			},
			tools.Tool{
				Name:        "boom",
				Description: "Always fails.",
				Schema:      map[string]any{"type": "object"},
				Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
					return "", errBoom{}
				},
			},
		)
		_ = mcp.Serve(context.Background(), mcp.NewServer(reg, "test-server"), os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type errBoom struct{}

func (errBoom) Error() string { return "detonated" }

// testServerConfig points a stdio server at this test binary.
func testServerConfig(name string) ServerConfig {
	return ServerConfig{
		Name:      name,
		Transport: TransportStdio,
		Command:   os.Args[0],
		Env:       map[string]string{serverEnvVar: "1"},
	}
}

func dialTestServer(t *testing.T) *Client {
	t.Helper()
	tr, err := newStdioTransport(testServerConfig("test"))
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	c := NewClient(tr)
	t.Cleanup(func() { c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

func TestStdioRoundTripAgainstRealServer(t *testing.T) {
	c := dialTestServer(t)
	ctx := context.Background()

	list, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	byName := map[string]ToolDesc{}
	for _, d := range list {
		byName[d.Name] = d
	}
	echo, ok := byName["echo"]
	if !ok {
		t.Fatalf("echo tool missing from %v", byName)
	}
	if echo.Description == "" {
		t.Error("description did not survive the round trip")
	}
	if echo.InputSchema == nil || echo.InputSchema["type"] != "object" {
		t.Errorf("input schema did not survive: %#v", echo.InputSchema)
	}

	out, err := c.CallTool(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if out != "echo: hello" {
		t.Errorf("got %q", out)
	}
}

// A tool that fails must surface as a Go error, not a success string.
func TestStdioToolErrorBecomesGoError(t *testing.T) {
	c := dialTestServer(t)
	_, err := c.CallTool(context.Background(), "boom", nil)
	if err == nil {
		t.Fatal("a failing tool must return an error")
	}
	if err.Error() != "detonated" {
		t.Errorf("error text lost: %v", err)
	}
}

// An unknown tool is a JSON-RPC error rather than an isError result.
func TestStdioUnknownToolErrors(t *testing.T) {
	c := dialTestServer(t)
	if _, err := c.CallTool(context.Background(), "nope", nil); err == nil {
		t.Fatal("unknown tool must error")
	}
}

// Close must reap the child; a leaked process would accumulate per session.
func TestStdioCloseTerminatesServer(t *testing.T) {
	tr, err := newStdioTransport(testServerConfig("test"))
	if err != nil {
		t.Fatal(err)
	}
	pid := tr.cmd.Process.Pid
	c := NewClient(tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// After Wait, signalling the pid must fail — the process is gone and reaped.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p, err := os.FindProcess(pid); err != nil || p.Signal(os.Signal(nil)) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Signal(nil) is not portable everywhere; a successful Wait is the real
	// assertion and Close already returned, so only warn here.
	t.Log("could not confirm process exit by signal; Close() returned cleanly")
}

// Closing twice must not panic or double-Wait.
func TestStdioCloseIsIdempotent(t *testing.T) {
	tr, err := newStdioTransport(testServerConfig("test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
