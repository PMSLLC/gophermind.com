---
spec_version: "2.0"
id: gm-2026-10-28-002
title: notes-api
language: go
base_branch: main
landing: pull_request
on_ambiguity: halt
work_branch: gm/notes-api
milestone_approvals: true
env:
  - name: NOTES_ADDR
    purpose: HTTP listen address
    default: ":8080"
---

## Overview

`notes-api` is a small REST service that keeps notes in memory and speaks JSON over HTTP. A client can create a note, list all notes, fetch one, change one and delete one. It is for people who want a minimal, correct, well-tested API to build on or to learn from. It is one Go binary built on `net/http` only, with no database: notes live as long as the process does.

## Features

### Create a note

`POST /notes` with a JSON body creates a note.

- The request body is `{"title":"...","body":"..."}`; `body` is optional and defaults to the empty string.
- Success returns HTTP 201 with the note: `{"id":1,"title":"...","body":"..."}`.
- Ids are integers that start at 1 and rise by 1 for each created note. An id is never reused after a delete.
- The title is stored exactly as sent, not trimmed.
- A title that is empty or only white space returns HTTP 400 with `{"error":"title_required"}`.
- Unknown fields in the request are ignored; a client-supplied `id` is ignored.

### Read the request body

Create and update share these rules. They are checked in this order: size, then JSON shape, then title.

- A body larger than 65536 bytes returns HTTP 413 with `{"error":"body_too_large"}`; exactly 65536 bytes is accepted. The size check wins over every other body error.
- A body that is empty, is not valid JSON, is JSON but not an object, or has content after the first JSON value returns HTTP 400 with `{"error":"body_invalid"}`.
- A `title` or `body` that is present but not a string, including `null`, returns HTTP 400 with `{"error":"body_invalid"}`.
- The request `Content-Type` header is not checked.

### List notes

`GET /notes` returns every note.

- Success returns HTTP 200 with a JSON array of notes ordered by id ascending.
- With no notes the body is `[]`, never `null`.

### Get a note

`GET /notes/{id}` returns one note.

- Success returns HTTP 200 with `{"id":1,"title":"...","body":"..."}`.
- An id that does not exist returns HTTP 404 with `{"error":"not_found"}`.

### Update a note

`PUT /notes/{id}` replaces the title and body of an existing note.

- The request body follows the same rules as create: `title` is required, `body` defaults to the empty string.
- Success returns HTTP 200 with the updated note. The id does not change.
- An unknown id returns HTTP 404 with `{"error":"not_found"}`, checked before the body is read.

### Delete a note

`DELETE /notes/{id}` removes a note.

- Success returns HTTP 204 with an empty body.
- An unknown id returns HTTP 404 with `{"error":"not_found"}`; a second delete of the same id is therefore 404.

### Routing and response format

- The method is checked before the id, so `PATCH /notes/abc` returns 405.
- `/notes` accepts only `GET` and `POST`; any other method returns HTTP 405 with `{"error":"method_not_allowed"}` and the header `Allow: GET, POST`.
- `/notes/{id}` accepts only `GET`, `PUT` and `DELETE`; any other method returns HTTP 405 with `{"error":"method_not_allowed"}` and the header `Allow: GET, PUT, DELETE`.
- A valid id matches `[1-9][0-9]*` and fits in an `int`. `0`, `-1`, `01`, `+1`, `1.5`, `abc` and `99999999999999999999` return HTTP 404 with `{"error":"not_found"}`.
- A trailing slash (`/notes/`, `/notes/1/`) and any other path return HTTP 404 with `{"error":"not_found"}` for every method.
- Every response except 204 has the header `Content-Type: application/json` with no charset, and a compact JSON body (no spaces) followed by one newline. Strings are not HTML-escaped.

### Start up

- `notes-api` listens on `NOTES_ADDR`, default `:8080`.
- If listening fails, it prints `notes-api: <error>` on standard error and exits with code 1.
- On SIGINT or SIGTERM it waits up to 10 seconds for requests in flight and exits with code 0.

## Architecture

Packages:

- `cmd/notes-api` (package `main`): reads `NOTES_ADDR` and calls `run(addr string, stop <-chan struct{}, stderr io.Writer) int`. `run` builds the store and the handler and listens; on a listen failure it prints `notes-api: <error>` and returns 1; when `stop` closes it shuts down within 10 seconds and returns 0. `main` only wires SIGINT and SIGTERM to `stop` and exits with the returned code.
- `internal/store`: in-memory, guarded by a `sync.Mutex`. `New() *Store`, `Create(title, body string) Note`, `List() []Note` (ordered by id, never nil), `Get(id int) (Note, bool)`, `Update(id int, title, body string) (Note, bool)`, `Delete(id int) bool`. No HTTP, no JSON.
- `internal/api`: `New(s *store.Store) http.Handler` builds the routes and handlers, maps store results to status codes, and encodes JSON. It does not listen on a port.

Data flow: the handler checks the method, then the id, reads the body through `http.MaxBytesReader`, validates, calls the store, and writes the response.

Patterns to follow:

- Route by hand: register `/notes`, `/notes/` and `/` on `http.ServeMux`, or use one handler that switches on the path. Do not use Go 1.22 method patterns such as `"GET /notes"`, because `ServeMux` then writes its own plain-text 404 and 405. Every 404 and 405 body comes from the error helper.
- `decodeNote(w http.ResponseWriter, r *http.Request) (title, body string, ok bool)` applies the 64 KiB limit, decodes, validates, and writes the 400 or 413 itself. Create and update both call it.
- `parseID(path string) (int, bool)` applies the id rule.
- One helper writes a JSON response with `json.NewEncoder` and `SetEscapeHTML(false)`; one writes the error body. Each handler stays under 40 lines.
- Handlers are methods on a struct that holds the store. No package-level globals. Tests drive `api.New(store.New())` with `httptest.NewRecorder` and `httptest.NewRequest`, never a real port.

## Data

- `Note` holds `ID int` (JSON `id`), `Title string` (JSON `title`) and `Body string` (JSON `body`).
- Errors are `{"error":"<code>"}` where code is one of `title_required`, `body_invalid`, `body_too_large`, `not_found`, `method_not_allowed`.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean; tests pass with `-race`.
- Tests are table driven and use `httptest`, `strings.NewReader` and `bytes.Buffer`; they never use a real port, real files or `os.Stdin`.
- The store is safe for concurrent use; a test runs concurrent creates and finds unique ids.
- Notes are not persisted; a restart starts empty.

## Out of scope

- Any database or file storage.
- Authentication, rate limiting and CORS.
- Pagination, search and filtering on the list.
- Partial updates with `PATCH`.
- TLS, logging and metrics endpoints.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test -race ./...` passes.
- A test on a fresh `api.New(store.New())` posts `{"title":"a"}` to `/notes` and asserts status 201 and body `{"id":1,"title":"a","body":""}`.
- A test that has created that note asserts `GET /notes` returns `[{"id":1,"title":"a","body":""}]`.
- A test asserts `GET /notes/99` returns 404, a title of `" "` returns 400, `DELETE /notes/1` after a create returns 204, and `PATCH /notes` returns 405.
- A test calls `run` with the address `not-an-address` and asserts return code 1 and a standard error line starting `notes-api: `.
