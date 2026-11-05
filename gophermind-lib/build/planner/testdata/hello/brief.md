---
id: hello-v1-20261105-r001
repo: REPO_DIR
spec_version: "2.0"
title: hello
language: go
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
---

## Overview

`hello` is the smallest useful command-line program: it prints a greeting. With no argument it greets the world; with one argument it greets that name. It is one Go binary with no configuration.

## Features

### Say hello

`hello [NAME]` prints one line to standard output and exits with code 0.

- With no argument the line is `Hello, World!`.
- With one argument the line is `Hello, <NAME>!` with the argument inserted as given: `hello Ada` prints `Hello, Ada!`.
- The line ends with exactly one newline.
- Nothing is written to standard error on success.

### Report errors

- More than one argument prints `usage: hello [NAME]` on standard error and exits with code 2. Nothing is printed on standard output.

## Architecture

- `cmd/hello` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.
- `internal/greet`: `Greeting(name string) string` is pure and returns `Hello, <name>!`, or `Hello, World!` when `name` is empty. No printing.
- `internal/cli`: `Run(args []string, stdout, stderr io.Writer) int` checks the argument count, calls `greet.Greeting` and prints the result. It never calls `os.Exit`.

## Data

None.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven and hermetic, using `bytes.Buffer` for output.

## Out of scope

- Reading a name from standard input, flags, localisation, colour, or more than one greeting.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `hello` prints `Hello, World!`.
- `hello Ada` prints `Hello, Ada!`.
- `hello a b` exits with code 2 and prints nothing on standard output.
