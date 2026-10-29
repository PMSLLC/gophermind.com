---
spec_version: "2.0"
id: gm-2026-10-28-005
title: wordfreq
language: go
base_branch: main
landing: commit
on_ambiguity: halt
---

## Overview

`wordfreq` is a small command-line tool that prints the most frequent words in a text, one per line with its count. It is for people who want a quick look at what a text is about. It is one Go binary with no configuration file.

## Features

### Count words

A word is a maximal run of characters for which `unicode.IsLetter` or `unicode.IsDigit` is true. Words are compared after `strings.ToLower`. Every other character, including bytes that are not valid UTF-8, separates words.

- Input with no words, including empty input, gives an empty count and no error.
- `The` and `the` are one word, `don't` is the two words `don` and `t`, and `a1` is one word.

### Print the top words

`wordfreq [-n N] [FILE]` prints at most N lines of the form `<count> <word>`, N defaulting to 10.

- Lines are ordered by count descending and, for equal counts, by word in ascending byte order.
- Input `the cat the dog The end` with `-n 2` prints `3 the` then `1 cat`.
- An N larger than the number of distinct words prints every word. Input with no words prints nothing and exits 0.
- FILE `-`, or no FILE, reads standard input.
- Checks run in this order: flags, then `-n`, then the FILE count, then reading.
- `-n` must parse with `strconv.Atoi` and be at least 1. Otherwise `wordfreq: invalid -n "<value>"` goes to standard error and the exit code is 2.
- More than one FILE, an unknown flag, or `-n` with no value prints `usage: wordfreq [-n N] [FILE]` on standard error and exits 2.
- An unreadable FILE prints `wordfreq: <error>` on standard error and exits 1.

## Architecture

- `cmd/wordfreq` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdin`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.
- `internal/freq`: `Count(r io.Reader) (map[string]int, error)` and `Top(counts map[string]int, n int) []Entry`. `Top` sorts as described and cuts to `n`. No printing, no files.
- `internal/cli`: `Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int` parses the flags with a `flag.FlagSet` whose output is discarded, opens the file or uses `stdin`, calls `freq`, prints. It never calls `os.Exit`.

## Data

- `Entry` holds `Word string` and `Count int`.

## Constraints

- Standard library only.
- Go 1.22 or later, `gofmt` clean and `go vet` clean.
- Tests are table driven and hermetic: they use `strings.NewReader` and `bytes.Buffer`, and `t.TempDir` for the one file test, never `os.Stdin` or the working directory.

## Out of scope

- Stop word lists, stemming, other output formats, or counting across several files.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `printf 'the cat the dog The end\n' | wordfreq -n 2 -` prints two lines, `3 the` and `1 cat`.
- `printf '' | wordfreq` prints nothing and exits 0.
- `wordfreq -n 0` exits with code 2.
- `wordfreq a b` exits with code 2.
