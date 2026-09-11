# Pipeline Piece 3: Waves and Parallel Execution - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Assign every task to a wave from its dependencies, run a wave's tasks concurrently, run waves in sequence, and let a task flag a broken contract without improvising around it.

**Architecture:** Wave assignment is a pure function over `[]Task`. Concurrency is added inside `executeOnce`, with every status mutation routed through piece 1's locked `Update`.

**Spec:** `docs/superpowers/specs/2026-09-11-harness-pipeline-design.md` (piece 3), from source spec section 1.

## Global Constraints

- Module is `gophermind`. Go 1.26.5. Standard library only. `go.mod`/`go.sum` byte-unchanged.
- NO em dashes and NO emoji on added lines. Plain hyphens.
- Every exported symbol gets a doc comment.
- No test may make a network call.
- `go build ./... && go test ./... -race` green before each commit. **The race detector is not optional in this piece**; it is the point.
- **Every pre-existing `internal/phaseflow/execute_*_test.go` must pass unedited.**
- `git` is wrapped by a security shim; add `--gw-force` where it refuses.
- Commit messages end with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

---

### Task 1: Wave assignment

**Files:**
- Create: `internal/phaseflow/waves.go`
- Test: `internal/phaseflow/waves_test.go`

**Interfaces produced:**
```go
// AssignWaves computes each task's wave from its DependsOn edges: a task's
// wave is one more than the highest wave among its dependencies, and a task
// with no dependencies is wave 1. Wave 0 is reserved for the contract step.
func AssignWaves(tasks []Task) ([]Task, error)

// ErrCyclicDependency reports a dependency cycle, naming the tasks involved.
var ErrCyclicDependency = errors.New("phaseflow: cyclic task dependency")
```

**Rules:**
- Wave 0 is the contract task, if one exists. It is identified by having no dependencies AND being explicitly marked; see Task 3. Ordinary dependency-free tasks start at wave 1.
- A dependency on an unknown task id is an error, not a silently dropped edge. A typo in a plan must not quietly turn a dependent task into a wave-1 task that runs before its input exists.
- A cycle is an error naming the participating ids.
- The function is pure: same input, same output, no I/O.

- [ ] **Step 1: Write the failing tests**

Cover: a linear chain gets increasing waves; two independent tasks share a wave; a diamond (D depends on B and C, both on A) puts D one past the later of B and C; an unknown dependency id errors; a cycle errors and names the ids; an empty list is not an error; **the source spec's own acceptance test** - no task references another task's output unless that task is in an earlier wave, asserted over a generated plan.

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

### Task 2: Run a wave concurrently

**Files:**
- Modify: `internal/phaseflow/execute.go`
- Test: `internal/phaseflow/execute_parallel_test.go` (new)

`executeOnce` currently walks `a.Tasks` in ID order, running one at a time.
Change it to: group pending tasks by wave, then for each wave in ascending
order, run its tasks concurrently and wait for all of them before starting the
next wave.

**Every status mutation goes through `Update`.** Piece 1 made `Save` safe too,
but attempts and statuses must accumulate under one lock rather than overwrite
a whole file built from a stale read.

**Bound the concurrency.** An unbounded goroutine per task would fan out a
50-task wave onto 50 simultaneous free-tier calls and trip every rate limit at
once. Default to a small limit (4) exposed as a parameter, and say in your
report why you chose what you chose.

**Cancellation must still work.** The existing contract is that a cancelled
context reverts the in-flight task to pending and stops the loop. With several
in flight, all of them revert, and no further wave starts.

- [ ] **Step 1: Write the failing tests**

The one that matters, and the source spec names it: **two independent tasks in
the same wave actually execute concurrently, verified by overlap rather than by
being allowed to.** Have the stub runner record start and end times and assert
two tasks' intervals overlap. A test that only asserts both ran would pass on a
sequential implementation.

Also cover: a wave-2 task does not start until every wave-1 task has finished;
a failure in wave 1 does not prevent other wave-1 tasks from completing; the
concurrency limit is respected (record max simultaneous in-flight); cancellation
mid-wave reverts all in-flight tasks to pending and starts no later wave.

- [ ] **Step 2: Run to verify it fails.** The overlap test must fail against the
  current sequential code. Confirm that and report it: it is the evidence the
  test measures concurrency rather than permission.

- [ ] **Step 3: Implement**
- [ ] **Step 4: Run everything, with `-race`**
- [ ] **Step 5: Commit**

---

### Task 3: Wave 0 contract, and the contract flag

**Files:**
- Modify: `internal/phaseflow/assignments.go` (a `Task.IsContract` marker), `internal/phaseflow/execute.go`
- Test: extend the above

**Wave 0** runs alone, before everything, on the strongest available model. Its
output is the contract every later task must conform to.

**The contract flag.** A task that determines mid-execution that the contract is
wrong does not improvise around it. It returns a distinguishable status,
`StatusContractFlagged`, and:

- the **current wave pauses**: no new task in that wave starts, and in-flight
  tasks are allowed to finish rather than being killed mid-write
- a distinguishable event is emitted, **never swallowed and never silently
  worked around**, which is the source spec's own acceptance criterion
- no later wave starts

Pausing only the flagging task would let its siblings keep building against a
contract already known to be wrong. Pausing the whole run immediately is
heavier than the evidence warrants, and loses work already in flight.

- [ ] **Step 1: Write the failing tests**

- a contract flag in wave 2 stops wave 3 from starting
- the flag produces a distinct outcome event, not a generic failure
- tasks already in flight in the flagging wave are allowed to finish
- a run with no flag is unaffected

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green; every pre-existing `execute_*_test.go` unedited.
- [ ] Two same-wave tasks provably overlap in time.
- [ ] A later wave never starts before its predecessor finishes.
- [ ] A dependency on an unknown id is an error, not a dropped edge.
- [ ] A contract flag pauses the wave and emits a distinguishable event.
- [ ] Concurrency is bounded.
- [ ] No em dashes on added lines.
