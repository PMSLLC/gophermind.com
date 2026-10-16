# GopherMind Architecture Index

Comprehensive reference for gophermind's internal structure, data flow, and key abstractions. Use this for understanding where code lives, how components interact, and where to make changes.

## Module Layout

```
gophermind (go 1.25.0)
├── cmd/gophermind/
│   └── main.go                    # CLI entry, provider selection, serve/interactive/batch modes
├── internal/
│   ├── agent/                     # Core agent loop and task execution
│   │   ├── loop.go                # Main agent loop, tool invocation, streaming
│   │   ├── taskgraph.go           # Task decomposition, DAG execution
│   │   ├── verify.go              # Verification strategies, reflection
│   │   ├── stream.go              # Streaming response handling
│   │   └── *_test.go
│   ├── tools/                     # Agent tool definitions
│   │   ├── tool.go                # Tool struct, runner, schema
│   │   ├── read.go                # File read with symlink containment
│   │   ├── search.go              # Semantic search + grep
│   │   ├── edit.go                # Line-level edit + full-file replace
│   │   ├── shell.go               # Deny-list based command execution
│   │   ├── sql.go                 # SQL query, schema inspection
│   │   ├── http.go                # HTTP/webhook requests
│   │   ├── model.go               # Model switching, config
│   │   └── *_test.go
│   ├── tui/                       # Terminal user interface
│   │   ├── tui.go                 # Main TUI struct, update/view
│   │   ├── update.go              # Event handling, command dispatch
│   │   ├── commands.go            # Slash commands (/read, /edit, etc.)
│   │   ├── view.go                # Charm-based rendering
│   │   ├── prompt.go              # Prompt input with history
│   │   └── *_test.go
│   └── ...
├── .remember/                     # Session memory (gitignored)
├── .github/                       # CI/CD workflows
├── GOPHERMIND.toml                # Project metadata for gophermind agents
├── PROJECT.md                     # Project conventions
├── CONTEXT.md                     # Session handoff
└── go.mod, go.sum                 # Dependency manifest

```

## Key Data Flows

### Agent Loop (internal/agent/loop.go)

```
Input (user prompt)
    ↓
Parse command / check cache
    ↓
Generate task graph (agent → LLM for decomposition)
    ↓
Execute tasks (DAG traversal):
    - Tool invocation (read/search/edit/shell/sql/http)
    - Stream results to TUI
    - Collect outputs
    ↓
Verification (optional reflection loop):
    - LLM verifies task outputs
    - Marks tasks as done or requests retries
    ↓
Return synthesized response
    ↓
Cache result (optional)
```

### Tool Invocation (internal/tools/tool.go)

```
Tool{Name, Description, Schema, Run}
    ↓
LLM calls tool with JSON args
    ↓
Parse + validate against schema
    ↓
Run(ctx, args) → (result, error)
    ↓
Stream result to TUI + collect for response synthesis
```

## Core Abstractions

### Agent (internal/agent/)

**TaskGraph**: Represents work as a DAG of tasks. Each task has:
- Name, description
- Tools it will use
- Dependencies (other tasks)
- Status (pending/running/done/failed)

**Tool**: Runnable action with schema validation.
```go
type Tool struct {
    Name        string                 // "read", "edit", etc.
    Description string                 // What it does
    Schema      map[string]interface{} // JSON schema of args
    Run         func(ctx, args) (result, error)
}
```

**Stream**: Results flow back to TUI incrementally (not buffered). Supports:
- Partial LLM responses (token-by-token)
- Tool outputs (section headers, results, errors)
- Status indicators (working, done, failed)

### TUI (internal/tui/)

**Model**: Complete application state
- Current prompt and history
- Streaming output from agent
- Token/cost meter
- Mode (interactive, batch, serve)
- Approval gate state

**Update**: Event handling (user input, agent output, signals)

**View**: Render to terminal using Charm (bubbletea/lipgloss)
- Scrollback with syntax highlighting
- Inline approvals (prompt in-line, no modal)
- Live token/cost update
- Gopher greeting + fortune

### Prompt Registry (internal/prompt/)

Named prompts for different contexts:
- `agent_system` — core system prompt (tool descriptions, constraints, safety)
- `persona_*` — role-specific framing (debugger, architect, etc.)
- `reflex_critique` — reflection loop prompts

Rendered with context injection:
- Available tools (auto-generated from tool.go)
- Persona instructions
- Prior outputs (for synthesis)
- Constraints (file containment, denial-list)

### Model Provider (internal/models/)

Abstraction over OpenAI-compatible APIs:
```go
type Provider interface {
    Complete(ctx, model, prompt, tools) (response, usage, error)
    Stream(ctx, model, prompt, tools) (<-chan Token, error)
    ListModels(ctx) ([]Model, error)
}
```

Implementations: OpenAI, Ollama, vLLM, LM Studio, local endpoints.

## Important Files for Common Tasks

| Task | Primary File | Secondary Files |
|------|--------------|-----------------|
| Add a tool | `internal/tools/tool.go` | Update registry in `internal/prompt/registry.go` |
| Change TUI behavior | `internal/tui/update.go` | Check rendering in `internal/tui/view.go` |
| Modify agent loop | `internal/agent/loop.go` | Affects streaming in `internal/tui/`, tool invocation |
| Add a slash command | `internal/tui/commands.go` | Route to tool or agent action |
| Add a persona | `internal/prompt/registry.go` | Define system + role prompts |
| Change model provider | `internal/models/provider.go` | Update CLI in `cmd/gophermind/main.go` |

## Testing Patterns

**Unit tests**: Beside code as `*_test.go`, table-driven where helpful.
```bash
go test ./...           # Run all tests
go test -v ./internal/agent/  # Verbose, single package
go test -run TestName   # Single test
```

**Golden file tests**: `internal/prompt/testdata/` holds expected outputs.
```bash
GOLDEN_UPDATE=1 go test ./internal/prompt/  # Re-bless golden files
```

**iOS tests**: Separate Xcode suite.
```bash
make ios-test   # Run iOS unit tests
```

## Development Workflow

1. **Understand the task** — read CONTEXT.md for session state, check docs/ for background
2. **Locate the code** — use this index or grep for function/package names
3. **Write tests first** (TDD) — many tests live in .superpowers/plans/
4. **Make surgical changes** — touch only what's needed, match existing style
5. **Run the full gate** — `go test ./...`, `go vet ./...`, `gofmt -l .`
6. **Commit atomically** — one test or feature per commit with clear message
7. **Verify in-app** — run `make build` and test the feature in the TUI or iOS app

## Known Constraints & Gotchas

- **File containment** (internal/tools/read.go): Symlinks are resolved and checked against repo root. Paths outside the repo are denied.
- **Shell deny-list** (internal/tools/shell.go): Commands like `rm -rf`, `dd`, network tools are blocked. No shell escaping; use args array.
- **TUI streaming**: Results arrive incrementally; buffering them would defeat the purpose.
- **Model switching**: Changing model mid-conversation loses context (no multi-model synthesis yet).
- **Golden files**: Re-blessing with `GOLDEN_UPDATE=1` will bake in bugs if you don't visually inspect the output first.

## Related Documentation

- **PROJECT.md** — conventions, build commands, secrets
- **CONTEXT.md** — active session state and recent decisions
- **docs/DEPLOYMENT.md** — serve mode setup and monitoring
- **docs/SKILLS.md** — custom prompt injection packs
- **docs/RELEASING.md** — release process and signing
- **design/README.md** — design system and color palette
- **.superpowers/plans/** — execution plans and specs
