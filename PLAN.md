---
phase: 01-prompt-system
type: auto
wave: 1
depends_on: []
---

# Phase: Structured Prompt System

## Objective

Integrate the MetaPromptingFramework's structured prompting approach into GopherMind. Replace the flat, 20-line system prompt with a composable, role-aware prompt system that supports YAML frontmatter, XML-structured sections, and project context injection — while keeping the existing tool loop, safety, and LLM client unchanged.

## Context

- **Current state**: `internal/agent/prompt.go` contains a single `systemPrompt` constant (~20 lines of prose). The agent (`internal/agent/loop.go`) seeds this into `a.msgs` at construction.
- **Target state**: A `internal/prompt/` package that loads structured prompt templates (YAML frontmatter + XML sections), composes them into a final system prompt, and supports project context injection (CLAUDE.md, skills).
- **Reference**: `~/OtherProjects/MetaPromptingFramework/get-shit-done/agents/` — PhaseFlow agent files use YAML frontmatter (`name`, `description`, `tools`, `color`) and XML sections (`<role>`, `<project_context>`, `<philosophy>`, `<execution_flow>`, `<deviation_rules>`, `<verification_strategy>`, etc.).
- **Constraints**: Do not change the LLM client, tool registry, safety package, or the agent's tool loop. Only add a new `internal/prompt/` package and wire it into the agent constructor.

## Tasks

### Task 1: Create `internal/prompt/` package with template parser

**Files:** `internal/prompt/template.go` (new), `internal/prompt/template_test.go` (new)

**Action:** Build a prompt template system that:

1. **Parses YAML frontmatter** from `.md` files — extracts `name`, `description`, `tools`, `color` fields. Use `gopkg.in/yaml.v3` (already available via go.mod) or a lightweight custom parser.
2. **Parses XML-like sections** — extracts content between `<section_name>` and `</section_name>` tags. Section names are case-sensitive. Content is trimmed of leading/trailing whitespace.
3. **Provides a `Template` struct** with:
   - `Name string` — from frontmatter
   - `Description string` — from frontmatter
   - `Tools []string` — from frontmatter
   - `Color string` — from frontmatter
   - `Sections map[string]string` — section name → content
   - `Raw string` — the full original content
4. **Provides a `Load(path string) (*Template, error)`** function that reads a file, parses frontmatter and sections.
5. **Provides a `Build(template *Template, context map[string]string) string`** function that assembles the final system prompt by concatenating sections in a defined order, optionally injecting context variables (e.g., `{{.ProjectInstructions}}`).

**Verify:** Unit tests for:
- Parsing a file with frontmatter and multiple XML sections
- Parsing a file with only frontmatter (no sections)
- Parsing a file with only sections (no frontmatter)
- Error handling for malformed frontmatter
- Error handling for unclosed XML tags
- Context variable injection in sections

### Task 2: Create prompt builder with default enhanced system prompt

**Files:** `internal/prompt/builder.go` (new), `internal/prompt/builder_test.go` (new)

**Action:** Build a `Builder` type that:

1. **Loads a default enhanced system prompt** from an embedded or bundled `.md` file. The default prompt should be inspired by the MetaPromptingFramework's agent files but adapted for GopherMind's simpler scope. It should include:
   - `<role>` — identity as GopherMind, precise coding agent
   - `<workflow>` — explore → edit → verify loop
   - `<tools>` — description of available tools and their constraints
   - `<safety>` — path containment, command deny-list, approval gates
   - `<rules>` — path handling, style matching, completion criteria
2. **Supports project context injection** — when a `CLAUDE.md` exists in the repo root, read it and inject it into the prompt under a `<project_context>` section.
3. **Supports skills discovery** — check for `.claude/skills/` or `.agents/skills/` directories, read `SKILL.md` for each, and inject relevant skill rules.
4. **Provides `Build() (string, error)`** that returns the final system prompt string.

**Verify:** Unit tests for:
- Building a prompt from the default template
- Building a prompt with project context injection
- Building a prompt with skills injection
- Error handling when template file is missing

### Task 3: Wire the prompt system into the agent

**Files:** `internal/agent/loop.go` (modify), `internal/agent/prompt.go` (remove or deprecate)

**Action:**

1. **Modify `internal/agent/loop.go`**:
   - Import `gophermind/internal/prompt`
   - In `New()`, use `prompt.Builder` to construct the system prompt instead of the hardcoded `systemPrompt` constant
   - Pass the repo root path to the builder so it can find `CLAUDE.md` and skills
   - Handle builder errors gracefully (fall back to a minimal default prompt if building fails)
2. **Remove or deprecate `internal/agent/prompt.go`**:
   - Delete the `systemPrompt` constant
   - The file can be removed entirely since the prompt system now lives in `internal/prompt/`

**Verify:**
- `go build ./...` passes
- `go test ./...` passes
- The agent still works with the new prompt system (integration test)

### Task 4: Add integration test for the prompt system

**Files:** `internal/prompt/integration_test.go` (new)

**Action:**

1. Create an integration test that:
   - Sets up a temporary directory with a mock `CLAUDE.md` and a skills directory
   - Builds a prompt using the builder
   - Verifies the final prompt contains the expected sections and injected context
   - Cleans up the temporary directory

**Verify:**
- Test passes in isolation
- Test is self-contained (no external dependencies)

## Success Criteria

- [ ] `internal/prompt/` package exists with template parser and builder
- [ ] Template parser handles YAML frontmatter and XML sections
- [ ] Builder produces a structured system prompt with project context injection
- [ ] Agent uses the new prompt system instead of the hardcoded constant
- [ ] All existing tests pass
- [ ] New tests cover template parsing, building, and integration
- [ ] `go build ./...` and `go test ./...` pass
- [ ] The system prompt is inspired by MetaPromptingFramework's structured approach but adapted for GopherMind's scope

## Notes

- The MetaPromptingFramework's agent files are designed for Claude Code's multi-agent orchestration. GopherMind is a single-agent system, so the prompt should be simplified accordingly.
- Do not attempt to replicate the full PhaseFlow orchestration (phases, waves, checkpoints). Focus on the prompt structure and context injection.
- The `gopkg.in/yaml.v3` dependency is already in go.mod, so no new dependencies are needed.
- If `CLAUDE.md` doesn't exist, the builder should work without it (graceful degradation).