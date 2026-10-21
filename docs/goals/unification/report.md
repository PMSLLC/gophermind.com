# Unification goal report

Written 2026-10-15. Branch `feat/briefv2-planner-core` (worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`), tip `b977a59`. Merged to `main` and pushed on 2026-10-20 (merge commits d869c51, a647ceb); all 305 branch commits were re-authored as John Brahy on 2026-10-20, so the hashes below are the rewritten ones. The smoke and the graded run are still open (`docs/GOAL-FINISH.md`). Goal document: `docs/GOAL-UNIFICATION.md`.

## Where the goal stopped

Tasks 0 to 10, 12 and the dry-run merge are done. Task 11.1 (csvstat on the mini) and 11.2 (the graded AIVS run) did not run: a human prerequisite is missing (list below). Everything before them is done and verified.

## What was built, with commit ranges (merged to `main`)

| Sub-project | Commits | Result |
|---|---|---|
| A: run state as files | `7565af7` and its range, earlier sessions | no SQLite; runtime files, `_state/events.jsonl`, `calls.jsonl`, scrubbed `attempts/` |
| B: node widening and question model | earlier sessions | every group on every node; questions in rounds; decisions in the tree |
| C: `/project` single entry | up to `5bb886b` | `/project` drives plan and build through the one system |
| E: planner revise stage | `ce2fc06..da545e9` (spec fix in main `956917b`) | a failed leaf is rewritten within the approval rules; bounded by `max_revisions` |
| F: parallel leaves | `0e10090..b834b99` | `executor.workers` 1 to 8, `--workers`; private work trees; deterministic landing |
| D and G: run view, v1 removal, rename | `fcb0d68..af1e421` | `runview`, node-based pipeline contract, dashboard, desktop client and panel, v1 and `/phase` removed, `briefv2` renamed `build` |
| Docs sweep | `3348844`, `b977a59` | README, CHANGELOG, architecture index, `INDEX.md` regenerated |

Docs commits on `main` (unchanged by the rewrite): plans and specs for E, F, D+G; spec amendments `fb9ca16`; overview `f52632b`; rename map and GOAL.md path `1a308a6`.

## Verification (final matrix, tip b977a59, all pass)

- `gofmt -l`: empty. `go build` and `go vet`: clean in the root module, `gophermind-lib`, `gophermind-osx` (cgo and libui present).
- Executor package alone: pass in 1924 s (the known load flake did not trigger). The other 80 `gophermind-lib` packages, `./cmd/...`, `gophermind-server` (plain and `-race`), `desktop`: pass.
- `gophermind-osx`: client, ui, connection with `-race`: pass; panel tests (`-run Pipeline`): pass. An unsigned `build-app.sh` ran during D+G task 11 ("Bundle OK") and was deleted.
- `-race` on `runview` and `serve`: pass.
- Gates: removal and rename gates pass; no `briefv2` outside the historical list; no importer of `plantree`, `orchestrate`, `phaseflow`; no SQLite in `gophermind-lib/build`.
- `brief validate`: both briefs (AIVS and csvstat) print no warning.
- `brief plan --fake` on the greeter: `Requirements covered: 7 of 7`. `brief run` has no `--fake` flag; the offline greeter run is covered by the executor and cmd Go tests, not by a shell run.
- Dry-run merge (`git merge-tree --write-tree main feat/briefv2-planner-core`): no conflicts, result tree `f0e01e9`. The merge was done on 2026-10-20.
- Per-task and final reviews: every task had a spec and quality review; E, F and D+G each had a multi-agent final review with adversarial verification and one fix wave plus a scoped re-review.

## Rulings made (all in the SDD ledgers under `.superpowers/sdd/*/progress.md` and `rulings.md`)

- E: one fix wave then one scoped re-review; parked minors are listed in the ledger.
- F: accepted all 18 pre-flight conflicts (`rulings.md` overrides plan text); candidate sanity rule replaced by "Revision 0 valid, hash the reply text"; mid-write kill leaves `foreign_dirt` and is documented, not changed.
- D+G: accepted pre-flight conflicts C1 to C29. Main ones: newest run is the most recently updated; decisions include the build's revision decisions; component status is derived from children; a `/project` run that ended in planning shows its `project.json` status; `revision` log lines emit `node-status`; only verified and failed finish a wave; clients refetch state and report on every reconnect; removed commands print a notice for one release.
- Root package name `build` was the plan default (your open item 1 in the overview); it is one mechanical commit to change.
- Review-found defects fixed in D+G: SSE line limit, write deadline, resume past the log, dashboard double-applied and dropped frames, panel follower restart, `invalid_brief` status, agent tools skipping a directory named `build`.

## Known limits (also in `docs/build/README.md` where user-facing)

- A kill in the middle of a file write leaves a temp file; resume stops with `foreign_dirt` (pre-existing serial behavior).
- Leaves that share a path land first-fit, not id order (a plan defect case).
- A harness fault in a landing check lets leaves waiting behind it land first; the faulted leaf lands on resume.
- In rewrite mode, a cancel after a revise reply ends as a harness fault, not an interrupt (E code, backlog).
- The desktop attempt double-count window: a snapshot read mid-run can inflate an attempt count on replay (low).
- A stale desktop seed can write state after `stop()`; the next reconnect refetch corrects it (low).
- `gophermind-server` has two tests that flaked under `-race` earlier in this work (`TestRunWithConfig_EndToEnd`, `TestRunSweeper_RemovesTimedOutPeersOnATicker`); they passed in the final matrix.
- `TestPipelineSmokeRealRoot` skips without `GOPHERMIND_SMOKE_ROOT`; no real-root smoke ran.

## Open decisions for you

1. Start Breakdown (macOS app and Wails app) still seeds v1 `.planning` files that nothing reads; `projectctx` still reads `.planning/SPEC.md`. Keep, rewrite or remove.
2. `gophermind-server/deploy/README.md` install paths and unit details are unverified for the Fedora mini (the service file uses `/opt/gophermind`); check over `ssh home` before the deploy.
3. `~/.gophermind/config.json` points at `192.168.1.35` with an Ollama-style model name; the home server runs llama-server on `127.0.0.1:8081` of the Fedora box. Pick the endpoint the settings file should use.

## What blocked tasks 11.1 and 11.2

Preflight failed: `~/.gophermind/gophermind.yaml` does not exist (`gophermind-lib/build/projectrun/preflight_env.go:27`). Needed for csvstat and the graded run: a provider for the mini with `models.strong`, `models.standard`, `models.any` and `context_tokens` each; `toolchain.PATH` containing the go directory; `executor.sandbox: off` or a working `sandbox-exec`. Needed only for the graded AIVS run: the vault passphrase (`GOPHERMIND_VAULT_PASSPHRASE`) and a vault holding `DATABASE_URL` and `TEST_DATABASE_URL`; two empty PostgreSQL 15+ databases on loopback (for example `ssh -N -L 55432:127.0.0.1:5432 <host>`); `privacy.mode: private_only` with the mini in every tier; the installed `gophermind-dev` binary. The mini was healthy at 19:25 PDT (model service active, about 3 GB memory available, 8 of 32 GB swap used). No attempt was made and none is recorded in `docs/goals/attempts/`.

## Steps for you

1. Done 2026-10-20: the branch was merged to `main` and pushed; the remote branch is deleted.
2. Done 2026-10-20: the executor suite passes (41 minutes, `-timeout 60m`; the fixture was stale, entry point stage) and the server shutdown and sweeper test flakes were fixed in test code.
3. Write `~/.gophermind/gophermind.yaml` and provide the AIVS prerequisites above, then tell me; I run the csvstat smoke, then the graded loop to `v0.1.0` in the target repo.
4. Deploy the server to the mini (Fedora, systemd, not launchd): on the host `git fetch` and `git checkout <ref>` (use `ssh -A`, the host has no GitHub key), build there, `sudo systemctl restart gophermind` (unit name per `gophermind-server/deploy/gophermind.service`), then fetch `/pipeline/runs` and `/pipeline/state` with a token.
5. Build, sign and notarise the desktop app and release it in the same window as the server (the pipeline contract changed; they ship together). `build-app.sh` needs your signing identity.
6. Tag the release (the `/phase` notice says it was removed in 0.10.0, constant `tui.PhaseRemovedIn`).
