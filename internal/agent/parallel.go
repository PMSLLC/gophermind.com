package agent

import (
	"context"
	"fmt"
	"sync"

	"gophermind/internal/llm"
)

// dispatchParallel runs tool calls concurrently instead of sequentially.
// Independent tool calls (reads, searches) are executed in parallel to
// reduce wall-clock time for multi-file operations.
func (a *Agent) dispatchParallel(ctx context.Context, calls []llm.ToolCall) []llm.Message {
	var mu sync.Mutex
	results := make([]llm.Message, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c llm.ToolCall) {
			defer wg.Done()
			out := a.dispatch(ctx, c)
			mu.Lock()
			results[idx] = llm.Message{
				Role:       "tool",
				ToolCallID: c.ID,
				Name:       c.Function.Name,
				Content:    out,
			}
			mu.Unlock()
		}(i, call)
	}

	wg.Wait()
	return results
}

// DispatchParallel is like Send but executes tool calls in parallel within
// each turn. This speeds up multi-file reads and searches that are
// independent of each other.
func (a *Agent) DispatchParallel(ctx context.Context, userInput string) (string, error) {
	base := len(a.msgs)
	a.msgs = append(a.msgs, llm.Message{Role: "user", Content: userInput})
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

		// Execute tool calls in parallel.
		msgs := a.dispatchParallel(ctx, reply.ToolCalls)
		a.msgs = append(a.msgs, msgs...)
	}
	return "", fmt.Errorf("hit max iterations (%d) without a final answer", a.maxIter)
}
