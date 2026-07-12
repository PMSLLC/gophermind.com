# PROJECT.md — gophermind

Project-specific overrides and conventions for this repo. Committed; read at
session start alongside CONTEXT.md.

## What this is

`gophermind` is a Go coding-agent CLI (a Claude-Code-style tool) that talks to
any OpenAI-compatible model endpoint. It has its own agent loop, tool set, TUI,
sessions, personas, prompt registry, and — as of the PhaseFlow work — a native
spec-driven workflow (`internal/phaseflow`).

- Module: `gophermind` (see `go.mod`, `go 1.25.0`; local toolchain go1.26.4)
- Entrypoint: `cmd/gophermind/main.go`
- Core packages: `internal/agent` (loop, taskgraph, verify, reflection),
  `internal/tools` (read/search/edit/shell/sql/http/…), `internal/tui`,
  `internal/phaseflow` (spec-driven workflow).

## Build / test / format

```
go build ./...            # build everything
go test ./...             # full suite
gofmt -w <dir>            # format (CI expects gofmt-clean)
go vet ./...              # vet
make build                # produces ./gophermind (gitignored)
```

Always run these from the repo root (the directory containing `go.mod`).

## Conventions (this repo)

- Comment density and style match surrounding code: doc comments explain *why*,
  not *what*; every exported symbol has a doc comment.
- Prefer surgical changes; match existing style even when you'd do it differently.
- Tests live beside code as `*_test.go`; table-driven where it helps.
- New subsystems go under `internal/<name>/`; keep the repo root minimal (docs in
  `docs/`, scripts in `scripts/`).
- Slash commands: TUI dispatch in `internal/tui/update.go` + `commands.go`; CLI
  subcommands are `if cmd == "…"` blocks (early) or `case` arms in the main
  `switch cmd` in `cmd/gophermind/main.go`.

## PhaseFlow (internal/phaseflow)

Native port of github.com/jbrahy/metaphaseflow (MIT, © 2025 Lex Christopherson).
State under `.planning/`. User surface: `gophermind phase <cmd>` and `/phase
<cmd>`. Deterministic bookkeeping (status/next/done/sync/archive) is pure Go;
loop steps (roadmap/plan/execute/verify/milestone + any embedded command by
name) run the agent with a state-seeded prompt. Vendored assets under
`internal/phaseflow/assets/` are the source of truth for embedding — re-vendor,
don't hand-edit. See CREDITS.md for attribution.

## Secrets

`credentials.md` holds secrets and is gitignored — never commit it, never log
its contents.
