# GopherMind brief specification v3 (draft)

Status: a proposal, dated 2026-11-05. The upload page and `gophermind brief validate` still accept only `spec_version: "2.0"`; a v3 brief is refused until the parser lands. Write v2 briefs today (download the current specification from the same page), and read this to see what changes and why. Every change comes from a brief that planned or built badly under v2.

v3 keeps the shape of v2: one Markdown file, YAML frontmatter, the same seven sections, bullets under Constraints and Acceptance, secrets by name only, binaries under `cmd/<name>`. It adds structure where v2 relied on prose conventions the planner recovered with pattern matching.

## 1. File layout

One `.md` file, at most 256 KiB, in this order:

1. A YAML frontmatter block between two `---` lines, starting on the first line.
2. The seven required `##` sections, each present once and not empty, in any order: `Overview`, `Features`, `Architecture`, `Data`, `Constraints`, `Out of scope`, `Acceptance`.
3. Two optional `##` sections: `Decisions` and `Tests`.
4. Inside `## Features`, at least one `### <Feature name>` block.

## 2. Frontmatter

Unknown keys are rejected.

| Key | Required | Rule |
| --- | --- | --- |
| `spec_version` | yes | The string `"3.0"`. |
| `title` | yes | 1 to 120 characters. The product name; the library files versions by it. |
| `language` | yes | `go`. |
| `base_branch` | yes | For example `main`. |
| `landing` | yes | `diff_only`, `commit` or `pull_request`. |
| `on_ambiguity` | yes | `halt` or `assume_and_document`. |
| `module` | no | The Go module path. Default: the title slug. |
| `work_branch` | no | Defaults to `gm/<run id>`. |
| `milestone_approvals` | no | `true` pauses for a human after every wave. Default `false`. |
| `env` | no | List of `name` (UPPER_SNAKE_CASE), `purpose`, optional `default`. |
| `secrets` | no | List of `name` and `purpose`. Names only, never values. |
| `network` | no | List of `host`, `purpose`, `critical`. |
| `budget` | no | `max_context_tokens` (at least 1024), `max_revisions` (0 or more). |
| `conventions` | no | A map of known keys, section 6. |

Removed from v2: `id` (the server and the CLI assign the run id; a stray `id:` line is ignored, never an error) and `repo` (a CLI flag only). Both are dropped from a saved version as in v2.

The same name may not appear twice or in both `env` and `secrets`. The harness-owned names `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `GOPHERMIND_NODE`, `PATH`, `HOME`, `GOCACHE`, `GOMODCACHE`, `GOPATH`, `TMPDIR` may not be declared.

## 3. Requirements have ids

Every bullet under `## Features`, `## Constraints` and `## Acceptance` is a requirement. v2 numbered them by position (`F1`, `C3`, `A6`), which changes when a bullet is inserted. v3 lets the writer fix the id by starting the bullet with `[id]`:

```markdown
### Say hello

- [F-greet-world] With no argument the line is `Hello, World!`.
- [F-greet-name] With one argument the line is `Hello, <NAME>!` with the argument inserted as given.
```

An id is `[A-Za-z][A-Za-z0-9-]*`, unique in the file. A bullet without one gets the positional id as in v2. Ids are what coverage, the test writer and the architecture block refer to; a feature heading is a title, not an id, and a function may satisfy bullets from several features.

## 4. Architecture is a block, not prose

v2 recovered directories, signatures and the entry call from backticks in bullets; a bullet written in a different shape was silently ignored. v3 puts the fixed facts in one fenced block, in YAML, and keeps prose beside it for everything else.

```architecture
module: hello
packages:
  - dir: internal/greet
    package: greet
    functions:
      - sig: "func Greeting(name string) string"
        satisfies: [F-greet-world, F-greet-name]
        pure: true
    forbid: [print, stdin, exit]
  - dir: internal/cli
    package: cli
    functions:
      - sig: "func Run(args []string, stdout, stderr io.Writer) int"
        satisfies: [F-greet-world, F-greet-name, F-usage]
        uses: [greet.Greeting]
    forbid: [exit, stdin, flags]
  - dir: cmd/hello
    package: main
    entry:
      call: cli.Run
      args: [os.Args[1:], os.Stdout, os.Stderr]
      exit: true
    only: true
```

Rules:

- `dir` is a directory relative to the module root. `package` is the Go package clause; for a `cmd/` directory it must be `main`, elsewhere it defaults to the last path element. The two are separate facts; nothing is inferred from the other.
- `functions[].sig` is a complete Go signature with named parameters. It is fixed: later stages may not rename it, move it, add or remove parameters, or add a receiver. The planner may add unexported helpers in the same package unless `only: true` is set on the package, which means these functions and no others.
- `satisfies` lists requirement ids. Coverage checks that every requirement id under Features is satisfied by at least one function and that the function's contract text carries the requirement's output literals.
- `uses` lists `package.Function` calls the function must make. They become dependencies, never side effects.
- `forbid` lists capabilities the package may not use: `print`, `stdin`, `exit`, `flags`, `files`, `network`, `env`, `log`. A package with `exit` forbidden may not call `os.Exit`; the executor's source gate and the planner's prompt read the same fact.
- `entry` marks the process entry point. `call` names the function `main` calls, `args` the values passed in order, and `exit: true` means `main` passes the result to `os.Exit`. Only an `entry` package may call `os.Exit`.
- A block is required when the Acceptance section runs a binary. A brief with no fixed architecture may omit it and let the planner propose packages.

Prose after the block explains data flow and patterns. It is given to the model; it is not parsed.

## 5. Acceptance cases are exact

A v2 acceptance bullet was a sentence the planner turned into a shell command, and the command could lose a nonzero exit status or mask a failure. v3 keeps bullets for build checks and adds a fenced block for program runs, which the harness executes itself with captured stdout, stderr and status:

```markdown
## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
```

```acceptance
- run: hello
  stdout: "Hello, World!\n"
  stderr: ""
  exit: 0
- run: hello Ada
  stdout: "Hello, Ada!\n"
  stderr: ""
  exit: 0
- run: hello a b
  stdout: ""
  stderr: "usage: hello [NAME]\n"
  exit: 2
```

`run` is the binary name (built from `cmd/<name>`, on PATH) and its arguments; `stdin` is optional; `stdout` and `stderr` are byte-exact strings with `\n` visible, or omitted to mean "not checked"; `exit` is required. Each case is a requirement with an id (`[A-...]` prefix on the `run` line, or positional). No model writes these commands, so none can be vacuous.

## 6. Conventions as known keys

Rules that v2 stated in Constraints prose, and that enrichment or the test writer could ignore, become frontmatter keys the planner checks structurally:

```yaml
conventions:
  logging: none            # none | stdlib
  tests: [table_driven, hermetic]
  output_newline: single   # every printed line ends with exactly one newline
  stdlib_only: true
```

An unknown key is an error. A convention cannot be overridden by a model stage: enrichment may not add a log event when `logging: none`, and a test file for a behavioural package must contain a table when `tests` has `table_driven`.

## 7. Decisions

The clarify stage asks its questions once per run and the answers are not written back. v3 adds an optional `## Decisions` section: bullets of `question: answer`. A question whose text matches a decision is not asked again, and the decision is carried in every packet as a settled fact. The server appends the answers given in the browser to this section when a new version is saved from a run.

## 8. Body rules

- Under `## Constraints` and `## Acceptance`, every non-blank line is a bullet, an indented continuation, a heading, an HTML comment or part of a fence.
- Every code fence opened is closed.
- A `##` section appears at most once. The seven required sections are present and not empty.
- `## Features` contains at least one `###` block.
- The `architecture` and `acceptance` fenced blocks parse as YAML with the keys above; an unknown key, an unparseable signature or a `satisfies` id that does not exist is an error with its line.
- `## Tests`, when present, holds bullets the test writer must cover, written as calls with expected results (`Greeting("") returns Hello, World!`). They are given to the test writer verbatim.

## 9. What each section is for

| Section | Write |
| --- | --- |
| Overview | What the program does and for whom, in a paragraph. Not given to per-function packets. |
| Features | One `###` block per feature: a sentence of behaviour, then bullet requirements with exact text, exit codes and error messages. |
| Architecture | The fenced block, then prose on data flow and patterns. |
| Data | Every shared type with its fields and Go types, or "none". |
| Constraints | Bullets the planner cannot check as conventions: Go version, allowed modules, formatting. |
| Out of scope | Bullets: what must not be built. Used to refuse additions. |
| Acceptance | Build-check bullets plus the fenced cases. |
| Decisions | Optional. Settled answers. |
| Tests | Optional. Cases the unit tests must contain. |

## 10. Practices (unchanged from v2 where not covered above)

- One behaviour per bullet; a bullet with three behaviours becomes one large function.
- A function that reads standard input takes an `io.Reader`; output goes to an `io.Writer`; only `main` touches `os.Stdin` and `os.Stdout`.
- Settle every edge the tests will assert.
- Exact text for outputs, errors and exit codes, including the newline rule.
- Prefer the standard library; name any module and its download host in `network`.
- No secret values in the file.
- One program per brief, a few hundred lines at most. The planner refuses a Features section that does not fit one packet and names the section to shorten.

## 11. Migration from v2

A v2 brief is a valid v3 brief after: `spec_version` set to `"3.0"`, the `id` and `repo` lines removed, and the Architecture bullets rewritten as the fenced block (the prose may stay). Acceptance bullets that run the binary are moved into the `acceptance` block. The planner keeps accepting `"2.0"` with the v2 rules for versions already in the library.

## 12. Complete example

```markdown
---
spec_version: "3.0"
title: hello
language: go
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
conventions:
  logging: none
  tests: [table_driven, hermetic]
  output_newline: single
  stdlib_only: true
---

## Overview

`hello` is the smallest useful command-line program: it prints a greeting. With no argument it greets the world; with one argument it greets that name. One binary, no configuration.

## Features

### Say hello

`hello [NAME]` prints one line to standard output and exits with code 0.

- [F-greet-world] With no argument the line is `Hello, World!`.
- [F-greet-name] With one argument the line is `Hello, <NAME>!` with the argument inserted as given: `hello Ada` prints `Hello, Ada!`.
- [F-newline] The line ends with exactly one newline and nothing is written to standard error.

### Report errors

- [F-usage] More than one argument prints `usage: hello [NAME]` on standard error and exits with code 2. Nothing is printed on standard output.

## Architecture

```architecture
module: hello
packages:
  - dir: internal/greet
    package: greet
    functions:
      - sig: "func Greeting(name string) string"
        satisfies: [F-greet-world, F-greet-name]
        pure: true
    forbid: [print, stdin, exit]
    only: true
  - dir: internal/cli
    package: cli
    functions:
      - sig: "func Run(args []string, stdout, stderr io.Writer) int"
        satisfies: [F-greet-world, F-greet-name, F-newline, F-usage]
        uses: [greet.Greeting]
    forbid: [exit, stdin, flags]
    only: true
  - dir: cmd/hello
    package: main
    entry:
      call: cli.Run
      args: [os.Args[1:], os.Stdout, os.Stderr]
      exit: true
    only: true
```

`main` is process wiring only. `Run` checks the argument count, calls `greet.Greeting` and prints the result.

## Data

- None.

## Constraints

- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.

## Out of scope

- Reading a name from standard input, flags, localisation, colour, or more than one greeting.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.

```acceptance
- run: hello
  stdout: "Hello, World!\n"
  stderr: ""
  exit: 0
- run: hello Ada
  stdout: "Hello, Ada!\n"
  stderr: ""
  exit: 0
- run: hello a b
  stdout: ""
  stderr: "usage: hello [NAME]\n"
  exit: 2
```
```
