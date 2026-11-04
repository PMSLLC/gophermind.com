# Getting a brief through the test writer with the local model

Date: 2026-11-03. Status: approved direction (John: "it must stay local only"); document for the builder check.

## Intent

No brief has ever passed the test-writer stage of the planner. Of the 147 runs on John's laptop (`~/.gophermind/local/briefs`), 64 reached the test writer and none left it: 34 died with `router: no model answered for tier any: gemma/gemma-4-26b-a4b: excluded (its last reply was unusable)`, the rest were interrupted. 16 more died the same way at the contract stage. 18 died at load because three example briefs (hookrelay, ledger-sync, taskboard) declare secrets and the server has no vault passphrase.

The verbose log of `linkcheck-v1-20261104-r001` (node `fn-follow`, calls 47 to 52 in `_state/llm/`) shows why the test writer fails:

- Gemma wrote a 146-line table-driven test file that was almost right. It had two compile errors: `next` declared and not used (line 27, `if next, ok := m.redirects[...]`) and `context.WithTimeout(...)` assigned to one value (line 108).
- The harness told the model only `unused variable at line 27; does not compile at line 108`. `compileMessage` (`build/planner/compilecheck.go`) reduces a diagnostic to a phrase and a line number (a few cases name contract types), and by design never copies the compiler's text or the model's source, so the model never saw the name `next` or the two-value error.
- The retry (`withParseError` in `build/router/parsed.go`) appends the broken reply as an assistant message and says "Reply again with the corrected output only". The model copied itself: six attempts over five minutes (1 call + `max_parse_retries: 5`), replies within a few bytes of each other, the same two errors every time. The chain has one model, so after the retries the model was excluded and the run died.

The model can fix "unused variable `next`" and "`context.WithTimeout` returns two values". It cannot fix a line number inside a JSON-escaped string. This is a harness defect first.

## Goals

1. **Feedback that names the defect.** Every compile refusal, for each diagnostic (at most three), carries the line number, the compiler's own message with the scratch path removed, and the text of that source line (trimmed, capped at 120 characters). The existing phrases and the `rich*` guidance stay as the prefix where they add advice. Example: `unused variable at line 27: declared and not used: next: if next, ok := m.redirects[req.URL.String()]; ok {`; `does not compile at line 108: assignment mismatch: 1 variable but context.WithTimeout returns 2 values: ctx: context.WithTimeout(context.Background(), 1*time.Millisecond),`. This reverses a rule the code enforces today (comments in `compilecheck.go`, `events.go` and `refusal.go`, and the tests `TestCompileMessageNeverQuotesDiagnostic`, `TestCompileMessageModelTypeFallsBack`, `TestCompileMessageInterfaceToWriter` and the unsafe-call-name check in `compilecheck_test.go`); those comments and tests are rewritten on purpose to the new rule: the model's test file and the compiler's words about it may be quoted, the brief may not. `maxRefusalBytes` in `refusal.go` rises so the recorded refusal matches what the model was sent (three diagnostics with lines fit in 800 bytes).
2. **A retry that does not copy the last reply.** In the router (`CallParsed`), for every stage: the first retry stays as it is (the reply plus what was wrong). From the second retry on, the request is the original prompt plus a note listing the distinct errors so far (capped at 600 bytes), without any earlier reply. A reply whose text is identical (same hash) to an earlier failed reply goes straight to the fresh form. The note counts toward the prompt budget of `callSized` stages, so a request that fit before still fits.
3. **A node that does not kill the run.** When the router exhausts the chain for a test-writer node, the test writer starts the node again from a fresh call with a one-paragraph summary of the earlier rounds' errors in the prompt, up to three rounds in all, then fails as today. Rounds survive a resume (recorded in `_state/testwriter.json`). The contract stage gets the same treatment only if it falls out of goal 2 for free; it is not a goal of this round.
4. **The escape glitch is pinned, not newly repaired.** Calls 50 and 51 of the linkcheck run contain a doubled escape (`\\n` in the JSON, a literal backslash-n in the Go source between two table cases) and were refused for the unused variable only, so the existing unescape step (`unescapeQuotedSource` and `nudgeSyntax` in the parse pipeline) already absorbs it. A test built from that reply (`_state/llm/50.json`, the `test_file` field) pins that: it parses, and the compile check reports exactly the `next` diagnostic. No new automatic edit of the test file.
5. **Secrets for the three briefs.** The local service runs with `GOPHERMIND_VAULT_PASSPHRASE` read from a mode-600 file, and the harness scope of the vault holds placeholder values for every secret the example briefs declare, so those briefs get past load. The placeholder values are not real credentials.
6. **A measuring loop that takes minutes, not hours.** A resume of a failed run restarts at the stage that failed (`gophermind brief resume <id>`; `_state/testwriter.json` keeps the nodes already written), so each fix is checked against real failed runs: `linkcheck-v1-20261104-r001` first, then the other test-writer failures. A script lists the run ids and the stage each reaches.

Done for the round: `linkcheck-v1-20261104-r001` resumes past the test writer, the other failed test-writer runs are resumed and their reached stages recorded, and the Harness is started. The target the round is measured against overnight is one example brief at a green build with Gemma alone. The executor has never run on real data; a failure there is the next round's evidence, not this round's failure.

## Non-goals

- No cloud model, no second local model, no change to the tier chains. Gemma 4 26B-A4B on 127.0.0.1:8081 is the only model (John, 2026-10-29 and 2026-11-03).
- No rewrite of the test-writer prompt beyond the error feedback.
- No change to the executor.
- No change to how the planner stores state (files in the run folder).

## Anti-goals (failure even if it works)

- Brief text, secret values or the vault passphrase appear in a refusal, a log line, the ledger or a model prompt they did not already reach. The model's own test file and the compiler's diagnostics about it are allowed in feedback; the brief is not.
- A retry loop without a bound. The bound is rounds times (1 + `max_parse_retries`) calls per node; about 20 minutes at today's settings is a guide, not a cap.
- An automatic repair that changes what a test checks. Only the escape glitch in goal 4 is repaired.
- A placeholder secret that looks like a real credential, or a real one committed anywhere. The passphrase must not reach the environment of the programs the executor builds and runs; the plan verifies this.

## Constraints

- Repo rules: no em dashes or emojis, subagent-driven plan, commit per task with explicit paths, no push, deploy or install unless John asks.
- Tests use the offline fixture model (`planner.FixtureProvider`, `provider.Fake`): a fixture can serve different replies to the nth request of a stage, which is how a model that repeats itself and a model that fixes its file on the fresh form are simulated.
- The planner's race tests take 5 to 10 minutes; run the focused tests per task and the full planner and router packages once at the end.
- `max_parse_retries` stays a setting (today 5 in `~/.gophermind/gophermind.yaml`).

## Decided by John

- Local only.
- The plan is harness fixes, measured on the failed runs, then the Harness runs overnight.

## Author's calls (John can overturn any of them)

- Feedback may quote the model's own source line and the compiler's text about it. The refusal is written to `_state/refusals.jsonl` and the run log inside the run folder, which already holds the plan and the test file, so this adds no new exposure. The brief is never quoted.
- The fresh form of a retry lives in the router (`CallParsed`), because the router owns the retry count; the per-node rounds live in the test writer, because only it knows a node.
- The vault passphrase file is `~/.gophermind/local/vault-passphrase`, created by `scripts/local-service.sh install` when missing (random, mode 600, never printed), and the install step also sets the placeholder secrets.
- The measuring script is `scripts/testwriter-bench.sh`: it takes run ids, resumes each in turn with the local environment, and prints the stage reached and the error, if any.

## Left to the builder

- The exact message format and caps (line text length, total refusal length) and how `diagRE` and `compileMessage` carry the identifier out of the compiler's text.
- How the escape glitch is located without parsing a file that does not parse (for example `go/scanner` positions of string literals, then a text replacement outside them).
- Where in `_state/testwriter.json` the round counter and the error summary live, and how a resume reads them.
- How the fresh retry note is phrased and bounded.
- Which secrets the example briefs declare (read the briefs) and the placeholder format.
