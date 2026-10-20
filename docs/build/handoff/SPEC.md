# GopherMind Brief Spec v2

This is the design document. `BUILD_PLAN.md` holds the ordered work list and implementation mechanics; `examples/` holds a complete brief and tree; `schema/`, `interfaces/`, and `prompts/` are the machine-facing parts. Where this file and `BUILD_PLAN.md` differ, `BUILD_PLAN.md` is more recent.

## Purpose

A GopherMind brief is a Markdown file describing one whole product. The harness reads it, collects secrets into its vault, then decomposes it into a tree of small JSON task documents, one per function, each with defined inputs, outputs, and tests. Every leaf must be solvable by a small model with a small context.

| Layer | Format | Written by | Holds |
| --- | --- | --- | --- |
| Brief | `.md` with YAML frontmatter | A human or a brief generator | Product intent, features, constraints, product-level acceptance |
| Task node | `.json`, validated by JSON Schema | The harness decomposer | One unit of work with a contract and tests |

Design rules behind v2:

- **Small context beats speed.** Run length does not matter. Every leaf fits the smallest model the harness targets.
- **Contracts before code.** Parent nodes fix interfaces before any leaf is implemented, so a leaf never reads a sibling's body.
- **Verifiable, not aspirational.** Every leaf carries runnable tests. Every parent carries integration tests.
- **No secrets in files.** The brief names secrets; values live only in the harness vault.
- **Portable.** Every tool the harness uses is Go, compiled into the harness binary.

## Brief format

The brief is one `.md` file: YAML frontmatter for machine-read settings, then fixed H2 sections the decomposer maps to tree nodes.

### Frontmatter

```markdown
---
spec_version: "2.0"
id: gm-2026-09-29-001
title: Acme Registration API
language: go
repo: ~/src/acme-api
base_branch: main
landing: commit            # diff_only | commit | pull_request
on_ambiguity: assume_and_document   # halt | assume_and_document
secrets:                   # names only, never values
  - name: CRM_API_KEY
    purpose: Push new users to the CRM
env:                       # non-secret config the product or its tests read
  - name: CRM_BASE_URL
    purpose: CRM API base URL
    default: https://api.crm.example.com
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: api.crm.example.com
    purpose: CRM sync
    critical: false
---
```

| Field | Required | Meaning |
| --- | --- | --- |
| `spec_version` | Yes | Always `"2.0"`. |
| `id` | Yes | Unique brief ID. Becomes the tree root ID and work branch name. |
| `title` | Yes | Product name. |
| `language` | Yes | Target language of the product being built. |
| `repo`, `base_branch`, `landing` | Yes | Where work happens and how it lands. |
| `on_ambiguity` | Yes | `halt` stops and asks. `assume_and_document` picks the conservative option and logs it on the node. |
| `secrets` | No | Secret names and purposes. The harness prompts for each value on load. Values reach commands as env vars from the vault only. |
| `env` | No | Non-secret environment variables with `name`, `purpose`, and optional `default`. Passed through to command execution with their defaults unless overridden in harness config. A name may not appear in both `secrets` and `env`; the loader rejects the brief. Declared `secrets` and `env` names are excluded from the secret-name scan. |
| `network` | No | Hosts the product or build needs. `critical: true` means a failed request fails the task; `false` means warn and continue. All traffic goes through the harness proxy. |

### Required sections

| Section | Maps to | Must contain |
| --- | --- | --- |
| `## Overview` | Root node | What the product does and for whom, in a paragraph. |
| `## Features` | One component node per `### Feature` | Behavior plus acceptance criteria as bullet points. |
| `## Architecture` | Root node context | Packages, data flow, patterns to follow, anything fixed in advance. |
| `## Data` | Shared types node | Entities, fields, and types. |
| `## Constraints` | Inherited by every node | Style, dependency, and compatibility rules. |
| `## Out of scope` | Root node | Work the harness must not do. |
| `## Acceptance` | Root node tests | Product-level commands that prove the whole thing works. |

## Task tree

The decomposer turns the brief into three node kinds: one `root`, one `component` per feature or package, and one `function` leaf per function. Nodes are stored one file each, mirroring the tree:

```text
.gophermind/gm-2026-09-29-001/
  root.json
  contracts.json
  types/
    component.json
    fn-validation-error-error.json
  registration/
    component.json
    fn-validate-email.json
    fn-validate-username.json
    fn-register-handler.json
    ...
```

A run moves through these stages. A later stage never edits an earlier stage's contracts without routing back through the planner.

1. **Load.** Validate the brief's frontmatter. Prompt for every declared secret and store it in the vault.
2. **Clarify.** The planner asks all its questions up front, before any decomposition.
3. **Approve.** The plan and tree skeleton are shown for human approval. Nothing executes before this gate.
4. **Wave 0: contracts.** Runs alone on the strongest model. Produces shared types, component interfaces, function signatures, file layout, and naming conventions. Every later node conforms to it.
5. **Decompose.** Split components into function leaves, write each leaf's tests from its contract, and assign waves from `depends_on`. The planner may pause and ask the human here if a new ambiguity surfaces.
6. **Execute.** Waves run in sequence; leaves within a wave run in parallel. Each leaf is claimed atomically on the blackboard and tried against the model fallback chain one model at a time until its tests pass.
7. **Revise.** If every model fails a leaf, its full failure history goes to the planner, which rewrites the definition. Capped at 2 revision rounds, then escalates to a human checkpoint.
8. **Roll up.** Component integration tests, then root acceptance tests. Optional milestone approvals between waves.
9. **Report.** A per-model performance summary derived from the attempt log: pass rate, first-try vs fallback wins, average duration, and failure patterns.

## Task node schema

JSON Schema (draft 2020-12). Fields fall into two groups:

- **Definition** fields are written by the planner and change only through a revision round. They live in the node file in the tree.
- **Runtime** fields (`status`, `claim`, `attempts`, `result`) change constantly during a run. Proposed: store these on the blackboard keyed by node `id`, not in the file, so claims stay atomic without file locking. The schema covers both so the merged view validates.

`function` nodes must carry a `contract` and at least one test with a command.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://gophermind.local/schema/task-node/2.0",
  "type": "object",
  "additionalProperties": false,
  "required": ["spec_version", "id", "kind", "title", "description", "brief_ref", "status"],
  "properties": {
    "spec_version": {"const": "2.0"},
    "id": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]*$"},
    "kind": {"enum": ["root", "component", "function"]},
    "parent": {"type": "string"},
    "children": {"type": "array", "items": {"type": "string"}},
    "title": {"type": "string", "maxLength": 120},
    "description": {"type": "string"},
    "brief_ref": {"type": "string", "description": "Heading anchor in the brief this node came from"},
    "wave": {"type": "integer", "minimum": 0, "description": "0 = contracts; assigned from depends_on"},
    "model_tier": {"enum": ["strong", "standard", "any"], "description": "Contracts and shared interfaces use strong"},
    "revision": {"type": "integer", "minimum": 0, "description": "Definition revision round, 0 = original"},
    "depends_on": {"type": "array", "items": {"type": "string"}},
    "context": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "dependency_signatures": {"type": "array", "items": {"type": "string"}},
        "files_read": {"type": "array", "items": {"type": "string"}},
        "constraints": {"type": "array", "items": {"type": "string"}},
        "notes": {"type": "array", "items": {"type": "string"}}
      }
    },
    "contract": {
      "type": "object",
      "additionalProperties": false,
      "required": ["package", "file", "signature", "inputs", "outputs"],
      "properties": {
        "package": {"type": "string"},
        "file": {"type": "string"},
        "signature": {"type": "string"},
        "inputs": {"type": "array", "items": {"$ref": "#/$defs/param"}},
        "outputs": {"type": "array", "items": {"$ref": "#/$defs/param"}},
        "errors": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["when", "returns"],
            "properties": {"when": {"type": "string"}, "returns": {"type": "string"}}
          }
        },
        "side_effects": {"type": "array", "items": {"type": "string"}}
      }
    },
    "tests": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "level", "given", "expect"],
        "properties": {
          "name": {"type": "string"},
          "level": {"enum": ["unit", "integration", "acceptance"]},
          "given": {"type": "string"},
          "expect": {"type": "string"},
          "command": {"type": "string"}
        }
      }
    },
    "network": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["host", "critical"],
        "properties": {"host": {"type": "string"}, "critical": {"type": "boolean"}}
      }
    },
    "secrets": {"type": "array", "items": {"type": "string"}, "description": "Secret names only"},
    "budget": {
      "type": "object",
      "properties": {
        "max_context_tokens": {"type": "integer", "minimum": 1},
        "max_revisions": {"type": "integer", "minimum": 0, "default": 2}
      }
    },
    "assumptions": {"type": "array", "items": {"type": "string"}},

    "status": {"enum": ["pending", "ready", "claimed", "in_progress", "verified", "failed", "needs_revision", "escalated"]},
    "claim": {
      "type": "object",
      "properties": {"worker": {"type": "string"}, "claimed_at": {"type": "string", "format": "date-time"}}
    },
    "attempts": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["model", "provider", "revision", "verdict"],
        "properties": {
          "model": {"type": "string"},
          "provider": {"type": "string"},
          "revision": {"type": "integer"},
          "order": {"type": "integer", "description": "Position in the fallback chain, 1 = first try"},
          "started_at": {"type": "string", "format": "date-time"},
          "duration_ms": {"type": "integer"},
          "verdict": {"enum": ["pass", "fail", "error"]},
          "tests_passed": {"type": "integer"},
          "tests_total": {"type": "integer"},
          "failed_tests": {"type": "array", "items": {"type": "string"}},
          "failure_reason": {"type": "string"}
        }
      }
    },
    "result": {
      "type": "object",
      "properties": {
        "files_changed": {"type": "array", "items": {"type": "string"}},
        "commit": {"type": "string"}
      }
    }
  },
  "allOf": [
    {
      "if": {"properties": {"kind": {"const": "function"}}},
      "then": {
        "required": ["parent", "contract", "tests", "wave"],
        "properties": {
          "tests": {"minItems": 1, "contains": {"required": ["command"]}}
        }
      }
    },
    {
      "if": {"properties": {"kind": {"const": "root"}}},
      "else": {"required": ["parent"]}
    }
  ],
  "$defs": {
    "param": {
      "type": "object",
      "required": ["name", "type"],
      "properties": {
        "name": {"type": "string"},
        "type": {"type": "string"},
        "description": {"type": "string"},
        "constraints": {"type": "array", "items": {"type": "string"}}
      }
    }
  }
}
```

## Example leaf

One `function` node the decomposer would produce from the registration brief above. Everything a small model needs is in this file; it never opens `register.go` or a sibling's body.

```json
{
  "spec_version": "2.0",
  "id": "fn-validate-email",
  "kind": "function",
  "parent": "registration",
  "title": "Validate an email address",
  "description": "Reject malformed emails before they reach the CRM sync.",
  "brief_ref": "#features/registration",
  "status": "ready",
  "wave": 1,
  "model_tier": "any",
  "revision": 0,
  "depends_on": ["fn-validation-error-error"],
  "context": {
    "dependency_signatures": ["type ValidationError struct { Code string; Field string; Msg string }"],
    "constraints": ["Standard library only", "Return ValidationError, never panic"]
  },
  "contract": {
    "package": "validation",
    "file": "src/validation/email.go",
    "signature": "func ValidateEmail(email string) *ValidationError",
    "inputs": [
      {"name": "email", "type": "string", "constraints": ["May be empty", "May contain surrounding whitespace"]}
    ],
    "outputs": [
      {"name": "err", "type": "*ValidationError", "description": "nil when valid"}
    ],
    "errors": [
      {"when": "email is empty after trimming", "returns": "Code EMAIL_REQUIRED"},
      {"when": "email fails net/mail.ParseAddress or has no dot in the domain", "returns": "Code EMAIL_INVALID"},
      {"when": "email longer than 254 chars", "returns": "Code EMAIL_TOO_LONG"}
    ],
    "side_effects": []
  },
  "tests": [
    {"name": "accepts plain address", "level": "unit", "given": "a@b.co", "expect": "nil", "command": "go test ./src/validation -run TestValidateEmail"},
    {"name": "rejects empty", "level": "unit", "given": "\"  \"", "expect": "EMAIL_REQUIRED"},
    {"name": "rejects missing domain dot", "level": "unit", "given": "a@localhost", "expect": "EMAIL_INVALID"},
    {"name": "rejects 255 chars", "level": "unit", "given": "255-char address", "expect": "EMAIL_TOO_LONG"}
  ],
  "budget": {"max_context_tokens": 4000, "max_revisions": 2}
}
```

## Harness requirements

Every capability the brief relies on is Go, compiled into the harness. Library picks are proposals, not decisions.

| Capability | Proposed Go implementation | Notes |
| --- | --- | --- |
| Blackboard | Reuse the store from the earlier recursive agent system | Atomic claim semantics so two workers never take the same leaf. Holds runtime fields: status, claim, attempts. |
| Model router | Harness code, fallback chain per `model_tier` | Tries one model at a time, strongest first for `strong` nodes. Later: reorder the chain adaptively from the attempt log. |
| Git | go-git (`github.com/go-git/go-git/v5`) | Branch, commit, diff, push are solid. Merge support is limited, so the harness should rebase leaves onto a single work branch rather than merge. PRs need a forge API call (GitHub, Gitea) through the proxy. |
| Network proxy | `net/http` forward proxy inside the harness | Allowlist from brief `network`. Logs every request per node. Critical failure fails the node; non-critical logs a warning. LLM provider calls go through it too. |
| Secrets vault | Encrypted file keyed by a passphrase (e.g. `filippo.io/age` or NaCl secretbox) | Prompted for on brief load. Injected as env vars into commands only. Values never enter model context or node files. |
| Schema validation | `github.com/santhosh-tekuri/jsonschema` | Validate every node on write. An invalid node is a decomposer bug, not a runtime error. |
| Test runner | `go test -json` executed by the harness | Parsed into the attempt's `tests_passed`, `failed_tests`, and `failure_reason`. |
| Context packer | Harness code | Builds each leaf prompt from the node file only and enforces `max_context_tokens`. |
| Run report | Aggregation over `attempts` | Per model: pass rate, first-try vs fallback wins, average duration, failure patterns. No extra fields needed. |
| Live view | Wave board plus attempt log (mockup exists in the harness chat) | Reads the blackboard. |

## Rules for the decomposer

1. One function per leaf. Exception: methods of one small type may share a leaf if the leaf still fits its budget.
2. A leaf is complete without reading any other node's implementation. Dependencies appear only as signatures in `context.dependency_signatures`.
3. Parents own interfaces. A leaf may not change its own `signature`. A worker that finds the contract wrong flags it to the planner; it never patches around it.
4. Every leaf has at least one test with a runnable `command`. Tests are written before implementation.
5. `depends_on` is acyclic and references only node IDs in the same tree. `wave` is derived from it, never set by hand.
6. Contract and shared-interface nodes get `model_tier: strong`. Independent leaves get `standard` or `any`.
7. No node file ever contains a secret value. Secret values reach commands only from the vault; a name in `secrets` never gets a default and is never sourced from `env` or harness config.
8. Brief `## Constraints` are copied into every node's `context.constraints`, trimmed to what applies.
9. An unknown fact follows `on_ambiguity`. Assumptions go in the node's `assumptions` array, never silently into code.
10. A revision receives the node's full `attempts` history, not just a failure flag. Failures converging on one test suggest the test is stricter than the contract; scattered failures suggest the definition is underspecified.

## Decisions

| Question | Decision |
| --- | --- |
| Shell access | Full harness. New tools are written in Go and compiled in. |
| Git and landing | Go-native git library, not the CLI. |
| Network | Allowed. Brief marks each host critical or not; all traffic goes through the harness proxy. |
| Deliverable consumer | The harness. Brief is `.md`; the harness decomposes it into JSON task nodes validated by schema. One task per function, each with inputs, outputs, and tests. |
| Chaining | No brief chaining. One brief produces one tree of task documents. |
| Secrets | Never in the brief. Harness prompts on load and stores them in its vault. |
| Gates | All questions asked before work starts; human approves the plan; planner may pause and ask during decomposition. |
| Parallelism | Wave-based. Wave 0 contracts run first on the strongest model; leaves within a wave run in parallel. |
| Model use per leaf | Sequential fallback through free providers until tests pass, not parallel fan-out. |
| Exhausted fallback | Full failure history to the planner for revision, max 2 rounds, then human checkpoint. |
| Coordination | Reuse the blackboard with atomic claims from the earlier recursive agent system. |
| Run report | Per-model performance summary derived from the attempt log. |

## Recommendations

These resolve the remaining open questions. Build against them unless JB overrides.

| Question | Recommendation | Why |
| --- | --- | --- |
| Target language | Go only in v2. Keep the `language` field; the loader rejects anything but `go`. | One signature format, one test runner, one linter. Other languages are a v3 concern. |
| Who writes tests | A separate test-writer pass during Decompose, on a `strong` model, from the contract alone. The implementer never edits `*_test.go`; those paths are forbidden for it. If it believes a test is wrong, it flags the planner. | Implementer-written tests tend to match the implementer's bugs. Separation makes the test an independent check. |
| Smallest model | Design for 8k-context free models. Leaf prompt target under 4k tokens, hard cap 8k. A leaf over budget goes back to the planner to be split, never truncated. | Truncated context is the main way small models fail silently. |
| Secret names | Declare in frontmatter. The loader also scans the brief body and warns on undeclared tokens that look like secrets: `UPPER_SNAKE_CASE` ending in `_KEY`, `_SECRET`, `_TOKEN`, `_URL`, `_DSN`, `_PASSWORD`, or `_PASSPHRASE`, or any `UPPER_SNAKE_CASE` token on a line containing "secret", "credential", or "environment variable". Declared `secrets` and `env` names are exempt. It never auto-adds. Non-secret config goes in `env`. | Deterministic, with a safety net for the one you forgot, and quiet enough on a real brief that the net is visible. |
| Runtime state | Definitions in the file tree, runtime state on the blackboard keyed by node `id`. On run end the harness writes a merged `runtime.json` next to each node for archival. If the existing blackboard is not Go or not durable, implement it behind a `Blackboard` interface with a SQLite backend (`modernc.org/sqlite`, pure Go, no cgo). | Atomic claims without file locks; the tree stays a clean record of intent. |
| Fallback chain | Static ordered list per tier in `gophermind.yaml` for v2. Every attempt is logged. Adaptive reordering from run reports is v2.1, once there is data. | No data yet to learn from. |
| Contract change mid-run | Pause only dependents. Increment the contract node's `revision`, mark every transitive dependent `needs_revision`, refresh their `dependency_signatures`, and let unaffected leaves in the wave finish. | Pausing the whole wave wastes work that was never wrong. |

## Build plan

For the Claude Code instance working in the GopherMind repo. Each item is a deliverable plus the test that proves it is done, in build order. Entry point: `gophermind run brief.md`.

1. **Brief loader and vault.** Parse frontmatter, validate against the Brief format section, reject `language != go`, prompt for each declared secret, store in an encrypted vault, expose as env vars to command execution only. Test: the example brief loads; a brief missing `spec_version` is rejected with a clear message; after a run, grep of the tree and logs finds no secret value.
2. **Node schema and tree store.** Embed the JSON Schema, validate every node on write, store nodes at `.gophermind/<brief-id>/<component>/<node>.json`. Test: the example leaf validates; a `function` node with no `command` test is rejected; a node with `depends_on` forming a cycle is rejected.
3. **Planner.** Clarify pass that asks all questions and blocks until answered; plan and skeleton shown for approval; Wave 0 contract generation on a `strong` model; component and function decomposition; test-writer pass; wave assignment derived from `depends_on`. Test: from the example brief, every `function` node has a contract, at least one command test, and a wave; no leaf's `context` references a sibling body; nothing executes before approval is recorded.
4. **Blackboard and executor.** `Blackboard` interface with atomic claim; wave scheduler; per-leaf model fallback from `gophermind.yaml`; attempt logging; revision loop capped by `max_revisions`; escalation state. Test: two workers racing for one leaf yields exactly one claim; a model failing tests triggers the next model in the chain; exhausting the chain moves the node to `needs_revision` with the full attempt history; a third exhaustion moves it to `escalated` and halts that branch.
5. **Network proxy.** Forward proxy with allowlist from the brief, per-node request log, critical vs non-critical handling. All model provider calls routed through it. Test: a request to an unlisted host is refused and logged; a failed critical request fails the node; a failed non-critical request logs a warning and the node continues.
6. **Git landing.** go-git integration: work branch `gm/<brief-id>`, one commit per verified leaf, rebase not merge, `result.commit` recorded. Test: a verified leaf produces exactly one commit on the work branch containing only its `files_changed`.
7. **Roll-up and run report.** Component integration tests, root acceptance tests, per-model report aggregated from `attempts`. Test: report totals per model match a manual count over the attempt log; a failing acceptance test leaves the root in `failed`, not `verified`.
8. **Live view.** Wave board plus attempt log reading from the blackboard, per the mockup in the harness chat. Last, and optional for the first working run. Test: the view reflects a status change on the blackboard within one refresh interval.

```yaml
# gophermind.yaml
models:
  strong:   ["provider-a/best-model", "provider-b/best-model"]
  standard: ["provider-c/mid-model", "provider-a/best-model"]
  any:      ["provider-d/small-model", "provider-c/mid-model", "provider-a/best-model"]
defaults:
  max_context_tokens: 8000
  max_revisions: 2
proxy:
  listen: 127.0.0.1:8480
```
