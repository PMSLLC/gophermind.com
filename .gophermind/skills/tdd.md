<!-- Local adaptation of mattpocock/skills (MIT). Preserve upstream license notices. -->
# Test-Driven Development

Use for test-first features, behavioral changes, and regression fixes. Follow the
shared rules; for an unexplained bug, diagnose it before implementing a fix.

## Define the behavior and seam

Use the spec, domain vocabulary, and existing tests to identify one observable
behavior and an appropriate boundary. Prefer established, previously agreed
seams. State the boundary and proceed when the contract is clear; ask only when
an unresolved choice changes externally visible behavior or substantial design.
Do not require approval before every routine test.

Test a meaningful package/API/CLI boundary, not incidental private structure.
A Go `internal/` package can still expose the right boundary to its consumers.
Use a broader integration seam when the real failure crosses callers/components;
a narrow unit test is not interchangeable with that regression coverage.

## One vertical slice at a time

1. **Red:** Write one focused behavioral test or cohesive table-driven case group.
   Derive expected results independently from the requirement, known-good fixture,
   or worked example. Run it and confirm failure for the intended behavior, not
   unrelated compilation, setup, or authentication errors. Add only necessary
   scaffolding to reach the meaningful assertion.
2. **Green:** Implement only enough to satisfy that behavior. Run the test again,
   then relevant neighboring tests. Do not change the expectation to match an
   unexplained result or add speculative features.
3. **Repeat:** Select the next behavior based on what the prior slice established.
   Defer discretionary refactoring to review, preserving this pack's red/green
   policy. When red-green-refactor is explicitly requested, refactor only while
   green and rerun affected tests after each behavior-preserving change.

## Test quality

Assert outcomes, errors, invariants, and documented side effects. Use real small
components where practical; fake external boundaries for determinism. Do not mock
the behavior under test or assert internal call choreography without a contract.

Avoid tautological expectations, snapshots blessed without inspection, hidden
side-channel assertions, arbitrary sleeps, and bulk tests for imagined interfaces.
Use isolated fixtures and cleanup; keep tests independent of order and shared
mutable state. Cover relevant failure paths as separate vertical slices.

Finish through `gophermind-build-test`. Report behavior covered, the actual red and
green commands/results, and coverage or environment gaps. When tests cannot run,
label them unexecuted; do not describe a completed red/green cycle.
