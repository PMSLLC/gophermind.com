# BUILD_AUDIT_REPORT — gophermind (Go coding-agent TUI)

**Date:** 2026-06-12 · **Stack:** Go 1.24 (built with go 1.26.4), Bubble Tea TUI,
OpenAI-compatible LLM client · **Branch:** `security/audit-2026-06-12` ·
**Deploy:** staged only, not shipped.

## Baseline
- `go build ./...` OK, `go vet ./...` OK, `go test ./...` PASS across all
  packages (agent, config, llm, tools, tui). Established before any change.

## Features added
- **None.** Enumerated work-set was finite and empty: no TODO/FIXME/HACK in
  code, no BACKLOG/ROADMAP/FEATURES files, and the only design doc (the TUI
  plan in `docs/superpowers/`) is already implemented. This was therefore an
  **audit + hardening** pass of the agent's security surface.

## Security issues found and fixed
All three are root-cause fixes in the agent's sandbox, each with a regression
test. Commit `0a49387` on `security/audit-2026-06-12`.

1. **F1 — HIGH — Sandbox escape via symlink in `SafeJoin`.**
   `internal/safety/safety.go` — `SafeJoin` did a lexical join + prefix check
   but never resolved symlinks, so a symlink inside the repo pointing outside
   (`repo/evil -> /etc`) let `read_file`/`write_file` reach outside root
   (`evil/passwd` → `/etc/passwd`). Fixed by resolving the real root and the
   deepest existing ancestor of the target (new `resolveExisting`) and
   re-checking containment; still works for not-yet-created write targets.
   Tests: `TestSafeJoinRejectsSymlinkEscape`, `TestSafeJoinAllowsContained`.
2. **F2 — MEDIUM — Argument injection in the `search` tool.**
   `internal/tools/search.go:30-39` — the user pattern was passed to `rg`/`grep`
   as a positional with no `--` terminator, so a pattern like `--pre=…` / `-f…`
   was parsed as a flag (`rg --pre` runs an external preprocessor). Fixed by
   adding the `--` terminator (and `grep -e`) so the pattern is always literal.
   Test: `TestSearchPatternNotTreatedAsFlag`.
3. **F3 — MEDIUM — Deny-list whitespace bypass in `CheckCommand`.**
   `internal/safety/safety.go` — substring matching let `rm  -rf` (extra
   space), `rm\t-rf`, `rm -fr`, and `>/etc` slip past. Fixed by whitespace-
   normalizing the command before matching and broadening the patterns (rm flag
   orderings, `mkfs`, `dd if=`, `>/`, `> ~`). Test:
   `TestCheckCommandWhitespaceBypass`. *Honest caveat:* a deny-list is
   best-effort defense-in-depth; the **approval gate** (`safety.ApprovalFunc`,
   `run_shell` is gated) remains the primary control for shell execution.

## Re-audit (clean)
A second full pass across the coverage list found no new actionable issues:
- Secrets: API key only ever set as `Authorization` header; never logged.
- TLS: `InsecureTLS` is opt-in, default-false, documented.
- exec surface limited to gated `bash -lc` (shell) and `--`-terminated rg/grep.
- authn/authz/IDOR/SSRF/CSRF/CORS/PII: N/A for a single-user local CLI; LLM
  `BaseURL` is operator-configured, not user-controlled.
- `govulncheck ./...`: **0 reachable vulnerabilities** (7 imported / 3 module
  advisories exist but are unreachable — no called symbols).

## Skipped / deferred (with rationale)
- **`read_file` unbounded output** — returns full file content (unlike
  shell/search/list, which truncate). This is a context-window concern, **not a
  security vulnerability** (no escalation or escape), so it was left as-is to
  avoid changing legitimate read semantics. Noted for future product work.
- **Imported-package CVE advisories** — not reachable per govulncheck; a routine
  `go get -u` is optional maintenance, out of scope for a security fix.

## Final status
- **Tests:** `go vet` clean; `go test ./...` PASS (all packages, no regressions).
- **Audit:** complete; one full re-audit pass found nothing new.
- **Deploy:** **intentionally staged, not shipped.** Build command recorded in
  `DEPLOY_QUEUE.md` for manual run. No IaC in this repo.
- **Item status:** DONE.
