# secaudit — security bug-hunt for a repository

**Date:** 2026-07-24
**Status:** approved

## Goal

Point gophermind at a repository and systematically hunt for likely security
vulnerabilities: a deterministic static scan, an optional LLM review that
verifies findings and reasons about logic flaws, and a ranked report. A clean
run means "nothing found by these checks," never "provably secure" — the tool
says so in its own output.

## Architecture

`internal/secaudit` is the engine; the `secaudit` subcommand and `/secaudit`
TUI command are thin front ends.

### Types (`secaudit.go`)

- `Severity`: Critical, High, Medium, Low, Info.
- `Finding`: RuleID (CWE-tagged), Severity, Category, File, Line, Snippet,
  Message, Remediation, Verified (bool), VerifyNote.
- `Report`: the target path, the findings, and counts by severity.

### Static scan (`scan.go`) — deterministic, no model required

Walks the tree, skipping `vendor/`, `dist/`, `.git/`, `node_modules/`, and
binary files. Two rule sets:

- **Language-agnostic** (regex over any text file): hardcoded secrets
  (AWS access keys, PEM private-key blocks, bearer/API tokens, `password=`),
  weak crypto names (md5/sha1/des/rc4), disabled TLS verification, injection-
  shaped string building, dynamic eval/exec, unsafe temp files.
- **Go AST** (parsed, Go files only): `InsecureSkipVerify: true`,
  `exec.Command` with concatenated arguments, string concatenation into
  `database/sql` query calls, `math/rand` used where crypto randomness is
  expected.

Each rule yields a `Finding` with a CWE-tagged id, severity, `file:line`, the
offending snippet, and a remediation note. A file that fails to parse is
skipped, not fatal.

### LLM review + verification (`review.go`) — additive, degrades gracefully

Given a client, for each static finding the model is asked to confirm or refute
it as actually exploitable (sets Verified + VerifyNote), and to surface
logic/authz flaws in that file that patterns miss. Bounded per file and chunked
to fit a small local context window. With no client, findings pass through
unverified and the scan is static-only.

### Report (`report.go`)

Renders `SECURITY-AUDIT.md`: a summary of counts by severity, then each finding
grouped by severity with location, evidence, remediation, and its verified/
unverified state. Opens with a prominent disclaimer that the audit surfaces
issues it can find and does not prove their absence.

### Command

`gophermind secaudit <path> [--static-only] [--out FILE] [--fail-on SEVERITY]`.
Writes the report (default `SECURITY-AUDIT.md` in the target) and prints a
summary. Exits non-zero when any finding meets `--fail-on` (default: high), so
it can gate CI or a deploy. `/secaudit [path]` runs the same over the TUI's
working directory.

## Testing

Every static rule has a positive fixture (must flag) and a negative fixture
(must not), so a rule cannot silently stop working or start over-firing. Plus:
report rendering (disclaimer present, counts correct), severity threshold logic,
the verify-merge (a refuted finding is dropped or downgraded), and command
wiring. All runs through the pre-deploy gate.

## Non-goals

- No auto-fixing of code — it reports, the user (or a later agent run) fixes.
- No guarantee of completeness or a "secure" certification.
- Not a replacement for dedicated SAST/DAST or a human security review.
