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
- No secrets. Refer to secrets by environment variable name only.
- There is no limit on the number of components or types. Do not merge features to keep the list short.

Brief:
<brief>
{{.Brief}}
</brief>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

Respond with one JSON object and nothing else:
{"module": "...", "conventions": {"layout": ["..."], "naming": ["..."], "errors": "...", "logging": "...", "testing": "..."}, "components": [{"id": "...", "package": "...", "exports": [], "integration_tests": []}], "types": [<type>]}

Each <type> matches this schema:
<schema>
{{.TypeSchema}}
</schema>
