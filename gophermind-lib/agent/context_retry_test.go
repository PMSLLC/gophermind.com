package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/tools"
)

// A server that knows its real window (n_ctx) rejects an oversized request
// with a 400 that says so. The agent had no probed capabilities and so never
// trimmed at all. It must adopt the stated limit, trim to it, and retry the
// same turn rather than failing the whole run.
func TestSendAdoptsServerContextLimitAndRetries(t *testing.T) {
	const nCtx = 10_000 // tokens; 40 KB by the 4-bytes-per-token estimate
	var calls, rejected int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		atomic.AddInt32(&calls, 1)
		if len(body)/4 > nCtx {
			atomic.AddInt32(&rejected, 1)
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"code":400,"message":"request exceeds the available context size","type":"exceed_context_size_error","n_prompt_tokens":99999,"n_ctx":10000}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n"))
	}))
	defer srv.Close()

	a := New(llm.New(srv.URL, "", "m", 5*time.Second, false), tools.NewRegistry(), 5, nil, nil)
	for i := 0; i < 40; i++ { // ~200 KB of earlier conversation
		a.msgs = append(a.msgs,
			llm.Message{Role: "user", Content: strings.Repeat("q", 2500)},
			llm.Message{Role: "assistant", Content: strings.Repeat("a", 2500)})
	}

	out, err := a.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v (calls=%d rejected=%d)", err, calls, rejected)
	}
	if out != "done" {
		t.Errorf("answer = %q, want done", out)
	}
	if rejected != 1 {
		t.Errorf("rejected = %d, want exactly 1 (one overflow, then a trimmed retry)", rejected)
	}
	if a.caps.ContextWindow != nCtx {
		t.Errorf("caps.ContextWindow = %d, want the server's %d", a.caps.ContextWindow, nCtx)
	}
}
