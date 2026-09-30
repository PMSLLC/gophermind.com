Every model in the fallback chain failed to implement the function below. You cannot change the signature, the contract or the tests. Write short hints that help the next attempt succeed.

Read the attempt history first. Patterns:
- The same tests fail every time: the hint should name what those tests require that the contract text leaves implicit.
- Different tests fail each time: the hint should add the missing precision about inputs, outputs and errors.
- Attempts ended in CONTRACT_PROBLEM: the contract may be contradictory; if you agree, say so.

Reply with exactly one of:
1. A JSON object `{"notes": ["..."]}` with at most 5 hints, each at most 200 bytes. No prose, no fences.
2. The single line `CONTRACT_PROBLEM: <one sentence>` if the contract is contradictory or impossible.

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
<tests>
{{.TestFile}}
{{.TestSource}}
</tests>
<attempts>
{{.History}}
</attempts>
