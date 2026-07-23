package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// TestStuckModelStopsEarly covers the reported symptom: a model that leaks
// Hermes-format scaffolding (a bare "</tool_response>") as its assistant prose
// while re-issuing the identical tool call forever. Before the stuck-loop
// guard this ran to MaxIter, echoing the tag 25 times; now it aborts after a
// few identical passes with an error that names the cause.
func TestStuckModelStopsEarly(t *testing.T) {
	const leak = "</tool_response>"

	// Every response: the leaked tag as content, plus a tool call, so the loop
	// never sees a "final answer" and keeps going to the iteration ceiling.
	body := "data: " + `{"choices":[{"delta":{"content":"` + leak + `"}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x.txt\"}"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(t.TempDir()))

	const maxIter = 25
	var assistantEchoes int
	var streamed strings.Builder
	a := New(client, reg, maxIter, nil, func(e Event) {
		switch e.Type {
		case "assistant":
			if strings.Contains(e.Text, leak) {
				assistantEchoes++
			}
		case "token":
			streamed.WriteString(e.Text)
		}
	})

	_, err := a.Send(context.Background(), "go")
	if err == nil {
		t.Fatal("expected an error when the model makes no progress, got nil")
	}
	t.Logf("terminating error: %v", err)
	t.Logf("assistant echoes of %q: %d", leak, assistantEchoes)

	if !errors.Is(err, ErrStuckLoop) {
		t.Errorf("error = %v, want it to wrap ErrStuckLoop", err)
	}
	if errors.Is(err, ErrMaxIterations) {
		t.Errorf("ran all the way to the iteration ceiling: %v", err)
	}
	// The guard must fire long before MaxIter, so the user sees a handful of
	// repeats instead of 25.
	if assistantEchoes > stuckRepeats {
		t.Errorf("assistant echoes = %d, want <= %d", assistantEchoes, stuckRepeats)
	}
	if assistantEchoes >= maxIter {
		t.Errorf("assistant echoes = %d; the guard did not fire", assistantEchoes)
	}
}

// TestLegitimateRepeatedCallsStillWork guards against over-firing: a model that
// repeats a call but is otherwise making progress (different prose, then a real
// answer) must not be cut off.
func TestLegitimateRepeatedCallsStillWork(t *testing.T) {
	bodies := []string{
		"data: " + `{"choices":[{"delta":{"content":"checking"}}]}` + "\n\n" +
			"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x.txt\"}"}}]}}]}` + "\n\n" +
			"data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n",
		// Same tool + args again, but different prose: still progressing.
		"data: " + `{"choices":[{"delta":{"content":"checking once more"}}]}` + "\n\n" +
			"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x.txt\"}"}}]}}]}` + "\n\n" +
			"data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n",
		"data: " + `{"choices":[{"delta":{"content":"all done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n",
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
	a := New(client, reg, 25, nil, func(Event) {})

	got, err := a.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "all done" {
		t.Errorf("answer = %q, want %q", got, "all done")
	}
}
