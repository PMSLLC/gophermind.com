package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRequestOmitsReasoningEffortWhenUnset(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(lastBody(), "reasoning_effort") {
		t.Errorf("reasoning_effort should be omitted when unset, got: %s", lastBody())
	}
}

func TestRequestSendsReasoningEffortWhenSet(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.SetReasoningEffort("high")
	if got := c.ReasoningEffort(); got != "high" {
		t.Fatalf("ReasoningEffort() = %q, want high", got)
	}
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var body ChatRequest
	if err := json.Unmarshal([]byte(lastBody()), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort = %q, want high", body.ReasoningEffort)
	}
}

func TestSetReasoningEffortEmptyUnsets(t *testing.T) {
	srv, lastBody := captureServer(t)
	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.SetReasoningEffort("low")
	c.SetReasoningEffort("")
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(lastBody(), "reasoning_effort") {
		t.Errorf("reasoning_effort should be omitted after unset, got: %s", lastBody())
	}
}
