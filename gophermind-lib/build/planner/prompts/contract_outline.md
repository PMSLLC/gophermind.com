You are the architect for GopherMind. Produce the outline of the contract for the product below: the Go module, the conventions every implementer follows, the list of components, and the types more than one component shares. A later pass writes each component's functions, so do not write any function here.

Requirements:
- Go only. Standard library unless the brief's Constraints allow modules.
- Components map to the brief's ### Feature headings plus a `types` component for shared declarations. A component id is lower case letters, digits and dashes. A type id follows the same syntax: `intake-session`, never `IntakeSession` or `intake_session`; a function id is `fn-validate-email`. Never use `logs` or `outline` as an id (they name a folder and a stage of the run).
- List the components in dependency order: a component comes after every component whose functions it calls.
- Every type decl is complete Go source with a doc comment, exactly as it will appear in the file. `uses` lists the ids of other types the decl references.
- Every type's `file` is a path relative to the repository root.
- A component has exactly these keys: `id`, `package`, `exports`, `integration_tests`. No other key is allowed, and a component has no `file`.
- Leave each component's `exports` empty; the harness fills it.
- `integration_tests` for a component describe behavior that needs more than one of its functions. Give `name`, `given`, `expect`, and a `command`.
- Error handling follows one convention stated in `conventions.errors`.
- `dependencies`: third-party Go modules the module needs, each `{module, version, purpose}`, version pinned like `v1.9.3`; `[]` when only the standard library is needed.
- No secrets. Refer to secrets by environment variable name only.
- There is no limit on the number of components or types. Do not merge features to keep the list short.
- The outline is written in passes chosen by the harness; you never decide whether more remains, and a `more` field is ignored. A type comes before any type or component that uses it. Never repeat an id that is already declared: it keeps its first version, so a repeat changes nothing and only wastes the reply.
{{if .Entry}}- This is the entry-point pass: see the end of this prompt.
{{else if .Unresolved}}- This is a repair pass: see the end of this prompt.
{{else if .Batch}}- This is a batch pass. Write the components and types for THESE features only: {{.Batch}}
  Each feature's full text is under its `### <name>` heading in the brief. Write one component per feature, plus any type only these features need. Do not write components or types for other features. You may name the ids already declared below in `uses`.
{{else}}- This is the shared pass. Write `module`, `conventions`, `dependencies`, the `types` component (id `types`, for shared declarations) and the shared domain types: the entities more than one feature uses. Write NO feature component here; each feature gets its own pass afterwards.
{{end}}{{if .EntryName}}- The brief describes something that is run, so this pass also writes the entry point: one component with id `entry-point` and package `cmd/{{.EntryName}}`, listed after the feature components. A later pass gives it a function `fn-main` named `main` with the signature `func main()`, and the functions that wire the feature functions into the program (flags, input, output, exit codes).
{{end}}- If the fixed section below names the repository's go.mod module, `module` is that path exactly; do not invent another one.
- The first pass writes `module`, `conventions` and `dependencies`. A later pass leaves `module` and `conventions` out and lists only the components, types and dependencies that are new.

Module and conventions already fixed by earlier passes:
<fixed>
{{.Fixed}}
</fixed>

Ids already declared by earlier passes (components, types): do not repeat them; you may reference them in `uses`:
<emitted>
{{.Emitted}}
</emitted>
{{if .Unresolved}}
Repair: the outline above is complete, but {{.UnresolvedCount}} ids are listed in a `uses` array and were never declared by any type or function:
<unresolved>
{{.Unresolved}}
</unresolved>
Reply in the same JSON shape with the missing types written in full (and a component only if one is really missing). An id that an earlier pass already wrote keeps its first version, so resending it changes nothing.
{{end}}{{if .Entry}}
Entry point: the brief describes something that is run, yet no component has a package under cmd/, so no binary can be built and no behavior of the program can be proven. Reply in the same JSON shape with one component: id `entry-point`, package `cmd/{{.EntryName}}`, `exports` empty. The binary is named {{.EntryName}}. Write no other component.
{{end}}
Brief:
<brief>
{{.Brief}}
</brief>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

Respond with one JSON object and nothing else:
{"module": "...", "conventions": {"layout": ["..."], "naming": ["..."], "errors": "...", "logging": "...", "testing": "..."}, "components": [{"id": "...", "package": "...", "exports": [], "integration_tests": []}], "types": [<type>], "dependencies": []}

Each <type> matches this schema:
<schema>
{{.TypeSchema}}
</schema>
