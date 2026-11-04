# Test-writer replies: mechanical fixes and a narrow repair call

Date: 2026-11-04. Asked for by John ("both") after the r004 review: "Can we have a syntax fix that fixes problems in code before it rejects."

## Intent

In linkcheck-v1-20261104-r004 the test writer spent 37 refusals on six nodes and failed the run on one. Nineteen of those refusals were compile or syntax errors in a file whose shape was otherwise right: an unused variable, a missing standard import, a wrong type in one return statement, a brace out of place. Today every one of them costs a full round: the whole 2.8k-token brief goes back to the model with the error appended, and the model re-solves the task while fixing the error, often breaking something else (r004 fn-process-file alternated between two defects for 18 rounds).

Two changes, in the order they run:

1. **Mechanical fixes.** A compiler diagnostic with exactly one correct fix is applied by the harness, the way unused imports already are, and the file is compiled again with no model call.
2. **A narrow repair call.** When the file still has syntax or compile errors, one extra model call receives only the file and the error list, nothing from the brief or the contract, and returns the corrected file. The pipeline runs again on that file. Only if it still fails does the reply become a refusal and a round as today.

## Goals

1. A reply refused today only for an unused variable or a missing standard import passes with no model call.
2. A reply refused today for syntax or compile errors gets one focused repair call before it costs a round; the repair call sees the file and the errors only.
3. Both are visible in the run: an event names each mechanical fix and each successful repair, the repair call is a row in calls.jsonl under its own stage, and a refusal is recorded only when the repair failed, with the errors that remained.
4. The revise stage's whole-file test rewrite (revise.go) gets the same treatment, since it runs the same pipeline.

## Non-goals

- Fixing semantic refusals (real host, test exercises nothing, declared name, shadowed import) by the repair call. Those keep the round-based path. The host rule in particular depends on the brief and the contract, which the repair call does not see.
- Guessing a fix for an unbalanced brace in the harness. That goes to the repair call.
- Changing the round count (testwriterRounds) or the router's parse retries.
- Unused-import handling, done in dff2ed8 and 92df06d.

## Decided rules

### Mechanical fixes

Applied after a failed compile check, to the formatted source, driven by the diagnostics of that check. Each fix has one correct form; nothing is guessed. After applying any fix the file is formatted and compiled once more. The second compile's diagnostics are what the repair call or the refusal sees.

- **Unused variable.** Diagnostic `declared and not used: x`. `_ = x` is inserted where `x` is in scope: after the declaring statement when it is a plain statement in a block; as the first statement of the body when `x` is declared in an `if`, `for`, `switch` or `select` header; as the first statement of every clause when it is the variable of a type switch. One insertion per name. The test still runs the same code, and the model's intent (a `wantErr` it never compared) stays visible in the file for the reader.
- **Missing standard import.** Diagnostic `undefined: pkg` where `pkg` is in `stdTestPackages` (compilecheck.go) and the file does not already import a package whose name is `pkg`. The import `"pkg"` (path from a fixed map: `fmt`, `strings`, ... map to themselves; `testing` and the rest likewise, since every member of `stdTestPackages` is a top-level path) is added to the import block. The existing `missingStdImport` helper names the package.

Nothing else is fixed mechanically in this change. The two-value assignment from a one-value call and the `*httptest.Server` versus `*http.Server` return mismatch seen in r004 cascade into other lines and are left to the repair call.

Each applied fix emits an event `test_file_mechanical_fix: <kind> <name> in the test file of node <id>` at warning level, the level `test_import_normalized` uses.

**Line numbers in a refusal.** The model only ever sees its own reply, so a refusal always reports the diagnostics of the first compile, the one on the model's file, minus the diagnostics the mechanical pass fixed. The diagnostics of the fixed or repaired file go to events only. The router's retry therefore shows the model its own reply with errors whose line numbers match it.

### Repair call

- **When.** After the mechanical pass, if the file still fails the compile check; and when `parseTestwriteCounted` refuses the file as not valid Go (syntax). Not for any other refusal.
- **How many.** One repair call per model reply. If the repaired file fails too, the reply is refused with the errors of the repaired file, and the round proceeds exactly as today.
- **Stage and routing.** `callSpec{stage: "testwrite:<id>:fix", taskType: "testwrite", nodeID, nodeClass, tier, scope, maxTokens}` copied from the node's test-writer spec with the stage suffix. Same model tier. No jsonObject, no schema.
- **Prompt** (`prompts/testwriter_fix.md`): the file in one fenced go block, then the error list exactly as the refusal would word it (the compile or syntax text, which already quotes the model's own lines and never the brief; the advice `compilePhrase` adds from the contract, such as the declared names of the package, stays, since it is the contract's own words and is what fixes an undefined name), then: `Reply with the complete corrected Go file in one fenced go block and nothing else. Change only what the errors require; keep every test case, helper and assertion.` No brief section, no node JSON, no contract, no dependency signatures. The system prompt is the router's usual one.
- **Reply.** Parsed with `splitSourceReply`: a fenced go block, or a file-only reply. The parse given to the router always accepts the reply (returns nil) and hands the text out, so the router never retries a repair call and its ledger row is a normal successful call; whether the repair worked is the planner's own record. A repair reply with no Go file, or a file whose package clause differs from the original, is a failed repair: the original errors are refused, and the event says the repair reply was unusable. The repair call copies the node's call spec, so its reply gets the same secret scan (`skipSave`, the secret-in-reply class) as any test-writer reply.
- **After the repair.** The repaired file goes through the same checks as a fresh reply from the point after JSON parsing: the repair chain (unescape, quote, nudge), dropUnusedImportAlias, fixTestPackageClause, dropSelfImport, dropLongComments, checkTestSourceIn, checkTestExercises, the host rule, resolveSharedNames, dropUnusedImports, format, mechanical fixes, compile. The tests list (the JSON head) is the original reply's; the repair does not change it.
- **Events and records.** The call is a normal call (a calls.jsonl row and an llm/N.json dump under its stage). On success: event `test_file_repaired: the test file of node <id> compiled after a repair call` at warning level. On failure: the refusal recorded for the node is the one for the repaired file, prefixed `after a repair call: `, so the retry note tells the model the repair did not help and names the remaining errors.
- **Where it lives.** The closure in `writeTests` (testwriter.go) and the one in revise.go (the test rewrite) share one function, `p.settleTestFile(ctx, r, cs, src, ...)` or a name the builder picks, that runs the post-parse checks, the mechanical pass, the repair call and the final compile. Both call sites use it so the behaviour is identical. That gives the revise rewrite two checks it skips today, `checkTestExercises` and the host rule; intended, since a rewritten test that fetches a real host or asserts nothing fails later for the same reasons a fresh one would.

### Accounting

- The accepted node record in testwriter.json (`writtenTests`) gains `fixes []string` (the kinds of mechanical fix applied to the accepted file, empty when none) and `repaired bool` (the accepted file came from a repair call). `nodeRounds` is unchanged: it exists only while a node is failing and is deleted on a rerun. The runs page is not changed in this work.

## Left to the builder

- The exact AST walk for finding the declaring statement of an unused variable (go/ast with positions; the diagnostic gives line and column).
- The helper names and the file split (a new file beside compilecheck.go for the mechanical pass, a new file for the repair call).
- Test fixtures: the three r004 replies that show each case are a good source (fn-all round 1 for the unused variable, fn-follow round 4 for the missing import pattern, fn-process-file round 11 for syntax); copy the relevant Go, not the brief.
