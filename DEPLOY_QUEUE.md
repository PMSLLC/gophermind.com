# DEPLOY_QUEUE — gophermind

Staged deploys only. Nothing here has been shipped. gophermind has no remote/
production deploy pipeline — its "deploy" is a local build of the CLI binary
(`bin/build.sh` builds then launches the interactive TUI). The build step is
recorded below for a human to run; the launch step is intentionally not
automated (it would block on an interactive session).

| Date | Change | Tests | Deploy command (run manually) |
|------|--------|-------|-------------------------------|
| 2026-06-12 | `security/audit-2026-06-12` @ `0a49387` — sandbox hardening: SafeJoin symlink containment (F1), search arg-injection guard (F2), deny-list whitespace bypass (F3) | `go vet` clean; `go test ./...` PASS (all packages) | `go build -o gophermind ./cmd/gophermind` (then `./gophermind` to launch, or `bin/build.sh`) |

No infrastructure / IaC changes in this audit (none exists in this repo).
