# Goal: every example brief builds, offline, on the local US models

Written 2026-10-28 23:20 PDT for John; revised 23:30 PDT to run overnight. Delegated run: no review gates between tasks, one report at the end. Start it with the `goal` skill: `/goal docs/GOAL-EXAMPLES.md`.

John leaves for a week with no network. Nothing can be fixed once he is gone, so this goal exists to find and fix every failure now, on this laptop, with the same models and settings he will have at sea.

## What done means (all true, with evidence in `docs/goals/examples/report.md`)

1. **Each of the 13 briefs in `examples/briefs/` builds.** One unattended run through the local server (`http://127.0.0.1:8090/app/briefs`) ends with phase `done`, every leaf verified, and every acceptance command passing (the run page's Acceptance section, or `gophermind brief report <run-id>`).
2. **The output is real.** For each run, `go build ./... && go vet ./... && go test ./...` pass in the output repo (`~/.gophermind/local/repos/<run-id>`) with `GOPROXY=off GOTOOLCHAIN=local`, and a spot check of at least two acceptance commands by hand on the built binary gives the result the brief states. No hand edits to the output repo.
3. **A final sweep passes with no changes in between.** On the final commit, all 13 briefs run one after another with no code, config or brief change during the sweep, on a second server instance whose process runs under `sandbox-exec -f scripts/offline.sb` (no network at all), with its own state folders and its own port (never the real ones).
4. **The test suites pass:** `go build`, `go vet`, `gofmt -l` clean and `go test -count=1` for `gophermind-lib` (planner, provider, router, settings, ledger, serve, brief, executor), `gophermind-server` and `cmd`. The executor suite takes about 41 minutes: use `-timeout 60m` in the background.
5. **The system is documented for a week at sea:** `docs/local-setup.md` says what runs where, how to check it, how to restart it, and what to do when a run fails (start from the run page's Errors section).
6. **Everything is local commits.** Nothing pushed, merged, tagged or deployed.
7. **`docs/goals/examples/progress.md` is current**, so John can see at a glance what happened overnight.

## State at the start (verified 2026-10-28 23:15 PDT)

- Code is on `main` at `7772aac`, 18 commits ahead of origin, unpushed. The service installed under launchd is built from that commit (`~/.gophermind/local/INSTALLED_COMMIT`).
- Agents (all `RunAtLoad`, `KeepAlive`): `com.gophermind.local.server` (8090, GopherMind), `com.gophermind.local.llm` (8081, gpt-oss-20b), `com.gophermind.local.embedder` (8083) and `com.gophermind.local.burrow` (8085) are ours or burrow-08's; `com.metaglimmer.serve` (8000, Muse-Glimmer-30B) is metaglimmer-10's. Installers: `scripts/local-service.sh`, `scripts/local-llm.sh`.
- Models, planner config `~/.gophermind/gophermind.yaml`: provider `gptoss` (temperature 1.0, `json_mode: true`, `reasoning_effort: low`) first on every tier, provider `muse` (`reasoning_effort: low`) behind it as the fallback. Backups of earlier configs sit next to it as `gophermind.yaml.bak-*`.
- What is known about the models:
  - gpt-oss-20b is fast (about 44 tokens a second) but at temperature 0.2 it looped and broke JSON. With temperature 1.0 and JSON mode the csvstat contract outline passed (7 of 10 calls usable in 86 seconds). It then failed in the contract repair step: it invented a type `io-reader` for the standard library's `io.Reader` and, told to fix it, wrote the declaration as a comment three times. That call now has JSON mode too (`7772aac`); the Muse fallback is meant to catch what gpt-oss cannot.
  - Muse-Glimmer-30B is slower (about 15 tokens a second, 1 to 3 minutes a planning call at low reasoning) but completed the csvstat contract stage with 4 of 4 usable calls. It rejects `reasoning_effort` values other than none, minimal, low, medium and high, and any `response_format` other than text. It has one named session slot; `GOPHERMIND_SESSION_CLOSE=1` is set in `~/.gophermind/config.json` so each run frees it.
  - `json_mode` makes a server wrap a list in an object, so the planner asks for it only on calls whose reply is one object (the contract outline, contract components, contract repair, enrich structure). Clarify, decompose and per-function enrich replies are lists.
- Baseline: csvstat run `gm-2026-10-29-007` was started at 23:10 on this configuration; its outcome is the first entry of the report. No other brief has been run on the planner yet.
- Memory: 48 GB; Muse about 22.6 GB resident, gpt-oss about 12 GB. With both loaded free memory is near zero and swap is mostly used. Never load a third model.
- Tools you can use: the run page Errors section and "Why the run stopped" (`/app/briefs/<id>`), `GET /app/briefs/<id>/state`, `gophermind brief calls|status|diagnose <id>` (run in the output repo), `_state/replies/*.txt` and `_state/planner_error.json` in the run folder, `run.log`. The 8090 server marks any run it does not own as interrupted when another server starts on the same state folders, so never start a second server on `~/.gophermind/local`.
- Not this goal's: the iOS tests and the pre-push gate (John paused iOS work), the Pipeline page, the example picker on the upload page.

## Rules

- **Models: US-made, official, unmodified weights only.** gpt-oss-20b (OpenAI) and Muse-Glimmer-30B (Meta). No Qwen or other non-US model, no abliterated build, no cloud model, no download of any model, and no network use by a run. If a brief cannot pass without a different model, stop that brief and escalate; do not add one.
- **Fix GopherMind, never the output repo.** Changes go in the planner, executor, router, provider, settings, prompts or serve code, or in `~/.gophermind/gophermind.yaml`. A fix is general: no case for one brief, repair prompts name the defect kind, line and valid values and never quote reply content, and the ledger never stores prompt or reply text.
- **A brief may change only when it is wrong against `gophermind-lib/serve/assets/brief-spec.md`** (the validator, the lint, or a contradiction between its own sections). Keep what it asks for; never weaken an acceptance command to make a run pass. Record every brief change in the report.
- **Test first.** A fix to code starts with a failing test, then the fix, then the affected suites. Run `go vet`, `gofmt -l` and the suites of the packages touched.
- **One writer.** One code writer at a time in this checkout. Read-only diagnosers may run in parallel. Commit per fix with explicit `git add` paths and a message that names the failure it fixes. Never `--no-verify`, never push, merge, tag, release, deploy, or install the desktop app.
- **One run at a time.** The server runs one brief at a time. Reinstall the service with `scripts/local-service.sh install` only between runs: it restarts the server and ends the active run. After a change to contract, decompose or enrich logic start a fresh run (a resubmit with `?resubmit=1` gets a fresh id).
- **Other sessions own their services.** Muse belongs to metaglimmer-10 and Burrow and the embedder to burrow-08. To change them, ask through `SendMessage`; do not edit their agents. Do not stop Muse: it is the fallback.
- **Offline.** Nothing in a run may need the network. Anything that fails with a network error is a bug to fix (`GOPROXY=off`, `GOTOOLCHAIN=local`, `HF_HUB_OFFLINE=1` are already set for the service).
- Guard every recursive delete (assign, check absolute and inside the intended folder, print, then `rm -rf -- "${target:?}"`). Never print a credential or `~/.claude/private/*`. No em dashes, no emojis in anything written.
- **Failure budget:** three fix rounds on the same failure (same stage and same error kind) and that brief is blocked: write `docs/goals/examples/blocked.md` (brief, run ids, the reply or error, what was tried), move on to the next brief, and come back to it once at the end.
- **Hang rule:** a run with no new model call for 15 minutes while a model server is idle is hung: stop it, diagnose, and count it as a failure round.

## Execution mode

Subagent-driven, per the goal skill's rule 4. The orchestrator owns the runs (background processes it polls), the service reinstalls and the commits. Diagnosis fans out to read-only subagents (one per failing run: calls, replies, planner error, the code that raised it). Each code fix is one fresh implementer subagent working test first, then a spec review and a quality review before the commit.

## Overnight run

John is going to sleep and wants this to run all night, until either he wakes and checks on it, or all 13 briefs and the tasks below are complete. Nobody is awake to answer.

- **Never wait for a person.** Do not ask a question and do not stop to ask. Where a question would come up, decide by the tie-breaker and record the decision in the progress file.
- **Power first.** Before anything else run `pmset -g batt`. The run is heavy (two models plus builds) and drained the battery from 76 percent to 7 percent in about an hour on 2026-10-28. If the laptop is not on AC power (`Now drawing from 'AC Power'`), do not start: write that at the top of the progress file and stop. A run on battery ends with the machine off.
- **Keep the machine awake.** The first action is `caffeinate -dimsu -t 43000 & echo $!` (about 12 hours); note the pid in the progress file, confirm with `pmset -g assertions`, and kill it as the last action. The laptop sleeps after one idle minute on battery, and a sleeping laptop stops every service. The lid must stay open (closing it sleeps the machine whatever caffeinate says); that is John's to leave open.
- **Watch the power.** Check `pmset -g batt` every hour. If it is on battery below 25 percent, stop starting new runs, let the active one finish if it can, write the report, and stop with the services left running. Say so at the top of the report.
- **Progress file.** Keep `docs/goals/examples/progress.md` current, and write it after every run result and every fix round, newest first. Its first five lines are always: status (`running`, `done`, `stopped` or `blocked`), the time of the last update, briefs done as `n of 13`, what is happening now, and the next action. Below that, one line per event with the time. John reads this first when he wakes. Commit it with each fix commit and each finished brief.
- **Poll gently.** A run is a background process. Watch it with a loop that sleeps 30 to 60 seconds between looks and ends after 50 minutes, then look again. Do not read huge output files into the conversation; read the run page's state, `gophermind brief calls` and the first lines of an error.
- **When to finish.** Stop when every brief is `done` and tasks 4 to 7 are complete, or a stop condition below fires. If every remaining brief is blocked, write the report and stop; do not repeat the same fixes.
- **Leave it clean.** At the end: the real service reinstalled from the final commit and healthy; the sweep server stopped and its folders removed; the caffeinate stopped; the working tree with nothing uncommitted except John's own files; nothing pushed.

## Tasks

1. **Preflight (15 minutes).** `scripts/local-service.sh status`, `scripts/local-llm.sh status`, and `curl` the health of Muse (`:8000/v1/models`), the embedder (`:8083/health`), Burrow (`:8085/healthz`). Confirm the planner config matches the state above. `gophermind brief validate` every brief: all `ok`, no `lint:` lines. Run the planner, provider, router, settings and serve suites once. Record the baseline run's outcome.
2. **The loop, per brief.** Order: `09-slugify`, `10-wordfreq`, `11-humandur`, `12-roman`, `13-hashsum`, `04-csvstat`, `05-retryx`, `01-linkcheck`, `02-notes-api`, `03-cliphist`, `06-hookrelay`, `07-ledger-sync`, `08-taskboard`. For each:
   - Submit: `curl -s -X POST --data-binary @examples/briefs/<file> -H 'Content-Type: text/markdown' 'http://127.0.0.1:8090/app/briefs?resubmit=1'`.
   - Watch the run page state until the phase is `done`, `failed`, `stopped` or `interrupted`.
   - On anything but `done`, diagnose from the Errors section, "Why the run stopped", `gophermind brief calls <id>`, and the saved replies. Classify it: a harness bug, a model limit, a prompt gap, a settings gap (temperature, tier, token budget, JSON mode), or a brief bug. Make the smallest general fix, test, commit, reinstall, and resubmit the same brief.
   - When a build or test inside the run fails, read `_state` and the leaf's attempts; the fix is to GopherMind's prompts, repair path or settings, not to the generated code.
   - When it is `done`, verify the output by the done-means rules 1 and 2, and add a line to the report: run id, wall time, calls per model, fixes made.
3. **Second pass on blocked briefs** (if any), once.
4. **Regression.** All suites in done-means rule 4, in the background, results in the report.
5. **Final sweep.** Create the state folders `/tmp/gm-examples-sweep/{briefs,repos,ws}`, build the current server to `/tmp/gm-examples-sweep/gophermind-server` and the CLI to `/tmp/gm-examples-sweep/gophermind` (`-buildvcs=false`), and start the server on port 8092 under `sandbox-exec -f scripts/offline.sb` with `PATH` put the sweep CLI first, `GOPROXY=off GOTOOLCHAIN=local GOSUMDB=off HF_HUB_OFFLINE=1`, `GOPHERMIND_LOCAL=1`, `GOPHERMIND_LLM_ENDPOINT=http://127.0.0.1:8081/v1` and its own token. Run all 13 in the same order, changing nothing. Confirm with `lsof` that the server and its children held no non-loopback socket. Stop it and remove the sweep folders (guarded). A failure in the sweep is a failure round: fix, reinstall, and restart the whole sweep.
6. **Documents.** Write `docs/local-setup.md`: the services, ports and agent labels; how to check them (the commands from task 1); how to restart each; what a reboot does and the order things come back; how to read a failed run; which models and why; the memory limit; the "before you leave" checklist. Update `examples/briefs/README.md` with the measured wall time per brief. Add a dated section to `CONTEXT.md`. Commit with explicit paths.
7. **Report.** Write `docs/goals/examples/report.md`: per brief the result, run ids, wall time and fixes; the commits; what is still not proven (a real reboot, a cold model load with no network).

## Stop and escalate

Stop the whole goal, write what you know into the report, and say why, only when:

- a fix would need the network, a non-US model, a model download, or a change to another session's service;
- free disk drops below 50 GB, or the machine becomes unresponsive under memory pressure and a run cannot make progress with only the two models loaded;
- something would be destructive or unsafe (anything outside this checkout, `~/.gophermind`, `/tmp/gm-examples-sweep`, the logs folder);
- more than half the briefs are blocked.

Otherwise do not stop: a blocked brief is recorded and skipped, not an end of the run.

## Tie-breaker

When a decision is ambiguous, choose in this order: keep everything offline and local; use only the two US models; make the smallest general change; prefer what can be undone; prefer what makes the next failure easier to read. Record the choice in the report.

## Commits

Local commits only, with explicit paths, Co-Authored-By trailer per the session's attribution rules, one per fix and one per document. The final report commit comes last.
