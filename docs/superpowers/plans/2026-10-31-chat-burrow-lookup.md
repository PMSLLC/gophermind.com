# Chat Burrow lookup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The web chat agent can ask Burrow whether a verified function with a given contract already exists, read-only, exact intent-key match.

**Architecture:** A small stdlib HTTP client for `POST /v1/lookup` (legacy mode, no vectors) lives in `serve` next to the other chat tools and backs one tool, `burrow_lookup`. The tool exists only when `gophermind-server` is given a Burrow URL and token file. No import of Burrow's private Go module.

**Tech Stack:** Go (`gophermind-lib/serve`, `gophermind-server`), `scripts/local-service.sh`.

**Spec:** Agreed in chat 2026-10-31 (small change, no separate design doc). Burrow API: `~/OtherProjects/PMSLLC/burrow/docs/api.md` (sections "Authentication", "Type grammar", "POST /v1/lookup", "Response 200, legacy mode", "Errors"). Context: `docs/superpowers/specs/2026-10-30-chat-brief-tools-design.md` (this was its deferred goal 5) and `docs/specs/gopher-burrow-gophermind-shared-spec.md` section 6.1.

## Global Constraints

- No em dashes or emojis in anything written.
- Read-only tool, not in `safety.Gated`. It sends no vectors, so Burrow answers exact intent-key only (`outcome` `exact` or `none`); the tool says so in its Description.
- Contract sent to Burrow has `effects: []`. Burrow lookups never block on effects but the tool does not offer effects.
- The bearer token is read from the token file on each call, never logged, never returned, never in an error. Redirects are not followed (the token must not be forwarded). Response body read is capped at 1 MiB; output goes through `chatTools.cleanWith` (8 KiB cap, redaction).
- The tool is registered only when both `GOPHERMIND_BURROW_URL` and `GOPHERMIND_BURROW_TOKEN_FILE` are set on `gophermind-server`.
- Commit per task with explicit `git add` paths; no push, deploy or install until John asks. Run from `gophermind-lib`: `go build ./... && go vet ./... && gofmt -l . && go test -race ./serve ./tools ./safety ./agent` (skip the full-lib `go test ./...`: `build/executor` hangs here). From `gophermind-server`: `go test -race ./...`.

## Review Focus

1. Burrow unreachable, 401, 400 (bad contract), 5xx, timeout: each gives one plain sentence, none leaks the token or the token path.
2. A redirect response from Burrow is not followed.
3. A hit with a very large rendering body: output capped, still shows contract, key and parameter order.
4. Model supplies a malformed type (`list<`), empty verb, or too many inputs: tool rejects locally or relays Burrow's 400 text, never panics.
5. Ambiguous arguments (case-variant or unknown keys): rejected like the write tools do (reuse the strict-args helper from `chattools_write.go`).

## File Structure

- `gophermind-lib/serve/burrowclient.go` (create): `burrowClient`, `lookup`.
- `gophermind-lib/serve/chattools_burrow.go` (create): the `burrow_lookup` tool.
- `gophermind-lib/serve/chattools.go`, `webhook.go` (modify): `chatTools.burrow`, `Options.BurrowURL/BurrowTokenFile`, registration.
- `gophermind-server/main.go`, `briefs_wiring.go`, `server.go` (modify): config and wiring, guidance sentence.
- `scripts/local-service.sh`, `CHANGELOG.md` (modify).

---

### Task 1: Burrow client

**Files:** Create `gophermind-lib/serve/burrowclient.go`; Test `gophermind-lib/serve/burrowclient_test.go`

**Interfaces:**
- Produces: `type burrowClient struct{...}`; `func newBurrowClient(baseURL, tokenFile string) *burrowClient`; `func (c *burrowClient) lookup(ctx context.Context, contract map[string]any, lang string) (burrowLookup, error)`; `type burrowLookup struct{ Outcome, Key string; Contract json.RawMessage; Rendering *struct{ Lang string; Version int; Body string; VerifiedAt string }; Eligible, AwaitingConfirmation bool }`; `type burrowError struct{ Kind string; Msg string }` with `Error()`, kinds `unreachable`, `unauthorized`, `bad_request`, `server`, `timeout`, `other`.

- [ ] **Step 1: Write failing tests** against `httptest.Server` fakes: `TestBurrowLookupHit` (request is POST `/v1/lookup` with `Authorization: Bearer <token from file>`, `Content-Type: application/json`, body has `contract` and `lang` and NO `vectors` key; hit decodes `Outcome=="exact"`, rendering body), `TestBurrowLookupMiss` (`outcome":"none"`), `TestBurrowLookupErrors` (401 -> kind `unauthorized`; 400 with the API's error envelope -> kind `bad_request` carrying Burrow's message; 500 -> `server`; closed server -> `unreachable`; slow server with 50ms timeout -> `timeout`), `TestBurrowTokenNeverInError` (token string absent from every error message), `TestBurrowNoRedirect` (302 to another httptest server: the second server never sees a request, result is kind `other`), `TestBurrowBodyCap` (2 MiB response -> error, not memory growth), `TestBurrowTokenFileMissing`.
- [ ] **Step 2: Run, expect compile failure.** `go test ./serve -run Burrow`
- [ ] **Step 3: Implement** with `http.Client{Timeout: 15s, CheckRedirect: ErrUseLastResponse}`, token read per call (`strings.TrimSpace`), body via `io.LimitReader(…, 1<<20)` and an explicit over-limit error, error envelope decode per the API's "Error envelope" section (read it; take the message field), unknown JSON fields tolerated (do not make this client strict).
- [ ] **Step 4: Run, expect PASS** (`-race`).
- [ ] **Step 5: Commit** `git add gophermind-lib/serve/burrowclient.go gophermind-lib/serve/burrowclient_test.go`

### Task 2: The `burrow_lookup` tool

**Files:** Create `gophermind-lib/serve/chattools_burrow.go`; Modify `gophermind-lib/serve/chattools.go` (field `burrow *burrowClient`, register in `all()` only when non-nil), `gophermind-lib/serve/webhook.go` (`Options.BurrowURL`, `Options.BurrowTokenFile`; build the client when both set and pass it to `chatTools`); Test `gophermind-lib/serve/chattools_burrow_test.go`

**Interfaces:**
- Consumes: `newBurrowClient`, `lookup`, `burrowError` (Task 1); the strict-args helper and `cleanWith` from the existing chat tool files (read `chattools_write.go` for the key-set/duplicate-case check and reuse it, extending it if it is private to the write tools).
- Produces: tool `burrow_lookup` with args `verb` (string), `object` (string), `inputs` (array of `{name,type}`), `outputs` (array of type strings), optional `failures` (array of strings), optional `traits` (array of strings), optional `lang` (default `go`). Output on hit: `found: yes`, `key`, canonical contract line, `parameter order: <names in stored contract order>`, `verified at`, `language`, `eligible`, then the rendering body (capped). Miss: `found: no (exact contract only; similar functions are not searched without test vectors)`. `awaiting_confirmation` true: say a generated body exists but is not confirmed and is not shown. Errors: one sentence per `burrowError` kind (unauthorized says the Burrow token was rejected, unreachable says Burrow did not answer, bad_request relays Burrow's message).

- [ ] **Step 1: Write failing tests** with a fake Burrow: `TestBurrowLookupToolHit`, `TestBurrowLookupToolMiss`, `TestBurrowLookupToolBodyCapped` (output under 8 KiB, still contains key and parameter order), `TestBurrowLookupToolErrorSentences` (each kind), `TestBurrowLookupToolRejectsBadArgs` (case-variant key, unknown key, empty verb, missing outputs), `TestBurrowLookupToolNotRegisteredWithoutConfig` (`all()` has no `burrow_lookup` when `burrow == nil`), `TestBurrowLookupNotGated` (`safety.Gated("burrow_lookup")` is false).
- [ ] **Step 2: Run, expect FAIL. Step 3: Implement. Step 4: Run, expect PASS (`-race`). Step 5: Commit** with explicit paths.

### Task 3: Server wiring, local install, guidance, changelog

**Files:** Modify `gophermind-server/main.go` (serverConfig fields `BurrowURL`, `BurrowTokenFile` from env `GOPHERMIND_BURROW_URL` and `GOPHERMIND_BURROW_TOKEN_FILE`, following how `BriefsDir` is read), `gophermind-server/briefs_wiring.go` (copy into `serve.Options`), `gophermind-server/server.go` (guidance sentence for `burrow_lookup`, appended only when the tool is in `ChatToolsFrom(ctx)`), `scripts/local-service.sh` (write the two exports into `run-server.sh` when `GM_BURROW_URL`/`GM_BURROW_TOKEN_FILE` are set at install time; default them to `http://127.0.0.1:8085` and `$HOME/OtherProjects/PMSLLC/burrow/.local/token` when that file exists), `CHANGELOG.md`; Test `gophermind-server/*_test.go`

- [ ] **Step 1: Write failing tests:** `TestBurrowToolWiredFromConfig` (server built with a fake Burrow URL and a temp token file: a new session's model request lists `burrow_lookup` and the guidance mentions it; without the settings neither appears; isolate `GOPHERMIND_CONFIG_DIR` with `t.TempDir()`), and a parse test that the env vars land in `serverConfig`.
- [ ] **Step 2: Run, expect FAIL. Step 3: Implement.** Guidance sentence (no em dashes): use `burrow_lookup` before suggesting a new function be built; it matches exact contracts only, so draft the contract in Burrow's type grammar and say plainly when a miss may only mean a different spelling.
- [ ] **Step 4: Verify the script** without installing: `bash -n scripts/local-service.sh` and a dry read of the generated heredoc text; do not run `install`.
- [ ] **Step 5: Run the verification commands; expect clean. Commit** with explicit paths.
- [ ] **Step 6 (controller, after review, only with John's go):** `bash scripts/local-service.sh install`, then ask the live chat for a lookup of `Count Text.Words` (contract in the Burrow docs example) and confirm a hit; and a made-up contract and confirm a miss.
