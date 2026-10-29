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
- Success returns HTTP 201 with `Content-Type: application/json` and the note: `{"id":1,"title":"...","body":"..."}`.
- Ids are integers that start at 1 and rise by 1 for each created note. An id is never reused after a delete.
- A title that is empty or only white space returns HTTP 400 with `{"error":"title_required"}`.
- A body that is not valid JSON, or that is JSON but not an object, returns HTTP 400 with `{"error":"body_invalid"}`.
- A request body larger than 64 KiB returns HTTP 413 with `{"error":"body_too_large"}`.
- Unknown fields in the request are ignored; a client-supplied `id` is ignored.

### List notes

`GET /notes` returns every note.

- Success returns HTTP 200 with a JSON array of notes ordered by id ascending.
- With no notes the body is `[]`, never `null`.

### Get a note

`GET /notes/{id}` returns one note.

- Success returns HTTP 200 with `{"id":1,"title":"...","body":"..."}`.
- An id that does not exist, or that is not a positive integer, returns HTTP 404 with `{"error":"not_found"}`.

### Update a note

`PUT /notes/{id}` replaces the title and body of an existing note.

- The request body has the same shape and the same rules as create: `title` is required, `body` defaults to the empty string.
- Success returns HTTP 200 with the updated note. The id does not change.
- An unknown or non-numeric id returns HTTP 404 with `{"error":"not_found"}`, checked before the body is read.
- An empty title, malformed JSON and an oversize body return the same status codes and error bodies as create.

### Delete a note

`DELETE /notes/{id}` removes a note.

- Success returns HTTP 204 with an empty body.
- An unknown or non-numeric id returns HTTP 404 with `{"error":"not_found"}`.
- A second delete of the same id returns HTTP 404.

### Reject wrong methods

- `/notes` accepts only `GET` and `POST`; any other method returns HTTP 405 with `{"error":"method_not_allowed"}` and the header `Allow: GET, POST`.
- `/notes/{id}` accepts only `GET`, `PUT` and `DELETE`; any other method returns HTTP 405 with `{"error":"method_not_allowed"}` and the header `Allow: GET, PUT, DELETE`.
- Any other path returns HTTP 404 with `{"error":"not_found"}`.
- Every response except 204 carries `Content-Type: application/json`.

## Architecture

Packages:

- `cmd/notes-api` (package `main`): reads `NOTES_ADDR`, builds the store and the handler, listens, and shuts down on SIGINT and SIGTERM. Nothing else.
- `internal/store`: an in-memory store of notes guarded by a `sync.Mutex`. It has `Create`, `List`, `Get`, `Update` and `Delete`. No HTTP, no JSON.
- `internal/api`: `New(s *store.Store) http.Handler` builds the routes and the handlers. It decodes and encodes JSON and maps store results to status codes. It does not listen on a port.

Data flow: the handler checks the method and the id, reads the body through `http.MaxBytesReader` with a 64 KiB limit, decodes it, validates the title, calls the store, and writes the JSON response.

Patterns to follow:

- Handlers are methods on a struct that holds the store. No package-level globals.
- Each handler decodes the request with `json.NewDecoder` on a reader and writes to an `http.ResponseWriter`, so tests drive it with `httptest.NewRecorder` and `httptest.NewRequest` and never open a real port.
- One small helper writes a JSON body with a status code, and one writes an error body, so the error shape lives in one place.
- Route by hand with `http.ServeMux` or a prefix check; do not add a router module.

## Data

- `Note` holds `ID int` (JSON `id`), `Title string` (JSON `title`) and `Body string` (JSON `body`).
- `store.Store` holds a `map[int]Note` and a `nextID int` that starts at 1.
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
- `go test ./...` passes.
- With `NOTES_ADDR=127.0.0.1:18080 notes-api` running, `curl -s -X POST -d '{"title":"a"}' 127.0.0.1:18080/notes` prints `{"id":1,"title":"a","body":""}`.
- `curl -s 127.0.0.1:18080/notes` then prints `[{"id":1,"title":"a","body":""}]`.
- `curl -s -o /dev/null -w '%{http_code}' 127.0.0.1:18080/notes/99` prints `404`.
- `curl -s -o /dev/null -w '%{http_code}' -X POST -d '{"title":" "}' 127.0.0.1:18080/notes` prints `400`.
- `curl -s -o /dev/null -w '%{http_code}' -X DELETE 127.0.0.1:18080/notes/1` prints `204`.
- `curl -s -o /dev/null -w '%{http_code}' -X PATCH 127.0.0.1:18080/notes` prints `405`.
