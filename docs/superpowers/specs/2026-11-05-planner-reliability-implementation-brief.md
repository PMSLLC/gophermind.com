Change GopherMind so that each planning stage proposes a bounded change, the harness validates that change, and only a consistent contract can produce implementation tasks. Preserve the complete behavioral requirements from the brief, keep package and function ownership explicit, and let repairs update the records they are intended to correct.

The acceptance benchmark is `slugify-v1-20261105-r001`. Its recorded sessions expose the failures to reproduce: feature text disappears from downstream prompts, package directories become inconsistent, replacement attempts retain the first incorrect record, and later stages expand incomplete contracts. The work below changes the existing planning pipeline, its prompts, validation, and recovery behavior.

# 1 Implementation outcome

The completed pipeline must satisfy these conditions:

- Every implementation task receives the original requirements relevant to its function, including exact diagnostics, ordering rules, examples, applicable constraints, and settled assumptions.

- Components have explicit package locations. Every function has one owning component and its file belongs to that component's Go package. Every type has one canonical home package; a shared-declaration bookkeeping node does not dictate its physical directory.

- Model responses remain provisional until the checks appropriate to their stage pass.

- Incomplete drafts can accumulate legitimate pending references while scheduled passes continue.

- The completed contract must pass global validation before implementation tasks become eligible for execution.

- Repairs may replace records, remove incorrect references, retarget references, or add justified declarations. A successful repair changes the stored state and resolves its stated defect.

- A resumed run reuses work only when its recorded inputs remain current.

- Success reporting distinguishes a completed model request from an accepted contract, completed implementation, and passed verification.

Implement this within the current planner and persistence mechanisms. Retain function-level tasks and the current model-provider system. Use the original slugify brief as a regression fixture; do not embed slugify-specific behavior in the general planner.

# 2 Locate the affected subsystems

Locate the current implementation before changing source. Search for the stage names and prompt fragments shown in the trace, then follow their callers into context selection, merging, validation, task creation, and checkpoints.

A useful starting search is:

```bash
rg -n 'contract:outline:entry|contract:_repair|brief_section|entry-point-main|model_tier|Already declared' .
```

Use the repository's existing instructions and tests. The following names identify responsibilities, rather than presumed source filenames.

| Subsystem | Required change |
| - | - |
| Brief parser and context selector | Retain complete feature requirements and explicit source associations |
| Repository fact collector | Supply verified module, layout, toolchain, and execution-target facts when available |
| Outline and component prompt renderer | Respect fixed architecture and permit only the records assigned to the active pass |
| Contract registry and merger | Enforce ownership and duplicate rules; accept explicit replacement transactions |
| Contract validator | Separate local draft checks from global readiness |
| Dependency repair | Repair an incorrect edge as well as a missing declaration |
| Task materializer | Copy structural metadata from the validated contract |
| Enrichment renderer and validator | Add design notes without changing behavior or contract identity |
| Test planner and runner | Receive original requirements and use executable, hermetic checks |
| Checkpoint and resume logic | Record input fingerprints and invalidate stale derived work |
| Session reporting | Display validation and application outcomes separately from model-call completion |

Version changes to persisted planner contracts separately from the product brief's `spec_version`. Update schemas, prompt examples, decoders, validators, checkpoint readers, and tests together.

# 3 Preserve requirements through every stage

## 3 1 Fix this benchmark first

For the slugify fixture, send the complete original brief to contract generation, decomposition, enrichment, and test generation. Its size makes full propagation the simplest safe repair.

Sessions 6 through 8 and 10 through 13 receive only Architecture under `brief_section`. Their missing feature rules cannot be recovered reliably from function names or generic documentation. Preserve the literal rules before optimizing prompt size.

Include settled decisions and assumptions separately so that an assumption cannot silently replace an explicit requirement.

## 3 2 Retain source associations for larger briefs

Give source sections or normative source blocks stable identifiers within a version of the brief. Retain the original text and its source location. Associate components and functions with those identifiers explicitly.

For example, a requirement record may contain:

```json
{
  "id": "req-slug-trailing-hyphen",
  "source_section": "Features / Make a slug",
  "text": "`--max N` with N at least 1 cuts the slug to at most N characters, then strips a trailing hyphen: `--max 6` on `Hello, World!` gives `hello`, and `--max 99` changes nothing."
}
```

The exact field names may follow the current schema. The required information is the stable identifier, source location, and retained requirement text.

Allow many-to-many associations. The CLI needs invocation and input requirements from Make a slug as well as the entire Report errors section. A generated component name such as `cli-logic` is not a sufficient source selector.

| Component | Required source |
| - | - |
| Slug logic | Transformation rules, examples, limit semantics, and pure-function architecture |
| CLI logic | Invocation grammar, input selection, whole-input behavior, diagnostics, precedence, and CLI architecture |
| Entry point | Required process arguments, streams, runner call, and exit behavior |
| Every applicable task | Constraints, exclusions, and relevant settled assumptions |

Keep global requirements such as formatting and dependency restrictions in shared policy where appropriate. They do not each require a separate implementation function.

A source-reference map proves that text was attached; it does not prove that generated prose or code implements the text correctly. Preserve original examples and independently checked acceptance expectations alongside that map.

## 3 3 Handle context limits explicitly

Measure the actual rendered packet against the chosen model's available context budget. Reserve space for the response. When the packet does not fit, reduce the number of functions in that pass or split the relevant requirements into planned passes.

Do not silently truncate normative text, substitute Architecture for an unavailable feature section, or continue after dropping exact error messages. An unresolved source selection must produce a specific context-assembly diagnostic.

Each packet should include the current function or component, relevant original requirements, fixed architecture, applicable decisions, and dependency interfaces. Supply direct callable dependencies and the type declarations necessary to understand those interfaces. Avoid expanding every transitive helper into an entry-point packet unless the implementer actually needs it.

## 3 4 Separate facts from owner decisions

Collect facts from the repository and execution environment when accessible: existing module path, relevant package directories, configured Go requirements, and actual host or selected execution target. Preserve the distinction between observed facts and conservative defaults.

Clarification should ask only for unresolved owner decisions that materially change implementation. Remove the contradictory instruction that forbids asking known facts while offering a fact question kind for facts already known.

For this fixture, retain the existing empty clarification result unless new information creates a material decision. Correcting context selection does not require another owner-question round.

# 4 Make package identity and ownership canonical

## 4 1 Define directory and declaration name separately

Use one authoritative package representation throughout the pipeline.

A clear new representation is:

```json
{
  "id": "cli-logic",
  "package_dir": "internal/cli",
  "package_name": "cli",
  "requirement_refs": [
    "feature-make-a-slug",
    "feature-report-errors",
    "architecture-cli"
  ],
  "function_ids": [],
  "integration_tests": []
}
```

These are proposed fields. A smaller compatibility change can retain `package` as the directory field and add or resolve the Go declaration name separately. The invariant matters more than the spelling.

For this fixture, the authoritative mapping is:

| Component | Directory | Go package name |
| - | - | - |
| slug-logic | internal/slug | slug |
| cli-logic | internal/cli | cli |
| entry-point | cmd/slugify | main |

Derive import paths from the module path and package directory. Multiple semantic components may share a package directory. Separate directories remain separate Go packages even if their declarations use the same name. This follows Go's documented package organization and import-path rules. [Go code organization](https://go.dev/doc/code)

## 4 2 Enforce declaration placement

For every ordinary source declaration:

- A function's owner component exists. A type has a canonical home package and an identified declaration authority.

- The file is a normalized repository-relative Go source path.

- The file's immediate parent directory equals the function owner's package directory or the type's canonical home package.

- The package declaration name matches the package record.

- Any function-level package metadata agrees with its owner.

- Absolute paths and paths escaping the repository are rejected.

- A reference across package directories respects Go visibility.

- Required symbols and signatures from the brief remain intact.

A prefix-only containment test is insufficient for package identity: a nested subdirectory is another package. Test files can use the appropriate external test-package convention through a separately defined test-generation rule.

Derive actual Go export visibility from declaration names. The existing `exports` array contains private helper IDs, so its presence must not be treated as proof that a symbol is accessible from another package. Either document its current meaning as a function inventory or migrate it deliberately.

## 4 3 Pin explicit architecture

Extract the brief's mandatory interfaces before open-ended function planning:

```go
// internal/slug
func Make(s string, max int) string

// internal/cli
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int

// cmd/slugify
func main()
```

For this fixture, main must forward the process arguments and streams and exit with the result:

```go
os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
```

The generic local `run` architecture must not override these declarations. The planner may add private helpers in their proper package, but ownership and public signatures stay fixed.

When no shared types exist, allow an empty shared-type list. Make an empty `types` bookkeeping component optional, or create it deterministically if the current storage model requires it. Do not generate an unused package or invent domain types to fill a mandatory slot.

When shared types do exist, assign each a canonical home package. Its shared emitting pass or `types` bookkeeping node does not impose a common directory. Assign the type to a real component in that package, or validate its placement directly against the canonical package record.

# 5 Validate proposed changes before accepting them

## 5 1 Separate the validation boundaries

Use the existing stage machinery with explicit distinctions between the following states:

| Boundary | Required evidence |
| - | - |
| Response received | The model request completed and its raw response was retained |
| Decoded | The response contains a usable value in the expected outer format |
| Candidate valid | The whole candidate satisfies the active stage's schema, scope, identity, and local invariants |
| Draft committed | The candidate was applied to the current planning draft atomically |
| Contract validated | All scheduled contract work is complete and global invariants pass |
| Task packet validated | The packet matches the validated contract and its source requirements |
| Ready for implementation | Current prerequisites and existing run gates permit dispatch |

Use equivalent existing status names where possible. A committed draft can still be incomplete. A completed model request does not imply any of the later states.

## 5 2 Validate the whole candidate

Before adding any of a response to the draft, check:

- The active stage's schema and allowed fields.

- Allowed component and declaration scope.

- Identifier syntax.

- Duplicate IDs.

- Function and type ownership.

- Source-file placement.

- Go declaration syntax.

- Required signatures.

- Dependency identifier syntax and declared pending obligations.

- Required source associations.

- Any consistency rules applicable at that stage.

Parse Go signatures and declarations with Go syntax tooling. If metadata describes `stdout, stderr io.Writer`, it must have one input record for stdout and another for stderr. Reject a signature mismatch rather than silently repairing its name or parameter order.

Treat a component response as one candidate transaction. If a CLI response includes a foreign `fn-main`, reject that candidate with a targeted diagnostic. Do not merge its apparently correct helpers while leaving an invalid portion behind.

Return machine-readable diagnostics with a stable code, stage, record ID, field, expected value, actual value, and applicable source reference. For example:

```json
{
  "code": "FOREIGN_COMPONENT_DECLARATION",
  "stage": "contract:cli-logic",
  "record_id": "fn-main",
  "field": "owner",
  "expected": "entry-point",
  "actual": "cli-logic",
  "source_refs": ["architecture-entry"],
  "message": "This pass cannot declare fn-main; retain the scheduled entry-point pass."
}
```

Define separate codes for conflicting IDs, invalid file placement, fixed-signature changes, missing source associations, unresolved final dependencies, inaccessible references, stale revisions, and no-progress repairs. Use the diagnostics to build a targeted repair request and to assert the intended failure in regression tests.

## 5 3 Define duplicate behavior

| Incoming record | Result |
| - | - |
| New ID with valid content | Add it to the draft |
| Existing ID with identical canonical content | Idempotent replay with no new progress |
| Existing ID with different content | Conflict requiring an explicit repair |

Remove silent first-definition-wins behavior for conflicting declarations. Do not replace it with unrestricted last-definition-wins behavior.

Canonical content comparisons should exclude timestamps, attempt counters, and incidental display metadata. Record identity and semantic contract content must participate.

## 5 4 Permit legitimate incomplete drafts

A function may refer to a declaration scheduled for a later continuation only through an explicit pending declaration commitment. Record its ID, expected kind, expected owner or home package, source requirement, and the boundary by which it must resolve. Do not manufacture a missing function while that planned work is still outstanding.

Add this exception to the component response schema and prompt together. References must resolve to an existing declaration, a declaration in the same candidate, or a valid pending commitment. Returning `more: true` alone does not legitimize an unknown ID. Pending commitments are obligations, not completed declarations, and cannot satisfy the final readiness gate.

For example, `fn-run` may refer to `fn-read-input` while recording that it is a function owned by `cli-logic`, justified by the one-input read requirement, and due by component completion. A later continuation supplies the declaration. The draft can remain locally valid during this interval. Reject the eventual declaration if its kind or ownership conflicts with the commitment.

When a component or the complete contract declares itself finished, enforce the obligations due at that boundary. An arbitrary unknown ID is not an approved declaration simply because it was recorded as pending.

## 5 5 Add the global contract gate

After all contract passes and continuations finish, check:

- Every required component and fixed symbol exists.

- Every active declaration has one authoritative ID and owner.

- Every required dependency resolves.

- File placement and package identity agree.

- Cross-package references are accessible.

- Package imports do not form cycles.

- The graph has an execution ordering supported by the existing scheduler.

- Required entry points and responsibilities match the brief.

- No obsolete workaround component remains active.

- Required source material and behavioral/error contracts are present.

Distinguish package import cycles from legal function recursion and recursive types. If the scheduler cannot handle a plan with cyclic implementation prerequisites, report that scheduling limitation explicitly.

The session 11 arrangement of `Run` in `cli/main.go` and private helpers in `internal/cli` must fail this gate. No builder receives those tasks.

# 6 Change the prompts by stage

Use stage-specific response schemas and examples. A later pass must not be shown an example that encourages it to re-emit fields it is forbidden to change.

| Stage | Required prompt change |
| - | - |
| Clarification | Ask only unresolved owner decisions; supplied facts are read-only context |
| Shared outline | Emit module, conventions, dependencies, and actual shared declarations; handle empty shared types explicitly |
| Feature outline | Emit only the assigned feature components and their allowed declarations |
| Entry outline | Create the one designated entry component; apply the brief's explicit architecture |
| Component contract | Emit only functions/types owned by the requested component; preserve fixed interfaces |
| Dependency repair | Propose explicit changes to edges or declarations with target IDs and a base revision |
| Decomposition | Add semantic task information to fixed canonical records rather than rewriting package, file, or signature |
| Enrichment | Add design guidance consistent with the contract; do not alter behavior, ownership, or dependency identity |
| Test generation | Receive original requirements, decisions, and the contract; keep expected results independent of generated implementation |

For this fixture, schedule entry-point creation exactly once in the dedicated entry pass. Remove the instruction that also creates it in the feature batch. When entry metadata is wrong, invoke the explicit repair operation for that record.

Use these rules in the appropriate prompts:

```text
The explicit Architecture section and supplied fixed interfaces are authoritative.
Use generic architecture defaults only where the brief is silent.

This pass may declare records only for the component and directory shown below.
Do not emit entry-point functions for another component.

Package directories, owning component IDs, and fixed signatures are supplied facts.
If they conflict, report the conflict; do not resolve it by renaming or relocating a function.

Preserve the original behavioral requirements and their ordering.
A short doc comment does not replace the attached requirement records.

An unresolved reference may be an incorrect dependency edge.
Do not assume that every missing ID requires another declaration.

Return only the response shape for this stage.
Do not echo fixed fields or unrelated existing records.
```

Return unknown fields as specific validation errors. If fenced JSON is tolerated for compatibility, unwrap exactly one complete JSON fence, log that normalization, and still validate the resulting value. Do not use permissive extraction to guess which of several objects is authoritative.

# 7 Make repair an explicit transaction

## 7 1 Support the necessary operations

Extend the existing repair mechanism with a bounded operation set:

- Replace an identified component.

- Replace an identified function or type.

- Remove an erroneous dependency.

- Retarget a dependency.

- Add a declaration justified by a requirement.

- Remove an obsolete record after handling its references.

Retain an existing ID when correcting the same conceptual record. Correct `entry-point` instead of inventing `entry-point-main`.

A repair proposal must carry the draft revision it was prepared against, a reason, and explicit targets. A representative operation shape is:

```json
{
  "base_revision": 12,
  "reason": "Use the entry directory required by Architecture.",
  "operations": [
    {
      "op": "replace_component",
      "id": "entry-point",
      "expected_record_hash": "<current record hash>",
      "value": {
        "id": "entry-point",
        "package_dir": "cmd/slugify",
        "package_name": "main",
        "requirement_refs": ["architecture-entry"],
        "function_ids": [],
        "integration_tests": []
      }
    }
  ]
}
```

The revision and hash shown are illustrative guards. Use the current values returned by the actual state store.

## 7 2 Apply changes atomically

For each repair:

1. Check that the base revision and record guards still match.

2. Apply all operations to an in-memory candidate.

3. Validate the changed records and affected relationships.

4. Recompute derived indexes and function inventories, and mark affected downstream artifacts stale in the candidate state.

5. Commit the snapshot, revision, readiness state, and invalidation markers together using the existing crash-safe persistence mechanism.

6. Rebuild derived dependency packets from the committed revision and validate them before reuse.

7. Re-run the readiness checks due at the current planning boundary.

Do not partially commit a repair if another operation fails. A replacement that affects several related fields must update them together. Every dispatch, artifact reuse, and worker-result acceptance must verify the applicable current contract revision or validated input fingerprint. A worker completing against a superseded contract cannot have its output accepted as current. This check closes the interval between a repair and regeneration of its downstream work.

A repair to an incomplete draft may leave legitimate future declarations pending. A repair to a previously ready contract must pass global validation again before dispatch resumes.

## 7 3 Repair unsupported dependencies directly

For the recorded `cfg-options` case, require the repair to assess the reference itself.

If the parser does not need it, remove the edge. If an existing helper supplies the behavior, retarget the edge. Add a new helper only when the retained requirements establish a useful responsibility and its full contract is specified.

If a type-like ID becomes a function ID, update the referring `uses` entry explicitly in the same transaction. Rebuild `depends_on` and dependency packets from the canonical contract rather than maintaining independent hand-edited copies.

# 8 Detect ineffective repairs and expose transformations

A repair succeeds when its intended invariant is satisfied in the stored state. A corrected model reply alone does not establish success.

Compare the relevant canonical state and validation diagnostics before and after the attempt. Exclude timestamps and log counters from this comparison.

If the same defect remains and the relevant state has not changed, record `no_progress`. Do not submit the identical repair context repeatedly. For this workflow, use at most two targeted repair proposals per unchanged failure context, bounded by a finite contract-wide repair budget. A no-progress attempt may receive a different, more specific correction request; a repeated identical attempt stops with a diagnostic.

Record meaningful transformations with the record ID, old value, new value, reason, and resulting revision. Whitespace formatting can be normalization. Renaming `run` to `Run`, moving a function between packages, or adding `entry-point-main` changes the contract and must follow repair validation.

The regression assertion for sessions 4 and 5 is concrete: the next prompt must contain the corrected entry directory, exactly one active entry record, and no lingering version of the original mismatch.

# 9 Materialize tasks from canonical contracts

The task builder should copy fixed structural fields itself:

- Function ID and owning component.

- Package directory and declaration name.

- Source file.

- Exact signature.

- Parameter and result names/types.

- Dependency references and resolved interface records.

- Requirement references and contract revision.

Use parsed signatures for structural metadata. Resolved Go type information can establish whether a type can represent nil; neither syntax nor type resolution establishes whether this API permits nil, what an empty value means, or which units apply. Those are semantic contract requirements and must come from retained source, documented assumptions, or validated design. Do not add a complete type-checking subsystem solely for this metadata; preserve the contract's semantics when resolution is unavailable.

For example, `args []string` can naturally represent an empty argument list as nil. Do not generate blanket non-nil requirements for all slices without a product reason.

Dependency IDs must resolve to canonical contract declarations. Standard-library imports belong in import metadata and must not become invented function IDs such as `fn-io` or `fn-fmt`.

Separate callable dependencies from external side effects. A pure function may call another pure helper and remain pure. Retain function references in `uses` or a clearly defined calls field; reserve `side_effects` for observable effects such as I/O, state changes, and process termination. Update the prompt that currently demands both an empty array for pure functions and a side-effect entry for every function call.

For CLI status-returning functions, preserve explicit failure cases and their codes. A generic `returns: err` description is insufficient where the brief defines exact diagnostics or precedence.

Derive or validate model routing from the task's actual responsibilities. Under the current routing rule, `Run` and the I/O coordinator require `strong`. Resolve that tier through the existing configured model registry. If a required tier is unavailable, report a routing limitation rather than silently lowering the requirement.

# 10 Restrict enrichment to useful consistent guidance

Enrichment consumes a validated contract revision. It may explain an approach, list construction steps, identify relevant risks, and suggest targeted performance checks. It may not alter the signature, owner, file, dependencies, error ordering, accepted input scope, or required outputs.

Construction steps for Make must preserve lowercasing, the ASCII allowlist, separator collapse, edge trimming, truncation, and the trim after truncation. If enrichment contradicts a rule, reject it and retain the valid underlying contract.

Keep the execution target separate from supported-platform notes. Supply `darwin/arm64` as the known execution host for this fixture. Describe the pure slug function as having no identified OS-specific dependency; do not copy `linux/amd64` from a sample object or automatically limit portability to the host.

Make empty-array rules explicit:

- `assumptions` and `decision_ids` may be empty.

- `open_questions` is empty when planning can proceed.

- `error_kinds` is empty when the function has no error cases.

- A group such as `refactor_notes` must contain supported notes or its allowed `not_applicable` representation.

- An application with no logging requirement may have no log events, no metrics, and no trace span.

Do not require benchmarks, latency targets, security narratives, or future refactors where there is no corresponding need. Performance claims should either cite measurements or remain modest design judgments. A simple pure function can have concise enrichment.

# 11 Generate tests against the original requirements

The test planner must receive both the canonical contract and the original behavioral requirements. Retain exact examples from the brief as independent expectations.

Maintain a requirement-to-test map that distinguishes:

- Required behavior.

- Proposed assumptions resolving ambiguous behavior.

- Structural harness invariants.

- Verification policies such as formatting, build, and vet.

Do not count the presence of a requirement ID or the existence of a test name as evidence that behavior passed. Record the actual test execution result separately.

Define executable test targets. Prefer a Go package test and a stable test selector over a free-form pseudo-call. For process checks, build the target into a temporary directory and execute that exact path. Do not depend on a previously installed `slugify` in PATH.

Use `cli.Run` tests for most diagnostic and return-code combinations. Include a small built-binary check to prove that main forwards streams and propagates the process exit code. The Go tool documents that `go run` does not preserve the compiled program's exit status as its own. [Go run documentation](https://pkg.go.dev/cmd/go#hdr-Compile_and_run_Go_program)

Use temporary directories for files and guaranteed-absent child paths for read failures. Compare platform-dependent operating-system errors appropriately rather than fixing Linux wording in a Darwin run.

For validation-before-I/O checks, use a reader that records or fails on an unexpected read. Verify file-open ordering through the program's existing test seam if available. Do not expand the product's public API solely to add a test hook.

# 12 Make resume depend on current inputs

Persist enough input identity to determine whether a completed artifact is still valid:

- Original brief content or a stable content hash.

- Relevant source associations.

- Settled decisions and assumptions.

- Applicable repository facts and fixed architecture.

- Contract revision and schema version.

- Relevant declarations and behavior.

- Prompt and context-selection versions that affect the artifact.

- Verification policy where it changes the required checks.

For the first implementation, use conservative invalidation from the earliest changed planning stage. A change to the canonical contract can invalidate all contract-derived nodes, enrichment, test plans, and unverified builder work. Preserve the original brief, still-valid facts, and settled clarification decisions.

This avoids requiring a new incremental planning engine. More selective reuse can be added later using per-artifact input fingerprints and both the old and new dependency graphs.

For an ordinary interruption with unchanged inputs, resume from the last durable completed checkpoint. Commit an artifact and the state that declares it complete consistently. Replaying the last committed response must be idempotent.

For changed inputs, mark the derived artifacts stale explicitly. Do not reuse a node merely because its signature is unchanged; its behavioral requirements may have changed.

Read legacy records through a version-aware adapter. Migrate package identity only when the brief or verified layout resolves the mapping unambiguously. Otherwise regenerate the affected planning state. Do not infer an arbitrary directory by prepending a legacy package string.

For the failing slugify run, retain the source brief, verified facts, clarification result, and diagnostic logs. After the fixes, regenerate the outline and its downstream artifacts. Existing reviewed or completed labels must not bypass new validation for stale content.

# 13 Define the slugify regression contract

## 13 1 Required function boundaries

| Function | Responsibility |
| - | - |
| Make | Pure string transformation with no printing or files |
| Run | Argument validation, one whole input, transformation, output, and returned status |
| main | Process argument and stream forwarding, then exit |

Optional private parser and reader helpers may be used. Their existence must follow a useful responsibility. Do not assert a fixed helper count in the regression tests.

## 13 2 Preserve the original behavior

Make must produce lowercase ASCII slugs with one hyphen for each run of disallowed bytes, strip edge hyphens, apply a positive maximum, and strip a trailing hyphen after truncation. Zero or a negative maximum supplied directly to Make means unlimited output.

Run must read one whole input from the selected file or stdin, ignore one terminal line feed as specified, call Make once for that input, and write one final newline. An empty slug is successful.

Usage errors must precede numeric-value errors, which must precede opening or reading input. The CLI rejects a maximum below one even though the pure Make API treats a nonpositive maximum as unlimited.

## 13 3 Record proposed defaults for underspecified edges

The following decisions make the regression fixture deterministic under `assume_and_document`. They are proposed clarifications beyond the unambiguous original examples and should be stored as assumptions.

| ID | Proposed default |
| - | - |
| A1 | Apply `strings.ToLower` before the ASCII byte filter. This is Unicode lowercasing followed by ASCII output filtering, with no transliteration step. |
| A2 | Scan the entire argument structure before converting any maximum. Validate every supplied maximum in occurrence order. The first invalid value fails; when all are valid, the last value controls the limit. |
| A3 | `--max` consumes its immediately following token as its value, even if that token starts with a hyphen. Only absence of that token is a missing-value usage error. |
| A4 | Permit recognized options before or after the single FILE. A standalone `--` is an unknown flag because the brief defines no end-of-options escape. |
| A5 | An input-reader failure or output-writer failure returns code 1 and attempts the standard `slugify: <error>` diagnostic. Failure to write stderr preserves the intended status without recursive error reporting. |
| A6 | Diagnostics end with one newline. Invalid values are escaped as Go quoted strings so quotes and control bytes do not create extra diagnostic lines. |
| A7 | Nil `args` is an empty argument list. Streams must be usable when selected; no extra nil-stream recovery errors are introduced. |
| A8 | An explicitly supplied empty FILE argument is an empty filename. It does not mean missing FILE and must not select stdin. |

`strings.ToLower` uses Unicode lowercasing. `strconv.Atoi` supplies the required integer conversion behavior. Keep those operations explicit rather than adding a new numeric grammar or a broad Unicode-alphanumeric allowlist. [strings documentation](https://pkg.go.dev/strings#ToLower) [strconv documentation](https://pkg.go.dev/strconv#Atoi)

Under A3, `--max --unknown` is an invalid maximum, while `--max abc --unknown` is a usage error because the final token is an unconsumed unknown flag. Under A2, `--max bad --max 5` reports the earlier invalid value.

A1 fixes the order for Unicode characters that lowercase into ASCII. It does not allow accented letters to remain in the output. Add a focused Unicode-mapping test for the chosen operation; the original accented example must still pass.

## 13 4 Product regression cases

In the table, quoted outputs use `\n` for a line feed. Pure Make tests omit the CLI newline.

| Case | Expected result |
| - | - |
| Make of `Hello, World!` | `hello-world` |
| Make of `  --Go & Rust  ` | `go-rust` |
| Make of `Crème Brûlée` | `cr-me-br-l-e` |
| Make of `A \t\n!! B` | `a-b` |
| Make of `hello world` with max 6 | `hello` |
| Make with max 0 or negative | Unlimited transformation |
| CLI `Hello, World!` with max 99 | `hello-world\n` and code 0 |
| CLI input `a\nb\n` | Exactly `a-b\n` |
| Empty or all-separator input | Exactly `\n` and code 0 |
| Missing FILE or FILE equal to `-` | Read supplied stdin |
| File in a temporary directory | Read that file and leave supplied stdin unused |
| `--max 2 --max 5` with `abcdef` | `abcde\n` |
| `--max +3` with `abcdef` | `abc\n` |
| `--max 0` | `slugify: invalid --max "0"\n` and code 2 |
| `--max abc` | `slugify: invalid --max "abc"\n` and code 2 |
| `--max -1` | Invalid-max diagnostic and code 2 |
| `--max 999999999999999999999999999999999999` | Invalid-max diagnostic and code 2 |
| Missing value, `--max=3`, `--unknown`, or two FILEs | Exact usage line and code 2 |
| `--max abc --unknown` | Usage error wins |
| `--max 0 file-a file-b` | Usage error wins |
| `--max 0 missing-file` | Invalid maximum wins before input access |
| Missing file with valid arguments | Read-error diagnostic and code 1 |
| Explicit empty FILE argument | Read error and no stdin read under A8 |
| `--max bad --max 5` | Earlier invalid value fails under A2 |
| `--max --unknown` | Invalid maximum under A3 |
| Failing stdin reader or stdout writer | Code 1 under A5 |
| Failing stderr during a usage error | Preserve code 2 under A5 |
| Built binary receiving invalid arguments | Actual process exit code matches Run |

The exact usage line is `usage: slugify [--max N] [FILE]\n`. Successful executions leave stderr empty. Validation and read failures leave stdout empty. A writer failure may already have emitted partial output; do not promise atomic output.

Removing exactly one terminal line feed is often indistinguishable in the final slug from retaining it or removing several because final separators are stripped. Preserve the requirement without creating an artificial helper API solely to inspect that intermediate operation.

# 14 Add focused harness regressions

Store the relevant recorded replies as fixtures. Where a new schema differs from the trace, retain the original fixture and adapt it through the legacy reader or construct its equivalent canonical state. Assert the intended failure boundary rather than merely detecting an unrelated format mismatch.

| Test | Required assertion |
| - | - |
| Full source retention | Slug and CLI packets retain their actual feature rules and examples |
| Component source overlap | CLI receives both invocation/input rules and Report errors |
| Foreign function ownership | A CLI reply containing command main is rejected atomically |
| Conflicting duplicate ID | A changed existing record cannot silently retain or overwrite the old definition |
| Identical replay | Reapplication changes neither content nor progress |
| Existing entry replacement | Correcting entry-point persists into the next prompt with one active record |
| Split directory layout | Run in cli and private helpers in internal/cli block readiness |
| Shared package across components | Private-helper access is permitted when both components share the same Go package |
| Continuation reference | A declaration arriving in a planned later continuation resolves without premature repair |
| Final missing reference | An unresolved dependency after completion blocks dispatch |
| Unsupported dependency removal | cfg-options can be removed without creating another parser |
| Dependency retarget | The canonical edge and derived packets agree after repair |
| No-progress repair | Unchanged state and diagnostic cannot produce an unlimited retry loop |
| Grouped Go parameters | stdout and stderr each appear once in task input metadata |
| Fixed signature drift | A proposed rename or parameter change is rejected |
| Pure helper call | A pure-to-pure function call does not create a false external side effect |
| Required routing | I/O orchestration satisfies the configured strong-tier rule |
| Enrichment contradiction | Truncation without required final trim cannot become accepted design guidance |
| Metadata defaults | Known host facts are not replaced by sample Linux/amd64 values |
| Stale behavioral contract | Changed requirements invalidate derived work even with an unchanged signature |
| Interrupted persistence | Resume sees the last complete snapshot and safely replays work |
| Schema compatibility | Legacy ambiguous package records become stale instead of being guessed into a layout |

For semantic enrichment checks, combine deterministic requirement preservation with targeted fixture tests or review. General prose equivalence cannot be guaranteed by checking for keywords alone.

# 15 Implement in ordered milestones

## Milestone 1 Preserve source and fixed architecture

Fix context selection, establish the package mapping, and remove the conflicting entry-point instructions. Capture the failing responses as fixtures.

Exit criteria: generated planning packets contain the complete required source and the three mandatory interfaces. No component receives an incompatible default architecture.

## Milestone 2 Enforce candidate and global validation

Add stage-specific schemas, atomic candidate acceptance, ownership checks, duplicate handling, pending-reference tracking, and the global readiness gate.

Exit criteria: the foreign-main and split-directory cases fail at their intended boundaries; legitimate shared-package and continuation cases pass; no implementation dispatch occurs from an invalid final contract.

## Milestone 3 Repair records and recover correctly

Implement explicit repair operations, revision checks, no-progress detection, transformation records, and conservative stale-work invalidation.

Exit criteria: the existing entry record can be corrected without adding another entry component; replay and interrupted commits are safe; resumed work cannot use the old invalid contract.

## Milestone 4 Produce trustworthy tasks and tests

Make structural task metadata deterministic, enforce routing, narrow enrichment, and give the test planner original requirements and explicit assumptions.

Exit criteria: task packets agree with the validated contract, exact CLI errors remain visible, and the independent slugify cases identify the missing-final-trim and precedence defects.

## Milestone 5 Rerun and evaluate

Regenerate the slugify run from the corrected source and outline. Build the resulting program, run vet, run tests, verify formatting, and execute the required binary examples.

Record first-pass candidate validity, global contract validity, repair attempts, no-progress attempts, acceptance coverage, and stage durations. Label token accounting according to the provider's actual fields. Keep model-call completion separate from validation and build success.

Fix deterministic failures before evaluating model changes. A model comparison should use the same complete source, schemas, validators, acceptance suite, and retry budget.

# 16 Definition of done

The work is complete when:

- The original failed transitions have focused regression coverage.

- Original requirements reach the appropriate function packets unchanged in meaning.

- Package identity and fixed interfaces remain consistent across every stage.

- Invalid candidates cannot partially contaminate shared planning state.

- Legitimate incomplete drafts can finish without premature repair.

- The complete contract must validate before any implementation task is dispatched.

- Repairs modify the intended records, resolve their diagnostics, and invalidate stale derived work.

- Repeated ineffective repairs terminate with useful diagnostics.

- Enrichment preserves required behavior and produces consistent metadata.

- Tests receive original requirements and execute against a known target.

- Resume honors current source and contract versions.

- The generated slugify binary satisfies the original acceptance requirements and the separately documented fixture assumptions.

Use the existing repository verification commands for the harness changes. For the generated slugify module, require `go build ./...`, `go vet ./...`, `go test ./...`, clean formatting, and direct execution of the specified CLI cases. Record actual results at implementation time.

# Evidence references

The primary evidence is the supplied 13-session trace for `slugify-v1-20261105-r001`:

- Sessions 3 through 6 demonstrate incorrect directory records and ineffective entry replacement.

- Sessions 6 through 8 and 10 through 13 demonstrate missing feature text in downstream context.

- Sessions 7 through 12 demonstrate ownership drift, duplicate IDs, intermediary transformations, and dependency repair.

- Sessions 11 and 13 demonstrate incomplete error contracts, incorrect routing, and inconsistent enrichment.

Technical references used for package and standard-library semantics:

- [Go code organization](https://go.dev/doc/code)

- [Go exported identifiers](https://go.dev/ref/spec#Exported_identifiers)

- [strings ToLower](https://pkg.go.dev/strings#ToLower)

- [strconv Atoi](https://pkg.go.dev/strconv#Atoi)

- [Go run command](https://pkg.go.dev/cmd/go#hdr-Compile_and_run_Go_program)
