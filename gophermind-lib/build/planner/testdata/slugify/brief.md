---
id: slugify-v1-20261105-r001
repo: REPO_DIR
spec_version: "2.0"
title: slugify
language: go
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
---

## Overview

`slugify` is a small command-line tool that turns text into a URL slug. It reads all of its input as one string and prints one line. It is for people who need clean slugs in shell scripts. It is one Go binary with no configuration file.

## Features

### Make a slug

`slugify [--max N] [FILE]` reads FILE, or standard input when FILE is `-` or missing, and prints the slug followed by one newline.

- One single trailing line feed in the input is ignored; the rest is the text.
- The text is lowercased, then only the bytes `a` to `z` and `0` to `9` are kept.
- Every run of any other bytes (spaces, tabs, line feeds inside the text, punctuation, accented or other non-ASCII letters) becomes one hyphen.
- Hyphens are stripped from both ends; an empty result prints an empty line and exits with code 0.
- `Hello, World!` gives `hello-world`, `  --Go & Rust  ` gives `go-rust`, and `Crème Brûlée` gives `cr-me-br-l-e`.
- `--max N` with N at least 1 cuts the slug to at most N characters, then strips a trailing hyphen: `--max 6` on `Hello, World!` gives `hello`, and `--max 99` changes nothing.
- A repeated `--max` uses the last value.

### Report errors

- A `--max` value that `strconv.Atoi` rejects or that is below 1 prints `slugify: invalid --max "<value>"` on standard error and exits with code 2.
- More than one FILE, an unknown flag (any argument starting with `-` other than `-` itself, including `--max=3`), or `--max` with no value prints `usage: slugify [--max N] [FILE]` on standard error and exits with code 2.
- Usage errors are checked before `--max` values, and both before the file is opened.
- A file that cannot be read prints `slugify: <error>` on standard error and exits with code 1.

## Architecture

- `cmd/slugify` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdin`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.
- `internal/slug`: `Make(s string, max int) string` is pure; a `max` of 0 or less means no limit. No printing, no files.
- `internal/cli`: `Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int` parses the arguments, reads the whole input from the file or from `stdin`, calls `slug.Make` and prints the result. It never calls `os.Exit`.

## Data

- There are no shared types: the text is a `string` and the limit is an `int`.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven and hermetic, using `strings.NewReader` and `bytes.Buffer`, and `t.TempDir` for any file.

## Out of scope

- Transliterating accented letters, Unicode slugs, other separators, reading several files, or one slug per line.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `printf 'Hello, World!\n' | slugify -` prints `hello-world`.
- `printf 'Hello, World!\n' | slugify --max 6` prints `hello`.
- `slugify --max 0` exits with code 2.
- `slugify a b` exits with code 2.
