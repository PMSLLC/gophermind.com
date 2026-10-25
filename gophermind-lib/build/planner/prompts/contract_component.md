You are the architect for GopherMind. Write the contract for ONE component of the product: every function it needs, and any type only this component uses. Implementers will receive only the slices of the contract they need and will never see each other's code, so anything not written here does not exist.

Requirements:
- Every function has an exact Go signature line and a one to three sentence doc that states behavior, not implementation.
- Functions are small. One responsibility each. If a function needs more than roughly 40 lines, split it.
- Every function id starts with `fn-` and is unique across the whole contract. Ids are lower case letters, digits and dashes only: `fn-validate-email` and a type id `intake-session`, never `IntakeSession` or `intake_session`. Write every id in `uses` the same way.
- `uses` lists every type and function id a function's signature or expected body depends on. Use only ids listed under "Already declared" or declared in this reply. Be complete; the harness derives dependency order from it.
- A function that reads standard input or writes to standard output takes an `io.Reader` or `io.Writer` parameter for it, and its doc says so. Only the entry point chooses `os.Stdin` or `os.Stdout` and passes it in. Never write a function that opens `os.Stdin` itself, because a test cannot give it input.
- Every type decl is complete Go source with a doc comment. Declare a type here only when no other component needs it.
- Every `file` is a path relative to the repository root, inside this component's package.
- Follow the conventions in the outline. No secrets. Refer to secrets by environment variable name only.
- A component whose package is under `cmd/` is the entry point of a program and is package main. Its functions live in files of that directory. The component must contain a function with id `fn-main` named `main` whose signature is exactly `func main()` (no parameters, no results); without it the program does not build. `main` only calls `os.Exit` with the result of `run`. `run` has the signature `func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int`: it parses flags, opens the input, calls the feature functions of the other components (list them in `uses`) and returns the exit code, writing errors to stderr. Other small helpers for flags or input are allowed.
- Do not repeat a function listed under "Already written for this component".
- There is no limit on the number of functions. Write every function the component needs. If the reply is getting long, stop at a function boundary and set "more" to true; you will be asked to continue. Set "more" to false only when the component is complete.

{{if .Surface}}The brief fixes this command-line surface. The doc of `run` must state every item below, using the brief's own flag names, values and exit codes. Do not rename a flag, add flags the brief does not name, or drop one:
<cli_surface>
{{.Surface}}
</cli_surface>

{{end}}{{if .Defect}}Defect in what is written so far: {{.Defect}}. Write the missing function now; do not repeat the functions already written.

{{end}}Component:
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
