# Manifest

| Path | What it is |
| --- | --- |
| README.md | Start here. Purpose, reading order, non-negotiables, definition of done. |
| SPEC.md | The design document (exported from the AI Venture Studio project doc, v2, 2026-09-29). |
| BUILD_PLAN.md | Ordered deliverables 1 to 15, each with mechanics and a test. Supersedes SPEC.md where they differ. |
| MANIFEST.md | This file. |
| schema/task-node.schema.json | JSON Schema for every node in the tree. Embed in the binary. |
| schema/brief-frontmatter.schema.json | JSON Schema for the brief's YAML frontmatter, including the `env` block. Embed. |
| schema/contract.schema.json | JSON Schema for the Wave 0 contracts.json artifact. Embed. |
| examples/brief.md | Complete example brief (Acme Registration API). Used by build-plan tests. |
| examples/gophermind.yaml | Complete harness config with providers, chains, rate limits, proxy, vault, human mode. |
| examples/tree/gm-2026-09-29-001/contracts.json | Wave 0 contract for the example brief. Validates against contract.schema.json. |
| examples/tree/gm-2026-09-29-001/root.json | Root node with acceptance tests. |
| examples/tree/gm-2026-09-29-001/types/ | Shared-types component and one function leaf. |
| examples/tree/gm-2026-09-29-001/registration/ | Registration component and three function leaves across waves 1 and 2. |
| examples/runtime/fn-validate-email.runtime.json | What one blackboard row looks like after a run, including a rate-limit error attempt. |
| interfaces/blackboard.go | The Blackboard interface, status transitions, and typed errors. Implement exactly. |
| interfaces/provider.go | The Provider interface and the typed errors the router switches on. Implement exactly. |
| prompts/README.md | Which template is called where, and the rules around every call. |
| prompts/01-clarify.md to 06-revise.md | The six prompt templates. |

All example JSON has been validated against the schemas in this zip.
