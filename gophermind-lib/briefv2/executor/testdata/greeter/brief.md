---
spec_version: "2.0"
id: gm-2026-09-30-901
title: Greeter
language: go
repo: REPO_DIR
base_branch: main
landing: commit
on_ambiguity: assume_and_document
secrets:
  - name: GREETER_TOKEN
    purpose: "Token the second acceptance check requires to be present"
---

## Overview

A tiny HTTP server that greets and says goodbye to a name. It is the one
fixture every executor test runs against: three features, two constraints and
two acceptance bullets.

## Features

### Greeting

`Greet(name)` returns `Hello, <name>!` and refuses an empty name.

### Farewell

`Farewell(name)` returns `Goodbye, <name>!` and refuses an empty name.

### Server

`cmd/greeter` serves `/hello` and `/bye` over HTTP. The query parameter `name`
defaults to `world`.

## Architecture

- `internal/greet`: pure functions that build messages. No I/O.
- `cmd/greeter`: the HTTP handlers and the server. `main.go` is given.

## Data

Name: a string that is not empty after trimming spaces.

## Constraints

- gofmt and go vet clean
- Standard library only

## Out of scope

- Localisation.

## Acceptance

- GET /hello?name=Ada returns Hello, Ada!
- GET /bye?name=Ada returns Goodbye, Ada!
