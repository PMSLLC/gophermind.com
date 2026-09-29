# Build plan

Ordered deliverables for the Claude Code instance. Each has a test that proves it is done. Build in
order; later items depend on earlier ones. `SPEC.md` is the design; this file is the work list plus the
mechanics the spec leaves to the implementer.

## 0. Ground rules

- Go 1.22+. Pure Go only. No cgo. One binary.
- Adapt to the existing repo layout. The layout below is a suggestion for new code.
- Every package has tests. Use table-driven tests and `httptest` and temp dirs; no test touches the
  network or a real provider.
- No secret value ever reaches a log line, a node file, a blackboard row, or a prompt. Add a test that
  greps for a canary secret after an end-to-end run.

Suggested layout:

```text
cmd/gophermind/          CLI entry
internal/brief/          parse + validate brief.md, secret-name scan
internal/vault/          age-encrypted secret store
internal/schema/         embedded JSON Schemas + validator
internal/tree/           node file store, tree walk, cycle check, wave assignment
internal/contract/       contracts.json load/slice, dependency_signatures derivation
internal/planner/        clarify, approve, contract, decompose, testwrite, revise
internal/prompt/         template loading + context packer + token estimate
internal/blackboard/     interface (copy from handoff) + sqlite backend
internal/provider/       interface (copy from handoff) + openai-compatible client
internal/router/         fallback chain, cooldowns, backoff
internal/executor/       wave scheduler, worker loop, test runner
internal/proxy/          forward proxy, allowlist, request log
internal/gitland/        go-git branch/commit/rebase, forge PR
internal/report/         per-model aggregation
internal/human/          interactive and file-mode gates
internal/view/           live view (last)
```

## 1. CLI and run directory

Commands:

```text
gophermind validate <brief.md>            parse + schema check, exit 0/1, no side effects
gophermind run <brief.md> [--yes]         full pipeline; --yes skips the approval gate (dev only)
gophermind resume <run-id>                continue an interrupted run
gophermind status <run-id>                print wave board as text
gophermind report <run-id>                print or write report.json
gophermind vault set <NAME>               prompt for a value, store encrypted
gophermind vault list                     names only
gophermind tree show <run-id>             dump the tree with runtime status merged
```

Exit codes: 0 done, 1 error, 2 invalid brief, 3 waiting on human (file mode), 4 escalated node needs a human.

Run directory `.gophermind/<run-id>/` inside the target repo (gitignored by the harness):

```text
brief.md            copy of the input at load time
answers.json        clarifying Q&A
approval.json       {"approved_at", "approved_by", "plan_hash"}
contracts.json      Wave 0 artifact
root.json
<component>/component.json
<component>/fn-*.json
<component>/fn-*.runtime.json   written only at run end (archival merge)
logs/run.log        structured (slog JSON)
logs/proxy.log      one line per request: ts node host method status bytes critical
report.json
QUESTIONS.md / APPROVAL.md / ESCALATION-<node>.md   file-mode gates
```

Test: `gophermind validate examples/brief.md` exits 0; the same brief with `spec_version` removed
exits 2 and prints the field name; `gophermind run` on a brief whose `language` is `python` exits 2.

## 2. Brief loader and vault

- Split frontmatter on the first `---` pair, parse as YAML, convert to JSON, validate against
  `schema/brief-frontmatter.schema.json`.
- Parse the body into sections keyed by H2 text; `## Features` is further split by H3. All seven
  required sections must exist and be non-empty.
- `env` block: parse into `Frontmatter.Env []EnvVar{Name, Purpose, Default}`. Reject the brief with
  exit 2 and message `ENV_SECRET_OVERLAP: <name>` if any name appears in both `secrets` and `env`.
- Secret-name scan over the body. A token is `\b[A-Z][A-Z0-9_]{2,}\b`. It is flagged only when it is
  not declared in `secrets` or `env` and either (a) ends in `_KEY`, `_SECRET`, `_TOKEN`, `_URL`,
  `_DSN`, `_PASSWORD`, or `_PASSPHRASE`, or (b) sits on a line that contains "secret", "credential",
  or "environment variable" (case-insensitive). Each hit is one warning with token and line number.
  Warnings never change the exit code. Never auto-adds. Test fixture: the AI Venture Studio server
  brief must produce at most 3 warnings.
- Environment passed to `exec.Cmd`: exactly the declared `env` names with their defaults (overridable
  by `[v2.env]` in harness config), plus the declared `secrets` names with values from the vault, plus
  `HTTP_PROXY`/`HTTPS_PROXY`/`GOPHERMIND_NODE`. Nothing else from the harness process environment
  leaks through. A `secrets` name never has a default and is never read from harness config.
- Vault: `filippo.io/age` with a scrypt passphrase recipient, file at `vault.path`. Two scopes in one
  file: `harness/*` (provider keys, set via `vault set`) and `run/<run-id>/*` (brief secrets, prompted
  at load). Prompt with terminal echo off. `GOPHERMIND_VAULT_PASSPHRASE` env var bypasses the prompt
  for unattended runs.
- Secrets reach commands as env vars on the `exec.Cmd` only. They are never placed in the process
  environment of the harness itself.

Test: example brief loads with all sections; a body containing `SENDGRID_KEY` not declared triggers
a warning while `JSONB`, `TOKEN_REUSED`, and `PUT` do not; a brief with `FOO` in both `secrets` and
`env` exits 2; after `vault set CANARY` with value `canary-9f8e7d`, an end-to-end run followed by
`grep -r canary-9f8e7d .gophermind/ logs/` finds nothing.

## 3. Schema, tree store, wave assignment

- Embed the three schemas with `embed.FS`. Validate every node on every write with
  `github.com/santhosh-tekuri/jsonschema/v6`.
- Node path: `root.json` for the root, `<component-id>/component.json` for components,
  `<component-id>/<fn-id>.json` for functions.
- Cycle check over `depends_on` with a DFS; reject the tree with the cycle path in the error.
- Wave assignment: `wave(n) = 0` if `depends_on` is empty and `kind != function`, otherwise
  `1 + max(wave(d) for d in depends_on)`. Components take the max wave of their children. `wave` is
  always recomputed on write; a hand-set value that disagrees is an error.
- Readiness: a function node becomes `ready` when every ID in `depends_on` is `verified`. A
  component becomes `ready` when every child is `verified`. Root becomes `ready` when every
  component is `verified`.

Test: every file under `examples/tree/` validates; a function node with no `command` in any test is
rejected; a two-node cycle is rejected with both IDs in the message; wave numbers in the examples are
reproduced exactly by the assigner.

## 4. Contract artifact and dependency slicing

`contracts.json` is the only source of `dependency_signatures`. The harness derives them; models
never write them.

Algorithm for a function node `n`:

1. Start with the set `S` = `n.depends_on`.
2. For each ID in `S`, look it up in `contracts.types` or `contracts.functions`. Add its `uses`.
   Repeat to closure.
3. Remove `n`'s own ID.
4. For each type in the closure, emit `decl`. For each function, emit `signature` prefixed by its
   `doc` as a comment. Order: types first (topologically by `uses`), then functions.
5. Deduplicate. Write to `context.dependency_signatures`.

Test: slicing `fn-register-handler` in the example yields the seven entries shown in its node file, in
that order; changing `validation-error.decl` in contracts.json and re-slicing changes every dependent's
signatures and marks them `needs_revision` on the blackboard.

## 5. Planner

Stages 2 to 5 of the run lifecycle in `SPEC.md`.

- **Clarify** with `prompts/01-clarify.md` on a `strong` model. Present questions through the human
  gate (item 8). Store answers in `answers.json`. Skip the gate if the array is empty.
- **Plan and approve**: render the proposed component list, function count per component, and wave
  count as Markdown; hash it; block on approval. `--yes` records `approved_by: "flag"`.
- **Contract (Wave 0)** with `prompts/02-contract.md`. Validate against `contract.schema.json`. Retry
  the same model once with the validation error appended; then fall through the `strong` chain.
- **Decompose** with `prompts/03-decompose.md` once per component. Validate each returned node with
  `tests` allowed to be absent at this point only.
- **Test writer** with `prompts/04-testwriter.md` once per function node. Write the test file to the
  repo immediately (before any implementation) and commit it in the Wave 0 commit. Add every
  `*_test.go` path to a run-level forbidden list the executor enforces.
- Pause-and-ask: if any decompose or testwriter response contains `QUESTION:` on its own line, route
  the remainder to the human gate, store the answer, and rerun that single call.
- Every node write goes through item 3's validator and item 4's slicer.

Test: from `examples/brief.md` with a fake provider that returns canned responses from a fixture
directory, the planner produces a tree where every function node has a contract, at least one
command test, a wave, and non-empty `dependency_signatures` when `depends_on` is non-empty; nothing
under `internal/executor` is invoked before `approval.json` exists.

## 6. Blackboard

Implement `interfaces/blackboard.go` exactly.

- Backend: `modernc.org/sqlite`, WAL mode, `busy_timeout=5000`. One table `rows` keyed by
  `(run_id, node_id)` with `status`, `revision`, `claim_worker`, `claim_at`, `heartbeat_at`,
  `attempts` (JSON), `result` (JSON), `wave`, `updated_at`. One table `events` for `Watch`.
- `Claim` is `UPDATE rows SET status='claimed', claim_worker=?, claim_at=?, heartbeat_at=?
  WHERE run_id=? AND node_id=? AND status='ready'`; return `RowsAffected() == 1`.
- `SetStatus` checks the transition table in the interface file and the current status in one
  statement.
- If JB's existing blackboard already satisfies the interface, wrap it instead and skip the SQLite
  work. Ask first.

Test: 20 goroutines racing `Claim` on one ready node produce exactly one `true`; `SetStatus(ready,
verified)` fails with `ErrBadTransition`; `ReleaseStale` returns a node whose heartbeat is 20 minutes
old and leaves one that is 1 minute old.

## 7. Provider, router, rate limits

- One `Provider` implementation for OpenAI-compatible chat completion endpoints covers Groq, Together,
  OpenRouter, Gemini's OpenAI-compatible endpoint, and Ollama. Add others only if a configured provider
  needs it.
- Map HTTP results to the typed errors in `interfaces/provider.go`: 429 to `ErrRateLimited` (honor
  `Retry-After`), 401/403 to `ErrAuth`, 400 with a context-length message to `ErrContextTooLong`,
  5xx and transport errors to `ErrTransient`.
- Router: for a node with tier `T`, iterate `models[T]` in order. Skip a provider in cooldown. On
  `ErrTransient` back off (initial, multiplier, max from config) and retry the same model up to 3
  times, then treat as `ErrRateLimited`. On `ErrRateLimited` put the provider in cooldown for
  `Retry-After` or `cooldown_after_429_seconds`, log `VerdictError`, move on. On `ErrContextTooLong`
  log and move on. On `ErrAuth` disable the provider for the run and warn once.
- Per-provider concurrency is a semaphore of `max_concurrent`.
- The chain is exhausted when every model has returned `VerdictFail` for this revision, or every
  model is unavailable (cooldown, auth, context). If exhaustion is due only to cooldowns, wait for
  the shortest cooldown and try again rather than going to revision; cap the total wait at 30 minutes
  per node, then go to `needs_revision` with `failure_reason: "all_providers_unavailable"`.

Test: a fake server returning 429 with `Retry-After: 2` puts the provider in cooldown and the router
takes the next model; after 2 seconds the provider is eligible again; a 401 disables the provider and
a second call never hits the server.

## 8. Human gates

`human.mode` in config.

- `interactive`: print to stderr, read from stdin. Questions are numbered; the default from
  `default_if_unanswered` is accepted on empty input. Approval is `yes`/`no`. Escalation shows the
  node, the attempt history, and offers `edit` (opens `$EDITOR` on the node file, then re-validates
  and sets `ready`), `skip` (sets `failed`), or `abort`.
- `file`: write `QUESTIONS.md`, `APPROVAL.md`, or `ESCALATION-<node>.md` into the run dir with a
  fenced answer block, exit 3 (or 4 for escalation). `gophermind resume` reads the file, validates
  the answer block, and continues. This is the mode for unattended and remote use.
- Milestone approvals (`milestone_approvals: true`): after every wave, the same approval flow with a
  summary of what was verified.

Test: in file mode, `run` exits 3 and writes `QUESTIONS.md`; after filling the answer block,
`resume` proceeds to approval and exits 3 again with `APPROVAL.md`; writing `approved: yes` and
resuming reaches Wave 0.

## 9. Context packer

Builds each prompt from the node file only.

- Token estimate: `len(bytes)/4` rounded up, plus 10 percent. Good enough for a hard cap; do not
  pull in a tokenizer dependency.
- Assembly order for `05-implement.md`: signature, contract, dependency_signatures, constraints,
  tests, previous failure (only the failed test names and the first 30 lines of `go test` output).
- If the packed prompt exceeds `budget.max_context_tokens` (node, else brief, else config), do not
  call any model; set `needs_revision` with `failure_reason: "context_too_long"`.
- Never include file contents from the repo. Never include another node's body.

Test: a node whose dependency_signatures are padded to 40 KB goes straight to `needs_revision`
without a provider call.

## 10. Executor

Worker loop per leaf:

```text
claim -> in_progress
for revision := current; ; {
  for each model in chain(tier) not skipped {
    prompt := pack(node, previousFailure)
    resp   := provider.Complete(prompt)          // typed errors handled by router
    if resp is CONTRACT_PROBLEM { log attempt VerdictFail reason "contract_problem: ..."; break to revise }
    write resp to node.contract.file (only that file; refuse any other path)
    gofmt the file; if gofmt fails: VerdictFail reason "gofmt: ..."
    run each distinct test command with -json, timeout test_timeout_seconds,
        env = declared env (defaults, config overrides) + secrets from vault + proxy vars, nothing else
    parse pass/fail per subtest; append Attempt
    if all pass { gitland.CommitLeaf(node); SetResult; in_progress -> verified; return }
    previousFailure = failed test names + trimmed output
  }
  if revision >= max_revisions { in_progress -> needs_revision -> escalated; return }
  in_progress -> needs_revision; planner.Revise(node, attempts)   // prompts/06-revise.md
  on SPLIT_REQUIRED: planner replaces node with children, marks new nodes ready, return
  on CONTRACT_CHANGE_REQUIRED: planner revises contracts.json, bumps revision, marks dependents; return
  needs_revision -> ready; revision++; re-claim
}
```

- Wave scheduler: for wave `w` in ascending order, mark eligible nodes `ready`, run up to
  `max_parallel_leaves` workers, wait for every node in the wave to be `verified` or terminal.
  A wave with an `escalated` node blocks at the end of the wave (other leaves finish) until the
  human resolves it.
- Test runner: `go test -json -run <name> ./pkg` with `-count=1`. Parse the event stream; a subtest
  passes when its `pass` action arrives. `tests_passed` counts subtests named in the node's `tests`.
- Heartbeat every 30 seconds while a worker holds a claim.
- Forbidden writes: any path not equal to `contract.file` and any `*_test.go` are refused before the
  file is written; the attempt is logged as `VerdictFail` with `forbidden_write:`.

Test: with a fake provider that fails twice then passes, the node ends `verified` with three
attempts; with a provider that always fails, the node reaches `needs_revision`, the fake reviser
rewrites it, and after `max_revisions` it ends `escalated`; a response that tries to write
`other.go` is rejected without touching disk.

## 11. Resume

`gophermind resume <run-id>`:

1. Load brief copy, contracts, tree, blackboard rows. Fail if `approval.json` is absent and mode is
   not file (nothing to resume).
2. `ReleaseStale(run, stale_claim_seconds)`. Log released IDs.
3. Recompute readiness from `verified` rows and set `ready` where due.
4. Continue the wave scheduler from the lowest wave with unfinished nodes.
5. Git: check out the work branch; if the working tree is dirty, stash it under `gm/resume-<ts>` and
   warn. Verified nodes are never re-run; their commits are trusted.

Test: start a run against a fake provider with an artificial 5 second delay per attempt, `kill -9`
the process mid-wave, `resume`, and the run completes with exactly one commit per leaf and no leaf
attempted twice for the same revision after the kill (allowing one duplicate attempt for the leaf that
was in flight).

## 12. Network proxy

- HTTP forward proxy (plain and CONNECT) on `proxy.listen`. Every outbound `http.Client` in the
  harness and every `exec.Cmd` environment gets `HTTP_PROXY`/`HTTPS_PROXY` pointing at it.
- Allowlist: brief `network` hosts plus every configured provider `base_url` host plus
  `proxy.golang.org` and `sum.golang.org` unless the brief overrides. Exact match or `*.suffix`.
- The active node ID is attached to each request via a header the proxy strips
  (`X-GopherMind-Node`); commands get it through `GOPHERMIND_NODE` and a tiny env hint in the
  proxy URL (`http://node-<id>@127.0.0.1:8480`).
- Denied request: 403 from the proxy, one line in `proxy.log` with `denied`.
- Failure semantics: a request to a `critical: true` host that fails (denied, timeout, 5xx) marks
  the current attempt `VerdictFail` with `failure_reason: "network_critical: <host>"`; three such in
  a row on one node set `failed` (terminal, not revision). A non-critical failure logs `warn` and
  continues.
- `log_bodies` is false and there is no code path that logs bodies; do not add one.

Test: with `example.org` absent from the allowlist, a command running `curl example.org` gets 403
and one `denied` line; a critical host served by a fake that returns 500 fails the attempt; a
non-critical 500 leaves the attempt's verdict to the tests.

## 13. Git landing

- On run start: open the repo with go-git, verify `base_branch` exists, create `work_branch` from it
  (or check it out on resume).
- Wave 0 commit: all `*_test.go` files, contracts.json is NOT committed (it stays in `.gophermind/`).
- Per verified leaf: stage only `result.files_changed`, commit with message
  `gm(<node-id>): <title>` and a trailer `GopherMind-Node: <id>`. Record the short hash.
- Parallel leaves each write different files by construction (one file per node, checked at
  decompose time; two nodes claiming the same `contract.file` is a validation error). So commits
  serialize through a mutex in `gitland`; no merges.
- `landing: diff_only`: no commits after Wave 0; at run end write `.gophermind/<id>/changes.patch`
  with `git diff base_branch`.
- `landing: pull_request`: push the work branch and call the forge API. Support GitHub first; detect
  from the origin URL; token from vault key `GITHUB_TOKEN`. Gitea is a follow-up.

Test: after two leaves verify, `git log --format=%s work_branch` shows the Wave 0 commit plus two
`gm(...)` commits, each touching exactly its `files_changed`.

## 14. Roll-up and report

- When every child of a component is `verified`, run the component's `tests` (integration). Pass sets
  the component `verified`; fail sets `needs_revision` on the component and the planner is asked
  which children to revise (route through `06-revise.md` with the component as the node).
- When every component is `verified`, run root `tests` (acceptance). Fail leaves root `failed` and
  exits 1 with the failing command in the message.
- `report.json`:

```json
{
  "run_id": "...", "started_at": "...", "finished_at": "...", "status": "verified|failed|escalated",
  "waves": 3, "nodes": {"total": 14, "verified": 14, "failed": 0, "escalated": 0},
  "models": [
    {"model": "groq/llama-3.3-70b-versatile", "attempts": 9, "pass": 7, "fail": 1, "error": 1,
     "pass_rate": 0.875, "first_try_wins": 5, "fallback_wins": 2, "avg_duration_ms": 5100,
     "failure_reasons": {"off_by_one": 1}}
  ],
  "revisions": 1, "escalations": 0
}
```

`pass_rate` excludes `error` verdicts from the denominator. `failure_reasons` groups by the text
before the first colon in `failure_reason`, lowercased.

Test: a fixture blackboard with a known attempt log produces a report whose per-model counts match a
hand count; a root acceptance failure leaves root `failed` and the exit code is 1.

## 15. Live view (last, optional for first run)

- `gophermind view <run-id>` serves a single HTML page on `127.0.0.1:8481` that polls
  `/api/state` every 2 seconds (or SSE from `Watch`).
- Layout per the mockup JB already has: wave board on the left, attempt log for the selected node on
  the right, exhausted-fallback path shown as a revision block.
- Reads the blackboard only. No writes.

Test: changing a node's status on the blackboard is reflected in `/api/state` within one poll.

## Order of work and checkpoints

Build 1 through 4 first and stop: run `gophermind validate` and the tree tools on the examples. Then
5 through 8 with a fixture-backed fake provider so the whole planner runs offline. Then 9 through 13
and do the first real end-to-end run against `examples/brief.md` in a scratch repo with one real free
provider. Then 14 and 15.

Report back at each checkpoint with what deviated from this plan and why.
