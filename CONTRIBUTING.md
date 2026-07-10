# Contributing to GopherMind

Thanks for your interest — GopherMind is meant to be small and hackable, so
jumping in is easy. This guide gets you productive in a few minutes.

## Prerequisites

- **Go 1.24+**
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) recommended — the
  `search` tool uses it (falls back to `grep`)
- An **OpenAI-compatible endpoint** to test against (local `llama.cpp`, Ollama,
  LM Studio, vLLM, …)

## Build & test

```sh
make build     # compile ./gophermind
make test      # go test ./...
make vet       # go vet ./...
```

Or the raw commands: `go build ./...`, `go test ./...`.

## Test-driven, please

This codebase is written test-first, and we'd like to keep it that way:

1. Write a failing test that captures the behavior you want.
2. Watch it fail for the right reason.
3. Write the minimal code to make it pass.

New logic should come with tests. Keep them focused — one behavior each. Bug
fix? Add a test that reproduces the bug first.

## Project layout

| Package | Responsibility |
|---|---|
| `cmd/gophermind` | CLI entry point, flag/subcommand wiring |
| `internal/agent` | the tool-calling agent loop |
| `internal/llm` | OpenAI-compatible client (streaming, retry, cache, TLS) |
| `internal/tools` | the built-in tools (read/write/edit/list/search/shell) |
| `internal/safety` | path containment, shell deny-list, approval gate |
| `internal/config` | env/flag/`.env` config + the setup wizard's persistence |
| `internal/setup` | the first-run configuration wizard |
| `internal/tui` | the Bubble Tea terminal UI |
| `internal/banner`, `internal/fortune`, `internal/version` | startup splash |

## Adding a tool

A tool is just a `tools.Tool{Name, Description, Schema, Run}`. Implement it in
`internal/tools`, register it in `cmd/gophermind/main.go`, and add tests. The
model discovers it automatically from its name + description + JSON schema — so
write those carefully; they *are* the interface.

## Pull requests

- Branch from `main`; keep PRs focused (one logical change).
- Make sure `make test` and `make vet` pass.
- Follow the surrounding style; match existing patterns rather than introducing
  new ones.
- Reference the backlog item (from [`todo.md`](todo.md) / [`todo-2.md`](todo-2.md))
  if your change implements one.
- Describe what changed and how you verified it.

## Where to start

The two backlog files list ~200 concrete ideas with the existing code they build
on. Great starters: a new file/search tool option, an MCP client, richer diff
rendering in the TUI, or a `doctor` command. Open an issue to discuss anything
larger before diving in.

## Filing issues

Include: what you ran, what you expected, what happened, your OS, and your
endpoint/model (e.g. "llama.cpp, Qwen"). A minimal repro helps a lot.

All contributors are expected to follow our
[Code of Conduct](CODE_OF_CONDUCT.md). Every PR is checked by CI (gofmt, `go
vet`, build, and `go test -race`), so run those locally before pushing.

By contributing, you agree your contributions are licensed under the project's
[MIT License](LICENSE).
