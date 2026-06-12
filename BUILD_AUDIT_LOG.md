# BUILD_AUDIT_LOG — gophermind (Go coding-agent TUI)

Scope: the project in this working directory only. Constraints: staged deploy
only (no production ship; build command recorded in DEPLOY_QUEUE.md), commit on
branch `security/audit-2026-06-12`, leave pre-existing untracked files alone.

## Baseline (established)
- Toolchain: go 1.26.4 (module `gophermind`, go 1.24.2).
- `go build ./...` OK; `go vet ./...` OK; `go test ./...` PASS
  (agent, config, llm, tools, tui — no FAIL).
- Pre-existing untracked (NOT mine, will not commit): `gophermind` binary,
  `docs/training-datasets.txt`.

## Work-set enumeration (finite)
- Feature signals: no TODO/FIXME/HACK in code; no BACKLOG/ROADMAP/FEATURES
  files; the TUI plan in docs/superpowers is already implemented. Feature
  backlog is empty → primary work is the security audit of the agent sandbox.
- Security surface (a coding agent that executes shell + file ops): `safety`
  (path containment, shell deny-list, approval gate), `tools` (shell, files,
  search), `llm` client.

## Findings & fixes

### Pass 1
- **F1 (HIGH) — sandbox escape via symlink in `safety.SafeJoin`.**
  `SafeJoin` joined lexically and prefix-checked but never resolved symlinks, so
  a symlink inside the repo pointing outside (repo/evil -> /etc) let read_file/
  write_file escape root ("evil/passwd" -> /etc/passwd). Fixed: resolve the real
  root and the deepest existing ancestor (new `resolveExisting`) and re-check
  containment; works for not-yet-created write targets.
  `internal/safety/safety.go`. Tests: `TestSafeJoinRejectsSymlinkEscape`,
  `TestSafeJoinAllowsContained`.
- **F2 (MEDIUM) — argument injection in `tools.Search`.**
  User pattern was passed as a positional with no `--` terminator, so a pattern
  like `--pre=…`/`-f…` was parsed by rg/grep as a flag (rg `--pre` runs an
  external preprocessor). Fixed: add `--` (and grep `-e`) so the pattern is
  always literal. `internal/tools/search.go`. Test:
  `TestSearchPatternNotTreatedAsFlag`.
- **F3 (MEDIUM) — deny-list whitespace bypass in `safety.CheckCommand`.**
  Substring match let `rm  -rf`, `rm\t-rf`, `rm -fr`, `>/etc` slip past. Fixed:
  whitespace-normalize before matching and broaden patterns (rm flag orderings,
  mkfs, dd if=, `>/`, `> ~`). Honest note: a deny-list is best-effort; the
  approval gate is the primary shell control. `internal/safety/safety.go`.
  Test: `TestCheckCommandWhitespaceBypass`.
- Result: `go vet` clean, `go test ./...` PASS (no regressions).

### Pass 2 (re-audit — no new actionable findings)
- Secrets: API key only set as Authorization header (stream.go/client.go);
  never logged/printed. OK.
- TLS: `InsecureTLS` is opt-in, default-false, documented (self-signed internal
  endpoints). Acceptable; noted, not changed.
- exec surface: only `bash -lc` (shell, gated + deny-list) and rg/grep (search,
  now `--`-terminated). No other exec.
- SSRF/CSRF/CORS/authn/authz/PII: N/A for a single-user local CLI; LLM BaseURL
  is operator-configured, not user-controlled.
- Observation (non-security, not fixed): `read_file` returns full file content
  unbounded (shell/search/list are truncated) — a context-window concern, not a
  vuln; left as-is to avoid changing legitimate read semantics.
- Dependency CVEs: `govulncheck ./...` → 0 reachable vulnerabilities (your code
  calls none). 7 imported / 3 module advisories exist but are unreachable.

### Deploy
- Staged only. No production ship (gophermind has no remote deploy pipeline).
  Build command recorded in DEPLOY_QUEUE.md. No IaC in repo.

DONE: full audit pass clean, tests green, deploy staged. See BUILD_AUDIT_REPORT.md.

## Coordination decision (2026-06-12)
- cwd item (gophermind.com) DONE; no todo.pm in cwd to source further items.
- All machine-wide todo.pm items are DONE (report committed) or owned by a live
  agent in the running overnight fleet (run.sh PID 9588, 12 jobs active).
- Decision: stand down. Taking any remaining item would collide with a running
  agent or redo a completed audit. Do not disturb the fleet. Nothing left to do.
