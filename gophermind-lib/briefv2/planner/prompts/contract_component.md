You are the architect for GopherMind. Write the contract for ONE component of the product: every function it needs, and any type only this component uses. Implementers will receive only the slices of the contract they need and will never see each other's code, so anything not written here does not exist.

Requirements:
- Every function has an exact Go signature line and a one to three sentence doc that states behavior, not implementation.
- Functions are small. One responsibility each. If a function needs more than roughly 40 lines, split it.
- Every function id starts with `fn-` and is unique across the whole contract. Ids are lower case letters, digits and dashes only: `fn-validate-email` and a type id `intake-session`, never `IntakeSession` or `intake_session`. Write every id in `uses` the same way.
- `uses` lists every type and function id a function's signature or expected body depends on. Use only ids listed under "Already declared" or declared in this reply. Be complete; the harness derives dependency order from it.
- Every type decl is complete Go source with a doc comment. Declare a type here only when no other component needs it.
- Every `file` is a path relative to the repository root, inside this component's package.
- Follow the conventions in the outline. No secrets. Refer to secrets by environment variable name only.
- Do not repeat a function listed under "Already written for this component".
- There is no limit on the number of functions. Write every function the component needs. If the reply is getting long, stop at a function boundary and set "more" to true; you will be asked to continue. Set "more" to false only when the component is complete.

Component:
<component>
{{.Component}}
</component>

Outline (module, conventions, all components):
<outline>
{{.Outline}}
</outline>

Already declared (id, then declaration):
<declared>
{{.Declared}}
</declared>

Already written for this component:
<written>
{{.Written}}
</written>

Brief section for this component:
<brief_section>
{{.BriefSection}}
</brief_section>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

Respond with one JSON object and nothing else:
{"types": [<type>], "functions": [<function>], "more": false}

<type> and <function> match these schemas (leave `component` out; the harness sets it):
<schema>
{{.ItemSchemas}}
</schema>
