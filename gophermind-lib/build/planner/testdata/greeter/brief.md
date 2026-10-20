---
spec_version: "2.0"
id: gm-2026-09-29-900
title: Greeter
language: go
repo: REPO_DIR
base_branch: main
landing: diff_only
on_ambiguity: halt
---

## Overview

A tiny library that builds greeting and farewell messages for a name. It is the
fixture brief for the planner's offline tests: two features, two constraints,
three acceptance bullets.

## Features

### Greeting

`Greet(name)` returns `Hello, <name>!`.

Acceptance criteria:

- An empty name returns a `*NameError`.

### Farewell

`Farewell(name)` returns `Goodbye, <name>!`.

Acceptance criteria:

- An empty name returns a `*NameError`.

## Architecture

- `internal/greet`: pure functions that build messages. No I/O.

## Data

Name: a string that is not empty after trimming spaces.

## Constraints

- Standard library only.
- `gofmt` clean and `go vet` clean.

## Out of scope

- Localisation.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
