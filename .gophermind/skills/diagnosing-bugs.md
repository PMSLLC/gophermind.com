<!-- Local adaptation of mattpocock/skills (MIT). Preserve upstream license notices. -->
# Diagnosing Bugs

Use for failures, incorrect behavior, intermittent bugs, or performance regressions.
Follow the shared rules. Preserve the six-phase workflow, scaling effort to the bug.

## 1. Build a feedback loop

Capture expected versus actual behavior, relevant inputs, environment, and recent
changes. Read enough relevant code and logs to construct a useful experiment.
Prefer a focused failing test; otherwise use a request/CLI replay, browser harness,
minimal fixture, differential run, or bounded property/fuzz test.

Define one command or repeatable procedure that detects the user's exact symptom.
Run it and record the actual result, exit status, and redacted evidence. A build,
authentication, or setup failure is not reproduction of a different application bug.
Make the loop as fast and deterministic as practical, not obligatorily seconds.

For intermittent bugs, record failures/attempts, seed, load, and observation window.
Use bounded local stress; do not stress production without authorization. A passing
sample does not establish absence. Use structured manual steps if automation is
unavailable; do not depend on a missing HITL template.

After a bounded set of materially different attempts, report any missing access
or evidence. Continue useful read-only diagnosis with labeled hypotheses, but do
not claim a reproduced bug or verified fix when the environment prevents it.

## 2. Reproduce and minimize

Confirm the observed failure matches the report. Reduce one input, dependency,
or step at a time while preserving the symptom. Stop when the repro is practical
and discriminating; exhaustive minimality is not required. Keep the original
scenario for final verification.

## 3. Hypothesize

Rank plausible causes using evidence. Give each a falsifiable prediction and the
next discriminating experiment. Consider alternatives when ambiguity remains;
do not fabricate a quota of hypotheses for an obvious failure. Share the leading
explanation and test without waiting for routine approval.

## 4. Instrument and test

Change one variable per experiment. Prefer targeted debugger inspection, boundary
logs, traces, or a controlled bisect in an isolated worktree. Tag temporary probes,
limit captured data, and record which prediction each result supports or rejects.
Do not reset or check out over the user's work.

For performance, measure a repeatable baseline under comparable workload, warm-up,
and environment; use profiling/query plans as appropriate. Compare repeated
measurements, not one timing or added logging. Bound retries and experiment cost.

## 5. Fix and regression-test

Follow `tdd`: add a test at the boundary that reaches the real failure, observe the
relevant failure, make the smallest supported fix, then observe success. Exercise
multiple callers/concurrency when necessary; a shallower test is not equivalent.
When no suitable automated seam exists, retain the best repeatable regression
procedure and document the coverage gap. Do not claim architectural impossibility
merely because no unit-test seam is obvious.

## 6. Verify and clean up

Rerun the original scenario, regression test, and relevant build/test checks.
For flaky or performance issues, compare equivalent pre/post measurements and
state residual uncertainty. Remove only your temporary probes, files, and processes;
retain intentional diagnostics and reusable tests. Preserve any required evidence.

Report symptom, supported cause and confidence, fix, before/after evidence,
commands actually run, and remaining blockers or unverified behavior.
