# The brief spec v2, explained

Written 2026-11-05 for whoever writes the next brief, or drafts a v3 of the spec. It says what a v2 brief is, what GopherMind does with each part, which rules are enforced and which are only advice, where the rules live in the code, and what a v3 should change.

## 1. Where the spec lives

There is no single file that is "the v2 spec". Four things define it, in this order of authority:

| What | Where | Role |
| --- | --- | --- |
| The user-facing specification | `gophermind-lib/serve/assets/brief-spec.md`, served at `/app/brief-spec.md` | The document to hand to a brief writer. Sections 1 to 7: layout, frontmatter, body rules, what each section is for, practices, uploading, a complete example (`wordcount`). |
| The frontmatter schema | `gophermind-lib/build/schema/brief-frontmatter.schema.json` | JSON Schema 2020-12. `spec_version` is the constant `"2.0"`, `language` the constant `go`. Unknown keys are refused. |
| The parser, checker and linter | `gophermind-lib/build/brief/{brief.go,check.go,lint.go}` | `Parse` (first error), `Check` (every error with its line), `Lint` (warnings from section 5 of the spec), `UndeclaredSecrets` (warn-only secret scan). |
| The original design document | `docs/build/handoff/SPEC.md` ("GopherMind Brief Spec v2", September 2026) | The reasoning behind v2 and the task-node JSON schema. Parts of it are stale: the `src/` layout, the blackboard and go-git recommendations, "one component node per feature". Read it for intent, not for current rules. |

Fourteen accepted examples are in `examples/briefs/` (`README.md` there tables which options each uses). `14-hello.md` is the smallest; `wordcount` in the served spec is the canonical one.

## 2. What a v2 brief is

One Markdown file, at most 256 KiB:

1. A YAML frontmatter block between two `---` lines, starting on line 1.
2. Seven `##` sections, each exactly once and not empty, in any order: `Overview`, `Features`, `Architecture`, `Data`, `Constraints`, `Out of scope`, `Acceptance`.
3. Under `## Features`, at least one `### <Feature name>` block.

The brief describes one whole program. GopherMind plans a tree of small Go functions from it (contracts, then nodes, then tests), builds each function with a model, and proves the result against the acceptance commands. The brief is the only product specification the models ever see; nothing outside it counts.

### 2.1 Frontmatter

| Key | Required | Rule and meaning |
| --- | --- | --- |
| `spec_version` | yes | The string `"2.0"`. Anything else is refused by the schema. |
| `id` | for `gophermind brief validate` only | Pattern `gm-YYYY-MM-DD-NNN` or `<family>-<version>-<yyyymmdd>-rNNN`. The web server assigns the run id itself (`<family>-<version>-<date>-rNNN`, family from the title) and ignores the line. |
| `title` | yes | 1 to 120 characters. The product name. The library files uploads by title (`library.Slug(title)` is the family). |
| `language` | yes | `go`. The only value accepted. |
| `repo` | no | Local path for `gophermind brief plan` on your machine. The server builds every run in a repository of its own and ignores it. |
| `base_branch` | yes | For example `main`. |
| `work_branch` | no | Defaults to `gm/<id>`. |
| `landing` | yes | `diff_only`, `commit` or `pull_request`. |
| `on_ambiguity` | yes | `halt` (stop and ask) or `assume_and_document` (pick the conservative option and record it on the node). |
| `milestone_approvals` | no | `true` pauses for a human after every wave. Default `false`. |
| `env` | no | List of `name` (UPPER_SNAKE_CASE), `purpose`, optional `default` (string). Non-secret variables the program or its tests read. |
| `secrets` | no | List of `name` and `purpose`. Names only. Values are asked for at run time and live in the vault; they reach commands as environment variables and never enter a prompt or a node file. |
| `network` | no | List of `host` (hostname or `*.suffix`, no scheme or path), `purpose`, `critical`. Hosts the build or program may reach through the harness proxy. `critical: true` means a failed request fails the node. |
| `budget` | no | `max_context_tokens` (at least 1024), `max_revisions` (0 or more). |

Rules the schema cannot express, enforced by the parser: a name may not be declared twice, or in both `env` and `secrets`; the harness-owned names `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `GOPHERMIND_NODE`, `PATH`, `HOME`, `GOCACHE`, `GOMODCACHE`, `GOPATH`, `TMPDIR` may not be declared.

### 2.2 Body rules that refuse a brief

- Under `## Constraints` and `## Acceptance`, every non-blank line must be a bullet (`- `, `* `, `+ `, `1. `, `1) `), an indented continuation, a heading or an HTML comment. A plain paragraph there is refused because it would not count as a requirement.
- Every code fence opened must be closed; an unclosed fence hides everything after it.
- A `##` section may not appear twice. The seven required ones may not be missing or empty.
- `## Features` must contain at least one `###` block.

### 2.3 Warnings that do not refuse

- Undeclared secret-looking tokens: `UPPER_SNAKE_CASE` ending in `_KEY`, `_SECRET`, `_TOKEN`, `_URL`, `_DSN`, `_PASSWORD` or `_PASSPHRASE`, or any such token on a line mentioning "secret", "credential" or "environment variable". Declared `env` and `secrets` names are exempt.
- Lint (section 5 of the spec): acceptance runs the program with `go run`, `go install`, `go build -o` or a `./path`; acceptance lacks `go build ./...`, `go vet ./...` or `go test`; Architecture does not put `main` in `cmd/`; the brief reads standard input but Architecture never says `io.Reader`; more than 400 lines.

## 3. What GopherMind does with each section

This is the part the served spec does not say, and the part a brief writer most needs. Verified against the planner code on 2026-11-05 (Milestone 1 of the reliability work; see `docs/superpowers/plans/2026-11-05-planner-reliability-m1.md`).

| Section | What the planner reads from it |
| --- | --- |
| Overview | Context for the clarify and outline stages only. It is dropped from the per-function packets to save tokens, so nothing that must reach an implementer may live only here. |
| Features | The requirements. Each `### Feature` becomes a numbered feature requirement (`F1`, `F2`) that coverage must map to at least one function, and the test writer tests against its text. Today the outline also creates one component per feature heading; this is a v2 assumption being loosened (a function may serve several features). Exact output text, exit codes, flag names and error messages written here are tested as written. |
| Architecture | The fixed facts. Each bullet that starts with a directory in backticks (`` `internal/cli` ``) names a package directory; the planner maps the outline's components onto those directories. A backticked span shaped like a Go signature on such a bullet (`` `Run(args []string, stdout, stderr io.Writer) int` ``) is a fixed interface: it is shown to the model and enforced by name and exact signature after the component pass, with one repair round, then an error. A `cmd/<name>` bullet is the entry package: its `main` is created once in the entry pass, and the first `pkg.Func` on that bullet that names a non-cmd Architecture package (`cli.Run`) is the fixed entry call. Before the acceptance commands run, every `main` package under `cmd/` is built and its binary put on PATH. |
| Data | Shared types. The outline writes a types component from it; fields and Go types written here become the type declarations other functions use. |
| Constraints | Numbered constraint requirements (`C1`, `C2`), copied into every node's context and mapped by coverage to a function or a root test. Style, dependency, Go version and test-style rules go here. |
| Out of scope | Given to the planner so it does not build it, and used to refuse additions (a `stdin` parameter when stdin is out of scope). |
| Acceptance | Numbered acceptance requirements (`A1`, `A2`). Each bullet becomes a root test: a shell command run against the built binaries after the leaves pass, with its expected result. Commands are checked before approval: a vacuous command (no built binary, `go run`), a masked failure (`|| true`), a print-only test or an undefined variable is refused. |

Two facts that follow from this and bite brief writers:

- Architecture is parsed by convention, not by a grammar. Put the directory first in backticks, one directory per bullet, the signatures on that bullet in backticks exactly as Go would write them (parameter names included), and the entry call as `pkg.Func`. A backticked call example on an Architecture bullet (`` `Make(s, 0)` ``) is skipped, not treated as a signature, but a prose signature without backticks is not seen at all.
- Features are the oracle. The test writer and the coverage check derive expected values from the feature text, so "prints a greeting" is not testable and `` prints `Hello, World!` followed by one newline `` is.

## 4. Writing a brief that builds

The practices in section 5 of the served spec, with the reason each exists:

1. One behaviour per bullet. The planner splits features into functions of roughly 40 lines; a bullet with three behaviours becomes one large function nobody can test.
2. Say how input reaches the code. A function that reads standard input takes an `io.Reader`, output goes to an `io.Writer`, only `main` touches `os.Stdin` and `os.Stdout`. The planner refuses a function that reads standard input without a reader.
3. Say how tests are written: table driven, `strings.NewReader` and `bytes.Buffer`, never real files.
4. Settle every edge the tests will assert (empty input, overflow, malformed row). An unsettled edge is invented by the test writer and the build then fails against the invention.
5. Exact text: outputs, error messages and exit codes in full, including the newline rule.
6. `main` in `cmd/<name>/`, said so in Architecture.
7. Acceptance runs the binary by name (`printf 'a\n' | wordcount -`) with the result stated, plus `go build ./...`, `go vet ./...`, `go test ./...`.
8. Standard library unless a module is named and its download host is in `network`.
9. No secret values anywhere in the file.
10. Short: one program, a few hundred lines at most.

A minimal skeleton (the `hello` example, accepted as written):

```markdown
---
spec_version: "2.0"
id: gm-2026-01-01-001
title: hello
language: go
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
---

## Overview
One paragraph: what it does, for whom, one binary.

## Features
### Say hello
`hello [NAME]` prints one line and exits 0.
- With no argument the line is `Hello, World!`.
- The line ends with exactly one newline. Nothing on standard error.
### Report errors
- More than one argument prints `usage: hello [NAME]` on standard error and exits with code 2.

## Architecture
- `cmd/hello` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr`, and exits with the returned code. Nothing else.
- `internal/greet`: `Greeting(name string) string` is pure. No printing.
- `internal/cli`: `Run(args []string, stdout, stderr io.Writer) int` checks the argument count, calls `greet.Greeting` and prints the result. It never calls `os.Exit`.

## Data
None.

## Constraints
- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven and hermetic, using `bytes.Buffer` for output.

## Out of scope
- Reading a name from standard input, flags, localisation.

## Acceptance
- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `hello` prints `Hello, World!`.
- `hello a b` exits with code 2 and prints nothing on standard output.
```

## 5. What a v3 should change

Each item names the v2 weakness, what the planner does today, and the proposed form. The first five come straight from the failed `hello` run and John's fix brief (`docs/superpowers/specs/2026-11-05-hello-pipeline-fixes-brief.md`).

1. **Architecture as data, not prose.** Today directories, signatures, ownership and forbidden effects are recovered from backticks by regular expressions. v3: a fenced `architecture` block (YAML or a table) per package with `dir`, `package`, `functions` (full Go signatures), `uses`, `pure`, `forbidden` (`os.Exit`, printing, stdin), `entry: true` with the call `main` makes. Prose stays allowed beside it. The parser gives the planner typed facts; nothing is inferred.
2. **Requirements with stable ids, many-to-many with code.** v2 ties one component to one `### Feature` heading. v3: every bullet under Features, Constraints and Acceptance carries an id (`F1.2`, or an explicit `[id]` prefix), and the architecture block lists which requirement ids each function satisfies. Coverage and the test writer then work from ids, not from heading names.
3. **Exact bytes for outputs.** "prints `Hello, World!`" leaves the newline and stderr ambiguous. v3: an acceptance case is a tuple: command, stdout (fenced, byte-exact, `\n` visible), stderr, exit code. The harness runs it with captured status instead of asking a model to write a shell check. Keep free-form bullets for `go build`, `go vet`, `go test`.
4. **Directory and package name as two fields.** `package` in the contract means a directory but is read as a Go name in places. v3 declares `dir` and `package` (clause) separately; `cmd/x` implies `main`.
5. **Entry capabilities declared.** Whether `main` may call `os.Exit` (and nothing else may) should be a fact of the architecture block, not a rule the executor and the planner each guess.
6. **Conventions the models must honour, as keys.** "No logging", "no flags", "table-driven tests", "hermetic tests" are today prose constraints that enrichment and the test writer can ignore. v3: a `conventions` map with known keys (`logging: none`, `tests: table_driven, hermetic`) that the planner checks structurally and that enrichment cannot override.
7. **Settled decisions in the file.** The clarify stage asks questions per run; answers are not written back. v3: a `## Decisions` section (or frontmatter `decisions`) with the question and the chosen answer, so a re-run skips the question and the packet carries the decision.
8. **Frontmatter cleanup.** `id` is required for `validate` and ignored on upload; `repo` is ignored on upload; `language` can only be `go`. v3: `id` optional everywhere (server assigns), `repo` a CLI flag only, `language` kept with the same constant for a future second language, `spec_version: "3.0"` and a parser that accepts `2.0` with the v2 rules for the existing library.
9. **Size and shape limits that match the models.** The 400-line lint and the 256 KiB cap are not derived from the context window the local model has. v3 states the budget per section (features text that must fit a node packet) and the planner refuses early with the section to shorten.
10. **Split the documents.** `SPEC.md` mixes the brief format with harness design (blackboard, go-git, waves). v3 is two documents: the brief specification (what a writer needs) and the harness design (what a builder needs), each with its own version.

Things to keep as they are: the seven sections and their order independence, bullets-only under Constraints and Acceptance, secrets by name only, `cmd/<name>` for binaries, acceptance by binary name, the warn-only secret scan, and the practice list in section 5 of the served spec.
