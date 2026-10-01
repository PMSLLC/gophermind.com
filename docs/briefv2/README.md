# GopherMind v2: brief to build (foundations, planner and executor)

v2 turns a written brief (a markdown file with YAML front matter) into a tree of
task nodes that agents build, wave by wave, against a shared contract. This
directory documents the foundations (brief loading, the secret vault, the node
tree with waves, contract slicing, the run directory), the planner, which takes
a brief as far as an approved plan with its tests written, and the executor,
which builds that plan, proves it and commits it (`gophermind brief run`).

## Commands

```text
gophermind brief validate <brief.md>
gophermind brief plan <brief.md> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
gophermind brief resume <run-id> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
gophermind brief run <run-id> [--gate terminal|file] [--repo <path>] [--workers n]
gophermind brief run <run-id> --check-env      (environment preflight, no model call)
gophermind brief report <run-id> [--json]
gophermind brief status <run-id>
gophermind brief coverage <run-id>
gophermind brief calls <run-id>
gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
gophermind brief vault list
gophermind brief tree check <run-dir>
```

Anything else under `gophermind brief` prints this usage and exits 1.
`run` builds an approved plan and `report` prints what a run left behind (see
"Running a plan").

Exit codes: 0 ok (for `run`: verified), 1 error (usage, unreadable file, failed
check, uncovered requirements, a failed run), 2 invalid brief (the message names
the offending field), 3 waiting on a human (`plan`, `resume` and `run` with the
file gate), 4 an escalated leaf or a human stop (`run`), 5 interrupted by a
signal or `max_run_minutes`, resumable (`run`), 6 a failed preflight (`run
--check-env`; not a run), 7 a harness fault (`run`: the repository cannot be
opened, the sandbox is refused, the settings are invalid, the plan changed since
the run started, a leaf is held by a live worker).

For an orchestrator driving `gophermind brief run`: stdout carries the summary,
whose last two lines are `Requirements covered: N of N` and `Acceptance passed: N
of N`; stderr carries progress lines, `report: <path>`, `waiting: ...`,
`interrupted; ...` and `sandbox: off` (printed once, at the start; the summary's
`Sandbox:` line and the report's environment section record it afterwards). A
failed run exits 1 and a harness fault exits 7, so the two can be told apart; a
harness fault that left no report prints `error: ...` and no summary.

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
are covered, the executor lines (see "Running a plan") and the model calls summed
by task type and node class. `coverage`
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

## Running a plan

`gophermind brief run <run-id>` builds the plan the planner approved. The run id
is the brief's `id`; the command finds the run through the run record, like
`resume`. It is safe to repeat: `_state/executor.json` says whether the run
starts or resumes, so there is no separate verb for resuming. `gophermind brief
resume` keeps its planner meaning (it continues planning and approval).

A plan that is not approved (no `approval.json`, or one that no longer matches the
plan) is refused with a fixed message and no model call. `--gate` is `terminal`
or `file` (default: `human.mode` of the settings); a person's answer to an
escalated leaf comes through it, and no answer means the run stops (exit 4 at a
terminal that ends its input, exit 3 with the file gate, which writes an
`ESCALATION-<node>.md` file into the run folder and waits). `--repo` runs against
another clone of the repository than the one the run record names (it must hold a
`.git`). `--workers` must be 1: leaves share one working tree.

The vault passphrase comes from `GOPHERMIND_VAULT_PASSPHRASE`, or from a prompt
only when a terminal is attached. A run that needs the vault (the brief declares a
secret, or a provider names `api_key_secret`) and has neither stops with a fixed
message; it never waits on input that cannot come. Provider base URLs come from
the settings.

The order of a run:

1. Approval check: the plan hashes must match `approval.json` and, on a resume,
   the hashes recorded at the first start.
2. Preflight: the sandbox, the `go` and `git` binaries, the settings. Nothing in a
   run calls a model before it passes.
3. Types and stubs: the contract's type declarations and one stub per leaf are
   written, so every package builds.
4. The deps step: the only step that touches the network (see below).
5. Wave 0 commit: the test files the planner wrote (leaf tests and Go acceptance
   tests), the types, the stubs, `go.mod` and `go.sum`; first the acceptance red
   check (see Acceptance root tests).
6. Waves in order. Each leaf is built from its contract alone through the
   escalation ladder (the same model again with the failure, the next model of the
   tier, a human); after each wave the harness builds, vets and runs the race
   tests of the verified leaves, and attributes a failure to a leaf or stops.
7. Acceptance: the plan's root tests run against the built binaries.
8. `go mod verify`, a full build, vet and race test, and a scan.
9. Landing.

`Requirements covered: N of N` and `Acceptance passed: N of N` are the two proof
lines, and they are the last two lines of every printed summary. The first is the
planner's coverage of the brief's requirements (features, constraints, acceptance
bullets). The second is printed only when every acceptance bullet has at least one
root test and every one of its tests exited 0 against the built binary; N is the
number of acceptance bullets in `requirements.json`, so a bullet that lost its
test cannot make N smaller.

Landing modes (`landing` in the brief):

- `commit`: the work happens on the branch `gm/<brief-id>`, one commit per leaf, then
  an empty final commit carrying the summary, then a fast-forward of the base
  branch to it. If the base branch moved, the run stops `failed` with
  `landing_blocked` and the work branch is left intact.
- `diff_only`: nothing is committed; the files stay in the working tree and
  `changes.patch` is written to the run folder.
- `pull_request`: refused at the start with an error naming the field.

GopherMind never pushes, forces, resets or rebases, and never stashes or discards
work in your tree.

While a run goes, one line per event is printed to stderr (a leaf verified or
failed, a block, a wave check, an escalation, a resume), made of ids, counts and
fixed words only, so they are safe to read after a `kill -9`. At the end, on every
path that produced a report (including a failed or interrupted one), the command
prints `report: <path>` to stderr and the summary to stdout. `waiting: answer in
<run folder>, then run gophermind brief run <id>` appears when the file gate is
waiting. When the sandbox is off, `sandbox: off` is printed once, at the start.

`gophermind brief report <run-id>` prints the summary of `report.json` (readable
for schema versions 1 and 2). `--json` prints the file unchanged. With no report
yet it says so and exits 1. `gophermind brief status <run-id>` gains, once the
executor has started, `executor: <status>` (`in progress` until a report exists),
the last stop reason, `waves done: <w> of <n>`, `leaves: verified <v> of <t>` with
the other counts, and one `blocked: <id> (waiting on <dep>)` line for each leaf
that waits on a failed or escalated leaf. Status reads the run folder and the
blackboard and takes no claim.

An escalation that needs a person ends the run with exit 4 (or 3 with the file
gate). A harness error that left no report (the plan changed since the run started,
a leaf held by a live worker, a refused preflight) prints `error: ...` and exits 7;
for a live worker, wait up to `executor.stale_claim_seconds` and run again.

### Preflight without a run

`gophermind brief run <run-id> --check-env` checks the environment and calls no model. It prints one line per check, `pass: <name>` or `FAIL: <name>: <reason>`, then `preflight: N of M passed`, and exits 6 when any check failed and 0 when all passed. Nothing is written. The checks, by their fixed names:

- `sandbox`: the setting, and that `sandbox-exec` runs when it is on.
- `go toolchain`: a `go` binary on the settings toolchain PATH.
- `git repository`: the repository root holds a `.git` entry (the check does not look for the `git` binary; the next check runs it).
- `clean tree`: `git status` shows nothing changed except the test files of the plan (a run that has already started is left to the executor).
- `vault passphrase`: the passphrase is in `GOPHERMIND_VAULT_PASSPHRASE` or a terminal is attached. It is `not needed` (and passes) when the brief declares no secret and no provider names an `api_key_secret`, because the run never opens the vault then.
- `secret <NAME>` for each declared secret: present under the run's scope (names only, never values).
- `secret host <NAME>` for each secret whose value is a URL with a loopback host: a 2 second dial.
- `provider <name>` for each provider that any tier chain uses: `GET /api/version` on the host answers 200, or `GET <base_url>/models` answers 2xx, 401 or 403 (a server that wants a key is there), within 3 seconds in all. A redirect, a 404 or a 5xx is not an answer.
- `approval`: present and valid.
- `disk space`: 2 GiB free where the module cache lives.

A provider may list `base_url_fallbacks` in the settings. When `base_url` does not answer, the fallbacks are tried in order, at preflight and at the start of a run, and the first that answers is used for that process only: the settings file is never rewritten. A fallback may not change who sees the prompts and the provider's key. The router's privacy rule keys on a provider's visibility, so a `private` provider (and every provider under `privacy.mode: private_only`) accepts only a private-network host: loopback, RFC 1918, the VPN range 100.64.0.0/10, link-local, IPv6 ULA, or a name ending in `.local`, `.internal` or `.lan`; a public provider accepts only public hosts. The host is judged by its text and no name is resolved. `settings.Validate` refuses a violating entry (the message names the provider and the key), and preflight and the start of a run skip one without dialling it. The default mini entry lists its VPN address (`http://10.8.0.6:11434/v1`). The preflight says which host answered, and the run report's environment section records the host (host only, never the URL).

## Sandbox and network

Every child process of a run (builds, vet, tests, acceptance commands and the
built binaries) runs under `sandbox-exec` on macOS. It may write only the
repository, a per-run scratch directory and the caches, it cannot write the run
folder or `.git`, it cannot read the rest of your home directory, it cannot read
`.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*` or `.git/config` inside the repo, and
it cannot reach any address but the loopback ports the run lists (the harness
proxy, the acceptance addresses and the loopback `host:port` of URL-valued
secrets), so a local Ollama or Postgres is out of reach unless it is listed. Off macOS the executor refuses to start unless
`executor.sandbox: off` is set; the setting is recorded in the report and printed
at the start and end. Only the deps step reaches a module proxy: the harness runs
a small forward proxy on loopback, with a host allowlist, and fetches modules
through it (the child itself only ever talks to loopback). Leaf commands run with
`GOPROXY=off`, so no leaf depends on the network. The proxy is policy and an audit
log (`proxy.log`); the sandbox is the containment. A run whose
`dependencies.json` is empty never touches the network.

The profile also closes the macOS escape routes that `(allow default)` leaves open:
a child cannot start anything through Launch Services (`open`, Apple events,
`osascript`), read the clipboard, talk to the security daemons, signal a process
outside its own process group, or exec `open`, `osascript`, `security`,
`launchctl`, `sudo`, `ssh`, `scp`, `nc`, `su` or `login`. `curl`, `go` and `git`
stay executable, but `git` run inside the sandbox cannot read `.git/config` and
fails; the harness runs its own git outside it. A child that calls `setsid` leaves
the process group, so after every command the runner finds the descendants by
ancestry, kills them and records a warning if one survives. The proxy pins a
provider's allowlist entry to its `base_url` port (so the Mac mini's address
reaches port 11434 only, never ssh or a database), while a brief `network` entry
without a port still matches every port of its host. The state database
(`blackboard.db`, with its `-wal` and `-shm` files) is created owner-only (0600).
Model calls never follow a redirect and ignore `HTTP_PROXY`; a call that times
out puts that model on a cooldown (`rate_limits.cooldown_after_timeout_seconds`,
default 60) and the next model in the chain is tried; a provider may set
`call_timeout_seconds` (at least 1) to override `defaults.call_timeout`, and a new
config gives the private mini 1500 (25 minutes); `settings.Validate` refuses a
`private` provider whose `base_url` is not a private-network host.

## Executor settings

The `executor` section of `gophermind.yaml` and its defaults:

```yaml
executor:
  workers: 1                    # must be 1
  fix_attempts: 2
  repair_rounds: 2
  acceptance_repair_rounds: 2
  test_timeout_seconds: 120
  acceptance_timeout_seconds: 300
  stale_claim_seconds: 120
  output_cap_bytes: 65536
  heartbeat_seconds: 30
  go_mod_cache: ~/.gophermind/gomodcache
  max_run_minutes: 720
  sandbox: on                   # on | off
  proxy: {listen: "127.0.0.1:0", log: proxy.log}
toolchain:
  PATH: /usr/local/go/bin:/usr/bin:/bin
```

Every number is positive. `toolchain.PATH` is where the executor finds `go`; nothing
else from your environment reaches a build. A provider entry may carry
`base_url_fallbacks` (a list of http or https URLs).

## Resume and limits

`max_run_minutes` bounds one invocation: at the limit no new claim is made, in-flight
calls are cancelled, claims are released, and the run ends `interrupted` with
`stop_reason: max_run_minutes` (exit 5). SIGINT and SIGTERM end it the same way
(`signal`, exit 5). Run `gophermind brief run <id>` again to continue: finished
leaves are not built again (a released leaf's file on disk is checked first and
committed with no model call when it passes), attempts keep their numbering, each
leaf is committed once, and the limit restarts from zero. The report of a run that
ever resumed says `resumed: true`, so a run that was meant to go through without a
restart can be told apart. A resume refuses to go on when the plan files changed,
when commits that are not this run's sit on the work branch, or when the base
branch moved away from it.

## Files the executor writes

In the run folder: `report.json`, `acceptance.json` (the proof of N of N: for each
requirement the command, exit code, output size and SHA-256), `changes.patch`
(`diff_only` only), `proxy.log`, `_state/executor.json` (the run's state), and
`_state/notes.json` (hints a person added on a retry). Prompt text, reply text,
command output and secret values are never stored, in these files, in the ledger or
in the blackboard: only ids, counts, sizes, hashes and fixed class words.

## Known limits

- There is no revise stage: a leaf the ladder cannot build is escalated to a person,
  and a bad test or a wrong contract is fixed by hand and the run continued.
- `landing: pull_request` and anything that pushes are not supported.
- The sandbox exists on macOS only; elsewhere the run needs `executor.sandbox: off`.
- Leaves are built one at a time (`workers` is 1).
- Adding a dependency after approval needs a new approval.

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
| Contract | The outline in harness-driven passes (a shared pass, then one per batch of 3 features), then one call per component (continued while it adds new functions) | `contracts.json` |
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
not need a bigger reply: the harness drives the outline passes, because a model
cannot tell what is left (it repeats earlier ids and still says more). Pass 1
(`contract:outline:1`) is the shared pass: module, conventions, dependencies, the
`types` component and the shared domain types, and no feature component. Then
the brief's features, in brief order, are split into batches of 3
(`outlineBatchSize`) and each batch is one pass (`contract:outline:2`, `:3`,
...): the prompt carries the whole brief, names the batch's features, and lists
the component and type ids already declared, which it may reference in `uses`
but must not repeat. The shared pass keeps only the `types` component: any other component in it is
dropped with the warning `outline_shared_extra` (the batches write them). The
list of declared ids in a batch prompt is bounded (the shared ids, the 150 most
recent ids, a count of the rest and up to 100 short entries of withheld
components), so a prompt does not grow with the brief; duplicates are handled
by the harness whether or not the model saw an id. The model never says `more`; the field is accepted and
ignored, and there is no cap on the number of passes because the batch list, not
the model, ends them. A pass that adds nothing new is not an error: it raises the
warning `outline_pass_empty` (the batch number only) and the run goes on; the
Coverage stage later checks the plan against the brief. Passes are merged by id
(first emission kept), each pass is stored in `_state/contract.json` together
with the batch list (`outline_batches`) and the index of the next batch
(`outline_batch_next`), so a resumed run asks only for the unfinished pass. Between passes
only local checks run (a type may use one a later pass writes); when the
outline is complete, ids still used but never declared are asked for in up to 2
repair passes (`contract:outline:repair`, stored like any pass), and only then does the stage fail, naming
at most 10 of the ids. An id that a later emission repeats with different content (in the same reply
or a later pass, for components, types and functions) is model noise, not a
plan defect: the first emission is kept, the repeat is dropped, a warning
`outline_duplicate_ignored` names the ids (only ids of at most 64 bytes that
pass the id syntax, anything else by length, with this pass's count and the
running total) and they are listed in `_state/contract.json` under
`ignored_duplicates` (at most 200, with the exact count in `ignored_total` and
`ignored_truncated` when the list was cut; the run report shows them as
`planner_warnings`). Components, types and functions share one id namespace, so
a function whose id equals a type id is dropped the same way; coverage later checks the plan against the brief, so
nothing the brief needs is lost. Identical repeats are dropped silently. Ids are
normalised before validation: a model's `IntakeSession`, `intake_session` or
`Intake Session` becomes `intake-session` (case boundaries split, lower case,
other runs of characters one dash; a function id also gets its `fn-` prefix),
and every reference to it in the same reply (`uses`, a function's `component`)
or to an earlier pass is rewritten the same way, so a resumed run, whose stored
contract is already normalised, rewrites identically. A declared id that
already matches the syntax is never touched, and neither is a reference to one.
A reference to an id nobody has declared yet (a function a later pass writes,
for example `ValidateEmail` or `validate-email`) keeps both forms,
`validate-email|fn-validate-email`, in the stored contract until every
component is written; then each becomes whichever form is declared (if both
are, a function's `uses` takes the function and a type's the type), and
contracts.json carries declared ids only. A reference still undeclared goes to
the repair pass as one id, `validate-email`, with a hint that a function would
be `fn-validate-email`. (A type that names a function a later pass writes is
asked for in the outline repair as a type first.) Two different spellings that become one id keep
the first. A warning `outline_id_normalized` gives the count and at most 10
examples (the old id only when it is at most 64 printable ASCII bytes, else its
length); the total is kept in `_state/contract.json` as `ids_normalized`. A
reply that still fails the schema is asked for again once with the bounded
list of offending pointers (the first 5 and a count, never reply text). A
model's `exports` in the outline are ignored (the harness fills them). The
component ids `logs` and `outline` are reserved (a run folder and a stage
name); the repair stage is `contract:_repair`, which no component can be named
because ids cannot start with an underscore, so a component called Repair is
fine. Failed repair attempts count toward the bound across restarts.
Component passes work the same way: a component reply may say `more` and is
continued only while each reply adds a new function (a repeat with nothing new ends
the component, it is not an error; at most 40 passes per component, then a fixed error); a function may use
one a later component writes, and ids still undeclared after every component is
written go through up to 2 `contract:_repair` passes.

Reshaped nodes. Before the leaf checks run, a Decompose node is normalised for
deviations that change only its shape: `side_effects` entries sent as objects
(`{type, target, description}`) become one string each, a string where an array
is wanted becomes a one-element array, null becomes an empty array or object, a
`model_tier` is trimmed and case-folded to the schema's enum (an invalid value
becomes `standard`; a node class written there is taken as the `node_class` when
that is missing), properties the schema forbids are dropped, and an empty title is
derived from the function name in the signature. The warning `leaf_normalized`
gives the count and at most 10 examples (function id, field, kind of change,
never the content). A node that still fails the schema is reported to its repair
by field and keyword in a fixed vocabulary (`field:contract.side_effects[]
keyword:type`, `[]` for an array item, only property names the schema knows), and
the stage failure names the same for at most 10 nodes. Nodes saved as pending by
an earlier run are checked again with the current normalisation before any model
is asked.

Decompose repairs one node at a time. A node of a Decompose reply that fails a
leaf check (an input per parameter, an output per result, `errors` for a function
that returns `error`, a node class, a title, a description, known `depends_on`
ids, the node schema) no longer sinks its batch: the nodes that passed are stored
at once, and the others are kept in `_state/decomposed.json` under `pending` with
their defects as fixed words (`outputs_count`, `inputs_missing`, `errors_missing`,
`title`, ...) and the passes spent on them. Up to 2 `decompose:_fix` passes ask for
only those nodes, at most 8 per call: the prompt carries each node's id, its
defects, the contract entry of the function and the node as it stands, and only
the fields the defects name are taken from the answer. Failed attempts count
across restarts. After the bound, `inputs`, `outputs` and `errors` of a node whose
defects are all structural are derived from the function's signature (an input per
parameter, an output per result named by the result name or `result` and `err`,
description `returned value`, and `returned error` for a function that returns
`error`) and the warning `leaf_defaulted` gives the count and at most 10 ids;
titles, descriptions, node classes and dependencies are never invented, so a node
still missing one of them ends the stage with an error naming it. A reply that is
not a JSON array of nodes at all is still an unusable reply. Nodes in a reply that
nobody asked for, or repeated, are ignored (the first is kept).

Developer debug dump (not for graded runs). When `GOPHERMIND_DEBUG_DUMP_DIR` names
an existing directory, the planner writes, for every model reply it parses,
`<stage>-<n>.request.json` (the prompt messages it built; a router retry after an
unusable reply adds the previous reply and the error to the prompt, which is not
shown) and `<stage>-<n>.reply.txt` (the raw reply) with mode 0600, and prints
`debug dump enabled` on stderr once. It exists to see what a model really sent
when a run fails and is never referenced from the ledger, the state, the events
or the report. It is ignored, with a warning on stderr, when the directory is
inside the target repository, the run folder or the config folder. The
orchestrator never sets it; do not set it for a graded run, because the files
hold prompts and replies in clear text.

Prompt size. Every contract call is sized to the smallest context window among
the models of the strong tier (`context_tokens` in `gophermind.yaml`): the
estimate of the prompt (bytes/4 plus a tenth, the router's own) plus the reserved
reply plus a 1000 token headroom must fit. If it does not, optional context
shrinks in a fixed order instead of the call failing. Level 0 is everything (an
outline pass carries the whole brief, so a small brief is sent unchanged). Level
1 replaces the brief by an excerpt: Overview, Data, Architecture and Constraints
plus only the features the pass concerns, each capped. Level 2 cuts the
declared-ids list and the caps further and lists declarations without their
text. Level 3 trims existing nodes to signature and doc. The repair passes
(`contract:_repair`, `contract:_schema`, the outline repair) never carry the whole
brief: they carry the offending nodes, their components and the brief sections
of those components' features. If a model refuses a prompt as too long (the
router's typed `too_long`, which is not an unusable reply) the prompt is rebuilt
one level down and asked again; when level 3 is refused or does not fit, the stage
fails with a fixed message naming the stage. Each call reports
`prompt_tokens_estimate`, `reserved`, `window` and `level` as a `prompt_size`
event (sizes only).

The merged contract is stored in `_state/contract.json` before its last
validation. If a node then lacks a required field (a function without `doc`,
`signature`, `file` or `package`; a type without `decl`, `file` or `package`),
the stage asks for those nodes again in up to 2 `contract:_schema` passes, at
most 10 nodes per call: the prompt lists each id, the fields it lacks and the
node as it stands, and only the missing fields are taken from the reply (the
first emission's other fields stay). Attempts count across restarts
(`schema_repairs`), and nothing already asked is asked again on `resume`. After
the bound only a missing `doc`, a documentation comment, is filled with
`<Name> implements <component> behaviour described in the brief.` (Name from the
signature) and the warning `doc_defaulted` gives the count and at most 10 ids;
any other gap, and any gap that cannot be named (an id that fails the syntax),
ends the stage with an error that names each node and field (at most 5 and a
count), never a bare JSON pointer.

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

## Acceptance root tests

A root test is the shell command that proves one acceptance bullet (or a
command-checked constraint). The executor counts a bullet as passed when its
command exits 0, so a command that cannot fail makes `Acceptance passed: N of N`
meaningless. Five rules keep that number honest.

**One shared checker.** `briefv2/acceptcheck` reads a command the way the shell
does (quotes, pipes, `&&`, `||`, `$( )`, here-documents, `sh -c`). The planner and
the executor both import it; the planner never imports the executor. Its
`Vacuous` level says a command cannot prove the built server at all (a no-op, `go
run|install|get|generate`, a binary by a path, nothing that touches a build or the
server), and the executor still uses only that level. Its `Quality` level says a
command cannot fail for the right reason, and the planner uses it. The findings
are a fixed vocabulary and never quote a command: `masked_failure` (`|| true`,
`|| :`, `|| echo ...`, `|| exit 0`, `2>/dev/null || true`, `set +e`, a last line of
`; true` or `; echo ...`), `success_echo` (a trailing `&& echo OK` is the only
assertion), `placeholder` (`{id}`, `<id>`, `...`, TODO, `(simulate`),
`undefined_variable` (a `$NAME` nothing earlier in the command sets and that is
not a `GM_ACCEPTANCE_*`, declared env or secret name), `curl_unasserted` (no
`-f`/`--fail` and the output is not tested), `jq_unasserted` (no `-e` and the
output is not tested) and `print_only` (echo, printf, cat, ls, find without
`-exec`, head, awk without `exit`). A guard that exits non-zero (`|| exit 1`) is fine.

**The prompts state the harness contract.** `planner/prompts/harness.md` is put into
the Coverage and Coverage-fill prompts: commands run with `sh -c` from the repo
root; every main package under `cmd/` is built into a directory that is first on
`PATH`; `GM_ACCEPTANCE_ADDR`, `GM_ACCEPTANCE_URL`, `GM_ACCEPTANCE_BIN` and
`GM_ACCEPTANCE_PIDFILE` are exported; no placeholders; pass means exit 0. It has
three good and three bad examples, and a test holds every example to the checker.

**The quality gate.** After the first Coverage reply and after each fill round the
checker runs on every root test. A finding is a coverage defect handled by the
existing bounded fill rounds: the fill prompt lists requirement ids and finding
names only, and a replacement root test drops the flagged one. A root test that is
still weak after `max_coverage_rounds` stops the planner before approval with a
fixed message of at most 10 requirement ids and finding names (a
`planner.QualityError`); `approve` runs the same check, so a `coverage.json` edited
by hand cannot be approved either. `APPROVAL.md` has a `Quality` column per
requirement: `ok` or the finding names.

**Go acceptance tests.** A narrative acceptance bullet (no command in backticks)
that describes a flow is proven by a Go test, not a shell script. When the
Coverage reply declares `serve` (`{"command": "venture-server serve", "ready":
"/healthz"}`, kept in `coverage.json`, so the approval hash covers it) the harness
fixes that bullet's root test command to `go test -tags acceptance ./acceptance
-run '^TestA8$' -count=1 -v`, whatever the model wrote. After approval the
Test-writer writes `acceptance/<id>_test.go`: package `acceptance`, standard
library only (no `os/exec`, `syscall`, `httptest`), one `TestA<N>`, a `//go:build
acceptance` line the harness adds (so a plain `go test ./...` never runs it),
`os.Getenv("GM_ACCEPTANCE_URL")`, `t.Fatal` on any unmet expectation, never
`t.Skip`, and helpers only with the prefix `a<N>`. The file goes through
`pathsafe`, is hashed into `_state/acceptance_tests.json` (like
`leaf_tests.json`), is committed by Wave 0 and is checked against its hash before
Wave 0 and before every acceptance round (`test_file_changed`). The executor
starts the `serve` command once per acceptance round, waits until the ready path
answers, runs those tests against `GM_ACCEPTANCE_URL`, kills the process group
and checks nothing is left (`acceptance_serve`, `acceptance_leak`). Any command
that is one plain `go test` invocation is run through `go test -json` and judged
by the runner's rule: a test ran and passed, none failed, every test named by
`-run` passed, a skipped test is not a pass.

**The acceptance red check.** At Wave 0, once the stubs compile, each root test
that exercises the server (it runs a built binary, curl, a `GM_ACCEPTANCE_*`
variable, a loopback address, or a go test of the acceptance package) runs
against the stub repository and must fail. One that passes proves nothing: the run
stops `failed` with `acceptance_not_red`, naming bullet ids only. Commands that
only check the code (`go build`, `go vet`, `gofmt`, `grep`) are exempt.

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
`decompose`, `coverage`, `testwrite`), node class for a call about one function
(a leaf call only: the clarify, contract, decompose and coverage stage calls leave it empty),
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
  acceptcheck/ the shell-aware checker of acceptance root test commands (planner and executor)
  executor/   the plan loader, leaf loop, waves, acceptance, resume, landing
  report/     report.json and the printed summary
  gitland/    the work branch, commits and landing
  sandbox/    the sandbox-exec profile
  proxy/      the harness module proxy
  runner/     build, vet, test and acceptance commands
cmd/gophermind/brief.go        the `gophermind brief ...` command group
cmd/gophermind/brief_plan.go   plan, resume, status, coverage, calls
cmd/gophermind/brief_run.go    run, report, the executor status lines, base URL fallbacks
cmd/gophermind/brief_preflight.go  run --check-env
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
