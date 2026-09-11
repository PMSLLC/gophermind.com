# Pipeline Piece 4: Revisit-Definition Circuit Breaker - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When every candidate model fails a task, revise the task definition rather than re-running it, cap the rounds, and escalate to a human instead of looping.

**Architecture:** A `Reviser` interface alongside `TaskRunner`, invoked only for `needs_revision` tasks. The revised definition is persisted through piece 1's locked `Update`, and the task re-enters piece 2's fallback loop with a fresh attempt count.

**Spec:** `docs/superpowers/specs/2026-09-11-harness-pipeline-design.md` (piece 4), from source spec section 3.

## Global Constraints

- Module is `gophermind`. Go 1.26.5. Standard library only. `go.mod`/`go.sum` byte-unchanged.
- NO em dashes and NO emoji on added lines. Plain hyphens.
- Every exported symbol gets a doc comment.
- No test may make a network call; a stub reviser exercises every case.
- `go build ./... && go test ./... -race` green before each commit.
- **Every pre-existing `internal/phaseflow/*_test.go` must pass unedited.**
- `git` is wrapped by a security shim; add `--gw-force` where refused.
- Commit messages end with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## The premise, and why the reason strings matter

The source spec's reasoning: when every model fails the same task, the likelier
explanation is a bad task spec than every model being incapable. Retrying the
same definition burns quota for the same failure.

That inference is only possible because piece 2 records a **specific reason**
per attempt, not a bare pass/fail. The reviser receives the whole history, and
the source spec's acceptance test is explicit: three failures sharing a root
cause must produce a revision that **addresses that pattern**, not a generic
"try again". A reviser prompt that discards the reasons cannot satisfy that.

---

### Task 1: The Reviser seam

**Files:**
- Create: `internal/phaseflow/revise.go`
- Test: `internal/phaseflow/revise_test.go`

**Interfaces produced:**
```go
// Reviser rewrites a task definition after every candidate model failed it.
// It receives the full attempt history so it can see what the failures had in
// common, which is the whole reason revision beats retrying.
type Reviser interface {
	Revise(ctx context.Context, t Task, attempts []Attempt) (Revision, error)
}

// MaxRevisionRounds bounds how many times a task's definition is rewritten
// before it escalates to a human. Two is the source spec's recommendation:
// enough for a genuinely under-specified task, few enough that a task nothing
// can satisfy stops burning quota.
const MaxRevisionRounds = 2

// ApplyRevision records a revision on a task and resets it for another pass:
// status back to pending, attempts cleared for a fresh count, revision
// appended to the history.
func ApplyRevision(t *Task, r Revision)
```

**Rules:**
- `ApplyRevision` clears `Attempts` so the next pass starts a fresh count, but
  **appends** to `RevisionRounds`, which is the durable record of what was tried.
  Losing that history would hide the pattern the next revision needs.
- A revision that changes nothing (empty deliverable and empty test) is an
  error. Recording a no-op revision would consume a round and teach nothing.
- At `MaxRevisionRounds`, the task becomes `escalated` and **no further model
  attempt is made automatically**, which is the source spec's own test.

- [ ] **Step 1: Write the failing tests**

- `ApplyRevision` sets status pending, clears attempts, appends the revision with an incrementing round number
- prior revisions survive a second revision (history accumulates)
- an empty revision is rejected
- a task at `MaxRevisionRounds` escalates rather than revising again
- an escalated task consumes no further model attempts (assert a stub runner's call count, as piece 2 did)

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

### Task 2: Wire revision into the run loop

**Files:**
- Modify: `internal/phaseflow/execute.go`
- Test: `internal/phaseflow/execute_revision_test.go` (new)

After a wave completes, any task left `needs_revision` is handed to the
`Reviser` (when one is configured), and on success re-enters the loop.

**Where revision happens matters.** Do it between waves, not mid-wave: a
revised task may change what its dependents expect, and revising while its
wave's siblings are still running reintroduces exactly the problem waves exist
to prevent. State in your report where you put it and why.

**A nil Reviser is not an error.** Existing callers that never revise must keep
working unchanged: a `needs_revision` task simply stays that way.

- [ ] **Step 1: Write the failing tests**

- a task exhausting its models is revised once and then succeeds: final status done, one entry in `RevisionRounds`, and the attempts from the successful pass present
- a task failing twice through revision ends `escalated`, with two revisions recorded and **no third model attempt** (call count again)
- a nil Reviser leaves the task `needs_revision` and does not panic
- a Reviser error leaves the task `needs_revision` rather than losing it

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

### Task 3: An LLM-backed reviser

**Files:**
- Create: `internal/orchestrate/reviser.go` (or wherever the existing runner lives; match it)
- Test: alongside, with a stubbed LLM client

A `Reviser` that prompts the model with the task and its full attempt history,
asking for a revised deliverable and test plus a short note on what changed and
why.

**The prompt must include every failure reason**, and must ask what the failures
have in common. That is the difference between the source spec's acceptance
criterion and a generic retry. Say in your report how the prompt does that.

Use the strongest configured model, as the contract step does: this is a
reasoning task about why work failed, not a coding task.

- [ ] **Step 1: Write the failing tests**

With a stubbed LLM: the prompt contains every attempt's model and reason; a
well-formed response parses into a `Revision`; a malformed response is an error
rather than a silently empty revision.

- [ ] **Step 2-4:** fail, implement, pass.
- [ ] **Step 5: Commit**

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green; pre-existing tests unedited.
- [ ] A revised task re-enters the fallback loop with a fresh attempt count and its revision history intact.
- [ ] Two failed revisions escalate, and an escalated task consumes no further attempts (proven by call count).
- [ ] The reviser prompt carries every failure reason.
- [ ] A nil Reviser changes nothing for existing callers.
- [ ] No em dashes on added lines.
