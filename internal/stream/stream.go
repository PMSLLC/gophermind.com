// Package stream implements a non-interactive `--print` mode that speaks a
// documented subset of Claude Code's stream-json protocol, so external drivers
// (e.g. OpenCoven's Coven) can run gophermind programmatically. It maps the
// agent's existing progress events onto newline-delimited JSON messages.
package stream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gophermind/internal/agent"
)

// NewSessionID returns a random hex session identifier.
func NewSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "session"
	}
	return hex.EncodeToString(b[:])
}

// Session is the minimal agent surface the runner drives: one turn per Send.
// *agent.Agent satisfies it. Progress events reach the encoder via the agent's
// onEvent hook, wired to Encoder.Handle by the caller.
type Session interface {
	Send(ctx context.Context, userInput string) (string, error)
}

// Options configures a print-mode run.
type Options struct {
	In          io.Reader // stream-json input source
	InputFormat string    // "text" | "stream-json"
	Prompt      string    // used when InputFormat == "text"
	Model       string
	Tools       []string
	Cwd         string
}

// Run drives print mode: writes the init line, then one turn per input (a single
// prompt for text input, or one NDJSON user message per line for stream-json),
// emitting a result line after each turn.
func Run(ctx context.Context, enc *Encoder, sess Session, opts Options) error {
	if err := enc.Init(opts.Model, opts.Tools, opts.Cwd); err != nil {
		return err
	}
	turn := func(input string) error {
		answer, sErr := sess.Send(ctx, input)
		if sErr != nil {
			return enc.Result(sErr.Error(), true)
		}
		return enc.Result(answer, false)
	}

	if opts.InputFormat == "stream-json" {
		sc := bufio.NewScanner(opts.In)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // tolerate large turns
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			text, err := parseUserText(line)
			if err != nil {
				return fmt.Errorf("parse input: %w", err)
			}
			if err := turn(text); err != nil {
				return err
			}
		}
		return sc.Err()
	}
	return turn(opts.Prompt)
}

// parseUserText extracts the prompt text from a stream-json user message line.
// Content may be a bare string or an array of {type:"text",text:...} blocks.
func parseUserText(line []byte) (string, error) {
	var m struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return "", err
	}
	if len(m.Message.Content) == 0 {
		return "", fmt.Errorf("message has no content")
	}
	var s string
	if err := json.Unmarshal(m.Message.Content, &s); err == nil {
		return s, nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Message.Content, &blocks); err != nil {
		return "", fmt.Errorf("content is neither string nor block array")
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), nil
}

// contentBlock is one item in a message's content array.
type contentBlock struct {
	Type      string          `json:"type"` // "text" | "tool_use" | "tool_result"
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type innerMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

// Encoder serializes a session's agent events as stream-json lines to w. One
// Encoder is used for the whole process; Init is written once, then Handle per
// event, then Result per completed turn.
type Encoder struct {
	w         io.Writer
	sessionID string
	toolSeq   int
	lastTool  string // id of the most recent tool_use, to correlate its result
	turns     int
	usage     agent.UsageSnapshot
}

// NewEncoder builds an Encoder writing to w for the given session id.
func NewEncoder(w io.Writer, sessionID string) *Encoder {
	return &Encoder{w: w, sessionID: sessionID}
}

func (e *Encoder) writeLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := e.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// Init writes the leading system/init line describing the session.
func (e *Encoder) Init(model string, tools []string, cwd string) error {
	if tools == nil {
		tools = []string{}
	}
	return e.writeLine(map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": e.sessionID,
		"model":      model,
		"tools":      tools,
		"cwd":        cwd,
	})
}

// Handle serializes a single agent event. token and usage events produce no
// output line (tokens are display deltas; usage is tracked for the result).
func (e *Encoder) Handle(ev agent.Event) error {
	switch ev.Type {
	case "assistant":
		if ev.Text == "" {
			return nil
		}
		return e.message("assistant", "assistant", []contentBlock{{Type: "text", Text: ev.Text}})

	case "tool_call":
		e.toolSeq++
		e.lastTool = fmt.Sprintf("toolu_%d", e.toolSeq)
		input := json.RawMessage(ev.Text)
		if len(input) == 0 || !json.Valid(input) {
			input = json.RawMessage(`{}`)
		}
		return e.message("assistant", "assistant", []contentBlock{{
			Type: "tool_use", ID: e.lastTool, Name: ev.Name, Input: input,
		}})

	case "tool_result":
		return e.message("user", "user", []contentBlock{{
			Type: "tool_result", ToolUseID: e.lastTool, Content: ev.Text,
		}})

	case "usage":
		e.usage = ev.Usage
		return nil

	default: // "token" and anything else: no output line
		return nil
	}
}

func (e *Encoder) message(lineType, role string, content []contentBlock) error {
	return e.writeLine(map[string]any{
		"type":       lineType,
		"session_id": e.sessionID,
		"message":    innerMessage{Role: role, Content: content},
	})
}

// Result writes the terminal result line for a turn and advances the turn count.
func (e *Encoder) Result(finalText string, isError bool) error {
	e.turns++
	subtype := "success"
	if isError {
		subtype = "error"
	}
	return e.writeLine(map[string]any{
		"type":           "result",
		"subtype":        subtype,
		"is_error":       isError,
		"result":         finalText,
		"session_id":     e.sessionID,
		"num_turns":      e.turns,
		"input_tokens":   e.usage.PromptTokens,
		"output_tokens":  e.usage.CompletionTokens,
		"total_cost_usd": e.usage.CostUSD,
	})
}
