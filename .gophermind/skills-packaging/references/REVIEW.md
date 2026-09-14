# GopherMind skill-pack review and optimization

Date: September 14, 2026

## Contents

- [Scope and conclusion](#scope-and-conclusion)
- [Measured size](#size-measured-on-the-seven-injected-files)
- [High-priority findings](#high-priority-findings-and-corrections)
- [Additional corrections](#additional-corrections)
- [Policy changes](#intentional-workflow-policy-changes)
- [Validation and limits](#validation-and-limits)
- [Future loader improvement](#next-architecture-improvement-not-implemented-here)
- [Sources](#source-inventory)

## Scope and conclusion

Reviewed all seven supplied Markdown files and produced seven full replacements.
The goals are fewer always-loaded instructions, safer examples, clear triggers,
consistent workflow boundaries, and evidence-based completion. This is a revision
of instruction files, not a patch to GopherMind application code.

The uploaded README says `Skills()` concatenates every Markdown file in this
directory into every turn and strips YAML frontmatter on import [S1]. The replacements
therefore preserve the flat, frontmatter-free format. Six filenames are unchanged;
the timestamped README upload becomes `README.md`.

Repository paths, APIs, Makefile targets, CLI behavior, PhaseFlow schema, and license
provenance beyond the supplied documents were not verified against a GopherMind
checkout. None was supplied. External verification was limited to official command/
language semantics and the upstream license; it does not establish application
behavior. Behavioral changes below are editorial proposals embodied in the pack,
not claims that the original implementation already behaves that way.

## Size measured on the seven injected files

Words are counted with whitespace splitting; characters are Unicode code points.
Supporting review, tests, and packaging metadata are excluded because they should
not be copied into GopherMind's injected directory.

| File | Original words | Revised words | Reduction |
|---|---:|---:|---:|
| `README.md` | 232 | 237 | -2.2% |
| `code-review.md` | 1,072 | 589 | 45.1% |
| `diagnosing-bugs.md` | 1,410 | 520 | 63.1% |
| `gophermind-architecture.md` | 924 | 446 | 51.7% |
| `gophermind-build-test.md` | 547 | 466 | 14.8% |
| `gophermind-phaseflow.md` | 897 | 501 | 44.1% |
| `tdd.md` | 567 | 375 | 33.9% |
| **Total** | **5,649** | **3,134** | **44.5%** |

Characters: 37,202 -> 22,363 (39.9% reduction).
UTF-8 bytes: 37,376 -> 22,363 (40.2% reduction).
These are not measured token, billing, latency, or model-quality improvements.
The configured tokenizer and runtime were not available. The routing README grows
slightly because it now centralizes protections and avoids repeating them in six
workflows. Exact sizes and SHA-256 digests are in [metrics.json](metrics.json).

## High-priority findings and corrections

### 1. Formatting check can announce success with unformatted code

**Source:** `gophermind-build-test.md`, lines 46-51, chains `gofmt -l .` to a success
message. The flag lists differences; the source does not make list-only differences
a failing exit status [D1, D2]. This was also reproduced with Go 1.23.2.

**Change:** Require both a successful formatter invocation and empty output. Select
intended existing files, including new ones. Fail on syntax errors, missing files,
or an unavailable formatter. Keep checking separate from deliberate formatting;
do not recursively rewrite the whole checkout. The replacement snippet is exercised
in isolated fixtures, including a filename containing spaces.

### 2. Architecture contains an unsafe path-containment example

**Source:** `gophermind-architecture.md`, lines 156-162, uses `strings.HasPrefix` on an
absolute path. A sibling such as `/work/repo-other` shares the prefix `/work/repo`;
a symlink lexically beneath the root can resolve outside it. Both cases were
demonstrated in temporary Go tests. This finding concerns the documented example,
not proof of a vulnerability in unseen application code.

**Change:** Remove the copyable insecure implementation. Require inspection of the
real containment helper, with root-scoped operations appropriate to the toolchain,
platform, and threat model. Go's traversal guidance also explains why lexical
validation and check-then-open symlink handling are insufficient [D3]. No new API
is assumed to exist in the user's Go version.

### 3. PhaseFlow instructions contradict themselves and permit false completion

**Source:** `gophermind-phaseflow.md` describes `next` as advancing phases in lines
29-36 but as executing the next task in lines 170-174. Initialization is described
both through `next` and `init` in lines 213-215 and 245. Line 249 changes the earlier
project-scoped state path to `.planning/state.json`. Line 251 recommends setting
`status: "done"` manually to skip a phase [S6].

**Change:** Preserve the documented operation names but label their exact semantics
as requiring confirmation from implementation/help. Separate task progress, phase
completion, verification gates, and persisted state. Remove assumed initialization
syntax, the root-level path, and the skip-by-marking-done shortcut. Add controlled
recovery and side-effect/idempotency checks after persistence failures. These are
safety requirements for agent behavior, not a claimed new PhaseFlow state machine.

### 4. Review scope misses local work and changes exact-baseline meaning

**Source:** `code-review.md` advertises work-in-progress review, but lines 18-22 use
`git diff <fixed-point>...HEAD` for every case. Three-dot compares against a merge-base,
not necessarily the exact supplied commit, and a committed diff does not cover
uncommitted/untracked work [S2, D4].

**Change:** Distinguish branch/PR, exact-baseline, staged, unstaged, and current-work
reviews. Resolve refs safely, pin commit IDs, enumerate untracked files separately,
and report scope/exclusions. Do not reject local work solely because a committed
diff is empty. Preserve two report axes rather than merging them into a score.

### 5. Debugging gates are too rigid for unavailable or intermittent environments

**Source:** `diagnosing-bugs.md`, lines 54-65, forbids hypotheses or proceeding before
an executed reproduction command. Lines 79-85 require exhaustive minimization, and
line 89 mandates 3-5 hypotheses [S3]. These instructions can block useful source-led
investigation when access is missing or encourage unnecessary ceremony for simple
bugs. That is a workflow assessment, not an empirical claim about a measured model.

**Change:** Keep all six phases and the repro-first preference, but permit relevant
code reading to construct a loop and labeled read-only hypotheses when execution
is unavailable. Bound experiments; record attempts and failure rates for flaky bugs.
Require honest distinction between reproduced, inferred, fixed, and verified.
Do not invent hypotheses just to meet a quota.

### 6. Missing optional capabilities are treated as mandatory dependencies

**Source:** Code review requires parallel sub-agents and a tracker setup command
[S2]. TDD references `tests.md`, `mocking.md`, and a `codebase-design` skill [S7].
Debugging depends on `scripts/hitl-loop.template.sh` [S3]. These resources are not
included in the seven uploaded files; their existence elsewhere is unknown.

**Change:** Use parallel agents only when available/useful, otherwise independent
sequential passes. Continue Standards review with Spec explicitly unassessed when
necessary. Inline the essential test guidance and permit a repeatable manual repro
procedure rather than requiring unavailable dependencies.

## Additional corrections

**Streaming terminology:** The architecture calls its channel send non-blocking,
but its select can wait until either sending or cancellation is ready. The Go
specification confirms the blocking behavior when no case is ready and there is
no default [S4, D5]. The revised instructions also call out blocked receives,
channel ownership, and the risk of adding a dropping default branch.

**Shell execution:** The source presents a deny-list as a boundary [S4]. The revision
treats it as defense-in-depth and asks for the actual argument policy, isolation,
working directory, environment, and approval controls. OWASP's guidance supports
avoiding shell interpretation and separating executable/arguments [D6]. The new
text does not claim that prompt instructions constitute a security sandbox.

**Flaky tests:** The build guide suggests adding `time.Sleep` [S5]. The revision
prefers deterministic synchronization, isolated state, and existing fake clocks.
Official Go guidance explains that real-time sleeps trade flakiness for slowness
rather than reliably establishing synchronization [D7].

**Build artifacts:** The original equates `go build ./...` with a Makefile target
producing `./gophermind`. Go may compile multiple packages without keeping their
output [S5, D8]. The revision requires inspecting the Makefile and distinguishing
compile checks from an actual freshly built executable.

**Unverified details:** Remove incomplete Go examples that look like runnable
implementations, hard-coded file lengths, historical logo walkthroughs, assumed
method signatures, and machine-specific troubleshooting. Preserve domain/package
navigation and refer to actual interfaces/tests. A path or command in the old docs
is not proof that it exists in the present checkout.

**Release boundaries:** Separate ordinary verification from device deployment,
release signing, commits, and production changes. Require authorization and report
unsupported platform checks as not run or blocked, not passed.

**Golden outputs:** Retain the inspect-before-updating rule, require that the update
flag actually exists, and rerun with updating disabled and cache bypassed for that
regression check. Updating expected output alone is not verification.

## Intentional workflow-policy changes

These changes are explicit rather than presented as mere copy editing:

- Replace mandatory seam confirmation for every test with reuse of established
  decisions; ask when the contract or a substantial design choice is unresolved.
- Permit useful static diagnosis before a runnable repro exists, while prohibiting
  claims of reproduction/verification without execution evidence.
- Replace mandatory parallel sub-agents with a capability-aware fallback.
- Expand Standards with labeled correctness/security checks, while keeping repo
  rules, demonstrated defects, subjective heuristics, and Spec separate.
- Require gate evidence before PhaseFlow completion instead of allowing a manual
  `done` value to stand in for completed work.
- Mark the three vendored workflows as local adaptations. The old upstream-only
  update policy would otherwise conflict with delivering edited copies. Retain
  license notices and track these changes through later imports.

Preserved: the two-axis review design, six-phase diagnosis organization, behavioral
seams, vertical test slices, red-before-green evidence, inspected golden updates,
and the original default of deferring discretionary refactoring to review. Explicit
requests for red-green-refactor get a controlled green-only refactoring branch.

## Validation and limits

Run [validate_pack.py](../scripts/validate_pack.py) to reproduce structural checks
and isolated command fixtures. The saved [validation receipt](validation-results.json)
records results and tool versions. Checks include pack format/links, a word budget,
formatter failure modes, Git comparison scopes, untracked-file handling, invalid
refs, and demonstrations of the unsafe containment example.

The structural validator for the native wrapper is separate from these command
fixtures. Neither establishes that GopherMind loads or obeys the pack. Proposed
[behavioral acceptance checks](ACCEPTANCE-CHECKS.md) are supplied but were not executed
with a live model. No GopherMind build, PhaseFlow operation, iOS simulator, release,
production action, or application security test was run.

## Next architecture improvement, not implemented here

Keep a small routing manifest in the always-loaded prompt and load workflow bodies
only when needed. Exclude README/history/license/review material from skill-body
loading. Add explicit discovery/loading support, stable workflow identifiers, and
tests for selective loading before changing behavior. Verify how `Skills()` works
first; no patch can be asserted correct without its implementation.

The replacement routing table by itself does NOT create lazy loading. It reduces
unnecessary workflow execution; all seven files still consume context under the
loader described in the uploaded README. The historical `humanize` move in that
README is the source's own example of moving rarely used material out of the hot
path [S1]. Model-specific before/after token measurement remains a runtime task.

## Source inventory

### Uploaded sources

[S1] `README(20260914-194807).md`, especially lines 6-8 and 28-30.
[S2] `code-review.md`, especially lines 3, 18-31, 57-77.
[S3] `diagnosing-bugs.md`, especially lines 23-34, 48-65, 79-97.
[S4] `gophermind-architecture.md`, especially lines 23-34, 135-169.
[S5] `gophermind-build-test.md`, especially lines 9-22, 34-57, 79-102.
[S6] `gophermind-phaseflow.md`, especially lines 29-36, 170-174, 213-251.
[S7] `tdd.md`, especially lines 15-25 and 33-37.

These references describe the supplied versions, not the replacement line numbers.

### Primary external references checked on September 14, 2026

[D1] Go gofmt command documentation: https://pkg.go.dev/cmd/gofmt
[D2] Go gofmt implementation: https://go.dev/src/cmd/gofmt/gofmt.go
[D3] Go traversal-resistant file APIs: https://go.dev/blog/osroot
[D4] Git diff documentation: https://git-scm.com/docs/git-diff
[D5] Go select statement specification: https://go.dev/ref/spec#Select_statements
[D6] OWASP OS command injection defense guidance:
https://cheatsheetseries.owasp.org/cheatsheets/OS_Command_Injection_Defense_Cheat_Sheet.html
[D7] Go testing time and asynchronicities: https://go.dev/blog/testing-time
[D8] Go build command documentation: https://pkg.go.dev/cmd/go

Upstream attribution and retained license details are in [PROVENANCE.txt](PROVENANCE.txt).
The complete content comparison is available in [changes.diff](changes.diff).
