# Spec 1 — `/project` planning

**Status:** approved 2026-07-13. Implementation in progress.
**Follow-on:** Spec 2 (orchestrated verify-and-correct execution) consumes this spec's `assignments.json` schema.

## Goal

Add a TUI `/project <name>` command that opens a modal dialog, interviews the
user (iterating with the LLM) to produce a comprehensive spec, decomposes the
spec into phases/tasks with explicit acceptance criteria, and assigns each task
to an agent type (a `prompt.md`) tailored per task, on a chosen model. The
result is an **approved plan** — the input contract for execution (Spec 2).
`/project` itself does not execute tasks.

## Decisions (from brainstorming)

- **Flow:** guided — name + interview → scaffold → generate full plan → approval gate.
- **Completion:** the plan is auto-validated (no placeholders; every task has ≥1
  acceptance criterion, an agent, and a model) and then **user-approved with a
  revise loop**. Approval marks the project ready.
- **Gate:** hard block — `/phase plan|execute|verify|milestone` refuse until
  approved. **TUI only**; CLI `gophermind phase …` is unaffected.
- **Surface:** TUI `/project` only (no CLI subcommand).
- **Agent catalog:** hybrid — pick from a catalog of `prompt.md` agent types
  (seeded from the 33 embedded PhaseFlow agents), tailored per task with an
  addendum.
- **Model:** each catalog type has a default tier (speed|strong, using existing
  `GOPHERMIND_SPEED_MODEL`/strong config); the planner may override per task.
- **UI:** a Bubble Tea modal overlay, not inline prompts.

## User flow

1. `/project [name]` opens the modal. Scaffold `.planning/`; confirm the name.
2. **Interview:** the LLM asks clarifying questions a few at a time (goals,
   users, scope, non-goals, constraints, success criteria); the user answers in
   the modal; loops until the LLM judges it comprehensive or the user hits
   **Generate**. Writes `SPEC.md`.
3. **Decompose + assign:** a planner turn breaks the spec into phases → tasks,
   each with acceptance criteria, and assigns each an agent (catalog + addendum)
   + model. Writes `ROADMAP.md` and `assignments.json`.
4. **Review + approve:** the modal shows the summary + auto-validation. The user
   approves or requests revisions (loops). Approval writes the gate marker and
   closes the modal.

## Persistence (`.planning/`)

- `SPEC.md` — comprehensive spec (overview, goals, users, scope, non-goals,
  constraints, requirements, success criteria).
- `ROADMAP.md` — phases + plans + phase success criteria (existing phaseflow format).
- `assignments.json` — machine-readable, **the Spec 2 contract**:
  ```json
  {
    "tasks": [
      {
        "id": "01-01",
        "phase": "1",
        "title": "…",
        "description": "…",
        "acceptance_criteria": ["…", "…"],
        "agent": "coder",
        "agent_addendum": "task-specific guidance",
        "model": "strong",
        "status": "pending"
      }
    ]
  }
  ```
- `agents/` — the agent catalog (`<name>.prompt.md`, frontmatter `name`,
  `description`, `default_model`, `keywords`), seeded from the embedded PhaseFlow
  agents; user-editable.
- `.outline-approved` — presence marks the plan approved (the gate).

## Components

### A. `internal/phaseflow` (deterministic, fully tested)

- **Assignment schema** — `Task`, `Assignments` types; `LoadAssignments` /
  `SaveAssignments` (`assignments.json`).
- **Agent catalog** — `Agent` type (name, description, default model, keywords,
  body); `LoadCatalog(dir)` parsing `*.prompt.md` frontmatter; `SeedCatalog` to
  write the embedded PhaseFlow agents into the catalog dir with a default model.
- **Validation** — `ValidatePlan(root)` → `{Phases, Tasks int, Issues []string,
  Complete bool}`: no placeholder tokens (`[…]`, `TBD`), every task has ≥1
  acceptance criterion + a known agent + a model, ≥1 phase and task.
- **Approval marker** — `Approved()`, `Approve()`, `Unapprove()` over
  `.planning/.outline-approved`.

### B. TUI modal (`internal/tui`)

- A modal sub-model (states: `name → interview → generating → review →
  approved/cancelled`) rendered as an overlay; the main model delegates
  Update/View to it while active.
- Runs LLM turns via the agent; the interview and decomposition steps build
  seeded prompts (from `SPEC.md` template + the agent catalog) and display
  responses. Agent file tools write `SPEC.md`/`ROADMAP.md`/`assignments.json`.
- The prompt-construction seams are unit-tested; live LLM turns are not.

### C. Gate

- The TUI `/phase` handler checks `Approved()` for `plan|execute|verify|
  milestone` and blocks with a message until approved. `roadmap` stays allowed.

## Spec 2 contract (built later)

The orchestrator reads `assignments.json` + the catalog, builds each task's agent
from `prompt.md` + `agent_addendum`, runs it on the assigned model, and after
each step/phase verifies against the acceptance/phase criteria. On drift it
auto-corrects with bounded retries (a fixer agent) and pauses for the human only
when unresolved, updating each task's `status`.

## Testing

- phaseflow: assignment round-trip, catalog load + seed, `ValidatePlan`
  (missing criteria/agent/placeholder), approval marker.
- TUI: modal state transitions, gate blocking/allowing.

## Out of scope (Spec 2)

Task execution, multi-agent dispatch, verify-and-correct loops, `status`
transitions during a run.
