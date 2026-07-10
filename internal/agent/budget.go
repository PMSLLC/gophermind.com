package agent

import (
	"context"
	"fmt"

	"gophermind/internal/llm"
)

// toolCallBudget is the default maximum number of tool calls per assistant turn.
const defaultToolCallBudget = 20

// toolCallBudgetTool is the tool definition for the per-turn budget tracker.
var toolCallBudgetTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        "_gophermind_budget",
		Description: "Track tool call count for this turn. Call after each tool execution.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"count": map[string]any{"type": "integer", "description": "Current tool call count this turn"}},
			"required":   []string{"count"},
		},
	},
}

// ToolCallBudget wraps the agent with a per-turn tool-call budget. When the
// budget is approached (within 20% of the limit), a warning is emitted. When
// the budget is reached, the model is instructed to stop and return a final
// answer.
//
// budget is the maximum tool calls per turn; 0 means unlimited (default).
func (a *Agent) WithToolCallBudget(budget int) *Agent {
	if budget <= 0 {
		budget = defaultToolCallBudget
	}
	return &Agent{
		llm:     a.llm,
		reg:     a.reg,
		maxIter: a.maxIter,
		approve: a.approve,
		onEvent: a.onEvent,
		msgs:    a.msgs,
		usage:   a.usage,
		caps:    a.caps,
		budget:  budget,
	}
}

// budget tracks how many tool calls have been made in the current turn.
type budgetState struct {
	count int
	limit int
}

func (bs *budgetState) approaching() bool {
	return bs.count >= int(float64(bs.limit)*0.8)
}

func (bs *budgetState) exhausted() bool {
	return bs.count >= bs.limit
}

// SendWithBudget is like Send but enforces a per-turn tool-call budget.
func (a *Agent) SendWithBudget(ctx context.Context, userInput string) (string, error) {
	base := len(a.msgs)
	a.msgs = append(a.msgs, llm.Message{Role: "user", Content: userInput})
	defs := a.reg.Definitions()
	bs := &budgetState{limit: a.budget}

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

		// Check budget before executing tool calls.
		if bs.exhausted() {
			a.onEvent(Event{Type: "assistant", Text: fmt.Sprintf("⚠ Tool call budget (%d) reached. Returning final answer.", a.budget)})
			a.msgs = append(a.msgs, llm.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("Tool call budget (%d) reached. Please provide a final answer based on the work done so far.", a.budget),
			})
			continue
		}
		if bs.approaching() {
			a.onEvent(Event{Type: "assistant", Text: fmt.Sprintf("⚠ Approaching tool call budget (%d/%d).", bs.count, a.budget)})
		}

		for _, call := range reply.ToolCalls {
			out := a.dispatch(ctx, call)
			bs.count++
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
