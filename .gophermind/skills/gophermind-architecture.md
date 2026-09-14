# GopherMind Architecture & Navigation

Quick reference for finding code, understanding data flow, and identifying where to make changes.

## Module Structure at a Glance

**gophermind** is a single Go binary with these core domains:

| Domain | Package | Purpose |
|--------|---------|---------|
| **Agent Loop** | `internal/agent/` | Task decomposition, tool invocation, streaming results |
| **Tools** | `internal/tools/` | File read/search/edit, shell commands, SQL, HTTP |
| **TUI** | `internal/tui/` | Terminal UI (Charm-based), command dispatch, rendering |
| **Workflow** | `internal/phaseflow/` | Spec-driven tasks (from metaphaseflow) |
| **Prompts** | `internal/prompt/` | Prompt registry, persona system, ASCII art |
| **Models** | `internal/models/` | OpenAI-compatible API abstraction |
| **CLI** | `cmd/gophermind/` | Entry point, provider selection, mode routing |

**State**: `.planning/` (PhaseFlow), `.remember/` (sessions), `workspace/` (projects).

## Finding Code by Task

### "I need to add a new tool" (e.g., `read-csv`)
1. Define the tool in `internal/tools/csv.go`:
   ```go
   type CSVTool struct {}
   func (t *CSVTool) Name() string { return "read-csv" }
   func (t *CSVTool) Description() string { return "Read CSV file..." }
   func (t *CSVTool) Schema() map[string]interface{} { return ... }
   func (t *CSVTool) Run(ctx, args) (result, error) { ... }
   ```
2. Register in `internal/tools/registry.go` or `internal/agent/loop.go`
3. Update system prompt in `internal/prompt/registry.go` (tool descriptions)
4. Add tests as `internal/tools/csv_test.go`

### "I need to change how the TUI renders"
1. Check rendering logic in `internal/tui/view.go`
2. Update model in `internal/tui/tui.go` if state changes needed
3. Check event handling in `internal/tui/update.go`
4. Test with `make build` and run `./gophermind` interactively

### "I need to add a slash command" (e.g., `/reset`)
1. Handle in `internal/tui/commands.go` or `internal/tui/update.go`
2. Dispatch to agent action or tool
3. Add documentation in help text
4. Test in TUI

### "I need to modify the agent loop"
1. Core loop logic: `internal/agent/loop.go`
2. Task decomposition: `internal/agent/taskgraph.go`
3. Verification: `internal/agent/verify.go`
4. Streaming: `internal/agent/stream.go` or `internal/models/stream.go`

### "I need to add a persona" (e.g., "debugger", "architect")
1. Define prompts in `internal/prompt/registry.go`
2. User selects via `--persona` flag or `/persona` command
3. Injected into system prompt via `internal/prompt/render.go`

### "I need to change model provider logic"
1. Provider interface: `internal/models/provider.go`
2. Implementations (OpenAI, Ollama, etc.): `internal/models/`
3. CLI entry point: `cmd/gophermind/main.go` (provider selection)
4. Config loading: `internal/config/`

## Key Files for Context

**When reading code**, start with these to understand structure:

| File | Context | Lines |
|------|---------|-------|
| `cmd/gophermind/main.go` | CLI entry, mode selection | First 100 lines |
| `internal/agent/loop.go` | Main agent loop, tool invocation | Full file (~200) |
| `internal/tui/tui.go` | TUI state model | Structure only |
| `internal/tui/update.go` | Event handler, command dispatch | Skip event details, focus on flow |
| `internal/tools/tool.go` | Tool interface and runner | Full file (~50) |
| `internal/prompt/registry.go` | System prompt, personas | Skim structure |
| `internal/models/provider.go` | Model provider interface | Full file (~30) |

## Data Flow Diagrams

### User Input to Tool Execution
```
TUI: User types prompt
  ↓
tui/update.go: Parse command
  ↓
Dispatch to agent.Execute(ctx, prompt)
  ↓
agent/loop.go: Decompose → LLM generates task graph
  ↓
For each task:
  - Invoke tool (e.g., read, edit, shell)
  - Stream results to TUI
  - Collect for synthesis
  ↓
LLM synthesizes response
  ↓
TUI: Display response + token meter
```

### Tool Invocation
```
Tool{Name, Description, Schema, Run} defined in internal/tools/
  ↓
LLM calls: {"tool":"read","args":{"path":"main.go"}}
  ↓
agent/loop.go: Unmarshal args, validate schema
  ↓
Call tool.Run(ctx, args)
  ↓
Stream result:
  - Partial output chunks
  - Error if it fails
  ↓
Collect full result for synthesis
```

### PhaseFlow State Machine
```
.planning/PROJECT/state.json holds phase state
  ↓
/phase status → Read and display state
  ↓
/phase next → Advance to next phase, run agent with phase prompt
  ↓
Agent executes tasks for that phase
  ↓
/phase done → Mark complete, archive results
  ↓
State updated, next /phase next will skip to next phase
```

## Common Patterns

**Streaming output** — Don't buffer results; stream them incrementally:
```go
// In a tool's Run():
for line := range results {
    select {
    case output <- line:  // Non-blocking send to stream
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

**Error handling** — Tool failures are not agent failures; return error + partial result:
```go
// In agent/loop.go:
result, err := tool.Run(ctx, args)
if err != nil {
    // Log error, stream it to TUI, mark task as failed but continue
}
```

**File containment** — All file paths must stay within repo root:
```go
// In tools/read.go:
path, err := filepath.Abs(userPath)
if err != nil || !strings.HasPrefix(path, repoRoot) {
    return "", ErrOutsideRepo
}
```

**Deny-list for shell** — Commands like `rm -rf`, `dd` are blocked:
```go
// In tools/shell.go:
if DenyList.Contains(cmd) {
    return "", ErrCommandDenied
}
```

## Testing Strategy

- **Unit tests**: Each package has `*_test.go` files beside code
- **Table-driven**: Multiple scenarios in one test
- **Golden files**: ASCII art and prompt output use `testdata/*.golden`
- **Mocks**: Mock LLM provider for agent tests
- **Integration**: iOS tests run against real simulator

Always run `go test ./...` before committing.

## Related Docs

- **docs/ARCHITECTURE-INDEX.md** — Full architectural reference
- **PROJECT.md** — Conventions, build, secrets
- **docs/DEPLOYMENT.md** — Serve mode and monitoring
- **docs/SKILLS.md** — Custom skill packs
- **.superpowers/plans/** — Execution plans for major features

## Quick Grep References

Find code by pattern:
```bash
# Find all tool registrations
grep -r "Name()" internal/tools/ | grep -v _test

# Find TUI command handlers
grep -r "/[a-z]" internal/tui/commands.go

# Find system prompt definitions
grep -r "system prompt" internal/prompt/

# Find all uses of a function
grep -r "FunctionName" --include="*.go"
```
