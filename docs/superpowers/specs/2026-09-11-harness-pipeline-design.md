# Contract-First Task Runner with Model Fallback and Live Pipeline View - Design

**Date:** 2026-09-11
**Status:** Approved (decomposition and architecture)
**Source requirements:** `harness-pipeline-feature-spec.md`, supplied by the user
**UI reference:** `pipeline_view.html`, a static mockup that pins the target layout and data shape

> Style note: plain hyphens, no em dashes, per the global rule for new documents.

## What this document adds to the source spec

The source spec says what to build and how to know it works. This one says how it
lands in THIS codebase: what already exists, what must change underneath, and the
order that keeps each piece shippable on its own.

## What already exists

`internal/phaseflow` is a real foundation, not a greenfield.

| Existing | Where |
|---|---|
| `Task{ID, Phase, Title, Description, AcceptanceCriteria, Agent, Model, Status}` | `assignments.go:19` |
| Status values `pending / running / done / failed / corrected` | `assignments.go:33` |
| `Assignments` persisted to `.planning/assignments.json` | `assignments.go:46-78` |
| `TaskRunner interface { Run(ctx, Task) (status, detail string, err error) }` | `execute.go:40` |
| Sequential execution with verify-and-correct rounds | `execute.go:44` |
| `/project-execute`, which runs every pending task unattended | `cmd/gophermind` |

`Task.AcceptanceCriteria` is the source spec's `test`. `Task.Description` is close
to its `deliverable`. So the model extends rather than replaces.

## Two facts that shape everything below

**1. `Assignments.Save` is not concurrency-safe.** It is `os.WriteFile` with no
lock and no atomic rename (`assignments.go:69-78`). That is fine today because
execution is strictly sequential. The moment two tasks in a wave finish at once,
one result silently overwrites the other. Parallel waves therefore require fixing
the store before they can be trusted, not afterward.

**2. `TaskRunner` is the right seam for model fallback.** Per-task candidate
models do not belong inside the execution engine. A `fallbackRunner` that wraps an
inner `TaskRunner` and tries candidates in order keeps the engine unchanged and
makes the fallback policy independently testable with a stub runner.

## Coordination store

The source spec says to reuse a "blackboard/shared-store pattern with atomic claim
semantics from the earlier recursive agent system". **No such component exists in
this repository.** It belongs to another project.

**Decision: extend `.planning/assignments.json` with the claim pattern this
codebase already proves**, in `internal/freellm/odometer.go`: an advisory `flock`
on unix, an mtime-based takeover on Windows, a temp-file-plus-`Sync`-plus-rename
write, and a merge that never lowers a value. That pattern survived a real
corruption incident in this repo on 2026-09-10 and has tests for the torn-write,
stale-lock and concurrent-writer cases.

Rejected alternatives, and why:

- **Porting the other project's blackboard.** Truest to the spec's intent, but it
  introduces a second coordination store alongside `assignments.json` unless it
  replaces it, and the fit cannot be judged without reading it. Revisit if the
  file-lock approach shows contention problems.
- **SQLite.** The strongest concurrency story and the best fit for a dashboard
  reading while workers write. Rejected for now because this project is pure
  standard library with JSON state, and a second persistence format is a large
  change to justify before parallelism has proven to need it.

## Data model

Extends `phaseflow.Task`. Every new field is additive with `omitempty`, so an
existing `assignments.json` loads unchanged and a plan written before this
continues to execute.

```go
type Task struct {
	// ...existing fields unchanged...

	Wave            int      `json:"wave,omitempty"`             // 0 means unassigned, computed from DependsOn
	DependsOn       []string `json:"depends_on,omitempty"`       // task ids
	CandidateModels []string `json:"candidate_models,omitempty"` // ordered; empty falls back to Model
	Attempts        []Attempt `json:"attempts,omitempty"`
	RevisionRounds  []Revision `json:"revision_rounds,omitempty"`
}

// Attempt is one model's try at a task, recorded whether it passed or failed.
type Attempt struct {
	Model     string    `json:"model"`
	StartedAt time.Time `json:"started_at"`
	Duration  string    `json:"duration"`
	Verdict   string    `json:"verdict"` // "pass" or "fail"
	Reason    string    `json:"reason"`  // what specifically failed, never just "failed"
}

// Revision is one round of the planner rewriting a task after every candidate
// model failed.
type Revision struct {
	Round      int       `json:"round"`
	At         time.Time `json:"at"`
	Note       string    `json:"note"`        // what changed and why
	Deliverable string   `json:"deliverable,omitempty"`
	Test       []string  `json:"test,omitempty"`
}
```

Two new status values join the existing five: `needs_revision` and `escalated`.

`Attempt.Reason` carrying a specific failure is load-bearing, not decoration. The
circuit breaker in piece 4 exists to notice that three models failed **the same
way**, and it cannot do that from a bare pass/fail.

## Delivery: five pieces, each shippable alone

### Piece 1: Task model, attempts history, and a safe store

Extends `Task` as above, adds the two status values, and makes `Assignments.Save`
concurrency-safe using the odometer's pattern. Nothing behavioural changes yet.
Useful alone because the unsafe `Save` is a latent bug the moment anything runs
in parallel.

### Piece 2: Sequential model fallback per task

A `fallbackRunner` wrapping `TaskRunner`: try each candidate in order, run the
task's test, record an `Attempt` either way, stop on the first pass. Exhausting
the list sets `needs_revision`, never an infinite retry.

Reuses this morning's `modelcat` work: `modelcat.Next` already picks a model under
user preferences and exclusions, and a task's `CandidateModels` defaults from it
when the field is empty. **A term the user excluded must not appear in any
candidate list**, for the same reason it cannot be auto-selected: that setting
exists for legal reasons.

Useful alone: a task that fails on one free provider and succeeds on the next is
the single biggest reliability win in the source spec.

### Piece 3: Waves and parallel execution

Wave 0 contract generation on the strongest available model, wave assignment
computed from `DependsOn`, and concurrent execution within a wave using the
claim semantics from piece 1.

A mid-wave contract flag **pauses the wave**, per the source spec's own
recommendation, and emits a distinguishable event. Pausing only the flagging task
lets its siblings keep building against a contract already known to be wrong;
pausing the whole run is heavier than the evidence warrants.

### Piece 4: Revision circuit breaker

On exhaustion, send the full attempt history to the planner for a revised task
definition, not a re-run. Cap at 2 rounds, then `escalated`. The revised task
re-enters piece 2's loop with a fresh attempt count.

### Piece 5: Live view and run summary

The dashboard from `pipeline_view.html`, wired to real state, plus the end-of-run
model performance summary. **The summary is a derived view over `Attempts`**, as
the source spec says, so it needs no new per-task fields.

**Transport: SSE.** `internal/serve` already streams SSE for session turns, the
frontend already parses SSE frames, and the model picker already emits a
`model-switched` event. Polling would be a second mechanism for the same job.

## Open questions from the source spec, answered

| Question | Answer |
|---|---|
| Static vs adaptive model ordering | Static first. `Attempts` records model, verdict, reason and duration per task, which is exactly the input adaptive ranking needs, so it comes without a migration. |
| How disruptive is a mid-wave contract flag | Pause the wave. The spec's own recommendation, and the reasoning above. |
| Live view transport | SSE, because the codebase already has it end to end. |

## Risks

- **Parallelism on an unsafe store.** Piece 1 must land before piece 3, and its
  concurrency test is the gate. This is called out because the natural temptation
  is to build waves first, since they are the visible feature.
- **Attempt history grows without bound** on a long run. Piece 1 should decide a
  retention rule rather than discovering the file is enormous later. The odometer's
  ring buffer is the precedent.
- **The dashboard is the most visible piece and the last one.** That ordering is
  deliberate: it is a view over state that must exist and be correct first. A
  dashboard built early would show invented data, which is the failure the source
  spec's own test for piece 5 forbids.
