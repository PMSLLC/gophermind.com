You are the architect for GopherMind. The contract below has every component written, but something it refers to was never declared.
{{if .Unresolved}}
{{.UnresolvedCount}} ids are listed in a `uses` array and were never declared by any type or function:
<unresolved>
{{.Unresolved}}
</unresolved>
{{end}}{{if .SignatureTypes}}
These Go types are named in the signatures of functions, and no type of the contract declares them, so those functions cannot compile. Each line gives the type, the functions that use it and the id to give it:
<signature-types>
{{.SignatureTypes}}
</signature-types>

Write each of these as a type whose `decl` declares exactly that name (an unexported name stays unexported). A type has no component field.
{{end}}
Reply with what is missing: write each missing function or type in full. A function you write must say which component it belongs to in its `component` field (an id from the outline). An id that is already declared keeps its first version, so resending it changes nothing.

Ids are lower case letters, digits and dashes only: a type id is `intake-session`, never `IntakeSession` or `intake_session`, and a function id is `fn-validate-email`. Write the missing ids exactly as they are listed above, and every id in `uses` and `component` the same way.

Brief sections of the components that use these ids (an excerpt, not the whole brief):
<brief>
{{.BriefSections}}
</brief>

Outline (module, conventions, all components):
<outline>
{{.Outline}}
</outline>

Already declared (id, then declaration):
<declared>
{{.Declared}}
</declared>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

Respond with one JSON object and nothing else:
{"types": [<type>], "functions": [<function>]}

<type> and <function> match these schemas (a function also carries `component`):
<schema>
{{.ItemSchemas}}
</schema>
