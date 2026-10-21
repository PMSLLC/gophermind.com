Superseded by docs/GOAL-FINISH.md (2026-10-20)

# Goal: finish the unification, prove it on the real system, then run the graded build

Set by John on 2026-10-19. Delegated run: no review gates between tasks, one report at the end. Start it with the `goal` skill. It continues `docs/GOAL-UNIFICATION.md`, whose sub-projects A to G are built on the branch; this goal covers what is left: task 11.1 (csvstat smoke), the executor test suite, the push, and task 11.2 (the graded run of `docs/GOAL.md`). The map of every document is `docs/unification-overview.md`.

## What finishing means

All of this is true, with evidence in `docs/goals/unification/report.md`:

1. The csvstat smoke ends `verified` in one unattended `/project` run from a cleared state, on the home-box model, with no hand edits to the output repo.
2. The output repo builds, vets and tests clean, and the file inspection of task 11.1 passes (below).
3. The whole test matrix passes on the branch tip, including the executor suite that currently hangs.
4. The branch is pushed (with John's OK) and the report lists the steps John takes next.
5. The graded AIVS run reached its perfect release by `docs/GOAL.md`'s rule, or the goal stopped at the prerequisite list with everything before it done.

## Inputs and state at the start

- Code: branch `feat/briefv2-planner-core`, worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`. About 17 fix commits sit on top of the pushed tip, not pushed: from `359e8f9` to `3126e08` (repair-prompt hints, lenient reply parsing, package path versus clause, entry point in the contract, vacuity check in coverage, qualifier and file-path normalization, boundary covers tag). This checkout (`main`) holds docs only.
- Model: llama-server (qwen3.6-35b-a3b, ctx 65536, one slot, reasoning off) on the home box, `127.0.0.1:8081` there. The laptop reaches it through `~/.gophermind/mini-tunnel.py` (forwarder on `127.0.0.1:18081`, one `ssh home nc` per connection, 7 minute no-response watchdog). Start it with `nohup python3 -I ~/.gophermind/mini-tunnel.py`. Warm it with `curl http://127.0.0.1:18081/v1/models` before a run: the first connection after idle takes about 13 s and fails the preflight probe.
- Settings: `~/.gophermind/gophermind.yaml` (privacy `private_only`, one provider `mini`, one model in every tier, `call_timeout_seconds` 1500). Do not add a cloud model: the graded run uses only the local model.
- Smoke inputs: brief `/tmp/csvstat-smoke-brief.md` (target; never edit it to make a run pass), latest run repo `/tmp/csvstat-smoke-repo9`. Make the brief and repo again from `~/src/csvstat` if `/tmp` is gone.
- Latest result: the planner stages (load, clarify, confirm, contract, decompose, enrich, coverage, approve) pass; the test-writer was running; the executor has never built or verified a function in any run.

## Rules

- The rule of this whole effort: fix GopherMind, never the target. When a run stops, find the failing reply under `<repo>/.gophermind/<id>/_state/replies/`, reproduce the validator failure, fix the cause in the planner or executor, add a test, rebuild, resume.
- A fix must be general, not a patch for csvstat. Good fixes so far: a repair prompt names the defect kind, line and column or valid values and never quotes reply content (public providers may see node-scope calls); a parser accepts what a reasonable model writes when the meaning is unambiguous; a check the executor makes is also made at plan time through one shared function.
- Subagent-driven: one fresh Sonnet implementer per fix, TDD (watch the new test fail first, and say so if it was not seen failing), commit with explicit `git add` paths and the two attribution lines. Run planner tests with a clean home: `HOME=$(mktemp -d) go test ./build/planner/...`.
- Build the binary from the branch tip for every attempt: `go build -o /tmp/gm-preN ./cmd/gophermind` in the worktree. Never run an old binary against a newer fix.
- Resume rules. A change to the contract, decompose or enrich logic needs a fresh run: new repo (`git init -b main` plus an empty commit), and delete `~/.gophermind/runs/gm-2026-09-29-003.json` first. A change to the test-writer, coverage or later logic can use `--resume` on the same repo, and then the run record must NOT be deleted (resume needs it; it holds `run_id`, `repo`, `run_dir`, `brief_path`, `started_at`). A failed enrich is retried by moving `_state/enriched.json` aside, then `--resume`.
- Do not push, merge, tag, deploy or install without John's OK in this session; an earlier OK covered one push only. The git wrapper blocks `push`, `checkout`, `switch`, `stash`, `reset`, `clean`, `restore`, `branch`, `worktree` without `--gw-force`; never force without asking.
- Guard every recursive delete (assign, check absolute and inside the intended folder, print, then `rm -rf -- "${target:?}"`). Never print a credential or `~/.claude/private/*`. No em dashes, no emojis.
- Stop after three fix rounds on the same failure and write `docs/goals/unification/blocked.md` with the reply path, the validator, and what was tried.

## Tasks

1. **Executor test suite.** The rig-based executor tests (for example `TestGreeterFixturePlanIsApproved`, `TestLoadPlan*`, and `TestProjectE2EGreeter` in `projectrun`) hang in the planner's router cooldown wait, with or without the recent fixes. Find the cause first: suspect the scripted fixture replies versus the prompts and stages added since (entry point stage `contract:outline:entry`, the normalization and vacuity changes). Fix the fixtures or the code. Verify: `gt ./gophermind-lib/build/executor/... -timeout 30m` and `./gophermind-lib/build/projectrun/...` pass. This runs before the smoke so a fixture break cannot hide a real regression.
2. **Finish the csvstat smoke.** Rebuild from the tip, warm the tunnel, run `project /tmp/csvstat-smoke-brief.md --repo <repo>` unattended (resume where the rules allow). Loop: stop, diagnose, fix GopherMind, rebuild, continue. Expected remaining ground: the executor's wave builds, the leaf ladder on the local model, acceptance with the built binary (the brief's own bullets begin with `go run .`, which the harness forbids: confirm how the executor treats such bullets and fix GopherMind if they block a pass), final scan, landing as a diff.
3. **Inspect the finished run** by file, as task 11.1 says: every function node has every group; the root `decisions` equals the question store; `attempts/` holds scrubbed artifacts; `_state/calls.jsonl` and `events.jsonl` exist; there is no `blackboard.db`; `go build ./... && go vet ./... && go test ./...` pass in the output repo; the server's pipeline endpoints show the run (run `gophermind-server` against the run folder, or the `e2e_pipeline_test.go` path). Record the result, the attempt count and the list of GopherMind fixes this took.
4. **Regression matrix** on the tip: `go build ./... && go vet ./... && gofmt -l .` in `gophermind-lib`, `gophermind-server`, `desktop`, `gophermind-osx`, `cmd`, `gophermind`; `go test ./... -count=1` in each (add `-race` for server, osx client, executor); the grep gates of task 12 of `docs/GOAL-UNIFICATION.md`; `gophermind brief validate` on both briefs; the offline `brief plan --fake` and `brief run --fake` checks. Record every failure that existed before this goal, do not fix those.
5. **Report and ask to push.** Update `docs/goals/unification/report.md` (150 lines at most): the smoke result, the fix list with commit hashes, the matrix result, rulings, what is undone. Then ask John once for the OK to push the branch (through the wrapper with `--gw-force`). Push only after the answer.
6. **Graded run prerequisites (human).** Check and list what is missing; stop here with everything above done if any is: the vault `~/.gophermind/vault.age` with `DATABASE_URL` and `TEST_DATABASE_URL` and its passphrase in `GOPHERMIND_VAULT_PASSPHRASE`; two empty loopback Postgres 15+ databases (Postgres listens on the home box `127.0.0.1:5432`, so a second forwarder like the model one is needed); `gophermind-dev` installed; `--require-private` honored by the settings. The vault passphrase is John's: ask for it only through the prompt the tool gives, never echo it.
7. **The graded run.** Execute `docs/GOAL.md`'s clean-run loop on the AIVS brief to its perfect release on the unified system, local model only, with its own stop rule (20 revisions of the same cause). Record the run in the report.

## Stop and escalate

Stop and write the reason, the evidence and the next step to the report when: a prerequisite of task 6 is missing; a task fails its verify after three fix rounds; the home box runs out of memory or swap or llama-server stops answering and `sudo systemctl restart llama-server` over `ssh home` does not bring it back; the VPN path to `10.8.0.6` is down for more than 30 minutes; or a step would need an action the rules forbid.

## Known hazards

- A model call that never reaches the server shows as a 25 minute timeout: check the forwarder and `journalctl -u llama-server` on the home box before blaming the model.
- A single-model chain makes one unusable reply (after one repair) fatal to the stage. If most slips are of this kind, consider making the router retry the only model once more instead of excluding it; discuss with John before changing router behavior.
- Each fresh run takes about 1.5 hours to reach the test-writer on this model. Prefer `--resume` whenever the rules above allow it.
