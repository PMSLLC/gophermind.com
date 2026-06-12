package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// scriptedProvider returns canned responses in sequence and records every
// inbound request so tests can assert message-list invariants.
type scriptedProvider struct {
	responses []string
	requests  []llm.ChatRequest
	i         int
}

func (s *scriptedProvider) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req llm.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		s.requests = append(s.requests, req)
		resp := finalResp("done")
		if s.i < len(s.responses) {
			resp = s.responses[s.i]
			s.i++
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(resp))
	}
}

// toolCallResp builds an SSE body whose single delta carries a tool call.
func toolCallResp(id, name, args string) string {
	b, _ := json.Marshal(args) // JSON-string-escape the arguments
	frame := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + id +
		`","type":"function","function":{"name":"` + name + `","arguments":` + string(b) + `}}]}}]}`
	return "data: " + frame + "\n\ndata: [DONE]\n\n"
}

// finalResp builds an SSE body whose single delta carries final prose.
func finalResp(text string) string {
	b, _ := json.Marshal(text)
	frame := `{"choices":[{"delta":{"content":` + string(b) + `},"finish_reason":"stop"}]}`
	return "data: " + frame + "\n\ndata: [DONE]\n\n"
}

func TestSendStreamsTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range []string{
			`{"choices":[{"delta":{"content":"Hi "}}]}`,
			`{"choices":[{"delta":{"content":"there"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`[DONE]`,
		} {
			w.Write([]byte("data: " + l + "\n\n"))
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(t.TempDir()))
	var tokens string
	a := New(client, reg, 25, nil, func(e Event) {
		if e.Type == "token" {
			tokens += e.Text
		}
	})
	out, err := a.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if out != "Hi there" {
		t.Errorf("answer = %q", out)
	}
	if tokens != "Hi there" {
		t.Errorf("tokens = %q", tokens)
	}
}

// TestLoopAccumulatesUsage runs two turns whose final chunks carry usage and
// checks the per-session meter sums them and emits a usage event each turn.
func TestLoopAccumulatesUsage(t *testing.T) {
	// Each turn: one tool-call response, then a final response carrying usage.
	bodies := []string{
		// turn 1: tool call (no usage on this intermediate response)
		toolCallResp("call_1", "read_file", `{"path":"x.txt"}`),
		// after tool result: final answer with usage
		"data: " + `{"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}` +
			"\n\ndata: " + `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}` +
			"\n\ndata: [DONE]\n\n",
	}
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := bodies[len(bodies)-1]
		if i < len(bodies) {
			body = bodies[i]
			i++
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(t.TempDir()))
	var usageEvents int
	a := New(client, reg, 25, nil, func(e Event) {
		if e.Type == "usage" {
			usageEvents++
		}
	})
	a.SetPrices(0.01, 0.03)

	if _, err := a.Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send turn 1: %v", err)
	}
	// Two model round-trips in turn 1 (tool call + final), but only the final
	// chunk carried usage, so totals reflect one 100/20/120 block.
	u := a.Usage()
	if u.PromptTokens != 100 || u.CompletionTokens != 20 || u.TotalTokens != 120 {
		t.Fatalf("after turn 1: %+v, want 100/20/120", u)
	}

	// A usage event must fire on every model response (>= 2 in turn 1).
	if usageEvents < 2 {
		t.Errorf("usage events = %d, want >= 2", usageEvents)
	}

	// Second user turn accumulates on top of the first.
	if _, err := a.Send(context.Background(), "again"); err != nil {
		t.Fatalf("Send turn 2: %v", err)
	}
	u = a.Usage()
	if u.TotalTokens != 240 {
		t.Errorf("after turn 2: total = %d, want 240 (accumulated)", u.TotalTokens)
	}
	// cost = 200/1000*0.01 + 40/1000*0.03 = 0.002 + 0.0012 = 0.0032
	if u.CostUSD <= 0 {
		t.Errorf("cost = %v, want > 0 with prices set", u.CostUSD)
	}
}

func newTestAgent(t *testing.T, sp *scriptedProvider, root string) *Agent {
	srv := httptest.NewServer(sp.handler(t))
	t.Cleanup(srv.Close)
	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(root), tools.WriteFile(root))
	return New(client, reg, 25, nil, nil)
}

func TestLoopRunsToolsThenFinishes(t *testing.T) {
	root := t.TempDir()
	sp := &scriptedProvider{responses: []string{
		toolCallResp("call_1", "write_file", `{"path":"x.txt","content":"hi"}`),
		toolCallResp("call_2", "read_file", `{"path":"x.txt"}`),
		finalResp("complete"),
	}}
	a := newTestAgent(t, sp, root)

	out, err := a.Send(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "complete" {
		t.Errorf("final = %q, want complete", out)
	}

	// Invariant checks on the LAST request the loop sent.
	last := sp.requests[len(sp.requests)-1]
	var sawAssistantWithCall, sawMatchingToolResult bool
	for j, m := range last.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			sawAssistantWithCall = true
			// The very next message must be a tool result echoing the call ID.
			if j+1 < len(last.Messages) {
				next := last.Messages[j+1]
				if next.Role == "tool" && next.ToolCallID == m.ToolCalls[0].ID {
					sawMatchingToolResult = true
				}
			}
		}
	}
	if !sawAssistantWithCall {
		t.Error("assistant tool-call turn was not replayed to the provider")
	}
	if !sawMatchingToolResult {
		t.Error("tool result did not immediately follow its assistant turn with a matching tool_call_id")
	}

	// The write_file tool should have actually run.
	if out, _ := tools.ReadFile(root).Run(context.Background(), json.RawMessage(`{"path":"x.txt"}`)); out != "hi" {
		t.Errorf("write_file did not run; file = %q", out)
	}
}

func TestLoopMaxIterations(t *testing.T) {
	// Provider always asks for another tool call -> must hit the cap.
	sp := &scriptedProvider{}
	always := toolCallResp("call_x", "read_file", `{"path":"x.txt"}`)
	for i := 0; i < 100; i++ {
		sp.responses = append(sp.responses, always)
	}
	srv := httptest.NewServer(sp.handler(t))
	defer srv.Close()
	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(t.TempDir()))
	a := New(client, reg, 5, nil, nil)

	_, err := a.Send(context.Background(), "loop forever")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("expected max-iterations error, got %v", err)
	}
}

func TestLoopUnknownToolRecovers(t *testing.T) {
	sp := &scriptedProvider{responses: []string{
		toolCallResp("call_1", "nonexistent", `{}`),
		finalResp("recovered"),
	}}
	a := newTestAgent(t, sp, t.TempDir())

	out, err := a.Send(context.Background(), "try unknown tool")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "recovered" {
		t.Errorf("final = %q", out)
	}
	// The error string must have been fed back as the tool result.
	last := sp.requests[len(sp.requests)-1]
	var fedError bool
	for _, m := range last.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "unknown tool") {
			fedError = true
		}
	}
	if !fedError {
		t.Error("unknown-tool error was not fed back to the model")
	}
}

func TestLoopDeniedGatedTool(t *testing.T) {
	root := t.TempDir()
	sp := &scriptedProvider{responses: []string{
		toolCallResp("call_1", "write_file", `{"path":"x.txt","content":"hi"}`),
		finalResp("ok"),
	}}
	srv := httptest.NewServer(sp.handler(t))
	defer srv.Close()
	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.WriteFile(root))
	deny := func(tool, args string) bool { return false }
	a := New(client, reg, 25, deny, nil)

	if _, err := a.Send(context.Background(), "write a file"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// File must NOT exist — the write was denied.
	if out, _ := tools.ReadFile(root).Run(context.Background(), json.RawMessage(`{"path":"x.txt"}`)); out == "hi" {
		t.Error("denied write_file still wrote the file")
	}
	last := sp.requests[len(sp.requests)-1]
	var denied bool
	for _, m := range last.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "denied") {
			denied = true
		}
	}
	if !denied {
		t.Error("denial was not reported back to the model")
	}
}
