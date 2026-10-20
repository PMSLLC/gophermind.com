# GopherMind Architecture Index

Reference for gophermind's structure, data flow, and key abstractions. Use this for understanding where code lives, how components interact, and where to make changes.

## Module Layout

The repository is a Go workspace (`go.work`) of four modules: the root (the CLI), `gophermind-lib` (shared packages), `gophermind-osx` (the native macOS app) and `bubblecomplete` (the autocomplete library). `gophermind-server` is a package of the root module.

```
gophermind.com
├── cmd/gophermind/                # CLI entry: main.go (modes, dispatch), brief*.go (`gophermind brief ...`),
│                                  #   project.go (`gophermind project`), phase.go (the /phase removal notice)
├── gophermind-server/             # HTTP server for the desktop app (serve, WireGuard, OAuth)
├── gophermind-lib/                # Shared packages
│   ├── agent/                     # Core agent loop, budgets, reflection, debate, checkpoints
│   ├── llm/                       # OpenAI-compatible client: streaming, retry, fallback, cache, TLS
│   ├── tools/                     # Agent tool definitions (read, search, edit, shell, SQL, HTTP, ...)
│   ├── tui/                       # Terminal UI (Bubble Tea): model, update, view, slash commands
│   ├── prompt/                    # Prompt registry, templates, golden-tested default prompt
│   ├── config/                    # Layered config: flags, env, .env, ~/.gophermind/config.json
│   ├── session/ , modelcat/       # Saved sessions; model catalogue
│   ├── serve/                     # `gophermind serve`: webhook, sessions (SSE), approvals, pipeline routes
│   ├── runview/                   # Read-only view of a build run folder (the pipeline contract's types)
│   ├── build/                     # Brief to build: planner, executor, run state (see below)
│   ├── project/ , projectctx/     # Project context loading and budgets
│   ├── safety/ , secaudit/        # Path containment, deny-lists, secret scanning
│   ├── gates/                     # Source-tree gate tests (removed packages and paths stay removed)
│   └── ... (mcp, mcpclient, skills, embed, codeindex, telemetry, and others)
├── gophermind-osx/                # Native macOS app (libui-ng): chat, connections, pipeline panel
├── desktop/                       # The Wails desktop app
├── ios/                           # iOS app
├── design/                        # Design assets and rationale
├── docs/                          # Documentation (see below)
├── scripts/                       # Build and deploy tooling
├── .gophermind/                   # Project-local skills and config
├── .github/                       # CI/CD workflows
├── GOPHERMIND.toml                # Project metadata for gophermind agents
├── PROJECT.md                     # Project conventions
└── go.mod, go.work                # Dependency manifest and workspace
```

Documentation: `docs/build/README.md` (the build pipeline), `docs/pipeline-contract.md` (the run view contract, `schema_version` 1), `docs/DEPLOYMENT.md`, `docs/RELEASING.md`, `docs/REMOTE-SESSIONS.md`, `docs/SKILLS.md`, `docs/mobile-serve.md`, and `docs/superpowers/` (specs and plans).

## Key Data Flows

### Agent Loop (gophermind-lib/agent/loop.go)

```
Input (user prompt)
    |
Parse command / check cache
    |
Build the message list (system prompt, history, project context)
    |
Model call (gophermind-lib/llm), streamed to the TUI
    |
Tool calls: validate, run behind the safety layer and the approval gate, return results
    |
Repeat until the model answers or the iteration budget ends
    |
Optional reflection or verification pass; cache the result
```

### Tool Invocation (gophermind-lib/tools/tool.go)

```
Tool{Name, Description, Schema, Run}
    |
Model calls the tool with JSON args
    |
Parse and validate against the schema
    |
Run(ctx, args) -> (result, error)
    |
Result streamed to the TUI and fed back to the model
```

### Brief to build (gophermind-lib/build)

```
brief.md --plan--> .gophermind/<run-id>/   nodes (JSON), approval.json, coverage.json
                        |
                        +--run--> executor: waves of leaves (optionally `executor.workers` at once),
                                  revise stage, acceptance, commits on a work branch
                        |
                        +--> runtime files, _state/events.jsonl, _state/calls.jsonl, attempts/, report.json
```

`gophermind brief plan` and `brief run` are the stepwise commands; `/project` and `gophermind project` chain them. All run state is files in the run folder (there is no database), so a run can be opened, copied and resumed. `gophermind serve` and the desktop app show a run through `gophermind-lib/runview`. See `docs/build/README.md`.

## Core Abstractions

### Agent (gophermind-lib/agent/)

**Loop**: model call, tool calls, repeat, bounded by an iteration and token budget.

**Tool**: Runnable action with schema validation.
```go
type Tool struct {
    Name        string                 // "read", "edit", etc.
    Description string                 // What it does
    Schema      map[string]interface{} // JSON schema of args
    Run         func(ctx, args) (result, error)
}
```

**Stream**: Results flow back to the TUI incrementally (not buffered): partial model responses, tool output, status indicators.

### TUI (gophermind-lib/tui/)

**Model**: Complete application state: current prompt and history, streaming output from the agent, token and cost meter, approval gate state.

**Update**: Event handling (user input, agent output, signals) and slash command dispatch. The retired `/phase`, `/plan-v1`, `/plan-v1-execute`, `/project-execute` and `/questions` print a one-line notice (`phase_removed.go`).

**View**: Rendering with Charm (bubbletea/lipgloss): scrollback, inline approvals, live token and cost update, gopher greeting.

### Prompt Registry (gophermind-lib/prompt/)

Named prompts and personas, rendered with context injection (available tools, persona instructions, constraints). The default prompt is checked against a golden file.

### Model Client (gophermind-lib/llm/)

`llm.Client` is the OpenAI-compatible client: `Stream`, `ListModels`, retry with backoff, fallback across models, an optional response cache, and mTLS or custom CA support. Profiles live in `gophermind-lib/config`.

### Build pipeline (gophermind-lib/build/)

Sub-packages by role: `brief` and `vault` (the brief and its secrets), `tree` (nodes, waves, readiness, the file store), `contract`, `planner` (the stages Load to Test-writer, and the revise stage), `executor` (leaf loop, waves, parallel workers, acceptance, resume, landing), `blackboard`, `ledger`, `events`, `revlog`, `runfs` (run state as files), `router` and `provider` (model routing and the privacy rule), `report`, `projectrun` (the `/project` chain and preflight), `sandbox`, `proxy`, `runner`. The package map is in `docs/build/README.md`.

### Run view and server (gophermind-lib/runview, gophermind-lib/serve)

`runview` reads a run folder and never writes to it. `serve` exposes it as `GET /pipeline` (dashboard) and `/pipeline/runs`, `/state`, `/node`, `/artifact`, `/decisions`, `/report` and `/events` (SSE). The server and the desktop app must be released together. The wire contract is `docs/pipeline-contract.md`.

## Important Files for Common Tasks

| Task | Primary File | Secondary Files |
|------|--------------|-----------------|
| Add a tool | `gophermind-lib/tools/tool.go` | Register it where `cmd/gophermind/main.go` builds the tool list |
| Change TUI behavior | `gophermind-lib/tui/update.go` | Rendering in `gophermind-lib/tui/view.go` |
| Modify agent loop | `gophermind-lib/agent/loop.go` | Streaming in `gophermind-lib/tui/`, tool invocation |
| Add a slash command | `gophermind-lib/tui/commands.go` | `commands_registry.go`; route to a tool or agent action |
| Add a persona | `gophermind-lib/prompt/registry.go` | Define system and role prompts |
| Change the model client | `gophermind-lib/llm/client.go` | CLI wiring in `cmd/gophermind/main.go` |
| Change the build pipeline | `gophermind-lib/build/planner/`, `gophermind-lib/build/executor/` | `docs/build/README.md`, `cmd/gophermind/brief*.go` |
| Change the pipeline endpoints | `gophermind-lib/runview/`, `gophermind-lib/serve/pipeline_api.go` | `docs/pipeline-contract.md`, `gophermind-osx/client/` (release together) |

## Testing Patterns

**Unit tests**: Beside code as `*_test.go`, table-driven where helpful. Run them with a throwaway HOME so a test never touches your config.
```bash
go test ./...                      # root module
go test ./gophermind-lib/...       # shared packages (the executor package takes about 11 minutes: -timeout 30m)
go test -v ./gophermind-lib/agent/ # Verbose, single package
go test -run TestName              # Single test
```

**Gate tests**: `gophermind-lib/gates` fails if a removed package, path or phrase returns.

**Golden file tests**: `gophermind-lib/prompt/testdata/` and `gophermind-lib/runview/testdata/golden/` hold expected outputs.

**iOS tests**: Separate Xcode suite.
```bash
make ios-test   # Run iOS unit tests
```

## Development Workflow

1. **Understand the task**: read CONTEXT.md for session state, check docs/ for background
2. **Locate the code**: use this index or grep for function and package names
3. **Write tests first** (TDD): many plans live in docs/superpowers/plans/
4. **Make surgical changes**: touch only what is needed, match existing style
5. **Run the full gate**: `go test ./...`, `go vet ./...`, `gofmt -l .`
6. **Commit atomically**: one test or feature per commit with a clear message
7. **Verify in-app**: run `make build` and test the feature in the TUI or the app

## Known Constraints & Gotchas

- **File containment** (gophermind-lib/safety): symlinks are resolved and checked against the repo root. Paths outside the repo are denied.
- **Shell deny-list** (gophermind-lib/tools/shell.go): commands like `rm -rf`, `dd`, network tools are blocked.
- **TUI streaming**: results arrive incrementally; buffering them would defeat the purpose.
- **Model switching**: changing model mid-conversation loses context (no multi-model synthesis yet).
- **Pipeline contract**: the server and the desktop app read the same node-based bodies; change one only with the other.
- **Golden files**: re-blessing hides bugs if you do not inspect the output first.

## Related Documentation

- **PROJECT.md**: conventions, build commands, secrets
- **docs/build/README.md**: the brief-to-build pipeline
- **docs/pipeline-contract.md**: the run view contract
- **docs/DEPLOYMENT.md**: serve mode setup and monitoring
- **docs/SKILLS.md**: custom prompt injection packs
- **docs/RELEASING.md**: release process and signing
- **design/README.md**: design system and color palette
