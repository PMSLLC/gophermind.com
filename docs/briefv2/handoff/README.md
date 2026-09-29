# GopherMind v2 Handoff

Read this file first. It tells you what you are building, in what order, and what you must not guess.

## What this is

GopherMind is JB's Go LLM harness. This handoff adds the v2 "brief to build" feature: the harness reads a
Markdown product brief, asks its questions, gets a plan approved, decomposes the product into a tree of
small JSON function-level tasks, and executes them with free LLM providers in waves, one model at a time
per task, until every test passes.

The authoritative design is `SPEC.md`. Everything else in this zip is either a machine-readable extract
of it (schemas), a worked example (examples/), an interface you must implement to the letter
(interfaces/), or a prompt template (prompts/). `BUILD_PLAN.md` is the ordered list of deliverables with
the test that proves each one is done.

## Reading order

1. `README.md` (this file)
2. `SPEC.md` sections Purpose, Brief format, Task tree, Task node schema
3. `examples/brief.md` and `examples/tree/` so the shapes are concrete
4. `schema/` (three JSON Schemas, embed them in the binary)
5. `interfaces/` (Go interfaces for the blackboard and the model provider)
6. `prompts/` (the six prompt templates the planner and executor use)
7. `BUILD_PLAN.md` and build in that order

## Before you write code

- Inspect the existing GopherMind repo and the existing blackboard from JB's recursive agent system.
  If that blackboard is Go, durable, and supports an atomic claim, adapt it to `interfaces/blackboard.go`.
  If not, implement the SQLite backend described in `BUILD_PLAN.md` item 4.
- Adapt package names to the repo's existing layout. The layout in `BUILD_PLAN.md` is a suggestion.
- Do not add non-Go dependencies. No cgo. The harness must stay a single portable binary.

## Non-negotiables

These come straight from JB. Do not trade them away for convenience.

1. Every tool is Go and compiled into the harness binary.
2. Git goes through go-git, not the git CLI.
3. Every outbound request, including LLM provider calls, goes through the harness proxy.
4. Secret values never appear in the brief, the tree, the blackboard, logs, or any model prompt.
5. One function per leaf. A leaf never reads another node's implementation.
6. Tests are written before implementation, by a separate pass, and the implementer cannot edit them.
7. Nothing executes before the human approves the plan.
8. A task that exhausts every model goes to revision with its full failure history, at most 2 rounds,
   then to a human.

## What to ask JB about

Ask before building if any of these turn out to differ from what the spec assumes. Otherwise build as
specified and note the assumption in your final summary.

- The existing blackboard's language, storage, and claim semantics.
- Which free LLM providers are wired in today and how their credentials are currently stored.
- Whether the repo already has a config file format (the spec proposes `gophermind.yaml`).

## Definition of done

Every item in `BUILD_PLAN.md` has its test passing, `examples/brief.md` runs end to end against a scratch
Go repo (a fresh `go mod init` with the example's package layout is fine), and `go vet` plus the repo's
linter are clean.
