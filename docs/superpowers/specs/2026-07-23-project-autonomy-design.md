# `/project` autonomy: one-question interview, PROJECT.md spec block, TDD tasks, retry rounds

**Date:** 2026-07-23
**Status:** approved

## Goal

Make `/project` produce a plan that gophermind can execute unattended to completion:
interview the user one question at a time, record the resulting spec and phases in a
root `PROJECT.md` the agent can refer back to, require tests in every task, and keep
retrying failed tasks in bounded rounds instead of stopping after one pass.

## Background

Today `/project <name>` scaffolds `.planning/`, runs a free-form LLM interview whose
prompt asks for "focused clarifying questions a FEW at a time", waits for a
`[[SPEC-READY]]` sentinel, then generates `.planning/SPEC.md`, `ROADMAP.md`, and
`assignments.json`. `/project-execute` runs pending tasks in ID order with a per-task
verify-and-correct pass (`StatusCorrected`), persisting status as it goes.

Four gaps: the interview batches questions and depends on a weak local model
remembering a sentinel token; the spec lives only under `.planning/`; nothing requires
tests; and a task that fails its correction pass stays failed and the run ends.

## Design

### 1. Structured single-question interview

Each interview round requests one JSON object from the model:

```json
{"question": "...", "why": "...", "done": false}
```

gophermind renders only `question`, collects the answer, appends the Q/A pair to a
running transcript held in model state, and asks for the next. `{"done": true}` ends
the interview and triggers generation, retiring the `[[SPEC-READY]]` sentinel.

Weak-model handling, in order: extract the first balanced `{…}` object from the reply
(models wrap JSON in prose); on parse failure retry once; if that also fails, surface
the raw reply and accept an answer anyway rather than dead-ending the flow.

The accumulated transcript is what feeds generation, so answers are never lost to a
context trim mid-interview.

### 2. PROJECT.md managed block

`.planning/SPEC.md` and `ROADMAP.md` remain the source of truth. `PROJECT.md` at the
repo root carries a rendered view between markers:

```
<!-- gophermind:spec:begin -->
...spec summary, phases, task table with live status...
<!-- gophermind:spec:end -->
```

Upsert semantics:

- markers present -> replace only what is between them
- file exists without markers -> append the block, leaving existing content untouched
- file absent -> create it containing the block

This preserves the file's existing role as hand-written project conventions (read at
session start) while giving the agent one root file holding spec plus phases. The
block is regenerated after plan generation and after each execute run, so task
statuses stay current.

### 3. TDD in every task

`generationPrompt` requires each generated task to write its failing test first and
carry at least one test-shaped acceptance criterion. `validate.go` enforces the
criterion so a non-compliant plan fails review instead of executing.

The executor gates each task on the project's test command passing: no green, no
`done`. The command is captured once per project (see below) rather than guessed from
repo layout.

### 4. Test command in phaseflow config

`Config` gains `TestCommand string`. It is asked as a required interview question,
saved to `.planning/config`, and run by the executor as the per-task gate. An empty
value means no gate (the task's own verification stands), so existing projects without
the field keep working.

### 5. Bounded retry rounds

`Execute` gains rounds. After a full pass, failed tasks are re-attempted with their
failure detail fed back into the task context. The loop stops when:

- no tasks failed, or
- a round fixed nothing (no progress), or
- `maxRounds` is reached (default 3).

The no-progress stop mirrors the stuck-loop guard in `internal/agent/loop.go`: bounded
autonomy, never an unbounded spin. Existing resumability (running -> pending recovery)
and cancellation semantics are preserved: a cancelled round stops immediately and
leaves later tasks pending.

## Testing

- JSON extraction: bare object, object wrapped in prose, malformed, retry path.
- Interview state machine: one question rendered per round, transcript accumulation,
  `done` ends the interview.
- PROJECT.md upsert: create, replace-between-markers, append-when-markers-absent,
  idempotent regeneration, and non-clobbering of surrounding content.
- Validation: task without a test-shaped acceptance criterion fails.
- Retry rounds: failures retried, no-progress round stops the loop, `maxRounds`
  respected, cancellation still stops immediately.

## Non-goals

- No change to the approval gate before execution.
- No changes to the agent catalog or per-task model assignment.
- No TUI redesign beyond rendering one question at a time.
- Not stripping leaked model scaffolding from output (separate concern; see the
  stuck-loop guard).
