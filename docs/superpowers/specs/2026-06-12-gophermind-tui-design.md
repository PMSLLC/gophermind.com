# GopherMind TUI — Design Spec

*Date: 2026-06-12 · Status: approved, ready for implementation plan*

## Context

GopherMind is a Go agentic coding harness. Its engine already works: a true
agentic tool loop (`internal/agent`) talking to an OpenAI-compatible endpoint
(llama.cpp at a user-configured endpoint, model auto-discovered) with file /
search / shell tools, safety guards, and a basic line-based REPL.

The goal of this work is to replace the basic REPL with a polished, conversational
**terminal UI that emulates the Claude Code experience** — the user's favorite
interface. Claude Code is not a terminal emulator or a plugin; it is a TUI that
runs *inside* any terminal (iTerm2 included). So we build a Go TUI on top of the
existing engine. iTerm2-specific niceties (marks, inline diffs, colors via escape
codes) are explicitly deferred polish, not the foundation.

## Goals / non-goals

**Goals:** a Claude-Code-like interactive session — streaming markdown answers,
tool calls as distinct blocks, inline permission prompts, a persistent input box,
a status line, interruptible turns.

**Non-goals (v1):** session save/restore, command-history search, `@file`
mentions, mouse, themes/config files, inline image diffs, multiple panes, the
iTerm2 escape-code polish layer, multi-provider support.

## Foundation

**Bubble Tea + Lipgloss + Glamour + Bubbles** (the Charm stack), inline
(non-fullscreen) rendering for a scrollback-friendly feel. This adds the first
external dependencies to the project; the engine packages stay stdlib-only. The
"simple and beautiful" value moves from "zero deps" to "small, focused code using
the right tool."

## Experience & v1 scope

A scrollback-style session that looks roughly like:

```
  gophermind · Qwen3.6-35B · ~/myrepo

  › refactor the auth handler to return errors instead of panicking

  ● read_file  internal/auth/handler.go
  ● search  "panic("        3 matches in 2 files
  ● edit_file  internal/auth/handler.go
    Approve edit? (y)es / (n)o / (a)lways  ▸ y
    ✓ edited internal/auth/handler.go
  ● run_shell  go test ./...        ok  0.2s

  I replaced the three panics with wrapped errors… (streaming markdown)

  ┌──────────────────────────────────────────────┐
  │ ›                                            │
  └──────────────────────────────────────────────┘
  qwen3.6-35b · ask mode · 12.4k tokens · ⏱ working…
```

v1 feature set:
- **Streaming responses** rendered as **markdown** (Glamour).
- **Tool calls as distinct blocks** (`● tool_name args`) with concise result summaries.
- **Inline permission prompts** for mutating tools — `y` / `n` / `a` (always-allow
  that tool this session); plus an `auto` mode that skips prompts.
- **Persistent multi-line input box**; **status line** (model · mode · tokens · spinner).
- **Interrupt**: `Esc` cancels the current turn and returns to the prompt;
  `Ctrl-C` / `Ctrl-D` / `/exit` quits.
- **Slash commands**: `/help`, `/clear` (reset conversation), `/exit`.

## Architecture

The agent engine stays headless. The TUI is a thin layer. The key design move:
**the agent runs in its own goroutine**, so its existing blocking API (including
the `ApprovalFunc`) keeps working and the UI thread never blocks.

### Package layout

```
cmd/gophermind/main.go     wiring: interactive → launch TUI; run/ask → plain one-shot
internal/
  llm/        + streaming (SSE) alongside Complete           [no UI knowledge]
  tools/      unchanged                                       [no UI knowledge]
  safety/     unchanged
  agent/      loop emits events; approval stays a func        [no terminal knowledge]
  tui/        NEW — Bubble Tea Model/Update/View              [no business logic]
  ui/         removed (replaced by tui); plain printer kept for one-shot
```

Boundaries: `agent` is testable headless (current tests keep passing); `tui` only
renders and routes keypresses. Each side changes without breaking the other.

### Data flow (Elm architecture)

```
        keypress ──► tui.Update ──► starts turn (tea.Cmd)
                                         │
                                         ▼
                          agent.Send runs in a goroutine
                                         │ emits Events on a channel
            ┌────────────────────────────┼───────────────────────────┐
            ▼                            ▼                            ▼
     assistant-token            tool-call / tool-result        approval-request
            │                            │                            │
            └──────────── each becomes a tea.Msg ─────────────────────┘
                                         ▼
                          tui.Update mutates Model ──► View re-renders
```

- A standard Bubble Tea "listen on channel" `tea.Cmd` reads one agent Event and
  returns it as a `tea.Msg`, then re-arms itself — streamed tokens and tool events
  flow into the UI naturally.
- **Approval**: the agent's `ApprovalFunc` implementation sends an
  `approval-request` Event and blocks on a reply channel; the TUI shows the
  `y/n/a` prompt; the keypress sends the decision back; the agent goroutine
  resumes. UI thread stays responsive.
- **Interrupt**: `Esc` cancels the turn's `context` (same per-turn cancel the REPL
  already uses).

### Engine refactors

1. **`llm` gains streaming** — `Stream(ctx, msgs, tools, onToken)` parses SSE
   deltas, calls `onToken` for prose, and reassembles fragmented `tool_call`
   argument deltas into the final assistant message. `agent.Send` forwards tokens
   as `assistant-token` events. `Complete` stays for one-shot/tests.
2. **`agent` event surface** — extend the existing `Event` type
   (`token`, `tool_call`, `tool_result`, `approval`, `done`, `error`) and route to
   the channel. The loop logic barely changes.

### One-shot stays plain

`run "task"` / `ask "q"` keep a simple line printer (no TUI) so they work in pipes
and CI. The TUI is interactive-only.

## Error handling

- **Not a TTY**: interactive mode detects no terminal and prints a hint to use
  `run`/`ask`; never launches a broken TUI.
- **LLM/transport/streaming errors mid-turn**: render an error block, return to the
  prompt; the session survives.
- **Tool errors**: unchanged — fed back to the model as text for self-correction.
- **TUI safety**: recover from render panics and restore the terminal (cooked mode,
  cursor) via deferred cleanup.
- **Resize**: handle `tea.WindowSizeMsg`, reflow the viewport.

## Testing

- Engine stays headless-testable; all current `llm`/`agent`/`tools` tests keep
  passing unchanged.
- **Streaming**: `httptest` server emitting `data:` SSE chunks; assert `onToken`
  fires per delta and the reassembled message/tool-calls are correct.
- **TUI**: `Model`/`Update` are pure — unit-test by feeding `tea.Msg`s and asserting
  `Model` state. Optionally golden-output via `charmbracelet/x/exp/teatest`.

## Risks (validate in the first implementation step against the live endpoint)

1. **llama.cpp streaming tool-calls** may differ from OpenAI's delta format.
   *Mitigation*: stream tokens for prose display, but rely on the final assembled
   message for `tool_calls`; if streamed tool-calls are unreliable, use a
   non-streamed call for tool-turns (streaming matters most for the final prose).
2. **Streaming markdown flicker/cost**: re-running Glamour per token is expensive.
   *Decision*: render raw text while streaming, swap to Glamour-rendered markdown
   when the message finalizes.
3. **Qwen tool-calling reliability** (uncensored community model): may need prompt
   nudges; verify early.

## Dependencies

`charmbracelet/bubbletea`, `lipgloss`, `glamour`, `bubbles` — pinned. Engine
packages remain stdlib-only.
