package agent

import (
	"context"
	"fmt"

	"gophermind/internal/llm"
)

// StructuredOutput runs a single, non-streaming turn that forces the model to
// answer by calling a synthetic "respond" tool whose parameters are the caller's
// JSON schema. It returns the tool call's raw JSON arguments — a schema-guided,
// reliably-structured result — without engaging the tool loop. The forced
// tool_choice is set only for this call and restored afterward.
func (a *Agent) StructuredOutput(ctx context.Context, task string, schema map[string]any) (string, error) {
	respondTool := llm.Tool{
		Type: "function",
		Function: llm.Function{
			Name:        "respond",
			Description: "Return the final answer as JSON matching the required schema.",
			Parameters:  schema,
		},
	}

	msgs := append(append([]llm.Message{}, a.msgs...), llm.Message{Role: "user", Content: task})

	a.llm.SetToolChoice(&llm.ToolChoiceConfig{Forced: &llm.ToolChoiceForced{Name: "respond"}})
	defer a.llm.SetToolChoice(nil)

	reply, usage, err := a.llm.Complete(ctx, msgs, []llm.Tool{respondTool})
	if err != nil {
		return "", err
	}
	a.usage.Add(usage)
	a.onEvent(Event{Type: "usage", Usage: a.usage.Snapshot()})

	for _, tc := range reply.ToolCalls {
		if tc.Function.Name == "respond" {
			return tc.Function.Arguments, nil
		}
	}
	return "", fmt.Errorf("model did not produce structured output")
}
