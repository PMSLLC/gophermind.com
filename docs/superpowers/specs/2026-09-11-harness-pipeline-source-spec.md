# Feature Spec: Contract-First Task Runner with Model Fallback + Live Pipeline View

## Context

The harness currently takes a product request and needs to run it to completion unattended, using a rotating set of free LLM providers. This spec covers three additions on top of that base loop:

1. Wave-based task decomposition, so parallelism is based on real dependencies instead of running everything at once.
2. Sequential model fallback per task, with a capped "revisit the task definition" escape hatch.
3. A live view of all of the above, so a run can be watched while it happens instead of just read back from logs after.

Each piece below is written as: what it does, why, and how to know it's built correctly. Definition of done = deliverable + test, same convention the harness itself uses for its own tasks.

---

## 1. Contract-first decomposition into waves

**Problem it solves:** naive "run all tasks in parallel" breaks the moment one task's output is another task's input (e.g. a frontend component consuming an API that doesn't exist yet).

**Deliverable:**
- After the plan is approved, an explicit **Wave 0: contract generation** step runs first, alone, using the strongest available model. Its output is a single artifact (schemas, interface signatures, file/module layout, naming conventions) that every later task must conform to.
- The planner then assigns every task to a wave number based on its dependencies on that contract and on other tasks' outputs. Tasks with no unresolved dependency go in the earliest possible wave.
- Tasks within a wave execute in parallel. Waves execute in sequence.
- If a task, mid-execution, determines the contract is wrong or incomplete, it does **not** improvise around it — it raises a flag back to the planner, which can revise the contract (this pauses the current wave; see open question below on how disruptive this should be allowed to be).

**Test:**
- Given a sample product request, the planner's task list has a `wave` field on every task, and no task references another task's output unless that task is in an earlier wave.
- Two independent tasks in the same wave actually execute concurrently (verify via timestamps/logs, not just that they're allowed to).
- A task that flags a contract problem produces a distinguishable event in the log (not silently swallowed, not silently worked around).

---

## 2. Sequential model fallback per task

**Problem it solves:** free LLMs vary a lot in reliability. Fanning a task out to multiple models in parallel every time wastes quota; picking one model and giving up on failure wastes the whole point of having several providers available.

**Deliverable:**
- Each task carries an ordered list of candidate models to try (default order can be static to start; see open question below on adaptive ordering).
- On execution: try model 1 → run the task's test against its output → if pass, mark task verified and stop. If fail, log the failure reason (not just pass/fail — what specifically failed) and try model 2, and so on.
- If **all** candidates in the list fail, do not retry indefinitely. Stop and enter the "needs revision" state (see #3).
- Every attempt (model, duration, verdict, failure reason if failed) is recorded as part of the task's history, in order.

**Test:**
- A task whose first candidate model fails and second succeeds ends in `verified` state, with both attempts visible in history, and only the second attempt's output persisted as the actual deliverable.
- A task where the model list is exhausted transitions to `needs_revision`, not to an infinite retry loop.
- Attempt history is queryable per task (used by both the revision step and the live view).

---

## 3. Revisit-definition circuit breaker

**Problem it solves:** when every candidate model fails the same task, the more likely explanation is often a bad task spec (test too strict, deliverable under-specified) rather than every model being incapable. Retrying with the same task definition just burns more quota for the same failure.

**Deliverable:**
- When a task exhausts its model list, the harness sends the task's full failure history (all attempts, all failure reasons) back to the planner with a request to revise the task definition — not just re-run it as-is.
- Planner produces a revised task definition (deliverable and/or test) and a short note on what changed and why.
- The revised task re-enters the model fallback loop from #2 with a fresh attempt count.
- Cap the number of revision rounds (recommend 2). If it still fails after the cap, escalate to a human checkpoint rather than looping further or silently giving up.

**Test:**
- A task with 3 failures that share a common root cause (e.g. all three miss the same edge case) produces a revision that specifically addresses that pattern, not a generic "try again" prompt.
- A task that fails revision twice stops and surfaces a clear "needs human input" state, and does not consume further model attempts automatically.

---

## 4. Live pipeline view

**Problem it solves:** an unattended multi-hour run across many free models is opaque without something better than tailing a log file.

**Deliverable:** a dashboard with two coordinated views, matching the attached mockup (`pipeline_view.html`):
- **Wave board** (left): every task, grouped by wave, each showing a status indicator — queued / running / verified / needs_revision — that updates live as state changes.
- **Task detail / attempt log** (right): for whichever task is selected, show its deliverable and test definition, then a running log of attempts as they happen — model name, duration, verdict, and failure reason on completion. When a task exhausts its model list, show the revision note inline once the planner produces it.
- Clicking any task pill in the wave board loads that task's live or historical attempt log into the detail panel.

**Test:**
- Opening the dashboard during an active run shows real task/wave/attempt state, not a static or replayed mock.
- A task transitioning from `running` → `verified` (or → `needs_revision`) updates the wave board status dot without a page reload.
- Attempt log entries appear in the order they actually occurred, each with model, duration, and verdict populated from real execution data (not placeholder text).

---

## 5. End-of-run model performance summary

**Problem it solves:** the whole point of routing across a myriad of free providers is being able to compare them, but right now attempt history lives per-task with nothing rolling it up across a run. Without this, "how did each model do" only exists if you read every task's log by hand.

**Deliverable:**
- On run completion (success or escalation), generate a summary covering every model that had at least one attempt in the run:
  - total attempts, and which tasks it was tried on
  - pass count, fail count, pass rate
  - for its passes: whether it was the 1st/2nd/3rd+ model tried on that task
  - average attempt duration
  - failure reasons it produced, so "fails consistently on X" is distinguishable from a one-off flaky failure
- Also roll up which tasks needed a definition revision and how many rounds, since that's a property of the run, not of any single model.
- Render it as a final "Run Report" in the dashboard, and also emit it as structured data (not just prose), since it's the natural input to the adaptive-ordering idea from the open questions below.

**Test:**
- Given a run with at least one multi-attempt task, the summary correctly attributes wins and losses to every model actually tried on a task, not just whichever one ultimately succeeded.
- A model's pass rate in the summary matches a hand-count of that model's attempts in the raw log for a small test run.
- The summary distinguishes "never tried on this task" from "tried and failed" — no conflating absence of data with failure.

---

## Data model notes

Reuse the existing blackboard/shared-store pattern with atomic claim semantics from the earlier recursive agent system rather than building new coordination state from scratch. Minimum fields needed per task to support all of the above:

- `id`, `wave`, `name`, `deliverable`, `test`
- `status`: `queued | running | verified | needs_revision | escalated`
- `candidate_models`: ordered list
- `attempts`: list of `{model, started_at, duration, verdict, reason}`
- `revision_rounds`: count, plus the note text from each revision
- `depends_on`: task ids (used to compute wave assignment, and to gate the dashboard's "queued because waiting on X" display)

Section 5's run summary is a derived view over the `attempts` data already captured per task — it shouldn't need new per-task fields, just an aggregation step keyed by model across all tasks in a run.

## Open questions for the build

- Static vs. adaptive model ordering: start static, but the `attempts` history above is exactly what's needed to later rank models by task category — worth designing the schema so that comes for free later rather than needing a migration.
- How disruptive should a mid-wave contract flag be — pause just the flagging task, the whole wave, or the whole run? Recommend starting with "pause the wave" as the safer default.
- Live view transport: polling vs. websocket/SSE against the blackboard store — pick whichever fits the existing store's change-notification story, if it has one.
