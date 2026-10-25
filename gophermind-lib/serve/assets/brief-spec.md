# GopherMind brief specification

Give this file to any tool or agent that writes briefs for GopherMind. A brief is one Markdown file. GopherMind reads it, plans a tree of small Go functions from it, writes tests, builds each function with a model, and checks the whole program against the acceptance commands you list. A brief that follows this document is accepted by the upload page and by `gophermind brief validate`.

Only Go is supported as the target language.

## 1. File layout

One `.md` file, at most 256 KiB, in this order:

1. A YAML frontmatter block between two `---` lines, starting on the first line of the file.
2. Seven required `##` sections, each present once and not empty: `Overview`, `Features`, `Architecture`, `Data`, `Constraints`, `Out of scope`, `Acceptance`. Order does not matter. A section may not appear twice.
3. Inside `## Features`, at least one `### <Feature name>` block.

## 2. Frontmatter

Unknown keys are rejected.

| Key | Required | Rule |
| --- | --- | --- |
| `spec_version` | yes | The string `"2.0"`. |
| `id` | yes | `gm-YYYY-MM-DD-NNN` (for example `gm-2026-10-25-004`). It names the run and must be unique on the server: a second upload with the same id is refused. Use today's date and the next free number. |
| `title` | yes | 1 to 120 characters. The product name. |
| `language` | yes | `go`. |
| `repo` | yes | For the upload page, the server's repo root exactly (the page shows it in the error if you get it wrong; on the current server it is `/opt/gophermind/workspace`). For `gophermind brief plan` on your own machine, a local path or a git URL. |
| `base_branch` | yes | For example `main`. |
| `landing` | yes | `diff_only` (leave the result as a diff), `commit`, or `pull_request`. |
| `on_ambiguity` | yes | `halt` (stop and ask) or `assume_and_document` (pick the conservative option and record it). |
| `work_branch` | no | Defaults to `gm/<id>`. |
| `milestone_approvals` | no | `true` pauses for a human after every wave. Default `false`. |
| `env` | no | List of `name` (UPPER_SNAKE_CASE), `purpose`, optional `default` (a string). Non-secret variables the program reads. |
| `secrets` | no | List of `name` (UPPER_SNAKE_CASE) and `purpose`. Names only, never values. |
| `network` | no | List of `host`, `purpose`, `critical` (true or false). Hosts the build or program may reach; `host` is a hostname or `*.suffix`, no scheme, no path. |
| `budget` | no | `max_context_tokens` (at least 1024) and `max_revisions` (0 or more). |

A name may not be declared twice, or in both `env` and `secrets`. These names belong to the harness and may not be declared: `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `GOPHERMIND_NODE`, `PATH`, `HOME`, `GOCACHE`, `GOMODCACHE`, `GOPATH`, `TMPDIR`.

## 3. Body rules

- Under `## Constraints` and `## Acceptance`, every line must be a bullet (`- `, `* `, `+ ` or `1. `), a blank line, an indented continuation, a heading or an HTML comment. A plain paragraph there is refused, because it would not count as a requirement. Other sections may hold ordinary prose.
- Every code fence that is opened must be closed. An unclosed fence hides everything after it.
- `## Features` holds one `### <name>` block per feature, each with a short description and bullet acceptance criteria.

## 4. What each section is for

| Section | Write |
| --- | --- |
| Overview | What the program does and for whom, in a paragraph. |
| Features | One `###` block per feature: behavior, then bullet acceptance criteria. Include exact output text, flag names, exit codes and error messages. |
| Architecture | Packages, data flow, and the patterns to follow. Name the packages and what each does and does not do. |
| Data | Every shared type with its fields and Go types. |
| Constraints | Bullets: standard library only or the allowed modules, Go version, formatting, how tests are written. |
| Out of scope | Bullets: what must not be built. |
| Acceptance | Bullets: commands that prove the whole program works, each with the expected result. |

## 5. Practices that make a brief plan and build well

These are not checked by the validator. They come from briefs that failed to build, and they change the result.

- Keep functions small. The planner splits each feature into functions of roughly 40 lines; a brief whose features describe several behaviors in one sentence produces large, hard to test functions. State one behavior per bullet.
- Say how input reaches the code. A function that reads standard input must take an `io.Reader` (and output an `io.Writer`); only `main` chooses `os.Stdin` or `os.Stdout`. The planner refuses a function that says it reads standard input without taking a reader. Write the architecture that way: "`Summarize` takes an `io.Reader`; `main` opens the file or standard input and passes it in."
- Say how tests are written: table driven, using `strings.NewReader` and `bytes.Buffer`, never real files or `os.Stdin`.
- Settle every boundary the tests will assert. If you do not say what happens on overflow, an empty file, a header with no rows, or a malformed row, the test writer invents an answer and the build then fails against it. Give the exact result for each edge you care about.
- Give exact text. Output formats, error messages and exit codes written out in full are tested as written.
- Acceptance commands must be runnable as written, for example `printf 'a\n1\n' | go run . --format json -`, with the result stated. Include at least `go build ./...`, `go vet ./...` and `go test ./...`.
- Prefer the standard library. Each added module is a download the build has to allow in `network`.
- No secrets in the file. Name them in `secrets` and the harness asks for values at run time.
- Keep the whole brief short. One focused program per brief; a few hundred lines at most.

## 6. Uploading

- Page: `https://gophermind.brahy.com/app/briefs/new` (Google sign-in with an `@brahy.com` account). Choose the file and upload. Planning starts at once and the run page shows every step.
- One run is active at a time. A second upload while one runs is refused with the id of the active run.
- Tick "Ask me the clarify questions" to answer the planner's questions in the browser. The run pauses until you do.
- On the command line: `gophermind brief validate brief.md` checks a file with the same rules, with no server and no model.

## 7. Complete example

The brief below is accepted as written (change the `id` to a free one before uploading).

```markdown
---
spec_version: "2.0"
id: gm-2026-01-01-001
title: wordcount
language: go
repo: /opt/gophermind/workspace
base_branch: main
landing: commit
on_ambiguity: assume_and_document
---

## Overview

`wordcount` is a small command-line tool that reads text from a file, or from standard input when the file name is `-`, and prints how many lines, words and bytes it holds. It is for people who want a quick size check of a text file. It is one Go binary with no configuration file.

## Features

### Count a stream

`wordcount notes.txt` prints one line: `<lines> <words> <bytes>`, separated by single spaces.

- A line is a run of bytes ended by a newline; a final run with no newline counts as a line.
- A word is a run of characters that are not white space, as defined by `unicode.IsSpace`.
- The byte count is the number of bytes read.
- An empty input prints `0 0 0`.

### Report errors

- A file that cannot be opened prints `wordcount: <error>` on standard error and exits with code 1.
- More than one argument prints a usage line on standard error and exits with code 2.

## Architecture

- `main` in the repository root: reads the argument, opens the file or chooses `os.Stdin`, calls `count.Stream`, prints the result and decides the exit code. Nothing else.
- `internal/count`: `Stream(r io.Reader) (Result, error)` counts lines, words and bytes. No printing, no files.

Data flow: `main` opens the input, `count.Stream` reads it once from top to bottom, `main` formats the line.

## Data

- `Result` holds `Lines int`, `Words int` and `Bytes int`.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven and use `strings.NewReader`, not files.
- Memory use does not grow with the size of the input.

## Out of scope

- Counting characters or code points.
- Reading several files at once.
- Any output format other than the single line.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `printf 'one two\nthree\n' | go run . -` prints `2 3 14`.
- `printf '' | go run . -` prints `0 0 0`.
- `go run . a b` exits with code 2.
```
