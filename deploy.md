# deploy.md: gophermind.com (GOAL.md run notes)

GopherMind is a CLI, not a server deployment; "deploy" here means building the binary, running rehearsals, and the graded clean run. The user's standing deployment rules (git-based deploys) apply to any server work; none is part of this goal.

## Build and install
- Never build the graded binary from a dirty worktree. Use a pushed commit: `git archive <sha> | tar -x -C <scratch>` then `cd <scratch> && go build -o <out> ./cmd/gophermind` (the user's git wrapper blocks `git worktree add`, `checkout`, `stash`, `--gw-force`).
- Planned: `~/.gophermind/bin/gophermind-dev` stamped with version and commit (`/project` plan task 13: guard script refuses unless clean and pushed). The installed `/opt/homebrew/bin/gophermind` is 0.9.0 and predates the branch.

## Model endpoint
- Mini Ollama: LAN http://192.168.1.35:11434/v1 (default in new configs) with `base_url_fallbacks` http://10.8.0.6:11434/v1 (VPN). Check: `curl -s --max-time 8 http://10.8.0.6:11434/api/version` and `/api/ps` (qwen3.6:35b-a3b must be loaded). Memory: `ssh mini 'memory_pressure | tail -1; sysctl vm.swapusage'`.
- Provider settings: `reasoning_effort: none` for the mini, call timeout 10 minutes default (25 minutes planned for private providers), `executor.workers: 1`, `max_run_minutes: 720`, `executor.sandbox: on`.

## Test commands
- `cd gophermind-lib && gofmt -l briefv2 && go vet ./briefv2/... && go test ./briefv2/... -race -count=1 -timeout 30m` (executor package ~11 min), then from the repo root `go test ./cmd/... -race -count=1`. In the worktree use `go build ./cmd/... ./gophermind-lib/...` (the ignored `desktop/frontend/dist` is missing).
- Pre-push gate (hook): gofmt, vet, build, `go test -race ./...` for root packages, iOS XCTest (flaky: retry unchanged, never bypass). Tests that run git must strip every GIT_* variable.

## Rehearsals and graded run
- Planner rehearsal on the real brief: `GOPHERMIND_VAULT_PASSPHRASE=... GOPHERMIND_CONFIG_DIR=<cfg> <gm> brief plan <brief> --gate file` then `<gm> brief resume <id> --gate file`; approve by editing APPROVAL.md in `<repo>/.gophermind/<id>/` then resume. Executor: `<gm> brief run <id>`, `<gm> brief report <id>`, `<gm> brief status <id>`, `<gm> brief run --check-env`. Exit codes: 0 verified, 1 failed, 3 waiting, 4 escalated, 5 interrupted, 6 preflight, 7 harness fault.
- Graded attempt: clear the target repo per GOAL.md (reset to tag `goal-baseline`, `git clean -fdx -e .remember`) PLUS the project state under `~/.gophermind` (paths printed by `--print-state-paths` once built); all deletes guarded (assign, test, `rm -rf -- "${target:?}"`). The git wrapper may block the clear commands: test first and ask John before any override. Record each dead attempt in `docs/goals/attempts/NN.md` (20 lines max).
- Health/monitoring: run logs under `<repo>/.gophermind/<id>/` (events and report only ids/counts), ledger in `~/.gophermind/blackboard.db` (WAL; open read-only for status), `brief calls <id>` for per-task-type and per-model counts.

## Rollback
- Code: revert on the feature branch (`git revert`), never force-push. Target repo: only GopherMind commits land; the orchestrator tags v0.1.0 on the final commit; `goal-baseline` is the empty start for clears.
