package ui

import (
	"testing"

	"gophermind/gophermind-lib/llm"
)

// TestApplyHistory_SkipsSystemMessage covers "resume: ... chat transcript
// loads": the seeded system prompt is internal, not something the user
// typed or should see re-rendered as a transcript entry.
func TestApplyHistory_SkipsSystemMessage(t *testing.T) {
	tr := NewTranscript()
	ApplyHistory(tr, []llm.Message{{Role: "system", Content: "you are an agent"}})
	if len(tr.Messages()) != 0 {
		t.Errorf("Messages() = %+v, want none (system message skipped)", tr.Messages())
	}
}

func TestApplyHistory_UserAndAssistantText(t *testing.T) {
	tr := NewTranscript()
	ApplyHistory(tr, []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	})
	msgs := tr.Messages()
	if len(msgs) != 2 {
		t.Fatalf("Messages() = %+v, want 2", msgs)
	}
	if msgs[0].Role != RoleUser || msgs[0].Text != "hello" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != RoleAssistant || msgs[1].Text != "hi there" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
}

func TestApplyHistory_AssistantToolCallBecomesToolCallEntry(t *testing.T) {
	tr := NewTranscript()
	ApplyHistory(tr, []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "run_shell", Arguments: `{"command":"ls"}`}},
		}},
	})
	msgs := tr.Messages()
	if len(msgs) != 1 {
		t.Fatalf("Messages() = %+v, want 1", msgs)
	}
	if msgs[0].Role != RoleToolCall || msgs[0].ToolName != "run_shell" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
}

func TestApplyHistory_ToolResultUsesNameField(t *testing.T) {
	tr := NewTranscript()
	ApplyHistory(tr, []llm.Message{
		{Role: "tool", Name: "run_shell", ToolCallID: "c1", Content: "file1\nfile2"},
	})
	msgs := tr.Messages()
	if len(msgs) != 1 {
		t.Fatalf("Messages() = %+v, want 1", msgs)
	}
	if msgs[0].Role != RoleToolResult || msgs[0].ToolName != "run_shell" || msgs[0].Text != "file1\nfile2" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
}

func TestApplyHistory_MultipleToolCallsBecomeSeparateEntries(t *testing.T) {
	tr := NewTranscript()
	ApplyHistory(tr, []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{Function: llm.FunctionCall{Name: "a", Arguments: "{}"}},
			{Function: llm.FunctionCall{Name: "b", Arguments: "{}"}},
		}},
	})
	msgs := tr.Messages()
	if len(msgs) != 2 || msgs[0].ToolName != "a" || msgs[1].ToolName != "b" {
		t.Errorf("Messages() = %+v", msgs)
	}
}
