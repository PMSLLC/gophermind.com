Implement exactly one Go function. Everything you need is below. You cannot see any other file and you must not assume anything not written here.

Rules:
- Output the complete contents of the file `{{.File}}`, package `{{.Package}}`, including the package clause, imports, and the function with its doc comment.
- The signature must be character-for-character: `{{.Signature}}`
- Use only these declarations from other files; they already exist, do not redefine them:
<dependency_signatures>
{{.DependencySignatures}}
</dependency_signatures>
- Standard library only unless a constraint says otherwise.
- Do not write tests. Do not modify any other file. Do not add exported identifiers beyond the signature above.
- If the contract is contradictory or impossible, do not guess. Output exactly the single line `CONTRACT_PROBLEM: <one sentence>` and nothing else.

Contract:
<contract>
{{.Contract}}
</contract>

Constraints:
<constraints>
{{.Constraints}}
</constraints>

Tests the file must pass (you cannot change them):
<tests>
{{.Tests}}
</tests>
{{if .PreviousFailure}}
Your previous attempt failed these tests:
<previous_failure>
{{.PreviousFailure}}
</previous_failure>
{{end}}
Respond with the Go source only. No prose, no fences.
