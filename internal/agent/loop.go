// Package agent implements the core agentic loop: it sends the conversation
// plus tool definitions to the model, executes any tool calls the model
// returns, feeds the results back, and repeats until the model produces a
// final answer with no tool calls.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gophermind/internal/llm"
	"gophermind/internal/safety"
	"gophermind/internal/tools"
)

// Event reports loop progress to an observer (e.g. the CLI), for display only.
type Event struct {
	Type  string        // "token" | "assistant" | "tool_call" | "tool_result" | "usage"
	Name  string        // tool name (for tool_call / tool_result)
	Text  string        // assistant prose, tool args, or tool result
	Usage UsageSnapshot // populated for Type == "usage": running session totals
}

// Agent drives the tool-calling loop and retains the conversation across
// turns so it can be used as an interactive session.
type Agent struct {
	llm         *llm.Client
	reg         *tools.Registry
	maxIter     int
	approve     safety.ApprovalFunc
	onEvent     func(Event)
	msgs        []llm.Message         // persistent conversation, seeded with the system prompt
	usage       UsageMeter            // running per-session token + cost accumulator
	caps        llm.Capabilities      // probed model capabilities (context window, etc.)
	budget      int                   // per-turn tool-call budget (0 = unlimited)
	checkpoints *checkpoints          // named conversation snapshots
	guardrails  Guardrails            // cost/time limits for autonomous runs
	startTime   time.Time             // when the agent was created (for duration tracking)
	redactor    *safety.SecretScanner // when non-nil, transcript content is scrubbed on export
}

// SetRedactTranscript enables (or disables) secret/PII redaction of message
// content when the transcript is exported. Off by default.
func (a *Agent) SetRedactTranscript(on bool) {
	if on {
		a.redactor = safety.NewSecretScanner()
	} else {
		a.redactor = nil
	}
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
		llm:         client,
		reg:         reg,
		maxIter:     maxIter,
		approve:     approve,
		onEvent:     onEvent,
		msgs:        []llm.Message{{Role: "system", Content: systemPrompt}},
		checkpoints: newCheckpoints(),
		startTime:   time.Now(),
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

// SetCapabilities records the probed model capabilities so the agent can adapt
// trimming and iteration limits to the real backend.
func (a *Agent) SetCapabilities(caps llm.Capabilities) { a.caps = caps }

// Temperature returns the client's current sampling temperature.
func (a *Agent) Temperature() float64 { return a.llm.Temperature() }

// TopP returns the client's current top_p (nil when unset).
func (a *Agent) TopP() *float64 { return a.llm.TopP() }

// Send adds a user turn to the conversation and runs the tool loop until the
// model produces a final answer (a reply with no tool calls). The conversation
// is retained, so subsequent Send calls continue the same session.
func (a *Agent) Send(ctx context.Context, userInput string) (string, error) {
	// Snapshot the history length so a turn that fails mid-stream (e.g. a Ctrl-C
	// cancellation) can be rolled back to exactly the prior completed state. This
	// drops both the user message added here and any committed assistant/tool
	// turns from this Send, so the next request is never malformed (no trailing
	// user turn without a reply, no assistant tool_call without tool results).
	base := len(a.msgs)
	a.msgs = append(a.msgs, llm.Message{Role: "user", Content: userInput})
	defs := a.reg.Definitions()

	for i := 0; i < a.maxIter; i++ {
		if err := ctx.Err(); err != nil {
			a.msgs = a.msgs[:base]
			return "", err
		}

		// Token-aware request trimming: if the conversation exceeds the model's
		// context window, drop the oldest user/tool turns so the request fits.
		// This prevents hard-failing on context-length errors during long sessions.
		if a.caps.ContextWindow > 0 {
			// Reserve 20% of the window for the response.
			budget := int(float64(a.caps.ContextWindow) * 0.8)
			if est := llm.EstimateMessagesTokens(a.msgs); est > budget {
				a.msgs, _ = llm.TrimToBudget(a.msgs, budget)
			}
		}

		reply, usage, err := a.llm.Stream(ctx, a.msgs, defs, func(tok string) {
			a.onEvent(Event{Type: "token", Text: tok})
		})
		if err != nil {
			// Discard the partial assistant turn (never appended) and the rest of
			// this Send's messages, restoring the last clean conversation state.
			a.msgs = a.msgs[:base]
			return "", fmt.Errorf("iteration %d: %w", i, err)
		}
		a.usage.Add(usage)
		a.onEvent(Event{Type: "usage", Usage: a.usage.Snapshot()})
		a.msgs = append(a.msgs, reply) // append the assistant turn before any tool results

		// Cost/time guardrails: abort an autonomous run that exceeds its ceilings,
		// returning whatever progress the model produced. All ceilings default to 0
		// (unlimited), so this is a no-op unless guardrails are configured.
		if msg, stop := a.guardrails.check(a.usage.Snapshot(), time.Since(a.startTime)); stop {
			a.onEvent(Event{Type: "assistant", Text: "⚠ " + msg})
			return reply.Content, nil
		}

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
	return "", fmt.Errorf("hit max iterations (%d) without a final answer: %w", a.maxIter, ErrMaxIterations)
}

// ErrMaxIterations is the sentinel wrapped when a turn exhausts the iteration
// budget without producing a final answer. Callers (e.g. the stream runner) use
// errors.Is to distinguish it from an execution error and report a distinct
// result subtype.
var ErrMaxIterations = errors.New("max iterations reached")

// ExportJSONL writes the full wire-level message history as JSONL: one JSON
// object per line, each the exact llm.Message as sent to / received from the
// API (role, content, tool_calls, tool_call_id, name). Lines are emitted in
// conversation order and each round-trips back to an llm.Message. Only the
// message history is written — no API key, Authorization header, base URL, or
// other client config is ever included. The writer is buffered internally and
// flushed before returning; messages are serialized one line at a time rather
// than buffering the whole transcript in memory.
func (a *Agent) ExportJSONL(w io.Writer) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	// json.Encoder.Encode writes a trailing newline after each value and
	// escapes any embedded newlines in content, so every message is exactly
	// one line of valid JSON.
	for i := range a.msgs {
		m := a.msgs[i]
		if a.redactor != nil {
			m.Content = a.redactor.Redact(m.Content)
		}
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("encode message %d: %w", i, err)
		}
	}
	return bw.Flush()
}

// WriteTranscript dumps the full message history as JSONL to path, an explicit
// user-provided destination. Because the transcript can contain sensitive
// prompt/response content, the file is created with 0600 permissions and any
// parent directory it must create with 0700. The file is truncated and
// overwritten so the dump always reflects the complete current session (for the
// one-shot run/ask modes this runs once at session end). An empty path is an
// error. Symlink/overwrite surprises are bounded by opening with O_CREATE|
// O_WRONLY|O_TRUNC and restrictive perms; the path is the user's own choice, so
// it is intentionally not contained to the repo root.
func (a *Agent) WriteTranscript(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("transcript path is empty")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// 0700: the directory may hold sensitive transcripts; keep it private.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create transcript dir: %w", err)
		}
	}
	// O_TRUNC overwrites a prior dump; 0600 keeps the file owner-only at rest.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open transcript file: %w", err)
	}
	if err := a.ExportJSONL(f); err != nil {
		f.Close()
		return fmt.Errorf("write transcript: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close transcript file: %w", err)
	}
	return nil
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
