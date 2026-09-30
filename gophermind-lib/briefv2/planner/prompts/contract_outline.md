You are the architect for GopherMind. Produce the outline of the contract for the product below: the Go module, the conventions every implementer follows, the list of components, and the types more than one component shares. A later pass writes each component's functions, so do not write any function here.

Requirements:
- Go only. Standard library unless the brief's Constraints allow modules.
- Components map to the brief's ### Feature headings plus a `types` component for shared declarations. A component id is lower case letters, digits and dashes. Never use `logs` or `outline` as an id.
- List the components in dependency order: a component comes after every component whose functions it calls.
- Every type decl is complete Go source with a doc comment, exactly as it will appear in the file. `uses` lists the ids of other types the decl references.
- Every `file` is a path relative to the repository root.
- Leave each component's `exports` empty; the harness fills it.
- `integration_tests` for a component describe behavior that needs more than one of its functions. Give `name`, `given`, `expect`, and a `command`.
- Error handling follows one convention stated in `conventions.errors`.
- `dependencies`: third-party Go modules the module needs, each `{module, version, purpose}`, version pinned like `v1.9.3`; `[]` when only the standard library is needed.
- No secrets. Refer to secrets by environment variable name only.
- There is no limit on the number of components or types. Do not merge features to keep the list short.
- The outline is written in passes. Write at most about 12 components and 12 types in this reply, and stop at a natural boundary (after a whole component, never in the middle of one). If the list is not complete, set `"more": true` and you will be asked again for the rest; when it is complete, set `"more": false` or leave `more` out. A type comes before any type or component that uses it. Never repeat an id already emitted, and never leave `more` true without adding at least one new component or type.
- The first pass writes `module`, `conventions` and `dependencies`. A later pass leaves `module` and `conventions` out and lists only the components, types and dependencies that are new.

Module and conventions already fixed by earlier passes:
<fixed>
{{.Fixed}}
</fixed>

Ids already emitted by earlier passes:
<emitted>
{{.Emitted}}
</emitted>

Brief:
<brief>
{{.Brief}}
</brief>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

Respond with one JSON object and nothing else:
{"module": "...", "conventions": {"layout": ["..."], "naming": ["..."], "errors": "...", "logging": "...", "testing": "..."}, "components": [{"id": "...", "package": "...", "exports": [], "integration_tests": []}], "types": [<type>], "dependencies": [], "more": false}

Each <type> matches this schema:
<schema>
{{.TypeSchema}}
</schema>
