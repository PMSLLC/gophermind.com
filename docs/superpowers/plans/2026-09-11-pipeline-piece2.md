# Pipeline Piece 2: Sequential Model Fallback Per Task - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Try a task's candidate models in order, verify after each, record every attempt with a specific reason, and stop at `needs_revision` rather than retrying forever.

**Architecture:** A `FallbackRunner` wrapping the existing `TaskRunner` interface. The execution engine is untouched; the policy lives in the wrapper and is testable with a stub runner.

**Spec:** `docs/superpowers/specs/2026-09-11-harness-pipeline-design.md` (piece 2), from `...-source-spec.md` section 2.

## Global Constraints

- Module is `gophermind`. Go 1.26.5. Standard library only. `go.mod`/`go.sum` byte-unchanged.
- NO em dashes and NO emoji on added lines. Plain hyphens.
- Every exported symbol gets a doc comment.
- No test may make a network call. The stub runner is how every case is exercised.
- `go build ./... && go test ./... -race` green before each commit.
- Commit messages end with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## The interaction that must not be got wrong

`ExecuteWithRounds` already retries. `resetFailedToPending` (`execute.go:125`)
requeues **every** task whose status is `StatusFailed` for the next round, up to
`DefaultMaxRounds` (3).

A task that exhausted its candidate models is `needs_revision`, not `failed`.
If rounds treated it as an ordinary failure it would re-run the same task
against the same exhausted list, burning free-tier quota for an identical
result. That is exactly the waste section 3 of the source spec exists to stop.

**So: `resetFailedToPending` must requeue `StatusFailed` only, never
`needs_revision` or `escalated`.** There is a test for it below. Piece 4 is what
moves a `needs_revision` task forward, by revising its definition first.

---

### Task 1: The fallback runner

**Files:**
- Create: `internal/phaseflow/fallback.go`
- Test: `internal/phaseflow/fallback_test.go`

**Interfaces produced:**
```go
// Verifier decides whether a runner's output satisfies the task. It returns a
// specific reason on failure, never a bare boolean: the circuit breaker in
// piece 4 exists to notice that several models failed the SAME way, and it
// cannot do that without one.
type Verifier interface {
	Verify(ctx context.Context, t Task, detail string) (ok bool, reason string)
}

// FallbackRunner tries a task's candidate models in order, verifying after
// each, and records every attempt whether it passed or failed.
type FallbackRunner struct {
	Inner    TaskRunner
	Verify   Verifier
	Now      func() time.Time // injectable so durations are deterministic in tests
	Candidates func(t Task) []string // defaults to t.CandidateModels, then t.Model
}

func (f *FallbackRunner) Run(ctx context.Context, t Task) (status, detail string, err error)
```

**Behaviour, in order:**

1. Resolve the candidate list. `t.CandidateModels` when non-empty, else a single-element list of `t.Model`. An empty result is a task-level error, not a silent no-op.
2. For each candidate: run `Inner` with the task's `Model` set to that candidate, then `Verify`.
3. Record an `Attempt` for every candidate tried, pass or fail, with model, start time, duration, verdict and reason.
4. First pass wins: return `StatusDone`, and **only that attempt's detail is the deliverable**. Earlier failed output is never returned.
5. All candidates fail: return `StatusNeedsRevision`, with a detail summarising the distinct failure reasons.
6. Context cancellation is not a failure: return the wrapped error immediately without recording a further attempt, so the engine's existing cancel path still works.

- [ ] **Step 1: Write the failing tests**

Create `internal/phaseflow/fallback_test.go` with a stub runner and stub
verifier. Cover:

- first candidate passes: status done, exactly one attempt recorded, detail is that model's output
- first fails, second passes: status done, **two** attempts in order, detail is the SECOND model's output and never the first's
- all candidates fail: status `needs_revision`, one attempt per candidate, no infinite loop
- every attempt carries a non-empty `Reason` on failure (the property piece 4 depends on)
- attempts are recorded in the order tried, with the model name on each
- an empty candidate list is an error rather than a silent pass
- `t.Model` is used when `CandidateModels` is empty (backward compatibility with existing plans)
- context cancelled mid-list: returns promptly, does not try the remaining candidates
- durations come from the injected clock, so the test is deterministic

- [ ] **Step 2: Run to verify it fails**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run the tests**
- [ ] **Step 5: Commit**

---

### Task 2: Keep rounds away from exhausted tasks

**Files:**
- Modify: `internal/phaseflow/execute.go` (`resetFailedToPending`, and the status handling that treats unknown statuses as failed)
- Test: `internal/phaseflow/execute_needsrevision_test.go` (new)

`Execute`'s contract says a runner status other than done/corrected/failed is
**treated as** `StatusFailed`. `StatusNeedsRevision` must not be swallowed by
that rule, or a task that exhausted its models is immediately requeued.

- [ ] **Step 1: Write the failing tests**

- a runner returning `StatusNeedsRevision` leaves the task `needs_revision` on disk, not `failed`
- `resetFailedToPending` requeues a `failed` task and leaves a `needs_revision` one alone
- across rounds, a `needs_revision` task is attempted exactly once, not `DefaultMaxRounds` times (this is the quota-burning case, and the test should assert the runner's call count)
- an `escalated` task is likewise never requeued

- [ ] **Step 2: Run to verify it fails**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run everything.** Every pre-existing `execute_*_test.go` must pass unedited; they pin the current round behaviour and this change must not alter it for ordinary failures.
- [ ] **Step 5: Commit**

---

### Task 3: Persist attempts, and default candidates from the model picker

**Files:**
- Modify: `internal/phaseflow/execute.go` (persist attempts via `Update`)
- Modify: wherever `/project-execute` builds its runner, to wrap it in `FallbackRunner`
- Test: extend the tests above

Two things:

**Persist attempts through `Update`, never `Save`.** Piece 1 made both safe, but
`Update` is the read-modify-write primitive, and attempts must accumulate rather
than overwrite.

**Default `CandidateModels` from the model picker.** When a task has none,
derive them from `modelcat`: reachable entries, in the user's preference order,
excluding anything whose terms the user excluded. **A term the user excluded
must never appear in a candidate list**, for exactly the reason it can never be
auto-selected: that setting exists for legal reasons, and a task quietly
falling back onto an excluded provider would defeat it.

Keep this wiring thin. If it needs more than a small adapter, say so in your
report rather than growing the runner.

- [ ] **Step 1: Write the failing tests**

- attempts survive a reload from disk, in order
- a task whose candidates would include an excluded-term provider does not get it
- an existing plan with only `Model` set still runs (no regression)

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green, with every pre-existing `execute_*_test.go` passing unedited.
- [ ] A task whose first model fails and second succeeds ends `done`, with both attempts visible and only the second's output kept.
- [ ] A task with an exhausted model list ends `needs_revision` and is attempted **once**, not once per round.
- [ ] Every failed attempt carries a specific reason.
- [ ] An excluded term never appears in a candidate list.
- [ ] No em dashes on added lines.
