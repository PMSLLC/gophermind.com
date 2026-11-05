# Resume a failed run from a chosen step

Date: 2026-11-05. Requested by John ("I would also like to be able to resume a failed run from a specific step"), questions skipped, so the choices below are mine and marked as such.

## Intent

A run that fails or escalates late has done expensive work before the failure. On the local model, hello's planning takes ten minutes and is right; r003 then failed at one node. Today the only way forward is "Resubmit", which replans everything in a new run. John wants to fix the harness or the brief, then re-enter the same run at the step that failed, keeping everything before it.

## Goals

- From the run page and the CLI, re-enter a finished run (failed, escalated, stopped, interrupted, harness fault) at a chosen step.
- A step is either a planner stage (`clarify`, `confirm`, `contract`, `decompose`, `enrich`, `coverage`, `approve`, `testwriter`) or one executor node (`node:fn-run`).
- Everything before the step is kept: planner state files, the blackboard rows and commits of verified nodes.
- Nothing is deleted. State the resume discards is moved to `_state/discarded/<UTC timestamp>/` in the run directory.
- The run keeps its id, its directory, its repository and its log; the run page shows the resumed run as one history.

## Non-goals

- Rerunning a verified node (its dependents are verified or blocked by nothing; undoing their commits is not in this round). A verified node is not offered as a step.
- Editing the brief in place. A changed brief is a new library version and a new run.
- Resuming an active run. The existing stop button, then resume.

## Anti-goals

- Resetting a user's own repository without being told. The CLI refuses a planner-stage resume that would discard executor work unless `--reset-repo` is given; the local service may reset its own per-run clone.
- Silent partial state. The discarded set is listed in the run's events and left on disk.

## How it works

### Steps and what each discards

| Step | Discards (moved aside) | Keeps |
| --- | --- | --- |
| planner stage S | the completion markers of S and every later stage (the files the stage's `done` check reads), `_state/planner_error.json`, `report.json`, and all executor state (`_state/executor.json`, blackboard rows, `_state/leaf-results.json`, `_state/notes.json`, `_state/escalations.json`, worker trees) | every earlier stage's files, `brief.md`, the run record, `run.log`, `_state/llm/` transcripts |
| `node:<id>` | the node's blackboard row (set back to ready, attempts moved aside), its entry in `_state/leaf-results.json`, `report.json` | every other row, the planner state, `_state/executor.json`, the work branch and its commits |

A `node:<id>` step is accepted only when the row is failed, escalated or needs_revision. Its dependents were blocked by it, so none is verified and nothing else moves. The executor's normal resume then schedules the node again as if it were fresh, and the ladder starts from attempt 1 with the installed code.

A planner-stage resume with executor state behind it must also return the repository to the base: for the local service, the run's clone under the briefs directory is reset to the base branch and cleaned (guarded: the path must be inside the service's repos directory). For the CLI, `--reset-repo` deletes the run's work branch only when every commit on it is the run's own, and restores the files the plan names; without the flag the command refuses and says so.

### CLI

`gophermind brief resume <run-id> --from <step>` does the discard, then continues as `brief resume` does today: the planner loop runs the stages whose markers are gone and exits 0 when planned. For `node:<id>` there is nothing to plan and the command exits 0 after the reset. In both cases the caller then runs `gophermind brief run <run-id> --repo <path>` as it would for a fresh plan; the executor's own resume path handles the rest.

`--from` is refused with a message when: the run is active (a live blackboard claim), the step name is unknown, the stage was never reached (nothing to discard and nothing before it), or the node is verified, blocked or never run.

### Service

`POST /app/briefs/{id}/resume?from=<step>` returns 202 `{run_id, url}` (the same id), 409 with `active_run_id` when a run is active, 400 with the CLI's refusal text, 404 for an unknown run. It runs `brief resume <id> --yes --gate file --from <step>` as step one and `brief run` as step two, through the same two-step runner a new run uses, so the phases, the log and the restart handling are unchanged. The server record gains `resumed_at` and `resumed_from` (last values) for the list and the run page.

`GET /app/briefs/{id}/state` gains `resume_points`: the steps the server would accept right now, each `{step, label}`, in pipeline order, computed from the run directory (stages whose marker exists, then nodes whose row is failed, escalated or needs_revision). Empty while the run is active or when nothing has run.

### Run page

Beside "Resubmit this brief", when `resume_points` is not empty: a select listing them and a button "Resume from this step". On 202 the page reloads the same run. Errors go to the existing message span.

## Constraints

- No em dashes, no emojis.
- The planner's stage order and `done` checks stay the source of truth; the discard list for a stage is derived from them in one table in the planner package, not duplicated in the serve layer.
- Every recursive delete or reset is guarded: assign the path, check it is absolute and inside the intended directory, record it in the events, then act.
- Subagents never touch `~/.gophermind`. Tests use temp run directories and the existing fixtures.

## Left to the builder

- The exact names of the stage marker files per stage (read them from each stage's `done` function).
- Whether the blackboard needs a new `Reset` method or whether an existing write path can set a row to ready; a new method is fine.
- The select's labels (stage names as they are; nodes as `node:<id>` with the node title).
