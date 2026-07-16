# Mobile / remote-control protocol (`gophermind serve`)

This is the precise, app-facing reference for driving a `gophermind serve`
process from a phone (or any remote client): how to run it, how to reach it,
and the exact HTTP + SSE contract. **This is the contract the SwiftUI app in
`ios/` is built against — it is generated from, and verified against, the
actual server code** (`cmd/gophermind/webhook.go`, `session_serve.go`,
`sse.go`, `approval.go`, `apns.go`). If server behavior and this doc ever
disagree, the code is right and this doc has a bug — please file one.

## Running serve for a phone

```
GOPHERMIND_SERVE_TOKEN=<a-strong-random-token> \
GOPHERMIND_SERVE_APPROVAL=remote \
gophermind serve
```

| Variable | Required | Purpose |
|---|---|---|
| `GOPHERMIND_SERVE_TOKEN` | **yes** | Bearer token for every request. `serve` refuses to start without it — an unauthenticated webhook can run shell commands and write files. |
| `GOPHERMIND_SERVE_ADDR` | no | Listen address (default `:8080`, all interfaces). |
| `GOPHERMIND_SERVE_APPROVAL` | no | `remote` routes gated tool calls to the phone via an `approval-needed` SSE frame + `POST /session/{id}/approve`, instead of the server's normal local auto-approve/ask policy. Anything else (including unset) keeps the server's local approval behavior. |
| `GOPHERMIND_SERVE_APPROVAL_TIMEOUT_S` | no | Seconds to wait for the phone's decision before auto-denying (default 300 = 5 minutes). Non-positive/invalid values fall back to the default. |
| `GOPHERMIND_SERVE_HMAC_SECRET` | no | When set, every request to `/run`, `/run/stream`, `/session*`, and `/devices` must also carry a valid `X-Hub-Signature-256` (see Auth below). |
| `GOPHERMIND_SERVE_RATE` | no | Requests/minute, shared across every task-running endpoint, keyed by the caller's bearer token. Exceeding it returns `429 rate limit exceeded`. |
| `GOPHERMIND_SESSION_KEY` | no | Passphrase that encrypts session history at rest. If set when a session is created, that session can only be resumed with the same key set. |
| `GOPHERMIND_APNS_KEY_P8` | no | Path to an Apple Push `.p8` auth key. Push is fully disabled (no-op, never errors a turn) unless this **and** `_KEY_ID`, `_TEAM_ID`, `_BUNDLE_ID` are all set. |
| `GOPHERMIND_APNS_KEY_ID` | no | APNs auth key ID. |
| `GOPHERMIND_APNS_TEAM_ID` | no | Apple Developer Team ID. |
| `GOPHERMIND_APNS_BUNDLE_ID` | no | iOS app bundle ID (used as `apns-topic`). |
| `GOPHERMIND_APNS_ENV` | no | `prod` to use `api.push.apple.com`; anything else (including unset) uses the sandbox host `api.sandbox.push.apple.com`. |

The startup log line confirms what's active, e.g.:

```
gophermind serving on :8080 (POST /run, /run/stream; /healthz /readyz)
  sessions: POST /session, POST /session/{id}/stream, POST /session/{id}/approve, POST /devices (remote approval, APNs configured)
```

## Reaching it from the phone

- **Same LAN**: point the app at `http://<your-machine-ip>:8080`. Simplest for
  local testing.
- **Tailscale** (recommended for anywhere-access): put the machine and phone
  on the same tailnet and use the machine's Tailscale IP/hostname. Traffic
  stays on an encrypted mesh without exposing a port to the internet.
- **Tunnel** (e.g. `cloudflared`, `ngrok`): exposes the port publicly with
  TLS termination — use this only with a strong `GOPHERMIND_SERVE_TOKEN` and
  ideally `GOPHERMIND_SERVE_HMAC_SECRET` set, since it's now internet-reachable.

**iOS ATS/HTTPS note**: App Transport Security blocks plain `http://` by
default. For LAN development the native app carries a scoped ATS local-network
exception so it can talk to plain `http://<lan-ip>:8080`. For anything beyond
the LAN (Tailscale over the open internet, or a tunnel), use `https://` — put
a TLS-terminating proxy (Tailscale Serve, `cloudflared`, or similar) in front
of `gophermind serve`, which itself only speaks plain HTTP.

## Auth (every endpoint below)

- `Authorization: Bearer <GOPHERMIND_SERVE_TOKEN>` — required, constant-time
  compared. Missing/wrong token → `401 unauthorized`.
- `X-Hub-Signature-256: sha256=<hex>` (or bare `<hex>`) — required **only**
  when `GOPHERMIND_SERVE_HMAC_SECRET` is set. HMAC-SHA256 of the raw request
  body under that shared secret, constant-time compared. Missing/wrong
  signature → `401 bad signature`.

## Endpoint reference

### `POST /session` → create/register a session

Body: optional JSON `{"id": "<caller-chosen id>"}`. IDs must match
`^[A-Za-z0-9._-]+$` (validated the same way session ids are validated
everywhere else); an invalid id is `400`. Omit the body (or the field) to get
a freshly generated id.

Response `200`: `{"id": "<id>"}`

### `POST /session/{id}/stream` → run one turn, streamed

Body: **raw text**, not JSON — the task/prompt string. An empty (post-trim)
body is `400 empty task`. An invalid `{id}` is `400`. A second request for an
`{id}` that already has a turn in flight is `409 session busy` (turns on the
same session id are serialized; different ids run concurrently).

Response: `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
`Connection: keep-alive`. The connection is held open for the duration of the
turn; the server resumes the session's saved history if `{id}` already
exists, else starts fresh, and saves it back at the end of the turn.

#### SSE frame format

Every frame is `event: <name>\ndata: <line>\n[data: <line>\n...]\n\n` — an
optional `event:` line, one or more `data:` lines (multi-line data is sent as
consecutive `data:` lines, and any `\r\n`/`\r` in the payload is normalized to
`\n` first so payload content can never inject a fake event boundary), then a
blank line.

#### Typed events

| Event | `data` payload | When |
|---|---|---|
| `token` | Raw text chunk (not JSON) — a streamed token/delta of the model's output. | Zero or more times, as the model streams. |
| `assistant` | Raw text (not JSON) — the assistant's final prose for the turn. | At most once per turn. |
| `tool_call` | JSON `{"name":"<tool>","args":"<raw args string>"}` | Once per tool invocation the agent makes. |
| `tool_result` | JSON `{"name":"<tool>","text":"<result text>"}` | Once per tool invocation, after it runs. |
| `usage` | JSON of the running per-session totals: `{"PromptTokens":<int>,"CompletionTokens":<int>,"TotalTokens":<int>,"CostUSD":<float>}` (raw Go struct field names — no JSON tags — so the keys are capitalized exactly as shown). | After usage is available for the turn. |
| `approval-needed` | JSON `{"approval_id":"<id>","tool":"<tool>","args":"<raw args string>"}` | Only when `GOPHERMIND_SERVE_APPROVAL=remote` and the agent hits a tool call gated by policy. The turn blocks until `POST /session/{id}/approve` resolves `approval_id`, `GOPHERMIND_SERVE_APPROVAL_TIMEOUT_S` elapses (auto-deny), or the client disconnects (auto-deny). If APNs is configured, a push alert (`{"session_id":..., "approval_id":...}` in the payload, title "Approval needed") is also sent to every registered device, best-effort. |
| `error` | Plain text `run failed` (generic — the real error is logged server-side only, never disclosed to the client). | At most once, only if the turn itself errored. |
| `done` | Empty. | **Always** sent exactly once, last — including after an `error` frame. Use it as the definitive end-of-turn signal. |

### `POST /session/{id}/approve` → resolve a pending remote approval

Body: `{"approval_id":"<id>","approved":true|false}`.

- `200 {"ok":true}` — resolved; the waiting turn resumes immediately with
  that decision.
- `404 {"error":"no pending approval"}` — unknown id, already resolved, or
  already timed out/cancelled.
- `400` — malformed body or missing `approval_id`.

### `GET /session` → list saved sessions

Response `200`: JSON array (never `null`, `[]` when empty) of:

```json
{"ID":"...","Path":"...","Size":123,"ModTime":"2026-01-01T00:00:00Z","Messages":4,"Title":"first user message, truncated"}
```

(Raw Go struct field names — no JSON tags — so the keys are capitalized.)

### `DELETE /session/{id}` → remove a saved session

`204 No Content` on success. `400` for an invalid id, `404` if the session
doesn't exist / removal fails.

### `POST /devices` → register a push token

Body: `{"device_token":"<APNs device token>","platform":"ios"}`. Tokens are
deduped; re-registering the same token is a no-op that still returns `200`.

- `200 {"ok":true}`
- `400` — empty token or malformed body.

## Errors and status codes at a glance

| Status | Meaning |
|---|---|
| `400` | Bad/empty body, invalid session id, malformed JSON. |
| `401` | Missing/wrong bearer token, or missing/wrong HMAC signature. |
| `404` | Unknown session (`DELETE`) or unknown/resolved approval id (`approve`). |
| `405` | Wrong HTTP method for the route. |
| `409` | `/session/{id}/stream` called while a turn on that id is already in flight. |
| `429` | `GOPHERMIND_SERVE_RATE` exceeded. |
