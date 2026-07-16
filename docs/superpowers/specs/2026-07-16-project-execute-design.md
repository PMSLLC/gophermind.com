# `/project-execute` — Autonomous Per-Task Executor (Spec 2)

**Date:** 2026-07-16
**Status:** Approved (fast-tracked) — building
**Branch:** `feat/project-execute` (off `main`)

## Summary

Add the executor that gophermind's `/project` planning was built for: `/project-execute`
runs every pending task in `.planning/assignments.json` autonomously — each task in a
**fresh, isolated agent context** with its assigned model + catalog prompt, verified
against its acceptance criteria, its status persisted back — clearing context between
tasks. Failed tasks are recorded and the run continues to a final summary.

## Locked decisions

| Decision | Choice |
|---|---|
| Trigger | TUI `/project-execute` (Approved-gated). No CLI in v1. |
| Scope | Whole plan — all `pending` tasks, every phase, in plan-id (NN-MM) order. |
| Autonomy | Fully autonomous: on unrecoverable task failure, mark `failed` and continue. |
| Isolation | Fresh `agent.New(...)` per task (clean conversation) → context cleared between tasks by construction. |
| Per-task prompt | catalog agent `Body` + `AgentAddendum` + a task context block as the system prompt; the task `Description`+`AcceptanceCriteria` as the user turn. |
| Model | Resolve task `Model` tier (`speed`/`strong`) → configured concrete model; concrete names pass through. |
| Verify | Reuse `agent.SendWithVerification` — fresh verifier judges output vs `AcceptanceCriteria`; one correction round. |
| Approval | Task agents run in **auto** approval mode (unattended); logged in the run header. |
| Status | Advance `pending→running→done|corrected|failed`; `Assignments.Save()` after each task. |
| Abort | Ctrl-C cancels the run's context; in-flight task ends, no further tasks start. |

## Architecture (3 layers, testable)

### 1. Executor core — `internal/phaseflow/execute.go` (pure orchestration, unit-tested)
```go
type TaskOutcome struct { ID string; Status string; Detail string }
type RunSummary struct { Done, Corrected, Failed int; Outcomes []TaskOutcome }

// TaskRunner runs ONE task to a terminal status. Injected so the core is testable
// without real LLM calls. The real impl (layer 2) creates a fresh agent + verify.
type TaskRunner interface {
    Run(ctx context.Context, t Task) (status string, detail string, err error)
}

// Execute loads assignments, iterates pending tasks in id order, runs each via
// runner, persists status after each, emits progress, returns a summary.
func Execute(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome)) (RunSummary, error)
```
- Sort pending tasks by `ID` (string NN-MM order). Skip non-`pending` (already done).
- Per task: set `running` + Save; call `runner.Run`; on ctx-cancel → stop the loop (leave `running`→ revert to `pending` or mark `aborted`? — revert to `pending` so a re-run retries it); set returned status (`done`/`corrected`/`failed`) + Save; `emit` progress.
- Never abort the whole run on a task error — record `failed`, continue.
- Return counts + per-task outcomes.

### 2. Real runner — `internal/agent` (or `internal/phaseflow`), bridges to a fresh agent
```go
// buildAgent: fresh agent.New with the shared llm client + tool registry, model set
// to the resolved concrete model, approval mode "auto", system prompt assembled from
// the catalog agent body + AgentAddendum + a phaseflow context block.
// Run: agent.SendWithVerification(ctx, userPrompt, acceptanceCriteria) → satisfied?done:corrected/failed.
```
- Model tier resolution: `speed`→`cfg.SpeedModel`, `strong`→`cfg.Model` (mirror `routeModel`, main.go:952); a concrete name passes through.
- Prompt assembly: reuse `Engine.contextBlock`/`BuildStepPrompt` patterns (engine.go) for the `<phaseflow-context>` preamble; load the catalog agent via `LoadCatalog`/`CatalogAgent.Body`.
- Emits the agent's events (prefixed per task) so the TUI can show live tool activity.

### 3. TUI wiring — `internal/tui/` (`/project-execute` command)
- Register `/project-execute` in the command registry (Task-6 registry). Arg: none. Desc: "autonomously execute the approved project plan".
- Handler (in `commands.go`/`update.go`): require `Engine.Approved()` (else print the gate message, like `/phase`). Then launch the executor on a goroutine (mirror `/project`'s `startTurn`, project.go:96-115) that streams progress to `m.sub`: a header line ("executing N tasks, auto-approve"), one line per task as it completes (`✓ 02-01 done` / `✓ 02-02 corrected` / `✗ 02-03 failed: <detail>`), and a final summary (`run complete: X done, Y corrected, Z failed`). Ctrl-C cancels the run context.
- The run is a normal `stateWorking` turn; results append to the transcript.

## Testing
- **Core** (`execute_test.go`): fake `TaskRunner` returning scripted statuses (incl. a `failed`) → assert iteration order, status persistence to `assignments.json` after each, continue-on-failure, correct summary counts, ctx-cancel stops early and leaves the unrun tasks `pending`. Use `t.TempDir()` + a hand-written `.planning/assignments.json`.
- **Runner** (`_test.go`): model-tier resolution; prompt assembly includes catalog body + addendum + criteria; verify path maps satisfied→done, corrected→corrected, unsatisfied-after-correction→failed. Fake the agent turn/verifier.
- **TUI**: `/project-execute` gated on approval (blocked when not approved); registered in `commandNames()`/help; a scripted core produces the expected transcript summary (teatest or handleSubmit-level).
- Full gate: `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l`.

## Out of scope (v1, YAGNI)
- CLI `gophermind phase execute` runner (TUI only for now).
- Task dependency graph / parallelism (sequential by id; no `Deps` field exists).
- A build/test gate beyond the LLM verifier (acceptance judged by `SendWithVerification`; a real `go build`/test gate is a strong follow-up).
- Resuming/mid-run human gates (fully autonomous per the decision).
- Per-task cost/budget caps beyond existing agent guardrails.

## Rollout (implementation tasks)
1. **E1** — executor core (`execute.go`) + tests (fake runner).
2. **E2** — real `TaskRunner` (fresh agent + model tier + catalog prompt + `SendWithVerification`) + tests.
3. **E3** — TUI `/project-execute` command (registry + gated handler + goroutine progress + Ctrl-C) + tests.
4. **E4** — README/CHANGELOG.
