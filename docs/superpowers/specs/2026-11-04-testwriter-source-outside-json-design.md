# Test-writer replies: the Go file outside JSON

Date: 2026-11-04. Asked for by John ("take source out of json") after reviewing linkcheck-v1-20261104-r002.

## Intent

Every test-writer reply today is one JSON object whose `test_file` field holds the whole Go test file as a JSON string. The model must JSON-escape every quote, backslash and newline of Go source, and a local model slips on exactly those: `\"` where `"` belongs, `\\n` at row ends, a missing backslash, an unquoted import path. Five repair functions in the planner (unescapeQuotedSource, quoteBareImports, nudgeSyntax, closeStrayStrings, unescapeBareEscapes) exist only to undo those slips, and r002 still spent two to seven rounds per node on them.

The fix is to stop asking for source inside JSON. The model writes the Go file as it would in an editor, in one fenced block after the JSON metadata. No escaping, nothing to repair.

## Goals

1. The test-writer reply is two parts: a JSON object with `tests`, then the complete test file in one fenced ```go block. The acceptance test writer, whose JSON today holds only `test_file`, replies with the fenced block alone.
2. The parser accepts the new form and the legacy form (`test_file` inside the JSON object), so recorded fixtures, resumed runs and older prompts keep working. When both are present the fenced block wins.
3. Those calls no longer ask the provider for JSON mode or a JSON schema: json_object mode would forbid the fenced block.
4. A reply with the JSON object but no fenced Go block is refused with text that says where the file goes.

## Non-goals

- Changing the revise stage's own reply (its first form is a JSON patch of node fields); the whole-file test rewrite it requests goes through the same parser and gains the new form for free.
- Removing the repair chain. It stays for the legacy form and for slips the model makes inside Go itself (a bare `\n` in source is still possible, just much rarer).
- Changing contract, enrich, decompose or coverage replies. They are structured data and stay JSON.

## Decided rules

- **Reply form (test writer):**
  ```
  {"tests": [{"name": "...", "given": "...", "expect": "...", "polarity": "success", "covers": "happy"}]}
  ```go
  package ...
  ```
  ```
  The JSON object may itself be fenced as ```json; StripReply already handles that on the head part.
- **Reply form (acceptance writer):** the file alone in one fenced ```go block. A JSON object with `test_file` is still accepted.
- **Split rule:** the source is the body of the last fenced block whose info string is empty or `go` and whose body parses with `parser.ParseFile(..., parser.PackageClauseOnly)` (so comments and `//go:build` lines may come before the package clause). Everything before that block's opening fence is the head and is parsed as JSON. A reply with no such block is parsed entirely as legacy JSON; if that JSON has no `test_file` either, the refusal is: `reply has no test file: after the JSON object, put the complete Go test file in one fenced go block`.
- **Precedence:** fenced block over `test_file` when both are present.
- **JSON mode:** the test-writer, revise-tests and acceptance-writer call specs set no `jsonObject` and no `schema`. The json-mode test asserts a testwrite request does not ask for JSON mode.
- **Fixtures:** `testdata/greeter/testwrite.fn-greet.txt` is converted to the new form so the end-to-end suite exercises it; the other two greeter testwrite fixtures stay legacy so that path is exercised too.
- **Prompt copy:** the test-writer prompt's last two lines become: `Respond with the JSON object of tests, then the complete Go test file in one fenced go block, and nothing else:` followed by the example above. The acceptance prompt's last two lines become: `Respond with the complete Go source of {{.File}} in one fenced go block and nothing else.`
- **Quoting:** refusals never quote the reply; existing rules stand.

## Left to the builder

- Where `splitSourceReply` lives (a new file beside parse.go is fine) and the exact helper names.
- Whether `fencedBlocks` is reused or the split scans fences itself; the split needs the opening fence's position, which `fencedBlocks` does not return.
