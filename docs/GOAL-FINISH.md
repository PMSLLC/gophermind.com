# Goal: finish the unification, ship it, and run the graded build

Written 2026-10-20 18:50 PDT for John. Delegated run: no review gates between tasks, one report at the end. Start it with the `goal` skill. It replaces `docs/GOAL-COMPLETE-VERIFICATION.md` (its tasks 1 and the push are done; the rest are carried here). Map of all documents: `docs/unification-overview.md`.

## What finishing means (all true, with evidence in `docs/goals/unification/report.md`)

1. The csvstat smoke ends `verified` in one unattended `/project` run on the home-box model, with no hand edits to the output repo.
2. The output repo builds, vets and tests clean, and the by-file inspection passes.
3. The full matrix passes on `main`, including `-race` for server, osx client and executor.
4. Repo docs and the website describe the system as it is now (unified `build` system, `/phase` retired, new pipeline contract).
5. A release is prepared and ready to tag; the tag, publish and deploy wait for John's OK.
6. The graded AIVS run reached its perfect release by `docs/GOAL.md`'s rule, or the goal stopped at the prerequisite list with everything before it done.

## State at the start (verified 2026-10-20)

- Code is on `main` at `a647ceb` and pushed. The branch `feat/briefv2-planner-core` is deleted on the remote; locally it and the worktree `../gophermind-planner-core` remain (same code). All commits are authored `john@brahy.com` (history was rewritten; old to new hashes: `/tmp/gm-rewrite/hash-map.txt`, backup `/tmp/gm-rewrite/pre-rewrite.bundle`, both may be gone).
- Done: executor and projectrun suites pass (fixture fix `edd41f6` era), SQL guard test, server shutdown test fixes. Executor suite takes about 41 minutes: use `-timeout 60m`.
- NOT in place, found missing today: the model tunnel (`~/.gophermind/mini-tunnel.py`, nothing on `127.0.0.1:18081`); the csvstat inputs (`~/src/csvstat` and `/tmp/csvstat-smoke-brief.md` do not exist); `~/.gophermind/vault.age`; `gophermind-dev`. `~/.gophermind/gophermind.yaml` exists (39 lines).
- Also found at 18:52: `ssh home` times out at `10.8.0.6:22` (VPN path down; do the `scutil` stop/start cycle in task 1), and no csvstat source tree exists on this laptop (only docs mention it: `docs/build/README.md`, the v2 planner and executor specs and plans), so the brief and repo must be rebuilt from those docs.
- Known failures that are not this goal's: `gophermind-osx/auth TestKeychain_RealStoreRoundTrip` (keychain OSStatus -60006 when headless). Record, do not fix.
- This checkout has untracked `AGENTS.md`, `CLAUDE.md`, `docs/GOAL-COMPLETE-VERIFICATION.md`; commit them with the docs task.

## Rules

- Fix GopherMind, never the target repo. A fix is general (repair prompts name defect kind, line, valid values and never quote reply content; one shared function for plan-time and run-time checks). Subagent-driven: one fresh Sonnet implementer per fix, TDD, one reviewer per task, commit with explicit `git add` paths and the two attribution lines. Run planner tests with a clean home: `HOME=$(mktemp -d) go test ./build/planner/...`.
- One code writer at a time in the worktree. Docs and website work happens in this checkout and touches only `docs/`, `site/`, `CLAUDE.md`, `CONTEXT.md`, `README.md`, `CHANGELOG.md`; it may run while the smoke waits on the mini. Merge worktree fixes into `main` at checkpoints (`git merge --no-ff`), then push.
- Local model only for the smoke and graded run; never add a cloud model to the settings.
- Authorized by this goal: pushing `main` through the pre-push gate (`--gw-force` for the wrapper), and updating the website through the existing Pages workflow. Never bypass the gate with `--no-verify`; on a flake, retry unchanged, and if it fails twice on the same test, fix the test's cause. NOT authorized without John's OK in this session: tags, GitHub releases, npm publish, Homebrew changes, deploying the server to the mini, building or installing the desktop app, force-pushing, deleting branches.
- Guard every recursive delete (assign, check absolute and inside the intended folder, print, then `rm -rf -- "${target:?}"`). Never print a credential or `~/.claude/private/*`. No em dashes, no emojis.
- Stop after three fix rounds on the same failure and write `docs/goals/unification/blocked.md` (reply path, validator, what was tried).
- Resume rules for smoke runs: a change to contract, decompose or enrich logic needs a fresh repo (`git init -b main` plus an empty commit) and `~/.gophermind/runs/gm-2026-09-29-003.json` deleted first; a change to test-writer, coverage or later logic uses `--resume` on the same repo and must NOT delete the run record. Build the binary from the tip for every attempt (`go build -o /tmp/gm-preN ./cmd/gophermind`).

## Tasks, in the order that finishes fastest

1. **Preflight (15 minutes).**
   - Start the tunnel: `nohup python3 -I ~/.gophermind/mini-tunnel.py &`, then warm it with `curl http://127.0.0.1:18081/v1/models` until it answers (first call after idle takes about 13 s).
   - Check the home box over `ssh home`: `systemctl is-active llama-server`, memory and swap. If `ssh home` times out, do the full `scutil --nc stop jbrahy`, wait, `scutil --nc start jbrahy` cycle (`~/Projects/aws-terraform/docs/migration/home-mini-vpn-ssh.md`).
   - Recreate the smoke inputs: find the csvstat sources (search `~/Projects`, `~/OtherProjects`, the repo `examples/`, `docs/`, git history for `csvstat`); if the brief cannot be found, rebuild it from `docs/goals/unification/report.md` and the unification specs and say so in the report. Make the repo with `git init -b main` plus an empty commit.
2. **Finish the csvstat smoke** (tasks 1 to 3 of the old goal). Run `project <brief> --repo <repo>` unattended; loop: stop, diagnose from `<repo>/.gophermind/<id>/_state/replies/`, fix GopherMind, rebuild, resume. Expected ground: test-writer, then the executor's first real wave builds, the leaf ladder, acceptance with the built binary (the brief's bullets begin with `go run .`, which the harness forbids: confirm the executor's treatment and fix GopherMind if they block), final scan, landing as a diff. Expect 2 to 4 attempts and 1 to 3 days of mini time; each fresh run takes about 1.5 hours to reach the test-writer, so prefer `--resume`.
3. **Inspect the finished run by file:** every function node has every group; root `decisions` equals the question store; `attempts/` holds scrubbed artifacts; `_state/calls.jsonl` and `events.jsonl` exist; no `blackboard.db`; `go build ./... && go vet ./... && go test ./...` pass in the output repo; the server's pipeline endpoints show the run (`e2e_pipeline_test.go` path, or run `gophermind-server` against the run folder). Record attempts and the fix list with commit hashes.
4. **Docs and website (run in parallel with task 2's waits).**
   - `CLAUDE.md` (project): `main` now holds code; remove the docs-only and worktree-only rules; keep the safety rules. `CONTEXT.md`: add a dated section for the merge, the author rewrite, the deleted branch and this goal. `docs/unification-overview.md` and `docs/goals/unification/report.md`: mark A to G merged, replace stale branch hashes using the hash map where it still exists, otherwise by commit subject.
   - Website: inspect `site/` and `docs/` for pages mentioning `/phase`, v1, `.planning`, `briefv2` or the old pipeline contract; update to the unified `build` system and `docs/pipeline-contract.md`; build and check links locally; deploy through the Pages workflow (`gh workflow run pages.yml`) and verify `200` and the changed text on `https://gophermind.com/`.
   - Commit docs with explicit paths; push `main` through the gate.
5. **Regression matrix on `main`:** `go build ./... && go vet ./... && gofmt -l .` and `go test ./... -count=1` in `.`, `gophermind-lib`, `gophermind-osx`, `bubblecomplete`, plus the server and desktop modules (find them via `go.work`); `-race` for server, osx client and executor (executor race may take over an hour: run in the background); the grep gates of task 12 of `docs/GOAL-UNIFICATION.md`; `gophermind brief validate` on both briefs; the offline `brief plan --fake` and `brief run --fake`. Record failures that predate this goal; do not fix them.
6. **Release preparation (no publish).** Decide the version (the `/phase` notice says it was removed in 0.10.0, constant `tui.PhaseRemovedIn`, so 0.10.0); write the `CHANGELOG.md` entry from the commits since v0.9.0; run `scripts/release.sh` steps that do not publish; write the steps John runs (tag, GoReleaser build with the manual `gh release create` workaround from `CONTEXT.md` gotchas, Homebrew casks, npm OTP, server deploy to the mini with `ssh -A`, desktop sign and notarize) into `docs/release-0.10.0.md`. Then ask John once for the OK to tag and publish.
7. **Graded run prerequisites (human).** Check and list what is missing, stop here with everything above done if any is: `~/.gophermind/vault.age` with `DATABASE_URL` and `TEST_DATABASE_URL` and the passphrase in `GOPHERMIND_VAULT_PASSPHRASE`; two empty loopback Postgres 15+ databases (Postgres is on the home box `127.0.0.1:5432`, so a second forwarder like the model one is needed); `gophermind-dev` installed (`scripts/build-dev-binary.sh`); `--require-private` honored by the settings. The vault passphrase is John's: ask only through the prompt the tool gives, never echo it.
8. **The graded run.** Execute `docs/GOAL.md`'s clean-run loop on the AIVS brief to its perfect release on the unified system, local model only, with its own stop rule (20 revisions of the same cause). Record each attempt in `docs/goals/attempts/NN.md` (20 lines at most) and in the report.

## Stop and escalate

Stop and write the reason, evidence and next step to the report when: a prerequisite of task 7 is missing; a task fails its verify after three fix rounds; the home box runs out of memory or swap or llama-server stops answering and `sudo systemctl restart llama-server` over `ssh home` does not bring it back; the VPN path to `10.8.0.6` is down for more than 30 minutes; the csvstat brief cannot be found or rebuilt faithfully; or a step needs an action the rules forbid.

## Known hazards

- A model call that never reaches the server shows as a 25 minute timeout: check the forwarder and `journalctl -u llama-server` on the home box before blaming the model.
- A single-model chain makes one unusable reply (after one repair) fatal to the stage. If most slips are of this kind, consider a router retry on the only model; discuss with John before changing router behavior.
- Tests that run `git` must strip every `GIT_*` variable (hooks export `GIT_DIR`), or they pollute the repo's config and authors.
- The pre-push gate takes about 8 minutes and needs the "iPhone 17" simulator for its iOS step.
