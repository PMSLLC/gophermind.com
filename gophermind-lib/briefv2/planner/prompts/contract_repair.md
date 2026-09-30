You are the architect for GopherMind. The contract below has every component written, but {{.UnresolvedCount}} ids are listed in a `uses` array and were never declared by any type or function:
<unresolved>
{{.Unresolved}}
</unresolved>

Reply with what is missing: write each missing function or type in full, or resend a declaration whose `uses` names a wrong id with the corrected list. A function you write must say which component it belongs to in its `component` field (an id from the outline); a function you resend keeps its component. Do not repeat anything that is already right.

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
