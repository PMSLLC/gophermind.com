# GopherMind brief specification v3 (draft)

Status: a proposal, dated 2026-11-05, revised the same day after review. The upload page and `gophermind brief validate` still accept only `spec_version: "2.0"`; a v3 brief is refused until the parser lands. Write v2 briefs today (download the current specification from the same page), and read this to see what changes and why. Every change comes from a brief that planned or built badly under v2.

v3 keeps the shape of v2: one Markdown file, YAML frontmatter, the same seven sections, bullets under Constraints and Acceptance, secrets by name only, binaries under `cmd/<name>`. It adds structure where v2 relied on prose conventions the planner recovered with pattern matching.

## 1. File layout

One `.md` file, at most 256 KiB, in this order:

1. A YAML frontmatter block between two `---` lines, starting on the first line.
2. The seven required `##` sections, each present once and not empty, in any order: `Overview`, `Features`, `Architecture`, `Data`, `Constraints`, `Out of scope`, `Acceptance`.
3. Two optional `##` sections: `Decisions` and `Tests`.
4. Inside `## Features`, at least one `### <Feature name>` block.

## 2. Frontmatter

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

Unknown frontmatter keys are errors, with two migration exceptions for keys v2 had:

- `id`: accepted and ignored, with a deprecation warning naming the line. The server and the CLI assign the run id.
- `repo`: rejected, with a message saying to pass the repository as the CLI argument instead.

Both are dropped from a saved version as in v2.

The same name may not appear twice or in both `env` and `secrets`. The harness-owned names `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `GOPHERMIND_NODE`, `PATH`, `HOME`, `GOCACHE`, `GOMODCACHE`, `GOPATH`, `TMPDIR` may not be declared.

## 3. Requirements have ids

Every bullet under `## Features`, `## Constraints` and `## Acceptance` is a requirement, and so is every case in the `acceptance` block (section 5). v2 numbered them by position (`F1`, `C3`, `A6`), which changes when a bullet is inserted. v3 lets the writer fix the id by starting the bullet with `[id]`:

```markdown
### Say hello

- [F-greet-world] With no argument the line is `Hello, World!`.
- [F-greet-name] With one argument the line is `Hello, <NAME>!` with the argument inserted as given.
```

An id is `[A-Za-z][A-Za-z0-9-]*`, unique in the file across every section and the `acceptance` block. A bullet without one gets the positional id as in v2. Ids are what coverage, the test writer and the architecture block refer to; a feature heading is a title, not an id, and a function may satisfy bullets from several features.

The three kinds of requirement are checked differently, and the difference is part of the contract:

- Feature requirements are mapped to functions through `satisfies` in the architecture block.
- Constraint requirements are checked globally: by a convention (section 6), by a static check, or by a build check. They are never assigned to a function.
- Acceptance requirements are checked by the build-check bullets or by running the acceptance cases.

`satisfies` may name only Feature ids. A `satisfies` entry that names a Constraint or Acceptance id is an error with its line.

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
        uses: [internal/greet.Greeting]
    forbid: [exit, stdin, flags]
  - dir: cmd/hello
    package: main
    entry:
      call: internal/cli.Run
      args: [os.Args[1:], os.Stdout, os.Stderr]
      exit: true
    only: true
```

Rules:

- `dir` is a directory relative to the module root. `package` is the Go package clause; for a `cmd/` directory it must be `main`, elsewhere it defaults to the last path element. The two are separate facts; nothing is inferred from the other.
- `functions[].sig` is a complete Go signature with named parameters. It is fixed: later stages may not rename it, move it, add or remove parameters, or add a receiver. The planner may add unexported helpers in the same package unless `only: true` is set on the package.
- `satisfies` lists Feature requirement ids (section 3). Coverage checks that every requirement id under Features is satisfied by at least one function and that the function's contract text carries the requirement's output literals.
- `uses` lists functions this function must call, as `<dir>.<Function>`: the architecture directory, then a dot, then the exported name (`internal/greet.Greeting`). Two packages may share a package name (`internal/api/client` and `internal/db/client` are both `client`), so the directory is the identity; the planner renders the Go call (`greet.Greeting`) after resolving imports. They become dependencies, never side effects. A `uses` entry that names a directory or function not in the block is an error with its line.
- `forbid` lists capabilities the package may not use, from the table in section 4a. The executor's source gate and the planner's prompt read the same fact.
- `entry` marks the process entry point. `call` names the function `main` calls, in the same `<dir>.<Function>` form; `args` the values passed in order; `exit: true` means `main` passes the result to `os.Exit`. Each `args` value is a Go expression, parsed with `go/parser`; a value that does not parse is an error with its line, found before planning. Only an `entry` package may call `os.Exit`.
- A block is required when the Acceptance section runs a binary. A brief with no fixed architecture may omit it and let the planner propose packages.

`only: true` on a package means: in that package's non-`_test.go` files, the top-level function and method declarations are exactly the listed `functions` (plus `main` in an `entry` package) and no others. It says nothing about types, constants and variables, which the package may declare as it needs. It says nothing about `_test.go` files: test functions, benchmarks, examples and test-only helpers are outside the restriction. There is no exception for generated code or build-tagged files: every non-`_test.go` file in the directory is read, whatever its build constraints, so an `only` package cannot hold a generated or platform-specific file with extra functions.

Prose after the block explains data flow and patterns. It is given to the model; it is not parsed.

### 4a. What each `forbid` capability means

The gate reads the package's non-`_test.go` files. `_test.go` files are exempt from every capability except `exit` (a test that calls `os.Exit` ends the test binary). The rules are by identifier, not by effect: writing to an `io.Writer` parameter is never `print`, and reading an `io.Reader` parameter is never `stdin`, because those are how a package is meant to do I/O it was handed.

| Capability | The package's production code may not |
| --- | --- |
| `exit` | Call `os.Exit`, `log.Fatal`, `log.Fatalf`, `log.Fatalln` or `runtime.Goexit`. `panic` is not covered; forbid it in Constraints prose if it matters. |
| `print` | Call `fmt.Print`, `fmt.Printf`, `fmt.Println`, the built-in `print` or `println`, or `log.Print`, `log.Printf`, `log.Println`; or reference `os.Stdout` or `os.Stderr`. |
| `stdin` | Reference `os.Stdin`, or call `fmt.Scan`, `fmt.Scanf`, `fmt.Scanln`. |
| `flags` | Import `flag`, or a flag or command package such as `github.com/spf13/pflag`, `github.com/spf13/cobra` or `github.com/urfave/cli`. |
| `log` | Import `log`, `log/slog` or a third-party logging package. |
| `files` | Call `os.Open`, `os.Create`, `os.OpenFile`, `os.ReadFile`, `os.WriteFile`, `os.ReadDir`, `os.Remove`, `os.RemoveAll`, `os.Rename`, `os.Mkdir`, `os.MkdirAll`, `os.MkdirTemp`, `os.CreateTemp`, `os.Stat`, `os.Lstat`, or `filepath.Walk`, `filepath.WalkDir`. |
| `network` | Import `net`, `net/http`, `net/rpc`, `net/smtp` or a client package built on them (gRPC, an HTTP client library). |
| `env` | Call `os.Getenv`, `os.LookupEnv`, `os.Setenv`, `os.Unsetenv`, `os.Environ` or `os.Clearenv`. |

`pure: true` on a function is shorthand for `forbid: [print, stdin, exit, flags, log, files, network, env]` applied to that function's package, plus a prompt-level instruction that the function depends only on its arguments.

## 5. Acceptance cases are exact

A v2 acceptance bullet was a sentence the planner turned into a shell command, and the command could lose a nonzero exit status or mask a failure. v3 keeps bullets for build checks and adds a fenced block for program runs, which the harness executes itself with captured stdout, stderr and status:

```markdown
## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
```

```acceptance
- id: A-default
  run: hello
  args: []
  stdout: "Hello, World!\n"
  stderr: ""
  exit: 0
- id: A-name
  run: hello
  args: ["Ada"]
  stdout: "Hello, Ada!\n"
  stderr: ""
  exit: 0
- id: A-too-many
  run: hello
  args: ["a", "b"]
  stdout: ""
  stderr: "usage: hello [NAME]\n"
  exit: 2
```

Each case is one requirement:

- `id` is optional and follows the identifier grammar of section 3; a case without one gets a positional id (`A1`, `A2`, in block order after the bullets). The Markdown `[id]` prefix is not used inside YAML.
- `run` is the binary name, built from `cmd/<name>`. It is one token: no arguments, no path, no shell syntax.
- `args` is a YAML list of strings passed as the process's argv, in order. No shell is involved: the harness invokes the binary with exactly these arguments, so spaces, quotes and backslashes are literal, and `args: [""]` passes one empty argument. The key is required; an empty list means no arguments.
- `stdin` is optional, a string written to the process's standard input and then closed.
- `stdout` and `stderr` are byte-exact strings with `\n` visible, or omitted to mean "not checked".
- `exit` is required.

No model writes these commands, so none can be vacuous.

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

The clarify stage asks its questions once per run and the answers are not written back. v3 adds an optional `## Decisions` section of settled answers, each with a stable id:

```markdown
## Decisions

- [D-empty-name] An empty NAME argument is treated as no argument: the line is `Hello, World!`.
```

Every clarify question carries a decision key, a short slug the planner derives from what the question is about (`empty-name`), not from its English wording. A question whose key matches a decision id with the `D-` prefix removed is not asked again, and the decision is carried in every packet as a settled fact. Matching by key means a change in the question's wording does not lose the decision. The server appends the answers given in the browser to this section, with generated ids, when a new version is saved from a run.

## 8. Body rules

- Under `## Constraints` and `## Acceptance`, every non-blank line is a bullet, an indented continuation, a heading, an HTML comment or part of a fence.
- Every code fence opened is closed.
- A `##` section appears at most once. The seven required sections are present and not empty.
- `## Features` contains at least one `###` block.
- The `architecture` and `acceptance` fenced blocks parse as YAML with the keys above; an unknown key, an unparseable signature, an `args` expression that `go/parser` refuses, a `uses` or `call` reference to nothing in the block, or a `satisfies` id that does not exist or is not a Feature id is an error with its line.
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
| Decisions | Optional. Settled answers with `D-` ids. |
| Tests | Optional. Cases the unit tests must contain. |

## 10. Practices (unchanged from v2 where not covered above)

- One cohesive requirement per bullet. A bullet may specify several observable parts of one outcome, such as the stdout, the stderr and the exit status of one invocation. Do not combine unrelated behaviours because the same function happens to implement them; that is what makes the planner produce one large function.
- A function that reads standard input takes an `io.Reader`; output goes to an `io.Writer`; only `main` touches `os.Stdin` and `os.Stdout`.
- Settle every edge the tests will assert.
- Exact text for outputs, errors and exit codes, including the newline rule.
- Prefer the standard library; name any module and its download host in `network`.
- No secret values in the file.
- One program per brief, a few hundred lines at most. The planner refuses a Features section that does not fit one packet and names the section to shorten.

## 11. Migration from v2

A v2 brief is a valid v3 brief after: `spec_version` set to `"3.0"`, the `repo` line removed (`id` may stay; it warns), and the Architecture bullets rewritten as the fenced block (the prose may stay). Acceptance bullets that run the binary are moved into the `acceptance` block as `run` plus `args` cases. The planner keeps accepting `"2.0"` with the v2 rules for versions already in the library.

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

- [F-usage] More than one argument prints `usage: hello [NAME]` on standard error, prints nothing on standard output, and exits with code 2.

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
    only: true
  - dir: internal/cli
    package: cli
    functions:
      - sig: "func Run(args []string, stdout, stderr io.Writer) int"
        satisfies: [F-greet-world, F-greet-name, F-newline, F-usage]
        uses: [internal/greet.Greeting]
    forbid: [exit, stdin, flags]
    only: true
  - dir: cmd/hello
    package: main
    entry:
      call: internal/cli.Run
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

## Decisions

- [D-empty-name] An empty NAME argument (`hello ""`) is greeted as given: the line is `Hello, !`.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.

```acceptance
- id: A-default
  run: hello
  args: []
  stdout: "Hello, World!\n"
  stderr: ""
  exit: 0
- id: A-name
  run: hello
  args: ["Ada"]
  stdout: "Hello, Ada!\n"
  stderr: ""
  exit: 0
- id: A-empty
  run: hello
  args: [""]
  stdout: "Hello, !\n"
  stderr: ""
  exit: 0
- id: A-too-many
  run: hello
  args: ["a", "b"]
  stdout: ""
  stderr: "usage: hello [NAME]\n"
  exit: 2
```
```
