# Test-writer feedback with the local model: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Get a brief through the planner's test-writer stage with Gemma alone by telling the model what is wrong, not letting it copy its last reply, giving a node more than one round, and letting the three secret-bearing briefs load.

**Architecture:** Three small changes in `gophermind-lib/build/planner` (compile feedback, per-node rounds, a pinned glitch) and one in `gophermind-lib/build/router` (fresh-form retries), plus the local service script (vault passphrase, placeholder secrets) and a bench script that resumes failed runs. No new state store: the round counter lives in `_state/testwriter.json`.

**Tech Stack:** Go 1.22+, the offline fixture model (`planner.FixtureProvider`, `provider.Fake`), bash.

**Spec:** `docs/superpowers/specs/2026-11-03-testwriter-feedback-design.md`

## Global Constraints

- Local model only; no new providers, tiers or models.
- No em dashes, no emojis, in code, comments, docs or commits.
- The brief's text, secret values and the vault passphrase never appear in a refusal, log line, ledger row or prompt. The model's own test file and the compiler's words about it may.
- Every refusal stays bounded: `maxRefusalBytes` becomes 800; one source line in a message is capped at 120 characters; the fresh-form retry note is capped at 600 bytes.
- Per-node bound: `testwriterRounds = 3` rounds of `1 + max_parse_retries` calls.
- Commit per task with explicit `git add` paths; no push, install or deploy. Commit trailers:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`.
- From `gophermind-lib`: `go build ./... && go vet ./... && gofmt -l .` clean. Run focused tests per task; the full `./build/planner` and `./build/router` packages once in the last task (`-timeout 30m`).
- Never touch `~/.gophermind` from a test. The bench task is the only one that runs against real data, and only resumes runs that are already failed.

## Review Focus

- A compiler diagnostic located in a stub or another file, not the test file: still skipped (Task 1 test).
- A fresh-form note that would exceed 600 bytes because many distinct errors accumulated: truncated, the request still sent (Task 3 test).
- A reply identical to an earlier failed reply: goes to the fresh form without spending the conversational retry (Task 3 test).
- A resume in the middle of a node's rounds: the round counter is read back and the bound still holds (Task 4 test).
- The passphrase file missing when the service starts: the server starts without a vault, it does not fail to start (Task 5 check).

---

### Task 1: Compile feedback names the defect

**Files:**
- Modify: `gophermind-lib/build/planner/compilecheck.go` (`refuseCompile` ~line 209, `compileMessage` ~line 304, the doc comments at lines 51 and 303)
- Modify: `gophermind-lib/build/planner/refusal.go` (`maxRefusalBytes`, the comment at line 17)
- Modify: `gophermind-lib/build/events/events.go` (comment at line 23)
- Test: `gophermind-lib/build/planner/compilecheck_test.go`

**Interfaces:**
- Consumes: `refuseCompile(c *contract.Contracts, dir, base, src, out string) error`, `compileMessage(c *contract.Contracts, dir, src, line string, n int, diag string) string`, `diagRE`.
- Produces: the same signatures. Every message has the shape `<phrase or rich guidance> at line N: <compiler text, scratch path removed>: <source line trimmed, at most 120 chars>`. `scrubDiag(diag string) string` (new, unexported) removes any absolute path and the scratch module prefix from the compiler text. `maxRefusalBytes = 800`.

- [ ] **Step 1: Replace the no-quote tests with the new rule**

Rewrite `TestCompileMessageNeverQuotesDiagnostic` as `TestCompileMessageQuotesTheDiagnosticAndTheLine`:

```go
func TestCompileMessageQuotesTheDiagnosticAndTheLine(t *testing.T) {
	c := compileCheckContract()
	line := "\tif next, ok := m.redirects[req.URL.String()]; ok {"
	m := compileMessage(c, "internal/stats", "package stats\n", line, 27, "declared and not used: next")
	for _, want := range []string{"unused variable at line 27", "declared and not used: next", "if next, ok := m.redirects"} {
		if !strings.Contains(m, want) {
			t.Errorf("message %q lacks %q", m, want)
		}
	}
	m = compileMessage(c, "internal/stats", "package stats\n", "\tctx: context.WithTimeout(context.Background(), 1*time.Millisecond),", 108,
		"assignment mismatch: 1 variable but context.WithTimeout returns 2 values")
	if !strings.Contains(m, "does not compile at line 108: assignment mismatch: 1 variable but context.WithTimeout returns 2 values: ctx: context.WithTimeout") {
		t.Errorf("fallback message = %q", m)
	}
}

func TestCompileMessageCapsTheSourceLineAndStripsPaths(t *testing.T) {
	c := compileCheckContract()
	long := strings.Repeat("x", 300)
	m := compileMessage(c, "internal/stats", "package stats\n", long, 3, "/tmp/gm-compilecheck-123/internal/stats/fn_x_test.go:3:1: weird thing")
	if strings.Contains(m, "/tmp/gm-compilecheck") || strings.Contains(m, strings.Repeat("x", 121)) {
		t.Errorf("message leaks a path or an uncapped line: %q", m)
	}
}
```

Update `TestCompileMessageModelTypeFallsBack` and `TestCompileMessageInterfaceToWriter` so they assert the guidance is still present and no longer assert that the model's names are absent. Keep every other assertion in the file.

Add to the existing `refuseCompile` coverage a case where the only diagnostic is in `zz_other0_test.go`: `refuseCompile` returns nil (unchanged behaviour, now pinned).

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd gophermind-lib && go test -count=1 -run 'TestCompileMessage|TestCompileCheck' ./build/planner`
Expected: FAIL on the two new tests (message lacks the diagnostic text).

- [ ] **Step 3: Implement**

In `compileMessage`: compute `tail := ": " + scrubDiag(diag) + ": " + capLine(line, 120)` once (omit the line part when `line` is empty after trimming) and append it to every return value, including the `rich*` ones. `scrubDiag` removes `<anything>/gm-compilecheck-<digits>/` prefixes and any `file:line:col: ` prefix. In `refuseCompile` nothing changes but the comment. Set `maxRefusalBytes = 800` and rewrite the three doc comments (compilecheck.go 51 and 303, refusal.go 17, events.go 23) to the new rule: the model's test file and the compiler's text about it may be quoted, the brief never.

- [ ] **Step 4: Run the tests**

Run: `cd gophermind-lib && go test -count=1 -run 'TestCompileMessage|TestCompileCheck|TestRefusal' ./build/planner && go vet ./build/planner ./build/events`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/compilecheck.go gophermind-lib/build/planner/compilecheck_test.go gophermind-lib/build/planner/refusal.go gophermind-lib/build/events/events.go
git commit -m "fix(planner): compile refusals quote the diagnostic and the source line"
```

---

### Task 2: Pin the escape glitch with the real reply

**Files:**
- Create: `gophermind-lib/build/planner/testdata/escape-glitch/fn_follow_test.go.txt` (the `test_file` string of call 50, decoded)
- Test: `gophermind-lib/build/planner/unescape_test.go` (or a new `escape_glitch_test.go`)

**Interfaces:**
- Consumes: `parseTestwriteCounted(text, ct, pkg, funcName, module string, hooks []string, pkgs importSet)`, `compileCheckTestFile(c, rel, src, others)`, `unescapeQuotedSource`, `nudgeSyntax`.
- Produces: nothing new; a test that pins existing behaviour.

- [ ] **Step 1: Extract the fixture**

From `~/.gophermind/local/repos/linkcheck-v1-20261104-r001/.gophermind/linkcheck-v1-20261104-r001/_state/llm/50.json`, take `messages[2].content` (the assistant reply), decode its `test_file` field with Go or Python, and write it verbatim to the testdata file. It contains a line `},\n\t\t{` where `\n` is a literal backslash and `n`. Confirm with `grep -c '\\n' testdata/escape-glitch/fn_follow_test.go.txt` (at least 1). The file is the model's output and holds no brief text.

- [ ] **Step 2: Write the test**

```go
func TestEscapeGlitchFromLinkcheckIsAbsorbedAndOnlyNextIsRefused(t *testing.T) {
	src := string(mustRead(t, filepath.Join("testdata", "escape-glitch", "fn_follow_test.go.txt")))
	fixed := nudgeSyntax(unescapeQuotedSource(src))
	if !parsesAsGo(fixed) {
		t.Fatal("the glitched file does not parse after the existing repairs")
	}
	c := followContract() // package check, func follow(ctx context.Context, client *http.Client, url string, depth int) (string, string, string), file internal/check/follow.go
	err := compileCheckTestFile(c, "internal/check/fn_follow_test.go", fixed, nil)
	if err == nil || !strings.Contains(err.Error(), "declared and not used: next") || strings.Contains(err.Error(), "line 108") {
		t.Fatalf("refusal = %v, want exactly the unused next", err)
	}
}
```

`followContract()` is a small helper building a `*contract.Contracts` like `compileCheckContract()` does. If the go tool is absent the test skips (follow `TestCompileCheckSkipsWithoutGo`).

- [ ] **Step 3: Run it**

Run: `cd gophermind-lib && go test -count=1 -run TestEscapeGlitch ./build/planner`
Expected: PASS without any production change. If it fails because the glitch is not absorbed, the implementer reports DONE_WITH_CONCERNS with the parse error; the fix is then scoped by the controller (it is not in this task).

- [ ] **Step 4: Commit**

```bash
git add gophermind-lib/build/planner/testdata/escape-glitch gophermind-lib/build/planner/escape_glitch_test.go
git commit -m "test(planner): pin the doubled-escape reply from the linkcheck run"
```

---

### Task 3: Router retries stop copying the last reply

**Files:**
- Modify: `gophermind-lib/build/router/parsed.go` (`CallParsed`, `withParseError`)
- Test: `gophermind-lib/build/router/router_test.go`

**Interfaces:**
- Consumes: `CallParsed(ctx, info CallInfo, req provider.Request, parse func(string) error) (Result, error)`, `withParseError(req, reply, perr)`, `markMalformed`, `soleUsable`.
- Produces: `withParseError` unchanged for retry 1; new `withErrorNote(req provider.Request, errs []string) provider.Request` for retries 2..N: the original messages plus one user message `Earlier attempts could not be used. Do not repeat them. Errors so far:\n- e1\n- e2 ...` capped at 600 bytes (keep whole lines, drop the oldest first). Distinct errors only (string equality). A reply whose sha256 equals an earlier failed reply's is treated as the fresh-form trigger even on retry 1.

- [ ] **Step 1: Write the tests**

```go
func TestCallParsedSecondRetryDropsTheBadReply(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call <= 2 { return provider.Response{Text: "bad" + strconv.Itoa(call), Model: r.Model}, nil }
		return provider.Response{Text: "good", Model: r.Model}, nil
	}, nil, nil, nil)
	res, err := g.r.CallParsed(context.Background(), info(router.TierStrong, router.ScopeNode), req("write JSON"), rejectBad)
	if err != nil || res.Text != "good" { t.Fatalf("%+v %v", res, err) }
	second := g.mini.Requests()[1].Messages // retry 1: conversational
	third := g.mini.Requests()[2].Messages  // retry 2: fresh form
	if len(second) < 3 || second[len(second)-2].Content != "bad1" { t.Errorf("retry 1 should carry the bad reply: %+v", second) }
	for _, m := range third { if m.Content == "bad1" || m.Content == "bad2" { t.Errorf("retry 2 carries a bad reply: %+v", third) } }
	last := third[len(third)-1].Content
	if !strings.Contains(last, "Errors so far") || !strings.Contains(last, "not JSON") { t.Errorf("retry 2 note = %q", last) }
}

func TestCallParsedIdenticalReplyGoesStraightToTheFreshForm(t *testing.T) {
	// call 1 and call 2 return the same text; call 2 must have been fresh-form already? No: call 2 is retry 1 (conversational).
	// After call 2 repeats call 1 byte for byte, call 3 is fresh form (it would be anyway); assert call 3 carries no reply
	// and that the note says the reply was repeated.
}

func TestCallParsedErrorNoteIsCapped(t *testing.T) {
	// max_parse_retries 5 in the rig's config; every reply fails with a distinct 300-byte error; the note in every request stays under 600 bytes and the call count is 6.
}
```

Write the second and third tests fully, in the style of `TestCallParsedSingleModelChainSucceedsOnTheSecondRetry`. Keep `TestCallParsedRetriesTheSameModelOnceWithTheParseError`, `TestMaxParseRetriesIsTwo` and the multi-model tests passing as they are (retry 1 is unchanged).

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test -count=1 -run 'TestCallParsed' ./build/router`
Expected: the three new tests FAIL (retry 2 still carries the reply).

- [ ] **Step 3: Implement in `CallParsed`**

Keep an `errs []string` of distinct parse errors and a `seen map[string]bool` of reply hashes. Retry 1 uses `withParseError` unless the reply's hash was seen, in which case `withErrorNote` with the error `the reply was identical to an earlier one` appended. Retries 2..N use `withErrorNote(req, errs)`. The ledger marking and the exclusion path stay as they are.

- [ ] **Step 4: Run the router package**

Run: `cd gophermind-lib && go test -count=1 -race ./build/router && go vet ./build/router`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/router/parsed.go gophermind-lib/build/router/router_test.go
git commit -m "fix(router): later parse retries carry the error list, not the bad reply"
```

---

### Task 4: A test-writer node gets three rounds

**Files:**
- Modify: `gophermind-lib/build/planner/testwriter.go` (`testwriterState`, `testwriter` loop ~line 120, `writeTests` ~line 498)
- Test: `gophermind-lib/build/planner/e2e_test.go` (beside `TestABadTestFileIsRefusedAndRetried`)
- Test fixture: a variant built with `variant(t, map[string]string{...})` as that test does

**Interfaces:**
- Consumes: `router.ChainExhausted` (with `ParseErr`), `writeJSON`, `readJSON`, `p.callAsking`, `renderTestwriterBrief`.
- Produces: `testwriterState.Rounds map[string]nodeRounds` with `type nodeRounds struct { Done int `json:"done"`; Errors []string `json:"errors"` }`; `const testwriterRounds = 3`; `writeTests` takes the rounds into account: when `callAsking` returns a `*router.ChainExhausted` with a non-nil `ParseErr` and `Rounds[id].Done < testwriterRounds-1`, it records the error (safeText, 300 bytes), increments `Done`, saves the state, and calls again with the prompt plus `\n\nEarlier rounds for this node failed; their errors were:\n- ...` (distinct errors, 600-byte cap). After the last round the original error is returned.

- [ ] **Step 1: Write the tests**

The planner test rig's router uses the default `MaxParseRetries` (2), so one router round is 3 calls.

```go
func TestATestWriterNodeGetsAFreshRoundAfterTheChainIsExhausted(t *testing.T) {
	bad := `{"tests": [], "test_file": "package greet\n"}` // too few tests: refused every time
	good := string(mustRead(t, filepath.Join(greeter, "testwrite.fn-greet.txt")))
	dir := variant(t, map[string]string{
		"testwrite.fn-greet.txt": bad, "testwrite.fn-greet.2.txt": bad, "testwrite.fn-greet.3.txt": bad,
		"testwrite.fn-greet.4.txt": good,
	})
	g := newRig(t, approving(), dir)
	g.mustPlan(planner.Options{})
	reqs := g.fake.Requests() // the fourth testwrite:fn-greet request is round 2
	var fnGreet []provider.Request
	for _, r := range reqs { if planner.StageOf(r) == "testwrite:fn-greet" { fnGreet = append(fnGreet, r) } }
	if len(fnGreet) != 4 { t.Fatalf("fn-greet calls = %d, want 4 (3 in round 1, 1 in round 2)", len(fnGreet)) }
	last := fnGreet[3].Messages[len(fnGreet[3].Messages)-1].Content
	if !strings.Contains(last, "Earlier rounds for this node failed") || !strings.Contains(last, "tests") { t.Errorf("round 2 prompt = %q", last) }
	st := g.read("_state/testwriter.json")
	if !strings.Contains(string(st), `"rounds"`) { t.Error("rounds are not recorded") }
}

func TestATestWriterNodeFailsAfterThreeRounds(t *testing.T) {
	// nine bad replies (files .txt and .2.txt to .9.txt); plan fails with "node fn-greet" and the last ParseErr text; _state/testwriter.json shows rounds.fn-greet.done == 2 and no file written.
}

func TestTestWriterRoundsSurviveAResume(t *testing.T) {
	// first plan: six bad replies then the process is "killed": use StopAfter? No. Instead: run with files .txt to .6.txt bad and .7.txt good, but between round 1 and 2 nothing can be interrupted deterministically.
	// Simplest deterministic form: write _state/testwriter.json by hand after a plan that stopped at approve (StopAfter: "approve"): rounds.fn-greet.done = 2, errors = ["x"]; then resume with three bad replies: the plan fails without a fourth call (the bound counts the recorded rounds).
}
```

Write the second and third fully.

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test -count=1 -run 'TestATestWriterNode|TestTestWriterRounds' ./build/planner`
Expected: FAIL (today the first exhausted chain fails the plan).

- [ ] **Step 3: Implement**

In `writeTests`, wrap the `p.callAsking(...)` call in a loop over rounds as described in Interfaces. The round state is saved before the next call so a crash between rounds is counted. The `StripReply`/parse closure is unchanged.

- [ ] **Step 4: Run the planner tests that touch the test writer**

Run: `cd gophermind-lib && go test -count=1 -run 'TestWriter|TestATestWriter|TestABadTestFile|TestTestWriterRounds|TestTheTestWriter' ./build/planner && go vet ./build/planner`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/testwriter.go gophermind-lib/build/planner/e2e_test.go
git commit -m "feat(planner): a test-writer node gets three rounds before it fails the run"
```

---

### Task 5: Vault passphrase and placeholder secrets for the local service

**Files:**
- Modify: `scripts/local-service.sh` (the `install` function; the `run-server.sh` heredoc at lines ~103 to 119)
- Modify: `docs/SERVER-SETUP.md` or the doc that describes the local service (find it with `grep -rl local-service.sh docs`)
- Test: `gophermind-lib/build/execenv/execenv_test.go` (pin that the passphrase never reaches a built program)

**Interfaces:**
- Consumes: `vault.PassphraseEnv = "GOPHERMIND_VAULT_PASSPHRASE"`, `gophermind brief vault set <NAME>` (value read from stdin, passphrase from the environment), `vault.HarnessScope`, `execenv.Build(in Inputs) ([]string, error)`.
- Produces: `~/.gophermind/local/vault-passphrase` (mode 600, random 32 hex bytes, made by `install` when missing, never printed); `run-server.sh` reads it with `IFS= read -r GOPHERMIND_VAULT_PASSPHRASE < "$BASE/vault-passphrase"` guarded by `[ -s ... ]` so a missing file leaves the variable unset and the server still starts; `install` sets a placeholder for each secret the example briefs declare when it is not in the vault yet.

- [ ] **Step 1: Pin the executor environment**

```go
func TestBuildNeverCopiesTheVaultPassphraseFromTheProcessEnvironment(t *testing.T) {
	t.Setenv("GOPHERMIND_VAULT_PASSPHRASE", "hunter2")
	env, err := Build(Inputs{Secrets: []string{"JWT_SIGNING_KEY"}, Resolve: func(string) (string, bool) { return "x", true }})
	if err != nil { t.Fatal(err) }
	for _, kv := range env { if strings.HasPrefix(kv, "GOPHERMIND_VAULT_PASSPHRASE=") { t.Fatal("the passphrase reached the build environment") } }
}
```

Adjust `Inputs` fields to the real struct (read `execenv.go`). Run: `cd gophermind-lib && go test -count=1 -run TestBuildNever ./build/execenv`. Expected: PASS (Build never reads os.Environ).

- [ ] **Step 2: Change the install script**

In `install`: after the token block, `if [ ! -s "$BASE/vault-passphrase" ]; then (umask 077; openssl rand -hex 32 > "$BASE/vault-passphrase"); fi; chmod 600 "$BASE/vault-passphrase"`. In the heredoc, after the token read: `if [ -s "$BASE/vault-passphrase" ]; then IFS= read -r GOPHERMIND_VAULT_PASSPHRASE < "$BASE/vault-passphrase"; export GOPHERMIND_VAULT_PASSPHRASE; fi`. After the binary is built and before the agent is loaded, for each name in the list `WEBHOOK_SIGNING_KEY SLACK_WEBHOOK_URL LISTEN_ADDR LOG_LEVEL RELAY_TIMEOUT LEDGER_API_TOKEN LEDGER_BASE_URL DATA_DIR BATCH_SIZE JWT_SIGNING_KEY SMTP_PASSWORD ADMIN_BOOTSTRAP_TOKEN DATABASE_PATH` (the secrets declared in `examples/briefs/06-hookrelay.md`, `07-ledger-sync.md`, `08-taskboard.md`; read them to confirm), run `printf '%s\n' "placeholder-$name-not-a-real-secret" | GOPHERMIND_VAULT_PASSPHRASE=... "$BASE/bin/gophermind" brief vault set "$name"` only when `brief vault list` does not already list it. Pass the passphrase with a read loop into the environment of that one command, never on argv. Confirm with `bash -n scripts/local-service.sh`.

- [ ] **Step 3: Document**

Add a short "Secrets for the example briefs" paragraph to the local service doc: where the passphrase file lives, that the values are placeholders, and how to replace one (`brief vault set NAME`).

- [ ] **Step 4: Verify the script without installing**

Run: `bash -n scripts/local-service.sh && shellcheck scripts/local-service.sh 2>/dev/null | head` (shellcheck optional). Do not run `install`. Expected: no syntax error.

- [ ] **Step 5: Commit**

```bash
git add scripts/local-service.sh gophermind-lib/build/execenv/execenv_test.go docs/<the doc>
git commit -m "feat(local-service): vault passphrase and placeholder secrets for the example briefs"
```

---

### Task 6: Bench script and the first measurement

**Files:**
- Create: `scripts/testwriter-bench.sh`
- Modify: `docs/brief-library.md` or `docs/SERVER-SETUP.md` (one paragraph on the bench)

**Interfaces:**
- Consumes: `gophermind brief resume <run-id> [--yes] [--gate file]`, the run record at `~/.gophermind/runs/<id>.json`, the run folder's `_state/planner_error.json` and `_state/status.json`, the environment of `~/.gophermind/local/run-server.sh` (same variables, minus `exec`).
- Produces: `scripts/testwriter-bench.sh <run-id>...`: builds a fresh `gophermind` binary into a `mktemp -d` folder, refuses to start while `curl -fsS http://127.0.0.1:8090/app/api/briefs` reports a non-empty `active_run_id`, exports the same `GOPHERMIND_*` and offline variables the service uses (read the values from `run-server.sh` with grep, never source it), reads the vault passphrase file into the environment with a read loop, then for each id runs `brief resume <id> --yes --gate file` with a 40-minute timeout, and prints one line per id: `id  before=<stage of planner_error.json>  after=<last stage line of run.log>  result=<ok|failed: first 160 chars of the error>`. Any `rm -rf` of the temp folder uses the guarded form.

- [ ] **Step 1: Write the script and check it**

Run: `bash -n scripts/testwriter-bench.sh`. Expected: no error.

- [ ] **Step 2: Measure linkcheck**

The server must be idle (check `/app/api/briefs` shows no active run; the controller confirms before this task is dispatched). Run: `scripts/testwriter-bench.sh linkcheck-v1-20261104-r001` and paste the printed line into the report. Expected: `after` is past `testwriter` (the plan reached `approve` or planning finished). If it still fails at the test writer, the report carries the first refusal texts of the node from `_state/refusals.jsonl` so the next fix is evidence-based; the task is still DONE.

- [ ] **Step 3: Full package tests**

Run: `cd gophermind-lib && go build ./... && go vet ./... && gofmt -l . && go test -count=1 -race -timeout 30m ./build/planner ./build/router ./build/execenv`
Expected: PASS. The planner package takes 5 to 10 minutes.

- [ ] **Step 4: Commit**

```bash
git add scripts/testwriter-bench.sh docs/<the doc>
git commit -m "feat(scripts): bench that resumes failed runs through the test writer"
```
