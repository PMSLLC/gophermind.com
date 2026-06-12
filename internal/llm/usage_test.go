package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCompleteParsesUsage checks that the usage block from a non-streaming
// response is decoded and returned.
func TestCompleteParsesUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}],` +
			`"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	_, usage, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if usage.PromptTokens != 12 || usage.CompletionTokens != 5 || usage.TotalTokens != 17 {
		t.Errorf("usage = %+v, want 12/5/17", usage)
	}
}

// TestCompleteZeroUsageWhenAbsent confirms a missing usage block yields the zero
// value rather than an error.
func TestCompleteZeroUsageWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	_, usage, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if usage != (Usage{}) {
		t.Errorf("usage = %+v, want zero value", usage)
	}
}

// TestStreamParsesFinalUsageChunk checks that usage carried in the final SSE
// chunk (which has an empty choices array) is parsed, and that the request asked
// the server to include it.
func TestStreamParsesFinalUsageChunk(t *testing.T) {
	var sawIncludeUsage bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		sawIncludeUsage = req.StreamOptions != nil && req.StreamOptions.IncludeUsage
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"delta":{"content":"hi"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":30,"completion_tokens":7,"total_tokens":37}}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	msg, usage, err := c.Stream(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !sawIncludeUsage {
		t.Error("stream_options.include_usage not set on the request")
	}
	if msg.Content != "hi" {
		t.Errorf("content = %q", msg.Content)
	}
	if usage.PromptTokens != 30 || usage.CompletionTokens != 7 || usage.TotalTokens != 37 {
		t.Errorf("usage = %+v, want 30/7/37", usage)
	}
}
