# MCP Client for gophermind — Design

**Date:** 2026-08-03
**Status:** Approved (Spec A of two)

## Problem

Gophermind can *serve* MCP but cannot *consume* it. `internal/mcp` is server-only:
six functions (`NewServer`, `Handle`, `callTool`, `reply`, `marshal`, `Serve`),
no HTTP, no URLs, no outbound connection of any kind. Its sole consumer is
`cmd/gophermind/main.go:945`, which pipes it over stdin/stdout.

So gophermind cannot use any third-party MCP server — neither hosted ones like
`https://mcp.pelagosnow.com/mcp` nor the large ecosystem of stdio servers
(`@modelcontextprotocol/server-filesystem`, `server-github`, and so on).

## Scope: this is Spec A of two

The approved auth model is OAuth 2.1. That is a subsystem in its own right —
`/.well-known` discovery, dynamic client registration, a browser redirect
listener, token storage, refresh. Bundling it produces one spec too large to
review and too large to land safely.

- **Spec A (this document)** — MCP client core. Ships working software, with
  static headers plus `${ENV}` expansion as the shipped `Authenticator`.
- **Spec B (next)** — OAuth 2.1, implemented behind the `Authenticator`
  interface Spec A defines. No rewrite of Spec A's code.

Spec A may be sufficient for pelagosnow on its own. A probe on 2026-08-03
returned `401` on `GET` and `403` with `cf-mitigated: challenge` on a `POST`
`initialize`. That tells us the endpoint requires credentials and sits behind a
Cloudflare challenge, but **not** which auth scheme it wants. If a bearer token
satisfies it, Spec B is unnecessary for this server.

### Known open risk

The Cloudflare managed challenge returned `403` to a non-browser client. It may
apply only to anonymous traffic and disappear once a valid `Authorization`
header is present — this is untested. If the challenge also fires for
authenticated API clients, no amount of client work fixes it; the endpoint
owner must allowlist non-browser clients. **Verify this with a real token
before starting Spec B.**

## Architecture

A new package, `internal/mcpclient`. `internal/mcp` stays server-only and is not
modified.

| File | Responsibility |
|---|---|
| `transport.go` | `Transport` interface; shared JSON-RPC envelope types |
| `stdio.go` | Stdio transport — spawn command, newline-delimited JSON over pipes |
| `http.go` | Streamable HTTP transport — POST JSON-RPC, decode JSON or SSE reply |
| `auth.go` | `Authenticator` interface; static-header implementation |
| `client.go` | Protocol: `initialize`, `notifications/initialized`, `tools/list`, `tools/call` |
| `config.go` | `ServerConfig`; load and merge global + per-project sources |
| `adapt.go` | Convert MCP tool descriptors into `[]tools.Tool` |
| `load.go` | `Load(ctx, cfg)` — the single entry point `main.go` calls |

Each file is independently testable. The seams that matter:

```go
// transport.go
type Transport interface {
    Send(ctx context.Context, msg []byte) ([]byte, error) // nil reply for notifications
    Close() error
}

// auth.go — Spec B implements this for OAuth without touching http.go
type Authenticator interface {
    Apply(req *http.Request) error
}
```

`client.go` depends only on `Transport`. `http.go` depends only on
`Authenticator`. Neither knows the other exists.

## Data flow

```
startup
  └─ mcpclient.Load(ctx, cfg)
       ├─ config.go     : read global config.json + .gophermind/*.mcp.json, merge
       └─ per server (bounded concurrency, 5s connect timeout each):
            ├─ transport : stdio.New(cmd,args,env) | http.New(url, auth)
            ├─ client    : initialize → notifications/initialized → tools/list
            └─ adapt     : each MCP tool → tools.Tool{Name: "srv__tool", Run: …}
                             └─ Run closes over the live client; calls tools/call
  └─ toolset = append(toolset, mcpTools...)   // main.go, beside plugins
  └─ tools.NewRegistry(toolset...)
```

At call time, a `tools.Tool.Run` marshals `{"name": <remote name>, "arguments": <raw args>}`,
sends `tools/call`, and flattens the response `content` array into the single
string the `tools.Tool` contract requires. `isError: true` becomes a Go `error`,
matching how the agent already surfaces tool failures.

## Protocol details

**Version negotiation.** The client sends `protocolVersion: "2025-06-18"` in
`initialize` and accepts whatever version the server echoes back, storing it for
the `MCP-Protocol-Version` header on later HTTP requests. Gophermind's own
server replies `2024-11-05` (`internal/mcp/server.go:61`), so the client must
tolerate an older echo rather than requiring an exact match.

**Stdio framing.** One JSON message per line, in and out, until EOF — identical
to `internal/mcp.Serve` (`server.go:130-143`). Reader buffer sized to match the
server's 8 MB maximum so a large `tools/list` cannot truncate.

**HTTP framing.** `POST` with `Accept: application/json, text/event-stream`. The
response is either `application/json` (one JSON-RPC reply) or `text/event-stream`
(reply delivered as SSE `data:` frames). Both must be handled — servers choose
per response. If `initialize` returns an `Mcp-Session-Id` header, every
subsequent request echoes it.

**Notifications.** `notifications/initialized` has no `id` and expects no reply;
`Transport.Send` returns `nil, nil` for it.

## Configuration

Two sources, merged. Per-project entries override global ones on name collision.

**Global** — `~/.gophermind/config.json`, new `mcpServers` key:

```json
{
  "mcpServers": {
    "pelagosnow": {
      "transport": "http",
      "url": "https://mcp.pelagosnow.com/mcp",
      "headers": { "Authorization": "Bearer ${PELAGOSNOW_TOKEN}" }
    },
    "filesystem": {
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/Users/you"],
      "env": { "LOG_LEVEL": "warn" }
    }
  }
}
```

**Per-project** — `.gophermind/*.mcp.json`, one server per file, mirroring the
existing `*.plugin.json` convention (`internal/tools/plugin.go:68`):

```json
{
  "name": "project-db",
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres", "${DATABASE_URL}"]
}
```

```go
type ServerConfig struct {
    Name      string            `json:"name"`      // from filename or map key
    Transport string            `json:"transport"` // "http" | "stdio"
    URL       string            `json:"url"`       // http only
    Headers   map[string]string `json:"headers"`   // http only
    Command   string            `json:"command"`   // stdio only
    Args      []string          `json:"args"`      // stdio only
    Env       map[string]string `json:"env"`       // stdio only
    Disabled  bool              `json:"disabled"`
}
```

**Secret handling.** Every string in `headers`, `args`, `env`, and `url`
supports `${VAR}` expansion from the process environment. An unset variable is a
hard configuration error, not an empty string — a silently blank `Authorization`
header would produce a confusing 401 far from its cause. This keeps live tokens
out of files that get committed or synced.

Configuration is env-var-driven elsewhere in gophermind, but a flat `KEY=value`
namespace cannot express a list of servers each with its own args, env, and
headers. JSON is required here; that is why `mcpServers` is a config.json key
rather than a `GOPHERMIND_*` variable.

## Tool naming

Registered as `server__tool` — for example `pelagosnow__search`.

Namespacing prevents collisions with builtins (a server exposing `run_shell`
must not shadow gophermind's) and between servers. The `__` separator is used
because tool names must satisfy `^[a-zA-Z0-9_-]+$` for most model providers, and
`__` is unlikely to occur naturally in a remote tool name.

Names are validated on load: a server name or tool name containing `__` is
rejected, so the namespace stays unambiguously reversible.

## Safety

**This is the change with the highest consequence in the spec.**

`safety.Gated` (`internal/safety/safety.go:114`) is a hardcoded switch over
known tool names, defaulting to `return false`. MCP tools have names unknown at
compile time, so they would fall through to **ungated** — silent execution of
code from a remote server. That is the opposite of the approved policy.

The fix, in `safety.Gated`:

```go
// MCP tools are namespaced "server__tool" and come from servers gophermind
// does not control; a server may change its tool set between runs. They are
// gated by default and relaxed only by explicit policy.
if strings.Contains(tool, "__") {
    return true
}
```

Two properties this preserves:

1. **Fails closed**, matching `RoleGate` and the `--read-only` flag.
2. **Policy still wins.** `.gophermind/policy`'s `gated_tools` map is consulted
   before `Gated` (`internal/safety/policy.go:75`), so a user relaxes individual
   tools with `"pelagosnow__search": "always"` without weakening the default.

The server's `readOnlyHint` annotation is deliberately **ignored**. It is
self-reported by the party being gated, so it is a claim rather than a
guarantee.

## Error handling

| Failure | Behavior |
|---|---|
| Server unreachable / `initialize` fails / `tools/list` fails | Log a warning naming the server; **skip it**; continue startup |
| Malformed `mcpServers` JSON or `*.mcp.json` | Hard error — refuse to start |
| `${VAR}` unset | Hard error — refuse to start |
| No config / no `.gophermind` dir | No MCP tools, no error (mirrors `LoadPlugins`) |
| `tools/call` transport failure | Returned as a Go `error` from `Tool.Run`; agent sees a failed tool call |
| `isError: true` in response | Returned as a Go `error` carrying the server's text |
| Server name collides with a builtin tool prefix | Hard error at load |

A dead server must never block a session — that is why connect failures are
warnings. A malformed config is a typo the user needs told about immediately,
so it is fatal. Connect timeout: 5 seconds per server, servers dialed
concurrently, so N dead servers cost 5s total rather than 5N.

Stdio subprocesses are terminated on shutdown via `Transport.Close`, with the
process group killed rather than just the direct child.

## Testing

| Area | Approach |
|---|---|
| Stdio transport | **Stand up gophermind's own `mcp.Serve` as the counterparty.** Client and server are in one repo, so this is a genuine end-to-end round trip with no mock protocol |
| HTTP transport | `httptest.Server` returning (a) `application/json` and (b) `text/event-stream`, asserting both decode identically; plus `Mcp-Session-Id` echo |
| Auth | Static headers applied; `${ENV}` expanded; unset var errors |
| Config | Global-only, project-only, both-with-override, malformed, absent |
| Adapt | Schema passthrough, `server__tool` naming, `isError` → Go error, content flattening |
| Safety | **`safety.Gated("x__y") == true`** — regression guard on the fail-closed default |
| Load | One dead server does not prevent healthy servers loading |

Every test runs offline. No test contacts pelagosnow or any real network
endpoint.

## Out of scope

Deliberately excluded from Spec A, to be revisited only when actually needed:

- OAuth 2.1 (Spec B)
- MCP resources and prompts — gophermind's agent consumes tools only
- Server-initiated requests (sampling, roots, elicitation)
- Hot reload of server config
- A `gophermind mcp add` CLI subcommand — edit the JSON directly for now

## Wiring

`cmd/gophermind/main.go`, beside the existing plugin load at line 801:

```go
mcpTools, err := mcpclient.Load(ctx, cfg)
if err != nil {
    return err // malformed config only; unreachable servers are warnings
}
toolset = append(toolset, mcpTools...)
reg := tools.NewRegistry(toolset...)
```

## Success criteria

1. A stdio MCP server declared in `.gophermind/*.mcp.json` has its tools appear
   in `tools/list` and execute successfully through the agent.
2. An HTTP MCP server with a bearer token does the same.
3. `safety.Gated` returns true for every MCP tool; a policy entry can relax one.
4. An unreachable server logs a warning and does not prevent startup.
5. `go build ./...` and `go test ./...` pass.
