# GopherMind TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the line-based REPL with a Claude-Code-style Bubble Tea TUI (streaming markdown, tool-call blocks, inline approvals, status line) on top of the existing agent engine.

**Architecture:** The agent engine stays headless and runs in a goroutine; it emits events and approval requests onto a single channel that a Bubble Tea program drains as `tea.Msg`s. The TUI (`internal/tui`) is pure rendering + key routing; the engine has no terminal knowledge. Streaming is added to `internal/llm` (SSE); the final assembled message still drives tool calls.

**Tech Stack:** Go 1.23, charmbracelet/bubbletea v1.3.10, lipgloss v1.1.0, bubbles v1.0.0, glamour v1.0.0. Engine packages remain stdlib-only.

---

## File Structure

```
go.mod                              add Charm deps
internal/llm/stream.go              NEW: Stream() SSE client + tool_call reassembly
internal/llm/stream_test.go         NEW
internal/agent/loop.go              MODIFY: add token streaming events; Send uses Stream
internal/agent/loop_test.go         MODIFY: assert token events
internal/tui/messages.go            NEW: tea.Msg types + event→msg bridge
internal/tui/model.go               NEW: Model struct, New, Init
internal/tui/update.go              NEW: Update (keys, agent msgs, approval)
internal/tui/update_test.go         NEW
internal/tui/view.go                NEW: View (transcript + input + status)
internal/tui/run.go                 NEW: Run() entrypoint, builds agent + program
cmd/gophermind/main.go              MODIFY: interactive → tui.Run; run/ask unchanged
```

`internal/ui` (plain Printer/Confirm) stays for one-shot `run`/`ask`. The old
`repl()` function in `main.go` is removed.

---

## Shared type contract (defined here, used across tasks — keep names exact)

`internal/agent` event types (string constants on `Event.Type`):
`"token"`, `"assistant"`, `"tool_call"`, `"tool_result"`.

`internal/tui` message types:
```go
type tokenMsg string                                   // streamed prose delta
type assistantMsg string                               // intermediate narration (turn had tool calls)
type toolCallMsg struct{ name, args string }
type toolResultMsg struct{ name, text string }
type approvalMsg struct {
	tool, args string
	reply      chan bool
}
type doneMsg struct{ answer string }
type errMsg struct{ err error }
```

---

## Task 1: Add Charm dependencies

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add the four libraries**

Run:
```bash
go get github.com/charmbracelet/bubbletea@v1.3.10 \
       github.com/charmbracelet/lipgloss@v1.1.0 \
       github.com/charmbracelet/bubbles@v1.0.0 \
       github.com/charmbracelet/glamour@v1.0.0
```
Expected: `go.mod`/`go.sum` updated, downloads succeed.

- [ ] **Step 2: Verify the module still builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add Charm TUI dependencies (bubbletea, lipgloss, bubbles, glamour)"
```

---

## Task 2: LLM streaming (SSE) with tool-call reassembly

**Files:**
- Create: `internal/llm/stream.go`
- Test: `internal/llm/stream_test.go`

The endpoint streams OpenAI-style chunks: `data: {"choices":[{"delta":{...}}]}` lines
ending with `data: [DONE]`. Prose arrives as `delta.content`; tool calls arrive as
`delta.tool_calls` with an `index`, and `function.arguments` is **fragmented across
chunks** and must be concatenated per index.

- [ ] **Step 1: Write the failing test**

```go
package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sse(w http.ResponseWriter, lines ...string) {
	f, _ := w.(http.Flusher)
	for _, l := range lines {
		w.Write([]byte("data: " + l + "\n\n"))
		if f != nil {
			f.Flush()
		}
	}
}

func TestStreamProseAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"x\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	var got string
	msg, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "Hello" {
		t.Errorf("tokens = %q, want Hello", got)
	}
	if msg.Content != "Hello" {
		t.Errorf("content = %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" || tc.Function.Arguments != `{"path":"x"}` {
		t.Errorf("reassembled tool call wrong: %+v", tc)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestStreamProseAndToolCalls -v`
Expected: FAIL — `c.Stream` undefined.

- [ ] **Step 3: Implement `Stream`**

Create `internal/llm/stream.go`:
```go
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// streamChunk is one SSE delta frame.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Stream performs a streaming chat completion. onToken is called for each prose
// delta. The fully assembled assistant message (including reassembled tool calls)
// is returned when the stream ends.
func (c *Client) Stream(ctx context.Context, msgs []Message, tools []Tool, onToken func(string)) (Message, error) {
	reqBody := ChatRequest{Model: c.Model, Messages: msgs, Tools: tools, Temperature: 0, Stream: true}
	if len(tools) > 0 {
		reqBody.ToolChoice = "auto"
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return Message{}, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Message{}, fmt.Errorf("status %d", resp.StatusCode)
	}

	var content strings.Builder
	// Accumulate tool-call fragments keyed by index.
	type acc struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*acc{}
	var order []int

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // tolerate keep-alives / partial frames
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			content.WriteString(d.Content)
			if onToken != nil {
				onToken(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			a := calls[tc.Index]
			if a == nil {
				a = &acc{}
				calls[tc.Index] = a
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				a.id = tc.ID
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
			}
			a.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		return Message{}, fmt.Errorf("read stream: %w", err)
	}

	msg := Message{Role: "assistant", Content: content.String()}
	for _, idx := range order {
		a := calls[idx]
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:       a.id,
			Type:     "function",
			Function: FunctionCall{Name: a.name, Arguments: a.args.String()},
		})
	}
	return msg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestStreamProseAndToolCalls -v`
Expected: PASS.

- [ ] **Step 5: Run the whole llm package**

Run: `go test ./internal/llm/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/llm/stream.go internal/llm/stream_test.go
git commit -m "feat(llm): streaming completions with tool-call reassembly"
```

---

## Task 3: Agent emits token events and uses streaming

**Files:**
- Modify: `internal/agent/loop.go`
- Modify: `internal/agent/loop_test.go`

`Send` currently calls `a.llm.Complete`. Switch to `a.llm.Stream`, forwarding each
prose delta as a `"token"` event. The assembled message drives tool calls exactly
as before.

- [ ] **Step 1: Write the failing test (append to loop_test.go)**

The existing `scriptedProvider` returns whole JSON bodies. Add an SSE-capable case:
```go
func TestSendStreamsTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range []string{
			`{"choices":[{"delta":{"content":"Hi "}}]}`,
			`{"choices":[{"delta":{"content":"there"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`[DONE]`,
		} {
			w.Write([]byte("data: " + l + "\n\n"))
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(t.TempDir()))
	var tokens string
	a := New(client, reg, 25, nil, func(e Event) {
		if e.Type == "token" {
			tokens += e.Text
		}
	})
	out, err := a.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if out != "Hi there" {
		t.Errorf("answer = %q", out)
	}
	if tokens != "Hi there" {
		t.Errorf("tokens = %q", tokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestSendStreamsTokens -v`
Expected: FAIL — no token events emitted (tokens empty), since `Send` still uses `Complete`.

- [ ] **Step 3: Switch `Send` to streaming**

In `internal/agent/loop.go`, replace the line:
```go
		reply, err := a.llm.Complete(ctx, a.msgs, defs)
```
with:
```go
		reply, err := a.llm.Stream(ctx, a.msgs, defs, func(tok string) {
			a.onEvent(Event{Type: "token", Text: tok})
		})
```

- [ ] **Step 4: Run the new test**

Run: `go test ./internal/agent/ -run TestSendStreamsTokens -v`
Expected: PASS.

- [ ] **Step 5: Run the whole agent package (existing tests must still pass)**

Run: `go test ./internal/agent/`
Expected: `ok`. (The existing `scriptedProvider` tests return whole-body JSON; `Stream`
tolerates a single non-SSE body? No — update them.) If any fail because the scripted
provider returns a non-SSE body, convert those handlers to emit the same content as a
single SSE frame followed by `[DONE]`. Example replacement for `finalResp`:
```go
func finalResp(text string) string {
	return "data: {\"choices\":[{\"delta\":{\"content\":\"" + text + "\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
}
```
and `toolCallResp` similarly emits one SSE frame with the tool_call in `delta`, then
`[DONE]`; the handler writes the string and flushes. Re-run until `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/loop.go internal/agent/loop_test.go
git commit -m "feat(agent): stream prose as token events"
```

---

## Task 4: TUI messages + event bridge

**Files:**
- Create: `internal/tui/messages.go`

- [ ] **Step 1: Create the message types and bridge**

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/agent"
)

type tokenMsg string
type assistantMsg string
type toolCallMsg struct{ name, args string }
type toolResultMsg struct{ name, text string }
type approvalMsg struct {
	tool, args string
	reply      chan bool
}
type doneMsg struct{ answer string }
type errMsg struct{ err error }

// eventToMsg converts an agent Event into the corresponding tea.Msg.
func eventToMsg(e agent.Event) tea.Msg {
	switch e.Type {
	case "token":
		return tokenMsg(e.Text)
	case "assistant":
		return assistantMsg(e.Text)
	case "tool_call":
		return toolCallMsg{name: e.Name, args: e.Text}
	case "tool_result":
		return toolResultMsg{name: e.Name, text: e.Text}
	default:
		return nil
	}
}

// waitFor reads the next message off the bridge channel. Re-issued after each
// message so the loop keeps draining agent activity.
func waitFor(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-sub }
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./internal/tui/`
Expected: builds (no other tui files yet reference these — that's fine).

- [ ] **Step 3: Commit**

```bash
git add internal/tui/messages.go
git commit -m "feat(tui): message types and agent event bridge"
```

---

## Task 5: TUI model

**Files:**
- Create: `internal/tui/model.go`

- [ ] **Step 1: Create the model**

```go
package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"gophermind/internal/agent"
)

type state int

const (
	stateIdle state = iota
	stateWorking
	stateApproval
)

type model struct {
	agent *agent.Agent
	sub   chan tea.Msg // agent events + approval requests + done/err arrive here

	model string // model name (for status line)
	mode  string // "auto" | "ask"

	input    textarea.Model
	viewport viewport.Model
	spin     spinner.Model
	render   *glamour.TermRenderer

	transcript string // accumulated rendered scrollback
	stream     string // prose buffered during the current streaming turn

	st      state
	pending approvalMsg // valid when st == stateApproval
	cancel  context.CancelFunc

	tokens int
	width  int
	height int
	ready  bool
	err    error
}

// newModel builds the model. The agent's onEvent/approve closures push onto sub.
func newModel(buildAgent func(sub chan tea.Msg) *agent.Agent, modelName, mode string) model {
	sub := make(chan tea.Msg, 64)

	ta := textarea.New()
	ta.Placeholder = "Ask gophermind to do something…"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	r, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))

	return model{
		agent:  buildAgent(sub),
		sub:    sub,
		model:  modelName,
		mode:   mode,
		input:  ta,
		spin:   sp,
		render: r,
		st:     stateIdle,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spin.Tick, waitFor(m.sub))
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./internal/tui/`
Expected: builds (View/Update not yet present → `model` has no Update/View, but it
compiles as a struct; `tea.Model` interface not yet satisfied, which is fine until Task 8).

- [ ] **Step 3: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): model state, components, init"
```

---

## Task 6: TUI update — keys, turn start, slash commands

**Files:**
- Create: `internal/tui/update.go`
- Test: `internal/tui/update_test.go`

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel() model {
	m := newModel(func(sub chan tea.Msg) *agent.Agent { return nil }, "m", "auto")
	m.width, m.height, m.ready = 80, 24, true
	return m
}

func TestSlashClearResetsTranscript(t *testing.T) {
	m := testModel()
	m.transcript = "old stuff"
	m.input.SetValue("/clear")
	m2, _ := m.handleSubmit()
	if m2.transcript != "" {
		t.Errorf("transcript not cleared: %q", m2.transcript)
	}
	if m2.st != stateIdle {
		t.Errorf("state = %v, want idle", m2.st)
	}
}

func TestSlashExitQuits(t *testing.T) {
	m := testModel()
	m.input.SetValue("/exit")
	_, cmd := m.handleSubmit()
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected tea.QuitMsg")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("got %T, want tea.QuitMsg", msg)
	}
}
```
(Needs `import "gophermind/internal/agent"` added to the test file.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestSlash -v`
Expected: FAIL — `handleSubmit` undefined.

- [ ] **Step 3: Implement Update + handleSubmit**

```go
package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(msg.Width - 2)
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tokenMsg:
		m.stream += string(msg)
		m.tokens++
		return m, waitFor(m.sub)

	case assistantMsg:
		m.transcript += "\n" + string(msg) + "\n"
		return m, waitFor(m.sub)

	case toolCallMsg:
		m.transcript += "\n● " + msg.name + "  " + oneLine(msg.args) + "\n"
		return m, waitFor(m.sub)

	case toolResultMsg:
		m.transcript += "  " + oneLine(msg.text) + "\n"
		return m, waitFor(m.sub)

	case approvalMsg:
		m.st = stateApproval
		m.pending = msg
		return m, waitFor(m.sub)

	case doneMsg:
		if s := strings.TrimSpace(m.stream); s != "" {
			if out, err := m.render.Render(s); err == nil {
				m.transcript += "\n" + out
			} else {
				m.transcript += "\n" + s + "\n"
			}
		}
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		return m, waitFor(m.sub)

	case errMsg:
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		m.transcript += "\nerror: " + msg.err.Error() + "\n"
		return m, waitFor(m.sub)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		return m, tea.Quit

	case tea.KeyEsc:
		if m.st == stateApproval {
			m.pending.reply <- false
			m.st = stateWorking
		}
		if m.cancel != nil {
			m.cancel()
		}
		return m, nil
	}

	// Approval keys take priority while awaiting a decision.
	if m.st == stateApproval {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.pending.reply <- true
			m.st = stateWorking
			return m, nil
		case "n":
			m.pending.reply <- false
			m.st = stateWorking
			return m, nil
		case "a":
			m.alwaysAllow(m.pending.tool)
			m.pending.reply <- true
			m.st = stateWorking
			return m, nil
		}
		return m, nil
	}

	if msg.Type == tea.KeyEnter && m.st == stateIdle {
		return m.handleSubmit()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleSubmit() (model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	if text == "" {
		return m, nil
	}
	switch text {
	case "/exit", "/quit":
		return m, tea.Quit
	case "/clear":
		m.transcript = ""
		m.agent.Reset()
		return m, nil
	case "/help":
		m.transcript += "\nCommands: /help /clear /exit · y/n/a to approve · Esc to interrupt\n"
		return m, nil
	}

	m.transcript += "\n› " + text + "\n"
	m.st = stateWorking

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	ag := m.agent
	go func() {
		ans, err := ag.Send(ctx, text)
		if err != nil {
			sub <- errMsg{err}
		} else {
			sub <- doneMsg{answer: ans}
		}
	}()
	return m, nil
}
```

- [ ] **Step 4: Add `alwaysAllow`, `oneLine`, and `agent.Reset`**

Add to `internal/tui/model.go` (struct field + method):
```go
// add to model struct:
//   allowed map[string]bool
```
Initialise it in `newModel` (`allowed: map[string]bool{}`) and add:
```go
func (m *model) alwaysAllow(tool string) {
	if m.allowed == nil {
		m.allowed = map[string]bool{}
	}
	m.allowed[tool] = true
}
```
Add helper to `internal/tui/update.go`:
```go
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
```
Add to `internal/agent/loop.go`:
```go
// Reset clears the conversation back to just the system prompt.
func (a *Agent) Reset() {
	a.msgs = a.msgs[:1]
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -run TestSlash -v`
Expected: PASS both.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/update.go internal/tui/update_test.go internal/tui/model.go internal/agent/loop.go
git commit -m "feat(tui): update loop — keys, turn start, approval, slash commands"
```

---

## Task 7: Approval flow test

**Files:**
- Modify: `internal/tui/update_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestApprovalKeysReply(t *testing.T) {
	m := testModel()
	reply := make(chan bool, 1)
	m.st = stateApproval
	m.pending = approvalMsg{tool: "write_file", args: "{}", reply: reply}

	m2, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	mm := m2.(model)
	select {
	case v := <-reply:
		if !v {
			t.Error("'a' should approve")
		}
	default:
		t.Fatal("no reply sent")
	}
	if !mm.allowed["write_file"] {
		t.Error("'a' should add to always-allow")
	}
	if mm.st != stateWorking {
		t.Errorf("state = %v, want working", mm.st)
	}
}
```

- [ ] **Step 2: Run to verify it fails, then passes**

Run: `go test ./internal/tui/ -run TestApprovalKeysReply -v`
Expected: PASS (logic already implemented in Task 6). If `msg.String()` for runes
doesn't yield `"a"`, adjust `handleKey` to read `string(msg.Runes)` for `KeyRunes`.
Re-run until PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/update_test.go
git commit -m "test(tui): approval key handling"
```

---

## Task 8: TUI view

**Files:**
- Create: `internal/tui/view.go`

- [ ] **Step 1: Implement View (satisfies tea.Model)**

```go
package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	statusStyle = lipgloss.NewStyle().Faint(true)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}

	body := m.transcript
	if m.st == stateWorking && m.stream != "" {
		body += "\n" + m.stream
	}

	var status string
	switch m.st {
	case stateWorking:
		status = fmt.Sprintf("%s %s · %s mode · %d tokens · %s working", m.spin.View(), m.model, m.mode, m.tokens, m.model)
	case stateApproval:
		status = fmt.Sprintf("approve %s %s ? (y)es (n)o (a)lways", m.pending.tool, oneLine(m.pending.args))
	default:
		status = fmt.Sprintf("%s · %s mode · ready", m.model, m.mode)
	}

	return body + "\n" +
		boxStyle.Width(m.width-2).Render(m.input.View()) + "\n" +
		statusStyle.Render(status)
}
```

- [ ] **Step 2: Verify the package builds and `model` satisfies `tea.Model`**

Add a compile-time assertion at the bottom of `view.go`:
```go
var _ tea.Model = model{}
```
Add the import `tea "github.com/charmbracelet/bubbletea"`.
Run: `go build ./internal/tui/`
Expected: builds.

- [ ] **Step 3: Run all tui tests**

Run: `go test ./internal/tui/`
Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/view.go
git commit -m "feat(tui): view — transcript, input box, status line"
```

---

## Task 9: Run entrypoint + main wiring

**Files:**
- Create: `internal/tui/run.go`
- Modify: `cmd/gophermind/main.go`

- [ ] **Step 1: Create the Run entrypoint**

```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/agent"
	"gophermind/internal/llm"
	"gophermind/internal/safety"
	"gophermind/internal/tools"
)

// Config carries everything Run needs from the caller.
type Config struct {
	Client    *llm.Client
	Registry  *tools.Registry
	Model     string
	Mode      string // "auto" | "ask"
	MaxIter   int
}

// Run starts the interactive TUI and blocks until the user quits.
func Run(cfg Config) error {
	build := func(sub chan tea.Msg) *agent.Agent {
		// holder lets the approval closure see the model's always-allow set
		var allowed func(string) bool
		approve := func(tool, args string) bool {
			if cfg.Mode == "auto" || (allowed != nil && allowed(tool)) {
				return true
			}
			reply := make(chan bool, 1)
			sub <- approvalMsg{tool: tool, args: args, reply: reply}
			return <-reply
		}
		onEvent := func(e agent.Event) {
			if msg := eventToMsg(e); msg != nil {
				sub <- msg
			}
		}
		ag := agent.New(cfg.Client, cfg.Registry, cfg.MaxIter, approve, onEvent)
		_ = allowed // set below via model wiring
		return ag
	}

	m := newModel(build, cfg.Model, cfg.Mode)
	// Wire the approval closure's always-allow lookup to the model's set.
	_ = time.Second
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
```

Note: the `allowed` lookup wiring is finalised in Step 2 by storing the program’s
model set on the `model` and reading it in `approve`. Implement by adding to `model`:
`allowedLookup` is unnecessary — instead, the `a` key already mutates `m.allowed`, and
`approve` checks `cfg.Mode`. For the always-allow fast-path across turns, have
`approve` consult a shared `map[string]bool` created in `Run` and also mutated by the
`a` branch. Replace the `m.alwaysAllow` body to write into that shared map by passing
it into `newModel`. Concretely:

- Add `allow map[string]bool` param to `newModel` and store on `model.allowed`.
- In `Run`, create `allow := map[string]bool{}`; pass to both the `approve` closure
  (`allowed = func(t string) bool { return allow[t] }`) and `newModel(build, ..., allow)`.

- [ ] **Step 2: Finalise the shared always-allow map**

Update `newModel` signature to `newModel(buildAgent func(chan tea.Msg) *agent.Agent, modelName, mode string, allow map[string]bool) model` and set `allowed: allow`. Update `Run` to create the map and pass it both places. Update the Task-5/6 call sites and `testModel()` (pass `map[string]bool{}`).

- [ ] **Step 3: Wire main.go**

In `cmd/gophermind/main.go`, replace the `case "chat":` body with:
```go
	case "chat":
		if !isatty() {
			return fmt.Errorf("interactive session needs a terminal; use `run`/`ask` for non-interactive use")
		}
		return tui.Run(tui.Config{
			Client: client, Registry: reg, Model: cfg.Model, Mode: cfg.ApprovalMode, MaxIter: cfg.MaxIter,
		})
```
Remove the old `repl(...)` function and its `ui.Printer{Verbose: true}` usage. Add the
TTY check helper:
```go
func isatty() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}
```
Add `"gophermind/internal/tui"` to imports; drop the now-unused `bufio`/`ui` imports if
no longer referenced (the one-shot `run`/`ask` still use `ui.Printer` and the shared
`stdin` reader for `ui.Confirm`, so keep those).

- [ ] **Step 4: Build everything**

Run: `go build ./...`
Expected: builds. Fix any unused-import errors as directed by the compiler.

- [ ] **Step 5: Run the full test suite**

Run: `gofmt -l cmd internal && go vet ./... && go test ./...`
Expected: gofmt clean, vet clean, all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/run.go cmd/gophermind/main.go internal/tui/model.go internal/tui/update_test.go
git commit -m "feat(tui): Run entrypoint and interactive wiring in main"
```

---

## Task 10: Manual end-to-end verification (requires VPN to the endpoint)

**Files:** none (verification only)

- [ ] **Step 1: Build the binary**

Run: `go build -o gophermind ./cmd/gophermind`
Expected: binary produced.

- [ ] **Step 2: Launch the interactive session**

Run: `./gophermind` (with VPN up so your configured endpoint is reachable)
Expected: banner + input box; model auto-discovered.

- [ ] **Step 3: Exercise a real coding turn**

Type: `add a function hello() that returns "hi" to a new file hello.go, then run go test ./...`
Verify: streamed markdown answer; `● write_file` / `● run_shell` blocks appear; an
`approve … (y/n/a)` prompt shows in `ask` mode and `y` proceeds; tests run.

- [ ] **Step 4: Verify interrupt and slash commands**

- Start a turn, press `Esc` → returns to prompt with the turn cancelled.
- `/clear` empties the transcript; `/help` lists commands; `/exit` quits cleanly,
  terminal restored (cursor visible, cooked mode).

- [ ] **Step 5: Confirm streaming tool-calls against llama.cpp (Risk #1)**

If tool calls are malformed under streaming, implement the documented fallback: in
`agent.Send`, when a turn is expected to call tools, use `Complete` instead of
`Stream` (stream only the final prose turn). Add a follow-up commit if needed.

---

## Self-Review

**Spec coverage:** streaming (Task 2–3) ✓ · tool-call blocks (Task 6 toolCallMsg) ✓ ·
inline approvals y/n/a + auto (Task 6–7, Run) ✓ · input box + status line (Task 5,8) ✓ ·
interrupt Esc (Task 6) ✓ · slash commands (Task 6) ✓ · markdown finalize-render
(Task 6 doneMsg) ✓ · not-a-TTY guard (Task 9) ✓ · one-shot stays plain (Task 9 keeps
`ui`) ✓ · headless tests still pass (Task 3 Step 5, Task 9 Step 5) ✓ · risks validated
(Task 10) ✓.

**Placeholder scan:** Task 9's always-allow wiring is described in prose because it
threads a shared map through three call sites; Steps 1–2 give the exact signature
change and both assignment sites, so it is actionable, not a placeholder.

**Type consistency:** `model`, `state`, `approvalMsg{tool,args,reply}`, `eventToMsg`,
`waitFor`, `handleSubmit`, `handleKey`, `alwaysAllow`/`allowed`, `agent.Reset`,
`llm.Stream` names are used identically across tasks.
