// Package agent implements the core agentic loop: it sends the conversation
// plus tool definitions to the model, executes any tool calls the model
// returns, feeds the results back, and repeats until the model produces a
// final answer with no tool calls.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/llm"
	"gophermind/internal/safety"
	"gophermind/internal/tools"
)

// Event reports loop progress to an observer (e.g. the CLI), for display only.
type Event struct {
	Type  string // "token" | "assistant" | "tool_call" | "tool_result" | "usage"
	Name  string // tool name (for tool_call / tool_result)
	Text  string // assistant prose, tool args, or tool result
	Usage UsageSnapshot // populated for Type == "usage": running session totals
}

// Agent drives the tool-calling loop and retains the conversation across
// turns so it can be used as an interactive session.
type Agent struct {
	llm     *llm.Client
	reg     *tools.Registry
	maxIter int
	approve safety.ApprovalFunc
	onEvent func(Event)
	msgs    []llm.Message // persistent conversation, seeded with the system prompt
	usage   UsageMeter    // running per-session token + cost accumulator
}

// New builds an Agent. If onEvent is nil, progress events are discarded.
func New(client *llm.Client, reg *tools.Registry, maxIter int, approve safety.ApprovalFunc, onEvent func(Event)) *Agent {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	if approve == nil {
		approve = safety.Auto
	}
	return &Agent{
		llm:     client,
		reg:     reg,
		maxIter: maxIter,
		approve: approve,
		onEvent: onEvent,
		msgs:    []llm.Message{{Role: "system", Content: systemPrompt}},
	}
}

// SetPrices configures the per-1,000-token input and output prices (USD) used
// to estimate session cost. Both default to 0, so cost reads 0 when unset.
func (a *Agent) SetPrices(inputPer1K, outputPer1K float64) {
	a.usage.InputPricePer1K = inputPer1K
	a.usage.OutputPricePer1K = outputPer1K
}

// Usage returns a snapshot of the running per-session token totals and
// estimated cost.
func (a *Agent) Usage() UsageSnapshot { return a.usage.Snapshot() }

// SetTemperature updates the sampling temperature on the underlying client; it
// takes effect on the next request. Range validation is the caller's job.
func (a *Agent) SetTemperature(t float64) { a.llm.SetTemperature(t) }

// SetTopP updates the sampling top_p on the underlying client (nil unsets it);
// it takes effect on the next request. Range validation is the caller's job.
func (a *Agent) SetTopP(p *float64) { a.llm.SetTopP(p) }

// Temperature returns the client's current sampling temperature.
func (a *Agent) Temperature() float64 { return a.llm.Temperature() }

// TopP returns the client's current top_p (nil when unset).
func (a *Agent) TopP() *float64 { return a.llm.TopP() }

// Send adds a user turn to the conversation and runs the tool loop until the
// model produces a final answer (a reply with no tool calls). The conversation
// is retained, so subsequent Send calls continue the same session.
func (a *Agent) Send(ctx context.Context, userInput string) (string, error) {
	a.msgs = append(a.msgs, llm.Message{Role: "user", Content: userInput})
	defs := a.reg.Definitions()

	for i := 0; i < a.maxIter; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		reply, usage, err := a.llm.Stream(ctx, a.msgs, defs, func(tok string) {
			a.onEvent(Event{Type: "token", Text: tok})
		})
		if err != nil {
			return "", fmt.Errorf("iteration %d: %w", i, err)
		}
		a.usage.Add(usage)
		a.onEvent(Event{Type: "usage", Usage: a.usage.Snapshot()})
		a.msgs = append(a.msgs, reply) // append the assistant turn before any tool results

		// A reply with no tool calls is the final answer; the caller prints it.
		if len(reply.ToolCalls) == 0 {
			return reply.Content, nil
		}
		// Otherwise any prose is intermediate narration ("planning…") — show it.
		if reply.Content != "" {
			a.onEvent(Event{Type: "assistant", Text: reply.Content})
		}

		for _, call := range reply.ToolCalls {
			out := a.dispatch(ctx, call)
			a.msgs = append(a.msgs, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID, // must echo the call ID
				Name:       call.Function.Name,
				Content:    out,
			})
		}
	}
	return "", fmt.Errorf("hit max iterations (%d) without a final answer", a.maxIter)
}

// Reset clears the conversation back to just the system prompt.
func (a *Agent) Reset() {
	if len(a.msgs) > 0 {
		a.msgs = a.msgs[:1]
	}
}

// dispatch runs one tool call and returns the result string to feed back to
// the model. Tool errors are returned as text (never fatal) so the model can
// see and recover from them.
func (a *Agent) dispatch(ctx context.Context, call llm.ToolCall) string {
	name := call.Function.Name
	rawArgs := call.Function.Arguments
	a.onEvent(Event{Type: "tool_call", Name: name, Text: rawArgs})

	t, ok := a.reg.Get(name)
	if !ok {
		return "error: unknown tool " + name
	}
	if safety.Gated(name) && !a.approve(name, rawArgs) {
		return "denied by user"
	}

	out, err := t.Run(ctx, json.RawMessage(rawArgs))
	if err != nil {
		out = "error: " + err.Error()
	}
	a.onEvent(Event{Type: "tool_result", Name: name, Text: out})
	return out
}
