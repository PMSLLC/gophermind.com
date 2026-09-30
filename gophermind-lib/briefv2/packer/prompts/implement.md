Implement exactly one Go function. Everything you need is below. You cannot see any other file and you must not assume anything not written here.

Rules:
- Output the complete contents of the file named in the file section, with the package named there, including the package clause, imports, and the function with its doc comment.
- The signature must be character-for-character the one in the signature section.
- The reply is the whole Go file, nothing else. Import only the standard library, this module's own packages, and the modules in `<constraints>`.
- Use only the declarations in the dependency section from other files; they already exist, do not redefine them.
- Do not write tests. Do not modify any other file. Do not add exported identifiers beyond the signature.
- If the contract is contradictory or impossible, do not guess. Output exactly the single line `CONTRACT_PROBLEM: <one sentence>` and nothing else.
- Respond with the Go source only. No prose, no fences.

<file>
file: {{.File}}
package: {{.Package}}
</file>
<signature>
{{.Signature}}
</signature>
<contract>
{{.Contract}}
</contract>
<dependency_signatures>
{{.Deps}}
</dependency_signatures>
<constraints>
{{.Constraints}}
</constraints>
{{if .Notes}}<revision_notes>
{{.Notes}}
</revision_notes>
{{end}}<tests>
{{.TestFile}}
{{.TestSource}}
</tests>
{{if .Failure}}<previous_failure>
{{.Failure}}
</previous_failure>
{{end}}
