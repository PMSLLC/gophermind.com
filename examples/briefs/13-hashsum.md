---
spec_version: "2.0"
id: gm-2026-10-28-008
title: hashsum
language: go
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
---

## Overview

`hashsum` is a small command-line tool that prints the hex digest of its input, like `sha256sum`. It reads files or standard input and supports sha256, sha1, sha512 and md5. It is one Go binary with no configuration file.

## Features

### Compute a digest

`sum.Digest("sha256", r)` returns the lowercase hex digest of everything `r` yields.

- The algorithm names are `sha256`, `sha1`, `sha512` and `md5`, lowercase only. Any other name returns an error whose text is `unknown algorithm "<name>"`.
- The digest is computed by `io.Copy` into the hash, never by reading the whole input into memory.
- An empty input is hashed like any other and is not an error.
- sha256 of empty is `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`, and of `abc` is `ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad`.
- sha1 of empty is `da39a3ee5e6b4b0d3255bfef95601890afd80709`, and of `abc` is `a9993e364706816aba3e25717850c26c9cd0d89d`.
- md5 of empty is `d41d8cd98f00b204e9800998ecf8427e`, and of `abc` is `900150983cd24fb0d6963f7d28e17f72`.

### Command line

`hashsum [--algo NAME] [FILE]...` prints one line per input.

- NAME defaults to `sha256`. A NAME that `sum.Digest` rejects prints `hashsum: unknown algorithm "<name>"` on standard error and exits with code 2.
- With no FILE, or the FILE `-`, the input is standard input. Files are processed in the order given.
- Each line is the digest, two spaces, the input name, then a newline. The name is `-` for standard input, otherwise the FILE argument exactly as given.
- A file that cannot be read prints `hashsum: <error>` on standard error, processing continues with the next file, and the final exit code is 1. With no failure the exit code is 0.
- An unknown flag prints `usage: hashsum [--algo NAME] [FILE]...` on standard error and exits with code 2.

## Architecture

- `cmd/hashsum` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdin`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.
- `internal/sum`: `Digest(algo string, r io.Reader) (string, error)`. No printing, no files.
- `internal/cli`: `Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int` parses the flags, opens each file and calls `sum.Digest`. It never calls `os.Exit`.

## Data

- There are no shared types: a digest is a `string` and an exit code is an `int`.

## Constraints

- Standard library only.
- Go 1.22 or later, `gofmt` clean and `go vet` clean.
- Tests are table driven and hermetic: they use `strings.NewReader`, `bytes.Buffer` and `t.TempDir`, never `os.Stdin` or the working directory.

## Out of scope

- Checking digests against a list (`-c`), other algorithms, recursing into directories and parallel reads.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `printf 'abc' | hashsum -` prints `ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  -`.
- `printf '' | hashsum --algo md5 -` prints `d41d8cd98f00b204e9800998ecf8427e  -`.
- `hashsum --algo crc32 -` exits with code 2.
