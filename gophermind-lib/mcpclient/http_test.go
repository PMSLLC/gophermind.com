package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mcpHandler answers initialize/tools-list/tools-call, writing the reply in the
// chosen framing so both are exercised against identical protocol logic.
func mcpHandler(t *testing.T, sse bool, seen *[]*http.Request) http.HandlerFunc {
	t.Helper()
	var mu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if seen != nil {
			mu.Lock()
			clone := r.Clone(context.Background())
			*seen = append(*seen, clone)
			mu.Unlock()
		}

		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.Unmarshal(body, &req)

		if len(req.ID) == 0 { // notification
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result string
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-123")
			result = `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"fake","version":"1"}}`
		case "tools/list":
			result = `{"tools":[{"name":"search","description":"Search things.","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`
		case "tools/call":
			if req.Params.Name == "bad" {
				result = `{"content":[{"type":"text","text":"it broke"}],"isError":true}`
			} else {
				result = `{"content":[{"type":"text","text":"found it"}]}`
			}
		default:
			result = `{}`
		}
		payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)

		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, ": keepalive\nevent: message\ndata: %s\n\n", payload)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, payload)
	}
}

func httpClientFor(t *testing.T, sse bool, seen *[]*http.Request) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mcpHandler(t, sse, seen))
	t.Cleanup(srv.Close)

	tr := newHTTPTransport(ServerConfig{
		Name:      "remote",
		Transport: TransportHTTP,
		URL:       srv.URL,
		Headers:   map[string]string{"Authorization": "Bearer tok-abc"},
	}, srv.Client())
	c := NewClient(tr)
	t.Cleanup(func() { c.Close() })
	return c, srv
}

// Both framings must decode to exactly the same protocol results.
func TestHTTPBothFramingsAgree(t *testing.T) {
	for _, sse := range []bool{false, true} {
		name := "json"
		if sse {
			name = "sse"
		}
		t.Run(name, func(t *testing.T) {
			c, _ := httpClientFor(t, sse, nil)
			ctx := context.Background()
			if err := c.Initialize(ctx); err != nil {
				t.Fatalf("initialize: %v", err)
			}
			list, err := c.ListTools(ctx)
			if err != nil {
				t.Fatalf("tools/list: %v", err)
			}
			if len(list) != 1 || list[0].Name != "search" {
				t.Fatalf("unexpected tools: %+v", list)
			}
			if list[0].InputSchema["type"] != "object" {
				t.Errorf("schema lost: %#v", list[0].InputSchema)
			}
			out, err := c.CallTool(ctx, "search", json.RawMessage(`{"q":"x"}`))
			if err != nil {
				t.Fatalf("tools/call: %v", err)
			}
			if out != "found it" {
				t.Errorf("got %q", out)
			}
		})
	}
}

func TestHTTPSendsAuthSessionAndVersion(t *testing.T) {
	var seen []*http.Request
	c, _ := httpClientFor(t, false, &seen)
	ctx := context.Background()
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := c.ListTools(ctx); err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	if len(seen) < 3 {
		t.Fatalf("want initialize + notification + list, got %d requests", len(seen))
	}
	for i, r := range seen {
		if got := r.Header.Get("Authorization"); got != "Bearer tok-abc" {
			t.Errorf("request %d missing auth header: %q", i, got)
		}
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Errorf("request %d must accept SSE: %q", i, r.Header.Get("Accept"))
		}
	}
	// The session id from initialize must be echoed on later requests.
	last := seen[len(seen)-1]
	if got := last.Header.Get("Mcp-Session-Id"); got != "sess-123" {
		t.Errorf("session id not echoed: %q", got)
	}
	if got := last.Header.Get("MCP-Protocol-Version"); got != "2025-06-18" {
		t.Errorf("negotiated version not sent: %q", got)
	}
}

func TestHTTPToolErrorBecomesGoError(t *testing.T) {
	c, _ := httpClientFor(t, false, nil)
	ctx := context.Background()
	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := c.CallTool(ctx, "bad", nil)
	if err == nil {
		t.Fatal("isError result must become a Go error")
	}
	if !strings.Contains(err.Error(), "it broke") {
		t.Errorf("error text lost: %v", err)
	}
}

// A 401 or a Cloudflare challenge must produce a legible error, not a panic or
// a silent empty tool list.
func TestHTTPNon2xxIsALegibleError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "authentication required\nsecond line ignored")
	}))
	defer srv.Close()

	c := NewClient(newHTTPTransport(ServerConfig{URL: srv.URL}, srv.Client()))
	err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("401 must be an error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("error should carry status and body: %v", err)
	}
}

func TestReadSSEMultilineData(t *testing.T) {
	stream := "event: message\ndata: {\"a\":1,\ndata: \"b\":2}\n\n"
	got, err := readSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	want := "{\"a\":1,\n\"b\":2}"
	if string(got) != want {
		t.Errorf("got %q want %q", got, want)
	}
}
