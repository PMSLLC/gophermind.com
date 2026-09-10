<div align="center">

```
          __                          __
        _/  \________________________/  \_
       /                                   \
      |    .------.            .------.     |
      |   /   __   \          /   __   \    |
      |  |  /(o )\  |________|  /( o)\  |    |
      |  |  \ '' /  |  .--.  |  \ '' /  |    |
      |   \  '--'  / (    )  \  '--'  /     |
      |    '------'   \ __ /   '------'     |
      |             .---. .---.             |
      |             |   | |   |             |
      |             |   | |   |             |
      |             '---' '---'             |
       \                                   /
        \._                             _./
         \ '""--..____________..--"'   /
          '-.._____________________..-'

              G O P H E R M I N D
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

## Run it for free

gophermind vendors a registry of free LLM API providers, so you can try it with
no signup and no key at all:

```sh
gophermind --profile free-ovhcloud ask "hello"
```

That runs against [OVHcloud AI Endpoints](https://endpoints.ai.cloud.ovh.net),
served anonymously (2 requests/minute per IP) — no config file, no
environment variable, nothing to sign up for. `free-kilocode` is the other
no-key profile. `gophermind free list` shows every provider gophermind knows
about, no-key ones first; `gophermind free show <profile>` prints a
provider's models, rate limits, and free-tier terms; `gophermind free usage`
shows a lifetime odometer of the free tokens and requests you've used.

A provider whose endpoint can't be known statically (for example Cloudflare
Workers AI, which embeds your account ID) needs a hand-supplied `/v1` base
URL. Point gophermind at it with `GOPHERMIND_BASE_URL`, and if that URL
already ends in a version segment like `/v1`, set `GOPHERMIND_CHAT_PATH` and
`GOPHERMIND_MODELS_PATH` to `/chat/completions` and `/models` so the client
doesn't double the segment into `/v1/v1/chat/completions`.

See [docs/free-providers.md](docs/free-providers.md) for the full table.

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

The suggestion engine is built on `bubblecomplete`, a reusable library carried in this repo at [`bubblecomplete/`](bubblecomplete/) and wired in through a `replace` directive in `go.mod`.

### Prompt History

Submitted prompts are saved to `~/.gophermind/history` for recall and training (honors `GOPHERMIND_CONFIG_DIR`; falls back to `.gophermind/history` if the home directory is unavailable).

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

GopherMind natively speaks PhaseFlow,
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

Everything is optional and layered: **flags > real env > `./.env` >
`~/.gophermind/config.json` > defaults**. Copy [`.env.example`](.env.example) to
`.env` for a fully documented list, or just run the wizard. Highlights:

| Setting | What it does |
|---|---|
| `GOPHERMIND_BASE_URL` | Your OpenAI-compatible endpoint (required) |
| `GOPHERMIND_MODEL` | Model name (empty = auto-discover) |
| `GOPHERMIND_APPROVAL` | `ask` (default) or `auto` |
| `GOPHERMIND_PROFILE` | Named backend: `local-llama`, `openai`, … |
| `GOPHERMIND_ATTENTION_FLASHES` | Screen flashes when the TUI needs you (default 4; 0 off) |
| `GOPHERMIND_CHAT_PATH` | Path appended to `GOPHERMIND_BASE_URL` for chat completions (default `/v1/chat/completions`) |
| `GOPHERMIND_MODELS_PATH` | Path appended to `GOPHERMIND_BASE_URL` for model listing (default `/v1/models`) |

### The global config file

`gophermind config` (and `/config` in the TUI) writes `~/.gophermind/config.json`,
which is also the directory holding sessions, prompt history, and device tokens.
It is a flat JSON object meant to be edited by hand — a lowercase key is the
`GOPHERMIND_` variable without its prefix, and an ALL-CAPS key is used verbatim:

```json
{
  "base_url": "http://192.168.5.2:8080/v1",
  "model": "qwen2.5-coder-32b",
  "approval": "auto",
  "max_iter": 25,
  "attention_flashes": 4,
  "fallback_models": ["qwen2.5-coder-14b"],
  "GITHUB_TOKEN": "…"
}
```

Real environment variables always win over the file, and saving merges rather
than rewrites, so hand-added keys survive re-running the wizard. Set
`GOPHERMIND_CONFIG_DIR` to put the directory somewhere else. Upgrading from a
release that used `<os user config dir>/gophermind/.env` migrates automatically
on first run — the `.env` becomes `config.json` and the sibling state files move
with it.

Secure options for internal endpoints (mTLS, custom CA), a response cache,
sampling controls, and JSONL transcript export are all supported — see
[`.env.example`](.env.example).

Beyond `chat`/`run`/`ask`, the CLI exposes subcommands for sessions, prompts,
plugins, config bundles, the MCP server, benchmarks, diagnostics, and more —
run `gophermind --help` for the full list.

### Connecting to MCP servers

GopherMind both *serves* MCP (`gophermind mcp`) and *consumes* it. Third-party
MCP servers are declared in an `mcpServers` block in `~/.gophermind/config.json`,
and their tools join the registry alongside the builtins:

```json
{
  "mcpServers": {
    "pelagosnow": {
      "transport": "http",
      "url": "https://mcp.pelagosnow.com/mcp",
      "headers": { "Authorization": "Bearer ${PELAGOSNOW_TOKEN}" }
    },
    "filesystem": {
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/Users/you/code"],
      "env": { "LOG_LEVEL": "warn" },
      "disabled": false
    }
  }
}
```

Project-scoped servers go in `.gophermind/<name>.mcp.json` — one server per
file, the same fields, and an entry there overrides a global one of the same
name. This is the form to commit alongside a repo:

```json
{
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres", "${DATABASE_URL}"]
}
```

A few things worth knowing:

- **Tools are namespaced** `server__tool`, so a remote server can never shadow
  a builtin. The filesystem server above contributes `filesystem__read_file`.
- **Every string supports `${VAR}`**, expanded from the environment. An unset
  variable is a startup error rather than a blank value, so tokens stay out of
  the file and a missing one is reported where it happens. This keeps
  `.gophermind/*.mcp.json` safe to commit.
- **MCP tools always require approval.** They come from servers you do not
  control, so they fail closed. Relax individual ones in `.gophermind/policy`:
  `"gated_tools": {"pelagosnow__search": "always"}`.
- **A server that is down is a warning, not a failure** — the session starts
  without it. A malformed config *is* fatal, so typos surface immediately.

### Shell completion

`gophermind autocomplete` detects your shell from `$SHELL` and prints a
completion script (pass `bash`, `zsh`, or `fish` to override). Rather than
appending it to your rc file — which duplicates the block every time you
re-run it — write it to its own file and source that, so regenerating after an
upgrade is a single command and your rc file gains exactly one line:

```sh
mkdir -p ~/.gophermind
gophermind autocomplete > ~/.gophermind/completion.sh
echo 'source ~/.gophermind/completion.sh' >> ~/.zshrc   # or ~/.bashrc
```

Appending straight to an rc file works too — the zsh script guards its
`compdef` call so it is safe to source before `compinit` has run.

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
