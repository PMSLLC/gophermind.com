# Test-writer source outside JSON: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the test writer and acceptance writer get the Go file in a fenced block after (or instead of) the JSON object, with the legacy `test_file` form still accepted.

**Architecture:** one split function finds the trailing fenced Go block and hands the head to the existing JSON decoding. `parseTestwriteCounted` and the acceptance reply parser call it and take the source from the block when present. The three call specs stop asking for JSON mode. Prompts and one fixture move to the new form.

**Tech Stack:** Go, `gophermind-lib/build/planner`.

**Spec:** `docs/superpowers/specs/2026-11-04-testwriter-source-outside-json-design.md`

## Global Constraints

- No em dashes, no emojis in anything written.
- A refusal never quotes the model's reply beyond what existing rules allow; the brief is never quoted.
- Legacy replies (`test_file` inside the JSON object) keep parsing; when both forms are present the fenced block wins.
- Run `go build ./... && go vet ./... && gofmt -l .` from `gophermind-lib` and the planner suite before each commit. Commit with explicit `git add` paths and the trailers `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`.
- Never push, install or deploy.

## Review Focus

1. A Go file that contains a fenced block inside a raw string: the split must take the last block that starts with `package `, not the first fence.
2. A reply whose JSON object is itself inside a ```json fence followed by the ```go block: head parsing through StripReply must still find the object.
3. A reply that is only a fenced Go block with no JSON head (acceptance writer, or a test-writer slip): the test writer refuses for missing tests, the acceptance writer accepts.
4. A fenced block with info string `golang` or `Go`: accepted as Go (case-insensitive, prefix `go`).
5. Windows line endings in the reply: the fence detection trims `\r`.

---

### Task 1: the split and the two parsers

**Files:**
- Create: `gophermind-lib/build/planner/sourcereply.go`
- Create: `gophermind-lib/build/planner/sourcereply_internal_test.go`
- Modify: `gophermind-lib/build/planner/testwriter.go` (`parseTestwriteCounted`, around line 789)
- Modify: `gophermind-lib/build/planner/acceptance_testwriter.go` (the reply closure, around line 89)
- Modify: `gophermind-lib/build/planner/testwriter_internal_test.go` (the never-quote fixture gains a new-form case)

**Interfaces:**
- Produces: `func splitSourceReply(text string) (head, source string)`. `source` is the body of the last fenced block whose info string is empty or starts with `go` (case-insensitive) and whose trimmed body starts with `package `; `head` is the text before that block's opening fence, trimmed. With no such block: `head = text`, `source = ""`. Lines are compared with `\r` trimmed.
- Produces: `const errNoTestFile = "reply has no test file: after the JSON object, put the complete Go test file in one fenced go block"` (an error value is fine too; the text is what matters).

- [ ] **Step 1: Write the failing tests** in `sourcereply_internal_test.go`:

```go
func TestSplitSourceReplyTakesTheTrailingGoBlock(t *testing.T)
// head is the JSON object, source is "package cli\n..." for:
//   {"tests": []}\n```go\npackage cli\n\nfunc TestX(t *testing.T) {}\n```
// the same with ```json fencing around the object and a blank line between
// info string "Go" and "golang" accepted; "```python" body starting with "package" rejected
// CRLF reply: fences detected, source has no \r
func TestSplitSourceReplyPrefersTheLastPackageBlock(t *testing.T)
// a first ```go block whose body starts with "func helper()" and a last one starting with "package p": source is the last
func TestSplitSourceReplyWithoutABlock(t *testing.T)
// a legacy JSON reply: head == text, source == ""
func TestParseTestwriteAcceptsBothForms(t *testing.T)
// through parseTestwrite: the legacy greeter fixture and the same reply rewritten as JSON tests + fenced block both parse to the same tests and the same source; a reply with both forms gives the fenced source
func TestParseTestwriteRefusesAMissingFile(t *testing.T)
// {"tests":[...three valid tests...]} with no block: error text contains "no test file" and "fenced go block"
```

Build the fixtures from `testdata/greeter/testwrite.fn-greet.txt` (read it, decode `test_file`, re-emit as the two-part form) so the test does not hand-write a Go file.

- [ ] **Step 2: Run them**: `cd gophermind-lib && go test ./build/planner -run 'TestSplitSourceReply|TestParseTestwrite' -count=1` fails to compile (undefined splitSourceReply).

- [ ] **Step 3: Implement `splitSourceReply`** in `sourcereply.go`. Scan lines; a line whose trimmed text starts with three backticks opens or closes a fence; record each block's opening-line offset, info string and body; pick the last block meeting the rule.

- [ ] **Step 4: Wire `parseTestwriteCounted`**: at the top, `head, fenced := splitSourceReply(text)`; decode `StripReply(head)` where it decoded `text` before (the `closeStrayStrings` fallback applies to the head). After decoding: `if fenced != "" { reply.TestFile = fenced }`; then `if strings.TrimSpace(reply.TestFile) == "" { return errNoTestFile }` placed before `checkNotPlaceholder`. The two call sites that pass `StripReply(text)` (testwriter.go writeTests closure, revise.go line 477) now pass `text`: `StripReply` on a two-part reply would keep only the first fenced block.

- [ ] **Step 5: Wire the acceptance reply closure** in `acceptance_testwriter.go`: `head, fenced := splitSourceReply(reply)`; when `fenced != ""` use it as `rep.TestFile` and skip the JSON unmarshal; else unmarshal `StripReply(head)` as today. The existing "not a JSON object" error stays for the legacy path.

- [ ] **Step 6: Add a new-form case** to `TestTestwriterErrorsNeverQuoteTheReply` (testwriter_internal_test.go): the canary inside a fenced block body that is not Go, asserting the canary never appears in the error.

- [ ] **Step 7: Run** `go test ./build/planner -run 'TestSplitSourceReply|TestParseTestwrite|TestTestwriter|TestAcceptance' -count=1`, then the whole planner suite, then `go build ./... && go vet ./... && gofmt -l .`.

- [ ] **Step 8: Commit**

```bash
git add gophermind-lib/build/planner/sourcereply.go gophermind-lib/build/planner/sourcereply_internal_test.go gophermind-lib/build/planner/testwriter.go gophermind-lib/build/planner/acceptance_testwriter.go gophermind-lib/build/planner/revise.go gophermind-lib/build/planner/testwriter_internal_test.go
git commit -m "feat(planner): the test file may follow the JSON reply in a fenced go block"
```

### Task 2: prompts, call specs, json-mode test, fixture

**Files:**
- Modify: `gophermind-lib/build/planner/prompts/testwriter.md` (last two lines)
- Modify: `gophermind-lib/build/planner/prompts/testwriter_accept.md` (last two lines)
- Modify: `gophermind-lib/build/planner/testwriter.go` (the `callSpec` at line 562), `revise.go` (line 467), `acceptance_testwriter.go` (line 82: already without jsonObject; leave it)
- Modify: `gophermind-lib/build/planner/replyschemas.go` (remove `testwriteReplySchema` if nothing else uses it)
- Modify: `gophermind-lib/build/planner/jsonmode_test.go`
- Modify: `gophermind-lib/build/planner/testwriter_prompt_internal_test.go`
- Modify: `gophermind-lib/build/planner/testdata/greeter/testwrite.fn-greet.txt`

**Interfaces:**
- Consumes: `splitSourceReply` and the two-form parser from Task 1.

- [ ] **Step 1: Write the failing tests**:

```go
// testwriter_prompt_internal_test.go
func TestTestwriterPromptAsksForAFencedGoFile(t *testing.T)
// the rendered testwriter prompt contains "one fenced go block" and does not contain `"test_file"`
// the rendered testwriter_accept prompt contains "one fenced go block" and does not contain `"test_file"`
```
and in `jsonmode_test.go` flip the testwrite branch: a request whose system prompt contains `testwrite:` must have `rq.JSON == false` and an empty schema; coverage keeps its assertions.

- [ ] **Step 2: Run them**: `go test ./build/planner -run 'TestTestwriterPrompt|TestOnlyObjectReplyStages' -count=1` fails.

- [ ] **Step 3: Change the prompts.** testwriter.md: replace the last two lines with
```
Respond with the JSON object of tests, then the complete Go test file in one fenced go block, and nothing else:
{"tests": [{"name": "...", "given": "...", "expect": "...", "polarity": "success", "covers": "happy"}]}
```go
<full Go source of the test file>
```
```
testwriter_accept.md: replace the last two lines with
```
Respond with the complete Go source of {{.File}} in one fenced go block and nothing else.
```

- [ ] **Step 4: Call specs.** Remove `jsonObject: true, schema: testwriteReplySchema` from the test-writer callSpec (testwriter.go line 562) and the revise-tests callSpec (revise.go line 467). Delete `testwriteReplySchema` from replyschemas.go if it is now unused (`grep -rn testwriteReplySchema`).

- [ ] **Step 5: Convert the fixture** `testdata/greeter/testwrite.fn-greet.txt` to the two-part form: the JSON object with only `tests`, a blank line, then the decoded `test_file` inside ```go fences. Decode with a short Go test or `python3 -c` and paste; do not hand-type the file. Leave `testwrite.fn-farewell.txt` and `testwrite.fn-name-error-error.txt` as they are.

- [ ] **Step 6: Run** the full planner suite, then `go build ./... && go vet ./... && gofmt -l .`, then `go test ./build/router ./build/execenv -count=1`.

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/build/planner/prompts/testwriter.md gophermind-lib/build/planner/prompts/testwriter_accept.md gophermind-lib/build/planner/testwriter.go gophermind-lib/build/planner/revise.go gophermind-lib/build/planner/replyschemas.go gophermind-lib/build/planner/jsonmode_test.go gophermind-lib/build/planner/testwriter_prompt_internal_test.go gophermind-lib/build/planner/testdata/greeter/testwrite.fn-greet.txt
git commit -m "feat(planner): the test-writer prompts ask for the Go file in a fenced block, not JSON"
```
