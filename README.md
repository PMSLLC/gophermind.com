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
      |                |==|                 |
       \               '--'                /
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
