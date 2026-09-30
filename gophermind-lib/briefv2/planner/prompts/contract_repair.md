You are the architect for GopherMind. The contract below has every component written, but {{.UnresolvedCount}} ids are listed in a `uses` array and were never declared by any type or function:
<unresolved>
{{.Unresolved}}
</unresolved>

Reply with what is missing: write each missing function or type in full. A function you write must say which component it belongs to in its `component` field (an id from the outline). An id that is already declared keeps its first version, so resending it changes nothing.

Ids are lower case letters, digits and dashes only: a type id is `intake-session`, never `IntakeSession` or `intake_session`, and a function id is `fn-validate-email`. Write the missing ids exactly as they are listed above, and every id in `uses` and `component` the same way.

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
