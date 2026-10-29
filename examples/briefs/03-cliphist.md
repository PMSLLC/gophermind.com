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

- Every argument after `add` is text, including ones that start with `-`.
- Text that joins to an empty string or only white space (`strings.TrimSpace` gives empty), or no arguments at all, prints `cliphist: nothing to add` on standard error and exits with code 2.
- Text that passes is stored exactly as joined, without trimming.
- When the text equals the newest item exactly, nothing is stored and `duplicate` is printed. Otherwise `added` is printed. Both exit with code 0.
- Text equal to an older item, not the newest, is stored as a new item.
- After the new item is placed first, the history is cut to its first `CLIPHIST_MAX` items, so a file holding more than `CLIPHIST_MAX` items shrinks to that count on the next add that stores an item. A `duplicate` add changes nothing, even then.
- A history file that does not exist is created by the first add that stores an item.

### List items

`cliphist list` prints one line per item, newest first, in the form `<n>: <text>`, where `n` starts at 1.

- Only the line feed character in an item is printed as the two characters `\n`; carriage returns, tabs and backslashes print as stored.
- An empty history, or a history file that does not exist, prints nothing and exits with code 0.
- Any argument after `list` prints the usage line from Report errors and exits with code 2.

### Copy an item back

`cliphist copy 2` prints the text of item 2, exactly as stored and followed by one newline, on standard output.

- Item numbers are those shown by `list`.
- A value that `strconv.Atoi` rejects (letters, an empty string, a decimal point, too large for int) prints `cliphist: invalid number "<value>"` on standard error and exits with code 2.
- A value `Atoi` accepts is the parsed number, so `+2` is item 2.
- A parsed number below 1 or above the item count, including 0 and -1, prints `cliphist: no item <n>` with the parsed number on standard error and exits with code 1. So does `copy 1` on an empty or missing history.
- `copy` with no number, or with more than one, prints the usage line from Report errors and exits with code 2.

### Clear the history

`cliphist clear` writes an empty history, prints `cleared` and exits with code 0.

- `clear` does not read the history file: it always writes `[]`, so it also repairs a corrupt file, and clearing a missing file creates it holding `[]`.
- Any argument after `clear` prints the usage line from Report errors and exits with code 2.

### Report errors

Checks run in this order: an invalid `CLIPHIST_MAX` (exit 1), then usage and argument errors (exit 2), then history file errors (exit 1). Arguments are checked before the history file is read, so `cliphist copy x` with a corrupt file exits 2.

- The usage line is `usage: cliphist add <text...> | list | copy <n> | clear`, written to standard error. No command, or an unknown command, prints it and exits with code 2.
- `CLIPHIST_MAX` set but empty, rejected by `Atoi`, or below 1 (`""`, `0`, `-1`, `1.5`, `abc`) prints `cliphist: invalid CLIPHIST_MAX "<value>"` on standard error and exits with code 1 for every command. Unset means 100.
- A history file whose content is not exactly one JSON array of strings (invalid JSON, a zero-byte file, `null`, `{}`, `[1]`, `["a",null]`) prints `cliphist: corrupt history file <path>` on standard error and exits with code 1, where `<path>` is `CLIPHIST_FILE` exactly as given. The file is left untouched.
- Any other read or write failure prints `cliphist: <error>` on standard error and exits with code 1.

## Architecture

- `cmd/cliphist` (package `main`): reads `CLIPHIST_FILE` and `CLIPHIST_MAX` with their defaults, validates the maximum, calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.
- `internal/history`: the store. `Load(path string) ([]string, error)` returns the items newest first, an empty slice when the file is missing, and an error wrapping `ErrCorrupt` when the content is not one JSON array of strings. `Save(path string, items []string) error` writes the file with mode 0600, and writes `[]` for a nil or empty slice, never `null`. `Add(items []string, text string, max int) ([]string, bool)` returns the new slice, cut to `max` items, and whether it stored the item. No printing, no environment access.
- `internal/cli`: `Run(args []string, path string, max int, stdout, stderr io.Writer) int` only dispatches to `runAdd`, `runList`, `runCopy` and `runClear`, each under 40 lines. `usageError` writes the usage line and returns 2. When `errors.Is(err, history.ErrCorrupt)`, the code prints the corrupt-file text and never the wrapped error text. Nothing here calls `os.Exit` or reads the environment.

Data flow: `main` hands the path and maximum to `cli.Run`. `Run` checks the arguments, loads the file for add, list and copy only, applies the command, saves the file when the command changed it, and prints the result.

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
- `cliphist` with no arguments prints `usage: cliphist add <text...> | list | copy <n> | clear` on standard error and exits with code 2.
- `cliphist bogus` exits with code 2.
- `cliphist add` prints `cliphist: nothing to add` on standard error and exits with code 2.
- `cliphist copy x` prints `cliphist: invalid number "x"` on standard error and exits with code 2.
- `cliphist list extra` exits with code 2.
- A test calls `cli.Run` on a `t.TempDir` file with `add one`, `add one`, `add two`, `list`, `copy 2`, `copy 3` in turn and asserts `added`, `duplicate`, `added`, the lines `1: two` and `2: one`, the line `one`, and exit code 1 with `cliphist: no item 3`.
- A test calls `cli.Run` with max 2 on a history of two then one, runs `add three` then `list`, and asserts `1: three` and `2: two`.
