---
spec_version: "2.0"
id: gm-2026-09-29-007
title: taskboard
language: go
repo: ~/src/taskboard
base_branch: main
work_branch: gm/taskboard
landing: pull_request
on_ambiguity: halt
milestone_approvals: true
secrets:
  - name: JWT_SIGNING_KEY
    purpose: HMAC key for access and refresh tokens
  - name: SMTP_PASSWORD
    purpose: Password for the mail relay used by the reminder worker
  - name: ADMIN_BOOTSTRAP_TOKEN
    purpose: One-time token that lets the first administrator be created
env:
  - name: LISTEN_ADDR
    purpose: HTTP listen address
    default: ":8080"
  - name: DATABASE_PATH
    purpose: Path of the SQLite database file
    default: ./taskboard.db
  - name: SMTP_HOST
    purpose: Mail relay host name; reminders are logged instead of sent when it is empty
  - name: SMTP_PORT
    purpose: Mail relay port
    default: "587"
  - name: MAIL_FROM
    purpose: Sender address on reminder emails
    default: taskboard@localhost
  - name: REMINDER_INTERVAL
    purpose: How often the reminder worker looks for tasks that are due, as a Go duration
    default: 1m
  - name: LOG_LEVEL
    purpose: Log level, one of debug, info, warn, error
    default: info
  - name: TB_SERVER
    purpose: Server address used by the tb command-line client
    default: http://localhost:8080
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: sum.golang.org
    purpose: Module checksum database
    critical: true
  - host: "*.mail.example.com"
    purpose: Mail relay for reminders (tests use a fake SMTP server)
    critical: false
budget:
  max_context_tokens: 8000
  max_revisions: 2
---

## Overview

`taskboard` is a small team task tracker. People sign in, create boards, invite teammates, and move tasks between
columns. Everyone looking at a board sees changes as they happen, and a background worker emails people when a task
assigned to them is about to be due. A server holds all the data, and a command-line client called `tb` lets people
work without a browser. There is no web front end in this version; the API is the product.

This is the largest example brief: seven features across a server, a background worker, a live-update stream, and a
second program. It uses every gate the harness has. The result is delivered as a pull request
(`landing: pull_request`) on its own branch (`work_branch: gm/taskboard`), the run pauses for approval after every
wave (`milestone_approvals: true`), it stops to ask whenever the brief is unclear (`on_ambiguity: halt`), it has
three secrets and eight settings, and one outside host that may fail without failing the build.

## Features

### Accounts and sign-in

People register with an email and a password and receive tokens.

Acceptance criteria:

- `POST /v1/auth/signup` with a new email and a password of 12 or more characters returns HTTP 201 with the user
  and an access and refresh token pair. A shorter password returns 400 `PASSWORD_TOO_SHORT`; a used email returns
  409 `EMAIL_TAKEN`.
- `POST /v1/auth/login` returns tokens for a correct password and 401 `BAD_LOGIN` for a wrong one, taking the same
  time within 50 milliseconds either way.
- Access tokens last 15 minutes and refresh tokens 7 days; both are signed with `JWT_SIGNING_KEY`.
- `POST /v1/auth/refresh` exchanges a valid refresh token for a new pair and revokes the old refresh token. Using a
  revoked refresh token revokes every refresh token of that user and returns 401 `TOKEN_REUSED`.
- Passwords are stored only as argon2id hashes (from `golang.org/x/crypto/argon2`) with the parameters recorded in
  the hash string, never as plain text.
- `POST /v1/admin/bootstrap` with the header `X-Bootstrap: <ADMIN_BOOTSTRAP_TOKEN>` creates the first administrator
  when no administrator exists and returns 404 afterwards.

### Boards and members

A board groups tasks and belongs to the people who are members of it.

Acceptance criteria:

- `POST /v1/boards` creates a board with a name of 1 to 80 characters and makes the caller its `owner`.
- `GET /v1/boards` lists only the boards the caller belongs to.
- `POST /v1/boards/{id}/members` adds a member by email with the role `editor` or `viewer`; only an owner may do it,
  and anyone else receives 403 `OWNER_REQUIRED`.
- A user who is not a member receives 404 for every route under `/v1/boards/{id}`, so board ids cannot be probed.
- A board's owner cannot be removed or demoted while the board has other members; the attempt returns 409
  `OWNER_LAST`.

### Tasks and comments

Tasks live in the columns `todo`, `doing`, and `done`.

Acceptance criteria:

- `POST /v1/boards/{id}/tasks` creates a task with a title of 1 to 200 characters, an optional description, an
  optional assignee who is a member, and an optional due time in RFC 3339.
- `PATCH /v1/tasks/{id}` changes any of those fields and the column; a viewer receives 403 `EDITOR_REQUIRED`.
- Moving a task to `done` records the completion time; moving it out of `done` clears it.
- `GET /v1/boards/{id}/tasks?column=doing&assignee=me` filters; results are ordered by due time with tasks
  that have no due time last, then by creation time.
- `POST /v1/tasks/{id}/comments` adds a comment of 1 to 2000 characters, and `GET` on the same path lists comments
  oldest first.
- Deleting a task deletes its comments in the same transaction.

### Live updates

A board's viewers see changes without polling.

Acceptance criteria:

- `GET /v1/boards/{id}/events` is a Server-Sent Events stream that sends one event per change: `task.created`,
  `task.updated`, `task.deleted`, `comment.added`, and `member.added`, each with a JSON body containing the id
  of the thing that changed and the actor's user id.
- A new subscriber receives a comment line every 20 seconds as a keep-alive.
- Events are delivered only to subscribers of that board who are still members when the event is sent.
- A subscriber that reads too slowly, with more than 100 events queued, is disconnected instead of slowing anyone
  else down.
- The stream ends promptly when the server shuts down.

### Reminders

A background worker warns people that work is due soon.

Acceptance criteria:

- Every `REMINDER_INTERVAL` the worker finds tasks that are not `done`, have an assignee, and are due within the
  next 24 hours, and have not been reminded yet.
- It sends one email per task to the assignee from `MAIL_FROM` through `SMTP_HOST` and `SMTP_PORT`, signing in with
  `SMTP_PASSWORD`, and then records the reminder so the same task is never reminded twice.
- When `SMTP_HOST` is empty the worker writes the reminder to the log at info level instead and still records it.
- A failed send is logged at warn level and retried on the next tick, up to 3 attempts per task, then given up.
- The worker stops cleanly when the server shuts down and finishes the send in progress first.

### Command-line client

`tb` talks to the server so people can work from a terminal.

Acceptance criteria:

- `tb login` asks for an email and a password without echoing the password, and stores the token pair in
  `~/.config/tb/session.json` with file mode 0600.
- `tb boards`, `tb tasks <board>`, `tb add <board> <title>`, and `tb move <task> <column>` print short readable
  output, or JSON with `--json`.
- `tb watch <board>` prints board events as they arrive, one per line, and reconnects with a backoff if the stream
  drops.
- The server address comes from `--server` or the `TB_SERVER` setting and defaults to `http://localhost:8080`.
- Expired access tokens are refreshed transparently once; if that fails the command exits with code 4 and tells
  the person to run `tb login`.

### Operations

Someone has to run this in production.

Acceptance criteria:

- `GET /healthz` returns HTTP 200 and `ok` with no dependency checks; `GET /readyz` returns 200 only when the
  database answers a trivial query within 200 milliseconds.
- `GET /metrics` returns plain text counters for requests by status class, active event subscribers, reminders
  sent, and reminders failed.
- Every state-changing request appends a row to an `audit_log` table with the actor, the action, the target id, and
  the time; audit rows are never updated or deleted by the application.
- Requests are logged as one structured line with method, path, status, and duration, and never with bodies or
  authorization headers.
- SIGINT and SIGTERM stop accepting connections, let requests in flight finish for up to 10 seconds, then exit.

## Architecture

Programs and packages:

- `cmd/taskboard`: the server; wires everything, starts the reminder worker, and handles shutdown.
- `cmd/tb`: the command-line client. It shares only `internal/api` (request and response types) with the server.
- `internal/auth`: password hashing, token issue and verification, refresh-token rotation and reuse detection.
- `internal/store`: SQLite access with one small interface per area (users, boards, tasks, comments, reminders,
  audit). Every method takes a `context.Context`.
- `internal/httpapi`: routing, handlers, JSON helpers, error mapping, and middleware for logging, authentication,
  and recovery.
- `internal/events`: an in-memory broker with per-board subscribers and bounded queues.
- `internal/reminder`: the worker loop and the mail sender interface, with an SMTP implementation and a log-only one.
- `internal/audit`: writes audit rows; called by handlers through the store, not by hand in each handler.
- `internal/config`: parses the settings once, with the defaults listed in the header.

Data flow: a request passes the middleware, the handler checks membership through the store, changes data in one
transaction, writes an audit row in the same transaction, and publishes an event to the broker after the commit.

Patterns to follow:

- Handlers are methods on a `Server` struct that holds its dependencies. No package-level mutable state.
- Errors returned to clients are `{"error": {"code": "...", "message": "..."}}` with stable upper-case codes.
- Time is read through a `Clock` interface so tests control it.
- The broker never blocks a publisher.

## Data

Tables:

- `users(id, email UNIQUE, password_hash, is_admin, created_at)`.
- `refresh_tokens(id, user_id, family_id, token_hash, expires_at, revoked_at)`.
- `boards(id, name, created_by, created_at)`.
- `members(board_id, user_id, role, added_at, PRIMARY KEY(board_id, user_id))`, role one of `owner`, `editor`,
  `viewer`.
- `tasks(id, board_id, title, description, column, assignee_id, due_at, completed_at, reminded_at, created_at,
  updated_at)`.
- `comments(id, task_id, author_id, body, created_at)`.
- `audit_log(id, actor_id, action, target_id, created_at)`.

Identifiers are random 16-byte values encoded as 26-character lowercase base32 strings. All times are stored as UTC
RFC 3339 text.

## Constraints

- Two third-party modules are allowed: `modernc.org/sqlite` (no C compiler needed) and `golang.org/x/crypto` (for
  argon2). Everything else is the standard library.
- Go 1.22 or later, using the standard library's method-and-path routing.
- `gofmt` clean and `go vet` clean; tests pass with `-race`.
- Tests are table-driven, use `httptest` and a temporary database, and control time with a fake clock; nothing waits
  for real time.
- The signing key, the mail password, and the bootstrap token never appear in logs, error responses, the audit log,
  or test output.
- No request bodies or authorization headers are logged.
- A board with 10,000 tasks lists its first page in under 100 milliseconds on a laptop.

## Out of scope

- A web or mobile front end.
- Password reset by email, two-factor sign-in, and single sign-on.
- File attachments and rich-text comments.
- Multiple server processes; the event broker is in memory.
- Rate limiting and abuse detection.
- Internationalization of emails.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test -race ./...` passes.
- A test signs up two users, creates a board, adds the second as an editor, has the first create a task assigned to
  the second, and asserts that a subscriber of the board's event stream receives `task.created` within one second.
- A test with a fake clock and a fake mail sender creates a task due in 2 hours, advances the clock by one
  reminder interval, and asserts exactly one email is sent, and none after a second tick.
- A test reuses a refresh token twice and asserts every refresh token of that user is revoked.
- A test asserts that a user who is not a member gets 404 (not 403) for a board they cannot see.
- `go run ./cmd/taskboard` starts, and `curl -s localhost:8080/healthz` prints `ok`.
- `go run ./cmd/tb --help` lists `login`, `boards`, `tasks`, `add`, `move`, and `watch`.
