package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sse(w http.ResponseWriter, lines ...string) {
	f, _ := w.(http.Flusher)
	for _, l := range lines {
		w.Write([]byte("data: " + l + "\n\n"))
		if f != nil {
			f.Flush()
		}
	}
}

func TestStreamProseAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"x\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	var got string
	msg, _, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "Hello" {
		t.Errorf("tokens = %q, want Hello", got)
	}
	if msg.Content != "Hello" {
		t.Errorf("content = %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" || tc.Function.Arguments != `{"path":"x"}` {
		t.Errorf("reassembled tool call wrong: %+v", tc)
	}
}
