# `/project` as the single entry point: Design

Status: draft for review. Date: 2026-09-30.
Scope: GOAL task 4 (`docs/GOAL.md` in the main checkout, authoritative): `/project` takes a brief and plans and builds it, with no second command and no human step, using the v2 planner and the v2 executor. Also the support task 5 needs: clear-and-rerun, the binary used by a graded attempt, and the list of things a human must provide before one.
Builds on: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (planner), `docs/superpowers/specs/2026-09-30-v2-executor-design.md` (executor; `executor.Run(ctx, Options)` and its Options are fixed there, and R11 "brief run is resume-safe").

## 1. Why this exists

GOAL.md defines one graded attempt as one `/project` run from a cleared state to a finished build, in one process, with no restart, no resume and no hand edit. An attempt also fails when GopherMind stopped to ask a human. Today that sentence cannot be satisfied, because `/project` is the v1 planner and the v2 planner and executor sit behind three separate commands that each stop for a human. A rehearsal of `brief plan` on the real brief (`gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md`, `on_ambiguity: halt`, `milestone_approvals: true`, four declared secrets) showed two stops: Load refused to start until every declared secret was in the vault ("secret DATABASE_URL is not in the vault: run `gophermind brief vault set`"), and Clarify asked one question, wrote `QUESTIONS.md` and halted, although the question already carried `default_if_unanswered`.

This document decides how one command removes every such stop without hiding one: each human decision is either taken by a stated unattended rule and recorded, or is a prerequisite that a preflight checks before any model call.

## 2. What exists today

All of this is read from the code on branch `feat/briefv2-planner-core`.

**v1 `/project` (TUI only).** `gophermind-lib/tui/commands_registry.go` registers `/project <name> <brief>` ("plan a brief into phases, tasks and steps, then approve and export it") and `/project-execute`. `update.go` routes both. `tui/project.go`: `parseProjectCommand` splits a name from a brief path; `startProject` reads the brief file (any text, no front matter), scaffolds `.planning/` through `phaseflow`, and `startPlanning` runs `plan.RunPass1` (skeleton) and `plan.RunPass2` (specify every step) from `gophermind-lib/plantree/plan` on a goroutine, under a run lock, with the session's own agent client (`m.planClient()`), sized to that client's context window. The plan tree lives in `.planning/plan` (`plantree`). Open questions go to a question round (`tui/questions.go`, `/questions`). `tui/approve.go` shows a summary, waits for y/n/revise typed by a person, and exports `ROADMAP.md` and `.planning/assignments.json` (`plantree/export`). Building is a second command: `/project-execute` (`tui/execute.go`) runs each pending assignment through `phaseflow` and `orchestrate`, each in a fresh agent context. There is no contract stage, no coverage check, no test-writer, no blackboard, no ledger, no sandbox, no router, no acceptance proof, and no git landing.

**There is no `gophermind project` subcommand.** `cmd/gophermind/main.go` dispatches `brief` (line 615, before `cfg.Validate`) and `phase`; `project` is not a verb. `gophermind-lib/project` is unrelated (repo context and instruction loading).

**Who else depends on v1.** `gophermind-lib/serve/pipeline*.go`, `gophermind-osx/` (desktop pipeline panel and client) and `cmd/gophermind/phase.go` import `plantree`, `phaseflow` or `orchestrate`. `/phase` uses the same packages. Deleting v1 would break the server, the desktop app and `/phase`.

**v2 today.** `gophermind brief plan <brief> [--yes] [--gate terminal|file]` runs Load, Clarify, Contract, Decompose, Coverage, Approve, Test-writer (`planner/planner.go` `stages`) and exits 0, or exits 3 when the file gate is waiting. `brief resume <id>` continues it. `brief run <id>` (executor plan Task 15) builds it and is resume-safe. Facts in the planner that matter here:

- Load (`planner/load.go`) resolves the repo from the brief's own `repo:` field (`resolveRepo`); there is no override. The testdata brief says `~/src/venture-studio-server`, which is not the GOAL target `~/OtherProjects/AIVentureStudio`.
- `storeSecrets` copies `vault.HarnessScope` values into `vault.RunScope(<id>)`, prompts a person when `Deps.PromptSecret` is set, and otherwise refuses to start. A value already in the run scope is left alone.
- `clarify` with `on_ambiguity: assume_and_document` takes each question's `default_if_unanswered` (or a fixed conservative sentence), marks the answer `Assumed`, and never asks. Any other value goes to `Gate.Ask`; the file gate writes `QUESTIONS.md` and returns `human.ErrWaiting`. `callAsking` (used by Contract and Decompose for a mid-stage `QUESTION:` reply) does the same split.
- `approve` writes `approval.json` with `approved_by` and a `plan_hash` (`RenderPlan`: the summary plus the bytes of contracts, drafts, classes, coverage, answers, dependencies). With no `--yes` it asks `Gate.Approve`; `Decision.By` becomes `approved_by`.
- `brief.Front.MilestoneApprovals` is parsed and used by nothing. Neither planner nor executor has a milestone gate.
- `settings.Load` writes a default `gophermind.yaml` when none exists. The default `standard` chain includes a public provider (`kilo`), and the default `toolchain.PATH` is `/usr/local/go/bin:/usr/bin:/bin`, which does not contain this machine's `go` (`/opt/homebrew/bin/go`).
- `planner.Options.Yes` records `approved_by: flag`; there is no unattended notion.

**State a run leaves** (planner and executor specs): in the target repo `.gophermind/<id>/` (run folder) and `.gophermind/<id>-scratch/`, plus the git branch `gm/<id>`; under `~/.gophermind` (or `GOPHERMIND_CONFIG_DIR`) `blackboard.db` (global SQLite: `rows`, `events`, `calls`, keyed by run id), `runs/<id>.json`, `vault.age`, `gophermind.yaml`, and `gomodcache/`.

## 3. Goals and non-goals

Goals:

- One entry point, `projectrun.Run`, used by the slash command `/project <brief>` and the CLI `gophermind project <brief-path>`, running load, clarify, contract, decompose, coverage, approve, test-writer, then `executor.Run`, then one final report, in one process.
- No step in a healthy run needs a human, a restart or a resume. Every decision that used to need a human is either taken by a written rule and recorded, or checked by a preflight first.
- A preflight that fails fast, before any model call and without writing anything, with the exact list of what is missing.
- A clear-and-rerun contract exact enough that an orchestrator removes precisely the state of one attempt.
- The binary that runs a graded attempt states its own version and commit in the report.

Non-goals: see section 13.

## 4. Architecture

New package `gophermind-lib/briefv2/projectrun`. Dependencies point downward to `planner`, `executor`, `report`, `settings`, `vault`, `router`, `blackboard`, `ledger`, `db`, `human`, `events`, `brief`, `sandbox`, `gitland` (for `ValidateLanding`), and `version`.

```text
projectrun   ParseArgs, Preflight, ResolveSecrets/ProvisionRunScope, unattendedGate,
             Run (plan then build then report), StatePaths, ProjectReport, progress sink
cmd/gophermind/project.go       `gophermind project ...`: flags in, exit code out (thin)
gophermind-lib/tui/project_v2.go  `/project ...`: same ParseArgs, same Run, output to the transcript
```

```go
func ParseArgs(args []string, allowAttended bool) (Options, error) // the one parser, CLI and TUI
func Run(ctx context.Context, o Options, env Env) Result           // never panics, never returns before the report is printed
type Result struct { ExitCode int; Status, StopReason string }
```

`Env` holds the seams tests replace (settings and providers, vault opener, database opener, clock, version). Production uses `DefaultEnv()`, which loads `gophermind.yaml` through `settings.Load` (only after preflight has proved the file exists), builds providers with the vault as `brief plan` does, opens `db.DefaultPath()`, and reads `version.Version`, `Commit`, `Date`.

## 5. What one `/project` run does

```text
1  ParseArgs, then brief.Parse(<brief>)                       invalid brief: exit 2
2  Preflight (section 7)                                       any failed check: print the list, exit 6
   --print-state-paths / --preflight-only stop here with their own output
3  Provision secrets into the run scope (section 8.1)
4  Open db, router (ledger), blackboard, sink
5  planner.Run(ctx, {BriefPath, Repo, Unattended: true})       Load, Clarify, Contract, Decompose, Coverage, Approve, Test-writer
   (unattended gate: approval.json by "unattended", bound to the plan hash)
6  executor.Run(ctx, {RunDir, Repo, Gate: unattended gate, ...})  waves, repair, acceptance, go mod verify, landing
7  Write <run>/project.json, print the final report, return the exit code
```

Steps 5 and 6 are two calls in one process with one database handle and one router; nothing is written between them that a restart would read. A planner stage error stops the run at step 5 with `stop_reason: plan:<stage>`, the report is still written and printed, and the executor is not called. The planner can never return `Waiting` here (no gate asks); if it does, the run ends `failed` with `plan:waiting` and that is a defect, not a prompt.

`--resume` (R8) changes step 3 (existing run-scope values are kept), step 5 (`planner.Run` with `RunID`, which skips finished stages) and records `resumed: true`. Without it, any leftover state is a preflight failure, so a healthy graded attempt cannot resume by accident.

## 6. The two forms and the exit codes

Form A, slash command in the TUI: `/project <brief-path> [flags]`. Form B, CLI: `gophermind project <brief-path> [flags]`. Both call `ParseArgs` then `Run`; the TUI prints to the transcript through a sink, the CLI to stderr (progress) and stdout (report). Flags:

```text
--repo <path>               target repo; default the brief's repo: field (recorded, both values, in the report)
--generate NAME=KIND        make a value for a declared secret when the vault has none; KIND is hex32 or placeholder (repeatable)
--require-private           preflight fails unless privacy.mode is private_only and every tier entry is a private provider
--expect-head <rev>         preflight fails unless HEAD equals <rev> (GOAL: goal-baseline)
--expect-binary-commit <sha>  preflight fails unless this binary was stamped with that commit
--resume                    continue an unfinished run of this brief; without it leftover state is refused
--attended                  CLI only: terminal gate for questions, approval and escalations; prompts for missing secrets
--preflight-only            run the preflight, print it, exit 0 or 6
--print-state-paths         print every path of section 9 for this brief and repo, exit 0
```

Exit codes: `0` verified; `1` failed or harness fault (includes every planner stage failure); `2` invalid brief or bad usage of a brief field; `3` not used (the file gate is not offered); `4` an escalated leaf or a stop (executor); `5` interrupted, resumable; `6` preflight failed (nothing started). The executor's status maps as `verified 0, failed 1, escalated 4, interrupted 5` (executor spec 19). Usage errors exit 1 like `brief`.

## 7. The preflight

`Preflight` makes no model call, opens no database, creates no file, and never reads a secret value into an error. Each check yields `{Name, OK, Detail, Fix}`. All checks run (no early exit) so one run lists every missing item.

| Check | Fails when | Fix text names |
|---|---|---|
| repo | not a directory, not a git worktree, base branch missing, HEAD not on the base branch, tree dirty (ignoring `.gophermind/`), `--expect-head` differs | the path, the branch, the rev |
| stale state | `<repo>/.gophermind/<id>` exists, or branch `gm/<id>` exists, and `--resume` was not given; or `--resume` was given and the run folder is missing | the clear procedure (section 9) |
| landing | `gitland.ValidateLanding(front.landing)` fails | the brief field |
| tools | `go` or `git` not found on `toolchain.PATH`; on darwin `sandbox.Preflight` fails and `executor.sandbox` is not `off` | the setting |
| settings | `gophermind.yaml` missing (it is never created by a preflight), invalid, or a tier chain cannot be resolved; with `--require-private` a public entry or `privacy.mode != private_only` | the setting |
| providers | a provider used by a tier is not reachable by a TCP dial of its `base_url` host and port within 5 s (no HTTP request, no model call) | the URL |
| binary | `--expect-binary-commit` given and `version.Commit` does not start with it, or `Commit` is `none` | rebuild with the dev script |
| vault | the brief declares a secret and `GOPHERMIND_VAULT_PASSPHRASE` is empty; or the vault file is missing or does not open with it (only when some secret has no `--generate`) | the env var |
| secret S | S has no harness-scope vault value and no `--generate S=...` (attended runs may prompt instead) | `gophermind brief vault set S` |
| database URLs | a secret whose value parses as a `postgres` or `postgresql` URL names a non-loopback host, or is not reachable by TCP dial, or two such secrets name the same host, port and database | loopback or tunnel, distinct databases |
| warnings | (not a failure) `brief.UndeclaredSecrets` is non-empty: printed as `warning:` lines | |

Preflight failure is not a run failure (R7): exit 6, `status: preflight_failed`, no run folder, no ledger row, no `project.json` (the report goes to stdout only). A graded attempt that exits 6 has not started, so the orchestrator fixes the named items and runs again; the attempt count is unchanged. A database check cannot prove the databases are empty (there is no Postgres driver in this module and none is added); the preflight prints `note: database emptiness is not checked` and emptiness is part of the clear procedure.

## 8. The unattended policy

Unattended is the default (R3): `/project` means no human step. `--attended` (CLI only) swaps in the terminal gate and secret prompts and changes nothing else. The mode is recorded in the report. There is no `--yes`: an unattended approval is not an unseen approval, it is a stated rule that binds to a hash.

### 8.1 Secrets (3a)

Two provisioning sources only, resolved per declared secret in this order: (1) the vault's harness scope, set by a person with `gophermind brief vault set NAME` (value from a terminal or stdin, passphrase from `GOPHERMIND_VAULT_PASSPHRASE`); (2) a value GopherMind generates when the command line says so with `--generate NAME=KIND`: `hex32` is 32 random bytes as 64 hex characters, `placeholder` is `gm-placeholder-` plus 32 hex characters. A vault value beats a generated one. Nothing else makes a value: GopherMind never guesses a connection string, never reads a value from the environment, never takes one from a file in the repo.

At the start of a fresh run `ProvisionRunScope` writes every resolved value into `vault.RunScope(<id>)`, overwriting whatever an earlier attempt left there, so a changed harness value is never masked by a stale copy. With `--resume` existing run-scope values are kept (a regenerated value could differ from the one a half-built server already used). The report lists names and sources (`vault`, `generated:hex32`, `generated:placeholder`, `prompt`), never values; a canary test greps everything.

What the real brief needs, read from its Environment, Architecture and Acceptance text:

| Secret | What the acceptance actually uses | Ruling |
|---|---|---|
| `JWT_SIGNING_KEY` | HMAC key for tokens; every bullet that logs in uses one server instance, any non-empty key works | GopherMind generates: `--generate JWT_SIGNING_KEY=hex32` |
| `STUDIO_LLM_API_KEY` | only set as a required server variable; the end-to-end bullet points `STUDIO_LLM_BASE_URL` at the server's own `venture-server fake-llm`, and the network block says "tests use a fake" | GopherMind generates: `--generate STUDIO_LLM_API_KEY=placeholder`. A real key in the vault overrides it |
| `DATABASE_URL` | `migrate up` on an empty database, `seed-templates`, `serve`, every round trip | human prerequisite: a Postgres 15 or later database, empty at the start of each attempt |
| `TEST_DATABASE_URL` | `go test ./... -tags integration`; must be a different database from `DATABASE_URL` so the migrate bullet still sees an empty one | human prerequisite, a second empty database |

Why the databases are a human prerequisite: GopherMind has no Postgres driver (no new dependency) and the executor starts no database (executor spec 16 risk d). Why they must be loopback: the sandbox denies every non-loopback connection (executor spec 6.1), so a URL at `192.168.1.35` fails inside the acceptance commands. A database that lives on the mini is reached through `ssh -N -L 127.0.0.1:<port>:127.0.0.1:5432 mini`, which keeps it off the laptop, as CLAUDE.md requires, while the URL names `127.0.0.1`. The preflight enforces both points.

### 8.2 Clarify under `on_ambiguity: halt` (3b)

Ruling: when unattended, every question's `default_if_unanswered` is taken as its answer, each is written to `answers.json` with `Assumed: true` (existing fields: id, stage, question, answer, assumed), and all are listed in `project.json` and counted in the printed report. Nothing is invented: the Clarify reply parser, when `Options.Unattended` is set, rejects a question without a non-empty `default_if_unanswered`, which takes the router's normal malformed retry; if the retry still lacks one, the Clarify stage fails and the run ends `failed` with `plan:clarify`. A mid-stage `QUESTION:` reply (Contract and Decompose, which carry no default) uses the planner's existing assume-and-document branch: the model is told to choose the most conservative option and record it in the node's `assumptions`, which `RenderPlan` lists under "Assumptions" and which the plan hash covers. The report counts both kinds. The brief's `on_ambiguity: halt` is reported as overridden, with the counts, so the override is visible.

A justified alternative was considered: refuse to run unattended when the brief says `halt`. It would make this brief unbuildable by the goal's own rule (no human step) and buys nothing, because the approval summary already shows every assumption and the acceptance bullets are the contract.

### 8.3 Approval gates (3c)

Plan approval: the unattended gate's `Approve` returns `Approved: true, By: "unattended"` only when the summary's `Requirements covered: C of N` line has `C == N` and `N > 0` (belt and braces over the Coverage stage, which already stops on a gap). The planner writes `approval.json` itself, `approved_by: unattended`, with the `plan_hash` of the exact plan; `executor.LoadPlan` still verifies it, so a changed plan still invalidates it. Warnings in the plan are allowed and counted in the report.

Milestone approvals: `milestone_approvals: true` is declared by the brief and no code consumes it. Ruling: unattended, it is satisfied by the plan approval and the report says so in one line (`milestone_approvals: declared; the executor has no milestone gate; covered by the unattended plan approval`). The run is not refused and the line is not hidden. A future milestone gate must read the flag; until then the statement is the honest one.

Escalations: the gate's `Escalate` returns `stop` (R14 of the executor spec), so an escalated leaf ends the run `escalated`, exit 4.

### 8.4 What stops an unattended run (3d)

Nothing below is papered over: each produces a status, a `stop_reason`, an exit code and the report.

| Condition | Where | Status, exit | `stop_reason` |
|---|---|---|---|
| preflight item missing | before step 3 | `preflight_failed`, 6 | `preflight` |
| invalid brief | step 1 | 2 | `invalid_brief` |
| Clarify without defaults after retry; contract or decompose or test-writer failure | planner | `failed`, 1 | `plan:<stage>` |
| coverage gap after `max_coverage_rounds`, or approval refused for coverage | planner | `failed`, 1 | `plan:coverage`, `plan:approve` |
| contract problem on a leaf, a leaf that exhausts its ladder | executor | `escalated`, 4 | executor's (`human_stop`) |
| a leaf blocked by one that failed | executor | `failed` or `escalated` | executor's, leaf named |
| acceptance bullet failing after its repair rounds | executor | `failed`, 1 | executor's |
| sandbox refused at run time, an unattributable integration failure, landing blocked | executor | `failed`, 1 | executor's |
| SIGINT, SIGTERM, `max_run_minutes` | both | `interrupted`, 5 | `interrupted` or `max_run_minutes` |

## 9. State, clearing and rerun (item 4)

`/project` creates exactly this. "Per-project" means keyed by the brief id (for the real brief `gm-2026-09-29-002`); "global" means shared by every run on the machine.

| # | Path | Scope | On a graded clear |
|---|---|---|---|
| 1 | `<repo>/.gophermind/<id>/` (brief.md, contracts.json, tree/, approval.json, coverage.json, answers.json, report.json, acceptance.json, project.json, proxy.log, bin/, _state/) | per-project, in the repo, git-excluded | removed by `git clean -fdx -e .remember` (the GOAL command) |
| 2 | `<repo>/.gophermind/<id>-scratch/` | per-project, in the repo | removed by the same command |
| 3 | `<repo>/.git/info/exclude` line `.gophermind/` | per-repo | keep (harmless, `rundir` re-adds it) |
| 4 | git branch `gm/<id>` in the repo | per-project | **not removed by reset or clean**: delete it (below) |
| 5 | repo work tree and HEAD | per-project | `git reset --hard goal-baseline` (GOAL) |
| 6 | `~/.gophermind/runs/<id>.json` | per-project, global folder | delete the file |
| 7 | `~/.gophermind/blackboard.db` (+ `-wal`, `-shm`): rows, events and calls with `run_id = <id>` | **global file**, per-project rows | keep the file. The next fresh `/project` deletes this run's rows itself (`db.ClearRun`, called by the planner's Load). Read the ledger before starting the next attempt |
| 8 | `~/.gophermind/vault.age`: harness scope (human values) and `run/<id>` scope | global file | keep. The run scope is overwritten at every fresh start (8.1) |
| 9 | `~/.gophermind/gophermind.yaml` | global | keep |
| 10 | `~/.gophermind/gomodcache/` | global | keep (checksum-verified module cache; delete only on suspected corruption) |
| 11 | the two Postgres databases of `DATABASE_URL` and `TEST_DATABASE_URL` | external | drop and recreate, by the orchestrator |

The clear order matters, and the GOAL commands alone are not enough. After a failed attempt HEAD sits on branch `gm/<id>`; `git reset --hard goal-baseline` would move that branch instead of `main`, and after any attempt the surviving branch `gm/<id>` makes the next fresh `Start` switch to it instead of creating it (gitland `Start`: "work branch present: switch, no clean check"), carrying the old attempt's commits forward. So:

```bash
git=/usr/bin/git   # the machine's git wrapper blocks reset --hard, clean and branch -D; GOAL's own clear needs the real one
R="$HOME/OtherProjects/AIVentureStudio"; ID=gm-2026-09-29-002   # both printed by --print-state-paths
# 0. before clearing: read the ledger, project.json and report.json, write the attempt record
$git -C "$R" switch --discard-changes main
$git -C "$R" branch --list "gm/$ID" | grep -q . && $git -C "$R" branch -D "gm/$ID"
$git -C "$R" reset --hard goal-baseline
$git -C "$R" clean -fdx -e .remember
target="$HOME/.gophermind/runs/$ID.json"
case "$target" in ""|"/"|"$HOME") echo refuse >&2 ;; "$HOME"/.gophermind/runs/gm-*.json) rm -f -- "${target:?}" ;; *) echo refuse >&2 ;; esac
# 11. reset both databases (psql through the tunnel or on the mini)
# verify: git status clean, HEAD == goal-baseline, no gm/<id> branch, then:
gophermind-dev project <brief> ... --preflight-only     # exit 0
```

`gophermind project --print-state-paths <brief> [--repo <path>]` prints this table so the orchestrator never guesses: one line per path, tab separated, `action<TAB>scope<TAB>path-or-name`, where action is one of `git_clean`, `git_branch_delete`, `git_reset`, `delete_file`, `rows_cleared_at_start`, `keep`, `external`. It needs no network, no vault, writes nothing, and honors `GOPHERMIND_CONFIG_DIR` and `GOPHERMIND_VAULT_PATH`. The real-path output is what the orchestrator's guarded deletes use; `projectrun` itself deletes nothing.

## 10. The binary and the version (item 5)

`/opt/homebrew/bin/gophermind` is the 0.9.0 cask (commit `7941ce6`) and has no `project` verb. A graded attempt must run the binary built from the feature branch, and must not replace the cask. Ruling: `scripts/build-dev-binary.sh` (also `make dev-binary`) builds `./cmd/gophermind` with the same `-ldflags` the Makefile uses (`Version=dev+<sha>`, `Commit=<sha>`, `Date=<utc>`) to `~/.gophermind/bin/gophermind-dev`, refuses to build from a dirty tree or a commit not on the remote branch, and prints `gophermind-dev version`. The orchestrator runs `gophermind-dev project ... --expect-binary-commit "$(git rev-parse --short HEAD)"`, so a stale binary cannot run by mistake, and the report's first lines and `project.json` carry `binary.version`, `binary.commit`, `binary.date`.

## 11. The final report

`<run>/project.json` (mode 0600) and the printed report. Fields: `binary`, `mode`, `resumed`, `repo` (path, brief repo, base branch, HEAD at start), `preflight` (checks), `secrets` (names and sources), `ambiguity` (brief setting, effective policy, `clarify_defaulted` with id, question and answer, `conservative_assumptions` count), `milestone_approvals`, `approval` (by, plan hash), `plan` (functions, waves, warnings, `requirements_covered`), `stages`, `executor` (the executor's `report.json` embedded, absent when it did not run), `status`, `stop_reason`, `exit_code`. The printed form carries counts and ids; the question and answer text stay in `project.json` and `answers.json` (R10: no model-written text in a log line). The printed report ends with the executor summary, whose last two lines are `Requirements covered: N of N` and `Acceptance passed: N of N`; when the executor did not run the two lines are still printed, from the planner's files, with `0 of A` for acceptance and the count from `requirements.json` or the brief, so a log grep is always well formed.

## 12. Changes to existing packages

| Package | Change | Why |
|---|---|---|
| `planner` | `Options.Repo` (plan only; must equal the run record's repo on resume); `Options.Unattended` (Clarify defaults, parser requires them; `callAsking` conservative branch); exported `ReadAnswers(runDir)` | GOAL repo differs from the brief's; sections 8.2, 11 |
| `tui` | `/project` becomes v2; v1 commands renamed `/plan-v1` and `/plan-v1-execute`, with their own messages updated | R2 |
| `cmd/gophermind` | `project.go`, routing next to `brief`, usage text | R1 |
| `scripts`, `Makefile` | dev binary | R5 |
| `docs/briefv2` | README and a runbook | operators |

The executor and the planner's stage code are otherwise unchanged; `executor.Run` is called with its fixed `Options`.

## 13. Not in scope

- Deleting v1 (`plantree`, `plan`, `phaseflow`, `orchestrate`): the server, desktop app and `/phase` use them. A removal plan follows the perfect release.
- A Postgres provisioner, a driver, or a database emptiness check.
- A milestone gate, a file gate for `/project`, a desktop or server front end for `/project`.
- Replacing the brew cask, signing or notarizing the dev binary.
- Reading the vault passphrase from anywhere but `GOPHERMIND_VAULT_PASSPHRASE`.

## 14. Rulings and their cost if wrong

| # | GOAL item | Ruling | Cost if wrong |
|---|---|---|---|
| R1 | 1 | One library entry `projectrun.Run` behind both forms; a run is plan then build in one process; a fresh run refuses any leftover state; `--resume` is explicit and recorded | If the two forms drift, graded runs and interactive runs differ; one parser and one `Run` prevents it. A user who wants silent resume must add a flag |
| R2 | 2 | Keep v1 under new names `/plan-v1` and `/plan-v1-execute`; `/project` is v2 only; remove v1 after the perfect release | Remove now: breaks `/phase`, the server and the desktop app for no graded benefit. `/project --v1`: the goal's "single entry" is ambiguous and a typo runs the wrong planner. Cost of the chosen path: about 110 string edits in `tui`, and two v1 commands still exist for one release |
| R3 | 3 | Unattended is the default and `--attended` exists for people; no `--yes` | If a human wants gates on the real brief they pass `--attended`; if unattended were opt-in the graded command could silently stop for a human |
| R3a | 3a | Secrets: vault harness scope, or `--generate hex32|placeholder` named on the command line; GopherMind generates `JWT_SIGNING_KEY` and `STUDIO_LLM_API_KEY`; both Postgres URLs are human prerequisites; loopback and distinct databases enforced; run scope overwritten on a fresh start | A generated placeholder LLM key fails a bullet that needs a real key: the attempt fails honestly and a vault value (which wins) fixes it. A database not loopback would fail late, inside acceptance, which the preflight prevents |
| R3b | 3b | Clarify takes each default, records it, never invents; a question with no default is a parse failure and then a stage failure; mid-stage questions use the existing conservative rule and are listed | A wrong default becomes a wrong assumption in the plan, visible in the approval summary and in `project.json`; the acceptance bullets still decide the build |
| R3c | 3c | Approval by the unattended gate, bound to the plan hash, only at `C == N`; milestone approvals recorded as covered by it | A reader may expect a milestone pause; the line in the report says there is none |
| R3d | 3d | The stop table of 8.4, each with a status, reason, exit code | A missing row is a run that ends without a reason; a test walks the table |
| R4 | 4 | State table of section 9, clear order including `branch -D gm/<id>`, `--print-state-paths`, rows cleared by the next start | Skipping the branch delete carries an old attempt into a new one; deleting `blackboard.db` wholesale destroys other runs' history |
| R5 | 5 | `gophermind-dev` built by a script from a clean, pushed commit; `--expect-binary-commit`; version and commit in the report | The cask stays 0.9.0, so a typed `gophermind project` fails with an unknown verb, which is loud and safe |
| R6 | 6 | Tests of section 15, offline, no real `~/.gophermind` | A graded attempt is the only proof against the real model |
| R7 | (1, 3a) | Preflight failure is exit 6, is not a run failure, writes nothing, and reports every missing item at once | An orchestrator treating 6 as a failed attempt would waste attempts; the GOAL loop says preflight is fixed and rerun |
| R8 | (1) | Leftover run folder or `gm/<id>` branch without `--resume` is refused | A crash recovery costs one flag; accidental carry-over would be silent |
| R9 | (1) | `--repo` overrides the brief's repo at plan time and run time | Without it the brief would have to be edited by hand, which GOAL forbids |
| R10 | (11) | Printed report holds ids and counts only; model-written text stays in 0600 files | A reader must open `project.json` to see the defaulted questions |
| R11 | (8.3) | Milestone flag recorded, not enforced | See R3c |

## 15. Tests

All offline: fake provider, `t.TempDir()`, `GOPHERMIND_CONFIG_DIR` and `GOPHERMIND_VAULT_PATH` set to temp paths, a fast vault work factor, loopback listeners for database checks, no real network and no real `~/.gophermind`. The fixture is the executor's greeter (5 leaves, 3 waves, `executor/testdata/greeter`), edited by the test copy to `on_ambiguity: halt`, `milestone_approvals: true`, one declared secret, and a Clarify reply with two questions carrying defaults.

| Requirement | Test |
|---|---|
| One process plans and builds; unattended defaults and auto-approval; proof lines last | `TestProjectE2EGreeter` |
| Planner failure stops before the executor, exit 1, report still printed and well formed | `TestProjectPlanFailureExit1` |
| Escalation exit 4, interruption exit 5, exit table | `TestProjectEscalationExit4`, `TestProjectInterruptedExit5`, `TestExitCodes` |
| Stop table walk | `TestEveryStopConditionHasReasonAndCode` |
| Preflight lists everything missing, calls no model, writes nothing | `TestPreflightMissingListedNoModelCallNothingWritten` |
| Each preflight check | `TestPreflightRepoChecks`, `TestPreflightStaleStateAndBranch`, `TestPreflightSecretMatrix`, `TestPreflightVaultPassphrase`, `TestPreflightDatabaseLoopbackAndDistinct`, `TestPreflightRequirePrivate`, `TestPreflightExpectBinaryCommit`, `TestPreflightToolsAndSettingsNotCreated` |
| Preflight failure is not a run failure | `TestPreflightFailureExit6IsNotARun` |
| Secrets: vault beats generated, generated shapes, overwritten on fresh start, kept on resume, no value anywhere | `TestProvisionSources`, `TestProvisionOverwritesRunScope`, `TestProvisionKeepsOnResume`, `TestNoSecretValueAnywhere` |
| Clarify defaults, required defaults, mid-stage rule | `TestUnattendedClarifyTakesDefaults`, `TestUnattendedClarifyRequiresDefaults`, `TestUnattendedMidStageQuestionAssumes`, `TestAttendedClarifyUnchanged` |
| Approval binds to the hash, `approved_by unattended`, refused below N of N | `TestUnattendedApproveBindsHash`, `TestUnattendedGateRefusesGap` |
| Repo override | `TestRepoOverrideUsedForRunFolder`, `TestRepoOverrideMustMatchOnResume` |
| State paths complete and classified | `TestStatePathsListEverything`, `TestStatePathsNoSideEffects` |
| Clear then rerun starts clean | `TestClearedStateRerunsClean` |
| Resume flag | `TestResumeFlagContinuesAndIsRecorded` |
| Version and commit in the report | `TestReportCarriesBinaryVersion` |
| CLI and TUI share the parser and `Run` | `TestParseArgsTable`, `TestProjectCLIExitCodes`, `TestSlashProjectRunsProjectrun`, `TestSlashProjectRefusesAttended`, `TestPlanV1CommandsRenamed` |
| Manual, not CI | `--preflight-only` on the real brief (plan Task 15), then the GOAL loop |

## 16. What a human must provide before the graded attempt

These are prerequisites, not run steps. A preflight that finds any of them missing exits 6 before any model call.

1. `GOPHERMIND_VAULT_PASSPHRASE` exported in the orchestrator's environment, and a vault that opens with it.
2. Two empty Postgres 15 or later databases, reachable at `127.0.0.1` (for a database on the mini, an `ssh -N -L` tunnel kept up for the whole attempt), their URLs stored with `gophermind brief vault set DATABASE_URL` and `gophermind brief vault set TEST_DATABASE_URL`, and a way for the orchestrator to drop and recreate both between attempts.
3. `~/.gophermind/gophermind.yaml` with `privacy.mode: private_only`, every tier pointing at `mini/qwen3.6:35b-a3b`, and `toolchain.PATH` containing the directory of `go` (here `/opt/homebrew/bin`).
4. The mini reachable with Ollama running (checked by TCP only; GOAL's memory and swap checks stay the orchestrator's).
5. Optional: a real `STUDIO_LLM_API_KEY` in the vault, needed only if a bullet turns out to call a real LLM (the generated placeholder covers the fake-LLM bullets).

Everything else (the JWT key, the placeholder LLM key, run scope, approval, clarify defaults) GopherMind does itself.
