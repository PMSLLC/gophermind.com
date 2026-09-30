# GopherMind v2: brief to build (foundations and planner)

v2 turns a written brief (a markdown file with YAML front matter) into a tree of
task nodes that agents build, wave by wave, against a shared contract. This
directory documents the foundations (brief loading, the secret vault, the node
tree with waves, contract slicing, the run directory) and the planner, which
takes a brief as far as an approved plan with its tests written. Nothing is
built or executed yet; that is the executor plan.

## Commands

```text
gophermind brief validate <brief.md>
gophermind brief plan <brief.md> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
gophermind brief resume <run-id> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
gophermind brief status <run-id>
gophermind brief coverage <run-id>
gophermind brief calls <run-id>
gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
gophermind brief vault list
gophermind brief tree check <run-dir>
```

Anything else under `gophermind brief` prints this usage and exits 1. The run
and report subcommands arrive with the executor plan (deviation D1).

Exit codes: 0 ok, 1 error (usage, unreadable file, failed check, uncovered
requirements), 2 invalid brief (the message names the offending field), 3
waiting on a human (`plan` and `resume` with the file gate). Code 4 is reserved
for an escalated leaf in the executor plan.

`plan` takes a brief through Load, Clarify, Contract, Decompose, Coverage,
Approve and Test-writer (see "Planning a brief"). `resume` continues a run from
its first unfinished stage; the run id is the brief's `id`. `--yes` records the
approval as given by the flag without showing the plan. `--gate file` writes
`QUESTIONS.md` and `APPROVAL.md` into the run folder and exits 3 until they are
filled in; the default comes from `human.mode` in the settings. `--fake` answers
every model call from a directory of canned replies and needs no settings file,
no network and no model (several directories may be given, comma separated; the
first one holding a reply wins). `--allow-public` lets public providers see the
whole brief, and the run's status says so afterwards.

`status` prints each stage, what the run is waiting for, how many requirements
are covered, and the model calls summed by task type and node class. `coverage`
prints what covers each requirement of the brief and the warnings. `calls`
prints one line per model call.

`validate` prints one `warning:` line to stderr for each undeclared token that
looks like a secret name. Warnings never change the exit code.

`tree check` loads the run directory, then requires a well-formed tree: exactly
one root, every component's parent is that root, and every function's parent is
a component in the tree. An empty or wrong directory fails with exit 1. Two
nodes that would be written to the same file are rejected when the tree is
built. Readiness of a component or the root looks at its children only and
ignores its own `depends_on` (D2).

## Vault

Secrets are stored age-encrypted, in scopes: `harness` (used by
`brief vault set` and `list`) and `run/<brief-id>` (a brief's declared secrets).

- `GOPHERMIND_VAULT_PATH` overrides the vault file. The default is
  `<config dir>/vault.age`.
- `GOPHERMIND_VAULT_PASSPHRASE` supplies the passphrase without a prompt. It is
  required when stdin is a pipe, because the value for `vault set` is then read
  from that pipe and the passphrase must not compete for it.
- A piped value is stored whole: all of stdin is the value, including embedded
  newlines (so `cat key.pem | gophermind brief vault set KEY` keeps the full
  PEM), minus exactly one trailing newline. Empty input is an error.
- The vault file is fsynced before the rename and its directory is synced after.

## Run directory

`.gophermind/<brief-id>/` is created with mode 0700 (`brief.md` is 0600) and the
entry `.gophermind/` is added to the repository's `info/exclude`. In a git
worktree, where `.git` is a file, the exclude entry goes into the main
repository's `info/exclude` (found through `gitdir:` and `commondir`). A `.git`
that cannot be resolved is an error and nothing is created; a directory with no
`.git` is left alone.

## Planning a brief

`gophermind brief plan` runs these stages. Each one is skipped on `resume` when
its output is already in the run folder.

| Stage | What it does | Output |
|---|---|---|
| Load | Validates the brief, makes sure its secrets are in the vault, creates the run folder | `brief.md`, `requirements.json` |
| Clarify | Asks the model what it needs to know, then asks you (or takes the defaults when the brief says `assume_and_document`) | `answers.json` |
| Contract | The outline in passes (repeated while the model says more remains), then one call per component, also repeated while more remains | `contracts.json` |
| Decompose | One node per function, at most 8 functions per call | the root and component nodes, drafts in `_state/` |
| Coverage | Maps every requirement of the brief to the nodes and tests that satisfy it | `coverage.json`, acceptance tests on the root node |
| Approve | Shows the plan and waits for a decision | `approval.json` |
| Test-writer | Writes the tests of every function from its contract alone | test files in the target repository, the finished tree, blackboard rows |

Size. Nothing caps the number of components, functions or tests. A large brief
makes more calls, never a coarser plan.

Truncation and outline passes. A reply the model cut off at the token limit
(finish reason `length`) is a truncation even when some text arrived: the
provider returns a truncation error, the router records that attempt as outcome
`error` with error_kind `truncated` (not `malformed`), and retries the same model once with double the
budget. The Contract outline asks for 16000 tokens and may grow to 32768; every
other stage keeps its own budget and the router's 16384 cap. A large brief does
not need a bigger reply: the outline is written in passes of about 12 components
and 12 types, a pass sets `"more": true` until the list is complete, and each
later call is told the ids already written. Passes are merged by id (an
identical repeat is dropped, a different one is an error naming the id), the
whole merged outline is validated after every pass, and each pass is stored in
`_state/contract.json`, so a resumed run asks only for the unfinished pass. At
most 20 passes are made before the stage stops with an error. Between passes
only local checks run (a type may use one a later pass writes); when the
outline is complete, ids still used but never declared are asked for in up to 2
repair passes (stored like any pass), and only then does the stage fail, naming
at most 10 of the ids. An id that a later emission repeats with different content (in the same reply
or a later pass, for components, types and functions) is model noise, not a
plan defect: the first emission is kept, the repeat is dropped, a warning
`outline_duplicate_ignored` names the ids (only ids that pass the id syntax,
with a count) and they are listed in `_state/contract.json` under
`ignored_duplicates`; coverage later checks the plan against the brief, so
nothing the brief needs is lost. Identical repeats are dropped silently. A
model's `exports` in the outline are ignored (the harness fills them). The
component ids `logs` and `outline` are reserved (a run folder and a stage
name); the repair stage is `contract:_repair`, which no component can be named
because ids cannot start with an underscore, so a component called Repair is
fine. Failed repair attempts count toward the bound across restarts.
Component passes work the same way: a function may use
one a later component writes, and ids still undeclared after every component is
written go through up to 2 `contract:_repair` passes.

Requirements and coverage. Every top-level bullet under `## Constraints` (C1,
C2, ...) and `## Acceptance` (A1, A2, ...) and every `###` heading under
`## Features` (F1, F2, ...) is one requirement, parsed by code. A constraint
needs a covering node or a root test, a feature needs a covering node, and an
acceptance bullet needs a root test with a command. Gaps go back to the model
for up to `defaults.max_coverage_rounds` rounds; if any remain the run stops
with exit 1 and lists them, and nothing is approved. Write briefs with one
obligation per bullet: a bullet that bundles three counts as one requirement.

Leaf checks. A function node is refused unless its signature parses as Go, it
describes every parameter and every result, it lists at least one error
condition when the function returns `error`, and it carries a node class
(`pure`, `validation`, `handler`, `client`, `storage`, `concurrency`, `wiring`,
`other`). A test file is refused unless it parses, holds the expected test
function, imports only the standard library and the module's own packages, and
comes with at least one test per error condition plus one for the happy path.

What a model may not decide. Dependency signatures, waves, the path of a test
file and the command of every function test (`go test ./<dir> -run ^<Test>$`)
are derived by code. The only model-written commands in a plan are the root
acceptance tests, and the approval summary prints each one in full.

No source or test files are written to the target repository before
`approval.json` exists and matches the plan as it stands (the run folder
`.gophermind/<brief-id>/` inside it is written from the first stage). Test
files are left uncommitted.

Clearing a run. A brief id names one run, and the run's state lives in two
places: the run folder inside the repository, and, under the config dir
(`~/.gophermind`, or `GOPHERMIND_CONFIG_DIR`), the run record
`runs/<run-id>.json`, the rows of the run id in `blackboard.db` (blackboard,
events, ledger) and the run's vault scope. To start a brief again from nothing:

1. In the target repository: `git reset --hard <baseline>` and
   `git clean -fdx -e .remember`. This removes the run folder and any test
   files the test-writer wrote. If the clean is skipped, an untracked test file
   left by the earlier run makes the test-writer stop with an error rather
   than overwrite a file it did not write; delete it.
2. Nothing else is required: `gophermind brief plan` on the same brief clears
   the run id's rows in `blackboard.db` when it creates the new run folder and
   replaces the old run record, so no stale ledger row, wave or claim carries
   over. (`rm ~/.gophermind/runs/<run-id>.json` is harmless.)

A run folder that holds only what Load writes (the brief, `requirements.json`,
an empty `logs/`, `_state/status.json`) is replaced by `plan` without complaint.
A run folder with stage output is not: `plan` refuses and points at `resume`.

Run folder files added by the planner: `requirements.json`, `answers.json`,
`contracts.json`, `coverage.json`, `approval.json`, and working files under
`_state/` (`tree check` skips all of these). `<config dir>/runs/<run-id>.json`
records where a run's folder is, so `resume`, `status`, `coverage` and `calls`
need only the id.

## Settings

`<config dir>/gophermind.yaml` is written with defaults the first time `plan`
runs without `--fake`: the Mac mini as the one private provider, and two
providers that need no key. `models` lists, per tier (`strong`, `standard`,
`any`), the `provider/model` entries to try in order. `privacy.mode` is
`need_to_know` (public providers see single functions only) or `private_only`.
A provider's key is never in this file: `api_key_secret` names an entry set with
`gophermind brief vault set`.

## The call ledger

Every model call, including every failed attempt, is one row in the `calls`
table of `<config dir>/blackboard.db`: stage, task type (`clarify`, `contract`,
`decompose`, `coverage`, `testwrite`), node class for a call about one function,
provider, model requested and served, token counts, duration, and outcome. The
prompt and the reply are never stored, only their sizes and SHA-256 hashes.
`gophermind brief calls <run-id>` prints the rows; `status` prints them summed by
task type and node class, which is how models are compared by kind of work.

## Environment block

The frontmatter `env` section declares non-secret variables that a command may
read. Each declares a name, purpose, and optional default value. An env
name must not equal a secret name; overlap is rejected with exit 2 and the
message `ENV_SECRET_OVERLAP: <name>`.

`brief validate` also rejects, with exit 2, a secret name declared twice
(`secret <NAME> is declared twice`), an env name declared twice (`env <NAME> is
declared twice`), and any secret or env name reserved for the harness
(`<NAME> is reserved for the harness`). The ten reserved names are
HTTP_PROXY, HTTPS_PROXY, NO_PROXY, GOPHERMIND_NODE (proxy and node variables
the harness emits) and PATH, HOME, GOCACHE, GOMODCACHE, GOPATH, TMPDIR
(toolchain variables, see E5). The set lives in `brief.IsReservedName`, which
`execenv.Build` also uses.

## Secret-name scan rule

The `brief validate` command scans the brief body for undeclared tokens that
look like secret names. Only UPPER_SNAKE_CASE tokens (three or more characters)
are considered. A token is flagged when it (a) ends in `_KEY`,
`_SECRET`, `_TOKEN`, `_URL`, `_DSN`, `_PASSWORD`, or `_PASSPHRASE`, or (b)
sits on a line containing "secret", "credential" or "environment variable"
(case-insensitive). Undeclared tokens matching the secret suffix list or
appearing on a line with secret wording are printed as warnings to stderr; they
never change the exit code. Declared secret and env names are always exempt.

## Command environment

The `execenv.Build` function constructs the exact environment handed to an
exec.Cmd for running a node's commands. It includes the declared env variables
(applying defaults, then config overrides from the harness), the declared
secrets (fetched from the vault), HTTP_PROXY and HTTPS_PROXY (when a proxy URL
is given), and GOPHERMIND_NODE (the node id). Nothing from the harness process
environment is included. A secret name never has a default or an override. The
ten reserved names listed above are rejected if a brief declares them as env or
secret names, and `Build` itself also checks that every name matches
`^[A-Z][A-Z0-9_]*$`.

The optional `Inputs.Toolchain` map holds PATH, HOME, GOCACHE, GOMODCACHE,
GOPATH and TMPDIR. It is harness-owned, filled from harness config, and `Build`
never reads the process environment for it. Any other key or an empty value is
an error. When it is nil or empty the environment is exactly the strict list
above. It exists because that strict list cannot run `go test` (no build cache
without GOCACHE and HOME), so it deliberately widens the handoff's literal
list (E5). The result is sorted NAME=value strings suitable for exec.Cmd.Env.

## Decisions

E1 comes from the handoff; E2 to E5 are this project's decisions:

E1: The secret-name scan rule has no stoplist. A token is flagged based on
suffix or line wording, not a whitelist.

E2: The harness config format is still undecided, so overrides are passed to
the builder as a map; wiring them to config is a later task.

E3: The test fixture uses the updated brief version that includes the env
block, yielding 2 scan warnings (ceiling 3) on the venture-studio brief.

E4: The proxy URL is optional; empty means HTTP_PROXY and HTTPS_PROXY are
omitted. GOPHERMIND_NODE is always set. Once the network proxy exists the
executor must always set it.

E5: The handoff says nothing else may reach commands, but the exact environment
cannot run `go test`. `Inputs.Toolchain` is an optional harness-owned map of
six variables; nil means strict.

## Package map

```text
gophermind-lib/briefv2/
  schema/     embedded JSON schemas, Validate(kind, json)
  testdata/   example brief, contracts.json and a partial node tree
  brief/      front matter, sections, secret-name scan
  vault/      age-encrypted secret store, prompts
  tree/       nodes, cycle check, waves, readiness, file store
  contract/   load, Slice, Diff, Affected
  rundir/     .gophermind/<id>/ layout, kept out of git via .git/info/exclude
  db/         the shared SQLite file and its migrations
  blackboard/ runtime state of every node (rows, claims, attempts)
  ledger/     one row per model call
  events/     progress events and sinks
  provider/   the provider interface, the OpenAI-compatible client, a scripted fake
  settings/   gophermind.yaml
  router/     fallback chains, cooldowns, the privacy rule
  human/      the human gate: terminal, file, programmatic
  planner/    requirements, the stages, coverage, prompts, the offline fixture provider
cmd/gophermind/brief.go        the `gophermind brief ...` command group
cmd/gophermind/brief_plan.go   plan, resume, status, coverage, calls
docs/briefv2/handoff/     the original handoff, for reference (copied from the zip; the only edits are that
                           interfaces/blackboard.go was gofmt'd and interfaces/ has a go.mod so the repo's
                           Go tooling skips it: it holds two packages, blackboard and provider, meant to be
                           copied into real packages later)
```

## Example briefs

Five complete v2 briefs ship with the desktop app in `gophermind-osx/examples/briefs/`
(`04-csvstat` to `08-taskboard`), from a one-file CLI up to a seven-feature system.
Together they use every option the frontmatter has. That README has the table; a test
(`gophermind-lib/briefv2/brief/examples_test.go`) keeps every one valid with no scan
warnings and fails if the set stops covering an option. The AI Venture Studio brief is a
test fixture only and is not shipped.

## Deviations from the handoff

The handoff's stated tests cannot all pass against its own example data. These
resolutions reproduce every value in the examples that can be checked.

| # | Handoff says | Problem | Resolution in this plan |
|---|---|---|---|
| D1 | CLI is `gophermind run`, `resume`, `status`, `report`, `validate`, `vault`, `tree` | `run`, `resume`, `status`, `report` already exist as commands | Namespace everything as `gophermind brief <sub>` (`brief validate`, `brief vault set`, `brief tree check`, later `brief run`, ...) |
| D2 | Wave rule: components take the max wave of their children; function with empty `depends_on` has no defined wave | The example has `registration` at wave 1 with a child at wave 2, and `fn-validation-error-error` (no deps) at wave 0 | `wave = 0` if `depends_on` is empty, else `1 + max(wave of depends_on)`, for every kind. Children do not affect a node's wave. Readiness (children verified) is separate |
| D3 | Slicing `fn-register-handler` yields "the seven entries shown in its node file" | The stated algorithm over the shipped `contracts.json` yields 10 entries (4 types, 6 functions). The node file's 7 entries are hand-written, include a `CRM` interface and a `Server` struct that `contracts.json` does not define, and omit 5 signatures | The golden test asserts the algorithm's 10 entries. The shipped `contracts.json` is used unchanged. Open item for John: the contract is missing `Server` and `CRM` types that `fn-server-new` relies on |
| D4 | "Every file under examples/tree validates", waves reproduced | The example tree is a partial excerpt: only 7 node files, while `depends_on` and `children` reference ~10 more | Per-file schema validation runs on all 7. Tree-level tests use the 6-node subset whose dependencies exist, plus synthetic trees. `Load` does not require `children` to resolve |
| D5 | Secret scan regex `\b[A-Z][A-Z0-9_]{2,}\b` minus a stoplist | It flags 5 error codes in the example brief (`EMAIL_INVALID` etc.) | The handoff update resolved the scan noise. Venture-studio fixture: 2 warnings, ceiling 3. Warnings only, never blocking |
| D6 | Config in `gophermind.yaml`, then TOML | `GOPHERMIND.toml` is not parsed by any Go code and there is no TOML library | Items 1 to 4 need no harness config. Vault path defaults to `<config.Dir()>/vault.age`. Config format is decided in the provider/router plan |
| D7 | Blackboard: reuse the recursive agent system's | Not in this repo; John did not say where it lives | Assumption: build the SQLite backend in the blackboard plan (item 6). Not needed here |
| D8 | Live view (item 15) is a wave board plus attempt log | John wants a Gantt chart | Item 15 becomes a Gantt view: one row per leaf grouped by component, bars from each attempt's `started_at` and `duration_ms`, one bar per model in the fallback chain, dependency arrows from `depends_on`, rate-limit waits as gaps, revision rounds as a new block. It reads the blackboard only and works live and after the fact (no forecast before a run, since durations are unknown). No change to items 1 to 4 or to the item 6 blackboard interface. |
