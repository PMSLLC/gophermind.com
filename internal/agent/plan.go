package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/llm"
)

// planSchema is the JSON-Schema for the structured plan the model emits in
// plan-then-execute mode. It is a small, fixed schema so the model can produce
// it reliably.
var planSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"plan": map[string]any{
			"type":        "array",
			"description": "Ordered list of steps to complete the task.",
			"items": map[string]any{
				"type":       "object",
				"properties": map[string]any{"step": map[string]any{"type": "string", "description": "One concrete action."}},
				"required":   []string{"step"},
			},
		},
		"rationale": map[string]any{
			"type":        "string",
			"description": "Brief explanation of the approach.",
		},
	},
	"required": []string{"plan"},
}

// planTool is the tool definition the model uses to emit its structured plan.
var planTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        "_gophermind_plan",
		Description: "Emit a structured plan of steps before executing. Call this first with your plan, then execute each step.",
		Parameters:  planSchema,
	},
}

// PlanThenExecute runs the agent in plan-then-execute mode. It first asks the
// model to emit a structured plan (via the _gophermind_plan tool), shows it to
// the user through onEvent, then proceeds with normal tool execution. The plan
// turn is included in the conversation history so the model references it during
// execution.
//
// If the model does not emit the plan tool (e.g. it jumps straight to tools),
// the function falls through to normal execution with the user prompt.
func (a *Agent) PlanThenExecute(ctx context.Context, userInput string) (string, error) {
	base := len(a.msgs)
	a.msgs = append(a.msgs, llm.Message{Role: "user", Content: userInput})

	// First pass: ask for a plan.
	defs := a.reg.Definitions()
	// Prepend the plan tool so it's available first.
	allDefs := append([]llm.Tool{planTool}, defs...)

	reply, usage, err := a.llm.Stream(ctx, a.msgs, allDefs, func(tok string) {
		a.onEvent(Event{Type: "token", Text: tok})
	})
	if err != nil {
		a.msgs = a.msgs[:base]
		return "", fmt.Errorf("plan pass: %w", err)
	}
	a.usage.Add(usage)
	a.onEvent(Event{Type: "usage", Usage: a.usage.Snapshot()})
	a.msgs = append(a.msgs, reply)

	// Check if the model emitted the plan tool.
	if len(reply.ToolCalls) == 0 || reply.ToolCalls[0].Function.Name != "_gophermind_plan" {
		// Model skipped the plan; fall through to normal execution.
		return a.executeTurns(ctx, userInput, base)
	}

	// Parse and show the plan.
	planText, err := parsePlan(reply.ToolCalls[0].Function.Arguments)
	if err != nil {
		// Plan parse failed; fall through to normal execution.
		return a.executeTurns(ctx, userInput, base)
	}
	a.onEvent(Event{Type: "assistant", Text: "📋 Plan:\n" + planText})

	// Add the plan result as a tool result so the model acknowledges it.
	a.msgs = append(a.msgs, llm.Message{
		Role:       "tool",
		ToolCallID: reply.ToolCalls[0].ID,
		Name:       "_gophermind_plan",
		Content:    "Plan acknowledged. Executing steps.",
	})

	// Second pass: execute with normal tools.
	return a.executeTurns(ctx, userInput, base)
}

// parsePlan extracts a human-readable plan string from the model's JSON plan
// tool arguments. It returns the formatted plan or an error if the JSON is
// unparseable.
func parsePlan(args string) (string, error) {
	var plan struct {
		Plan      []struct{ Step string } `json:"plan"`
		Rationale string                  `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(args), &plan); err != nil {
		return "", fmt.Errorf("parse plan: %w", err)
	}
	var b strings.Builder
	for i, s := range plan.Plan {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s.Step)
	}
	if plan.Rationale != "" {
		fmt.Fprintf(&b, "\nRationale: %s\n", plan.Rationale)
	}
	return b.String(), nil
}

// executeTurns runs the normal tool loop starting from the current conversation
// state (msgs up to base are the pre-turn snapshot). It continues until the
// model produces a final answer or maxIter is reached.
func (a *Agent) executeTurns(ctx context.Context, userInput string, base int) (string, error) {
	defs := a.reg.Definitions()

	for i := 0; i < a.maxIter; i++ {
		if err := ctx.Err(); err != nil {
			a.msgs = a.msgs[:base]
			return "", err
		}

		if a.caps.ContextWindow > 0 {
			budget := int(float64(a.caps.ContextWindow) * 0.8)
			if est := llm.EstimateMessagesTokens(a.msgs); est > budget {
				a.msgs, _ = llm.TrimToBudget(a.msgs, budget)
			}
		}

		reply, usage, err := a.llm.Stream(ctx, a.msgs, defs, func(tok string) {
			a.onEvent(Event{Type: "token", Text: tok})
		})
		if err != nil {
			a.msgs = a.msgs[:base]
			return "", fmt.Errorf("iteration %d: %w", i, err)
		}
		a.usage.Add(usage)
		a.onEvent(Event{Type: "usage", Usage: a.usage.Snapshot()})
		a.msgs = append(a.msgs, reply)

		if len(reply.ToolCalls) == 0 {
			return reply.Content, nil
		}
		if reply.Content != "" {
			a.onEvent(Event{Type: "assistant", Text: reply.Content})
		}

		for _, call := range reply.ToolCalls {
			out := a.dispatch(ctx, call)
			a.msgs = append(a.msgs, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    out,
			})
		}
	}
	return "", fmt.Errorf("hit max iterations (%d) without a final answer", a.maxIter)
}
