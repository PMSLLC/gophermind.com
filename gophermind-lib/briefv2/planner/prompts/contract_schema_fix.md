You are the architect for GopherMind. The contract below is complete, but {{.Count}} of its nodes lack a required field. Write ONLY the missing fields of the nodes listed here.

Nodes to fix (each line names the node and the fields it lacks, then the node as it stands, for context):
<nodes>
{{.Nodes}}
</nodes>

Reply with one JSON object and nothing else: {"types": [...], "functions": [...]}. For each node above, send an object with its `id` and only the fields it lacks. Every other field, and every node not listed, is ignored, so resending them changes nothing.

- `doc` is the doc comment text, one to three sentences that state behavior, not implementation.
- `signature` is the exact Go signature line.
- `file` is a path relative to the repository root. `package` is the Go package name. `decl` is the complete Go declaration with its doc comment.
- Ids are lower case letters, digits and dashes only: a type id is `intake-session`, never `IntakeSession` or `intake_session`, and a function id is `fn-validate-email`.

Brief sections of the components that own these nodes (an excerpt, not the whole brief):
<brief>
{{.BriefSections}}
</brief>

Outline (the module and the components that own these nodes):
<outline>
{{.Outline}}
</outline>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

The node schemas:
<schema>
{{.ItemSchemas}}
</schema>
