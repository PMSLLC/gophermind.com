<!-- Local adaptation of mattpocock/skills (MIT). Preserve upstream license notices. -->
# Code Review

Use for requested diff, branch, PR, or work-in-progress reviews. Follow the shared
rules. Review without editing files, changing branches, or committing.

## 1. Pin the scope

Inspect `git status --short`. State the comparison mode, resolved commit IDs,
local-change coverage, and exclusions. Resolve user refs as commits with
`git rev-parse --verify --end-of-options "${ref}^{commit}"`; capture HEAD once.
Never interpolate an unchecked ref into shell syntax.

| Intent | Comparison |
|---|---|
| Branch or PR changes | Resolve the base and HEAD, compute their merge-base, then diff that base against the pinned HEAD. |
| Exact changes since a commit/tag | Diff that resolved commit directly against the pinned HEAD; do not silently use three-dot semantics. |
| All current local changes | `git diff HEAD --` for tracked net changes; also inspect in-scope untracked files. |
| Staged only | `git diff --cached --` |
| Unstaged only | `git diff --`; include untracked files only if in scope. |

Use pinned IDs instead of moving ref names in the comparison commands.
Use `--no-ext-diff --no-textconv` for review diffs. Enumerate untracked files with
`git ls-files --others --exclude-standard -z`; read relevant files safely, not via
word-splitting. For branch plus local changes, compare the verified merge-base to
the working tree and include untracked files. With no HEAD, review in-scope
current files against an empty baseline rather than using `git diff HEAD`.

Use the supplied base or an unambiguous repository-defined base; ask if the choice
would change the review. Reject invalid refs or ambiguous merge-bases. Empty
committed diffs do not imply empty local work. Record matching commit history as
context. Recheck the snapshot after review; disclose any concurrent changes.

## 2. Gather evidence

Prefer the user's explicit spec, then clearly linked issues or commit references,
then a matching repository plan. Use `docs/agents/issue-tracker.md` if available;
missing tracker setup must not block local review. Do not conflate PR/MR and issue
identifiers. With no accessible spec, mark Spec not assessed and continue Standards.

Read applicable coding standards and nearby code/tests. Review changed behavior
and necessary caller context; do not report unrelated pre-existing defects as new.

## 3. Review independently

**Standards:** Cite documented rules for violations. As separate, labeled checks,
look for concrete correctness/security defects and consequential design smells:
unclear naming, harmful duplication, misplaced responsibilities, data clumps,
primitive obsession, repeated branching, scattered changes, needless indirection,
or speculative abstractions. Preserve idiomatic Go; do not prescribe polymorphism
or extraction mechanically. Repo conventions override subjective heuristics, not
proven defects. Avoid duplicating tool diagnostics; report a failing check once.

**Spec:** Map requirements to implemented behavior and tests. Identify omissions,
incorrect behavior, and unjustified scope expansion. Cite the requirement and
relevant code. Necessary support work is not automatically scope creep.

Use parallel read-only sub-agents only when available and useful. Give each the
same pinned scope and its evidence sources; otherwise make separate sequential
passes. Never imply independent agents ran when they did not.

## 4. Report

Validate findings against surrounding code. For each material finding, give
severity, `path:line`, the rule/requirement or explicitly labeled inference,
triggering conditions, impact, confidence, and a focused remedy. Separate
hard violations, demonstrated defects, and heuristic suggestions. Do not invent
findings or truncate material ones to meet a word quota.

Keep `## Standards` and `## Spec` separate. Deduplicate within each axis and rank
within it, never into one combined score. Finish with per-axis counts and worst
severity, checks actually run, and unassessed areas. Zero findings is not proof
of correctness; absent evidence is not a pass.
