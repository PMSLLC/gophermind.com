# v2 Planner Core: Design

Status: draft for review. Date: 2026-09-29.
Scope: handoff build items 5 to 8 (planner, blackboard, providers with the fallback router, human gates), plus a model-call ledger and a need-to-know privacy rule.
Builds on: `docs/superpowers/plans/2026-09-29-brief-v2-foundations.md` and `2026-09-29-brief-v2-env-and-scan.md` (both merged and released in v0.9.0). Reference material: `docs/briefv2/handoff/` (SPEC.md, BUILD_PLAN.md, prompts, interfaces). Where this document and BUILD_PLAN.md differ, this document says so in "Deviations".

## 1. Why this exists

The long-term goal is that the desktop app runs a whole brief from start to finish: plan it, ask its questions, get approval, build it, and show who did what. The order agreed with John is:

1. The v2 engine first (this document covers its planning half), then the executor.
2. A run service in `gophermind-server` and the app screens after that.

Everything here is therefore built so that the terminal, files, and later the app can all drive it: the human gates and the progress reporting are Go interfaces, and the terminal is only one adapter.

## 2. Decisions taken with John (2026-09-29)

| # | Question | Decision |
|---|---|---|
| 1 | What "everything" in the app means | The full flow plus the v2 extras (secrets and env prompts, git landing, per-model report, Gantt live view) |
| 2 | Order of work | v2 engine first, then the run service and the app |
| 3 | Which models | The Mac mini's Ollama as the backbone, plus free providers that need no key (Kilo Code, OVHcloud); cloud keys can be added later through the vault |
| 4 | Blackboard | Build the pure-Go SQLite one; no existing blackboard is reused |
| 5 | Config format | YAML at `~/.gophermind/gophermind.yaml` (not TOML: no TOML library exists and nothing reads `GOPHERMIND.toml`) |
| 6 | Tracking | Record every model call (planning, questions, test-writing, build attempts, rewrites), not only build attempts |
| 7 | Privacy | Need-to-know: outside providers get only what a task needs; planning stays on the mini |

## 3. Goals and non-goals

Goals:

- `gophermind brief plan <brief.md>` takes a validated brief through Load, Clarify, Contract (Wave 0), Decompose, Coverage, Approve, and Test-writer, producing a schema-valid task tree, a `contracts.json`, and test files, with nothing executed.
- No plan reaches Approve while a requirement stated in the brief (a feature, a constraint, an acceptance bullet) has no node and no runnable test behind it. A plan that silently drops part of the brief is the failure this goal exists to prevent (section 9, Coverage).
- Every model call is routed by tier through a fallback chain that survives rate limits, bad keys, timeouts, and oversized prompts.
- Every attempt, success or failure, leaves exactly one ledger row saying who answered and how it went.
- A public (outside) provider never receives a call that carries the brief.
- No ceiling on plan size. Nothing in the planner caps the number of components, functions, or tests; a large brief is handled by making more calls (Contract per component with continuation, Decompose in batches), never by producing a coarser plan.
- Every leaf is one function, and code refuses a leaf that does not describe every input and output of its signature, its error conditions when it can fail, and a test for the happy path and for each error condition.
- Every model call is recorded with the kind of work it was (`task_type`) and, for a leaf, the class of function (`node_class`), so model performance can be compared by task type.
- The whole planner runs offline in tests against canned replies.

Non-goals (later plans): the wave scheduler, worker loop, and test runner; git landing; the network proxy; roll-up and the report; the run service; the app screens; the Gantt view.

## 4. Architecture

New packages under `gophermind-lib/briefv2/`. Dependencies point downward only.

```text
planner      stages, prompts, run directory state          uses: router, ledger, human, tree, contract, brief, vault
human        Gate interface + terminal, file, programmatic adapters
router       tier chains, cooldowns, privacy rule, per-provider concurrency; writes the ledger
provider     Provider interface (handoff) + OpenAI-compatible client + fake (fixtures)
ledger       Ledger interface + SQLite implementation (the calls table)
blackboard   Blackboard interface (handoff, exact) + SQLite implementation
db           opens ~/.gophermind/blackboard.db, applies migrations, hands out the shared *sql.DB
settings     reads gophermind.yaml, writes defaults on first use, validates
```

Existing packages reused unchanged: `brief`, `tree`, `contract`, `vault`, `schema`, `rundir`, `execenv` (later). Existing v1 code (`plantree`, `orchestrate`, `phaseflow`, `freellm`) is not modified. `freellm.Registry` supplies base URLs and documented limits for the default provider list; its usage counter is not used by v2 (the ledger replaces it for v2 runs).

## 5. The path of every model call

Every stage calls one function:

```go
type Tier string  // "strong" | "standard" | "any"
type Scope string // "brief" | "component" | "node"

type CallInfo struct {
    RunID    string
    Stage    string // clarify, contract, decompose:<component>, coverage, coverage_fill, testwrite:<node>, revise:<node>, implement:<node>
    NodeID   string // empty for run-level stages
    Tier     Tier
    Scope    Scope
    Revision int
    TaskType string // clarify, contract, decompose, coverage, testwrite, revise, implement: the stage without its suffix
    NodeClass string // for a leaf call: pure, validation, handler, client, storage, concurrency, wiring, other
    Exclude  []string // provider/model entries to skip, used after a malformed reply
    Only     string   // if set, try only this provider/model entry
}

type Result struct {
    provider.Response
    CallID   int64  // the ledger row this attempt wrote
    Entry    string // provider/model that was asked
    ChainPos int
}

func (r *Router) Call(ctx context.Context, info CallInfo, req provider.Request) (Result, error)
```

The router walks `models[tier]` in order. For each `provider/model` entry it:

1. Skips the entry if its provider is cooling down, was disabled for the run by an auth failure, or is not eligible for `info.Scope` under the privacy rule (section 6).
2. Skips the entry, recording outcome `too_long`, if the estimated prompt size (bytes divided by 4, plus 10 percent) plus the request's `MaxTokens` exceeds the model's context.
3. Takes one of the provider's `max_concurrent` slots, calls the provider with `config.call_timeout` (default 10 minutes; the mini is slow), and releases the slot.
4. Writes one ledger row for the attempt whatever happened.
5. On success returns the response. On `ErrRateLimited` it puts the provider in cooldown for `Retry-After` or `cooldown_after_429_seconds` and moves on. On `ErrTransient` it retries the same model with backoff up to 3 times, then treats it as rate limited. On `ErrContextTooLong` it moves on. On `ErrAuth` it disables the provider for the run and warns once. On a malformed reply (the stage's parser rejects it) `router.CallParsed` marks that ledger row `malformed`, retries the same model once (`Only`) with the parse error appended, then calls the router again with that model in `Exclude`, until a reply parses or the chain is exhausted. A provider that answers 404 "model not found" (the mini's Ollama did this on 2026-09-29 when a model was removed) is treated as `model_missing`: that entry is skipped for the rest of the run and a warning is emitted.

If every entry is unavailable only because of cooldowns, the router waits for the shortest cooldown and tries again, for up to `max_wait_minutes` (default 30) in total per call. When the chain is exhausted it returns `*ChainExhausted`, which lists each entry and why it was skipped or failed, so the caller can decide between waiting, asking a human, or failing.

Ledger write failures do not fail the call: the model result is kept, an `Event{Kind: "ledger_error"}` is emitted, and the run is marked incomplete in its status. A full disk should not throw away an hour of model work.

## 6. The privacy rule (need-to-know)

Each provider in the config has `visibility: private` (the mini, anything on John's hardware) or `visibility: public` (Kilo Code, OVHcloud, any cloud API). Each call declares the widest thing it carries:

| Scope | Carries | Stages |
|---|---|---|
| `brief` | the whole brief | Clarify, Contract, Coverage |
| `component` | one component's slice of the brief | Decompose |
| `node` | one function's contract, its dependency signatures, its tests | Test-writer, Revise, and (executor plan) Implement |

The scope is fixed by the stage in code, never chosen by a model. The router applies `privacy.mode`:

- `need_to_know` (default): public providers are eligible only for `node` scope.
- `private_only`: public providers are never used.
- A run started with `--allow-public` also allows public providers for `component` and `brief` scope; the run's status records that it did.

No prompt ever contains a secret value (already a project rule); only secret names appear where the brief lists them. What still leaks to a public provider in `need_to_know` mode is the contract and tests of one function, which reveals what is being built but not why or for whom.

## 7. The call ledger

One SQLite table, `calls`, in the same database file as the blackboard:

```text
id INTEGER PRIMARY KEY, run_id, at (UTC RFC 3339), stage, task_type, node_class, node_id, revision,
scope, tier, chain_pos, provider, model_requested, model_served,
prompt_tokens, completion_tokens, prompt_bytes, prompt_sha256,
response_bytes, response_sha256, duration_ms,
outcome, error_kind, retry_after_s
```

- `model_served` comes from the reply, because Kilo Code's auto-router picks the model (a live probe returned `stealth/space-bunny-alpha` for `kilo-auto/free`).
- `outcome` is one of `ok`, `rate_limited`, `timeout`, `malformed`, `auth`, `too_long`, `model_missing`, `error`. A reply that arrives fine but fails the stage's parser is first recorded `ok` and then corrected to `malformed` with `Amend`.
- Prompt and reply text are never stored, only sizes and SHA-256 hashes. Indexes: `(run_id, at)` and `(run_id, node_id)`.

```go
type Ledger interface {
    Record(ctx context.Context, c *Call) error                      // sets c.ID
    Amend(ctx context.Context, id int64, o Outcome, errorKind string) error
    List(ctx context.Context, runID string, f Filter) ([]Call, error)
    Summary(ctx context.Context, runID string) ([]ModelSummary, error) // per task_type, node_class, provider, model: calls, tokens, total time, outcome counts
}
```

The ledger says who answered and how the call went. Whether a build attempt's tests passed lives on the blackboard attempt, which the later report joins to the ledger row by node, revision, and chain position. Planner stages are not leaves, so they have ledger rows and no blackboard rows.

## 8. The blackboard

`blackboard.Blackboard` is implemented exactly as `docs/briefv2/handoff/interfaces/blackboard.go` defines it (copied into the package; the handoff copy stays as reference). Backend: `modernc.org/sqlite` (already a dependency), one file `~/.gophermind/blackboard.db`, WAL mode, `busy_timeout=5000`. Tables `rows` and `events` as in BUILD_PLAN item 6:

- `Claim` is a single `UPDATE rows SET status='claimed', ... WHERE run_id=? AND node_id=? AND status='ready'`, returning whether one row changed.
- `SetStatus` checks the transition table and the current status in one statement (compare and set).
- `ReleaseStale` returns claimed or in-progress rows whose heartbeat is older than a threshold to `ready`.

The planner writes the initial rows (`InitRun`, all `pending`, with waves) at the end of Test-writer, once the tree is complete. Nothing in this plan claims or executes a leaf; the executor plan does.

## 9. Stages

Each stage is idempotent and skipped on resume when its output already exists in the run folder `.gophermind/<brief-id>/`.

| Stage | Output | Tier | Scope |
|---|---|---|---|
| Load | run folder, `brief.md` copy, secrets in the vault under `run/<id>`, `requirements.json` (parsed by code, see Coverage) | none | none |
| Clarify | `answers.json` (questions asked, answers given or assumed) | strong | brief |
| Contract (Wave 0) | `contracts.json`, validated against the contract schema; built in passes (below) | strong | brief for the outline, component for each component pass |
| Decompose | function node drafts in `_state/decomposed.json`, then the root and component nodes in the tree; waves and harness-derived `dependency_signatures` computed | strong | component |
| Coverage | `coverage.json` mapping every requirement to the nodes and tests that satisfy it, plus warnings; gaps are filled or the run stops | strong | brief |
| Approve | `approval.json` with `approved_at`, `approved_by`, and a hash of the rendered plan | none | none |
| Test-writer | test files written to the target repo, tests recorded on each leaf node, the complete tree written and checked, blackboard rows created | strong | node |

Approve comes after Decompose and Coverage, not straight after Clarify as SPEC.md's lifecycle lists it, because the plan being approved (components, function counts per component, wave count, coverage, as BUILD_PLAN item 5 says to render) only exists once Contract, Decompose, and Coverage have run. Contract, Decompose, and Coverage only make model calls and write inside the run folder; Test-writer is the first stage that touches the target repo, and it refuses to run without a matching `approval.json`.

### Size: no ceiling, more calls

The v1 planner's plan for a 750-line brief was 99 steps, about two per task, because each call saw a slice of the brief and wrote a thin outline for it. Here the amount of output per call is bounded and the number of calls is not:

- **Contract runs in passes.** One `contract:outline` call (scope `brief`) returns the module, conventions, the component list, and the shared types. Then one `contract:<component>` call per component (scope `component`) returns that component's functions and any types only it needs, seeing the outline, the shared types, the signatures already written for earlier components, and its own section of the brief. A component reply ends with `"more": true` when functions remain; the harness calls again with the ids written so far until it is false. After every merge the whole contract is validated.
- **Decompose runs in batches** of at most 8 functions per call within a component.
- There is no limit on components, functions per component, or tests per function anywhere in code.

### What every leaf must carry

Checked by code on every Decompose draft; a draft that fails is `malformed` and takes the retry path of section 5:

- The signature parses as Go (`go/parser`).
- `inputs` has an entry for every parameter of the signature, by name, and each entry has a `type`; `outputs` has an entry for every result.
- A function whose results include `error`, or whose contract doc or side effects name a failure, has at least one `errors` entry, each with `when` and `returns`.
- `node_class` is one of `pure`, `validation`, `handler`, `client`, `storage`, `concurrency`, `wiring`, `other`. The model gives it beside each draft; it is kept in `_state/classes.json` (the node schema has no field for it) and travels on every later call for that node.

Checked by code on every Test-writer reply:

- At least one test for the happy path plus one for each `errors` entry, so `len(tests) >= len(errors) + 1`.
- The test file parses, names the expected test function, and imports only the standard library or the module's own packages.

Type checking of the implementation itself is the compiler's job at build time; the executor plan must also run each leaf's tests before the implementation exists and require them to fail, so a test that cannot fail is caught.

### Coverage

Why: on 2026-09-29 the v1 planner turned the 730-line AI Venture Studio brief into a plan of 21 phases, 47 tasks, and 99 steps, all approved. Read against the brief afterwards, 96 of the 99 steps had no test command, none of the brief's end-to-end acceptance round trips (SSE event order, ownership `as_of`, financial totals to the cent, cross-company 404) appeared anywhere, the constraints (a scan that every SQL statement carries `company_id`, `gofmt` and `go vet` clean, the 60-line handler cap) had no step, password hashing said "bcrypt/argon2" where the brief requires argon2id only, and the plan spread its entry point across `cmd/venture-server`, `cmd/server`, and `cmd/fake-llm` where the brief names one binary, `cmd/venture-server`, while 18 paths the brief names (`internal/tenancy` among them) had no file under them. Nothing in that planner compared the plan with the brief, and nothing here would have either. This stage does.

**Requirements are parsed by code, not by a model.** At Load, the brief file is scanned into `requirements.json`:

- Each top-level bullet under `## Constraints` is one requirement, id `C1`, `C2`, and so on. Indented continuation lines and sub-bullets belong to their parent bullet.
- Each top-level bullet under `## Acceptance` is one requirement, id `A1`, `A2`, and so on.
- Each `### Feature` heading under `## Features` is one requirement, id `F1`, `F2`, and so on, carrying the heading as its name and the block as its text.
- A line inside a fenced code block never starts a requirement. `## Out of scope` is not a requirement source.

Every requirement keeps its verbatim text and its 1-based line in the brief file.

**The model proposes the mapping; code checks it.** Coverage runs after Decompose, when function nodes exist as drafts without tests (Test-writer adds tests after approval). The Coverage call (tier strong, scope `brief`, so it stays on private providers under `need_to_know`) receives the requirements and the plan's nodes (id, kind, parent, title, file) and replies with two lists: `map`, giving for each requirement the ids of the function or component nodes whose code satisfies it, and `root_tests`, acceptance tests for the root node, each naming the requirement it proves and carrying a shell `command`. The harness then checks, without a model:

1. Every node id named exists in the plan. An unknown id is dropped and named in that requirement's gap reason. A reply that names an unknown requirement id, or gives a root test without a name or a command, is `malformed` and takes the retry path of section 5.
2. A covering node is a function node, or a component node with at least one function node under it. The root node alone covers nothing.
3. A `C` requirement needs at least one covering node or at least one root test.
4. An `F` requirement needs at least one covering node.
5. An `A` requirement needs at least one root test. Covering nodes are optional for it.

A covering function node counts before it has a test because Test-writer gives every function node at least one test whose command the harness derives, and the node schema rejects a function node without one, so a function node in the finished tree always has a runnable test. Root tests are the only model-written commands in a plan. They are printed in full in the Approve summary, because approving the plan approves running them at roll-up.

**Gaps are filled, then the run stops.** Requirements that fail a check are sent back in one `coverage_fill` call (same tier and scope) with each gap's id, text, and reason. The reply may add `map` entries, add `root_tests`, and declare the contract `types` and `functions` that are missing (each function naming an existing component). The harness appends the declarations to `contracts.json` (revision plus one, validated again), runs Decompose for just the new functions, and re-checks coverage. This repeats up to `defaults.max_coverage_rounds` (2) times. If gaps remain, the run stops in this stage listing each uncovered requirement, and `resume` restarts only this stage. It never proceeds to Approve with an uncovered requirement.

**Warnings, which do not block.** Three mechanical checks add to `coverage.json` and to the Approve summary:

- Every backticked token in the brief that begins `cmd/` or `internal/` and is a plain path should be a path prefix of at least one function node's `contract.file`; each that is not is listed.
- Every `cmd/<name>` directory that a function node's `contract.file` sits under should be named in the brief; each that is not is listed. This is the check that would have caught the stray `cmd/server` and `cmd/fake-llm`.
- An `A` bullet that begins with a backticked command should have that command inside one of its root tests' commands; each that does not is listed.

What code cannot check: a mapping that names a real node for the wrong requirement passes every check above. That gap is real (see section 16), so the Approve summary lists each requirement beside its covering nodes and root tests for a person to read.

**Approve shows the result.** The rendered plan gains a coverage table (requirement id, first 80 characters of its text, covering node ids, root test names), the root test commands in full, the warnings, and a line such as "Requirements covered: 41 of 41". The approval hash is taken over the rendered plan, which contains everything in `coverage.json`, so editing the mapping after approval invalidates the approval.

**Run folder.** `requirements.json` and `coverage.json` sit beside `answers.json`; working files sit under `_state/`. The tree store skips all three when it loads nodes (a named two-line change to `tree/store.go`).

Prompts are the six templates in `docs/briefv2/handoff/prompts/`, adopted as written with two changes: the `QUESTION:` protocol below, and output-format reminders trimmed to what the parsers check. Coverage adds two templates of its own, `coverage.md` and `coverage_fill.md`, which the handoff does not have. Each reply is stripped of code fences and leading prose before parsing. A reply that still does not parse counts as `malformed`.

**Target repo.** The brief's `repo` must be an existing local path (`~` is expanded). Test-writer writes test files into its working tree and leaves them uncommitted; the git-landing plan commits them in the Wave 0 commit. A `repo` that is a URL is rejected in this plan with a message saying cloning belongs to git landing.

**Approval gate.** The plan (component list, function count per component, wave count, assumptions made, secret and env names, hosts, landing mode) is rendered as Markdown and hashed. Test-writer, and everything the executor plan adds, refuses to run without an `approval.json` whose hash matches the plan as it stands. `--yes` records `approved_by: "flag"`.

**Pause and ask.** A Decompose or Test-writer reply containing `QUESTION:` alone on a line is routed to the human gate, the answer is stored in `answers.json`, and that one call is rerun with the answer added. In `on_ambiguity: assume_and_document` mode the planner instead picks the conservative option and records it in the node's `assumptions`.

**Derived, never model-written:** `dependency_signatures` (sliced from `contracts.json` by `contract.Slice`), `wave` (computed by `tree`), each function test's `command` (always `go test ./<package dir> -run ^<TestFunc>$`, with the test function name derived from the node id), the test file's path, and node validity (every write goes through the schema validator).

## 10. Human gates

```go
type Gate interface {
    Ask(ctx context.Context, qs []Question) ([]Answer, error)       // blocks until answered
    Approve(ctx context.Context, plan PlanSummary) (Decision, error)
    Escalate(ctx context.Context, e Escalation) (Resolution, error) // used by the executor plan
}
```

Adapters:

- **Terminal:** prompts on stderr, reads stdin; numbered questions, empty input takes `default_if_unanswered`.
- **File:** writes `QUESTIONS.md`, `APPROVAL.md`, or `ESCALATION-<node>.md` with a fenced answer block into the run folder and returns `ErrWaiting`; the command exits with code 3; `resume` reads and validates the answers.
- **Programmatic:** answers arrive over a channel and requests are published as events. This is what the run service and the app will use; it is included now so the interface is proven by two real users and one test double.

Progress reporting is a second small interface the engine writes to and never reads from:

```go
type Event struct { Kind, Stage, NodeID, Message string; At time.Time; Call *Call }
type Sink interface{ Emit(Event) }
```

Kinds: `stage_started`, `stage_finished`, `waiting_on_human`, `call` (carries the ledger row), `ledger_error`, `warning`.

## 11. Config

`~/.gophermind/gophermind.yaml`, created with these defaults on first use:

```yaml
providers:
  - name: mini
    base_url: http://192.168.1.35:11434/v1
    visibility: private
    max_concurrent: 1
    models: [{id: "qwen3.6:35b-a3b", context_tokens: 32768}]
  - name: kilo
    base_url: https://api.kilo.ai/api/gateway
    visibility: public
    max_concurrent: 2
    models: [{id: "kilo-auto/free", context_tokens: 131072}]
  - name: ovh
    base_url: https://oai.endpoints.kepler.ai.cloud.ovh.net/v1
    visibility: public
    max_concurrent: 1
    models: [{id: "Qwen3.6-27B", context_tokens: 131072}]
models:
  strong:   ["mini/qwen3.6:35b-a3b"]
  standard: ["mini/qwen3.6:35b-a3b", "kilo/kilo-auto/free"]
  any:      ["kilo/kilo-auto/free", "ovh/Qwen3.6-27B", "mini/qwen3.6:35b-a3b"]
privacy: {mode: need_to_know}
defaults: {max_context_tokens: 8000, max_revisions: 2, max_coverage_rounds: 2, call_timeout: 10m, max_wait_minutes: 30}
rate_limits: {cooldown_after_429_seconds: 60, backoff_initial_seconds: 5, backoff_max_seconds: 300, backoff_multiplier: 2}
human: {mode: terminal}
vault: {path: ~/.gophermind/vault.age}
```

A provider's key, when it has one, is named by `api_key_secret` and read from the vault's `harness` scope; a value is never written to this file. The default list uses only providers that need no key. Context sizes for the default entries come from `freellm`'s registry where it has them and are otherwise conservative.

## 12. Commands

Under `gophermind brief` (the namespace already in use):

- `plan <brief.md> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]`
- `resume <run-id>`
- `status <run-id>`: stages done, waiting on a human, ledger totals, requirements covered
- `coverage <run-id>`: the requirement-to-node table and any warnings
- `calls <run-id>`: the ledger as a table
- Existing: `validate`, `vault set|list`, `tree check`.

Exit codes: 0 done, 1 error, 2 invalid brief, 3 waiting on a human (file gate). Code 4 (an escalated leaf) belongs to the executor plan.

## 13. Error handling

- Invalid brief: exit 2 with the field named, before any model call.
- No eligible provider for a call (everything filtered by privacy or disabled): `*ChainExhausted` with reasons; the stage fails the run with a message naming the setting to change (`--allow-public`, a cloud key, or a config edit), not a generic error.
- Coverage gaps remain after `max_coverage_rounds`: the run stops in the Coverage stage listing each uncovered requirement (id, line in the brief, text) and the reason, and exits 1. Nothing is approved and nothing is written to the target repo.
- A stage's output fails validation after the retry and fallback described in section 5: the run stops in that stage with the last parse error; earlier stages' outputs stay, so `resume` restarts only that stage.
- Interrupt (SIGINT): the current call is cancelled, its ledger row records `error_kind: cancelled`, and the run stops resumable.

## 14. Testing

All in process, no network, table-driven, using temp directories.

- `provider`: the OpenAI-compatible client maps 429 (with `Retry-After`), 401 and 403, 400 with a context-length message, and 5xx or transport errors to the typed errors; a fake provider serves canned replies from a fixture directory.
- `router`: fake HTTP servers prove failover order, cooldown and its expiry, that a 401 disables a provider so a second call never reaches it, waiting when only cooldowns block, and exhaustion reasons.
- Privacy: a fake public provider counts its hits; `brief` and `component` scope calls must produce zero, `node` scope may reach it, `private_only` always zero, and `--allow-public` allows it.
- `ledger`: every attempt writes exactly one row, including failures; text never appears in the database (search the file for a canary string sent in a prompt); `Summary` matches a hand count.
- `blackboard`: 20 goroutines race `Claim` and exactly one wins; `SetStatus` rejects illegal transitions; `ReleaseStale` returns only stale rows.
- `human`: each adapter against the same table of scenarios; the file gate round trip (exit 3, fill the block, resume).
- Requirements parser (`planner`, table-driven): a brief with three constraint bullets (one with an indented sub-bullet), two acceptance bullets, and two features yields exactly `C1` to `C3`, `A1`, `A2`, `F1`, `F2` with verbatim text and correct line numbers; a bullet inside a fenced block is ignored; text before the first bullet of a section is ignored. `gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md` (already a fixture) yields exactly 10 constraints, 12 acceptance bullets, and 19 features, so a parser change that drops a bullet fails the test.
- Coverage checks (`planner`, table-driven): a requirement missing from the reply, a requirement whose only node does not exist, a constraint with neither node nor root test, an acceptance bullet with nodes but no root test, a feature mapped only to the root node or to a component with no functions each produce a gap naming the requirement and the reason; an unknown requirement id or a root test without a command is rejected as malformed.
- Coverage fill: the offline fixture's canned Coverage reply leaves one acceptance bullet without a root test and one constraint without a node; the planner must send `coverage_fill` listing both, accept the canned fill reply (a root test and a new contract function), decompose the new function, and reach Approve within the rounds allowed. A fixture whose fill reply is still incomplete stops in Coverage with the requirement named, and `resume` repeats only that stage with no call to an earlier stage.
- Golden failing plan: the 99 steps the v1 planner produced for AI Venture Studio on 2026-09-29 are kept as plan nodes (`planner/testdata/aivs/nodes.json`) beside a hand-reviewed mapping of which steps address which requirement (`mapping.json`), with no root tests because that plan had none. The checker must report as uncovered every acceptance bullet (among them the ownership `as_of`, SSE event order, financial round trip, and cross-company 404 bullets), the `company_id` SQL scan constraint, the 60-line handler constraint, and the `gofmt` and `go vet` constraint, must warn on brief paths with no file under them (`internal/tenancy` among them), and must warn on the stray `cmd/server` and `cmd/fake-llm`. This test exists so the failure that motivated the stage cannot return unnoticed.
- Approve gate: the rendered plan includes the coverage table, the root test commands, and the "covered" line; changing `coverage.json` after approval makes the stored hash stale and Test-writer refuses to run.
- Size: a fixture whose component reply sets `"more": true` makes a second `contract:<component>` call and merges both; a component with 9 functions makes two Decompose calls.
- Leaf checks (table-driven): a draft missing an input for a parameter, missing an output, returning `error` with no `errors` entry, or with an unknown `node_class` is rejected; a Test-writer reply with fewer tests than `errors + 1` is rejected.
- Ledger: every row carries `task_type`; leaf calls carry `node_class`; `Summary` groups by both.
- `planner`: an end-to-end run on the offline fixture with the Acme example brief produces a tree where every function node has a contract, a command test, a wave, and derived signatures; nothing is produced past Approve without `approval.json`; a `QUESTION:` reply pauses and resumes one call; killing the run between stages and resuming redoes only the unfinished stage.
- Manual, not in CI: `gophermind brief plan` on `04-csvstat.md` against the mini, to see whether a 35B mixture-of-experts model can write usable contracts.

## 15. Deviations from the handoff

| # | Handoff says | Here |
|---|---|---|
| P1 | Config `gophermind.yaml` beside a `[v2]` TOML question | YAML at `~/.gophermind/gophermind.yaml`, no TOML |
| P2 | Attempts logged for leaf builds only | A ledger row for every model call, including planning; the blackboard still holds leaf attempts as the interface defines |
| P3 | Provider list of free cloud providers with keys | Default list is the mini plus key-less providers; keys are supported but absent |
| P4 | Every outbound request goes through the harness proxy | Providers take an injected `*http.Client`. Until the proxy plan lands the planner uses a plain client. This plan makes model calls only, never runs commands, and the executor plan must not run commands before the proxy exists |
| P5 | One provider order per tier | Each entry also has a visibility, and calls carry a scope; the router enforces a need-to-know rule the handoff does not have |
| P6 | Human gate modes `interactive` and `file` | Also a programmatic adapter, for the run service and the app |
| P7 | Lifecycle: Clarify, Approve, Contract, Decompose | Approve after Decompose, because the plan it shows does not exist earlier (see section 9) |
| P8 | Provider errors: rate limited, context too long, auth, transient | Also `ErrModelNotFound`, after a real 404 on 2026-09-29 when the mini's model was removed mid-run |
| P9 | `Blackboard` and provider interfaces only | `Router.Call` returns a `Result` carrying the ledger row id; `Ledger.Record` takes a pointer and there is an `Amend`; the package that reads `gophermind.yaml` is named `settings` to avoid clashing with `gophermind-lib/config` |
| P11 | One Contract call and one Decompose call per component | Contract in an outline pass plus one pass per component with continuation; Decompose in batches of 8; no size ceiling |
| P12 | Ledger keyed by stage | Rows also carry `task_type` and `node_class`, and the summary groups by them, so models can be compared by kind of work |
| P10 | Lifecycle has no check that the plan covers the brief | A Coverage stage between Decompose and Approve: requirements parsed by code, mapping proposed by a model, checked by code, gaps refilled up to 2 rounds, never approved with a gap (section 9) |

## 16. Risks and open items

- **Model quality.** Whether `qwen3.6:35b-a3b` is strong enough to write consistent contracts is unknown until the csvstat smoke test. The chain design lets a stronger model be added later without code changes.
- **Speed.** Planning calls to the mini take minutes each (a 47-batch run of the current planner takes about two hours). Parallel decompose calls are deferred; Ollama's concurrency and the mini's memory bandwidth set the limit, and a faster key-based provider would help most.
- **Kilo Code's auto-router** may serve a different model on every call and may log prompts. That is why it is public and node-scope only by default.
- **OVHcloud** returned 429 to two anonymous requests from John's network on 2026-09-29 even after waiting out the documented window. It stays in the default list because the router skips a provider that keeps refusing; it should not be counted on.
- **Decision E5** (the optional `Toolchain` variables so commands can run `go test`) needs the handoff author's confirmation before the executor plan.
- **Coverage can be fooled by a plausible mapping.** Code checks that ids exist, that each requirement has a covering node, and that the covering test has a runnable command. It cannot check that the command tests the right thing. The Approve table puts each requirement beside its nodes so a person can look, and the manual csvstat smoke test should include reading that table.
- **Requirement granularity follows the brief's bullets.** A bullet that bundles three obligations counts as one requirement, so one weak covering test can satisfy all three. Briefs written with one obligation per bullet get the most from this stage; the brief-format documentation should say so.
- **Ledger hashes** of prompts allow confirming a guessed prompt. If that matters later, drop the hashes and keep sizes.
- The example `contracts.json` in the handoff lacks the `Server` and `CRM` types; the offline fixtures written for this plan are self-consistent instead.

## 17. What comes next

1. Written implementation plan for this document (packages in dependency order: `db`, `blackboard`, `ledger`, `provider`, `router`, `human`, `config`, `planner`, commands).
2. The executor plan (build items 9 to 13): wave scheduler, worker loop, test runner, proxy, git landing, resume.
3. The report and Gantt view (items 14 and 15).
4. The run service and the app screens.
