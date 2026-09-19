package plan

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/llm"
)

func TestClientCompleterStartsEveryCallFromScratchWithNoTools(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"REPLY"},"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := ClientCompleter{Client: llm.New(srv.URL, "", "m", 5*time.Second, false)}
	first, err := c.Complete(context.Background(), "FIRST-PROMPT-TEXT")
	if err != nil || first != "REPLY" {
		t.Fatalf("Complete = %q, %v", first, err)
	}
	if _, err := c.Complete(context.Background(), "SECOND-PROMPT-TEXT"); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("%d requests, want 2", len(bodies))
	}
	if !strings.Contains(bodies[1], "SECOND-PROMPT-TEXT") || strings.Contains(bodies[1], "FIRST-PROMPT-TEXT") || strings.Contains(bodies[1], "REPLY") {
		t.Error("the second request must not carry anything from the first call")
	}
	for i, b := range bodies {
		if strings.Contains(b, `"tools"`) {
			t.Errorf("request %d offers tool definitions: a planning pass must be offered none", i)
		}
		if strings.Contains(b, "precise coding agent") || !strings.Contains(b, "planning assistant") {
			t.Errorf("request %d must use the planner system prompt, not the coding agent's", i)
		}
	}
}

func TestClientCompleterReportsAnEmptyReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + `{"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := ClientCompleter{Client: llm.New(srv.URL, "", "m", 5*time.Second, false)}
	if _, err := c.Complete(context.Background(), "p"); err == nil {
		t.Error("an empty reply must be an error, not an empty string")
	}
}
