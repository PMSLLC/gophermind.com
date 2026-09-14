# GopherMind Architecture & Navigation

Use to locate code and assess change impact. Follow the shared rules. Library
code lives under `gophermind-lib/<pkg>` (not `internal/`); verify against the
checkout before relying on a path, symbol, interface signature, or execution model.

## Navigation map

| Domain | Start here | Inspect for |
|---|---|---|
| Agent loop | `gophermind-lib/agent/` | Execution, tool dispatch, task graph, verification, streaming |
| Tools | `gophermind-lib/tools/` | Actual tool interface, registration, validation, permissions |
| TUI | `gophermind-lib/tui/` | State, update/view flow, slash commands |
| Workflow | `gophermind-lib/phaseflow/` | State schema, transitions, gates, persistence |
| Prompts | `gophermind-lib/prompt/` | Registry, personas, rendering, skill loading |
| Skills | `gophermind-lib/skills/` | Repo-local pack loading, fetched-skill catalogue |
| Models | `gophermind-lib/llm/` | Provider interface, capability resolution, adapters, streaming |
| CLI/config | `cmd/gophermind/`, `gophermind-lib/config/` | Entry points, flags, provider selection |

Reported state locations: `.planning/<PROJECT>/` for PhaseFlow, `.remember/` for
sessions, and `workspace/` for projects. Verify root resolution and project selection.
Find symbols before reading entire files; inspect relevant callers and tests.

## Change routes

For a new tool, inspect the real interface and a neighboring implementation, then
find the actual registration site. Update prompt/tool metadata only where the
runtime requires it; do not invent signatures or duplicate registrations.

For TUI/slash-command changes, trace input handling through dispatch and state
updates to rendering. For agent/provider changes, trace context, request, streaming,
result/error handling, and cleanup. For personas, verify selection and rendering.
For workflow changes, use `gophermind-phaseflow` and inspect transition tests.

## Boundaries to verify

Streaming must preserve ordering, bounded resource use, cancellation, and channel
ownership. A select between a send and cancellation is cancellation-aware, not
non-blocking: it waits if neither can proceed. Check both blocked receives and
sends. Do not add a dropping default branch unless loss is explicitly acceptable.

Follow the implementation's recoverable/fatal error policy. Preserve partial
results when supported; never turn cancellation, permission denial, or a failed
prerequisite into success or continue dependent tasks blindly.

For file access, inspect the containment helper. A string-prefix test is unsafe
for sibling paths and symlinks. Lexical checks alone do not solve symlink races.
Use verified root-scoped filesystem operations suitable for the repo's Go version,
platform, and threat model; do not assume a particular API is available.

Treat shell deny-lists as defense-in-depth, not a sandbox. Check executable and
argument policy, explicit argument passing, environment, working directory,
timeouts, and actual execution isolation. Do not silently weaken approvals.

Add behavioral tests for the affected boundary; include cancellation, partial
failure, and adversarial input where relevant. Use `gophermind-build-test` for
verification, with platform-specific tests only when affected.

## Further context

Consult existing `docs/ARCHITECTURE-INDEX.md`, `PROJECT.md`, `docs/DEPLOYMENT.md`,
`docs/SKILLS.md`, and relevant plans. Verify whether this checkout uses
`.superpowers/plans/` or `docs/superpowers/plans/`; do not create competing trees.
