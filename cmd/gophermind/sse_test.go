package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"gophermind/internal/agent"
)

func TestWriteSSEEvent_Typed(t *testing.T) {
	var buf bytes.Buffer
	writeSSEEvent(&buf, nil, "token", "hi")
	want := "event: token\ndata: hi\n\n"
	if got := buf.String(); got != want {
		t.Errorf("writeSSEEvent() = %q, want %q", got, want)
	}
}

func TestWriteSSEEvent_MultiLineData(t *testing.T) {
	var buf bytes.Buffer
	writeSSEEvent(&buf, nil, "assistant", "line1\nline2\nline3")
	want := "event: assistant\ndata: line1\ndata: line2\ndata: line3\n\n"
	if got := buf.String(); got != want {
		t.Errorf("writeSSEEvent() = %q, want %q", got, want)
	}
}

func TestWriteSSEEvent_EmptyEventOmitsEventLine(t *testing.T) {
	var buf bytes.Buffer
	writeSSEEvent(&buf, nil, "", "hi")
	want := "data: hi\n\n"
	if got := buf.String(); got != want {
		t.Errorf("writeSSEEvent() = %q, want %q", got, want)
	}
}

func TestWriteSSEEvent_EmptyDataYieldsOneDataLine(t *testing.T) {
	var buf bytes.Buffer
	writeSSEEvent(&buf, nil, "done", "")
	want := "event: done\ndata: \n\n"
	if got := buf.String(); got != want {
		t.Errorf("writeSSEEvent() = %q, want %q", got, want)
	}
}

func TestWriteSSEEvent_EmptyEventAndData(t *testing.T) {
	var buf bytes.Buffer
	writeSSEEvent(&buf, nil, "", "")
	want := "data: \n\n"
	if got := buf.String(); got != want {
		t.Errorf("writeSSEEvent() = %q, want %q", got, want)
	}
}

func TestWriteSSEEvent_CRLFNormalized(t *testing.T) {
	var buf bytes.Buffer
	writeSSEEvent(&buf, nil, "token", "a\r\nb\rc")
	want := "event: token\ndata: a\ndata: b\ndata: c\n\n"
	if got := buf.String(); got != want {
		t.Errorf("writeSSEEvent() = %q, want %q", got, want)
	}
}

func TestSSEFramesForAgentEvent_Token(t *testing.T) {
	ev := agent.Event{Type: "token", Text: "hello"}
	event, data, emit := sseFramesForAgentEvent(ev)
	if !emit {
		t.Fatal("expected emit=true for token")
	}
	if event != "token" || data != "hello" {
		t.Errorf("got (%q, %q), want (%q, %q)", event, data, "token", "hello")
	}
}

func TestSSEFramesForAgentEvent_Assistant(t *testing.T) {
	ev := agent.Event{Type: "assistant", Text: "final answer"}
	event, data, emit := sseFramesForAgentEvent(ev)
	if !emit {
		t.Fatal("expected emit=true for assistant")
	}
	if event != "assistant" || data != "final answer" {
		t.Errorf("got (%q, %q), want (%q, %q)", event, data, "assistant", "final answer")
	}
}

func TestSSEFramesForAgentEvent_ToolCall(t *testing.T) {
	ev := agent.Event{Type: "tool_call", Name: "read_file", Text: `{"path":"a.go"}`}
	event, data, emit := sseFramesForAgentEvent(ev)
	if !emit {
		t.Fatal("expected emit=true for tool_call")
	}
	if event != "tool_call" {
		t.Errorf("event = %q, want %q", event, "tool_call")
	}
	var parsed struct {
		Name string `json:"name"`
		Args string `json:"args"`
	}
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		t.Fatalf("data is not valid JSON: %v (data=%q)", err, data)
	}
	if parsed.Name != "read_file" || parsed.Args != `{"path":"a.go"}` {
		t.Errorf("parsed = %+v, want name=read_file args={\"path\":\"a.go\"}", parsed)
	}
}

func TestSSEFramesForAgentEvent_ToolResult(t *testing.T) {
	ev := agent.Event{Type: "tool_result", Name: "read_file", Text: "file contents"}
	event, data, emit := sseFramesForAgentEvent(ev)
	if !emit {
		t.Fatal("expected emit=true for tool_result")
	}
	if event != "tool_result" {
		t.Errorf("event = %q, want %q", event, "tool_result")
	}
	var parsed struct {
		Name string `json:"name"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		t.Fatalf("data is not valid JSON: %v (data=%q)", err, data)
	}
	if parsed.Name != "read_file" || parsed.Text != "file contents" {
		t.Errorf("parsed = %+v, want name=read_file text=\"file contents\"", parsed)
	}
}

func TestSSEFramesForAgentEvent_Usage(t *testing.T) {
	ev := agent.Event{
		Type: "usage",
		Usage: agent.UsageSnapshot{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			CostUSD:          0.02,
		},
	}
	event, data, emit := sseFramesForAgentEvent(ev)
	if !emit {
		t.Fatal("expected emit=true for usage")
	}
	if event != "usage" {
		t.Errorf("event = %q, want %q", event, "usage")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		t.Fatalf("data is not valid JSON: %v (data=%q)", err, data)
	}
	if parsed["PromptTokens"] != float64(100) || parsed["TotalTokens"] != float64(150) {
		t.Errorf("parsed usage JSON missing expected fields: %+v", parsed)
	}
}

func TestSSEFramesForAgentEvent_UnknownType(t *testing.T) {
	ev := agent.Event{Type: "bogus"}
	event, data, emit := sseFramesForAgentEvent(ev)
	if emit {
		t.Errorf("expected emit=false for unknown type, got event=%q data=%q", event, data)
	}
}
