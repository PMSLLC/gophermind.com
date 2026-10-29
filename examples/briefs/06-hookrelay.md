---
spec_version: "2.0"
id: gm-2026-09-29-005
title: hookrelay
language: go
repo: ~/src/hookrelay
base_branch: main
landing: pull_request
on_ambiguity: halt
milestone_approvals: true
secrets:
  - name: WEBHOOK_SIGNING_KEY
    purpose: Shared key used to verify the signature on incoming webhook requests
  - name: SLACK_WEBHOOK_URL
    purpose: Incoming-webhook address that messages are posted to
env:
  - name: LISTEN_ADDR
    purpose: HTTP listen address
    default: ":8080"
  - name: LOG_LEVEL
    purpose: Log level, one of debug, info, warn, error
    default: info
  - name: RELAY_TIMEOUT
    purpose: Time allowed for the outgoing Slack request, as a Go duration
    default: 3s
  - name: ALLOWED_EVENTS
    purpose: Comma-separated event names to forward; other events are acknowledged and dropped
    default: push,pull_request
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: sum.golang.org
    purpose: Module checksum database
    critical: true
  - host: hooks.slack.com
    purpose: Posting notifications (tests use a fake server)
    critical: false
budget:
  max_context_tokens: 8000
  max_revisions: 2
---

## Overview

`hookrelay` is a small HTTP service that sits between a code host and a chat channel. The code host sends it a
signed webhook whenever something happens in a repository. `hookrelay` checks the signature, decides whether the
event is one the team cares about, and posts a one-line summary to a Slack channel. It exists so that nobody has to
give the code host a chat token, and so that unsigned or forged requests never reach the channel.

This example brief shows a service with two secrets, several settings with defaults, one outside host that is
allowed to fail, and human checkpoints. The harness delivers the result as a pull request
(`landing: pull_request`), pauses for approval after every wave (`milestone_approvals: true`), and stops to ask
rather than guess (`on_ambiguity: halt`).

## Features

### Receive and verify

`POST /webhook` accepts a JSON body with the header `X-Signature-256: sha256=<hex>` and the header
`X-Event-Name`.

Acceptance criteria:

- The signature is the hex HMAC-SHA256 of the raw request body using the value of `WEBHOOK_SIGNING_KEY`, and it is
  compared in constant time.
- A missing or malformed signature header returns HTTP 401 with the body `{"error":"signature_missing"}`.
- A signature that does not match returns HTTP 401 with the body `{"error":"signature_mismatch"}`.
- A body larger than 1 MiB returns HTTP 413 before the signature is computed.
- A body that is not valid JSON, after the signature checks out, returns HTTP 400 with `{"error":"body_invalid"}`.
- A request with a missing `X-Event-Name` header returns HTTP 400 with `{"error":"event_missing"}`.

### Route events

Only some events are forwarded.

Acceptance criteria:

- The event name is compared with the comma-separated list in `ALLOWED_EVENTS`, ignoring case and surrounding
  spaces.
- An allowed event is forwarded and the service answers HTTP 202 with `{"status":"forwarded"}`.
- An event that is not allowed is dropped and the service answers HTTP 200 with `{"status":"ignored"}`.
- The `ping` event is always answered with HTTP 200 and `{"status":"pong"}` and is never forwarded.

### Post to Slack

A forwarded event becomes one message.

Acceptance criteria:

- For a `push` event the message reads `<pusher> pushed <n> commit(s) to <repo>@<branch>`, using the fields
  `pusher.name`, `commits` (its length), `repository.full_name`, and `ref` with the `refs/heads/` prefix removed.
- For a `pull_request` event the message reads `<user> <action> PR #<number>: <title> (<repo>)`, using
  `pull_request.user.login`, `action`, `number`, `pull_request.title`, and `repository.full_name`.
- Any other allowed event reads `<event> on <repo>` when `repository.full_name` is present and `<event>` alone
  otherwise.
- The message is posted as `{"text": "..."}` to `SLACK_WEBHOOK_URL` with a timeout of `RELAY_TIMEOUT`.
- If Slack does not answer with a 2xx status in time, the service answers HTTP 502 with `{"error":"relay_failed"}`
  and logs the status code at warn level. It does not retry.
- Message text longer than 3000 characters is cut at 3000 and ends with `...`.

### Health and counters

Operators need to see that it is alive and what it has done.

Acceptance criteria:

- `GET /healthz` returns HTTP 200 and the body `ok` without checking any outside service.
- `GET /metrics` returns plain text, one `name value` pair per line, for `received_total`, `verified_total`,
  `rejected_total`, `forwarded_total`, `ignored_total`, and `relay_failed_total`.
- Counters use `sync/atomic` and are correct under concurrent requests.

## Architecture

Packages:

- `cmd/hookrelay`: reads settings, builds the server, listens on `LISTEN_ADDR`, and shuts down on SIGINT and
  SIGTERM after finishing requests in flight (10 second limit).
- `internal/sigcheck`: verifies a signature for a body and key. Pure, no I/O.
- `internal/router`: decides whether an event is forwarded, from a parsed `ALLOWED_EVENTS` list.
- `internal/format`: builds the message text for an event. Pure.
- `internal/slack`: posts a message. Takes an `*http.Client` so tests can inject a fake transport.
- `internal/httpapi`: handlers and counters. Depends on the four packages above through small interfaces.
- `internal/config`: parses the settings from the process environment, with the defaults listed in the header.

Data flow: the handler reads the body once with a size limit, calls `sigcheck`, decodes the JSON, asks `router`,
asks `format`, then calls `slack`.

Patterns to follow:

- Handlers are methods on a `Server` struct that holds its dependencies. No package-level globals.
- Configuration is parsed once at start-up and passed down. Packages other than `config` never read the process
  environment.
- A wrong setting (for example an unparsable `RELAY_TIMEOUT`) stops the program at start-up with a message naming
  the setting.

## Data

Event (only the fields used):

- `Name string`, from the `X-Event-Name` header.
- `Repo string`, from `repository.full_name`, possibly empty.
- `Action string`, from `action`, possibly empty.
- `Ref string`, from `ref`, possibly empty.
- `Pusher string`, from `pusher.name`, possibly empty.
- `Commits int`, the length of `commits`.
- `PRNumber int`, `PRTitle string`, `PRUser string`, from the `pull_request` object.

Settings:

- `ListenAddr string`, `LogLevel slog.Level`, `RelayTimeout time.Duration`, `Allowed map[string]bool`.
- `SigningKey []byte` and `SlackURL string` come from the two secrets and are never logged.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean; tests pass with `-race`.
- Tests are table-driven and use `httptest` for both the incoming and the outgoing side.
- No request bodies, signatures, or message text are logged at info level or above.
- The signing key and the Slack address never appear in logs, error messages, or test output.
- The service starts within 100 milliseconds and holds no state other than the counters.

## Out of scope

- Retrying, queueing, or storing failed messages.
- More than one destination channel.
- Any code host other than one that signs with HMAC-SHA256 in the header named above.
- Authentication of `/healthz` and `/metrics`.
- TLS termination; a reverse proxy does that.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test -race ./...` passes.
- With the service running and a fake Slack server, a request signed with the key and sent as a `push` event gets
  HTTP 202 and the fake server receives one message.
- The same request with one byte of the body changed gets HTTP 401 `signature_mismatch` and the fake server
  receives nothing.
- A signed `issues` event with `ALLOWED_EVENTS` left at its default gets HTTP 200 `ignored`.
- `GET /metrics` after these requests shows `received_total 3`, `verified_total 2`, `rejected_total 1`,
  `forwarded_total 1`, and `ignored_total 1`.
