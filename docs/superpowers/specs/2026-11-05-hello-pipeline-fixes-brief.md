# GopherMind pipeline fixes: the Hello World benchmark

Received from John on 2026-11-05 11:06 MST ("these are the fixes to the current gophermind system"). Verbatim below. The run it describes is `hello-v1-20261105-r001`, captured as `gophermind-lib/build/planner/testdata/hello`. It overlaps the planner reliability brief (`2026-11-05-planner-reliability-implementation-brief.md`); the plan `docs/superpowers/plans/2026-11-05-hello-pipeline-fixes.md` maps the two.

---

You are working in the GopherMind repository.

Your task is to fix the planning, contract-generation, decomposition, coverage, test-generation, execution, and repair pipeline so that GopherMind can reliably build a trivial Go command-line application from a precise brief without losing requirements, inventing architecture, generating contradictory tasks, or getting stuck in repair loops.

Do not solve this by special-casing the word "hello", the specific fixture below, component names, or individual function names. Fix the general planner/harness behavior that caused these failures.

The minimum acceptance test for this work is that GopherMind can take the Hello World brief at the end of this prompt and autonomously produce a correct, buildable, tested Go application.

## Primary goal

GopherMind must treat the uploaded brief as the authoritative product specification.

Models may propose implementation structure only where the brief leaves implementation choices open.

When the brief explicitly specifies:

- a package,
- a repository path,
- a function name,
- a function signature,
- ownership,
- a side effect,
- an exit code,
- an output string,
- an error condition,
- or an architectural relationship,

those facts are fixed constraints.

Later planner stages must not rename, reinterpret, omit, or contradict them.

For the Hello World fixture, a successful planner should converge on approximately three implementation responsibilities:

```text
internal/greet
  Greeting(name string) string

internal/cli
  Run(args []string, stdout, stderr io.Writer) int

cmd/hello
  main()
```

Private helpers are allowed only if they are justified by real complexity. They are not required merely because GopherMind supports decomposition.

---

# 1. First inspect the current implementation

Before changing code:

1. Read the applicable repository guidance:
   - `AGENTS.md`
   - `PROJECT.md`
   - relevant `CONTEXT.md`
   - planner/PhaseFlow ADRs or design documents
   - existing planner tests

2. Locate the real code responsible for:
   - brief parsing
   - clarification
   - requirement extraction
   - contract outline generation
   - component contract generation
   - fixed architecture/signature handling
   - task decomposition
   - enrichment
   - requirement coverage
   - unit-test generation
   - root acceptance-test generation
   - executor prompt construction
   - generated-source validation
   - retries/revisions
   - contract repairs
   - persistence/checkpoint reuse

3. Trace one run from uploaded brief through implementation dispatch.

Do not assume paths from this prompt are exact. Find the actual symbols and callers in the checkout.

Preserve unrelated local edits.

---

# 2. Add the Hello World run as a regression fixture first

Before fixing implementation behavior, create a fixture based on this prompt's Hello World brief.

Capture the intended externally observable result independently from current planner output.

The regression must fail for the current known defects rather than simply checking that the planner emitted some JSON.

At minimum, the regression must prove that the final accepted plan contains these fixed declarations:

```go
func Greeting(name string) string
```

in:

```text
internal/greet
```

and:

```go
func Run(args []string, stdout, stderr io.Writer) int
```

in:

```text
internal/cli
```

and:

```go
func main()
```

in:

```text
cmd/hello
```

It must also prove that successful CLI behavior has not disappeared:

```text
hello
=> stdout: "Hello, World!\n"
=> stderr: ""
=> exit 0

hello Ada
=> stdout: "Hello, Ada!\n"
=> stderr: ""
=> exit 0

hello a b
=> stdout: ""
=> stderr: "usage: hello [NAME]\n"
=> exit 2
```

Use the brief itself as the independent oracle. Do not generate the expected values from the generated contract being tested.

---

# 3. Separate product requirements from implementation components

The current planner incorrectly assumes that every `### Feature` heading maps one-to-one to an implementation component.

Remove that assumption.

A feature describes required behavior.

A component describes implementation ownership.

These are separate concepts and require a many-to-many relationship.

For example, this is valid:

```text
Requirement F1: Say hello
  -> greet.Greeting
  -> cli.Run
  -> process acceptance behavior

Requirement F2: Report errors
  -> cli.Run
  -> process acceptance behavior
```

while the implementation components are:

```text
internal/greet
internal/cli
cmd/hello
```

Do not force:

```text
feature heading == package == component
```

because one function may satisfy requirements from several feature sections.

Represent requirements independently with stable IDs.

A useful conceptual shape is:

```go
type Requirement struct {
    ID       string
    Kind     RequirementKind
    Source   SourceLocation
    Text     string
}
```

and some form of explicit association:

```text
requirement -> component/function
component/function -> requirement IDs
```

Use the repository's existing types and conventions where possible rather than introducing parallel infrastructure unnecessarily.

---

# 4. Extract fixed architecture before open-ended planning

Before asking a model to design functions, extract explicit architecture facts from the brief.

Examples of fixed facts include:

```text
package directory
Go package name
function name
exact function signature
exported/unexported status
ownership
required dependencies
required side effects
forbidden side effects
```

For this Hello fixture, these are immutable:

```text
A1:
directory: internal/greet
package: greet
signature: func Greeting(name string) string
pure: true
printing: forbidden

A2:
directory: internal/cli
package: cli
signature: func Run(args []string, stdout, stderr io.Writer) int
os.Exit: forbidden

A3:
directory: cmd/hello
package: main
signature: func main()
behavior:
  call cli.Run(os.Args[1:], os.Stdout, os.Stderr)
  pass result to os.Exit
  nothing else
```

The contract-generation model may propose private helpers around these declarations.

It may not replace:

```text
Greeting
```

with:

```text
FormatGreeting
```

or add a replacement output helper as the application's public architecture.

It may not transform:

```go
func Run(args []string, stdout, stderr io.Writer) int
```

into:

```go
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int
```

These should be deterministic contract-validation failures.

---

# 5. Distinguish package directory from Go package name

If the existing schema overloads `package` to sometimes mean:

```text
internal/cli
```

and sometimes:

```text
cli
```

fix that ambiguity.

Use separate concepts, equivalent to:

```json
{
  "package_dir": "internal/cli",
  "package_name": "cli"
}
```

The actual field names can follow repository conventions.

Validate that every generated source file belongs to the package directory that owns it.

For example:

```text
component.package_dir = internal/cli
function.file = internal/cli/run.go
function.package_name = cli
```

is valid.

Do not treat files in different directories as one Go package merely because both say `package cli`.

---

# 6. Propagate all requirements relevant to a function

Do not give a component/function only the text under one feature heading.

The current failure pattern is:

```text
Run belongs to "Report errors"

therefore Run receives only:
  "more than one argument prints usage..."
```

and loses:

```text
successful execution must print a greeting
```

That is unacceptable.

When constructing a component or function planning packet, include:

1. every source requirement mapped to it,
2. applicable architecture constraints,
3. applicable global constraints,
4. settled decisions,
5. documented assumptions,
6. applicable out-of-scope constraints.

For Hello's `Run`, its packet must contain both successful and error behavior:

```text
0 args:
  Greeting("")
  write "Hello, World!\n"
  return 0

1 arg:
  Greeting(args[0])
  write greeting + newline
  return 0

>1 args:
  stdout empty
  stderr "usage: hello [NAME]\n"
  return 2
```

plus the fixed architecture:

```go
func Run(args []string, stdout, stderr io.Writer) int
```

and:

```text
must call greet.Greeting
must not call os.Exit
```

If the prompt cannot fit all applicable requirements, split the work. Do not silently drop source requirements.

---

# 7. Make candidate contract updates transactional

A model response is a proposal, not authoritative state.

Before merging any model-produced contract records, validate the complete candidate.

Validate at least:

- schema
- allowed records for the active stage
- unique IDs
- component ownership
- package directory
- package name
- file placement
- exact fixed signatures
- fixed architecture
- valid dependency IDs
- dependency accessibility
- source requirement associations

If one record in a response is invalid, do not merge the apparently valid subset of that response unless the existing architecture explicitly supports validated partial transactions.

The safest initial behavior is atomic acceptance or rejection per response.

Examples that must be rejected:

```text
report-errors pass emits fn-main
```

```text
internal/cli component emits a file under cmd/hello
```

```text
fixed Run signature gains stdin
```

```text
entry-point pass emits another conflicting fn-run
```

---

# 8. Replace first-definition-wins with explicit conflict handling

Do not silently preserve the first version of a conflicting ID.

Use behavior equivalent to:

```text
new ID:
    add

same ID + canonical content identical:
    idempotent replay

same ID + content different:
    conflict
```

A conflicting existing declaration requires an explicit replacement/repair operation.

This matters because an incorrect first declaration must be repairable.

---

# 9. Make repair operations capable of actually replacing bad state

Repairs must not merely append more declarations.

Support explicit operations equivalent to:

```text
replace component
replace function
replace type
remove dependency
retarget dependency
add justified declaration
remove obsolete declaration
```

A repair must identify:

```text
base contract revision
target ID
reason
proposed change
```

Apply the repair to a candidate snapshot, validate the candidate, then commit.

Do not create workaround identities such as:

```text
entry-point-main
```

just because the original `entry-point` is wrong.

Repair the original record.

---

# 10. Version the contract and prevent stale work

Each accepted contract change should advance a contract revision or equivalent immutable version identifier.

Every downstream artifact should know which validated contract revision created it:

```text
decomposition
enrichment
tests
implementation packet
worker result
review
```

If the contract changes, invalidate affected derived artifacts.

Before accepting a returning worker result, verify that it still targets the active contract revision.

A worker result produced against an obsolete contract must not become current.

Persist the new contract and its stale-artifact invalidations atomically enough that a crash cannot expose a new contract with old tasks considered ready.

Use the repository's existing persistence architecture.

---

# 11. Make structural decomposition deterministic where possible

Do not ask an LLM to rediscover facts already present in the validated function signature.

The harness can derive:

```text
function ID
package
file
signature
parameter names
parameter Go types
return types
dependency signatures
requirement IDs
```

from the accepted contract.

Use Go parsing rather than model inference for signatures.

For example:

```go
func Run(args []string, stdout, stderr io.Writer) int
```

must deterministically produce three parameters:

```text
args    []string
stdout  io.Writer
stderr  io.Writer
```

There is no `stdin`.

Do not let decomposition change these facts.

Use the model only for semantic material that genuinely requires interpretation.

---

# 12. Keep dependencies separate from side effects

Do not treat every function call as an external side effect.

Maintain a clear distinction:

```text
depends_on / uses:
  dependencies on other contract declarations

side_effects:
  writes
  filesystem mutation
  network activity
  process exit
  persistent state mutation
  other externally observable effects
```

A pure function calling another pure helper can remain pure.

---

# 13. Do not let enrichment alter the contract

Enrichment exists to explain an already accepted contract.

It must never introduce product behavior.

It may explain:

```text
construction approach
rationale
security implications
performance considerations when relevant
portability when grounded
```

It may not add:

```text
logging that the application does not require
input sanitization contrary to "insert as given"
unsupported OS restrictions
unsupported architecture restrictions
new flags
new parameters
new dependencies
new output behavior
```

If conventions say:

```text
no logging
```

then enrichment must not add:

```text
log "Command execution started"
```

If the harness knows the current platform is Darwin/arm64, do not mechanically emit Linux/amd64 metadata.

Do not confuse:

```text
current execution host
```

with:

```text
supported application platforms
```

---

# 14. Fix model-tier assignment

If GopherMind has routing rules such as:

```text
I/O orchestration => strong
pure function => any
```

enforce them deterministically after decomposition.

Do not depend on the model to correctly label its own difficulty.

If the required model tier is unavailable, report that explicitly.

Do not silently downgrade.

---

# 15. Generate tests independently from original requirements

Unit-test generation must receive:

```text
the accepted function contract
AND
the original requirements that function satisfies
AND
the applicable architecture constraints
```

The test writer must not use the generated contract as its sole behavioral oracle.

Otherwise one bad interpretation produces both:

```text
bad implementation contract
bad tests that approve that contract
```

For Hello's `Run`, tests must independently know that success prints a greeting.

Required cases include at least:

```text
args = []
stdout = "Hello, World!\n"
stderr = ""
return = 0

args = ["Ada"]
stdout = "Hello, Ada!\n"
stderr = ""
return = 0

args = ["a", "b"]
stdout = ""
stderr = "usage: hello [NAME]\n"
return = 2
```

The tests for `Greeting` should independently derive:

```text
Greeting("") == "Hello, World!"
Greeting("Ada") == "Hello, Ada!"
```

Do not generate tests for invented functions if the planner should never have invented them.

---

# 16. Strengthen requirement coverage auditing

Coverage cannot merely ask a model:

```text
which node seems related to this requirement?
```

Coverage must verify that the mapped contracts actually contain the behavior.

Maintain explicit requirement IDs on declarations/tasks.

For every requirement, check that:

```text
at least one responsible implementation declaration exists
applicable behavior is represented
applicable acceptance verification exists
```

For Hello F1, coverage must detect that a `Run` contract which never writes the greeting does NOT satisfy the requirement.

For explicit architecture, coverage must detect that:

```text
required Greeting missing
```

or:

```text
Run signature changed
```

even if a similarly named replacement function exists.

---

# 17. Validate root acceptance tests before accepting them

Generated shell commands are executable program logic and must be checked.

Add deterministic validation for common invalid constructions.

The following current examples are wrong:

Wrong gofmt check:

```sh
gofmt -l . | grep -q . || ...
```

because unformatted files make the command succeed.

Prefer:

```sh
test -z "$(gofmt -l .)"
```

Wrong exit-code check:

```sh
hello a b > /dev/null && test ...
```

because the correct nonzero exit prevents the assertion from running.

Generate an acceptance test that deliberately captures the status:

```sh
out=$(mktemp)
err=$(mktemp)
hello a b >"$out" 2>"$err"
status=$?
test "$status" -eq 2 &&
test ! -s "$out" &&
test "$(cat "$err")" = "usage: hello [NAME]"
```

Use cleanup appropriate to the repository's acceptance harness.

The validator should reject at least:

```text
masked failures
wrong && handling of expected nonzero status
vacuous print-only tests
unasserted command output
undefined shell variables
forbidden go run usage
tests that never execute the built binary when CLI behavior is being verified
```

Do not claim a stylistic constraint such as:

```text
tests are table driven
```

has been proven simply because:

```sh
go test ./...
```

passes.

Either inspect the required structural property or classify it separately from executable behavioral verification.

---

# 18. Fix the executor's os.Exit contradiction

This is mandatory.

The Hello brief explicitly requires:

```go
func main() {
    os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
```

The current executor apparently prohibits `os.Exit` globally.

That makes the task impossible.

Change the generated-source safety policy so that process termination is allowed only when the validated node explicitly possesses that capability.

Use behavior equivalent to:

```text
default:
  os.Exit forbidden

exception:
  node is validated process entry point
  package == main
  signature == func main()
  accepted contract explicitly requires process exit
```

Prefer an explicit node capability rather than text matching:

```json
{
  "process_actions": {
    "allow_exit": true
  }
}
```

or the repository's equivalent.

For all other generated functions:

```text
os.Exit remains forbidden
```

The source validator and executor prompt must agree.

Do not tell the model to call `os.Exit` and then reject that exact source afterward.

---

# 19. Preflight executor policy before dispatch

Before giving a function to a builder, compare:

```text
required operations
vs.
executor policy
```

If a contract requires an operation the executor forbids, fail before the implementation retry ladder.

Return a deterministic planner/harness error such as:

```text
EXECUTION_POLICY_CONFLICT
```

with enough context to identify:

```text
function ID
required action
blocking rule
```

Do not spend four builder attempts rediscovering a contradiction that the harness can detect itself.

---

# 20. Improve retry and revision behavior

Retries should be used for genuinely different possible implementation attempts.

They should not repeatedly send the same impossible packet.

Track canonical attempt content.

If a builder returns materially identical source after the same deterministic rejection, do not continue the ordinary retry ladder indefinitely.

Escalate to the appropriate planner/contract/policy repair immediately.

Likewise, a repair that produces no material state change should count as:

```text
no_progress
```

and consume a bounded repair budget.

---

# 21. Reduce unnecessary decomposition

Do not split trivial explicit behavior into many functions merely to create more tasks.

For Hello World, this is sufficient:

```go
Greeting
Run
main
```

The existing brief already establishes good seams.

A helper should be created only when:

- a function would otherwise become meaningfully complex,
- the helper represents a reusable responsibility,
- the helper improves a real test seam,
- or another explicit requirement justifies it.

Do not automatically invent:

```text
FormatGreeting
WriteGreeting
validateArgs
formatUsageError
writeUsageError
```

for a three-function application.

Avoiding unnecessary nodes also reduces LLM calls and opportunities for contract drift.

---

# 22. Preserve newline requirements exactly

The Hello brief requires:

```text
success:
exactly one trailing newline

usage error:
the usage line on stderr
```

Make the expected bytes explicit in the accepted behavioral contract:

```text
Hello, World!\n
Hello, Ada!\n
usage: hello [NAME]\n
```

Do not leave newline behavior distributed ambiguously between formatter functions and writer helpers.

---

# 23. Do not add stdin when the brief excludes it

The Hello brief explicitly puts reading a name from stdin out of scope.

Therefore this is invalid:

```go
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int
```

The fixed architecture requires:

```go
func Run(args []string, stdout, stderr io.Writer) int
```

Add a regression assertion that an explicitly fixed signature cannot gain parameters during:

```text
contract generation
normalization
decomposition
enrichment
repair
```

---

# 24. Add semantic validation before implementation readiness

A contract may be structurally valid JSON and still be wrong.

Before marking the contract ready, verify at least:

```text
all fixed architecture declarations exist
fixed signatures match
package placement matches
required dependencies are represented
every requirement has responsible declarations
critical output/error behavior has not disappeared
no explicit out-of-scope behavior was added
executor policy is compatible
```

For this fixture, the contract must not become implementation-ready unless it proves:

```text
Greeting exists and is pure
Run calls/depends on Greeting
Run owns success output
Run owns usage-error behavior
main calls Run
main may call os.Exit
no stdin parameter exists
```

---

# 25. Treat stage "ok" separately from semantic validity

A successful model request means only that the model invocation completed and its response was parseable.

Do not use a single status such as:

```text
ok
```

to imply the result is correct.

Track statuses conceptually equivalent to:

```text
model_status
schema_status
candidate_validation_status
contract_status
verification_status
```

For example:

```text
model_status: ok
schema_status: ok
contract_status: rejected_fixed_signature_mismatch
```

This makes traces far easier to debug.

---

# 26. Expected Hello World plan

After the fixes, the planner should produce behavior equivalent to the following.

## Component 1

```text
id: greet
package_dir: internal/greet
package_name: greet
requirements:
  - say-hello
```

Function:

```go
func Greeting(name string) string
```

Behavior:

```text
name == "" -> "Hello, World!"
otherwise  -> "Hello, " + name + "!"

pure
no output
no process actions
```

## Component 2

```text
id: cli
package_dir: internal/cli
package_name: cli
requirements:
  - say-hello
  - report-errors
```

Function:

```go
func Run(args []string, stdout, stderr io.Writer) int
```

Behavior:

```text
len(args) == 0:
    greeting = greet.Greeting("")
    write greeting + "\n" to stdout
    write nothing to stderr
    return 0

len(args) == 1:
    greeting = greet.Greeting(args[0])
    write greeting + "\n" to stdout
    write nothing to stderr
    return 0

len(args) > 1:
    write nothing to stdout
    write "usage: hello [NAME]\n" to stderr
    return 2
```

Dependencies:

```text
greet.Greeting
```

Forbidden:

```text
os.Exit
reading stdin
flags
configuration
network
filesystem
```

## Component 3

```text
id: entry-point
package_dir: cmd/hello
package_name: main
```

Function:

```go
func main()
```

Behavior:

```go
os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
```

No other application logic.

Node capability:

```text
process exit allowed
```

---

# 27. Expected generated source

A valid final application may be as small as:

```go
// internal/greet/greet.go
package greet

func Greeting(name string) string {
    if name == "" {
        return "Hello, World!"
    }
    return "Hello, " + name + "!"
}
```

```go
// internal/cli/cli.go
package cli

import (
    "fmt"
    "io"

    "hello/internal/greet"
)

func Run(args []string, stdout, stderr io.Writer) int {
    if len(args) > 1 {
        fmt.Fprintln(stderr, "usage: hello [NAME]")
        return 2
    }

    name := ""
    if len(args) == 1 {
        name = args[0]
    }

    fmt.Fprintln(stdout, greet.Greeting(name))
    return 0
}
```

```go
// cmd/hello/main.go
package main

import (
    "os"

    "hello/internal/cli"
)

func main() {
    os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
```

The precise file split may vary if the brief does not fix filenames, but the packages, function signatures, responsibilities, and behavior may not vary.

---

# 28. Required tests for GopherMind itself

Add planner/harness regression tests covering at least the following.

### Fixed signature preservation

Given:

```go
func Run(args []string, stdout, stderr io.Writer) int
```

no later stage may add:

```text
stdin
```

### Fixed name preservation

Given:

```go
Greeting
```

the canonical contract may not replace it with:

```text
FormatGreeting
```

### Cross-feature function requirements

A function responsible for behavior from two feature headings receives both sets of requirements.

### Foreign declaration rejection

A contract response for `internal/cli` that emits:

```go
func main()
```

is rejected before merge.

### Duplicate conflict

A second different declaration using an existing function ID is rejected as a conflict.

### Idempotent replay

An identical replay does not mutate canonical state or count as meaningful progress.

### Requirement coverage

A plan where `Run` returns 0 on valid args but never writes a greeting fails F1 coverage.

### Executor capability

`os.Exit` is:

```text
accepted for validated main
rejected for internal/cli.Run
rejected for internal/greet.Greeting
```

### Policy preflight

A node whose required actions conflict with executor policy is blocked before builder dispatch.

### Root test validation

Reject acceptance commands that:

```text
lose the expected nonzero exit status through &&
invert gofmt logic
mask failures
never execute the built CLI
```

### Stale work

A worker response from an obsolete contract revision cannot be accepted.

### Repair effectiveness

Replacing a bad canonical declaration changes subsequent prompts.

A repair producing no canonical change is recorded as no progress.

### No needless stdin

An out-of-scope stdin capability does not appear in the Hello application.

---

# 29. End-to-end acceptance requirements

Do not consider this work complete until the repaired GopherMind pipeline can process the following brief from scratch and reach a successful terminal result without manual intervention.

It must generate source, tests, and a final application that passes:

```sh
go build ./...
go vet ./...
go test ./...
```

It must also build the `cmd/hello` binary and verify:

```text
hello
stdout exactly: Hello, World!\n
stderr exactly empty
exit code 0
```

```text
hello Ada
stdout exactly: Hello, Ada!\n
stderr exactly empty
exit code 0
```

```text
hello a b
stdout exactly empty
stderr exactly: usage: hello [NAME]\n
exit code 2
```

The run must not require a repair for:

```text
missing Greeting
added stdin
foreign main ownership
os.Exit being forbidden in main
missing success behavior in Run
```

Those should be prevented deterministically before implementation.

---

# 30. Verification procedure

Use a test-driven workflow.

For each failure class:

1. Add or identify a regression test that demonstrates the current defect.
2. Run it and confirm it fails for the intended reason.
3. Make the smallest architectural fix that corrects the class of defect.
4. Run the focused test again.
5. Run neighboring planner/harness tests.
6. Continue to the next defect.

After all focused fixes:

```sh
go test ./...
go vet ./...
```

and any repository-defined planner/PhaseFlow validation commands.

Then perform a fresh Hello World run through the actual GopherMind pipeline.

Do not merely construct the expected source manually and call that success. The planner, test generator, executor, validators, and acceptance system must produce and verify it through the normal application workflow.

Report:

- files changed,
- architectural changes made,
- regression tests added,
- tests run and actual results,
- the final Hello run stage summary,
- the final generated contract,
- the generated source tree,
- final build/vet/test results,
- CLI acceptance results,
- any remaining known limitations.

---

# Regression brief

Use this exact brief as the minimum end-to-end fixture:

```markdown
---
id: hello-v1-regression
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
```

# Definition of done

This task is complete only when a fresh GopherMind run of that brief:

1. preserves the explicit architecture,
2. produces `Greeting`, `Run`, and `main` with the correct exact signatures,
3. does not introduce stdin,
4. does not replace `Greeting`,
5. does not create an extra command-local runner,
6. preserves both success and error behavior in `Run`,
7. generates tests that independently verify the original brief,
8. allows `os.Exit` only for the validated process entry point,
9. rejects invalid model contract proposals before they become canonical state,
10. generates valid acceptance checks,
11. builds successfully,
12. passes `go vet`,
13. passes all tests,
14. passes all three binary-level acceptance cases,
15. reaches completion without an implementation/revision loop caused by a contradiction created by GopherMind itself.

Do not weaken the Hello brief to make the pipeline pass.

Fix GopherMind so the pipeline can satisfy the brief as written.
