# `/project` as the single entry point: Design

Status: revised against the built executor (2026-09-30) and amended 2026-10-08 against sub-projects A (run state as files) and B (node widening and the question model), as section 7 of the umbrella spec (`docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md` in the main checkout) requires: the preflight and state clearing name the file layout, the unattended policy covers `ConfirmUnderstanding`, and the report has a node-class dimension. Date: 2026-09-30.
Scope: GOAL task 4 (`docs/GOAL.md` in the main checkout, authoritative): `/project` takes a brief and plans and builds it, with no second command and no human step, using the v2 planner and the v2 executor. Also the support task 5 needs: clear-and-rerun, the binary used by a graded attempt, and the list of things a human must provide before one.
Builds on: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (planner), `docs/superpowers/specs/2026-09-30-v2-executor-design.md` (executor), `docs/superpowers/specs/2026-10-07-node-widening-and-question-model-design.md` (B). The executor is built: `executor.Run(ctx, Options) (Report, error)`, `brief run`, `brief run --check-env` and `report.ExitCode` exist on this branch.

## 1. Why this exists

GOAL.md defines one graded attempt as one `/project` run from a cleared state to a finished build, in one process, with no restart, no resume and no hand edit. An attempt also fails when GopherMind stopped to ask a human. Today that sentence cannot be satisfied, because `/project` is the v1 planner and the v2 planner and executor sit behind three separate commands that each stop for a human. A rehearsal of `brief plan` on the real brief (`gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md`, `on_ambiguity: halt`, `milestone_approvals: true`, four declared secrets) showed two stops: Load refused to start until every declared secret was in the vault ("secret DATABASE_URL is not in the vault: run `gophermind brief vault set`"), and Clarify asked one question, wrote `QUESTIONS.md` and halted, although the question already carried `default_if_unanswered`. (That rehearsal predates B. Since B the stop would be Clarify's frontier round and then the Confirm stage's `UNDERSTANDING.md`; sections 8.2 and 8.3 cover both.)

This document decides how one command removes every such stop without hiding one: each human decision is either taken by a stated unattended rule and recorded, or is a prerequisite that a preflight checks before any model call.

## 2. What exists today

All of this is read from the code on branch `feat/briefv2-planner-core`.

**v1 `/project` (TUI only).** `gophermind-lib/tui/commands_registry.go` registers `/project <name> <brief>` ("plan a brief into phases, tasks and steps, then approve and export it") and `/project-execute`. `update.go` routes both. `tui/project.go`: `parseProjectCommand` splits a name from a brief path; `startProject` reads the brief file (any text, no front matter), scaffolds `.planning/` through `phaseflow`, and `startPlanning` runs `plan.RunPass1` (skeleton) and `plan.RunPass2` (specify every step) from `gophermind-lib/plantree/plan` on a goroutine, under a run lock, with the session's own agent client (`m.planClient()`), sized to that client's context window. The plan tree lives in `.planning/plan` (`plantree`). Open questions go to a question round (`tui/questions.go`, `/questions`). `tui/approve.go` shows a summary, waits for y/n/revise typed by a person, and exports `ROADMAP.md` and `.planning/assignments.json` (`plantree/export`). Building is a second command: `/project-execute` (`tui/execute.go`) runs each pending assignment through `phaseflow` and `orchestrate`, each in a fresh agent context. There is no contract stage, no coverage check, no test-writer, no blackboard, no ledger, no sandbox, no router, no acceptance proof, and no git landing.

**There is no `gophermind project` subcommand.** `cmd/gophermind/main.go` dispatches `brief` (line 615, before `cfg.Validate`) and `phase`; `project` is not a verb. `gophermind-lib/project` is unrelated (repo context and instruction loading).

**Who else depends on v1.** `gophermind-lib/serve/pipeline*.go`, `gophermind-osx/` (desktop pipeline panel and client) and `cmd/gophermind/phase.go` import `plantree`, `phaseflow` or `orchestrate`. `/phase` uses the same packages. Deleting v1 would break the server, the desktop app and `/phase`.

**v2 today.** `gophermind brief plan <brief> [--yes] [--gate terminal|file]` runs Load, then the stages of `planner/planner.go` `stages`: Clarify, Confirm, Contract, Decompose, Enrich, Coverage, Approve, Test-writer. It exits 0, or exits 3 when the file gate is waiting. `brief resume <id>` continues it; `brief answer <id> <question-id> <text>` changes a settled answer and resets the work that cited it (`planner.ChangeAnswer`; refused once tests were written or the executor started). `brief run <id>` builds it, is resume-safe, and `brief run --check-env` runs the environment preflight (exit 6); a returned executor error is exit 7 (`exitFault`). `report.ExitCode(status, stopReason)` maps verified 0, failed 1, waiting 3, escalated 4, interrupted 5. Facts in the planner that matter here:

- Load (`planner/load.go`) resolves the repo from the brief's own `repo:` field (`resolveRepo`); there is no override. The testdata brief says `~/src/venture-studio-server`, which is not the GOAL target `~/OtherProjects/AIVentureStudio`.
- `storeSecrets` copies `vault.HarnessScope` values into `vault.RunScope(<id>)`, prompts a person when `Deps.PromptSecret` is set, and otherwise refuses to start. A value already in the run scope is left alone, never overwritten.
- Clarify (B) is a question model: the harness probes the facts, the model writes a dependency tree of questions (`_state/clarify/questions.json`), and each round puts the frontier (questions whose prerequisites are settled) to the owner; a `clarify:more` call then asks what the settled answers unblocked, up to `defaults.clarify_max_calls` (4) calls and `defaults.clarify_max_questions` (30) questions. `run.takesRecommendations()` (`planner/clarify.go`) is true only when the brief says `on_ambiguity: assume_and_document`; then every frontier question takes its `recommended` answer (the old `default_if_unanswered` field is read as the recommendation), is settled `answered_by: unattended-default` (shown in `answers.json` as `assumed: true`) and gets a record in `decisions/`; the rounds still run, so dependent questions still appear and are still answered. With that policy the reply parser requires a recommendation on every question (`qparseOpts.RequireRecommended`), which takes the router's malformed retry once. Any other `on_ambiguity` value goes to `Gate.Ask`; the file gate writes `QUESTIONS.md` and returns `human.ErrWaiting`. `callAsking` (a mid-stage `QUESTION:` reply in Contract and Decompose) tests `OnAmbiguity == "assume_and_document"` directly (`planner/planner.go`), records the question as a settled decision and tells the model to choose the most conservative option.
- Confirm (B) comes after Clarify and before Contract. It renders the understanding (facts, decisions, assumptions taken, open questions) as `UNDERSTANDING.md` text with a hash, and asks `Gate.Confirm`. `human.Gate` has four methods: `Ask`, `Approve`, `Confirm`, `Escalate`. Under `takesRecommendations()` the stage never calls the gate: it requires the question store complete with nothing open and records `_state/understanding.json` `confirmed_by: unattended` with the understanding hash; `--yes` records `flag`; otherwise the terminal gate asks y/N and the file gate writes `UNDERSTANDING.md` with a decision block and returns `ErrWaiting` until the owner writes `approve` or `reject: <why>` (an old answer never confirms a changed understanding, because the file carries the hash). A rejection fails the stage with `the understanding was not confirmed`.
- `approve` first requires the understanding to be confirmed and unchanged (`confirmDone`), refuses while any node still lists open questions, then writes `approval.json` with `approved_by`, a `plan_hash` (`RenderPlan`: the summary plus the bytes of contracts, drafts, classes, coverage, answers, dependencies, enrichment, facts) and the `understanding_hash` of the confirmed text; `planner.VerifyApproval` (and so `executor.LoadPlan`) refuses an approval that has no understanding hash. With no `--yes` it asks `Gate.Approve`; `Decision.By` becomes `approved_by`.
- Enrich (B) writes the widened node groups (rationale, construction, errors, tests, security, performance, portability, observability, alternatives, refactoring) in batches of `defaults.enrich_batch_size` (4) function nodes; with `defaults.route_by_node_tier` (default true) node-scope stages use the node's `model_tier` chain, so every tier chain in `gophermind.yaml` must resolve, not only `strong`. `node_class` is on the node (and still in `_state/classes.json`, which `planner.ReadClasses` reads).
- `brief.Front.MilestoneApprovals` is parsed and used by nothing. Neither planner nor executor has a milestone gate.
- `settings.Load` writes a default `gophermind.yaml` when none exists. The default `standard` chain includes a public provider (`kilo`), and the default `toolchain.PATH` is `/usr/local/go/bin:/usr/bin:/bin`, which does not contain this machine's `go` (`/opt/homebrew/bin/go`).
- `planner.Options.Yes` records `approved_by: flag` (and `confirmed_by: flag`); there is no unattended notion in `Options`. `answer` and `approval` are unexported; `planner.ReadCoverage`, `ReadRequirements`, `ReadClasses`, `VerifyApproval`, `RenderPlan`, `LookupRun`, `ReadStatus`, `ChangeAnswer` and `IgnoredDuplicates` are exported. `planner.Deps` has `Caller`, `Gate`, `Sink`, `Board`, `Settings`, `OpenSecrets`, `PromptSecret`, `LedgerErrors`, `Now` and nothing that clears run state: there is no `ResetRun`.
- The four planner warning counts (`leaf_defaulted`, `doc_defaulted`, `leaf_normalized`, `outline_id_normalized`) exist only as warning events; no file holds them.
- `executor.Report.Resumed` is sticky: once any invocation resumed, every later report of that run says so.
- `cmd/gophermind/brief_preflight.go` and the base-URL probing in `brief_run.go` are `package main`, so neither `projectrun` nor the TUI can import them.

**State a run leaves** (A, B and the executor spec). There is no database: run state is files, and no `briefv2` package may import `database/sql` (`briefv2/nosql_test.go` enforces it). In the target repo, one run folder `.gophermind/<id>/` holds: `brief.md`, `requirements.json`, `answers.json` (a view derived from the question store), `contracts.json`, `dependencies.json`, `coverage.json`, `approval.json`, `UNDERSTANDING.md`, `decisions/<question-id>.md`, the tree node files (`root.json`, `<component>/component.json`, `<component>/<fn-id>.json`), a `*.runtime.json` beside every node file (status, revision, wave, claim, attempts, result; `blackboard.FS`), `attempts/<node-id>/<n>/` (scrubbed reply and check output, when `executor.artifacts` is on), `logs/`, `report.json` and `acceptance.json` (executor), `_state/project.json` (this spec), and `_state/` (`status.json`, `clarify/questions.json`, `clarify/facts.json`, `clarify/rounds.jsonl`, `understanding.json`, `classes.json`, `decomposed.json`, `enriched.json`, `leaf_tests.json`, `acceptance_tests.json`, `executor.json`, `leaf-results.json`, `events.jsonl`, `calls.jsonl` with `calls.seq`, `replies/<stage>-<n>.txt`). `_state/project.json` and not `project.json` at the folder root, because `tree.Store.Load` treats every unlisted root-level `.json` file as a node and rejects it. Beside it sit `.gophermind/<id>-scratch/` and the work branch (`gm/<id>` unless the brief sets `work_branch`). Under `~/.gophermind` (or `GOPHERMIND_CONFIG_DIR`): `runs/<id>.json` (the run record that maps an id to its repo and folder, and what `blackboard.FS` and `ledger.FS` resolve through), `vault.age`, `gophermind.yaml`, and `gomodcache/`.

## 3. Goals and non-goals

Goals:

- One entry point, `projectrun.Run`, used by the slash command `/project <brief>` and the CLI `gophermind project <brief-path>`, running load, clarify, confirm, contract, decompose, enrich, coverage, approve, test-writer, then `executor.Run`, then one final report, in one process.
- No step in a healthy run needs a human, a restart or a resume. Every decision that used to need a human is either taken by a written rule and recorded, or checked by a preflight first.
- A preflight that fails fast, before any model call and without writing anything, with the exact list of what is missing.
- A clear-and-rerun contract exact enough that an orchestrator removes precisely the state of one attempt.
- The binary that runs a graded attempt states its own version and commit in the report.

Non-goals: see section 13.

## 4. Architecture

Two new packages. `gophermind-lib/briefv2/envcheck` holds the environment checks that `brief run --check-env` already uses (`ResolveBaseURLs`, `ProbeBaseURL`, `LookInPath`, `LoopbackAddr`, `ModuleCacheFree`, `DirtyOutsideTests`, `SafeLine`, `EnvNotes`), moved out of `package main` with no behavior change; `brief run --check-env` and `projectrun` both call it. `gophermind-lib/briefv2/projectrun` depends downward on `envcheck`, `planner`, `executor`, `report`, `settings`, `vault`, `router`, `blackboard` (`blackboard.FS`), `ledger` (`ledger.FS`), `human`, `events`, `brief`, `sandbox`, `gitland` (for `ValidateLanding`), `gitenv` (`gophermind-lib/gitenv`), `config` and `version`. It imports no database package, and no `briefv2` package may (`briefv2/nosql_test.go`).

```text
envcheck     shared environment checks (no behavior change from cmd/gophermind)
projectrun   Env (defined once), ParseArgs, pre-plan Preflight, generated-secret pre-step, unattendedGate,
             Run (plan then build then report), StatePaths, ProjectReport (with the node-class table), progress sink
cmd/gophermind/project.go       `gophermind project ...`: flags in, exit code out (thin)
gophermind-lib/tui/project_v2.go  `/project ...`: same ParseArgs, same Run, output to the transcript
```

```go
func ParseArgs(args []string, allowAttended bool) (Options, error) // the one parser, CLI and TUI
func Run(ctx context.Context, o Options, env Env) Result           // never panics, never returns before the report is printed
type Result struct { ExitCode int; Status, StopReason string }
```

`Env` is defined once and holds the seams tests replace (settings and providers, the base-URL probe, vault opener, the state backends, read-only git, dial, clock, version, executable path, the executor call). Production uses `DefaultEnv()`, which loads `gophermind.yaml` through `settings.Load` (only after preflight has proved the file exists), builds providers with the vault and the no-redirect HTTP client as `brief plan` does, builds the state backends the way `cmd/gophermind/brief_state.go` does (`blackboard.NewFS` and `ledger.NewFS`, both resolving a run id to its folder through `planner.LookupRun`; they hold no open file and nothing to close), and reads `version.Version`, `Commit`, `Date`. Every git call goes through `gitenv` (clean environment) and the git on `PATH`.

## 5. What one `/project` run does

```text
1  ParseArgs, then brief.Parse(<brief>)                       invalid brief: exit 2
2  Pre-plan preflight (section 7)                              any failed check: print the numbered list, exit 6
   --print-state-paths / --preflight-only stop here with their own output
3  Write generated secrets into the harness scope (section 8.1)
4  Build router (ledger.FS), blackboard.FS and sink; open the vault only when a secret or provider key needs it
5  planner.Run(ctx, {BriefPath, Repo, Unattended: true})       Load, Clarify, Confirm, Contract, Decompose, Enrich, Coverage, Approve, Test-writer
   (Clarify takes recommendations; Confirm records "unattended" with the understanding hash; the unattended gate
    approves by "unattended", bound to the plan hash, decided from coverage.json)
6  executor.Run(ctx, {RunDir, Repo, Gate: unattended gate, ...})  waves, repair, acceptance, go mod verify, landing
7  Write <run>/_state/project.json, print the final report, return the exit code
```

Steps 5 and 6 are two calls in one process with one router, one blackboard and one ledger over the same run folder; nothing is written between them that a restart would read. A planner stage error stops the run at step 5 with `stop_reason: plan:<stage>`, the report is still written and printed, and the executor is not called. The planner can never return `Waiting` here (no gate asks: Clarify takes recommendations, Confirm decides by rule, the unattended gate answers Approve); if it does, the run ends `failed` with `plan:waiting` and that is a defect, not a prompt.

`--resume` (R8) changes step 3 (existing run-scope values are kept), step 5 (`planner.Run` with `RunID`, which skips every stage whose `done` predicate holds, including Confirm while the understanding hash is unchanged) and records `resumed: true`. Without it, any leftover state is a preflight failure, so a healthy attempt cannot resume by accident. The no-restart rule: the healthy path is one `planner.Run` started from the brief path and one `executor.Run`; nothing on it may need, print or suggest `--resume`.

`--graded` (implied by `--expect-head`, R12) is the mode of a graded attempt: it refuses `--resume` and `--attended`, refuses any leftover state (run folder, the work branch, `runs/<id>.json`), requires a clean target repo at the expected HEAD, and the report says `graded: yes` and `resumed: no`. A graded attempt is a single invocation; because `executor.Report.Resumed` is sticky, a `true` there in graded mode is printed as `graded: INVALID (the run resumed)`.

## 6. The two forms and the exit codes

Form A, slash command in the TUI: `/project <brief-path> [flags]`. Form B, CLI: `gophermind project <brief-path> [flags]`. Both call `ParseArgs` then `Run`; the TUI prints to the transcript through a sink, the CLI to stderr (progress) and stdout (report). Flags:

```text
--repo <path>               target repo; default the brief's repo: field (recorded, both values, in the report)
--generate NAME=KIND        make a value for a declared secret when the vault has none; KIND is hex32 or placeholder (repeatable)
--require-private           preflight fails unless privacy.mode is private_only and every tier entry is a private provider
--expect-head <rev>         preflight fails unless HEAD equals <rev> (GOAL: goal-baseline); implies --graded
--graded                    graded attempt: single invocation, no resume, no leftover state, clean target at --expect-head (needs --expect-head)
--expect-binary-commit <sha>  preflight fails unless this binary was stamped with that commit
--resume                    continue an unfinished run of this brief; without it leftover state is refused
--attended                  CLI only: terminal gate for questions, approval and escalations; prompts for missing secrets (refused with --graded)
--preflight-only            run the preflight, print it, exit 0 or 6
--print-state-paths         print every path of section 9 for this brief and repo, exit 0
```

Exit codes come from `report.ExitCode(status, stopReason)` plus the two codes `brief run` adds: `0` verified; `1` failed (includes every planner stage failure); `2` invalid brief or bad usage of a brief field; `3` waiting on a human (`escalated` with `waiting_on_human`; the unattended gate never produces it, the mapping is kept truthful); `4` an escalated leaf or a stop (executor); `5` interrupted, resumable; `6` preflight failed (nothing started); `7` harness fault (`executor.Run` returned an error: no repo, sandbox refused, settings invalid, plan changed, live worker). A returned executor error is exit 7, not 1. Usage errors exit 1 like `brief`.

## 7. The preflight

This is the PRE-PLAN variant of `brief run --check-env`: it runs before any run folder exists, so there is no approval check and secrets are read from the harness scope (`vault.HarnessScope`), not the run scope. It shares `envcheck` with `--check-env`. It makes no model call (provider probes are the two GETs of `envcheck.ProbeBaseURL`, never a completion), builds no state backend (no run folder exists yet, and there is no database to open), creates no file, and never reads a secret value into an error. Each check yields `{Name, OK, Detail, Fix}` with a fixed name. All checks run (no early exit) so one run lists every missing item.

| Check (fixed name) | Fails when | Fix text |
|---|---|---|
| `repo` | not a directory, not a git worktree, base branch missing, HEAD not on the base branch | put the repo on branch `<base>` |
| `repo head` | `--expect-head` given and HEAD differs | the rev |
| `clean tree` | dirty (ignoring `.gophermind/`); skipped with `--resume` | read `git status` in the repo |
| `stale state` | the run folder `<repo>/.gophermind/<id>/` (the whole tree of section 9 row 1: node files, `*.runtime.json`, `_state/`, `attempts/`, `decisions/`, `UNDERSTANDING.md`), the scratch folder `<repo>/.gophermind/<id>-scratch/`, the work branch (`gm/<id>` unless the brief sets `work_branch`) or the run record `<config dir>/runs/<id>.json` exists and `--resume` was not given; or `--resume` was given and the run folder or run record is missing. Presence is tested on the folder and the record only; no file inside the folder is read | the clear procedure (section 9) or `--resume` |
| `landing` | `gitland.ValidateLanding(front.landing)` fails | the brief field |
| `graded` | `--graded` without `--expect-head`, or with `--resume`, or any leftover state | the first leftover named |
| `sandbox`, `go toolchain`, `git`, `disk space` | as `brief run --check-env`: sandbox required but unavailable and `executor.sandbox` not `off`; `go` not on `toolchain.PATH`; `git` not on PATH; under 2 GiB free for the module cache | `add <dir> to toolchain.PATH in <settings path>` |
| `settings` | `gophermind.yaml` missing (never created by a preflight), invalid (the file is decoded with unknown keys refused, and `settings.Validate` covers the planner keys `defaults.clarify_max_calls`, `clarify_max_questions`, `enrich_batch_size`, `route_by_node_tier` and the executor keys `executor.artifacts`, `artifact_max_bytes`), or a tier chain cannot be resolved | `create it (see runbook)` |
| `privacy` | with `--require-private`: `privacy.mode != private_only` or a public entry in a tier | the setting |
| `provider <name>` | for every provider that any of the three tier chains (`strong`, `standard`, `any`) names, because node-scope planner stages route by the node's `model_tier` (`defaults.route_by_node_tier`, default on): no `base_url` and no allowed `base_url_fallbacks` entry answered (`ResolveBaseURLs`); the mini's VPN address `10.8.0.6` is used when the LAN address `192.168.1.35` does not answer, and the host that answered is recorded; a missing provider key fails `provider <name> key` | start the model server or fix `base_url` and `base_url_fallbacks` |
| `binary` | `--expect-binary-commit` given and `version.Commit` does not start with it, or `Commit` is `none` | `rebuild with scripts/build-dev-binary.sh` |
| `vault passphrase`, `vault` | the brief declares a secret (or a provider has `api_key_secret`) and `GOPHERMIND_VAULT_PASSPHRASE` is empty; or the vault file does not open with it (a missing file opens empty and is not created) | `export GOPHERMIND_VAULT_PASSPHRASE=<passphrase>` |
| `secret <NAME>` | no harness-scope vault value and no `--generate NAME=...` (attended runs may prompt instead) | `gophermind brief vault set <NAME>` |
| `database <NAME>` | a declared secret whose harness value parses as a `postgres` or `postgresql` URL names a non-loopback host, or `127.0.0.1:<port>` cannot be dialled, or two such secrets name the same host, port and database | the exact tunnel command `ssh -N -L <port>:127.0.0.1:5432 mini` (the URL's port as the local port) |
| `warning` | (not a failure) `brief.UndeclaredSecrets` is non-empty: printed as `warning:` lines | |

Databases: `DATABASE_URL` and `TEST_DATABASE_URL` are secrets of URL form and must be loopback. When the dial fails the preflight prints the tunnel command for the human to run and keep up for the whole attempt; code never starts a tunnel (nothing hosts from the laptop, and a tunnel is the human's process). Only host and port are printed, never userinfo or the database name.

On failure the output is a numbered "what a human must provide" list: a heading, then for each failed check `  <n>. <name>: <detail>` and `     Fix: <exact command or setting line>`, then the sentence that this is not a run failure. All fixes name commands and settings, never a value.

Preflight failure is not a run failure (R7): exit 6, `status: preflight_failed`, no run folder, no ledger file, no `_state/project.json` (the report goes to stdout only). A graded attempt that exits 6 has not started, so the orchestrator fixes the named items and runs again; the attempt count is unchanged. A database check cannot prove the databases are empty (there is no Postgres driver in this module and none is added); the preflight prints `note: database emptiness is not checked` and emptiness is part of the clear procedure.

## 8. The unattended policy

Unattended is the default (R3): `/project` means no human step. `--attended` (CLI only) swaps in the terminal gate and secret prompts and changes nothing else. The mode is recorded in the report. There is no `--yes`: an unattended approval is not an unseen approval, it is a stated rule that binds to a hash.

### 8.1 Secrets (3a)

Two provisioning sources only, resolved per declared secret in this order: (1) the vault's harness scope, set by a person with `gophermind brief vault set NAME` (value from a terminal or stdin, passphrase from `GOPHERMIND_VAULT_PASSPHRASE`); (2) a value GopherMind generates when the command line says so with `--generate NAME=KIND`: `hex32` is 32 random bytes as 64 hex characters, `placeholder` is `gm-placeholder-` plus 32 hex characters. A vault value beats a generated one. Nothing else makes a value: GopherMind never guesses a connection string, never reads a value from the environment, never takes one from a file in the repo.

This is a THIN pre-step, not a second provisioning path: before the planner runs, `--generate` values for names the harness scope lacks are written into the HARNESS scope. The planner's own `storeSecrets` then copies harness values into the run scope, as it does for any run. Because `storeSecrets` never overwrites an existing run-scope value, the pre-step also overwrites a run-scope value that differs from the harness value on a fresh run, so a changed harness value is not masked by an earlier attempt's copy. With `--resume` neither scope is touched except to generate a missing harness value. A generated value persists in the harness scope (the vault has no delete), so a rerun reuses it and the report's source says `generated:<kind>` the first time and `vault` afterwards. Generated values are never printed or logged. The report lists names and sources (`vault`, `generated:hex32`, `generated:placeholder`, `prompt`), never values; a canary test greps everything.

What the real brief needs, read from its Environment, Architecture and Acceptance text:

| Secret | What the acceptance actually uses | Ruling |
|---|---|---|
| `JWT_SIGNING_KEY` | HMAC key for tokens; every bullet that logs in uses one server instance, any non-empty key works | GopherMind generates: `--generate JWT_SIGNING_KEY=hex32` |
| `STUDIO_LLM_API_KEY` | only set as a required server variable; the end-to-end bullet points `STUDIO_LLM_BASE_URL` at the server's own `venture-server fake-llm`, and the network block says "tests use a fake" | GopherMind generates: `--generate STUDIO_LLM_API_KEY=placeholder`. A real key in the vault overrides it |
| `DATABASE_URL` | `migrate up` on an empty database, `seed-templates`, `serve`, every round trip | human prerequisite: a Postgres 15 or later database, empty at the start of each attempt |
| `TEST_DATABASE_URL` | `go test ./... -tags integration`; must be a different database from `DATABASE_URL` so the migrate bullet still sees an empty one | human prerequisite, a second empty database |

Why the databases are a human prerequisite: GopherMind has no Postgres driver (no new dependency) and the executor starts no database (executor spec 16 risk d). Why they must be loopback: the sandbox denies every non-loopback connection (executor spec 6.1), so a URL at `192.168.1.35` fails inside the acceptance commands. A database that lives on the mini is reached through `ssh -N -L 127.0.0.1:<port>:127.0.0.1:5432 mini`, which keeps it off the laptop, as CLAUDE.md requires, while the URL names `127.0.0.1`. The preflight enforces both points.

### 8.2 Clarify under `on_ambiguity: halt` (3b)

Ruling: when unattended, `planner.Options.Unattended` makes `run.takesRecommendations()` true whatever the brief says, so Clarify does what `assume_and_document` does and the rounds still run. In each round the frontier takes every question's `recommended` answer (a reply that gives only the old `default_if_unanswered` field counts: the parser reads it as the recommendation); the question is settled `answered_by: unattended-default`, `answers.json` shows it `assumed: true`, and a record is written to `decisions/<question-id>.md`. Dependent questions still appear after their prerequisites are settled and are answered the same way, up to the caps of `defaults.clarify_max_calls` (4) and `clarify_max_questions` (30); at a cap every question still open is put in one last round. A fact the harness probe can answer is answered by code (`answered_by: probe`) and is not an assumption. Nothing is invented: with `Unattended` the reply parser rejects a question that has no recommendation, which takes the router's normal malformed retry; if the retry still lacks one, or a saved question store from an earlier attended run holds a question with no recommendation, the Clarify stage fails and the run ends `failed` with `plan:clarify`. A mid-stage `QUESTION:` reply (Contract and Decompose, which carry no recommendation) takes the planner's assume branch, which `Unattended` also enables (`callAsking` tests `takesRecommendations()`): the question is settled as a decision with the fixed answer `No answer was given; take the most conservative option.`, and the model is told to choose the most conservative option and record it in the node's `assumptions`, which `RenderPlan` lists under "Assumptions" and the plan hash covers. The report counts both kinds from the question store (`_state/clarify/questions.json`, by `answered_by`). The brief's `on_ambiguity: halt` is reported as overridden, with the counts, so the override is visible.

A justified alternative was considered: refuse to run unattended when the brief says `halt`. It would make this brief unbuildable by the goal's own rule (no human step) and buys nothing, because the approval summary already shows every assumption and the acceptance bullets are the contract.

### 8.2a The understanding gate (`ConfirmUnderstanding`)

The Confirm stage sits between Clarify and Contract. Its question to a person is "is this what the plan will be built on?", asked through `human.Gate.Confirm` with the understanding text and its hash (the text is also the file gate's `UNDERSTANDING.md`).

Unattended ruling: the planner confirms by the stated rule of `planner/confirm.go` and does not call the gate. The rule: the question store must be complete with nothing unsettled, else the stage fails with `the understanding still has open questions` (`plan:confirm`). When it holds, the stage writes `UNDERSTANDING.md` into the run folder (facts established by the harness, every Clarify decision with who decided it, the assumptions taken each marked `[assumed, nobody was asked]`, open questions), and records `_state/understanding.json` with `confirmed_by: unattended` and the SHA-256 of that text. `Options.Yes` is not used because it would record `flag`. The understanding hash is copied into `approval.json` as `understanding_hash`, so the approval binds to the understanding that was confirmed, and the plan hash covers `answers.json`, so a changed answer invalidates both. `confirmDone` re-checks the hash and `answers.json` against the store on a resume, so an edited answer puts the understanding to the owner again.

`unattendedGate.Confirm` exists because `human.Gate` requires it. It is never reached on the healthy path, because the planner decides by rule. If it is called (a planner started without `Options.Unattended` but with this gate) it returns `ErrUnattended`, as `Ask` does, so a gap fails loudly and never confirms something nobody saw.

Attended (`--attended`, CLI only): the terminal gate prints the understanding and `Understanding hash: <hash>` and asks `Confirm this understanding? [y/N]`; an empty line is a rejection, end of input is an error, `y` or `approve` confirms, `n` or `reject: why` rejects. A rejection fails the stage with `the understanding was not confirmed` and the reason; the owner then runs `gophermind brief answer <id> <question-id> <text>` and `brief resume`, or edits nothing and starts again. The file gate (`brief plan --gate file`; `/project` does not offer it, section 13) writes `UNDERSTANDING.md` with a decision block and returns `human.ErrWaiting` (exit 3) until the owner writes `approve` or `reject: <why>` and runs `brief resume`; an understanding that changed since the file was written is written again, so an old answer never confirms a new text. The programmatic gate used by the desktop app and the server publishes a `KindConfirm` request.

### 8.3 Approval gates (3c)

Plan approval: the unattended gate's `Approve` returns `Approved: true, By: "unattended"` only when `planner.ReadRequirements` is non-empty and `planner.ReadCoverage` lists every requirement id (C == N, N > 0; belt and braces over the Coverage stage, which already stops on a gap). The decision reads the files, not the markdown. (`planner.Options.Yes`, which records `flag`, is not used.) The planner writes `approval.json` itself, `approved_by: unattended`, with the `plan_hash` of the exact plan and the `understanding_hash` of section 8.2a; `executor.LoadPlan` still verifies both through `planner.VerifyApproval`, so a changed plan or a missing understanding hash still invalidates it. Warnings in the plan are allowed and counted in the report.

Milestone approvals: `milestone_approvals: true` is declared by the brief and no code consumes it (`brief.Front.MilestoneApprovals` is parsed and read by nothing). Ruling: unattended, it is satisfied by the plan approval and the report says so in one line (`milestone_approvals: declared; the executor has no milestone gate; covered by the unattended plan approval`). The run is not refused and the line is not hidden. A future milestone gate must read the flag; until then the statement is the honest one.

Escalations: the gate's `Escalate` returns `stop` with `AnsweredBy` `human.AnsweredByUnattended` (`unattended-default`), so an escalated leaf ends the run `escalated`, exit 4, and the executor's escalation log records who answered.

### 8.4 What stops an unattended run (3d)

Nothing below is papered over: each produces a status, a `stop_reason`, an exit code and the report.

| Condition | Where | Status, exit | `stop_reason` |
|---|---|---|---|
| preflight item missing | before step 3 | `preflight_failed`, 6 | `preflight` |
| invalid brief | step 1 | `invalid_brief`, 2 | `invalid_brief` |
| Clarify question without a recommendation after retry; confirm with open questions; contract, decompose, enrich or test-writer failure | planner | `failed`, 1 | `plan:<stage>` (`plan:clarify`, `plan:confirm`, `plan:contract`, `plan:decompose`, `plan:enrich`, `plan:testwriter`) |
| coverage gap after `max_coverage_rounds`, approval refused for coverage, or a node still listing an open question | planner | `failed`, 1 | `plan:coverage`, `plan:approve` |
| contract problem on a leaf, a leaf that exhausts its ladder | executor | `escalated`, 4 | executor's (`human_stop`) |
| a leaf blocked by one that failed | executor | `failed` or `escalated` | executor's, leaf named |
| acceptance bullet failing after its repair rounds | executor | `failed`, 1 | executor's |
| sandbox refused at run time, an unattributable integration failure, landing blocked | executor | `failed`, 1 | executor's |
| `executor.Run` returns an error (no repo, settings invalid, plan changed, live worker) | executor | `harness_fault`, 7 | `harness_fault` |
| SIGINT, SIGTERM, `max_run_minutes` | both | `interrupted`, 5 | `interrupted` or `max_run_minutes` |

## 9. State, clearing and rerun (item 4)

`/project` creates exactly this. "Per-project" means keyed by the brief id (for the real brief `gm-2026-09-29-002`); "global" means shared by every run on the machine. There is no database: a run is a folder, and clearing a run means removing that folder.

| # | Path | Scope | On a graded clear |
|---|---|---|---|
| 1 | `<repo>/.gophermind/<id>/`, the run folder: `brief.md`, `requirements.json`, `answers.json`, `contracts.json`, `dependencies.json`, `coverage.json`, `approval.json`, `UNDERSTANDING.md`, `report.json`, `acceptance.json`, `_state/project.json` (written by `/project`), `proxy.log`, `bin/`, `logs/`, the tree node files (`root.json`, `<component>/component.json`, `<component>/<fn-id>.json`) with a `<node>.runtime.json` beside each, `decisions/<question-id>.md`, `attempts/<node-id>/<n>/`, and `_state/` (`status.json`, `clarify/questions.json`, `clarify/facts.json`, `clarify/rounds.jsonl`, `understanding.json`, `classes.json`, `decomposed.json`, `enriched.json`, `leaf_tests.json`, `acceptance_tests.json`, `executor.json`, `leaf-results.json`, `events.jsonl`, `calls.jsonl`, `calls.seq`, `replies/`) | per-project, in the repo, git-excluded | removed by `git clean -fdx -e .remember` (the GOAL command; it removes untracked files, so the whole `.gophermind` folder is archived first), or by the exact-path removal of the clear script when git is not resetting the repo. The ledger, the blackboard and the event log are inside it, so removing the folder clears them |
| 2 | `<repo>/.gophermind/<id>-scratch/` | per-project, in the repo | removed by the same command or the same guarded `rm -rf` |
| 3 | `<repo>/.git/info/exclude` line `.gophermind/` | per-repo | keep (harmless, `rundir` re-adds it) |
| 4 | the work branch in the repo (`gm/<id>` unless the brief sets `work_branch`) | per-project | **not removed by reset or clean**: delete it with `git branch -D` (below; may be blocked by the wrapper) |
| 5 | repo work tree and HEAD | per-project | `git reset --hard goal-baseline` (GOAL) |
| 6 | `~/.gophermind/runs/<id>.json` | per-project, global folder | delete the file |
| 7 | `~/.gophermind/vault.age`: harness scope (human values) and `run/<id>` scope | global file | keep. A differing run-scope value is overwritten at a fresh start by the pre-step (8.1); generated harness values persist |
| 8 | `~/.gophermind/gophermind.yaml` | global | keep |
| 9 | `~/.gophermind/gomodcache/` | global | keep (checksum-verified module cache; delete only on suspected corruption) |
| 10 | the two Postgres databases of `DATABASE_URL` and `TEST_DATABASE_URL` | external | drop and recreate, by the orchestrator |

Nothing outside rows 1 to 6 holds state of a run, and no step clears rows in a shared file. The next fresh `/project` finds no run folder, so its ledger ids start at 1 and its blackboard starts empty by construction. A run folder is evidence (the ledger, the events, the saved attempts, the report), and clearing destroys it, so step 0 of the procedure archives it first.

The clear order matters, and the GOAL commands alone are not enough. After a failed attempt HEAD sits on branch `gm/<id>`; `git reset --hard goal-baseline` would move that branch instead of `main`, and after any attempt the surviving work branch (`gm/<id>` unless the brief sets `work_branch`) makes the next fresh `Start` switch to it instead of creating it (gitland `Start`: "work branch present: switch, no clean check"), carrying the old attempt's commits forward. So, with the git on `PATH` (never a hard-coded `/usr/bin/git`, never `git checkout`, `git stash`, `git worktree add` or `--gw-force`):

```bash
R="$HOME/OtherProjects/AIVentureStudio"; ID=gm-2026-09-29-002
BR="$(gophermind-dev project <brief> --repo "$R" --print-state-paths | awk -F'\t' '$1=="git_branch_delete"{print $3}')"   # the work branch
scripts/clear-project-state.sh "$R" "$ID" "$BR" "$SP"     # $SP: the scratchpad, receives the archive
# 10. reset both databases (psql through the tunnel or on the mini)
# verify: git status clean, HEAD == goal-baseline, no work branch, no $R/.gophermind/$ID, then:
gophermind-dev project <brief> ... --preflight-only     # exit 0
```

`scripts/clear-project-state.sh` (tested by `projectrun/clear_script_test.go`, which removes each guard in a copy of the script and expects its case to fail, and which runs the script under a git stub to prove that no git command runs before a refusal) validates every argument before any git command and exits 1 otherwise: the repo is absolute with no `..` component and is resolved through symlinks, then it must not be `/`, `$HOME` or an ancestor of `$HOME` and must hold `.git`; the id is `gm-YYYY-MM-DD-NNN`; the work branch, `BASE_BRANCH` and `BASELINE` are plain ref names (no leading `-`, no `..`) and the work branch is not the base branch; the archive directory exists and lies outside the repo. Then, in order: archive the WHOLE `<repo>/.gophermind` folder (every run id), because `git clean -fdx` removes untracked files, including the run folders of other ids; `git symbolic-ref HEAD refs/heads/<base>`; `git branch -D` the work branch; `git reset --hard goal-baseline` (GOAL command); `git clean -fdx -e .remember` (GOAL command); then remove by exact path the run folder, the scratch folder and the run record. Every git call after validation is `git -C <repo>`.

The user's git wrapper (`~/.local/bin/git-wrapper/git`) blocks `git worktree add`, `git checkout`, `git stash` and `--gw-force`, and may block the three clear commands of GOAL (`git reset --hard goal-baseline`, `git clean -fdx -e .remember`, `git branch -D` on the work branch). Production code and tests never need a blocked command: they use `gitenv.Command` (clean environment) and the git on `PATH`, and the tests probe the wrapper and skip, naming this section, when it blocks the clear. The ORCHESTRATOR must run the three commands in the target repo before the first graded attempt (on a throwaway branch or a copy, to keep the real state) and, if the wrapper blocks any of them, ask John for explicit sign-off before any override. Nothing needs resetting by hand beyond the list above: the ledger and the blackboard are files in the run folder.

`gophermind project --print-state-paths <brief> [--repo <path>]` prints every path to remove, from this table, so the orchestrator never guesses: one line per path, tab separated, `action<TAB>scope<TAB>path-or-name`, where action is one of `delete_dir` (rows 1 and 2), `git_branch_delete`, `git_reset`, `delete_file`, `keep`, `external`. It needs no network, no vault, writes nothing, and honors `GOPHERMIND_CONFIG_DIR` and `GOPHERMIND_VAULT_PATH`. The real-path output is what the orchestrator's guarded deletes use; `projectrun` itself deletes nothing.

## 10. The binary and the version (item 5)

`/opt/homebrew/bin/gophermind` is the 0.9.0 cask (commit `7941ce6`) and has no `project` verb. A graded attempt must run the binary built from the feature branch, and must not replace the cask. The Makefile's `rebuild-all` already stamps `Version=dev+<sha>`, `Commit` and `Date` and `gophermind version` (main.go) prints them, but `rebuild-all` also runs `gofmt -w`, commits and deploys the desktop app, so it is not reused. Ruling: `scripts/build-dev-binary.sh` is a guard script only. It refuses unless the worktree is clean and HEAD is pushed (on a remote branch), then builds `./cmd/gophermind` with the same `-ldflags` to `~/.gophermind/bin/gophermind-dev` and checks that `gophermind-dev version` prints the commit it stamped. The orchestrator runs `gophermind-dev project ... --expect-binary-commit "$(git rev-parse --short HEAD)"`, so a stale binary cannot run by mistake, and the report's first lines and `_state/project.json` carry `binary.path`, `binary.version`, `binary.commit`, `binary.date`.

## 11. The final report

`<run>/_state/project.json` (mode 0600; under `_state/` because `tree.Store.Load` would reject an unlisted root-level `.json` file as a node) and the printed report. Fields: `binary` (path, version, commit, date), `mode`, `graded`, `resumed` (`resumed: no` in a graded attempt), `repo` (path, brief repo, base branch, HEAD at start), `preflight` (checks), `providers` (name and the host that answered, host only, and whether a fallback answered), `secrets` (names and sources), `ambiguity` (brief setting, effective policy, `clarify_defaulted` with id, question and answer for every question settled `unattended-default`, the counts of questions by `answered_by` (`unattended-default`, `probe`, `human`, `accepted`), `rounds` and `clarify_calls` from the question store, `conservative_assumptions` count), `understanding` (confirmed by, understanding hash, from `_state/understanding.json`), `milestone_approvals`, `approval` (by, plan hash, understanding hash), `plan` (functions, waves, warnings, `requirements_covered`), `planner_warnings` (counts of `leaf_defaulted`, `doc_defaulted`, `leaf_normalized`, `outline_id_normalized`, taken from the progress sink's warning events, plus duplicates ignored from `planner.IgnoredDuplicates`, and the `PlannerWarnings` lines), `stages`, `by_node_class` (below), `executor` (the executor's `report.json` embedded, absent when it did not run), `status`, `stop_reason`, `exit_code`.

**The node-class dimension.** The review (`docs/reviews/2026-10-07-planner-executor-v1-v2-review.md` in the main checkout, section 5.3) asks for model performance by node class. Today `report.json` (schema version 2) splits calls by task type and model (`by_task_type`) and leaf attempts by model (`leaves`), and has no class; the ledger's `Call.NodeClass` and `ledger.Summary` do carry it for implement and revise calls. The executor report is not changed by this spec (a new `report.json` key is a schema bump that sub-projects D and G read). Instead `projectrun` builds `by_node_class` for `_state/project.json` and the printed report from three files it can read after the executor returns: the blackboard rows (`blackboard.FS.List`: status and attempts per leaf), `planner.ReadClasses` (leaf id to class, `_state/classes.json`; the same value is the `node_class` field on the node) and the ledger (`ledger.FS.List` with `Filter{TaskType: "implement"}`, grouped by `Call.NodeClass`). One entry per class that occurs in the plan (`pure`, `validation`, `handler`, `client`, `storage`, `concurrency`, `wiring`, `other`, and `unclassified` for a leaf with no class), in that fixed order, with: `leaves`, `verified`, `failed`, `escalated`, `blocked_or_not_run`, `attempts` (attempts whose verdict is not `error`, counted as `report.Build` counts them), `passes`, `first_try_wins` (a leaf whose first counted attempt passed, as `report.Build` defines it), `first_pass_rate` (`first_try_wins / leaves`, rounded to four places, 0 when there are no leaves), `calls`, `prompt_tokens`, `completion_tokens`. Ids, counts and class names only. Failure stage per model (gate, scan, build, vet, test) is not added here: the attempt rows keep a free-text `FailureReason`, not a stage, and the review's per-attempt fields belong to sub-project A or its follow-up. When the executor did not run the table is empty and the printed line says so.

The printed form carries counts and ids, including one line per class (`node class handler: 3 leaves, 3 verified, 2 first try, 5 attempts`); the Clarify defaults appear on stdout as ids only, and the question and answer text stay in `_state/project.json` and `answers.json` (R10: no model-written text in a log line). The printed report ends with the executor summary, whose last two lines are `Requirements covered: N of N` and `Acceptance passed: N of N`; when the executor did not run the two lines are still printed, from the planner's files, with `0 of A` for acceptance and the count from `requirements.json` or the brief, so a log grep is always well formed.

## 12. Changes to existing packages

| Package | Change | Why |
|---|---|---|
| `planner` | `Options.Repo` (plan only; must equal the run record's repo on resume); `Options.Unattended`, which is read in one place: `run.takesRecommendations()` becomes `OnAmbiguity == "assume_and_document" || opts.Unattended`, and `callAsking` calls that method instead of testing the brief field (so Clarify, Confirm and the mid-stage question all follow it); exported `ResolveRepo`, `ReadAnswers`, `ReadApproval` (with `UnderstandingHash`), `ReadUnderstanding`, `ReadQuestionCounts` (`answered_by` counts, rounds and calls from the question store); `answer`, `approval` and the question store types stay unexported; `ReadCoverage` and `ReadClasses` already exist | GOAL repo differs from the brief's; sections 7, 8.2, 8.2a, 11 |
| `cmd/gophermind` (`brief_preflight.go`, `brief_run.go`, `diskfree_*.go`) | the shared checks move to `briefv2/envcheck`; thin same-name wrappers keep every existing cmd test unchanged | `projectrun` and the TUI cannot import `package main` |
| `tui` | `/project` becomes v2; v1 commands renamed `/plan-v1` and `/plan-v1-execute`, with their own messages updated | R2 |
| `cmd/gophermind` | `project.go`, routing next to `brief`, usage text | R1 |
| `scripts` | `build-dev-binary.sh` guard script (the Makefile is not modified) | R5 |
| `docs/briefv2` | README and a runbook | operators |

The executor and the planner's stage code are otherwise unchanged; `executor.Run` is called with its fixed `Options`. `planner.Deps` is not changed: it has no reset hook, because a fresh run starts from no run folder.

## 13. Not in scope

- Deleting v1 (`plantree`, `plan`, `phaseflow`, `orchestrate`): the server, desktop app and `/phase` use them. A removal plan follows the perfect release.
- A Postgres provisioner, a driver, or a database emptiness check.
- A milestone gate, a file gate for `/project` (the file gate for the Confirm stage exists in `brief plan --gate file`), a desktop or server front end for `/project`.
- Putting the node-class dimension into the executor's `report.json` or the failure stage per attempt: `projectrun` builds its own table from the rows (section 11).
- Replacing the brew cask, signing or notarizing the dev binary.
- Reading the vault passphrase from anywhere but `GOPHERMIND_VAULT_PASSPHRASE`.

## 14. Rulings and their cost if wrong

| # | GOAL item | Ruling | Cost if wrong |
|---|---|---|---|
| R1 | 1 | One library entry `projectrun.Run` behind both forms; a run is plan then build in one process; a fresh run refuses any leftover state; `--resume` is explicit and recorded | If the two forms drift, graded runs and interactive runs differ; one parser and one `Run` prevents it. A user who wants silent resume must add a flag |
| R2 | 2 | Keep v1 under new names `/plan-v1` and `/plan-v1-execute`; `/project` is v2 only; remove v1 after the perfect release | Remove now: breaks `/phase`, the server and the desktop app for no graded benefit. `/project --v1`: the goal's "single entry" is ambiguous and a typo runs the wrong planner. Cost of the chosen path: about 110 string edits in `tui`, and two v1 commands still exist for one release |
| R3 | 3 | Unattended is the default and `--attended` exists for people; no `--yes` | If a human wants gates on the real brief they pass `--attended`; if unattended were opt-in the graded command could silently stop for a human |
| R3a | 3a | Secrets: vault harness scope, or `--generate hex32|placeholder` named on the command line and written into the harness scope by a thin pre-step (the planner's `storeSecrets` does the copy); GopherMind generates `JWT_SIGNING_KEY` and `STUDIO_LLM_API_KEY`; both Postgres URLs are human prerequisites; loopback and distinct databases enforced, tunnel command printed, never started; a stale run-scope value is overwritten on a fresh start | A generated placeholder LLM key fails a bullet that needs a real key: the attempt fails honestly and a vault value (which wins) fixes it. A database not loopback would fail late, inside acceptance, which the preflight prevents |
| R3b | 3b | Unattended makes the planner take recommendations (`takesRecommendations()`): every Clarify round's frontier takes its `recommended` answer, recorded `unattended-default` with a decision record, never invents; a question with no recommendation is a parse failure and then a stage failure; mid-stage questions use the existing conservative rule and are listed | A wrong default becomes a wrong assumption in the plan, visible in the approval summary and in `_state/project.json`; the acceptance bullets still decide the build |
| R3c | 3c | Approval by the unattended gate, bound to the plan hash, only at `C == N`; milestone approvals recorded as covered by it | A reader may expect a milestone pause; the line in the report says there is none |
| R3d | 3d | The stop table of 8.4, each with a status, reason, exit code | A missing row is a run that ends without a reason; a test walks the table |
| R4 | 4 | State table of section 9, clear order including `branch -D` on the work branch (`gm/<id>` unless the brief sets `work_branch`) and the guarded removal of the run folder, an archive of the run folder before the clear, `--print-state-paths`; the wrapper-blocked commands are tested by the orchestrator first and overridden only with John's sign-off | Skipping the branch delete carries an old attempt into a new one; clearing without the archive destroys the ledger, the events and the saved attempts of the attempt being recorded |
| R5 | 5 | `gophermind-dev` built by a guard script (refuse unless clean and pushed) around the Makefile's own ldflags; `--expect-binary-commit`; version and commit in the report | The cask stays 0.9.0, so a typed `gophermind project` fails with an unknown verb, which is loud and safe |
| R6 | 6 | Tests of section 15, offline, no real `~/.gophermind` | A graded attempt is the only proof against the real model |
| R7 | (1, 3a) | Preflight failure is exit 6, is not a run failure, writes nothing, and reports every missing item at once | An orchestrator treating 6 as a failed attempt would waste attempts; the GOAL loop says preflight is fixed and rerun |
| R8 | (1) | Leftover run folder or work branch without `--resume` is refused | A crash recovery costs one flag; accidental carry-over would be silent |
| R9 | (1) | `--repo` overrides the brief's repo at plan time and run time | Without it the brief would have to be edited by hand, which GOAL forbids |
| R10 | (11) | Printed report holds ids and counts only; model-written text stays in 0600 files | A reader must open `_state/project.json` to see the defaulted questions |
| R11 | (8.3) | Milestone flag recorded, not enforced | See R3c |
| R12 | (1) | `--graded` (implied by `--expect-head`) refuses `--resume`, `--attended` and any leftover state, requires a clean target at the expected HEAD, reports `resumed: no`; a graded attempt is one invocation | A graded attempt that quietly resumed would not count; the sticky `Resumed` flag makes the violation visible (`graded: INVALID`) |
| R13 | (7) | The shared environment checks live in `briefv2/envcheck`; `brief run --check-env` and `projectrun` call the same code | Two copies would drift; the extraction is behavior-neutral and proven by the unchanged cmd tests |
| R14 | (3, 8.2a) | `ConfirmUnderstanding` is decided by the planner's rule when unattended (store complete, nothing open, `confirmed_by: unattended`, understanding hash in `approval.json`); the unattended gate's `Confirm` fails loudly if ever reached; attended runs use the terminal gate | A reader may expect a human to read `UNDERSTANDING.md`; the report names `confirmed_by` and prints the assumptions count, and `UNDERSTANDING.md` lists each assumption |
| R15 | (11) | The node-class table is built by `projectrun` from rows, classes and the ledger; the executor's `report.json` is unchanged | `brief report` does not show it; a later schema bump can move it |
| R16 | (5, 7) | `Run` recovers a panic in its own goroutine and ends `harness_fault`, exit 7, with the report written; goroutines the executor starts (`executor/schedule.go` workers, `limits.go`, `serve.go`, `leaf.go`) are out of that recover's scope and are the executor's to recover. Prompts of the attended gate and the secret prompt are written straight to stderr, never through the lossy progress queue | A panic in an executor goroutine still kills the process (exit 2) with no `project.json`; the executor owns that fix |

## 15. Tests

All offline: fake provider, `t.TempDir()`, `GOPHERMIND_CONFIG_DIR` and `GOPHERMIND_VAULT_PATH` set to temp paths, a fast vault work factor, loopback listeners for database checks, no real network and no real `~/.gophermind`. There is no database file in any test, and the end-to-end tests assert that none was created. The fixture is the executor's greeter (5 leaves, 3 waves, `executor/testdata/greeter`), edited by the test copy to `on_ambiguity: halt`, `milestone_approvals: true`, two declared secrets, and a Clarify reply (`clarify.txt`, with the existing `clarify.more.txt` answering `[]` to the follow-up call) with two questions that carry `recommended` answers, the second depending on the first, so a second round runs. The fixture already holds the Enrich replies (`enrich.*.txt`, `enrich_comp.*.txt`, `enrich_root.txt`). `projectrun` tests cannot import the executor's `rig_test.go`, so the end-to-end tests have their own small rig (`testhelp_test.go`). Git in tests goes through `gitenv` and the PATH git, with `GIT_*` stripped in `TestMain`; briefv2 tests need `-timeout 30m`, the executor package alone takes about 11 minutes, and each end-to-end test must finish in under 3 minutes with `-count=1`.

| Requirement | Test |
|---|---|
| One process plans and builds; unattended recommendations, confirm by rule and auto-approval; proof lines last | `TestProjectE2EGreeter` |
| Planner failure stops before the executor, exit 1, report still printed and well formed | `TestProjectPlanFailureExit1` |
| Escalation exit 4, interruption exit 5, exit table | `TestProjectEscalationExit4`, `TestProjectInterruptedExit5`, `TestExitCodes` |
| Stop table walk | `TestEveryStopConditionHasReasonAndCode` |
| Preflight lists everything missing, calls no model, writes nothing | `TestPreflightMissingListedNoModelCallNothingWritten` |
| Each preflight check | `TestPreflightRepoChecks`, `TestPreflightGraded`, `TestPreflightProvidersFallback`, `TestPreflightDatabaseTunnelHint`, `TestPreflightHumanListNumbered`, `TestPreflightStaleStateAndBranch`, `TestPreflightSecretMatrix`, `TestPreflightVaultPassphrase`, `TestPreflightDatabaseLoopbackAndDistinct`, `TestPreflightRequirePrivate`, `TestPreflightExpectBinaryCommit`, `TestPreflightToolsAndSettingsNotCreated` |
| Preflight failure is not a run failure | `TestPreflightFailureExit6IsNotARun` |
| Secrets: vault beats generated, generated shapes, stale run scope overwritten on fresh start, kept on resume, no value anywhere | `TestVaultBeatsGenerated`, `TestGenerateShapes`, `TestRunScopeOverwrittenOnFreshRun`, `TestRunScopeKeptOnResume`, `TestGeneratedValueNeverPrinted`, `TestNoSecretValueAnywhere` |
| Clarify recommendations (rounds still run), required recommendations, mid-stage rule | `TestUnattendedClarifyTakesRecommendationsOverHalt`, `TestUnattendedClarifyRequiresRecommendations`, `TestUnattendedMidStageQuestionAssumes`, `TestAttendedClarifyUnchanged` |
| Confirm: unattended by rule, hash into approval, refused with an open question, gate fails loudly, attended gates | `TestUnattendedConfirmRecordsUnattendedAndBindsApproval`, `TestUnattendedConfirmRefusesOpenQuestions`, `TestUnattendedGateConfirmFailsLoudly`, `TestAttendedConfirmUsesGate` |
| Approval binds to the hash, `approved_by unattended`, refused below N of N | `TestUnattendedApproveBindsHash`, `TestUnattendedGateRefusesGap` |
| Repo override | `TestRepoOverrideUsedForRunFolder`, `TestRepoOverrideMustMatchOnResume` |
| State paths complete and classified | `TestStatePathsListEverything`, `TestStatePathsNoSideEffects` |
| Node-class dimension | `TestClassStatsAggregatesRowsClassesAndCalls`, `TestReportCarriesNodeClassTable`, `TestPrintedReportNodeClassLines` |
| Clear then rerun starts clean | `TestClearedStateRerunsClean` |
| Resume flag, no-restart rule, graded mode | `TestResumeFlagContinuesAndIsRecorded`, `TestHealthyPathNeverResumes`, `TestRunGradedReportsResumedNo`, `TestGradedRefusesSecondInvocation` |
| Shared checks extracted without change | the unchanged `cmd/gophermind` suite, `TestResolveBaseURLsFallbackUsed` |
| Report carries warnings, defaults, answering host, binary | `TestReportCarriesPlannerWarnings`, `TestSinkCountsPlannerWarnings`, `TestReportRecordsAnsweringHost` |
| Version and commit in the report | `TestReportCarriesBinaryVersion` |
| CLI and TUI share the parser and `Run` | `TestParseArgsTable`, `TestProjectCLIExitCodes`, `TestSlashProjectRunsProjectrun`, `TestSlashProjectRefusesAttended`, `TestPlanV1CommandsRenamed` |
| Manual, not CI | `--preflight-only` on the real brief (plan Task 14), then the GOAL loop |

## 16. What a human must provide before the graded attempt

These are prerequisites, not run steps. A preflight that finds any of them missing exits 6 before any model call and prints them as a numbered list, each with its exact fix command. Before the first graded attempt the orchestrator also tests the three clear commands of section 9 against the git wrapper in the target repo and asks John for sign-off if any is blocked.

1. `GOPHERMIND_VAULT_PASSPHRASE` exported in the orchestrator's environment, and a vault that opens with it (`export GOPHERMIND_VAULT_PASSPHRASE=<passphrase>`).
2. Two empty Postgres 15 or later databases, reachable at `127.0.0.1` (for a database on the mini, `ssh -N -L 55432:127.0.0.1:5432 mini` kept up for the whole attempt; the preflight prints this exact command when the dial fails), their URLs stored with `gophermind brief vault set DATABASE_URL` and `gophermind brief vault set TEST_DATABASE_URL`, and a way for the orchestrator to drop and recreate both between attempts.
3. `~/.gophermind/gophermind.yaml` with `privacy.mode: private_only`, every tier (`strong`, `standard` and `any`: node-scope planner stages route by the node's `model_tier`) pointing at `mini/qwen3.6:35b-a3b`, `base_url_fallbacks` holding the mini's VPN address `http://10.8.0.6:11434/v1` (used when the LAN address `192.168.1.35` does not answer), and `toolchain.PATH` containing the directory of `go` (here `/opt/homebrew/bin`).
4. The mini reachable with Ollama running (checked by TCP only; GOAL's memory and swap checks stay the orchestrator's).
5. Optional: a real `STUDIO_LLM_API_KEY` in the vault, needed only if a bullet turns out to call a real LLM (the generated placeholder covers the fake-LLM bullets).

Everything else (the JWT key, the placeholder LLM key, run scope, approval, clarify defaults) GopherMind does itself.
