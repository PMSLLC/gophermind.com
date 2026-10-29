---
spec_version: "2.0"
id: gm-2026-10-28-003
title: cliphist
language: go
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
env:
  - name: CLIPHIST_FILE
    purpose: Path of the JSON file that holds the history
    default: ".cliphist.json"
  - name: CLIPHIST_MAX
    purpose: Most items the history keeps; the oldest are dropped past this
    default: "100"
---

## Overview

`cliphist` is a small command-line tool that keeps a history of text snippets, the way a clipboard manager does. You add an item, list the items newest first, print one back by its number, or clear the history. The history lives in one JSON file, holds at most a configurable number of items, and ignores an add that repeats the newest item. It is for people who want a scriptable clipboard history. It is one Go binary with no configuration file.

## Features

### Add an item

`cliphist add hello world` stores the text `hello world`, the remaining arguments joined by single spaces, as the newest item.

- A new item is placed first, so it becomes number 1.
- When the text equals the newest item exactly, nothing is stored and `duplicate` is printed. Otherwise `added` is printed. Both exit with code 0.
- Text equal to an older item, not the newest, is stored as a new item.
- When the history already holds `CLIPHIST_MAX` items, the oldest item is dropped so the count stays at `CLIPHIST_MAX`.
- No arguments after `add`, or arguments that join to an empty string, print `cliphist: nothing to add` on standard error and exit with code 2.
- A history file that does not exist is created by the first add that stores an item.

### List items

`cliphist list` prints one line per item, newest first, in the form `<n>: <text>`, where `n` starts at 1.

- A newline inside an item is printed as the two characters `\n`, so each item takes one line.
- An empty history, or a history file that does not exist, prints nothing and exits with code 0.

### Copy an item back

`cliphist copy 2` prints the text of item 2, exactly as stored and followed by one newline, on standard output.

- Item numbers are those shown by `list`.
- A number below 1 or above the item count prints `cliphist: no item <n>` on standard error and exits with code 1.
- A value that is not a base-10 integer prints `cliphist: invalid number "<value>"` on standard error and exits with code 2.
- `copy` with no number, or with more than one, prints the usage line and exits with code 2.

### Clear the history

`cliphist clear` empties the history, prints `cleared` and exits with code 0.

- Clearing a missing file succeeds and leaves an empty history in the file.
- `list` or `clear` with extra arguments prints the usage line and exits with code 2.

### Report errors

- No command, or an unknown command, prints `usage: cliphist add <text...> | list | copy <n> | clear` on standard error and exits with code 2.
- A history file that is not valid JSON, including a zero-byte file, prints `cliphist: corrupt history file <path>` on standard error and exits with code 1. The file is left untouched.
- A `CLIPHIST_MAX` that is not a base-10 integer of at least 1 prints `cliphist: invalid CLIPHIST_MAX "<value>"` on standard error and exits with code 1.
- Any other read or write failure prints `cliphist: <error>` on standard error and exits with code 1.

## Architecture

- `cmd/cliphist` (package `main`): reads `CLIPHIST_FILE` and `CLIPHIST_MAX` with their defaults, validates the maximum, calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.
- `internal/history`: the store. `Load(path string) ([]string, error)` returns the items newest first, an empty slice when the file is missing, and `ErrCorrupt` (wrapped with the path) when the JSON is invalid. `Save(path string, items []string) error` writes the file with mode 0600. `Add(items []string, text string, max int) ([]string, bool)` returns the new slice and whether it stored the item. No printing, no environment access.
- `internal/cli`: `Run(args []string, path string, max int, stdout, stderr io.Writer) int` parses the command, calls `history`, writes the exact output text and returns the exit code. It never calls `os.Exit` and never reads the environment.

Data flow: `main` hands the path and maximum to `cli.Run`, `Run` loads the file, applies the command, saves the file when the command changed it, and prints the result.

Patterns to follow:

- Only `main` decides which streams and which path are used; every other function takes them as parameters.
- `history.Add` is pure: it neither reads nor writes any file.
- Errors are wrapped with `fmt.Errorf("...: %w", err)`.

## Data

- The history file holds one JSON array of strings, newest first, for example `["second","first"]`. An empty history is `[]`.
- `history.ErrCorrupt` is a sentinel error value.
- There is no item type: an item is a `string`.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven, hermetic and use `t.TempDir` for every file, `bytes.Buffer` for output, and never touch the real working directory, the environment or `os.Stdin`.
- Each function stays small and does one thing.

## Out of scope

- A menu bar or any other graphical interface.
- Reading or writing the system clipboard; `copy` only prints.
- Syncing between machines, encryption and file locking.
- Non-text items such as images, timestamps, search and deleting a single item.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist clear` prints `cleared`.
- `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist add one` prints `added`, then `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist add one` prints `duplicate`.
- After `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist add two`, `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist list` prints `1: two` then `2: one`.
- `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist copy 2` prints `one`.
- `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist copy 3` exits with code 1.
- `CLIPHIST_FILE=/tmp/cliphist-acc.json CLIPHIST_MAX=2 cliphist add three` followed by `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist list` prints `1: three` then `2: two`.
- `CLIPHIST_FILE=/tmp/cliphist-acc.json cliphist` with no command exits with code 2.
