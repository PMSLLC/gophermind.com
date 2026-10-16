# Prompt templates

Six templates, one per model call the harness makes. `{{name}}` is a Go text/template field.
Every template ends with an explicit output format because free models drift without one.

| File | Called by | Tier | Output |
| --- | --- | --- | --- |
| 01-clarify.md | Planner, stage 2 | strong | JSON array of questions |
| 02-contract.md | Planner, stage 4 (Wave 0) | strong | contracts.json |
| 03-decompose.md | Planner, stage 5, once per component | strong | array of function nodes without tests |
| 04-testwriter.md | Planner, stage 5, once per function node | strong | tests array plus the Go test file |
| 05-implement.md | Executor, per attempt | node's tier | one Go source file |
| 06-revise.md | Planner, stage 7 | strong | rewritten node definition |

Rules the harness enforces around every call:

- The harness strips markdown fences and leading prose before parsing. A response that still does not parse is logged as `VerdictError` with `failure_reason: malformed:` and the next model is tried.
- The harness counts tokens before sending. If the packed prompt exceeds `budget.max_context_tokens`, the call is not made and the node goes to `needs_revision` with `context_too_long`.
- Secret values are never interpolated. Only secret names appear, and only where the brief listed them.
- Temperature 0 for 05-implement; 0.2 for the rest.
