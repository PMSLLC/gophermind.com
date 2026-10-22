# Changelog

All notable changes to GopherMind are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

- **A type that a signature uses but no type declares is now asked for.** After the components are written, the contract stage lists the Go types named in function signatures (parameters, results and receivers) that no type declares and sends them to the existing repair pass. Before, the plan went on with a warning and the test-writer later refused every correct test of the function, because its stub could not compile. If the repair budget is spent the plan still continues, with a `signature_type_undeclared` warning.
- **A provider that cannot answer a request is skipped, not cooled down.** `provider.RequestFilter` lets a provider say it takes only some requests. The Cline provider takes only a leaf-file request (a worktree and a target file), so the planner's calls skip it with no ledger row; before, each one failed as transient and cooled the whole chain.

## [0.10.0] - 2026-10-21

### Added

- **A Cline provider.** A provider of `kind: cline` in `gophermind.yaml` lets the Cline CLI (`npm i -g cline`, a ClinePass or Cline credits account) write a leaf's target file in a scratch copy of the worktree; the executor reads back only that file and every existing gate still applies. It is opt-in and for public providers only. The executor now passes the leaf worktree and target file to the provider, and `cline_settings_dir` accepts a leading `~/`. See `docs/build/cline-provider.md`.
- **`/project` now runs the v2 planner and executor in one command.** `gophermind project <brief>` (and `/project <brief>` in the TUI) runs the preflight, plans the brief and builds it. Unattended runs take Clarify recommendations and confirm the understanding by rule, recording `confirmed_by: unattended` and its hash. A failed preflight exits 6 with a numbered list of what a human must provide. `--graded` marks a single graded attempt. The project report carries a node-class table. `scripts/build-dev-binary.sh` builds the version-stamped dev binary.
- **Parallel leaves.** `executor.workers` (1 to 8, default 1) and `gophermind brief run --workers n` run the leaves of one level at once, each in a private copy of the repository. Landing order is fixed by leaf id and the work branch stays a linear history. With 1 nothing changes.
- **A revise stage.** When a leaf has used its whole model ladder, or answers `CONTRACT_PROBLEM`, the planner rewrites the node (description, contract text, tests, groups) and the build goes on from the rewrite, bounded by `defaults.max_revisions`. A changed signature or `depends_on` is flagged to a person, not rewritten. Rewrites and decisions are kept in `_state/revisions/` and `decisions/rv-*.md`. The `revise:` section of `gophermind.yaml` (`mode`, `model_tier`, `context`, `rewrite_tests`, `max_tokens`, `max_refusals`) configures the rewrite.
- **`gophermind brief plan` and `gophermind brief resume`.** `gophermind brief plan <brief.md>` and `gophermind brief resume <run-id>` take a brief through Load, Clarify, Contract, Decompose, Coverage, Approve and Test-writer (the run id is the brief's `id`), with `--yes`, `--gate terminal|file`, `--fake <fixture-dir>` (canned replies, no settings, network or model) and `--allow-public`; exit 3 means the run is waiting on a human, and `plan` refuses a run folder that already holds stage output and points at `resume`.
- **`gophermind brief status`, `coverage`, `calls` and `report`.** `gophermind brief status`, `coverage`, `calls` and `report [--json] <run-id>` print each stage and what the run waits for (plus executor lines: status, waves done, leaves verified, blocked leaves), which nodes and tests cover each requirement, the model-call ledger (provider, model, tokens, duration, outcome; it records only sizes and SHA-256 hashes of prompts and replies, never their text), and a run's report.
- **`gophermind.yaml` is written with defaults on first use.** `<config dir>/gophermind.yaml` is written with defaults the first time `plan` runs: `providers` (OpenAI-compatible, `base_url`, `visibility`, `max_concurrent`, `api_key_secret` naming a vault entry), `models` listing `provider/model` fallback chains per tier (`strong`, `standard`, `any`), `privacy.mode` (`need_to_know` lets public providers see single functions only, or `private_only`), `rate_limits` cooldowns and backoff, `defaults` and `human.mode`.
- **The `executor:` and `toolchain:` sections of `gophermind.yaml`.** The `executor:` section (`fix_attempts`, `repair_rounds`, `acceptance_repair_rounds`, `test_timeout_seconds`, `acceptance_timeout_seconds`, `stale_claim_seconds`, `output_cap_bytes`, `heartbeat_seconds`, `max_run_minutes` (default 720), `go_mod_cache`, `sandbox`, `proxy.listen`, `proxy.log`, `artifacts` on or off and `artifact_max_bytes` for the scrubbed replies and check output saved under `attempts/` and `_state/replies/`) and a `toolchain:` section whose `PATH` is the only toolchain setting a file holds (the executor adds `HOME`, `GOCACHE`, `GOMODCACHE`, `GOPATH` and `TMPDIR` itself; a build sees nothing from the parent environment); every number must be positive and an explicit zero retry count is rejected.
- **New keys under `defaults:`.** `clarify_max_calls` (default 4), `clarify_max_questions` (default 30, 1 to 1000), `enrich_batch_size` (default 4, 1 to 32) and `route_by_node_tier` (default true); an explicit 0 is refused.
- **`reasoning_effort` per provider, with truncated and empty replies handled.** A provider may set `reasoning_effort` (`none`, `low`, `medium` or `high`; the mini defaults to `none`) for thinking models that answer in a reasoning field; a truncated reply is retried once with a doubled token budget (capped at 16384) and an empty reply is a typed failure, both before failing over to the next model.
- **`base_url_fallbacks`, private-network checks and per-provider timeouts.** A provider entry may list `base_url_fallbacks`, tried in order at preflight and at the start of a run when `base_url` does not answer; a private provider's `base_url` and fallbacks must be private-network hosts (loopback, RFC 1918, 100.64/10, link-local, IPv6 ULA, `.local`, `.internal`, `.lan`) and a public provider's must be public. Per-provider `call_timeout_seconds` overrides `defaults.call_timeout` and `rate_limits.cooldown_after_timeout_seconds` (default 60) cools down a chain entry whose call timed out; provider calls never follow redirects and ignore the proxy environment. The default `mini` entry gains the VPN fallback `http://10.8.0.6:11434/v1` and a 1500 second call timeout.
- **Human gates.** With `--gate file` (or `human.mode: file`) the planner writes `QUESTIONS.md` and `APPROVAL.md` into the run folder and exits 3 until they are filled in, `--gate terminal` asks in the terminal, and an approval is bound to the plan's hash, which covers function signatures, file paths and `depends_on`, so editing any of them after approval invalidates it.
- **Clarify in rounds, with a fact probe and a Confirm gate.** Clarify runs in rounds after a fact probe of the target repo (go.mod, directory listing, exported Go names only, no commands run); every question carries a recommendation and `accept` takes it in the terminal or in `QUESTIONS.md`, then a Confirm gate asks the person to approve the planner's understanding (y/N in the terminal, or `approve`/`reject: <reason>` in `UNDERSTANDING.md` in file mode) before planning continues.
- **`gophermind brief answer`.** `gophermind brief answer <run-id> <question-id> <new answer...>` changes a settled answer and restarts the planner from the stage that used it (the whole Contract stage, or one component's plan); run `gophermind brief resume` afterwards.
- **Coverage.** Every top-level bullet under `## Constraints` and `## Acceptance` and every `###` under `## Features` is a requirement; gaps go back to the model for up to `defaults.max_coverage_rounds`, and if any remain the run stops with exit 1, lists them, and nothing is approved.
- **The contract stage works within small model windows.** The outline is built in resumable passes (a shared pass, then batches of features), every contract prompt is sized to the smallest model in the chain (with a `prompt_size` event), a reply cut at the token limit is treated as a truncation and retried, dangling references and missing contract fields are repaired in bounded passes instead of ending the run, ids are normalised into one namespace where the first emission wins, and errors never quote more than a bounded id.
- **The contract stage plans an entry point.** A brief that names a command to run or describes a CLI tool or server gets a `cmd/<binary-name>` package, every main package must declare `func main()`, a warning is emitted when either is still missing after a retry, and `security.untrusted_inputs` accepts the process-level sources `os.Args`, `os.Stdin`, `os.Environ`, `os.Getenv` and `flag.Args`.
- **The test writer is stricter and compile-checks what it accepts.** The test writer refuses tests that import anything outside the standard library, the module and the contract packages, writes files crash-safely, never writes through a symlink, stops rather than overwrite a test file it did not write, and compile-checks each accepted test file against the contract stubs at plan time; compile refusals name only predeclared, standard library or contract-declared types and functions, and Go syntax errors report line and column.
- **The approval plan shows node groups and revision notices.** The plan a person approves has a Node groups section (not-applicable counts per group, trust boundaries per component, nodes no requirement names, Enrich warnings, assumptions and open questions, and a note when Enrich has not run or facts were unavailable) and shows a revision notice when the planner rewrote nodes.
- **Acceptance root tests are held to a quality gate.** The coverage stage flags tests that never exercise the built binary as `acceptance_vacuous` and asks the model to repair them, the planner refuses commands that cannot fail for the right reason (`masked_failure`, `success_echo`, `placeholder`, `undefined_variable`, `curl_unasserted`, `jq_unasserted`, `print_only`), a weak test left after the fill rounds stops the plan before approval, `APPROVAL.md` gains a Quality column, narrative acceptance bullets get a Go test `acceptance/<id>_test.go` run against a `serve` declaration kept in `coverage.json`, plain `go test` commands are judged with `go test -json`, and at Wave 0 each server-exercising root test must fail against the stubs or the run stops with `acceptance_not_red`.
- **Five new example briefs.** `gophermind-osx/examples/briefs` gains 04-csvstat, 05-retryx, 06-hookrelay, 07-ledger-sync and 08-taskboard, between them exercising every frontmatter option; all validate with no scan warnings.
- **`gophermind brief run` builds an approved plan end to end.** `gophermind brief run <run-id>` runs a preflight, Wave 0 (types, stubs, the deps step, a scan, the red check and one commit), a leaf loop with a fix-then-next-model ladder and failure classes, waves with integration checks that attribute a failure to the leaves that caused it, a bounded repair loop that reopens only the attributed leaves, and a final landing, printing one status line per event on stderr. With `landing: commit` the build works on the branch `gm/<brief-id>` with one exact-path commit per leaf and an empty final commit carrying the summary, then fast-forwards the base branch (if the base branch moved the run stops `failed` with `landing_blocked` and the work branch is left intact); `diff_only` writes `changes.patch`. GopherMind never pushes, forces, resets, rebases or stashes, and the executor requires the repository root.
- **`gophermind brief run --check-env`.** `gophermind brief run <run-id> --check-env` runs the environment preflight with no model call (sandbox, go toolchain, git repository, clean tree, vault passphrase, declared secrets, provider reachability, approval, disk space), prints `pass:`/`FAIL:` per check and exits 6 when any check fails.
- **`brief run` exit codes follow the run status.** 0 verified, 1 failed, 3 waiting on a human, 4 escalated, 5 interrupted, 7 for a harness fault (repository cannot be opened, sandbox refused, invalid settings, plan changed since the run started, leaf held by a live worker), recorded in the report as stop reason `harness_fault` so it can be told apart from a failed run.
- **`report.json` and the run summary.** Every `brief run` writes `report.json` (`schema_version` 3; an older binary refuses a newer report) and prints a summary ending in `Requirements covered: N of N` and `Acceptance passed: N of N`; the report carries planner warnings, optional `revised_leaves` (node id, revisions, changed fields, tests_rewritten, flag, refusals) and `rewritten_leaves` with a `Rewritten leaves:` summary line, an optional `parallel` object (`workers`, `parallel_levels`, `landing_conflicts`) with a `Parallel:` line, an unfinished-leaf list when the run stops, and a report that would contain a credential is not written.
- **Bounded runs, interrupts and resume.** `executor.max_run_minutes` bounds one invocation and SIGINT or SIGTERM end it the same way: claims are released and the run ends `interrupted` with exit 5. Running `gophermind brief run <id>` again resumes from the run state: finished leaves are not rebuilt, stale claims and a dead holder's lock (after a kill -9) are reclaimed instead of timing out, private trees are swept, a repair or an unfinished revision cut off by a kill is resumed without a model call, a leaf held by a live worker is refused, and a resume stops when the plan files changed, commits that are not its own sit on the work branch, the repository moved or the base branch moved.
- **The deps step goes through an allowlisted loopback proxy.** The deps step reaches the network only through a loopback forward proxy with a host allowlist built from the brief's `network` entries, each provider `base_url` (pinned to its port) and the Go module hosts; the proxy writes `proxy.log` (no paths or bodies), a failure on a host marked `critical` or three failures in a row stops the run, and a cancelled tunnel is not counted as a failure.
- **An acceptance run against freshly built binaries.** The plan's root tests (one per acceptance bullet, N taken from `requirements.json`, plain `go` commands accepted) run against freshly built binaries, which are rebuilt after an acceptance repair; a bullet with no root test stops the run before it starts, and the run is rejected when a test is vacuous, a test or built file was tampered with, or a stray file or a listening port is left behind. `acceptance.json` records each command, exit code, output size and SHA-256.
- **Escalation to a person.** A leaf that has used its whole model ladder, or reports a contract problem, is escalated to a person through the human gate (`--gate terminal|file`, the file gate writing `ESCALATION-<node>.md` into the run folder), telling the reviewer how many times the planner rewrote the node and what it proposes; no answer ends the run with exit 4 (terminal) or 3 (file), the report records who answered, and leaf loops run one at a time with a scheduler that orders them by level.
- **Revisions are commits on a hash chain.** A revision lands as its own commit on the work branch, subject `gm(revise): <node> test, revision <n>` with a `GopherMind-Revision` trailer, recorded as a `revision` event in `_state/events.jsonl`; the executor refuses to start a run whose revision log or hash chain is missing or broken, or whose node revision field is off the log's tip, and stops with reason `plan_changed`.
- **Parallel runs and the gate.** Parallel runs ask the gate one question at a time and warn at start when `executor.workers` exceeds the largest provider `max_concurrent`; a level falls back to serial with a warning when its base copy cannot be made or the tree copy is over 256 MiB, and overlapping file paths split a level into sub-levels only when workers is above 1.
- **New `gophermind project` flags.** `gophermind project` takes `--repo <path>` (overrides the brief's repo; with `--resume` it must match the run's recorded repo), `--generate NAME=hex32|placeholder` (repeatable; creates a secret the brief declares and stores it in the vault's harness scope), `--require-private`, `--expect-head <rev>` (implies `--graded`), `--expect-binary-commit <sha>` (7 to 40 hex characters), `--resume`, `--attended` (CLI only; the TUI never prompts) and `--preflight-only`; flags are two-dash only, no flag but `--generate` may repeat, and `--graded` needs `--expect-head` and can neither resume nor be attended.
- **`gophermind project --print-state-paths` and `scripts/clear-project-state.sh`.** `--print-state-paths` lists everything a clean attempt must clear, and `scripts/clear-project-state.sh <repo> <id> <work-branch> <archive-dir>` clears it: it archives the whole `.gophermind` folder, bundles the work branch, verifies `BASELINE` (default `goal-baseline`) and resets the repo onto `BASE_BRANCH` (default `main`), refusing `/`, `$HOME`, relative, `..` or symlinked paths.
- **A `/project` run writes `project.json`.** A `/project` run writes its full report to `.gophermind/<run-id>/_state/project.json` (binary version and commit stamp, preflight checks, each secret's source, how ambiguity was settled, planner warning counts, the graded verdict and its reason, any coverage error) with free text capped at 200 characters, and prints a one-screen summary.
- **`GOPHERMIND_DEBUG_DUMP_DIR` (developer only).** Setting it makes the planner write each model request and raw reply (mode 0600) to that directory; it is ignored with a warning when the directory is inside the repository, the run folder or the config folder, and is not to be set for a graded run (nothing enforces this).

### Changed

- **Breaking: the pipeline contract.** The server's `/pipeline/state`, `/pipeline/events` and `/pipeline/report` and gophermind-osx's pipeline panel (the native app) now read the run folder (nodes, runtime files, event log, report) instead of `.planning/assignments.json`. Every body carries `schema_version` 1 and every response the header `X-Gophermind-Pipeline-Schema: 1`. New routes: `/pipeline/runs`, `/pipeline/node`, `/pipeline/artifact`, `/pipeline/decisions`. Event streams carry ids, resume with `since` or `Last-Event-ID`, and are capped at 16 (`GOPHERMIND_PIPELINE_MAX_STREAMS`). The server and gophermind-osx must be updated together; a pair that disagrees shows a message in the panel and chat keeps working. (The Wails app in `desktop/` has no pipeline panel and is not affected.)
- **Breaking: run state is files, not SQLite.** A run's nodes, runtime state (`<node>.runtime.json`, claims and attempts), event log (`_state/events.jsonl`), call log (`_state/calls.jsonl`), saved attempts and report are all files in `.gophermind/<run-id>/`. `blackboard.db` is no longer created; a leftover one from an older version is ignored.
- **The pipeline is node based.** Planning and building work on a tree of nodes with groups and a contract slice, replacing the task-based `.planning/assignments.json` pipeline.
- The `briefv2` package folder is now `gophermind-lib/build` and `docs/briefv2` is `docs/build` (import path change only, no behavior change).
- **Breaking: `/project` takes the brief path as its only positional argument.** The v0.9.0 form `/project <name> <brief>` no longer works and is refused with a message asking for the brief path only.
- **Reserved component ids.** The component ids `attempts` and `decisions` are reserved alongside `logs`; a brief whose plan names a component that way is refused.
- **Executor stubs exit 86.** Executor stubs print `gm: not implemented` to stderr and exit with status 86 instead of panicking with status 2, so an acceptance test that only checks for exit code 2 no longer passes against a stub.
- **Whole-file replies are stripped to the leaf.** The executor strips contract-declared types and other leaves' functions from a model reply that returns the whole file (event `leaf_reply_stripped`, count only) and gives the model a retry note specific to what was wrong instead of a generic one.
- **The server's pipeline dashboard page.** It shows the run list, node tree, decisions, attempts and saved check output from the run folder, resyncs state on every stream connect without applying replayed or stale frames, keeps an open artifact and scroll position across re-renders, and stops retrying a stream refused with 401, 403 or 404 (other errors back off to 30 s). `invalid_brief` (the brief was rejected) is a documented run status in the pipeline contract; the dashboard treats it as terminal and fetches that run's report.

### Removed

- **`gophermind phase` and `/phase` are removed**, with PhaseFlow's roadmap workflow, its embedded prompts and its `.planning/` files (`ROADMAP.md`, `STATE.md`, `assignments.json`). There is no replacement for the roadmap loop. `gophermind phase ...` and `/phase` now print a notice and the CLI exits 2; plan and build a brief with `gophermind brief plan` and `gophermind brief run`, or with `/project`. Existing `.planning/` folders are not read. The notice stub is removed in the release after this one.
- **The v1 planner and runner are removed**: the `plantree` and `orchestrate` packages, `/plan-v1`, `/plan-v1-execute`, `/project-execute` and `/questions`. Typing one of them prints a notice pointing at `/project <brief>`; the stub goes with the `/phase` stub in the next release. The `gophermind-phaseflow` skill pack (`.gophermind/skills/gophermind-phaseflow.md`) and the `examples/phase-flow/ai-interview` example are deleted.

### Fixed

- **`gophermind brief validate` false secret warnings and silently dropped requirements.** It no longer warns that HTTP methods (`GET`, `POST`, `DELETE`, ...) or an error name after a status code (`401 BAD_CREDENTIALS`) look like undeclared secrets, and a brief with an unclosed code fence or a non-bullet line under `## Constraints` or `## Acceptance` is rejected with the line number instead of silently dropping the requirements after it; `+ `, `1. ` and `1) ` now count as bullets.
- **A dependency cycle between two functions no longer fails every wave plan on each resume.** The planner drops the back edge deterministically (contract order), warns by node id, and records the result.
- **Decompose repairs one node instead of discarding a batch.** Decompose repairs the one defective node of a batch instead of discarding and re-requesting the whole batch, normalises nodes whose shape deviates (side_effects objects become strings, strings become arrays, null becomes empty, `model_tier` is case-folded, forbidden properties dropped, empty titles derived from the signature) with a `leaf_normalized` warning, reports remaining schema defects by field and keyword, rechecks pending nodes from an earlier run before any model call, and repairs node prose that names fields its declared struct does not have (ignoring qualified types and file-like words).
- **Executor safety fixes.** An attempt is recorded as passed before a leaf is marked verified, a panic in a leaf is recovered, a claim is released on a cancelled retry, any `Fatal` or `Panic` method call the final scan would reject is refused at the reply gate, `diff_only` landing refuses foreign paths and secrets, and a plan whose coverage does not reach every requirement stops with `coverage_incomplete`.
- **Revision fixes.** A revise reply that returns the whole node is read as a patch of what changed (a changed frozen field is refused by name, an unchanged reply is `revise_noop`), the test replacement temp file is named `.gm-replace-<base>.<hex>` and resume removes a leftover one beside a node with an open revision, a committed rewrite is no longer counted twice after a human retry, a revision commit can no longer be mistaken for a leaf commit, and ReplaceTest keeps the test file's permission bits.
- **Gate text is printed on one line without format characters.** Gate text written by the model (proposal, reason, history) is printed on one line with bidi overrides, zero-width and other format characters dropped and is capped at 600 runes, so a reply cannot hide or reshape what a reviewer sees in a terminal or gate file.
- **Plan approval refuses while questions are open.** Plan approval refuses while questions are still open or when the approval predates the confirmed understanding, and the confirm stage refuses a hand-edited `answers.json` instead of overwriting it.
- **gophermind-osx stream and body limits, and the pipeline panel after a late connect.** The native app no longer fails on an event stream line over 64 KiB (the limit is now 8 MiB, with an explicit too-long error beyond it), reports a response body over 4 MiB as too large instead of silently truncating it, and its pipeline panel fills in after a later connect instead of staying empty until relaunch when the app started with no reachable backend or was disconnected.
- **Nested `build` directories are visible to the agent.** The list_files, glob and find_symbol tools no longer skip a build directory below the repository root; only the top-level `build/` is ignored, so folders such as `lib/build` are visible.
- **More model replies are repaired instead of failing the stage.** The planner repairs a stray backslash inside a string, boolean, number, object or array values for given and expect, an unambiguous shortened Clarify recommendation, over-long why_it_matters and recommended_why fields truncated instead of rejected, a test file package clause checked against the last path element with the module and package import path stated in the prompt, and an executor leaf package taken from the clause name when the contract gives a directory; schema errors list the allowed properties, JSON syntax errors give line, column and kind, and enrich repair prompts state every defect in plain words.
- **Contract signatures and file names are normalized at the contract stage.** Same-package qualifiers are dropped, types declared in exactly one other package are qualified, kebab-case type ids become Go type names, bare file names are joined with the package directory, and every function gets a file no other function or type uses so a leaf no longer overwrites shared type declarations (warnings `signature_qualifier_normalized`, `file_path_normalized`, `function_file_unique`; the executor applies the same rules when loading an older `contracts.json`).
- **Model-written test files are repaired before validation instead of refused.** Unused imports and long comment blocks are dropped (`test_import_normalized`, `test_comment_normalized`), helpers that collide with another accepted test file are renamed, an unterminated string on a pretty-printed line is closed, and empty given or expect strings are accepted.
- **Test-writer refusals are specific and the prompt states the rules.** Tests of a main package must be package main and not import it, comment-heavy files and files that redeclare a package name or define contract-declared code are refused with their own messages, an unexported function under test accepts only the plain package clause, a test is internal when the signature uses unexported types, imports outside std, the module and the contract packages are refused by name, covers refusals state the error count and the valid tags, and `boundary` is now a valid covers tag so edge-case tests that concern no single parameter are no longer refused.

### Security

- **Every child process runs in a `sandbox-exec` profile.** Every child process the executor runs (`go build`, `go test` and what they spawn) is wrapped in a macOS `sandbox-exec` profile that denies Launch Services, the pasteboard, security daemons, non-loopback network, signals outside the child's process group, secret-named files and writes under `<repo>/.gophermind`; the runner opens only the loopback ports a command's environment names, sweeps `setsid` descendants after a command ends and warns when one survives. When `sandbox-exec` is missing the run fails with a message, and `executor.sandbox: off` runs without one.
- **Secrets never reach a model prompt, a saved attempt or the report.** The prompt packer scrubs every declared secret form (including encoded ones) from failure output, the leaf, wave and report paths use the same check, a reply whose source holds a secret (even one that appears only after decoding JSON or Go escapes) is refused as `secret_in_reply` and never saved, retry notes and warnings are scrubbed, and saved check output drops the line the output cap cut so a cut secret prefix is never written.
- **Untrusted text cannot close a prompt section.** Untrusted text (the brief, model replies, repo facts) can no longer close a data section of a planner or executor prompt; section tags in it are neutralized before the prompt is built.

## [0.9.0] - 2026-09-29

### Added

- **`/project` plans a brief into a tree on disk.** `/project <name> <brief-file>` now reads the brief in bounded chunks, one fresh-context model call per chunk, so a long brief no longer has to fit one context window. Pass sizes are derived from the model's context window. The tree on disk is the state, so an interrupted run resumes where it stopped and refuses to resume against a changed brief.
- **A question round.** While planning, the model asks instead of guessing. `/questions` walks you through them, you can change an answer later, and only the steps that answer invalidated are re-planned. Open questions block approval.
- **Approve and export.** Approving the plan verifies it first and takes a run lock, then writes the legacy `ROADMAP.md`, `assignments.json`, `SPEC.md` and `PROJECT.md` that `/project-execute` reads.
- **`gophermind brief`**, the foundations of the v2 brief-to-build feature: `brief validate <brief.md>` checks a brief's frontmatter against an embedded schema and its required sections (exit 2 for an invalid brief), `brief vault set|list` keeps secrets in an age-encrypted vault, and `brief tree check <run-dir>` validates a task tree (cycles, waves, structure). A brief can declare non-secret `env` variables with defaults; a name in both `secrets` and `env`, a name declared twice, or a name the harness reserves (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `GOPHERMIND_NODE`, `PATH`, `HOME`, `GOCACHE`, `GOMODCACHE`, `GOPATH`, `TMPDIR`) is rejected. Undeclared secret-looking names produce warnings only. The planner and executor for v2 are not part of this release.
- **gophermind-osx:** approve and deny buttons with an auto-approve checkbox, Return sends and replies continue the session, live progress for a running turn, Start Breakdown wired to the live connection, and working Sessions and Model panels.

### Changed

- `/project` no longer runs the old interview; it plans from the brief you give it.

### Fixed

- **A session's working directory was ignored by the server**, so work ran in the server's own root. Sessions now run in the folder they were created with. gophermind-osx passes the chosen folder.
- **The model's context could be overrun.** A single `read_file` is capped at 32 KB, `TrimToBudget` now actually trims a tool-heavy turn, and the agent adopts the context window a server states when it rejects a request, then retries.
- gophermind-osx: answers rendered twice, the 30 second request timeout killed event streams, tool approvals were never delivered, the transcript was unreadable in one appearance, scrolling landed on text that was not drawn, the Settings panel could not be scrolled to its bottom, and the signed app could not launch because it linked a library from a path other Macs do not have. `libui.dylib` is now bundled and signed with the app.
- `gophermind brief` and the plan tree hardening found in review: node references are confined to ids so a model-written tree cannot write outside its directory, piped multi-line secrets are stored whole, the vault file is synced to disk before it replaces the old one, and the run-directory git exclude works inside git worktrees.

## [0.8.0] - 2026-09-18

### Added

- **gophermind-osx** — a native macOS desktop app built on libui-ng, a lighter-weight alternative to the Wails-based desktop app for people who just want a window: connection manager (local/remote/multi-tunnel, gocloak OAuth2, Keychain-backed credentials), a chat transcript with streaming, an approval tracker with a Y/N/timeout flow, a collapsible right panel, the model catalogue picker, full session-list CRUD, a pipeline panel with breakdown-seed and live task status, and a settings panel for backends/model preferences/skills/cache. Ships as a signed, dock-icon-bearing `.app` bundle with the server binary embedded in `Resources`; settings persist under `~/.gophermind` and survive upgrades and reinstalls. The menu bar gets a real status item too — a gopher-head glyph (not placeholder text), a File > Examples menu pointing at sample pipeline briefs, and the window launches maximized by default.
- **Run a session on another machine.** The desktop app can now point a session at a remote `gophermind serve` instead of its own embedded one, so the work happens on a server while the window stays local. Backends are configured in `~/.gophermind/backends.json`; the file does not exist by default and the app behaves exactly as before without one. Which machine a session runs on is shown in the status bar and, crucially, inside every approval prompt: a gated command approved in the window may execute on a server that hosts everything else, so it must never be ambiguous which box a prompt is about.
- **A session list across every backend**, so an existing conversation on a remote box can be resumed rather than always starting fresh. One unreachable backend costs only its own rows. Sessions can be renamed or deleted from the list, and now auto-name themselves from the first user message instead of staying "Untitled."
- **Skill sources.** `internal/skills` installs capability packs from a GitHub repository and injects only the ones switched on, managed from a panel in the desktop settings. Nothing fetched is enabled by arriving on disk: adding a source makes its content reviewable, and switching any of it on is a separate decision. Sources pin a commit rather than track a branch, because a repository can be rewritten after it was reviewed. A pack committed to a repo's own `.gophermind/skills` stays always on, which is the consent `CLAUDE.md` already carries.
- **`humanize` tool** — rewrites prose to remove AI writing tells, using the vendored [blader/humanizer](https://github.com/blader/humanizer) skill as its system prompt. It is a tool rather than an injected skill so its ~7k tokens of guidance cost nothing until it is called.
- **Vendored skill packs** for tdd, code review and bug diagnosis, from [mattpocock/skills](https://github.com/mattpocock/skills) (MIT).
- **Each session gets its own working directory** on the server (`/serve`), rather than sharing one process-wide root.
- **`/project` can seed its interview from a brief file** — `/project <name> <path-to-brief>` reads the file and asks questions that fill its gaps instead of starting cold. Sample briefs (a CLI tool, a REST API, a menu-bar app) ship for both the desktop app's Pick Brief... button and the CLI/TUI (`examples/phase-flow/briefs/`). `/project`'s own generated plan now asks for `depends_on`/`is_contract` up front, matching what the wave-based executor (0.7.0) already knows how to run.
- **`/project-execute` shows live per-task tool activity** as it runs — each task's tool calls/results stream to the transcript prefixed with the task id, instead of only appearing after the whole task finishes. It also carries the same `<phaseflow-context>` preamble (resolved config, roadmap progress) every interactive `/phase` step already gets, and a Ctrl-C mid-run now reports a tally of what finished before the cancel instead of a bare "cancelled." `/config` can change the speed-tier model live, without a restart.
- **A 429 from the model provider now falls through to the next candidate model** instead of failing the whole turn.
- Verbose debug mode showing every event with readable, unescaped formatting; a status bar that says what a turn is doing and for how long.

### Fixed

- **gophermind-osx would crash on launch outside of `open`** — `main()` never pinned the process to its start-up OS thread, so a blocking HTTP call during backend connect could resume the main goroutine on a different thread than Cocoa's, and the first AppKit call afterward (creating the menu-bar status item) crashed with "NSWindow should only be instantiated on the main thread!"
- gophermind-osx: local mode no longer lets the spawned server default its root to the current working directory; window edge margin, bundled-server-binary lookup, and panel captions corrected.
- **DELETE /session mapped every error to 404**, including a real disk failure deleting an existing session, not just "the session doesn't exist." Now distinguished via a `session.ErrNotFound` sentinel; a genuine failure reports 500.
- **HMAC signature verification ran on bodyless GET/DELETE session requests.** Signing an always-empty body isn't weaker protection, it's a broken one: the resulting signature is a fixed value for a given secret, replayable on any other GET/DELETE afterward. HMAC now only applies to POST/PATCH, where there's an actual payload to protect.
- iOS: pull-to-refresh silently did nothing when the session list was empty (`.refreshable` needs a scrollable container, and the empty state had none); a second approval push for the session already open pushed a duplicate nav layer instead of updating it.
- Conversation history no longer grows unbounded (trimmed to prevent stale errors from accumulating); the context window widened to 50 messages with an explicit turn-completion marker; a tool call now aborts after 3+ consecutive failures or once its result stops changing between retries, instead of looping.
- An empty skill catalogue serialized as `null` instead of `[]`, which broke the settings panel.
- A contract-flagged task in wave 0 could be dropped by `AssignWaves`; the run report now sees pre-revision attempts it previously lost; a contract flag stops the run rather than just failing the pass.
- Several model-attribution bugs: two paths misreported which model actually served a turn; the free-usage odometer metered the configured model rather than the one that ran; a corrupt model-settings file was swallowed instead of reported; the exclusion list wasn't applied to the model already in use.

### Security

- SSE error events are sanitized to prevent injection via error message content.
- The shell safety check now properly parses redirect targets (a redirect to `/dev/null` was previously misclassified as unsafe); `GIT_*` environment variables are stripped from both shell tools' environments so an inherited `GIT_DIR` can't redirect a git subprocess.
- A skill source URL whose path escaped the skill cache is rejected before install, both at the source-id derivation step and with a second check on the final destination.

## [0.7.1] - 2026-09-11

### Fixed

- **The desktop app declared itself version 1.0.0.** Wails renders `Info.plist` from `{{.Info.ProductVersion}}`, `wails.json` never set that key, and Wails substituted its own default — so every build this app has produced, including the one shipped as 0.7.0, reported 1.0.0 in Get Info, in the About box, and to anything else that reads a bundle version. The version is now written into the bundle at build time from the same argument that names the artifact and the cask, so the three cannot disagree, and it is written before codesigning because the signature covers `Info.plist`.
- **A freshly built app showed an old date in Finder.** `wails build -clean` rewrites the bundle's contents but leaves the `.app` directory's own mtime alone, and `ditto` preserves that through the zip, so the stale date reached every user rather than only the machine that built it.
- **The cask template still used the deprecated `depends_on macos: ">= :big_sur"`.** The fix had been applied to the published cask but not to the template it is generated from, so the next release would have regenerated the deprecated form and undone it.
- **`scripts/deploy.sh` hardcoded the development version to `0.5.0+dev`.** It was written when 0.5.0 was current and never updated, so every local and server deploy between then and 0.7.0 reported a version it was not. It now derives from the latest tag.

## [0.7.0] - 2026-09-11

### Added

- **Desktop application** — a Wails shell around the harness, with a chat screen, an approvals screen, a model picker and a settings panel. The frontend speaks only HTTP to an embedded instance of the same server the CLI runs; there is exactly one native binding, and all it does is tell the frontend where that server is. Ships as a signed, notarized and **stapled** universal app: `brew install --cask jbrahy/tap/gophermind-desktop`.
- **Model picker** — a dropdown of every available model with its remaining usage, a cycle-on-capacity switch, preference ordering, reachability and capacity filters, terms-based exclusions, and custom provider/model links. Every judgment call is a setting rather than a hardcoded rule.
- **Per-model metering.** The odometer introduced in 0.6.0 now records usage per model as well as per provider, and every model is measured against the rate limit *it* publishes. A model whose provider publishes no limit shows a bare count, never a borrowed one.
- **Contract-first task pipeline** — tasks gain `depends_on`, `candidate_models`, an attempt history and revision rounds. Waves are derived from the dependency graph and run concurrently (bounded at four); a task tries its candidate models in order, recording a specific reason for each failure; a task that exhausts every model has its definition revised rather than re-run, twice at most, then escalates to a human. A task that finds the contract wrong flags it and stops the run instead of improvising around it.
- **Live pipeline dashboard** at `GET /pipeline`, fed by SSE from the project's assignments file, with an end-of-run report attributing wins and losses to every model actually tried.

### Fixed

- **`run_shell` no longer passes `GIT_*` variables to the commands it runs.** 0.6.0 fixed this for git commands gophermind builds itself, but `run_shell` is the path an agent actually runs `git init` and `git commit` through, and it handed the whole parent environment to bash. Both shell tools are fixed, and the model-supplied `env_allow_list` is filtered too, so naming `GIT_DIR` there cannot reinstate it.
- **Each model is metered against its own published quota.** Every model on a provider was measured against the *default* model's rate limit, so `gemini-2.5-pro` (really 5 RPM, 50 RPD) displayed as 15 RPM / 1,500 RPD, and models publishing no limit at all were given one.
- **Candidate models stay on the endpoint that can serve them.** A project run against a private endpoint was handed two dozen hosted-provider routing ids and never tried the model the plan asked for.
- **Every turn is metered, not only the CLI's one-shot path.** Turns served over HTTP, through a session, or in the desktop app spent free-tier allowance that no counter saw, so the model picker's usage figures stayed at full forever and cycle-on-capacity could never fire.
- **Typing no longer decides a pending approval.** The Y/N approval shortcut was bound to the window with no check on the focused element, so a keystroke typed into a settings field could approve a gated shell command — and `preventDefault` hid it.
- **`POST /run` and `/run/stream` no longer auto-approve gated tools** in the desktop app, which made the approvals screen bypassable by using a different route with the same token.
- **Concurrent turns and concurrent tasks no longer share one mutable LLM client**, which let a turn run on a model another turn had selected while the recorded history named the model it asked for.
- **A contract flag stops the run**, rather than only the current pass, and is rendered as itself rather than as a success.
- **A corrupt or unreadable settings file is reported** instead of silently yielding defaults, which re-enabled providers excluded for legal reasons; a failed odometer read no longer overwrites the lifetime reading with zeros.
- **`Ctrl-C` reaches the server's graceful shutdown path**, which was unreachable because the serve command passed a context that never cancels.


## [0.6.0] - 2026-09-10

### Added

- **Free LLM providers: run gophermind with no API key and no local server.** A vendored, CC0 registry of 16 free providers (from [mnfst/awesome-free-llm-apis](https://github.com/mnfst/awesome-free-llm-apis)) is embedded in the binary, so it works offline and is byte-reproducible. Each entry surfaces as a `free-*` profile resolved through the existing `--profile` machinery, which means per-profile env overrides, the setup wizard and validation all work unchanged. **Two providers need no API key at all and were verified completing real turns: `free-ovhcloud` (2 RPM per IP, EU-hosted) and `free-kilocode` (200 requests/hour).** Start with `gophermind free list`, then `gophermind --profile free-ovhcloud ask "hello"`. API keys are still read only from `GOPHERMIND_PROFILE_<NAME>_API_KEY` and are never baked into the registry.

- **`gophermind free`** — `list` shows every provider with its default model, free-tier terms and link, no-key ones first; `show <profile>` prints the full card plus the exact `export` lines to use it; `check <profile>` probes the endpoint's `/models` so compatibility is proven rather than assumed; `usage` prints the odometer. It runs before endpoint validation, so it works with nothing configured — which is exactly the state of the user who needs it.

- **A free-usage odometer and per-quota trip meters.** A lifetime, monotonic count of tokens and requests served by free providers, kept in its own state file so it does not depend on `GOPHERMIND_USAGE_LOG` (which is off by default). Alongside it, rolling trip meters show consumption inside each provider's own published window. Free tiers are not token-denominated — Groq's limit is 1,000 requests/day, Cohere's is 1,000 calls/month, Cloudflare's is neurons — so both requests and tokens are tracked, and **a denominator is only ever shown when upstream published one**: a provider whose limit could not be parsed shows a bare count rather than an invented fraction.

- **Provider attribution.** The model in use is named with its provider and a link, in the startup banner, in the TUI status line (with an optional OSC 8 hyperlink), and in full via a new `/provider` command. Everything is conditional on the active profile being a free one, so a paid or unset profile renders exactly as before.

- **`GOPHERMIND_CHAT_PATH` and `GOPHERMIND_MODELS_PATH`** — explicit path overrides for an OpenAI-compatible endpoint whose base URL already includes the API version segment. Empty (the default) means the historical `/v1/chat/completions` and `/v1/models`, so existing configurations are byte-identical.

- **RAG + memory injection now applies to `serve`.** `GOPHERMIND_RAG` / `GOPHERMIND_MEMORY` previously only took effect in the one-shot `run`/`ask` commands — the served agent (`POST /run`, `/run/stream`, and session turns) silently ignored them, so a phone-driven or webhook-driven turn was never grounded. All three serve paths now inject the same blocks from the same stores. Injection is **per turn**, keyed to that turn's text rather than the session's first message, so later turns on a new topic are grounded too; for persisted sessions the system prompt is restored before the session is saved, so a session never accumulates a copy of each turn's retrieved context. Still opt-in and inert when embeddings are unconfigured.

### Fixed

- **No provider whose base URL ended in `/v1` could complete a turn.** `llm.Client` unconditionally appended `/v1/chat/completions` to `BaseURL`, producing `/v1/v1/chat/completions` and a 404. Every hosted provider publishes its OpenAI-compatible root in that form, and the built-in `openai` profile had the same bug. The same fault in capability probing (`/v1/models`) failed silently rather than loudly, so affected endpoints fell back to a default 8K context when the endpoint actually offered 131K. Fixed via the additive `ChatPath`/`ModelsPath` above; `local-llama` and every existing caller are unchanged.

- **An inherited `GIT_DIR` could redirect gophermind's git subprocesses at the wrong repository.** Code that ran `git` set the working directory but not the environment, and git ignores the working directory when `GIT_DIR` is set. Any gophermind command invoked from inside a git hook — or from any process exporting `GIT_DIR` — therefore read or wrote a different repository than the user intended. The worst case was `gophermind doctor fix`, which runs `git init`: with `GIT_DIR` set and no `GIT_WORK_TREE`, that reinitializes the pointed-at repository as **bare**, after which every ordinary git command in it fails. All eight call sites now build their commands through a new `internal/gitenv` that strips `GIT_*`.

- **`embed_index` no longer fails on prose-heavy repos, and can index large ones.** Chunks were split by line count only (50 lines) with no size bound, so a single dense Markdown chunk could exceed the embedding model's context and make the server reject the *entire* request — one oversized file failed the whole index build. Chunks are now also capped by length (splitting on line boundaries, and rune-safely inside a single over-long line, so minified files are handled). Separately, every chunk used to go out in one HTTP request against a 60s client timeout, which put a hard ceiling on repo size; requests are now sent in bounded batches. Both the full (`BuildIndex`) and incremental (`UpdateIndex`) paths are covered.

- **`GOPHERMIND_LLM_TIMEOUT`** — per-attempt timeout for LLM completion requests, kept separate from `GOPHERMIND_HTTP_TIMEOUT_S` (which now governs only the one-shot startup calls: model discovery/listing and the capability probe). Accepts a bare number of seconds (`900`) or a Go duration (`15m`); unset means it inherits `GOPHERMIND_HTTP_TIMEOUT_S`, so existing configs are unchanged. Streaming turns remain governed by `GOPHERMIND_STREAM_IDLE_TIMEOUT_S`.
- **`/project-execute` autonomous executor** — autonomously runs every `pending` task in an approved project plan (`.planning/assignments.json`), each in a fresh isolated agent with its assigned model and catalog prompt. Tasks are verified against acceptance criteria (verify-and-correct, one round); failed tasks are marked `failed` and execution continues to a summary. Task agents run in auto-approval mode (unattended). Ctrl-C aborts with graceful cleanup (in-flight tasks revert to `pending`). Requires an approved plan, gated like `/phase execute`.

## [0.5.0] - 2026-07-16

### Added

- **`/project` guided new-project flow (TUI)** — `/project <name>` opens a dialog that interviews you (iterating with the LLM) to build a comprehensive spec, then generates a validated plan: `SPEC.md`, a `ROADMAP.md`, and a machine-readable `assignments.json` that assigns **each task to an agent type** (a `prompt.md` from a per-project catalog, seeded from the embedded PhaseFlow agents) **and a model** (per-type default, overridable). You approve the plan (with a revise loop) before it's marked ready.
- Approval **gate**: in the TUI, `/phase plan|execute|verify|milestone` are blocked until the project plan is approved (CLI `gophermind phase …` is unaffected).
- `internal/phaseflow` plan backbone: assignments schema, agent catalog loader + seeding, `ValidatePlan`, and an approval marker. (Orchestrated verify-and-correct execution over these assignments is a planned follow-on.)
- **Predictive text & autocomplete** — hybrid ghost-text (inline suggestion, accept with Tab or →) and popup menu (for slash commands, file paths, and multiple history matches; navigate with ↑/↓, dismiss with Esc). Built on the reusable `bubblecomplete` library with four providers: slash-command, file/path, whole-prompt history recall, and Markov (n-gram) next-word prediction.
- **Prompt history** — submitted prompts saved to `<config dir>/gophermind/history` (plaintext JSONL, capped at 500 entries). Disable with `GOPHERMIND_HISTORY=off`.
- **Multi-line input** — input box grows 1–4 rows, then scrolls. **Enter** submits; **Shift+Enter** newline (fallbacks **Alt+Enter**, **Ctrl+J** for terminals that don't distinguish Shift+Enter).
- **`/goal` command** — set a session-scoped persistent steering goal (`/goal <text>` to set, bare `/goal` to show, `/goal clear` to remove) injected into every subsequent turn.
- **Native terminal text selection** — mouse capture dropped; now you can select and copy text with click-drag. Keyboard transcript navigation (**PgUp**/**PgDn**) unchanged.

### Fixed

- **Streaming turns no longer time out mid-response.** The LLM client previously shared one `http.Client` whose overall `Timeout` (default 300s, `GOPHERMIND_HTTP_TIMEOUT_S`) bounded the *entire* request including reading the streamed body, so long or heavy turns were killed with `read stream: … context deadline exceeded`. Streaming now runs without a total-request cap, guarded instead by a connect/response-header timeout plus an **idle/stall watchdog** that aborts only when tokens actually stop arriving (default 300s, configurable via `GOPHERMIND_STREAM_IDLE_TIMEOUT_S`). Non-streaming `Complete` and the startup model-probe calls remain bounded by a per-request deadline.

## [0.4.0] - 2026-07-13

### Added

- **Animated gopher intro** — a short (~1.5s), dependency-free truecolor gopher plays before the interactive chat TUI (fade-in → eyes-open reflection sweep → settle). It self-gates to interactive truecolor terminals ≥80×30, honors `--quiet`/`--no-banner`/`NO_COLOR`, is skippable by any keypress, and can be disabled with `GOPHERMIND_INTRO=off`.
- **Interactive `/config` wizard** — configure the endpoint, API key, model, approval mode, max iterations, and optional integrations from inside the chat TUI. It runs via `tea.Exec` (Bubble Tea cleanly hands over the terminal), persists to the config file, and applies changes to the running session live (switching approval mode to `ask` applies on next launch).
- New agent configuration API (`Config`, `SetBaseURL`/`SetModel`/`SetAPIKey`/`SetMaxIter`/`SetApprovalMode`) backing the wizard.

### Changed

- Refreshed the README header with a new full-body gopher.

## [0.3.0] - 2026-07-12

### Added

**PhaseFlow — native spec-driven workflow** (`internal/phaseflow`)
- Integrated [PhaseFlow](https://github.com/jbrahy/metaphaseflow) as a first-class, native subsystem: the **Roadmap → Phases → Plan → Execute → Verify → Milestone** loop, with workflow state persisted under `.planning/` (`ROADMAP.md`, `STATE.md`, `PROJECT.md`, `config.json`) — interchangeable with upstream PhaseFlow.
- `gophermind phase <cmd>` CLI and `/phase <cmd>` TUI slash command. State commands (`init`, `status`, `next`, `commands`) run locally; loop steps (`roadmap`, `plan`, `execute`, `verify`, `milestone`) build a state-seeded prompt from the embedded upstream command and run it through gophermind's agent.
- Go roadmap parser with decimal-phase (inserted-phase) ordering, progress computation, and in-place checkbox mutation that preserves human edits.
- **Deterministic bookkeeping ported to Go** (no model calls, cannot drift from the checkboxes): `phase done <plan-id>` marks a plan complete, auto-ticks a finished phase, and recomputes STATE.md's position/progress; `phase sync` refreshes STATE.md from the roadmap; `phase archive <version>` snapshots a shipped milestone and appends a stat-bearing entry to `.planning/MILESTONES.md`.
- The full embedded PhaseFlow command surface (not just the five loop steps) is runnable by name, e.g. `phase map-codebase`, `phase code-review`, `phase ship`.
- Vendored and embedded PhaseFlow's phase commands, subagent definitions, and templates (MIT, © 2025 Lex Christopherson — see `internal/phaseflow/assets/LICENSE.upstream` and `CREDITS.md`).

## [0.2.0] - 2026-07-11

A large feature release (170 commits since 0.1.0) spanning retrieval, data
tooling, multi-agent reasoning, observability, security hardening, and
distribution — plus several security fixes. All new integration/service tools
are configuration-gated (inert until you provide a token/endpoint).

### Added

**Retrieval, embeddings & knowledge** (pure-Go, no CGO)
- Local semantic index: `embed_index` + `semantic_search` over an OpenAI-compatible embeddings provider (`GOPHERMIND_EMBED_MODEL`), with **RAG context injection** (`GOPHERMIND_RAG`), **incremental** git-diff re-indexing, and `retrieval_eval` (hit@k).
- `hybrid_search` — BM25 (SQLite FTS5) + vector fused via reciprocal rank fusion.
- **Knowledge packs** (`import_pack` + `semantic_search pack=…`), long-term **vector memory** (`remember_fact`), global **profile memory** (`remember_profile`), and **episodic memory** (`record_episode`), injected at task start under `GOPHERMIND_MEMORY`.
- `docs_lookup` — fetch + per-`library@version` cache of library docs; answer-with-citations when web search is used.

**Data, databases & analytics**
- `db_schema`, `db_explain` (full-scan warnings), `data_transform` (filter/group/aggregate over CSV/JSONL), `log_metrics` (time-bucketed), `chart` (sparkline/bar), `detect_anomalies` (robust z-score), `seed_data`, and an opt-in `sql_query` result cache.
- `db_query` — read-only Postgres/MySQL behind a DSN allowlist; `migration_dryrun` (schema diff on a throwaway copy); `read_parquet` for columnar files.

**Agent reasoning & multi-agent**
- Turn strategies: `--debate` (two candidates synthesized), `--samples N` (self-consistency), `--reflexion` (retry with a structured lesson); a task-graph planner/executor, a heuristic tool-use **critic**, cost-aware model routing, subtask budget allocation, and **resumable** step execution.

**Prompt engineering & evaluation**
- Named prompt registry (`prompts` subcommand), per-section token accounting (`prompt-tokens`), a built-in `--schema @diff|@review|@plan` library, a few-shot example bank, extractive context compression, golden-transcript tests, and the `ab` harness gained an LLM-judge scorer, a multi-model **scoreboard**, and a `--min-score` CI gate.

**Sessions**
- `sessions` gained `merge`, `search`, tags + `--tag/--since/--until` filters, `replay`, `export --redact`, per-tag GC (`--keep-tag`), a remote store (`push`/`pull`), and auto-checkpoint before gated mutations.

**Serve, observability & operations**
- Webhook `serve` gained `/healthz`, `/readyz`, `/metrics` (Prometheus), an SSE `/run/stream`, per-caller rate limiting, and HMAC payload verification.
- Dependency-free span **tracing**, JSON + slow-request HTTP logging, a `usage report` cost dashboard, budget alerts, a `--report` HTML run artifact, and cost-anomaly detection.

**Security, sandboxing & governance**
- RBAC per role (`GOPHERMIND_ROLE`), a secrets-file vault (`@name` refs), HMAC-signed + shippable audit logs, prompt-injection defense, a data-egress classifier, policy-as-code tests (`policy test`), container/network-namespace `run_shell` isolation, and approval-timeout auto-deny.

**Distribution, ecosystem & platform**
- **MCP server** (`gophermind mcp`) exposing the tools over the Model Context Protocol; an out-of-process **plugin SDK** + marketplace (`plugins install`); a **WASM sandbox** (`run_wasm`, wazero); shareable config **bundles**; signed self-`upgrade`; opt-in local **telemetry**; and a **benchmark** suite.
- Packaging matrix: **deb/rpm/apk** (nfpm), Scoop, and winget configs; **SBOMs** (syft) and reproducible (`-trimpath`) builds.

**Developer experience**
- LSP-backed `find_definition`, colorized diffs + markdown rendering, a fuzzy `commands` palette, thin **VS Code / Neovim** clients, `doctor fix`, shell completions, `--dry-run`, a `persona new` scaffolder, `--every` (scheduler) and `--watch` (event-driven) triggers.

**Earlier in this cycle**
- `gophermind --print` non-interactive **stream-json** protocol (Claude-Code-compatible), session persistence (`--session-id`/`--resume`), print-mode `--append-system-prompt`/`--permission-mode`, an OpenCoven runtime manifest, npm distribution, a `--version` flag, gated file-mutation tools, `--read-only`, `--plan`/`--parallel`/`--tool-budget`, and auto-load of `CLAUDE.md`/`AGENTS.md`.

### Changed
- Requires **Go 1.25**; builds for **macOS, Linux, and Windows** (amd64/arm64) — releases now include Linux `.deb`/`.rpm`/`.apk` packages and SBOMs.

### Fixed
- **Security:** `serve` sibling-path auth/rate-limit parity (`/run/stream` now enforces HMAC + the shared limiter) and SSE frame injection; `migration_dryrun` ATTACH/`VACUUM INTO` sandbox escape; RBAC fail-open on an unknown role; the VS Code extension no longer honors a workspace-set binary path (ACE); `db_query` data-modifying-CTE bypass + substring DSN-allowlist escape; and path traversal in the LSP `find_definition` tool.
- `apply_patch` is now genuinely atomic — it computes all edits before writing and rolls back on failure.

## [0.1.0] - 2026-07-09
### Added
- First-run setup wizard — endpoint, API key, model picker, approval mode, and max iterations — saved to a global config, plus a `gophermind config` command to re-run it.
- Signed + notarized macOS release pipeline (GoReleaser) distributed via a Homebrew cask, and a `gophermind version` command.
- A random fortune, the version, and recent changes shown under the gopher banner on startup.
- A redrawn gopher ASCII banner.

### Changed
- No endpoint is baked into the binary anymore — configure it via the wizard, a provider profile, or `GOPHERMIND_BASE_URL`.

### Fixed
- The terminal OSC-11 background-color query no longer leaks escape codes into the input box.
