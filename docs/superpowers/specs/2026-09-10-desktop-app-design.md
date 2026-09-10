# GopherMind Desktop Application - Design

**Date:** 2026-09-10
**Status:** Approved
**Depends on:** nothing hard; sequenced after `2026-09-10-free-llm-providers-design.md`

> Style note: this document uses plain hyphens, not em dashes, per the global
> writing rule for newly created documents.

## Problem

GopherMind is terminal-only on the desktop. The harness is reachable three ways
today: the TUI, `gophermind run/ask`, and `gophermind serve` over HTTP. The third
is already consumed by a real GUI - the iOS app - but nothing consumes it on a
Mac, Windows, or Linux desktop.

Two things the terminal does badly and a window does well:

- **Approvals.** `POST /session/{id}/approve` gates a tool call on a human
  decision. In a TUI that decision is made against a wrapped, scrolling diff. A
  window can show the actual diff, side by side, before the user commits to it.
- **Instruments.** The free-usage odometer and trip meters from the companion
  spec are squeezed into a status line. They want a dash.

## The central finding: the harness is already headless

`gophermind serve` exposes sixteen routes, and the iOS app (3,584 lines of
mostly SwiftUI) already drives all of them in production:

```
POST   /session                      GET  /session
POST   /session/{id}/stream          GET  /session/{id}/messages
POST   /session/{id}/approve         GET  /session/{id}/config
PATCH  /session/{id}                 DELETE /session/{id}
GET    /models                       GET  /modes
POST   /devices                      POST /run, /run/stream
GET    /healthz, /readyz, /metrics
```

So the desktop app is a **third client on a proven API**, not a second
implementation of the harness. Nothing about agent loops, tool dispatch,
approvals, or session persistence is rewritten.

## Decision: the frontend always speaks HTTP

Embedded and remote are the same code path; only the base URL differs.

The rejected alternative was Wails bindings for the local case plus `fetch` for
the remote case. That means writing and testing every call twice, and it means
the two modes can drift. Speaking HTTP in both directions gives:

- Zero new handler code. The sixteen tested endpoints are the API.
- SSE streaming via plain `EventSource`, exactly as iOS does it.
- One contract shared by CLI, iOS, and desktop, so an endpoint change cannot
  silently break one client.

**Cost:** a loopback listener in embedded mode. Mitigated by binding
`127.0.0.1` only, port `0` (kernel-assigned), a cryptographically random token
generated per launch, held in memory, never persisted and never printed.

## Phase 0 (prerequisite): extract `internal/serve`

This is the real cost of the embedding decision and it is not optional.

`runServe` (`cmd/gophermind/webhook.go:227`) builds its mux inline and ends in
`http.ListenAndServe(addr, mux)`. It lives in `package main`, together with
1,391 lines of serve-path code that `desktop/` therefore cannot import:

| File | Lines |
|---|---|
| `cmd/gophermind/apns.go` | 423 |
| `cmd/gophermind/session_serve.go` | 371 |
| `cmd/gophermind/webhook.go` | 308 |
| `cmd/gophermind/approval.go` | 163 |
| `cmd/gophermind/ratelimit.go` | 86 |
| `cmd/gophermind/metrics.go` | 40 |

Phase 0 moves them to `internal/serve` and splits construction from listening:

```go
// Deps is everything the mux needs, supplied by the caller.
type Deps struct { Run, Stream, SessionTurn, Approvals, Devices, Messages, ListModels ... }

func NewMux(d Deps, opt Options) (*http.ServeMux, error) // no listening
func Serve(ctx context.Context, ln net.Listener, mux *http.ServeMux) error // graceful
```

`cmd/gophermind` then calls `NewMux` + `net.Listen(serveAddr())` + `Serve`, and
`desktop/` calls `NewMux` + `net.Listen("tcp", "127.0.0.1:0")` + `Serve`.

Two things this fixes independently of the desktop app: `serve` gains graceful
shutdown (it has none today, so an in-process server could not be stopped), and
1,391 lines of HTTP handlers stop living in `package main`.

**This is a pure refactor with no behavior change.** The existing
`webhook_test.go`, `session_*_test.go`, `approval_test.go`, `ratelimit_test.go`
and `apns_test.go` move with their code and must pass unchanged. That is the
safety net, and the gate on Phase 0: same tests, same assertions, green.

Token handling is the one deliberate behavior addition. `serveToken()` refuses
to start without `GOPHERMIND_SERVE_TOKEN`, correctly - the endpoint runs shell
commands. `Options` gains an explicit `Token string` so the desktop app can
supply a generated one without setting a process-wide env var. The env path
stays the default for the CLI, and the "never unauthenticated" invariant is
preserved: `NewMux` returns an error on an empty token.

## Architecture

```
desktop/
  main.go            Wails entry; starts internal/serve on 127.0.0.1:0
  app.go             window, menus, tray, embedded-vs-remote switch
  endpoint.go        resolves base URL + token, embedded or remote
  frontend/          React + TypeScript + Vite
    src/api/         one typed client over the sixteen routes
    src/screens/     Chat, Sessions, Approvals, Settings, Usage
  wails.json
```

Wails binds exactly one Go method to the frontend: `Endpoint() {baseURL, token}`.
Everything else is HTTP. That single binding is what lets the frontend stay
transport-agnostic, and it is the only thing that has to differ between modes.

### Runtime modes

**Embedded (default).** On launch the app generates a 32-byte random token,
listens on `127.0.0.1:0`, starts the mux, and hands the frontend the resolved
URL and token. Zero configuration; the app works offline on first open.

**Remote (opt-in).** Settings accepts a base URL and token, reusing the same
bearer auth the iOS app uses, so an existing `gophermind serve` on the network
is reachable without new server-side work. The embedded server is not started in
this mode. Switching modes tears down cleanly and reconnects.

The token is stored per-platform in the OS keychain (macOS Keychain, Windows
Credential Manager, libsecret), never in a config file. The iOS app's Keychain
module is the precedent for the pattern, not shared code.

## v1 screens

1. **Chat** - streaming transcript over `EventSource`, markdown rendering, the
   session's replayed history from `GET /session/{id}/messages` on open.
2. **Sessions sidebar** - list, create, rename, delete. Four endpoints that
   already exist and are already exercised by iOS.
3. **Approvals** - the reason this is worth building. A pending tool call renders
   as a real diff view with approve and deny, posting to
   `POST /session/{id}/approve`. Keyboard-first, so it is not slower than the TUI.
4. **Settings** - profile picker including the `free-*` providers, model picker
   (`GET /models`), mode picker (`GET /modes`), and the embedded/remote switch.
5. **Usage dash** - the odometer and per-provider trip meters from the companion
   spec, drawn as instruments rather than compressed into a status line.

**Out of scope for v1:** phaseflow and `/project`, MCP server management,
multi-window, plugins, the predictive-text/bubblecomplete surface.

## Error handling

| Condition | Behavior |
|---|---|
| Embedded listener fails to bind | Window opens on an error screen with the OS error; app does not exit silently |
| Remote unreachable | Banner with the failure and a retry; the app stays usable for reading local history |
| Remote returns 401 | Prompt to re-enter the token; never auto-retry with the bad one |
| SSE connection drops mid-turn | Reconnect with backoff, then replay via `GET /session/{id}/messages` so no output is lost |
| Serve mux errors at construction | Startup fails loudly with the reason, e.g. an empty token |
| App quits with a turn in flight | Cancel the context, shut the server down gracefully, do not orphan a listener |

## Testing

**Phase 0** is gated on the moved tests passing unchanged - that is the entire
proof that the refactor is behavior-preserving. Added: `NewMux` rejects an empty
token; `Serve` returns on context cancel; two `NewMux` calls yield independent
muxes.

**Go (desktop)**: token generation is 32 bytes from `crypto/rand` and differs per
launch; the listener binds loopback only (asserted by dialing the non-loopback
interface and expecting failure); `Endpoint()` returns remote values when
configured and never starts the embedded server in that mode.

**Frontend**: Vitest over the typed API client against a mock server, covering
each of the sixteen routes plus the 401 and reconnect paths. Component tests for
the approval diff, which is the highest-risk screen: a deny must never post an
approve.

**End to end**: Playwright is already available in this environment. One smoke
test - launch embedded, create a session, send a turn, receive streamed output,
approve a tool call.

**Regression**: the CLI's `gophermind serve` behavior is unchanged, proven by the
Phase 0 test suite rather than by inspection.

## Release

`wails build` for macOS, Windows, Linux, matching the platform set the CLI
already ships. macOS produces a `.app` and dmg signed and notarized through the
existing `MACOS_SIGN_IDENTITY` / `MACOS_NOTARY_PROFILE` pipeline in the
GoReleaser configuration rather than a parallel one. The desktop app versions
independently of the CLI; they share the harness, not a release cadence.

## Risks

- **Phase 0 is a 1,391-line refactor before a single window exists.** It is
  mechanical and test-covered, but it is real work with no visible output, and it
  is the point where this project is most likely to stall. Mitigation: Phase 0
  ships and merges on its own, justified by graceful shutdown alone.
- **A loopback HTTP server is an attack surface on a shared machine.** Any local
  process can reach `127.0.0.1`. Mitigation: the per-launch random token is
  required on every route, is never written to disk, and never appears in logs or
  argv. This is strictly better than today's `GOPHERMIND_SERVE_TOKEN`, which sits
  in the environment.
- **Three clients on one API raises the cost of changing it.** Mitigation: that
  cost is already paid - iOS and the CLI both depend on these routes today. The
  desktop app makes the contract more visible, not more fragile.
- **Wails v2 pins a webview per platform.** Rendering differs between WebKit,
  WebView2, and WebKitGTK. Mitigation: keep the frontend conservative, and treat
  the Linux webview as the floor for CSS decisions.
