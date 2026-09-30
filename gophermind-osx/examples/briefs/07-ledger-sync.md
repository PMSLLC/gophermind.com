---
spec_version: "2.0"
id: gm-2026-09-29-006
title: ledger-sync
language: go
repo: ~/src/ledger-sync
base_branch: main
landing: commit
on_ambiguity: assume_and_document
secrets:
  - name: LEDGER_API_TOKEN
    purpose: Bearer token for the ledger service's read-only API
env:
  - name: LEDGER_BASE_URL
    purpose: Base address of the ledger service API
    default: https://api.ledger.example.com
  - name: DATA_DIR
    purpose: Directory holding the SQLite database and the resume cursor
    default: ./data
  - name: BATCH_SIZE
    purpose: Rows fetched per request and rows written per database transaction
    default: "500"
  - name: DRY_RUN
    purpose: When true, fetch and normalize but write nothing
    default: "false"
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: sum.golang.org
    purpose: Module checksum database
    critical: true
  - host: api.ledger.example.com
    purpose: The ledger API (tests use a local fake server)
    critical: true
  - host: "*.cdn.ledger.example.com"
    purpose: Optional receipt downloads referenced by transactions
    critical: false
budget:
  max_context_tokens: 6000
  max_revisions: 3
---

## Overview

`ledger-sync` is a batch program that copies a finance team's transactions from a hosted ledger service into a
local SQLite database, so analysts can query them with plain SQL. Run from a scheduler, it fetches everything that
changed since its last run, cleans the data into one consistent shape, and writes it without ever creating
duplicates. It can be interrupted at any point and run again.

This example brief shows a data pipeline with five features, a secret, several settings, one required host and one
wildcard host that is allowed to fail, and a larger revision budget (`max_revisions: 3`) because the moving parts
depend on each other. The harness lands each verified task as a commit (`landing: commit`) and makes conservative
choices instead of stopping when something is unclear (`on_ambiguity: assume_and_document`); every assumption it
makes is written on the task that made it.

## Features

### Fetch with pagination

The client reads `GET $LEDGER_BASE_URL/v1/transactions?updated_after=<RFC3339>&limit=<n>&cursor=<c>`.

Acceptance criteria:

- Every request carries the header `Authorization: Bearer $LEDGER_API_TOKEN`.
- The response is `{"items": [...], "next_cursor": "..."}`; the client keeps requesting with `next_cursor` until it
  is empty or absent.
- `limit` is `BATCH_SIZE`; a value that is not a positive integer stops the program at start-up with a message
  naming `BATCH_SIZE`.
- HTTP 429 and 5xx responses are retried up to 5 times with a wait that doubles from 500 milliseconds; a
  `Retry-After` header in seconds is honoured when present and not longer than 60 seconds.
- HTTP 401 and 403 are never retried and stop the run with exit code 3.
- A response body larger than 10 MiB is an error.

### Normalize

Records from the service are inconsistent and are cleaned before storage.

Acceptance criteria:

- `amount` arrives as a string such as `"1,234.50"`, `"-12"`, or `"(45.10)"` (parentheses mean negative); it is
  stored as an integer number of cents.
- `currency` is upper-cased and must be a three-letter code; a record with any other value is rejected, counted,
  and not stored.
- `posted_at` arrives as RFC 3339 or as `YYYY-MM-DD`; both are stored as UTC RFC 3339, and a date without a time
  becomes midnight UTC.
- `description` is trimmed and internal runs of whitespace collapse to one space.
- `id` is required and non-empty; a record without one is rejected and counted.
- Rejected records are written, one JSON object per line with the reason, to `DATA_DIR/rejected.jsonl`.

### Load into SQLite

Storage is a single SQLite file with an upsert per record.

Acceptance criteria:

- The database is `DATA_DIR/ledger.db`, created with its table on first run.
- Records are written with `INSERT ... ON CONFLICT(id) DO UPDATE`, so running twice over the same data changes
  nothing and reports 0 inserted and 0 updated on the second run.
- Each batch of `BATCH_SIZE` records is one transaction; a failure rolls back that batch only.
- The `updated_at` column is set from the service's `updated_at` field, not from the local clock.
- The database is opened with a busy timeout of 5 seconds and foreign keys on.

### Resume and dry run

The program remembers how far it got.

Acceptance criteria:

- After each committed batch the program writes `DATA_DIR/cursor.json` atomically (write to a temporary file, then
  rename) containing the last `updated_at` seen and the last `next_cursor`.
- A run started with a cursor file resumes from it; `--full` ignores the cursor and starts from the beginning.
- With `DRY_RUN` set to `true` (or `--dry-run`), records are fetched and normalized and the report is printed, but
  the database and the cursor file are not touched.
- Interrupting the program with SIGINT finishes the current batch, writes the cursor, and exits with code 130.

### Run report

Every run ends with a summary.

Acceptance criteria:

- The last line on standard output is a JSON object with `fetched`, `inserted`, `updated`, `unchanged`,
  `rejected`, `batches`, and `duration_ms`.
- `--quiet` suppresses everything on standard output except that final line.
- The exit code is 0 when the run completed, 1 for an unexpected error, 2 for bad settings or flags, 3 for an
  authorization failure, and 130 after an interrupt.
- Receipt links on a record, when present and on an allowed host, are stored as text in a `receipt_url` column and
  never downloaded by this program.

## Architecture

Packages:

- `cmd/ledger-sync`: flags, settings, wiring, signal handling, and the exit code.
- `internal/ledger`: the API client. Takes an `*http.Client` and a base address so tests use `httptest`.
- `internal/normalize`: pure functions from a raw record to a clean record or a rejection reason.
- `internal/store`: the SQLite layer with `Upsert(ctx, []Record)` and nothing else exported.
- `internal/cursor`: reading and atomically writing the cursor file.
- `internal/report`: counters and the final JSON line.

Data flow: the client pages through the service; each page goes through `normalize`; clean records go to `store` in
one transaction; the cursor is written after the commit; the report is printed at the end.

Patterns to follow:

- The pipeline is a loop over pages, not a set of goroutines, in this version.
- Every function that does I/O takes a `context.Context` first.
- The token is passed to the client as a constructor argument and is never stored in a package variable.

## Data

Record (clean):

- `ID string`, `AccountID string`, `Amount int64` (cents), `Currency string`, `PostedAt time.Time`,
  `Description string`, `UpdatedAt time.Time`, `Receipt string` (possibly empty).

Table `transactions`:

- `id TEXT PRIMARY KEY`, `account_id TEXT NOT NULL`, `amount_cents INTEGER NOT NULL`, `currency TEXT NOT NULL`,
  `posted_at TEXT NOT NULL`, `description TEXT NOT NULL`, `updated_at TEXT NOT NULL`, `receipt_url TEXT`.
- An index on `posted_at` and one on `account_id`.

Cursor file: `{"updated_at": "<RFC3339>", "next_cursor": "<string>"}`.

## Constraints

- One third-party module is allowed: `modernc.org/sqlite`, because it needs no C compiler. Everything else is the
  standard library.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean; tests pass with `-race`.
- Tests are table-driven; the API is faked with `httptest`; the database is created in `t.TempDir()`.
- The token never appears in logs, the report, `rejected.jsonl`, or test output.
- Memory use is bounded by `BATCH_SIZE`, not by the size of the ledger.

## Out of scope

- Writing anything back to the ledger service.
- Downloading receipts.
- Running on a schedule; the caller's scheduler does that.
- Any database other than SQLite.
- Currency conversion.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test -race ./...` passes.
- An integration test serves three pages of five records from `httptest`, runs the program, and finds 15 rows.
- A second run of the same test reports `inserted` 0, `updated` 0, `unchanged` 15.
- A test that returns HTTP 401 makes the program exit with code 3 and print nothing that contains the token.
- A dry run leaves no `ledger.db` and no `cursor.json` behind.
