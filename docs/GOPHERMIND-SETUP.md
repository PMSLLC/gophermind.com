# GopherMind Setup for gophermind.com

This document describes how gophermind (the AI agent running on `<server-host>:8090`) is configured to work autonomously on the gophermind.com codebase.

## Setup Overview

Three components are configured:

1. **Project Configuration** — metadata for agent discovery
2. **RAG Indexing** — semantic understanding of architecture
3. **Custom Skills** — project-specific workflows and patterns

## 1. Project Configuration

**File**: `GOPHERMIND.toml`

Contains metadata about the project:
- Module name, language, version
- Directory structure and roles
- Build commands and make targets
- Testing patterns
- Architecture summary
- Development conventions
- Secrets and state directories

**Use**: Gophermind consults this to understand project layout, build process, and testing requirements. Agents know they can run `go build ./...`, `go test ./...`, `make ios-test`, etc.

## 2. RAG Indexing

Two documents provide semantic understanding:

### `docs/ARCHITECTURE-INDEX.md`

Comprehensive reference for:
- Complete module layout with descriptions
- Core abstractions (Agent, TaskGraph, Tool, TUI, Prompt Registry, Model Provider)
- Key data flows (agent loop → tool → stream)
- Important files for common tasks
- Testing patterns and golden file management
- Known constraints and gotchas
- Development workflow

**Size**: ~1500 lines. Gophermind uses this for:
- Understanding "where does the code for X live?"
- Answering "what is the relationship between A and B?"
- Learning testing patterns before adding tests
- Understanding constraints before making changes

### Architecture-Adjacent Docs

Also indexed for context:
- `PROJECT.md` — conventions, secrets, PhaseFlow
- `docs/DEPLOYMENT.md` — serve mode, monitoring
- `docs/SKILLS.md` — custom skill packs
- `docs/RELEASING.md` — release process
- `design/README.md` — design system

## 3. Custom Skills

Skills are Markdown files in `.gophermind/skills/` that are **injected into every turn's system prompt**. Project-specific skills added:

### `gophermind-build-test.md`

Workflow reference for:
- Build commands (go build, go test, make targets)
- Testing patterns (unit, golden, iOS, flaky test troubleshooting)
- Verification checklist before commit
- Release build requirements
- Common issues and fixes
- Test anatomy example

**Use**: When gophermind needs to build, test, or debug, it consults this for exact commands and patterns.

### `gophermind-architecture.md`

Quick navigation reference:
- Module structure at a glance
- How to find code for a task (e.g., "add a tool", "change TUI behavior")
- Key files for context-setting
- Data flow diagrams
- Common patterns (streaming, error handling, file containment)
- Testing strategy
- Quick grep references

**Use**: When gophermind approaches a task, it uses this to find the right files and understand patterns before coding.

### `gophermind-phaseflow.md`

Reference for:
- PhaseFlow concepts and commands
- State machine and JSON schema
- Agent execution in phases
- Phase spec structure
- Creating new phase projects
- Archiving and cleanup

**Use**: When working on multi-step projects (like the "Go Pher It" logo), gophermind uses this to understand phase workflow commands and state.

## Registering with gophermind Server

See **docs/SERVER-SETUP.md** for complete server setup including:
- Repository cloned to `/home/gophermind/workspace/gophermind.com/`
- SSH config for `gophermind-server` shorthand
- Service registration via HTTP API or CLI
- Keeping server copy in sync

**Quick registration** (once repo is cloned):
```bash
curl -X POST http://<server-host>:8090/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "gophermind",
    "path": "/home/gophermind/workspace/gophermind.com",
    "config_url": "https://raw.githubusercontent.com/jbrahy/gophermind.com/main/GOPHERMIND.toml"
  }'
```

## How Gophermind Uses This Setup

When gophermind starts working on a task in this repo:

1. **Discovery**: Reads `GOPHERMIND.toml` to understand project structure
2. **Context**: Injects project-specific skills from `.gophermind/skills/`
3. **Architecture Search**: Uses `docs/ARCHITECTURE-INDEX.md` for semantic understanding
4. **Task Planning**: Consults skills to find files, build commands, test patterns
5. **Execution**: Runs tools (read/edit/shell) with knowledge of project constraints
6. **Verification**: Uses build/test commands to validate changes

Example: If asked "add a new tool to read JSON files":

1. Reads `gophermind-architecture.md` → "tools live in internal/tools/, add to registry.go"
2. Reads `ARCHITECTURE-INDEX.md` → understands Tool interface and registration
3. Reads `internal/tools/tool.go` → sees existing pattern
4. Creates `internal/tools/json.go` with same pattern
5. Runs `go test ./...` to verify
6. Commits with atomic message

## What Agents Can Now Do

With this setup, gophermind can:

- **Understand the codebase** — knows where code lives, how it's organized, what constraints apply
- **Build and test** — knows exact commands, test patterns, verification gates
- **Work on features** — can read architecture, understand patterns, make surgical changes
- **Work on bugs** — has debugging skills + ability to find relevant code
- **Work on PhaseFlow projects** — can execute multi-step tasks with state tracking
- **Review code** — has code-review skill + architectural understanding
- **Release** — knows release process (from PROJECT.md, RELEASING.md)

## Maintenance

**Keep these in sync**:

- `GOPHERMIND.toml` — update when build commands or structure changes
- `docs/ARCHITECTURE-INDEX.md` — update when major packages/files are added/removed
- `.gophermind/skills/` — update when patterns, conventions, or workflows change
- `PROJECT.md` — source of truth for conventions; this setup just indexes it

**Don't over-document**:
- Skills have token cost (~7k for all 3 project skills per turn)
- If something isn't a pattern gophermind uses every turn, make it a tool instead
- Dead docs rot — keep architecture docs current or gophermind will make wrong assumptions

## Related Documentation

- `GOPHERMIND.toml` — Project metadata (this setup references)
- `docs/ARCHITECTURE-INDEX.md` — Full architecture reference (indexed for RAG)
- `PROJECT.md` — Project conventions (updated separately)
- `docs/SKILLS.md` — How skills work (user documentation)
- `.gophermind/skills/*.md` — Injected on every turn (this setup provides 3)

## Troubleshooting

**"Gophermind can't find the file"** — Check `ARCHITECTURE-INDEX.md` is up to date with actual paths. Run `find . -name <filename>` to verify the file exists.

**"Gophermind uses the wrong pattern"** — Check if an old example is embedded in a skill file. Update the skill with the current pattern, or provide a code example in a test.

**"Skill token cost is too high"** — Measure actual impact: `make test` should show per-turn token usage. If a skill isn't used on most turns, move it to a fetched skill source (off until enabled) or convert it to a tool.

**"Gophermind made a wrong architectural decision"** — Often means ARCHITECTURE-INDEX.md is stale or incomplete. Verify the decision is documented, then update the index.
