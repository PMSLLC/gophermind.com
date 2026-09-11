# Pipeline Piece 5: Live View and Run Summary - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Watch a run while it happens, and get a model-by-model report when it ends.

**Architecture:** The summary is a pure aggregation over `Task.Attempts`, which already exist. The dashboard is served by `internal/serve` and fed by SSE, the transport the codebase already uses end to end.

**Spec:** `docs/superpowers/specs/2026-09-11-harness-pipeline-design.md` (piece 5), from source spec sections 4 and 5.
**UI reference:** `docs/design/pipeline_view_mockup.html`, a static mockup that pins the layout and the data shape. It is the target, not the implementation: its `const tasks = [...]` is demo data.

## Global Constraints

- Module is `gophermind`. Go 1.26.5. Standard library only on the Go side. No new npm dependency.
- NO em dashes and NO emoji in code, comments or UI strings. Plain hyphens.
- Every exported symbol gets a doc comment.
- No test may make a network call.
- `go build ./... && go test ./... -race` green before each commit.
- **Every pre-existing test must pass unedited**, including `internal/serve`'s and `desktop`'s pinned security tests.
- `git` is wrapped by a security shim; add `--gw-force` where refused.
- Commit messages end with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

---

### Task 1: The run summary

**Files:**
- Create: `internal/phaseflow/summary.go`
- Test: `internal/phaseflow/summary_test.go`

**Interfaces produced:**
```go
// ModelStat is one model's record across a whole run.
type ModelStat struct {
	Model        string
	Attempts     int
	Tasks        []string // task ids it was tried on
	Passes       int
	Fails        int
	PassRate     float64
	PassPositions []int  // for each pass, whether it was the 1st, 2nd, 3rd... model tried on that task
	AvgDuration  time.Duration
	FailReasons  []string // distinct, so "fails consistently on X" is visible
}

// RunReport rolls up every model that had at least one attempt, plus the
// tasks that needed a revision.
type RunReport struct {
	Models          []ModelStat
	RevisedTasks    []RevisedTask // id, rounds, notes
	GeneratedAt     time.Time
}

// BuildRunReport aggregates a run's tasks into a report. It is a pure
// function over the tasks' existing Attempts; it needs no new per-task state.
func BuildRunReport(tasks []Task, now time.Time) RunReport
```

**The three rules the source spec tests, and one trap:**

1. **Attribute wins AND losses to every model actually tried**, not just whichever ultimately succeeded. A task where model A failed and model B passed is one fail for A and one pass for B.
2. **Pass rate must match a hand count.** Keep the arithmetic obvious.
3. **Never conflate "never tried on this task" with "tried and failed".** A model absent from a task's attempts contributes nothing to that task, it does not contribute a failure. This is the trap: a naive implementation that iterates models-by-tasks and counts missing entries as failures would pass the other two tests and fail this one.

`PassPositions` answers "did it win first, or only after others failed", which is what makes the eventual adaptive-ordering idea possible without a migration.

- [ ] **Step 1: Write the failing tests**

- a run with one multi-attempt task attributes the fail to the first model and the pass to the second
- pass rate over a small hand-countable run matches the hand count exactly
- a model never tried on a task contributes nothing for it, neither pass nor fail
- `PassPositions` records 2 when a model won as the second model tried
- distinct failure reasons are collected, and duplicates collapse
- revised tasks are rolled up with their round count and notes
- an empty run produces an empty report, not a nil-map panic

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

### Task 2: Pipeline state and live events

**Files:**
- Create: `internal/serve/pipeline.go`
- Modify: `internal/serve/webhook.go` (register routes, extend `Deps`)
- Test: `internal/serve/pipeline_test.go`

Routes, behind the same `sessionWrap` auth as the rest:

```
GET /pipeline/state    the current tasks, waves, statuses and attempts
GET /pipeline/events   SSE: task and attempt changes as they happen
GET /pipeline/report   the run report once a run has finished
```

`/pipeline/events` emits, at minimum:
- `task-status` when a task changes status, carrying id, status, wave
- `task-attempt` when an attempt completes, carrying task id, model, duration, verdict, reason
- `wave-changed` when a wave starts or finishes
- `run-report` when a run ends

**Use `writeSSEEvent` (`internal/serve/sse.go`)** rather than writing frames by
hand; it already handles the newline escaping that makes multi-line data safe.

**The attempt stream comes from `FallbackRunner.OnAttempt`**, which piece 3
added specifically so attempts surface as they happen rather than after a task
finishes. That is what the source spec means by "a running log of attempts as
they happen".

- [ ] **Step 1: Write the failing tests**

- `/pipeline/state` returns tasks grouped by wave, with attempts, and requires the bearer token
- `/pipeline/events` emits a `task-status` frame when a status changes
- an attempt completing emits `task-attempt` with model, duration, verdict and reason populated, not placeholders
- frames arrive in the order the events occurred
- a client disconnecting mid-stream does not block the run or leak a goroutine

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

### Task 3: The dashboard

**Files:**
- Create: `internal/serve/assets/pipeline.html` (embedded with `go:embed`)
- Modify: `internal/serve/pipeline.go` (serve it at `GET /pipeline`)
- Test: extend `pipeline_test.go`

Adapt `docs/design/pipeline_view_mockup.html`: keep its layout, typography and
status vocabulary; replace its hardcoded `const tasks` with a fetch of
`/pipeline/state` plus an `EventSource` on `/pipeline/events`.

**What the source spec's tests require, restated so they are not lost:**
- opening it during an active run shows **real** state, not a replay of the mock
- a task moving `running` to `verified` or `needs_revision` updates its dot **without a page reload**
- attempt log lines appear in the order they actually occurred, each with a real model, duration and verdict

**Status vocabulary:** the mockup uses `queued / running / verified / needs_revision`.
The engine uses `pending / running / done / corrected / needs_revision / escalated / contract_flagged`.
Map them in ONE place, near the render, and show the engine's own name when
there is no mockup equivalent. Do not silently collapse `escalated` or
`contract_flagged` into something friendlier: those are the two states that most
need to stand out.

- [ ] **Step 1: Implement, then verify by running**

```bash
go build -o /tmp/gm-p5 ./cmd/gophermind
GOPHERMIND_SERVE_TOKEN=t GOPHERMIND_SERVE_ADDR=127.0.0.1:8096 nohup /tmp/gm-p5 serve >/tmp/p5.log 2>&1 &
sleep 3
curl -s -H 'Authorization: Bearer t' http://127.0.0.1:8096/pipeline/state | head -c 400
echo
curl -s -o /dev/null -w 'dashboard=%{http_code}\n' -H 'Authorization: Bearer t' http://127.0.0.1:8096/pipeline
pkill -f 'gm-p5 serve'; rm -f /tmp/gm-p5 /tmp/p5.log
```
Paste the real output.

- [ ] **Step 2: Commit**

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green; pre-existing tests unedited.
- [ ] A model that failed then a model that passed are attributed separately.
- [ ] "Never tried" is distinguishable from "tried and failed".
- [ ] The dashboard serves, and `/pipeline/state` returns real tasks.
- [ ] `escalated` and `contract_flagged` are visible as themselves, not collapsed.
- [ ] No em dashes on added lines.
