# Goal: reach the summit (finish all outstanding work and planned features)

Written 2026-10-23 for John. Companion to `docs/ROADMAP.md` (the plan and the five summit statements) and `docs/GOAL-FINISH.md` (the earlier engine goal, whose release and graded-run tasks are folded in here). Start it with the `goal` skill. Delegated run: no review gates between tasks, one report at the end, `docs/goals/summit/report.md`.

## What finishing means (each true, with evidence in the report)

1. The csvstat brief runs unattended on the server and ends `verified` with acceptance 6 of 6, with no hand edits to the output repo.
2. A brief uploaded in the browser at https://gophermind.brahy.com runs to completion on the server, and every step is visible without the terminal: decomposition, per-leaf attempts, wave checks, repairs, acceptance, clarify questions.
3. Sign-in is Google only (`@brahy.com`). The temporary password login is removed from code, config and server.
4. GopherMind and Burrow are joined: the contract stage emits Burrow contracts, the leaf stage looks up and stores, the interface shows each leaf's `origin`, and a second brief reuses a function the first produced, with its tests passing.
5. 0.10.1 is released (GitHub, Homebrew, npm) with the deploy and installs done.
6. The graded AI Venture Studio run reached its perfect release by `docs/GOAL.md`, on the mini's local model only, or the goal stopped at the prerequisite list with everything before it done.

## State at the start (verified 2026-10-23)

- `origin/main` is `ac1990c` and is what `development-jb` runs. Local, unpushed: `c62daee` and `faf8562` and `a1efc2a` (enrich rationale errors and trim, single-model parse retry), plus the two roadmap commits already pushed.
- Server: https://gophermind.brahy.com on OCI `development-jb` (1 OCPU, 7 GB, shared with Burrow's sandbox). Google sign-in is live (client in `/opt/gophermind/.env`); the temporary password login (user `john`) is still on. A systemd drop-in `/etc/systemd/system/gophermind.service.d/state.conf` gives the service a writable state dir.
- Phase 2 (upload, one-at-a-time launch, stop, state JSON, run pages) is built and pushed but OFF on the server: it needs `GOPHERMIND_BRIEFS_DIR`, a launcher as `gmrun`, and a browser check. Not built: answering clarify questions in the browser, a live event stream, separate wave check and repair sections.
- csvstat: run `gm-2026-09-29-003` in `/home/gmrun/csvstat-run5` was planned with `ac1990c`, resumed after an enrich failure, and was in the test-writer stage. Earlier run: 5 of 5 leaves, 3 of 3 wave checks, acceptance 3 of 6, because the built `run` leaf ignored the brief (`-f` for `--format`, `-` not stdin). Fixed by `7f83b9f`, `3c55ffa`, `d24c92b`, `ac1990c`; real result not yet known.
- Burrow (own session and repo, private PMSLLC/burrow): phases 1 and 2 done and deployed, phase 3 on its last task, phases 4 to 8 not started, no network API yet. Eligible for storage: pure Go leaves, stdlib only, tests expressible as vectors. `origin` values agreed: `built`, `reused` (with key and version), `lookup_missed`, `not_eligible`.
- Known test hazard: `TestParallelDeterministicCommitOrder` in `build/executor` hangs when run alone, with or without recent changes. The executor suite needs `-skip TestParallelDeterministicCommitOrder` and a long timeout until it is investigated (task 5).

## Rules

- Subagent-driven, one code writer at a time per repo, a commit per task with explicit `git add` paths, fresh subagent per task, tests first. The Burrow repo belongs to the Burrow session: sync by message, never edit it.
- Fix GopherMind, never the target repo. A fix is general; repair prompts name the defect kind and valid values and never quote reply content.
- The graded run and its rehearsals use only the mini's local model. No cloud model in those settings.
- Deploy by git only (`git fetch` + `git checkout <ref>` on the target, `ssh -A`, ubuntu@10.40.1.202 with `-i ~/.ssh/oci-prod`, private GOCACHE, back up the old binaries with the old short sha). Verify after deploy: service active, `/healthz` 200, `/auth/login` redirects to Google.
- Guard every recursive delete. Never print a credential, the Google client secret, the session secret or `~/.claude/private/*`. Secrets never in tracked files. No em dashes, no emojis in anything written.
- Never `--no-verify`. Pushes use `--gw-force` and run the pre-push gate (about 10 minutes); if the push dies with rc 141 after the gate passed, rerun it in the foreground.
- Do not run heavy jobs on `development-jb` when the Burrow session has told you its sandbox is busy, and tell it when a run ends.

## Authorized by this goal

- Pushing `main` through the gate, and deploying the pushed commit to `development-jb` by git, after each task that changes server behavior and its tests pass.
- Editing `/opt/gophermind/.env` on the server for names listed in this document (`GOPHERMIND_BRIEFS_DIR`, `GOPHERMIND_BRIEF_CMD`, `GOPHERMIND_BRIEF_KILL_CMD`) and removing the `GOPHERMIND_WEB_*` variables in task 2, with a backup first.
- One narrow sudoers rule on `development-jb` letting user `gophermind` run only the brief commands as `gmrun` (exact commands in `docs/SERVER-SETUP.md`), nothing broader.

Not authorized (wait for John): tagging or publishing a release, npm publish, installing the desktop app, the graded run itself, starting Burrow's HTTP service, any cloud model, any change to the Google OAuth client.

## Tasks, in this order

1. **Finish csvstat.** Check the run in `/home/gmrun/csvstat-run5` (`gophermind brief status gm-2026-09-29-003`). If planning finished, `brief run` it as `gmrun` and read `report.json`. Failures: diagnose from `attempts/<leaf>/<n>` and `acceptance.json` before editing; fix the planner or executor generally; re-plan from a fresh repo. Done when acceptance is 6 of 6, or a new named blocker is in `docs/goals/summit/blocked.md`. Then push the three enrich and router commits and deploy.
2. **Retire the temporary login.** Confirm with John that Google sign-in works for him (it did on 2026-10-23), then follow the delete list in the header of `gophermind-lib/serve/passwordauth.go` (file, test, login asset, env wiring in `gophermind-server/main.go`, the `hashpw` subcommand), remove `GOPHERMIND_WEB_*` from the server env, update `docs/SERVER-SETUP.md` and the changelog. Done when `/auth/login?local=1` is gone and Google sign-in still works for an `@brahy.com` account and refuses another domain.
3. **Turn on brief upload.** Apply the sudoers rule, set the three env names, deploy. Verify in a real browser at phone and desktop widths (Claude in Chrome): upload the csvstat brief, watch the tree and statuses update, stop a run, refuse a second concurrent run. Check the process group of a `sudo`-launched command is actually killed by stop. Done when csvstat runs end to end from the browser.
4. **Finish the run view.** Answer clarify questions in the browser (so a run that needs a human continues instead of failing at the file gate), a live event stream instead of polling, wave checks and repairs as their own sections, per-leaf model and provider on each attempt. Done when a run with a clarify question can be answered and resumed from the page.
5. **Engine hardening.** Investigate and fix the `TestParallelDeterministicCommitOrder` hang so the executor suite runs whole. Record the malformed reason in `attempt.json`. Add the new routes to the `NewMux` route table comment. Add a brief-consistency test: every CLI token in a brief appears in the built `run` leaf's tests. Done when the full matrix passes with no skip, including `-race` for server, osx client and executor.
6. **Release 0.10.1.** Prepare per `docs/release-0.10.0.md` and `gophermind-release-procedure`: changelog, version, gate, build assets. Stop before tag and publish for John's word; npm needs his OTP.
7. **Join Burrow.** When the Burrow session sends its Phase 3 CLI output and response shape, and John approves the HTTP service and the local model for Burrow Phase 4: the contract stage emits `burrow-contract/v1` contracts for eligible leaves; the leaf stage does lookup (exact key now, embeddings after Phase 4), writes a hit through the same gates as a built leaf, calls it with the stored parameter order, and stores a pure Go result with vectors on a miss; the run report lists reused leaves; the interface shows `origin` and the lookup trail. Done when a second brief reuses a function from the first, shown in the interface, tests passing.
8. **Graded run.** Needs John's prerequisites (`~/.gophermind/vault.age`, `gophermind-dev`, the Postgres forwarder). Run the clean-run loop of `docs/GOAL-UNIFICATION.md` task 11.2 on the mini model only. Done when the perfect-release rule of `docs/GOAL.md` is met, with the report listing any Burrow reuse.
9. **Operate and tidy.** Laptop WireGuard persistence; remove the stale `ssh home` note; delete `/tmp/homebrew-tap` (guarded); update `docs/ROADMAP.md` states and dated log as each task ends; refresh memory notes.

## Waiting on John (ask once, early, in one message)

- Burrow: OK for its HTTP service, and the local model for its Phase 4.
- The graded-run prerequisites (task 8).
- npm OTP, the go for tag and publish, the desktop install.
- ClinePass terms for automated use.
- Change the temporary web password now (it was shown once in chat); it goes away in task 2.

## Stop and escalate

- Three fix rounds on the same failure: write `docs/goals/summit/blocked.md` (reply path, validator, what was tried) and move to the next task.
- Anything that needs a not-authorized action: stop that task, say exactly what is needed, continue with tasks that do not depend on it.
- Server load: if `development-jb` is starved, pause and message the Burrow session.

## Known hazards

- One local model: one unusable reply used to end a stage. `faf8562` gives the only model two retries; if stages still die this way, record the reply path and the parser message.
- Planning csvstat takes about 40 minutes; a run about an hour. Use background jobs and wait for completion notices; do not poll.
- The `brief` heuristics read flags from code spans and exit codes from "exit ... code" text. A brief written another way gets no CLI check; note it, do not widen the heuristic without a failing real brief.
- Browser rendering of the pages has never been looked at; treat task 3 as the first real check.

## Record keeping

Update `docs/ROADMAP.md` (states, dated log) as each task ends. The report at the end lists, per task: commits, test outcomes, deploy sha, evidence for each of the six statements, and what was not done and why.
