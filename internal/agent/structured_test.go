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

func TestStructuredOutputReturnsToolArguments(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		// Non-streaming completion carrying a forced tool call.
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"respond","arguments":"{\"answer\":42,\"unit\":\"C\"}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer srv.Close()

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	a := New(client, tools.NewRegistry(), 5, nil, nil)

	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"answer": map[string]any{"type": "integer"}, "unit": map[string]any{"type": "string"}},
		"required":   []string{"answer"},
	}
	out, err := a.StructuredOutput(context.Background(), "what temp?", schema)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output not JSON: %v (%q)", err, out)
	}
	if parsed["answer"].(float64) != 42 {
		t.Errorf("answer = %v", parsed["answer"])
	}
	// The request must have forced the respond tool.
	if !strings.Contains(gotBody, `"name":"respond"`) {
		t.Errorf("request did not force the respond tool: %s", gotBody)
	}
}

func TestStructuredOutputErrorsWithoutToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"just text"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	a := New(client, tools.NewRegistry(), 5, nil, nil)
	if _, err := a.StructuredOutput(context.Background(), "x", map[string]any{"type": "object"}); err == nil {
		t.Error("expected error when the model returns no structured tool call")
	}
}
