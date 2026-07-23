<div align="center">

```
┌───────────────────────────────────────────────┐
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░      ░░░░░░░░░░░░      ░░░░░░░░ │
│ ░░░░░░░░░░░░  .--.              .--.  ░░░░░░░ │
│ ░░░░░░░░░░░░ /    \____________/    \ ░░░░░░░ │
│ ░░░░░░░░░░░░ \                      /  ░░░░░░ │
│ ░░░░░░░░░░░░  |   .------..------.   |  ░░░░░ │
│ ░░░░░░░░░░░░░ |  /  .--.  \/  .--. \  | ░░░░░ │
│ ░░░░░░░░░░░░░ |  | ( o ) || ( o ) |  |  ░░░░░ │
│ ░░░░░░░░░░░░░ |  \  '--' /\  '--' /   | ░░░░░ │
│ ░░░░░░░░░░░░░ |   '------''------'   |  ░░░░░ │
│ ░░░░░░░░░░░░░ |         .--.         | ░░░░░░ │
│ ░░░░░░░░░░░░░ |        / .. \        | ░░░░░░ │
│ ░░░░░░░░░░░░░ |        \    /        | ░░░░░░ │
│ ░░░░░░░░░░░░░  \        |  |        /  ░░░░░░ │
│ ░░░░░░░░░░░░░░  \      _|  |_      /  ░░░░░░░ │
│ ░░░░░░░░░░░░░░░  |    | |  | |    |  ░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░ |    | |__| |    | ░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░  |     '----'     |  ░░░░░░░░ │
│ ░░░░░░░░░░░░░░  /                  \  ░░░░░░░ │
│ ░░░░░░░░░░░░░░ |    ___      ___    | ░░░░░░░ │
│ ░░░░░░░░░░░░░░ |   /   |    |   \   | ░░░░░░░ │
│ ░░░░░░░░░░░░░░  \ (    |    |    ) /  ░░░░░░░ │
│ ░░░░░░░░░░░░░░░  \ \   |    |   / /  ░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░  \_\  |    |  /_/  ░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░    | |    | |    ░░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░░░  | |    | |  ░░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░░  /   \  /   \  ░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░░ (_____)(_____) ░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░░                ░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│              G O P H E R M I N D              │
└───────────────────────────────────────────────┘
```

**A tiny, hackable AI coding agent for your terminal — pointed at *your* LLM.**

[![Go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform: macOS · Linux · Windows](https://img.shields.io/badge/platform-macOS%20·%20Linux%20·%20Windows-lightgrey)](#install)
[![Release](https://img.shields.io/github/v/release/jbrahy/gophermind.com?sort=semver)](https://github.com/jbrahy/gophermind.com/releases)
[![PRs welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

</div>

---

GopherMind is a **single Go binary** that turns any **OpenAI-compatible** model
into an agentic coding assistant that reads, searches, edits, and runs commands
in your repository — all from a clean terminal UI. It's built to run against a
**model you control** (local `llama.cpp`, Ollama, LM Studio, vLLM, or a hosted
endpoint), with **safety built in at every layer**.

No cloud lock-in. No 200-file framework. Just a small, readable codebase you can
actually understand and extend in an afternoon.

## Why GopherMind?

- 🧠 **Bring your own model.** Anything that speaks the OpenAI `/v1` API — your
  local GPU, a private endpoint over VPN, or OpenAI itself. Models are
  auto-discovered; switch backends with named **provider profiles**.
- 🔒 **Safe by default.** Every file path is contained to your repo
  (symlink-aware), shell commands run through a deny-list, and mutating actions
  hit an **approval gate** unless you opt into auto mode.
- ⚡ **Fast, focused loop.** Read / search / edit / shell tools drive a compact
  agent loop with streaming output, a live **token + cost meter**, retries with
  backoff, and an optional response cache.
- 🖥️ **A terminal UI that's actually pleasant.** Built on [Charm](https://charm.sh)
  — scrollback, syntax-aware rendering, inline approvals, and a gopher that
  greets you with a fortune.
- 🪶 **Small and hackable.** Pure Go, no CGO. Adding a new tool is still one
  struct and one function — the codebase stays readable even as it grows.
- 🧰 **Batteries included (opt-in).** A local **semantic index** & RAG, a
  read-only **SQL**/Parquet/CSV data toolkit, multi-agent strategies
  (`--debate`, `--samples`, `--reflexion`), an **MCP server** so any MCP client
  can drive it, a plugin SDK + **WASM** sandbox, and observability (Prometheus
  `/metrics`, tracing, cost dashboard). Every integration is inert until you
  configure it.

## Install

```sh
brew install jbrahy/tap/gophermind     # macOS (signed + notarized, no Gatekeeper warnings)
npm install -g gophermind              # macOS / Linux / Windows, x64 / arm64
```

The Homebrew build is a signed + notarized universal macOS binary; the npm
package downloads the prebuilt binary for your platform. **Linux** users can
also grab a `.deb`/`.rpm`/`.apk` (or a `.tar.gz`) straight from the
[latest release](https://github.com/jbrahy/gophermind.com/releases/latest);
**Windows** users a `.zip`. Every release ships an SBOM and `checksums.txt`.

Or build from source (Go 1.25+):

```sh
git clone https://github.com/jbrahy/gophermind.com
cd gophermind.com
make build      # -> ./gophermind
```

## Quickstart

```sh
gophermind            # first run walks you through a setup wizard, then chats
```

The wizard asks for your endpoint, an optional API key, a model (picked from a
live list), your approval mode, and a max-iteration budget — then saves it so
later launches go straight to the prompt. Re-run it anytime with
`gophermind config`.

One-shot, non-interactive use:

```sh
gophermind run "add a --json flag to the export command and a test for it"
gophermind ask "how does the retry backoff work?"   # read-only, never edits
```

## TUI Features

### Predictive Text & Autocomplete

As you type in the interactive TUI input box, suggestions appear in two forms:

- **Inline ghost text** — a single obvious continuation appears as faded text at the cursor: accept it with **Tab** or **→** (right arrow when the cursor is at the end of the line). Sources include history recall (prompts you've submitted before) and a Markov next-word predictor trained on your history.
- **Popup menu** — when there are multiple candidates, a popup menu appears above the input: navigate with **↑** and **↓**, accept with **Tab** (or **Enter** when the menu is open), dismiss with **Esc**. Sources include slash-command completion (when the line starts with `/`) and file/path completion for path-shaped tokens.

The suggestion engine is built on the reusable [`github.com/jbrahy/bubblecomplete`](https://github.com/jbrahy/bubblecomplete) library (vendored in-repo).

### Prompt History

Submitted prompts are saved to `<os user config dir>/gophermind/history` for recall and training (e.g., `~/Library/Application Support/gophermind/history` on macOS; falls back to `.gophermind/history` if the system config directory is unavailable).

**Privacy note:** Prompts are stored in plain text (one JSON-encoded string per line, JSONL format). Disable history persistence with `GOPHERMIND_HISTORY=off`. The history is capped at the most recent 500 entries; oldest entries are dropped first.

### Multi-line Input

The input box grows from 1 up to 4 rows, then scrolls. **Enter** submits; **Shift+Enter** inserts a literal newline. If your terminal does not distinguish Shift+Enter from plain Enter, use **Ctrl+J** instead.

**Alt+Enter** is not bound by default. It arrives as `ESC`+`CR`, which is byte-identical to what some keyboard remaps and terminal key mappings emit for ordinary keys — so binding it caused stray newlines when an unrelated key was pressed. Set `GOPHERMIND_ALT_ENTER_NEWLINE=1` to restore it.

### `/goal` — Session Steering Goal

Use `/goal <text>` to set a persistent goal that is injected into every subsequent turn, steering the agent's behavior without modifying the prompt each time. Bare `/goal` displays the current goal; `/goal clear` removes it. The goal is session-scoped and works with any backend.

### Text Selection

The TUI no longer captures the mouse, so you can select and copy text using your terminal's native selection (click-drag). Keyboard navigation of the transcript is unchanged: **PgUp** and **PgDn** scroll the message history.

## How it works

```
you ──▶ TUI ──▶ agent loop ──▶ OpenAI-compatible model
                    │  ▲
                    ▼  │ tool calls / results
             tools (read · search · edit · write · shell)
                    │
             safety: path containment · shell deny-list · approval gate
```

The model requests tools by name; the harness runs them against your repo
(inside the sandbox), feeds the results back, and repeats until it produces an
answer or hits the iteration budget. That's the whole idea — see
[`internal/agent`](internal/agent) and [`internal/tools`](internal/tools).

## PhaseFlow: spec-driven workflow

GopherMind natively speaks [PhaseFlow](https://github.com/jbrahy/metaphaseflow),
a spec-driven development loop: **Roadmap → Phases → Plan → Execute → Verify →
Milestone**. Workflow state lives under `.planning/` (`ROADMAP.md`, `STATE.md`,
`PROJECT.md`, `config.json`) — the same on-disk model as upstream, so the two
tools are interchangeable.

```
gophermind phase init "My Project"   # scaffold .planning/
gophermind phase roadmap             # draft the roadmap (agent)
gophermind phase status              # progress + current phase (local)
gophermind phase plan 1              # plan a phase (agent)
gophermind phase execute 1           # execute its plans (agent)
gophermind phase done 01-01          # mark a plan done, sync STATE.md (local)
gophermind phase verify 1            # verify success criteria (agent)
gophermind phase archive v1.0 MVP    # snapshot a shipped milestone (local)
```

The same commands are available in the TUI as `/phase <cmd>`. Loop steps
(`roadmap`/`plan`/`execute`/`verify`/`milestone`, and any embedded PhaseFlow
command by name) run gophermind's agent seeded with the current project state.
The bookkeeping commands (`status`, `next`, `done`, `sync`, `archive`) are pure
Go — they update `.planning/` deterministically with no model calls, so
progress can never drift from the roadmap's checkboxes. See
[`internal/phaseflow`](internal/phaseflow).

### Autonomous execution: `/project-execute`

Once a project plan is approved (via `/project <name>` → approve), you can run:

```sh
gophermind project-execute          # TUI: `/project-execute`
```

This autonomously executes every `pending` task in the approved `.planning/assignments.json` — **in plan-id order, all phases, each task in a fresh isolated agent**. Each task agent:
- Runs with its assigned **model** and **catalog prompt** (seeded from the project's per-type agent catalog)
- Verifies its output against the task's acceptance criteria (one verify-and-correct round)
- Updates its status (`pending` → `done` on success, or `failed` with details)

Failed tasks are marked `failed`, the executor continues to the next task, and a summary is printed at the end. Task agents run in **auto-approval mode** (unattended — no per-task prompts) for safety; this is a deliberate gating mechanism.

**Abort handling:** Pressing Ctrl-C stops the executor; in-flight tasks cleanly revert to `pending` so they can be re-run. Like `/phase execute`, this command requires an approved plan.

## Configuration

Everything is optional and layered: **flags > real env > `./.env` > global
config > defaults**. Copy [`.env.example`](.env.example) to `.env` for a fully
documented list, or just run the wizard. Highlights:

| Setting | What it does |
|---|---|
| `GOPHERMIND_BASE_URL` | Your OpenAI-compatible endpoint (required) |
| `GOPHERMIND_MODEL` | Model name (empty = auto-discover) |
| `GOPHERMIND_APPROVAL` | `ask` (default) or `auto` |
| `GOPHERMIND_PROFILE` | Named backend: `local-llama`, `openai`, … |

Secure options for internal endpoints (mTLS, custom CA), a response cache,
sampling controls, and JSONL transcript export are all supported — see
[`.env.example`](.env.example).

Beyond `chat`/`run`/`ask`, the CLI exposes subcommands for sessions, prompts,
plugins, config bundles, the MCP server, benchmarks, diagnostics, and more —
run `gophermind --help` (and `gophermind completion <shell>`) for the full list.

## Remote control from iOS / mobile

`gophermind serve` also exposes a session-based, multi-turn HTTP+SSE surface
so a phone can drive an agent running on your machine: create a session,
stream a turn's tokens/tool calls/usage live, and — with
`GOPHERMIND_SERVE_APPROVAL=remote` — approve or deny gated tool calls
on-phone (optionally with a push notification via APNs) instead of at the
machine's terminal.

See [`docs/mobile-serve.md`](docs/mobile-serve.md) for the full protocol
reference (run instructions, env vars, connectivity options, and the typed
SSE event schema). The native iOS app that implements this contract will
live in `ios/` (built next).

## Contributing

**We'd love your help.** GopherMind ships with a large idea backlog across four
batches ([`todo.md`](todo.md) → [`todo-4.md`](todo-4.md)) — the batch-4 set
landed in 0.2.0 (MCP server, embeddings, WASM sandbox, packaging, and more), so
the remaining tail is a good source of scoped work. Good first areas: adding a
tool, improving search, a `scoop-bucket`/`winget` publish path, or wiring up an
idea from the backlog.

Start with [CONTRIBUTING.md](CONTRIBUTING.md). The codebase is test-driven and
small enough to hold in your head.

## License & credits

GopherMind is [MIT](LICENSE) licensed. The startup fortunes come from
[Brian M. Clapper's fortune database](https://github.com/bmc/fortunes) under
CC BY 4.0 — see [CREDITS.md](CREDITS.md).
