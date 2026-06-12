package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompleteDecodesToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request carries our tool definition.
		var got ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(got.Tools) != 1 || got.Tools[0].Function.Name != "read_file" {
			t.Errorf("tools not forwarded: %+v", got.Tools)
		}
		if got.ToolChoice != "auto" {
			t.Errorf("tool_choice = %q, want auto", got.ToolChoice)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	tools := []Tool{{Type: "function", Function: Function{Name: "read_file"}}}
	msg, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, tools)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if tc.Function.Arguments != `{"path":"main.go"}` {
		t.Errorf("arguments = %q", tc.Function.Arguments)
	}
}

func TestCompleteFinalMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"all done"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	msg, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(msg.ToolCalls) != 0 {
		t.Errorf("expected no tool calls")
	}
	if msg.Content != "all done" {
		t.Errorf("content = %q", msg.Content)
	}
}

func TestCompleteSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad model"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	_, err := c.Complete(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if want := "bad model"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want to contain %q", err, want)
	}
}

func TestAuthHeaderOnlyWhenKeySet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	New(srv.URL, "", "m", time.Second, false).Complete(context.Background(), nil, nil)
	if gotAuth != "" {
		t.Errorf("auth header set with empty key: %q", gotAuth)
	}
	New(srv.URL, "secret", "m", time.Second, false).Complete(context.Background(), nil, nil)
	if gotAuth != "Bearer secret" {
		t.Errorf("auth header = %q, want Bearer secret", gotAuth)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
