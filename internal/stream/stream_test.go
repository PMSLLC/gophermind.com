package stream

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/internal/agent"
)

// decodeLines splits the encoder output into one parsed JSON object per line.
func decodeLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("line is not valid JSON: %q: %v", ln, err)
		}
		out = append(out, m)
	}
	return out
}

func TestInitLine(t *testing.T) {
	var b strings.Builder
	e := NewEncoder(&b, "sess-1")
	if err := e.Init("qwen", []string{"read_file", "run_shell"}, "/repo"); err != nil {
		t.Fatal(err)
	}
	got := decodeLines(t, b.String())
	if len(got) != 1 {
		t.Fatalf("want 1 line, got %d", len(got))
	}
	m := got[0]
	if m["type"] != "system" || m["subtype"] != "init" {
		t.Errorf("init type/subtype = %v/%v", m["type"], m["subtype"])
	}
	if m["session_id"] != "sess-1" || m["model"] != "qwen" || m["cwd"] != "/repo" {
		t.Errorf("init fields wrong: %v", m)
	}
}

func TestToolUseAndResultAreCorrelated(t *testing.T) {
	var b strings.Builder
	e := NewEncoder(&b, "s")
	_ = e.Handle(agent.Event{Type: "tool_call", Name: "read_file", Text: `{"path":"a.go"}`})
	_ = e.Handle(agent.Event{Type: "tool_result", Name: "read_file", Text: "package main"})
	lines := decodeLines(t, b.String())
	if len(lines) != 2 {
		t.Fatalf("want 2 lines (assistant tool_use, user tool_result), got %d: %v", len(lines), lines)
	}
	// assistant tool_use
	asg := lines[0]
	if asg["type"] != "assistant" {
		t.Fatalf("first line type = %v, want assistant", asg["type"])
	}
	block := asg["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if block["type"] != "tool_use" || block["name"] != "read_file" {
		t.Errorf("tool_use block wrong: %v", block)
	}
	useID, _ := block["id"].(string)
	if useID == "" {
		t.Error("tool_use missing id")
	}
	// user tool_result must reference the same id
	res := lines[1]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if res["type"] != "tool_result" || res["tool_use_id"] != useID {
		t.Errorf("tool_result not correlated: block=%v useID=%q", res, useID)
	}
	if res["content"] != "package main" {
		t.Errorf("tool_result content = %v", res["content"])
	}
}

func TestAssistantNarrationLine(t *testing.T) {
	var b strings.Builder
	e := NewEncoder(&b, "s")
	_ = e.Handle(agent.Event{Type: "assistant", Text: "let me look"})
	// token/usage produce no lines
	_ = e.Handle(agent.Event{Type: "token", Text: "x"})
	_ = e.Handle(agent.Event{Type: "usage"})
	lines := decodeLines(t, b.String())
	if len(lines) != 1 {
		t.Fatalf("want 1 line (assistant text), got %d: %v", len(lines), lines)
	}
	block := lines[0]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "let me look" {
		t.Errorf("assistant text block wrong: %v", block)
	}
}

func TestResultLine(t *testing.T) {
	var b strings.Builder
	e := NewEncoder(&b, "s")
	if err := e.Result("all done"); err != nil {
		t.Fatal(err)
	}
	m := decodeLines(t, b.String())[0]
	if m["type"] != "result" || m["subtype"] != "success" {
		t.Errorf("result type/subtype = %v/%v", m["type"], m["subtype"])
	}
	if m["result"] != "all done" || m["is_error"] != false {
		t.Errorf("result fields wrong: %v", m)
	}
	if m["session_id"] != "s" {
		t.Errorf("result session_id = %v", m["session_id"])
	}
}
