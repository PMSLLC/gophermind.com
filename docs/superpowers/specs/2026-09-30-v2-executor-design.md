# v2 Executor: Design

Status: draft for review. Date: 2026-09-30.
Scope: handoff build items 9 to 13 (context packer, executor, resume, network proxy, git landing), plus the parts of item 14 the executor must feed (the run report). Items 14 (component roll-up as a separate stage) and 15 (live view) are not built here.
Builds on: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (the planner, blackboard, ledger, router, human gates; the "planner spec" below) and `docs/superpowers/plans/2026-09-29-v2-planner-core.md`. Reference material: `docs/briefv2/handoff/` (SPEC.md, BUILD_PLAN.md items 9 to 14, `prompts/05-implement.md`, `prompts/06-revise.md`, `interfaces/`). Definition of a perfect release: `docs/GOAL.md` in the main checkout. Where this document and BUILD_PLAN.md differ, "Deviations" says so.

## 1. Why this exists

The planner produces an approved plan and writes nothing but tests. The executor turns that plan into working Go code in the target repo, proves it, and commits it. GOAL.md fixes the bar: one run, no restart, no error, no skipped or abandoned task, every acceptance bullet passing against the built server, GopherMind's own commit on `main`. The executor is where that bar is met or missed, so its rules are written to make a silent skip impossible: every leaf ends `verified` or in a named terminal state with a reason, and the run's exit code and report say which.

The model is the Mac mini's `qwen3.6:35b-a3b` (`reasoning_effort: none`, one request at a time, small effective context). It is slow, sometimes malformed (3 of 8 contract calls on the planner smoke test), and free. The design therefore spends model calls freely on cheap retries and spends nothing on anything code can decide.

## 2. Decisions taken earlier (carried over)

| # | Decision | Source |
|---|---|---|
| 1 | Mini's Ollama is the backbone; cloud keys optional; the GOAL run uses the mini only | planner spec 2, GOAL.md |
| 2 | Need-to-know: an implement call carries scope `node` only | planner spec 6 |
| 3 | Every model call gets a ledger row with `task_type` and `node_class`; prompt text is never stored, only hash and size | planner spec 7 |
| 4 | The blackboard is the runtime store; node files are plan, not state | planner spec 8 |
| 5 | Human gates are a Go interface; the terminal is one adapter | planner spec 10 |
| 6 | The executor must not run a command before the proxy exists (planner deviation P4) and needs the `Toolchain` env of decision E5 | planner spec 15, 16 |

## 3. Goals and non-goals

Goals:

- `executor.Run(ctx, Options)` takes an approved plan and runs every function leaf, wave by wave, to `verified`, or stops with every unfinished leaf named and a reason.
- A leaf is built only from its own slice of the plan (section 5) within a token budget, and the prompt is never persisted.
- A bounded, ordered escalation ladder (section 7) so that a malformed reply, a compile error, or a failing test costs a cheap retry and not the run.
- After every wave the whole repo builds, vets, and passes `-race` for what exists so far; integration failures are attributed to nodes by code, never guessed (section 8).
- The run ends with every acceptance bullet run against the built binary and the line `Acceptance passed: N of N`, N taken from the planner's requirements, not from the executor's own list (section 9).
- A killed run resumes from the blackboard and repeats no model call for a finished leaf (section 10).
- Model output can only ever change the one file its node names (section 6).
- Commits are made by GopherMind only, on a work branch, ending as a fast-forward of `main`; no force, no push (section 12).
- The report lists calls, malformed replies, retries, escalations and tokens per task type and model, and repeats `Requirements covered: N of N` (section 13).
- Everything runs offline in tests against a scripted fake model.

Non-goals: see "Not in scope" (section 17).

## 4. Architecture

New packages under `gophermind-lib/briefv2/`. Dependencies point downward only.

```text
executor    Run(ctx, Options): wave scheduler, leaf loop, wave checks, repair, acceptance, resume, landing calls
packer      builds one leaf prompt from one node; token estimate; overlay of revision notes
sandbox     darwin sandbox-exec profile generator and wrapper; refuses to run unsandboxed unless settings say so
runner      runs commands: go build/vet/test -json, gofmt, sh -c for acceptance; env, timeouts, process groups, output caps, failure classes
gitland     git work branch, wave 0 and per-leaf commits, final commit, fast-forward to base
proxy       forward proxy with allowlist and per-node request log; deps step helper
report      aggregation of ledger rows and blackboard attempts into report.json and the printed summary
pathsafe    the planner's path-safety rules (safeTestPath, insideRepo, no-follow open), moved out of planner
```

Reused unchanged: `brief`, `contract`, `tree`, `schema`, `rundir`, `vault`, `execenv`, `db`, `blackboard`, `ledger`, `events`, `provider`, `router`, `human`, `settings` (one new section, section 11). `planner` changes are limited to the four items in section 15 (dependencies file, its approval hash, its display, and importing `pathsafe`).

```go
// executor.Run is the only entry point the CLI and, later, /project use.
func Run(ctx context.Context, o Options) (Report, error)

type Options struct {
    RunDir   string             // <repo>/.gophermind/<brief-id>; holds brief.md, contracts.json, tree/, approval.json, coverage.json
    Repo     string             // target repo root; overrides the brief's repo field (GOAL uses ~/OtherProjects/AIVentureStudio)
    Caller   Caller             // *router.Router; same shape as planner.Caller
    Board    blackboard.Blackboard
    Ledger   ledger.Ledger
    Gate     human.Gate         // Escalate is used here; nil means every escalation stops the run
    Sink     events.Sink        // nil means events.Nop
    Settings *settings.Config
    Secrets  execenv.SecretSource // nil when the brief declares none
    Git      gitland.Repo       // nil means the git CLI implementation
    Proxy    *proxy.Proxy       // nil means the executor starts its own on a loopback port
    Now      func() time.Time
}
```

`Report` is the value written to `report.json` (section 13). `Run` returns a non-nil error only for harness faults (cannot open the blackboard, approval mismatch, git unusable); a failed build is a `Report` with `Status != "verified"` and a nil error, so callers cannot mistake an unfinished run for a fault or a success.

## 5. The context packer (item 9)

`packer.Pack(n NodeView, c *contract.Contracts, prevFailure string, notes []string, budget int) (Packed, error)`.

The prompt is the handoff's `05-implement.md` with these sections, in this order: file and package, the exact signature, the contract slice (`contract.Slice(dependsOn, self)`: this function's entry plus the types it uses), dependency signatures (`context.dependency_signatures`, harness-derived by the planner), constraints (`context.constraints`), revision notes (only when a revise call produced some), the leaf's test file, and the previous failure. Nothing else is ever added.

Rules:

- **Need-to-know.** No other node's description, tests, notes, or body appears. No repo file contents appear, with one deliberate exception (ruling R3): the leaf's own test file, which the harness itself wrote, so the model sees what it must satisfy.
- **Budget.** `budget = node.budget.max_context_tokens`, else the brief's, else `settings.Defaults.MaxContextTokens` (8000). The estimate is `ceil(len(bytes)/4) * 1.1`. Over budget, sections are dropped in a fixed order until the prompt fits: (1) the previous failure, down to a floor of 10 lines and then to its test names only, (2) revision notes, (3) the test file's comments and blank lines, (4) dependency signatures beyond those the contract slice's `uses` names. The signature, contract slice, constraints and the test's assertions are never dropped and a leaf is never split by the executor. The floor of required sections must fit in `max_context_tokens`, and the planner already keeps contracts small (one function per leaf, batches of 8 per decompose call); if the floor alone is over budget, that is a plan defect. In that case, and when a model returns `ErrContextTooLong`, the entry is recorded `too_long` (router) and the next chain entry, which may have a larger context, is tried. `context_too_long` therefore never kills a run by itself: only when every entry of the tier's chain (and then of the `strong` chain) has failed for size does the node go `needs_revision` and escalate through the gate with that reason; the run stops only if the human answers `stop` or nobody answers.
- **Previous failure** is the failed test names plus the first 30 lines of runner output, cut to 2 KB, with every line that contains a secret value removed (the vault's values for the run are checked by exact substring). It lives in memory only.
- **Max output.** `MaxTokens = min(4096, model.context_tokens - promptTokens)`; below 512 the model entry is skipped as `too_long`, which the router already records.
- **The prompt is never stored.** `Packed` carries `Text`, `Bytes`, `Tokens`, `SHA256`; only the last three ever leave the function, into the ledger row the router writes. Sibling canaries, repo canaries and secret canaries are grepped for in tests.
- **Reply contract.** The model returns the whole file, or the single line `CONTRACT_PROBLEM: <one sentence>`. Fences are stripped. Anything else is `malformed` and takes the router's retry with the parse error appended (cheap repair, section 7 rung 1).

## 6. What model output may touch

The model never names a path. The only file the executor writes for a leaf is `node.contract.file`, so "write outside the repo root" cannot be expressed. Guards, all in code and all tested:

1. `pathsafe.Resolve(repo, contract.file)`: relative, cleaned, no `..`, no absolute path, no component that is a symlink, the parent directory inside the repo, not under `.git/` or `.gophermind/`, name matching `^[A-Za-z0-9_./-]+\.go$`, not `*_test.go`. Write is temp file plus rename in the same directory, with the no-follow open the planner already uses.
2. Parse check with `go/parser`: one file, `package` equals the contract's package, the declared function's signature equals the contract's signature after whitespace normalization, no `_test` package, no cgo `import "C"`.
3. A reply containing a `// FILE:` header, a diff header, or more than one top-level fence is `forbidden_write`, recorded as a failed attempt and never written. (A reply with a process-control call, a forbidden directive or cgo is not of this class: the reply gate classes it `malformed`, see section 14.)
4. Import policy (ruling R8): the reply may import only the standard library minus `os/exec`, `syscall`, `unsafe`, `plugin`, `debug/*`, `runtime/cgo`; the module's own packages; and modules listed in `dependencies.json`. Anything else is `import_not_allowed`, the failure text lists the allowed modules, and the reply is not written.
5. `go/format` formats the file in place; a formatting failure is `malformed` (the file did not parse).
6. Stubs and type declarations (section 8.1) are the only other files the harness writes, by its own code, never from model output, each through `pathsafe`.
7. After every attempt the executor compares `git status --porcelain` of the repo (plus untracked files) with the snapshot taken before it. A path that is neither the node's file, its stub, nor an ignored run path fails the attempt as `forbidden_write` and is reverted by the harness (`git checkout -- <path>` or removal of the new file), and a stray path found after a wave check fails the wave.

Model-written code runs in the test process, on the machine that runs GopherMind, so containment is not optional: every child process runs in the sandbox of section 6.1.

### 6.1 The sandbox (ruling R6)

Package `sandbox` generates a `sandbox-exec` profile per run and wraps every `go build`, `go vet`, `go test`, `go mod` (except as below), acceptance command and built-binary process (`sandbox.Wrap(cmd, Profile)`).

- **Writes** are denied everywhere except: the repo directory, the run scratch directory (`HOME`, `TMPDIR`), `GOCACHE`, `GOMODCACHE`, and `/dev/null`, `/dev/tty` style devices. The run folder `.gophermind/<id>` is inside the repo and is denied to model-written processes by a more specific deny rule for that subpath (state, ledger inputs and `approval.json` cannot be edited by tests).
- **Reads** of `$HOME` are denied except the paths above and the Go toolchain root; system paths needed by Go and macOS (`/usr`, `/System`, `/Library`, `/private/var/db`, `/dev`) stay readable. Reads of `~/.gophermind`, `~/.ssh`, `~/.aws` and the rest of `$HOME` are therefore denied.
- **Network** is denied except loopback ports that are listed (`Profile.LoopbackPorts`, plus the contiguous `Profile.LoopbackRange`) and, only for the single deps step (section 14), outbound to the module proxy host through the harness proxy. A child cannot reach Ollama (11434), Postgres (5432) or any other local service unless its port is listed. The runner adds, per command, every loopback port the command's own environment names (`127.0.0.1:N`, `localhost:N` or `[::1]:N`: the harness proxy URL, `GM_ACCEPTANCE_ADDR` and `GM_ACCEPTANCE_URL`, and a URL-valued secret such as Postgres on `127.0.0.1:55432` through the user's tunnel), plus `runner.Spec.LoopbackPorts` for a port the environment does not name. A child that starts its own server on a random port and dials it needs `LoopbackRange` or `LoopbackPorts`, because SBPL cannot express "any port" together with "not these". Binding and inbound loopback stay open to every port. SBPL has no port ranges and the compiler's time grows faster than the port count (about 0.13 s per command at 1024 ports, 0.5 s at 4096, 30 s at 16384), so a range is expanded into single ports and capped at 4096; keep it far below that.
- **Escape routes closed (hardening wave).** `(allow default)` left these open, each verified with the real `sandbox-exec`, and each is now denied: Launch Services and Apple events (`mach-lookup` of `com.apple.coreservices*`, `com.apple.lsd*`, `com.apple.coreservices.appleevents`: `open -a Terminal x.command` started an unsandboxed process with parent pid 1), the pasteboard (`com.apple.pasteboard.1`), the security daemons (`com.apple.SecurityServer`, `com.apple.securityd`, `com.apple.security.agent`), signals to any process outside the child's own process group (`(deny signal)` then `(allow signal (target self) (target pgrp) (target children))`; timeout kills by the harness are unaffected), and exec of `/usr/bin/open`, `/usr/bin/osascript`, `/usr/bin/security`, `/bin/launchctl`, `/usr/bin/sudo`, `/usr/bin/ssh`, `/usr/bin/scp`, `/usr/bin/nc`, `/usr/bin/su` and `/usr/bin/login`. `curl`, `go` and `git` stay executable (acceptance needs curl).
- **Secret files in the repo** cannot be read: `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*` and `<repo>/.git/config` (remote URLs can carry tokens). These denies come after the reads allow so they win. A consequence: `git` run inside the sandbox fails with "unable to access .git/config"; the harness runs its own git outside the sandbox. Writes under `<repo>/.gophermind` are denied except a scratch or cache directory placed inside it; the run folder stays denied after that exception.
- **Descendant sweep (setsid).** The runner kills the process group, which a child that calls `setsid` escapes. Before a timeout or cancellation kill, and while a command runs, the runner records every descendant by ancestry (`ps -ax -o pid=,ppid=,lstart=`), SIGKILLs the ones outside the group first, kills the group, and after every run kills each recorded process that is still the same process (pid and start time) and verifies none remain. A survivor becomes a `Warnings` entry on the run result. Limit: a descendant that daemonizes and whose parent exits between two samples (every 500 ms for 5 s, then every 2 s; a command that ends before the first sample costs no ps call) is not recorded.
- **Other OSes.** The executor refuses to start unless `executor.sandbox: off` is set explicitly in settings; that value is copied into the report (`sandbox: "off"`) and printed at start and end. On darwin `sandbox: on` is the default and `off` is equally recorded.
- **`sandbox-exec` missing or a profile that fails to load** is a preflight failure before any model call.
- **After every `go get`** and before every wave, the harness scans `go.mod` and all repo `.go` files and rejects: a `replace` directive, a `toolchain` directive, `//go:generate`, `import "C"` (cgo), and `-toolexec` or `-overlay` anywhere in `GOFLAGS`. A hit fails the run `failed` if it is in `go.mod`. A hit in a leaf's source file that is already on disk (adoption of a file a previous run left, and the stray check) is `forbidden_write`. A model reply that contains `//go:generate`, `//go:linkname`, `//go:embed`, `import "C"` or a call to an exit function is refused by the reply gate before the scan and is classed `malformed` (the router's one repair applies; the reply is never written or checked and the stub stays in place). The scan over a reply's source remains as a second line behind the gate.
- **Honest limits.** A child can still exec any other binary it can read and use any mach service the profile does not name; the deny list is a list of the escapes found so far, not a proof. `sandbox-exec` is deprecated but present on current macOS and is the only unprivileged option; a kernel escape is out of scope. The proxy still governs HTTP only (section 14).
- **Model calls and the state database (hardening wave).** A timed-out chain entry sits out `rate_limits.cooldown_after_timeout_seconds` (default 60) so the walk goes on to the next entry and a dead provider costs one `call_timeout` once; a provider may set `call_timeout_seconds` (at least 1) to override `defaults.call_timeout`, and a new config gives the private mini 1500 (25 minutes). Provider HTTP clients never follow a redirect and ignore the proxy environment. `settings.Validate` refuses a private provider whose `base_url` is not a private-network host. The blackboard database and its `-wal` and `-shm` files are created with mode 0600.

## 7. The leaf loop and the escalation ladder (item 10)

### 7.1 States

Blackboard statuses are the handoff's. The executor uses them as follows and adds no new ones.

| Situation | Status | Terminal for the run? |
|---|---|---|
| Tests pass, build and vet pass, commit made | `verified` | yes |
| Ladder exhausted after `max_revisions` | `escalated`, then the gate decides | see 7.4 |
| Network critical host failed 3 attempts in a row | `failed` | yes, stops the run |
| A human chose `skip` | `failed` | yes, run status `failed` |
| A dependency is not `verified` | stays `pending`, reported `blocked` | run status `failed` or `escalated` |

A leaf is **done** only when, in one attempt: the reply parsed and passed section 6; the file is gofmt clean; `go build ./<pkg>` and `go vet ./<pkg>` pass; `go test ./<pkg> -run ^TestX$ -count=1 -json` shows the top-level `TestX` pass, no failing or panicking event, and at least one test event (zero events is `no_tests_ran`, a failure, not a pass); and the attempt used no `network_critical` host. Only then is the file's commit made (section 12) and the row set `verified` with `SetResult`.

A leaf is **blocked** when a `depends_on` node is not `verified` when its wave is reached. It is never claimed. The executor emits one `blocked` event per such node naming the dependency that blocks it, and lists it in the report. `blocked` is a report label, not a blackboard status (the row stays `pending`). Blocked is not done and not skipped: the run's status cannot be `verified` while any leaf is blocked.

The **run stops** in exactly these cases, each with a status, an exit code and a report: all leaves and acceptance verified (`verified`, 0); an acceptance bullet still failing after its repair rounds (`failed`, 1); a leaf `failed` or escalated and not resolved (`escalated` for an unresolved escalation, exit 4; `failed` otherwise, exit 1); an unattributable integration failure after a wave (`failed`, 1); landing blocked (`failed`, 1); a human `stop` (`escalated`, 4); the context cancelled or `executor.max_run_minutes` (default 720) elapsed (`interrupted`, 5, resumable). At the limit no new claim is made, in-flight calls finish or are cancelled with `error_kind: cancelled`, claims are released, the report is written with `status: "interrupted"` and `stop_reason: "max_run_minutes"`, and the exit is 5; `brief run` picks up where it stopped and the limit restarts from zero for that invocation. Nothing else ends a run and there is no code path that returns success with a leaf unaccounted for; a test walks the tree and asserts every leaf is `verified` when status is `verified`.

### 7.2 One attempt

```text
claim (ready -> claimed -> in_progress), heartbeat every 30s while held
if a previous run left this file on disk: run the leaf's checks first; if they pass, skip to "done" with no model call
for revision r = row.Revision .. max_revisions:
  for rung in ladder(r):                       // 7.3
    packed := packer.Pack(node, prevFailure, notes)
    res, err := router.CallParsed(info{Stage: "implement:"+id, Tier: rung.Tier, Only/Exclude: rung.Entries, ...}, req, parseReply)
    err is ChainExhausted, all entries privacy-skipped or cooling: see 7.5
    CONTRACT_PROBLEM reply: append attempt VerdictFail "contract_problem", go to the revise call (7.3 rung 6)
    write file (section 6); run checks (runner); classify
    AppendAttempt(...); pass -> commit, verified, return
    prevFailure = failed names + trimmed output (memory only)
```

Failure classes, which become the `failure_reason` prefix (the text before the first colon, so the report groups them) and the verdict:

| Class | Verdict | Meaning |
|---|---|---|
| `malformed` | error | reply did not parse or violated section 6 (router retry handles it) |
| `rate_limited`, `timeout`, `context_too_long` | error | provider level, from the router, does not consume a fix attempt |
| `build` | fail | `go build` failed |
| `vet` | fail | `go vet` failed |
| `test_fail` | fail | a test failed; names recorded |
| `test_panic` | fail | a test panicked, usually the stub or a nil deref |
| `test_timeout` | fail | exceeded `test_timeout_seconds` |
| `no_tests_ran` | fail | the run matched no test |
| `import_not_allowed` | fail | section 6 rule 4 |
| `forbidden_write` | fail | section 6 rule 3 |
| `contract_problem` | fail | the model called the contract impossible |
| `network_critical` | fail | a critical host failed during the attempt |

`failure_reason` in the blackboard holds the class and, for tests, the failed test names (`test_fail: TestX/case_a, TestX/case_b`). It never holds compiler or test output and never any reply text. Runner output exists only in memory, for the next prompt. A test greps the database, logs and events for a canary planted in a reply and in test output.

### 7.3 The ladder

For a leaf of tier `T` (node's `model_tier`, default `standard`), with chain `C(T)` from settings, per revision:

1. **Repair (router).** A reply that fails `parseReply` is retried on the same model once with the parse error appended (`CallParsed`, already built and cheap).
2. **Fix.** Same model, same packed prompt plus the previous failure. Up to `executor.fix_attempts` (default 2) per chain entry. If the reply is byte-identical (same SHA-256) to the previous one, the entry is abandoned at once, and a fix retry after a first fix failure runs at temperature 0.3 instead of 0, so a loop cannot repeat (`provider.Request.Temperature` carries it). The reply SHA-256 is stored on each blackboard attempt as a new `ReplySHA256` field (hash only, ruling R17), so the identical-reply check survives a resume.
3. **Next model.** The next entry of `C(T)` (router `Exclude`), with the same previous failure.
4. **Tier bump.** `standard` to `strong` entries not yet tried. With the GOAL configuration both tiers resolve to the mini alone, so rungs 3 and 4 are empty and the ladder falls to rung 5 without a wasted call; entries already tried are never tried twice in one revision.
5. **Revise.** One call, `revise:<node>`, task type `revise`, tier `strong`, scope `node`. It receives the node's contract slice, test file, and the attempt history as classes and test names (never reply text or raw output), and returns JSON `{"notes": [up to 5 short hints]}` or `CONTRACT_PROBLEM`. Hints go to `_state/notes.json` (overlay keyed by node id; node files and `contracts.json` are never modified during a run, checked by hashing them at start and end). The revision counter increments (`SetRevision`) and the ladder restarts at rung 2 with the notes in the prompt.
6. **Contract problem.** A `CONTRACT_PROBLEM` from implement or revise goes to rung 5 once per revision; a second one in the same revision escalates at once. The executor never edits a contract or a signature (rule 3 of the decomposer rules). Splitting a leaf (`SPLIT_REQUIRED`) and contract change (`CONTRACT_CHANGE_REQUIRED`) belong to the planner's revise stage, which does not exist yet; see section 17.
7. **Escalate.** After `max_revisions` (default 2, node `budget.max_revisions` overrides) revisions, or at once for a repeated contract problem or when every chain entry failed for size: `in_progress -> needs_revision -> escalated`, `Gate.Escalate` with the node id, reason and one line per attempt (class and test names).

The bound on model calls for one leaf is `entries * (1 + fix_attempts) * (max_revisions + 1) + max_revisions` plus at most one router repair per call. With the GOAL configuration (one entry, 2 fixes, 2 revisions) that is 11 implement calls plus 2 revise calls, the worst case, each one recorded.

### 7.4 What a human can do

`Gate.Escalate` returns `retry <note>`, `skip` or `stop`. `retry` writes the note into the notes overlay, moves `escalated -> ready`, raises the revision allowance by one, and re-runs the ladder from rung 2. `skip` sets `failed`; the run continues with the other leaves, dependents are `blocked`, and the final status is `failed` with the leaf named. `stop` ends the run as `escalated`. A nil gate, or a programmatic gate with no answer, is `stop`. Every human escalation is recorded with who answered it (`answered_by`: `human` for the terminal and file gates, `programmatic`, `unattended-default` for an auto-answering gate, `gate-absent` for a nil gate) in `_state/escalations.json` and in the report's `human_escalation_log` (report schema version 2); a resumed run never records the same escalation twice. `/project` in the GOAL run uses the programmatic gate configured to `stop`, so an escalation there ends the attempt honestly (GOAL.md counts it as a failed attempt, and the report says why).

**Errors that write no report.** A resume that finds the plan files changed (`plan files changed since the run started`) or a claim held by a live worker (`node <id> is held by a live worker`) returns `Report{}` and an error and writes no `report.json`; nothing is modified. The CLI (Task 15) maps both errors to exit 7, the harness-fault code. The live-worker refusal emits one `resume_blocked` event (ids and the number of seconds to wait, at most `stale_claim_seconds`) so a person who SIGKILLed a run knows to retry after that long. A repair that was cut off is recognised by the marker `_state/reopen-<id>` (leaf id, round, revision), written before the first candidate reaches the disk: with the marker and a dirty committed file the resume re-enters the leaf as reopened, so a pass makes the repair commit and a failure restores the committed file from git. The resume also refuses (`repo_moved`, counts only) when any commit after the recorded tip is not this run's, and (`base_moved`) when the base branch is no longer an ancestor of the work branch.

### 7.5 Nothing to call

If the router returns `*ChainExhausted` because every entry is cooling down, it has already waited up to `max_wait_minutes`. If it is still exhausted: for cooldown or provider errors the leaf is released to `ready` and the run stops as `interrupted` (exit 5, resumable, no attempt charged); if the cause is only privacy (`OnlyPrivacy()`), the run stops `failed` naming the setting to change, before any leaf is claimed, by a preflight that resolves each tier's chain once.

## 8. Waves, integration and repair (item 10 continued)

### 8.1 Types, stubs and the file swap

Go compiles every `_test.go` file of a package together, so the tests of a later leaf break the tests of an earlier leaf in the same package while its function does not exist. Leaves also use shared types. At run start, after the approval check, the harness writes by its own code, each through `pathsafe`:

- **Type declarations.** For each `contracts.json` Type `{File, Decl, Uses}`, the file `File` gets a package clause (the Type's `Package`), imports derived from `Uses` (the standard library or module packages that the declaration and its used types reference, found by `go/parser` and resolved against the contract's types and the standard library), then `Decl`. Types sharing a `File` go into one file in contract order. A declaration that does not parse, or an import it needs that is not allowed (section 6 rule 4), stops the run `failed` before any model call.
- **Stubs.** One per leaf: `<dir>/zz_gm_stub_<node-id>.go` holding the contract signature with a body that panics (`panic("gm: not implemented")`), and zero-value returns after it for the compiler.

Types, stubs, the test files, `go.mod` and `go.sum` form the Wave 0 commit (section 12), made after the deps step (section 14). Every commit therefore builds.

**The swap, exactly.** A leaf's real file (`contract.file`) and its stub (`zz_gm_stub_<id>.go`) declare the same function, so they can never coexist. For each attempt:

1. Snapshot the repo's `git status` (section 6 rule 7).
2. Delete the stub (`os.Remove` through `pathsafe`) and write the real file atomically. Nothing else changes.
3. Run the checks (7.2).
4. On failure, whatever the class: delete the real file and rewrite the stub from the contract, so the tree is exactly the Wave 0 state for this leaf and other leaves' checks never see a half-written file. The next attempt starts again at step 2.
5. On pass: keep the real file, leave the stub deleted, and commit both changes together: `CommitLeaf(add: [contract.file], remove: [stub])`. A leaf is committed only on pass.
6. After a repair reopen (8.3) the stub is not restored; the real file stays, and a failed repair attempt restores the leaf's last verified content from git (`git checkout -- <file>`).

On resume, a leaf with both files or neither is normalized to "stub only" before step 1, unless the real file alone exists, in which case section 10 step 5 checks it first. The **red check** (once per leaf, before its first model call, no model needed) runs the leaf's tests against its stub; a test that passes against a panicking stub tests nothing. It is recorded as a `weak_test` warning event and counted in the report, accepted as a warning and never blocking, because the executor cannot rewrite tests (ruling R9).

### 8.2 Scheduler

For wave `w` ascending: mark eligible `pending` leaves `ready` (all `depends_on` `verified`), run leaves in id order, one at a time (`executor.workers` must be 1, see the ruling below), wait until every leaf of the wave is `verified` or terminal. Order is deterministic so a scripted fake model can be written against it. **Ruling (workers):** leaf model loops are serialised. Every leaf works in the one repository tree, and the stray-file check (a `git status` snapshot around each attempt) and the stub swap both look at that whole tree, so two leaves at once would blame each other's files and swap each other's stubs; the spec also allows one model call at a time. `executor.workers` and `--workers` therefore accept only 1 (`executor.workers must be 1 until leaf-isolated trees exist`), and the scheduler holds a lock around each leaf body. Cost: no overlap of the non-model steps (build, vet, test, commit) of one leaf with the model call of another, so a wave takes the sum of its leaves, not the longest one. Lifting it needs one worktree per leaf. Wave 0 (contracts) has no leaf work; the executor starts at the lowest wave holding a leaf.

### 8.3 After each wave

Run by the harness, no model, in this order, each in the runner with the stripped env:

1. `go build ./...` and `go vet ./...`. Failure output is parsed into `file:line` records.
2. For each package holding at least one verified leaf, one run `go test -race -count=1 -json ./<pkg> -run '^(TestA|TestB)$'` where the regex lists only that package's verified leaves' test functions (later leaves in the package still hold stubs, so an unrestricted run would fail on them). Runs are per package, so a failure names its package and one package's regex never hides another's.
3. After the last wave, the same with no `-run` restriction: `go test -race ./...` on the whole repo.

**Attribution** (code, never a model): a build or vet record maps by file path to the node whose `contract.file` or generated test or stub path it is; a test failure maps by package plus test function name (`testFuncName` is the planner's) to the node; a record in a file no node owns (`go.mod`, `cmd/...` main packages from a wiring node, generated files) maps to the node that owns that path if any, else it is **unattributable**. An unattributable failure stops the run `failed` with the failing lines' locations in the report (ruling R10: guessing which leaf to blame could burn the repair budget on the wrong leaf).

**Repair loop**: attributed nodes go `verified -> needs_revision -> ready` (revision bumps, this uses the allowed transition for a reopened node), their stubs are not restored, and each gets one more ladder pass with the integration failure lines that mention its own files or tests as its previous failure (need-to-know: no other node's output). Then step 8.3 repeats for the wave. The bound is `executor.repair_rounds` (default 2) per wave. Still failing: the attributed nodes are escalated through `Gate.Escalate` exactly as in 7.4. Every reopen is an event and a blackboard status change, visible in the report.

## 9. Acceptance (item 14 subset)

**Mapping.** The planner already required a root test for every acceptance requirement and for every command-checked constraint (`coverage.json`, `root_tests[]`: `requirement`, `name`, `command`), and it stops before approval if one is missing. The executor does not trust that file for the count. It reads `requirements.json` (parsed from the brief by planner code) and computes, itself:

- `A` = the acceptance requirement ids (for AI Venture Studio, 12).
- For each `A_i`, the root tests whose `requirement` is `A_i`. An `A_i` with none, or with a test whose command is empty, is a failure of the run before anything is started (cannot happen after a valid approval, so the check is a tripwire).
- The same for command-checked constraints (`C_j` with a root test), reported as `Constraints checked: M of M`.

**Running.** After the last wave and its checks, the executor builds every main package under `cmd/` to `<run>/bin/<name>` with `go build -o`, then runs each root test command with `sh -c`, from the repo root, with: the `execenv` environment (declared env, declared secrets from the vault, proxy variables, `NO_PROXY=127.0.0.1,localhost,::1` so a server on loopback is reached directly), `PATH` prefixed by `<run>/bin` so a command such as `venture-server serve` finds the binary the executor just built (never one installed on the machine), its own process group, `acceptance_timeout_seconds` (default 300) and the output cap. When the command ends, timeout or not, the whole process group is killed, so a server started by the command cannot outlive it. Commands run one at a time, in requirement order, so ports are not contended.

**Proof of N of N.** Written to `acceptance.json`. The **proof** of a bullet is the recorded triple of its command, its exit code and the SHA-256 (and size) of its captured output, plus the counts N of N; no output text is stored. Contents: for each requirement id, each command's exit code, duration, output size and SHA-256, and a verdict. `Acceptance passed: N of N` is printed only when every `A_i` has at least one root test and every one of its tests exited 0. The count is over `requirements.json`, so an acceptance bullet that lost its test cannot make N smaller. The same file records the coverage line copied from the planner: `Requirements covered: N of N`.

**Failure.** A failing acceptance bullet is attributed with the planner's own mapping: the nodes listed for that requirement in `coverage.json` (`covered[].nodes`) are reopened as in 8.3 with the first 30 lines of that command's output (secret lines removed) as their previous failure, bounded by `executor.acceptance_repair_rounds` (default 2), followed by the wave checks and the full acceptance run again. A failing bullet with no mapped nodes ends the run `failed` at once with `stop_reason: acceptance_unmapped` and the bullet ids (no repair is tried and no leaf is guessed); one still failing after the rounds ends it `failed` (exit 1) with the bullet's id, text, command and exit code in the message. Per GOAL.md that kills the attempt; the executor's job is to have tried and to say exactly what failed.

**Acceptance quality, Go acceptance tests and the red check (acceptance quality wave).** A rehearsal on the real AI Venture Studio brief showed model-written root tests that always exit 0 (`grep ... || echo "No ORM found"`, `curl -s` with no assertion, `jq` without `-e`, `{id}` placeholders, a trailing `&& echo 'E2E OK'`), so `Acceptance passed: 12 of 12` would have proven nothing. The rules below close that. Rulings are in the report of the wave and in the README section "Acceptance root tests".

- **One checker.** The shell-aware command checker moved to `briefv2/acceptcheck`, imported by the planner and the executor (the planner does not import the executor). `Vacuous` (this section's refusal rules, unchanged) stays the executor's level. `Quality` is the planner's level: fixed-vocabulary findings `masked_failure`, `success_echo`, `placeholder`, `undefined_variable`, `curl_unasserted`, `jq_unasserted`, `print_only`, never quoted text. The executor does not run the quality level, so an approved plan is never refused for style at run time; the planner runs it in Coverage (bounded fill rounds, then a fixed stop before approval naming at most 10 requirement ids and finding names) and again in `approve`, and `APPROVAL.md` shows a `Quality` column.
- **Go acceptance tests.** A narrative acceptance bullet (no command in backticks) in a plan that declares `serve` (`{command, ready}` in `coverage.json`, covered by the approval hash) has its root test command fixed by the planner to `go test -tags acceptance ./acceptance -run '^TestA<N>$' -count=1 -v`. After approval the Test-writer writes `acceptance/<id>_test.go` (package `acceptance`, standard library only, a harness-added `//go:build acceptance`, one `TestA<N>` reading `GM_ACCEPTANCE_URL`, `t.Fatal` on any unmet expectation, never a skip) and records its SHA-256 in `_state/acceptance_tests.json`. Wave 0 commits the files; the executor refuses a changed file (`test_file_changed`) at Wave 0 and before every acceptance round. The tag keeps the tests out of the plain `go test ./...` runs.
- **The server.** For a round with such a bullet the executor starts `serve.command` once with `sh -c` in the acceptance environment (`GM_ACCEPTANCE_ADDR` is where it must listen), polls `serve.ready` on `GM_ACCEPTANCE_URL` until it answers 2xx (at most `min(acceptance_timeout_seconds, 60)` seconds), runs those bullets against it, then kills its process group and runs the leak check. A refused, early-exiting or never-ready server stops the run `failed` / `acceptance_serve`.
- **The pass rule for `go test`.** A root test command that is one plain `go test` invocation (optionally after `unset NAME`, with `-tags`, `-run`, `-count`, `-v`, `-race`, `-short`, one package) is run as `go test -json` and judged by the runner's parser: exit 0 in time, at least one test ran and passed, none failed, the build held, every test named by `-run` passed. A skipped test is not a pass, and a run that selects no test is a failure although it exits 0. Any other shape is judged by its exit status.
- **The red check.** At Wave 0, after the stubs compile and the leaf red checks, every root test of an acceptance bullet or checked constraint that exercises the server (`acceptcheck.ServerExercising`: a built binary, `curl`, `GM_ACCEPTANCE_*`, a loopback address, or a go test of the acceptance package) runs against the stub repository with the stubs' binaries and a time limit of `min(acceptance_timeout_seconds, 30)` seconds, and must fail (a timeout counts as failing). A bullet none of whose server-exercising tests fails stops the run before any model call: `failed` / `acceptance_not_red`, bullet ids only. Static commands (`go build`, `go vet`, `gofmt`, `grep`) are exempt. Go acceptance tests run without the server.

## 10. Resume (item 11)

`gophermind brief run <id>` is always resume-safe: it starts fresh when the blackboard has no rows past `pending` and resumes otherwise. There is no separate `resume` verb for the executor (ruling R11); `gophermind brief resume <id>` keeps its planner meaning.

1. Load run record, `brief.md`, `contracts.json`, tree, `coverage.json`, `approval.json`; recompute the plan hash and refuse to run on a mismatch, exactly as Test-writer does. Refuse when the node files' and contracts' hashes differ from the ones recorded at first start (`_state/executor.json`).
2. `Board.InitRun` (idempotent), then `ReleaseStale(run, stale_claim_seconds)` (default 120, four heartbeats). Emit one `resume` event listing the released ids.
3. Recompute readiness from `verified` rows.
4. Git: check out the work branch. A dirty tree is never stashed or discarded (this differs from BUILD_PLAN 11 step 5, deviation X11). Files that belong to a released leaf (its contract file or its stub) are left as they are; any other dirty path stops the run `failed` naming the path.
5. For each released leaf the executor **runs its checks on the file already on disk before any model call**. If they pass, the leaf is committed and `verified` with zero model calls. Otherwise the ladder starts at rung 2 with an empty previous failure (runner output is never stored, so it is recomputed by that first check and used as the previous failure at no model cost).
6. Continue at the lowest wave with an unfinished leaf.

Invariants, each a test: no ledger row for a leaf that was `verified` before the kill; at most one duplicate implement call, for the one leaf in flight; exactly one commit per leaf on the work branch after resume; blackboard attempts of the killed leaf are preserved and continue numbering. GOAL.md's clean-run loop forbids restarts inside a graded attempt, so the GOAL run never calls resume; the feature exists and is tested so that an accidental kill is recoverable in ordinary use, and the run report records `resumed: true` so a graded attempt that used it can be detected.

## 11. Config

One new section in `gophermind.yaml`, defaults written on first use, validated by `settings.Validate` (every value positive):

```yaml
executor:
  workers: 1
  fix_attempts: 2
  repair_rounds: 2
  acceptance_repair_rounds: 2
  test_timeout_seconds: 120
  acceptance_timeout_seconds: 300
  stale_claim_seconds: 120
  output_cap_bytes: 65536
  heartbeat_seconds: 30
  artifacts: on          # on | off; off stores hashes and sizes only, no reply or check output
  artifact_max_bytes: 65536   # cap on each saved attempt file
  go_mod_cache: ~/.gophermind/gomodcache
  max_run_minutes: 720
  sandbox: on            # on | off; off is required explicitly on any OS but darwin, and is recorded in the report
  proxy: {listen: "127.0.0.1:0", log: proxy.log}
toolchain:            # decision E5: what a Go command needs, nothing from the harness environment
  PATH: /usr/local/go/bin:/usr/bin:/bin
```

`execenv.Inputs.Toolchain` is filled from this section (`PATH`, and per run `HOME`, `TMPDIR`, `GOCACHE`, `GOMODCACHE`, `GOPATH`), and the new `GoEnv` (section 15) carries the Go policy variables. The `go` binary and `git` are located at start; a missing one is a preflight failure with a message, before any model call. `max_revisions` and `max_context_tokens` are the planner's existing `defaults`.

## 12. Git landing (item 13)

Interface `gitland.Repo` (the git CLI implementation runs `git` with fixed argument vectors, no shell, `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` pointed at an empty file, author and committer `GopherMind <gophermind@localhost>` from the environment):

```go
type Repo interface {
    Start(baseBranch, workBranch string) error       // verify base exists and tree clean; create or check out work branch
    CommitWave0(paths []string) (string, error)      // test files, stubs, go.mod, go.sum
    CommitLeaf(nodeID, title string, add, remove []string) (string, error)
    CommitRepair(nodeID string, round int, add []string) (string, error)
    Finish(msg string) (string, error)               // empty final commit, then ff-only merge into base
    Dirty() ([]string, error)
    Diff(base string) ([]byte, error)                // for diff_only
}
```

There is no `Push`, no `Force`, no `Rebase`, no `Reset --hard` method: the interface cannot express them, and a test asserts the git CLI implementation never spawns `push`, `--force`, `reset` or `rebase`.

- **Start.** `base_branch` (from the brief, `main`) must exist. On a fresh run the tree must be clean; a dirty tree stops the run before any model call. `work_branch` is `gm/<brief-id>` created from the base tip. On resume it is checked out (section 10).
- **Wave 0 commit.** Every `*_test.go` the Test-writer placed, the type declaration files, every stub, and `go.mod` and `go.sum`, made after the deps step so `go.sum` is complete. `contracts.json` and `.gophermind/` are not committed (`rundir` already adds `.gophermind` to `.git/info/exclude`).
- **Per leaf.** Stage exactly the leaf's file and the removal of its stub, commit `gm(<node-id>): <title>` with trailers `GopherMind-Node: <id>` and `GopherMind-Run: <brief-id>`. Commits are serialized by a mutex; leaves never share a file, so no merge is ever needed. The short hash is stored in `SetResult`. A repair commits as `gm(<node-id>): repair round <n>` with the same trailers.
- **Finish** (ruling R13). When every leaf is `verified` and `Acceptance passed: N of N`, an empty commit `gm(run): <title> built, Acceptance passed N of N` on the work branch carries the summary and is the commit `main` will end at; then `git checkout <base>` and `git merge --ff-only <work>`. If `main` moved so that a fast-forward is impossible, the run stops `failed` with `landing_blocked` and leaves the work branch intact; the executor never rebases, resets or forces. Result: `main` in the target repo holds GopherMind's commits, and the orchestrator's tag lands on the final commit.
- **`landing: diff_only`**: no work branch is created and nothing is committed; the files are written to the working tree and at the end `changes.patch` is written to the run folder from `Diff`. **`landing: pull_request`**: unsupported here; the executor stops at Start with an error naming the field, it does not downgrade silently (section 17).
- **No remote.** The target repo has none (AI Venture Studio). Nothing in this plan calls one, and `Start` does not require one.

## 13. The report (item 14 subset)

`report.json` in the run folder and a printed summary. Built by `report.Build(ledgerRows, boardRows, requirements, coverage, acceptance)`:

```json
{
  "run_id": "...", "started_at": "...", "finished_at": "...", "status": "verified|failed|escalated|interrupted",
  "resumed": false, "sandbox": "on", "stop_reason": "", "exit_code": 0,
  "requirements_covered": {"covered": 34, "total": 34},
  "acceptance": {"passed": 12, "total": 12},
  "constraints_checked": {"passed": 3, "total": 3},
  "waves": 5,
  "nodes": {"total": 61, "verified": 61, "failed": 0, "escalated": 0, "blocked": 0},
  "by_task_type": [
    {"task_type": "implement", "model": "mini/qwen3.6:35b-a3b", "calls": 88, "ok": 79, "malformed": 4,
     "retries": 9, "model_escalations": 1, "revision_escalations": 0, "human_escalations": 0, "prompt_tokens": 412000, "completion_tokens": 51000, "avg_duration_ms": 41000}
  ],
  "weak_tests": 2, "repairs": 1, "landing": {"branch": "gm/gm-2026-09-29-002", "commit": "abc1234", "merged_into": "main"}
}
```

Definitions, so a hand count can match: `calls` is ledger rows for that task type and model; `malformed` is rows with outcome `malformed`; `retries` counts rows beyond the first for the same node, stage and revision on the same model (ledger) plus attempts beyond the first for the same node, revision and model (blackboard); the single `escalations` number is replaced by three named fields: `model_escalations` (moves to another chain entry, including tier bumps), `revision_escalations` (revision increments, i.e. revise rungs) and `human_escalations` (`Gate.Escalate` calls); tokens are summed from the rows. Task types are the planner's (`clarify`, `contract`, `decompose`, `coverage`, `testwrite`) plus `implement` and `revise`, so one table shows who did what across the whole run. The printed summary ends with the two proof lines, in this order: `Requirements covered: N of N` (copied from the planner's `coverage.json` and `requirements.json`) and `Acceptance passed: N of N`. `pass_rate` and `first_try_wins` of BUILD_PLAN item 14 are derivable from the blackboard attempts and are included per model in the leaf section of the same file.

## 14. The network proxy (item 12)

Minimal design for a run whose only outside need is Go module fetching (ruling R7):

- **Server.** In-process `net/http` forward proxy on the configured loopback address (port 0 picks a free one), plain requests and `CONNECT`. One instance per run, closed on exit.
- **Allowlist.** Hosts from the brief's `network` block, every configured provider `base_url` host pinned to that URL's port (80 or 443 by scheme when the URL has none), so the mini's `192.168.1.35:11434` is allowed and `192.168.1.35:22`, `:3306` and `:5432` are denied and recorded as warnings; a brief network entry has no port and keeps matching every port of its host, and `proxy.golang.org` and `sum.golang.org` unless the brief overrides. Exact host or `*.suffix`. Loopback destinations are never sent through the proxy (`NO_PROXY`), so a server under test on `localhost` needs no rule.
- **Node attribution.** The proxy URL handed to a node's commands is `http://node-<id>@127.0.0.1:<port>`; the userinfo arrives as `Proxy-Authorization` and names the node. Harness clients (providers) send `X-GopherMind-Node`, which the proxy strips.
- **Denied** requests get 403 and one `proxy.log` line with `verdict: denied`. Log fields: time, node, method, host, port, verdict, status, bytes, duration. No path, no query, no body, no header values, and no code path that logs them.
- **Critical failure.** A request to a host marked `critical: true` that is denied, times out, or (plain HTTP only, since TLS through `CONNECT` is opaque) returns 5xx during an attempt makes that attempt fail with `network_critical: <host>`. Three in a row on one node set the node `failed`, terminal, and stop the run. Non-critical failures log a warning event and change nothing. The executor learns of failures by `Proxy.Failures(node, since)` after each command.
- **Go modules** (ruling R7). Leaf commands and wave checks run with `GOPROXY=off`, `GOFLAGS=-mod=readonly`, `GOTOOLCHAIN=local`, `GOSUMDB=off` (safe offline because the deps step already verified checksums), `GOPRIVATE=off`, `GOMODCACHE=<executor.go_mod_cache>`: they cannot fetch anything, so no leaf ever depends on the network. Modules are fetched exactly once, in a **deps step**, in this order at a fresh run start: (1) Wave 0 files are written (types, stubs; the Test-writer's tests already exist); (2) the harness writes `go.mod` (`go mod init <module>` when absent); (3) the deps step: for each entry of `dependencies.json`, `go get <module>@<version>` then `go mod download`, with `GOPROXY=https://proxy.golang.org`, `GOFLAGS=-mod=mod`, `GOSUMDB` at its default (checksums verified through `sum.golang.org`), the proxy variables set, network allowed to the proxy host only, so both hosts must be on the allowlist; (4) the scan of section 6.1 over `go.mod` and sources; (5) the Wave 0 commit including `go.sum`. `go mod verify` runs at the end of the run, before Finish, and a failure stops the run `failed`. A run with an empty `dependencies.json` never touches the network. Vendoring was rejected: it puts third-party source in the target repo and in the diff.
- **Proxy is not the sandbox.** The proxy governs HTTP and HTTPS through the standard library; raw TCP does not go through it. Containment is the sandbox of section 6.1, which denies non-loopback network, and loopback ports that are not listed, to every child. The proxy is policy and audit.

## 15. Changes required in the planner and execenv

Small, listed so the plan can order them first:

1. **`dependencies.json`.** The contract outline pass may return `dependencies: [{module, version, purpose}]`; the planner writes `<run>/dependencies.json` (empty list when none) and validates that each module path is plausible and each version is pinned semver (no `latest`, no range). `planner/approve.go` `hashedFiles` (around line 63) gains `dependencies.json`, and `RenderPlan` lists the dependencies. Consequence, stated plainly: every approval made before this change hashes a different file set and is invalidated; a plan approved earlier must be approved again.
2. **`execenv` `GoEnv`.** `execenv.Inputs` gains `GoEnv map[string]string`, allow-listed to `GOTOOLCHAIN`, `GOPROXY`, `GOFLAGS`, `GOSUMDB`, `GOPRIVATE`. Any other key is an error; `GOFLAGS` may never contain `-toolexec` or `-overlay`; a brief cannot declare these names. `Build` also always emits `NO_PROXY=127.0.0.1,localhost,::1` (and the lowercase forms) whenever `ProxyURL` is set, so loopback servers are reached directly. Tests: `TestGoEnvAllowList`, `TestGoFlagsRejectToolexec`, `TestNoProxyAlwaysEmitted`.
3. **`pathsafe`.** `safeTestPath`, `insideRepo` and the no-follow open move to `pathsafe` with their tests; the planner imports it. Behavior unchanged.
4. **Test-writer records** the test file path and the test function name on each leaf (it already writes `_state/test_files.json`); the executor reads that file instead of recomputing `testFuncName`.
5. **Types and stubs are generated by the executor** from `contracts.json` using `go/parser`; nothing in the planner changes for them.
6. **`blackboard.Attempt` gains `ReplySHA256 string`** (hash only), stored by the SQLite backend (migration adds a column); see R17 and deviation X12.

## 16. Error handling, privacy, risks

- **Interrupt (SIGINT/SIGTERM).** The current call and command are cancelled, the ledger row records `error_kind: cancelled`, in-flight claims are released, and the run ends `interrupted` (exit 5), resumable.
- **Ledger failure** never fails a call; `router.LedgerErrors()` is copied into the report and the run is marked incomplete, as in the planner.
- **Privacy.** No reply text, command output, prompt, or secret value reaches an error, event, log, blackboard row or ledger row. Secret values appear only in a command's environment. Persisted per attempt: class, test names, counts, sizes, hashes. The notes overlay holds model-written hints, is in the run folder (mode 0700, excluded from git), and is never logged. A canary secret, a canary in a reply and a canary in test output are grepped for after the end-to-end test (BUILD_PLAN ground rule).
- **Time.** Worst case per leaf is 11 implement plus 2 revise calls; with 60 or more leaves a run is long. `executor.max_run_minutes` (default 720) ends it cleanly and resumably (7.1).
- **Memory.** The mini has about 9 percent free memory: one request at a time (`workers: 1`, `max_concurrent: 1`), and the executor never loads a second model. A `memory_pressure` check is the operator's before a run, not the executor's.
- **Risks.** (a) `qwen3.6:35b-a3b` may not reach a green build on every leaf; the ladder is bounded and the failure is loud, but a perfect run may need prompt work driven by the ledger. (b) `sandbox-exec` is deprecated by Apple and could disappear in a future macOS; the preflight fails loudly if it does. (c) The proxy cannot see inside TLS; the sandbox's network deny covers that. (d) Acceptance commands that need Postgres depend on `TEST_DATABASE_URL` and a running database being provided by the operator through the brief's declared secrets; the executor starts no database. (e) Weak tests pass a wrong implementation, and only acceptance catches it. (f) The brief's `repo:` field names another path; `Options.Repo` overrides it and the report records both.

## 17. Not in scope

- The planner's revise stage (rewriting a node, `SPLIT_REQUIRED`, `CONTRACT_CHANGE_REQUIRED`, rewriting a bad test). The executor only adds hint notes and escalates.
- `landing: pull_request`, any push, any forge call, Gitea.
- Component roll-up as its own stage (BUILD_PLAN 14's per-component integration run); the per-wave checks cover it.
- The live view and Gantt (item 15), the run service in `gophermind-server`, the app screens.
- `/project` integration and the fate of the v1 planner (GOAL task 4). Only `executor.Run` is left as a clean entry point.
- Sandboxing on non-darwin systems (refused unless `sandbox: off`), vendoring, a database provisioner, non-Go targets.
- Parallel workers beyond the config knob; adaptive chain reordering from the attempt log.
- Adding a dependency after approval (a new approval is needed).

## 18. Testing

All offline: fake provider (scripted per node and call), `httptest`, temp directories with an isolated `GOPHERMIND_CONFIG_DIR`, `GOPROXY=file://` for the deps step, no real network and no `~/.gophermind`. A tiny **greeter repo** lives in `executor/testdata/greeter/` (module `example.com/greeter`: 2 packages, 5 leaves in 3 waves, one `cmd/greeter` server exposing `/hello`, two acceptance bullets with root tests) with its plan artifacts prebuilt as the planner would leave them (approved, hash matching).

Requirement to test map (names to be written in the plan):

| Requirement | Test |
|---|---|
| Packer includes only the leaf's own slice and its test file | `TestPackNeedToKnow` (sibling and repo canaries absent) |
| Assembly order and previous-failure cap (30 lines, 2 KB, secrets stripped) | `TestPackOrderAndFailureCap`, `TestPackStripsSecretLines` |
| Estimate `ceil(len/4)*1.1`; over budget after trimming makes no call | `TestPackBudget`, `TestPackDropOrder`, `TestContextTooLongTriesNextEntryThenEscalates` |
| BUILD_PLAN item 9: 40 KB of dependency signatures is trimmed by the drop order; with a required floor over budget and a one-entry chain it escalates with zero provider calls | `TestPackPaddedSignatures`, `TestPackFloorOverBudgetNoCall` |
| Prompt never stored; only hash and size | `TestNoPromptTextPersisted` (canary in DB, logs, events) |
| Model can only write the node's file | `TestForbiddenWriteRejected`, `TestReplyCannotNamePath`, `TestPathsafeTable` (symlink, `..`, `.git`, `_test.go`) |
| Signature and package checked | `TestReplySignatureMismatchMalformed` |
| Import policy and dependency vetting | `TestImportPolicy`, `TestDepsStepFetchesOnlyListed`, `TestDepsStepBadVersionStopsRun` |
| Fails twice then passes: verified with three attempts | `TestLeafFailTwiceThenPass` |
| Always fails: ladder in order, revise, escalated after `max_revisions`, gate called | `TestLadderOrderAndEscalation` |
| Bound on calls per leaf | `TestLeafCallBound` (asserts the formula on a never-passing fake) |
| Identical reply abandons the entry | `TestIdenticalReplyAbandonsEntry` |
| Malformed reply costs a router repair, not a leaf attempt | `TestMalformedIsCheapRepair` |
| Escalation follows the chain; single-entry chain skips empty rungs | `TestEscalationAlongChain`, `TestSingleEntryChainNoWastedCall` |
| CONTRACT_PROBLEM handled once then escalates | `TestContractProblemOnce` |
| Failure text never carries reply or output | `TestNoReplyOrOutputInPersistedFailure` |
| Blocked leaves are reported, never skipped | `TestBlockedLeavesReported`, `TestVerifiedImpliesEveryLeafVerified` |
| Stubs make every commit build; red check | `TestStubsBuildAtEveryCommit`, `TestRedCheckWeakTestWarned` (warning, reported, non-blocking) |
| Waves run in order, ids sorted, deterministic | `TestWaveOrder` |
| Wave checks and attribution | `TestWaveChecksAttribution` (build, vet, test failure map to nodes), `TestUnattributableStopsRun` |
| Repair loop is bounded | `TestRepairLoopBound`, `TestRepairEscalatesAfterBound` |
| Acceptance count comes from requirements, not the executor | `TestAcceptanceCountFromRequirements` (a dropped root test is a failure) |
| Acceptance runs the built binary, kills leftovers | `TestAcceptanceUsesBuiltBinary`, `TestAcceptanceKillsProcessGroup` |
| Acceptance failure repairs mapped nodes then fails loud | `TestAcceptanceRepairThenFail` |
| Resume: killed run, no repeated call for a finished leaf, one commit per leaf | `TestResumeAfterKill` (re-exec of the test binary killed with SIGKILL mid-wave, 5 s scripted delay), `TestResumeNoDuplicateCalls` |
| Resume checks the on-disk file before any call | `TestResumeVerifiesFileOnDiskFirst` |
| Resume refuses a dirty non-owned path and a changed plan | `TestResumeRefusesForeignDirt`, `TestResumeRefusesChangedPlan` |
| Stale claims released | `TestStaleClaimsReleased` |
| Proxy allowlist, denial, log fields, node attribution | `TestProxyAllowDeny`, `TestProxyLogHasNoPathOrBody`, `TestProxyNodeAttribution` |
| Critical failure fails the attempt; three in a row fails the node | `TestCriticalHostFailure`, `TestThreeCriticalFailuresTerminal`, `TestNonCriticalWarns` |
| Loopback bypasses the proxy; leaf commands cannot fetch | `TestLoopbackNoProxy`, `TestLeafCommandsOffline` |
| Git: Wave 0 commit, one commit per leaf touching exactly its files | `TestGitLandingCommits` (`git log --format=%s`, `git show --name-only`) |
| Finish fast-forwards `main`; blocked when it moved; no force or push ever | `TestFinishFastForward`, `TestLandingBlockedWhenMainMoved`, `TestNoPushForceResetRebase` |
| `diff_only` writes a patch; `pull_request` refused | `TestDiffOnlyPatch`, `TestPullRequestUnsupported` |
| Dirty tree stops a fresh run; no remote needed | `TestFreshRunDirtyTreeRefused`, `TestNoRemoteRequired` |
| Node files and contracts unchanged during a run | `TestPlanFilesImmutable` |
| Events for every stage | `TestEveryStageEmitsEvents` |
| Report matches a hand count; proof lines end the summary | `TestReportHandCount`, `TestSummaryEndsWithProofLines` |
| Exit codes | `TestExitCodes` (0, 1, 3, 4, 5, 7; 6 is the `--check-env` preflight) |
| Types are written from contracts and committed in Wave 0 | `TestTypeDeclsWritten`, `TestTypeDeclBadImportStopsRun` |
| Stub/real swap: stub removed before write, restored after failure, leaf committed only on pass, commit removes the stub | `TestStubSwapOnFailureRestoresStub`, `TestLeafCommitRemovesStub`, `TestResumeNormalizesStubAndReal` |
| Deps ordering: go.mod, deps, scan, then Wave 0 commit with go.sum; `go mod verify` at the end | `TestDepsStepOrder`, `TestGoModVerifyAtEnd` |
| Rejects replace, toolchain, go:generate, cgo | `TestScanRejectsReplaceToolchainGenerateCgo` |
| Git status snapshot catches stray files | `TestStrayFileFailsAttempt`, `TestStrayFileFailsWave` |
| Sandbox: cannot write outside allowed dirs, cannot read $HOME, cannot connect off loopback (skips with a clear message if `sandbox-exec` is missing) | `TestSandboxDeniesWriteOutside`, `TestSandboxDeniesHomeRead`, `TestSandboxDeniesNonLoopback`, `TestSandboxAllowsLoopback`, `TestSandboxRequiredOffDarwin` |
| Run limit stops cleanly and resumably | `TestMaxRunMinutesStopsCleanly` |
| Reply hash survives resume | `TestReplyHashPersistedAcrossResume` |
| Per-package race runs | `TestWaveRacePerPackage` |
| GoEnv and NO_PROXY | `TestGoEnvAllowList`, `TestNoProxyAlwaysEmitted` |
| Everything end to end on the greeter repo | `TestExecutorE2E` (wave order, a malformed reply, a failing then passing leaf, an escalation to a second chain entry, a repair, acceptance, git landing, canary secret grep) |
| Settings section defaults and validation | `TestExecutorSettingsDefaults`, `TestExecutorSettingsValidate` |
| Manual, not CI | `gophermind brief run` on `04-csvstat.md` against the mini, then the GOAL run |

Coverage target: every rule above has a named test, `go test -race ./gophermind-lib/briefv2/...` passes, and `go vet` is clean.

## 19. CLI and commands

Under `gophermind brief`: `run <id> [--gate terminal|file] [--repo <path>] [--workers n]` (resume-safe, section 10), `report <id>` (prints `report.json`), and the existing `status`, `calls`, `coverage`. `status` gains executor lines (waves done, leaves by status, blocked). Exit codes: 0 verified, 1 failed or error, 2 invalid brief, 3 waiting on a human (file gate), 4 escalated leaf or human stop, 5 interrupted, 6 failed `--check-env` preflight (not a run), 7 harness fault (`executor.Run` returned an error: repository not openable, sandbox refused, settings invalid, plan changed, live worker; also settings that fail to load). A failed run (1) and a harness fault (7) are distinct so an orchestrator can tell them apart. `<id>` resolves through the planner's `LookupRun`.

## 20. Deviations from the handoff

| # | Handoff says | Here |
|---|---|---|
| X1 | Packer never includes repo file contents | The leaf's own harness-written test file is included; nothing else from the repo |
| X2 | `gophermind resume <run-id>` | `brief run <id>` is resume-safe; no second verb |
| X3 | go-git for landing | The `git` CLI behind `gitland.Repo`; no new dependency, exact staging, same behavior John sees |
| X4 | Wave 0 commit is tests only; rebase leaves onto the work branch | Tests plus stubs, so every commit builds; ff-only merge, never a rebase |
| X5 | Fixed proxy port 8480 | Loopback port chosen at start; the URL is passed to commands |
| X6 | Revise via `06-revise.md` may split or change a contract | Notes and escalation only until the planner's revise stage exists |
| X7 | `landing: pull_request` supported | Refused with a message |
| X8 | Component tests then root tests as a roll-up stage | Per-wave repo checks and one acceptance run; a separate component stage is item 14 |
| X9 | Provider errors count as attempts in the log | An `error` verdict never consumes a fix attempt or a revision |
| X10 | Failed tests recorded with output | Names and counts only; output in memory for the next prompt |
| X11 | BUILD_PLAN 11 step 5: a dirty tree is stashed under `gm/resume-<ts>` | Never stashed; files of released leaves are checked in place, any other dirty path stops the run (nothing is hidden or lost) |
| X12 | `Attempt` has no reply hash | `ReplySHA256` added (hash only) so the identical-reply check survives resume |

## 21. Rulings and their cost if wrong

| # | Ruling | Cost if wrong |
|---|---|---|
| R1 | Package split: `packer`, `runner`, `executor`, `gitland`, `proxy`, `report`, `pathsafe`, `sandbox` (BUILD_PLAN has `prompt`, `executor`, `proxy`, `gitland`, `report`) | Low: packages merge or split by moving files; import direction is fixed |
| R2 | Library entry `executor.Run(ctx, Options)` returns a `Report` and reserves the error for harness faults | A caller treating `err == nil` as success would still see `Status`; fixed by one wrapper |
| R3 | The leaf's own test file goes in the prompt | If a model games tests it sees, weak tests pass wrong code; acceptance still catches behavior, and the alternative (givens and expects prose only) makes a passing implementation depend on the model guessing the assertions |
| R4 | Leaf done means one attempt with parse, gofmt, build, vet, named test pass, at least one test event | Too loose lets weak code through to wave checks; too strict costs extra retries on the free mini |
| R5 | Escalation ladder: repair, fix (2 per entry), next model, tier bump, revise (notes only), escalate; per-leaf call bound is a formula | Too many rungs waste mini time (11 implement calls worst case); too few end a run that another try would fix |
| R6 | Sandbox (overrides the first draft): on darwin every child runs under a generated `sandbox-exec` profile (writes only to repo, scratch, caches; no `$HOME` reads; loopback only, plus the proxy host during the deps step); other OSes refuse unless `sandbox: off` is set and recorded; scans reject replace, toolchain, generate, cgo; git status snapshots catch stray files | If Apple removes `sandbox-exec` the executor stops at preflight until an alternative exists; a profile too strict fails builds early and visibly, one too loose is caught by the sandbox test |
| R7 | Modules: fetch once in a deps step through the proxy, then `GOPROXY=off -mod=readonly` for everything else | If the module cache is shared and poisoned, builds pass wrongly; checksum verification and `go mod verify` at the end limit that. Vendoring would bloat the diff |
| R8 | New third-party dependency: declared in `dependencies.json` by the planner, approved with the plan, pinned, imported only if listed; a leaf that needs another fails with `import_not_allowed` and escalates | A brief needing a late dependency needs a new approval; a hallucinated version fails the deps step early and cheaply |
| R9 | A test that passes against the panicking stub is a warning, not a block | A vacuous test lets a wrong leaf through; only acceptance catches it |
| R10 | Unattributable integration failures stop the run rather than guess a node | A run stops where a guess might have finished; the alternative burns repair rounds on the wrong leaf and hides the real fault |
| R11 | `brief run` is resume-safe; no separate resume verb; GOAL runs never resume | A user expecting `resume` reads the docs; the report flag `resumed` makes a graded attempt that used it detectable |
| R12 | Acceptance N comes from `requirements.json`, not the executor's own list; failure repairs mapped nodes for 2 rounds then fails loud | If `requirements.json` mis-parses a bullet, N is wrong in the strict direction (a bullet counted without a test fails the run) |
| R13 | Landing: work branch `gm/<id>`, per-leaf commits, an empty final commit, ff-only merge into base; no rebase, no force, no push | A moved `main` blocks landing and needs a person; the alternative (rebasing) rewrites GopherMind's history |
| R14 | Escalation with no gate answer means `stop`; skip yields run status `failed` | A batch run halts on the first stuck leaf instead of finishing the rest; chosen because GOAL.md counts an abandoned task as a failed attempt anyway |
| R15 | Replies and check output are saved, scrubbed, under `attempts/` unless `executor.artifacts` is `off`; prompts are never stored. Persisted failures in the ledger are class, names, counts, hashes | A resumed leaf loses its last failure text and spends one check (no model call) to rebuild it |
| R17 | Add `ReplySHA256` to the attempt record (hash only) | A schema migration on the blackboard; without it the identical-reply guard is lost on resume and one extra call may repeat |
| R18 | Types and stubs written by the harness at run start, committed in Wave 0; stub swapped out and restored around every attempt | A wrong type declaration is a plan defect that stops the run before any model call rather than being patched by a leaf |
| R19 | `max_run_minutes` (default 720) ends the run cleanly, resumable, exit 5 | Too low interrupts a legitimate long run (resume continues it); too high lets a stuck run occupy the mini |
| R20 | Context overflow: fixed drop order, then next chain entry, escalate only after all entries fail | A leaf whose floor never fits escalates instead of being split; splitting needs the planner's revise stage |
| R16 | Node files and `contracts.json` are immutable during a run; revision hints live in `_state/notes.json` | Notes cannot fix a wrong contract; that needs the planner's revise stage |

## 22. Open questions resolved

| Question | Resolution |
|---|---|
| How do acceptance bullets map to executable checks? | Planner root tests (`coverage.json`), each a shell command; executor recounts from `requirements.json` (section 9) |
| How is N of N proven? | `acceptance.json` with every command's exit code, plus the printed `Acceptance passed: N of N` and the copied `Requirements covered: N of N` |
| Go modules: GOPROXY, vendoring, or the proxy? | Deps step through the proxy once, then offline `GOPROXY=off` (R7) |
| How is a new third-party dependency declared and vetted? | `dependencies.json`, pinned, in the approval hash (R8) |
| Commit per wave or per leaf? | Per leaf, plus Wave 0 and a final empty commit (R13) |
| Is there a remote? | No; nothing calls one |
| Who rewrites a bad leaf definition? | Nobody yet; hints and escalation (R16, section 17) |
| Does the packer see test files? | Yes, the leaf's own (R3) |
| What ends a run? | Section 7.1's list; no other path returns success |
| What about a leaf whose tests pass on a stub? | Warning event and report count (R9) |
| `go-git` or the `git` binary? | The binary behind an interface (X3) |
| Is the proxy a sandbox? | No; the sandbox (6.1, R6) contains, the proxy is policy and audit (section 14) |
| What does resume do with an in-flight leaf's file? | Runs its checks first; passing means verified with no model call (section 10) |
| What happens to `/project`? | Later task; only `executor.Run` is provided |

## 23. What comes next

1. Written implementation plan for this document (order: `pathsafe` and planner changes, `settings` section, `packer`, `runner`, `proxy`, `gitland`, `executor` leaf loop, waves and repair, acceptance, resume, `report`, CLI).
2. The manual csvstat run against the mini, then the GOAL clean-run loop through `/project`.
3. The planner's revise stage (split, contract change, test rewrite), the component roll-up, and the live view.

**Acceptance hardening (Task 13 fix rounds 1 and 2).** A root test command is accepted when it contains `go build`, `go vet` or `go test` with a package pattern (`./...` or `./path`), the name of a main package under `cmd/`, `curl`, `$GM_ACCEPTANCE_URL` or `$GM_ACCEPTANCE_ADDR` (a fresh loopback address the harness gives every command), or a loopback address. It is refused (`acceptance_vacuous`, bullet ids only, at start and again at finish) when its whole effect is a no-op builtin (`true`, `:`, `exit`, `echo`, `printf`, `sleep`, `test`, `[`, or only comments), when it contains `go run`, `go install`, `go get` or `go generate`, runs a binary by a path outside the built bin directory (`GM_ACCEPTANCE_BIN`), or runs `rm`, `mv`, `cp`, `chmod` or `ln` on the bin directory. A command such as `go build ./...` checks the code and needs no server. A bullet whose text says exactly `with NAME unset` (NAME a declared secret or env name, case sensitive) runs with NAME removed from its environment, and the proof id says so. A secret URL with a loopback host is dialled before acceptance (`acceptance_environment`, secret name only) only when a bullet that does not unset it names it. The SHA-256 of every built binary is recorded before the first command and re-checked after every command (`acceptance_tampered`). Every round is bracketed by `acceptance.json` with `complete` false then true, a stray-file check (`acceptance_stray`) and a leak check of the command's address and `GM_ACCEPTANCE_PIDFILE` pid (`acceptance_leak`). The harness dials loopback URL secrets before acceptance (`acceptance_environment`, secret name only). After acceptance finish runs build, vet, the race tests, `go mod verify` and the scan again and requires a clean tree before landing.

**Resume and limits (Task 14).** `resume` re-checks the plan hashes against `_state/executor.json` (`plan files changed since the run started`, an error: nothing is modified), runs `InitRun`, releases claims older than `stale_claim_seconds` and emits one `resume` event (ids and counts only); a claim still held afterwards has a live heartbeat and is the error `node <id> is held by a live worker`. The work branch is checked out through `Start`, and the run refuses (`failed`, `repo_moved`, a fixed message) when it no longer contains the tip recorded in `State.Tip` (`gitland.Repo.IsAncestor`; the tip is recorded after Wave 0, after every leaf commit and when an invocation ends, never after a refusal). Dirty paths that belong to no released leaf stop the run `failed`, `foreign_dirt`, naming at most 5 paths; with `diff_only` every leaf's files and Wave 0's files are the result and are never foreign. `Finish` makes no second final commit when the branch tip already is this run's `gm(run):` commit, so a kill between the final commit and the fast-forward, or after it, resumes to the same history. `acceptance.json` is not a node file for `tree.Store.Load` (a plan must load after the acceptance stage ran). `runContext` ends on SIGINT or SIGTERM (cause `signal`) and when `max_run_minutes` has elapsed since the invocation began (cause `max_run_minutes`, restarted by every invocation); the report is `interrupted`, exit 5, with that stop reason, in every stage including acceptance. `CheckWorkers` is the CLI's `--workers` refusal. There is no separate per-leaf wall clock: a leaf is bounded by its ladder (11 implement and 2 revise calls at most) and each call by `defaults.call_timeout`.

**Acceptance hardening, round 3.** The probe tokens must be the command word of a statement that will run, read by a small shell parser (quotes, `;`, `&&`, `||`, `|`, `&`, newline, `$( )`, backticks, `sh -c` and `bash -c` scripts, `if`/`while`/`until` blocks, comments); statements after an unconditional `exit`, the right side of `true ||` and `false &&`, and the statically dead branch of `if true`/`if false` are ignored, and `go run|install|get|generate` is refused anywhere in the text. After every acceptance repair round every main package is rebuilt (the old binaries are removed by a guarded delete) and the SHA-256 baseline is taken again. Loopback `host:port` literals in a command (`localhost:N`, `127.0.0.1:N`, `[::1]:N`) are dialled before the command (a port in use is `acceptance_environment`, port only) and after it (still accepting is `acceptance_leak`, port only). A round with stray files is written `complete: false` with its reason.
