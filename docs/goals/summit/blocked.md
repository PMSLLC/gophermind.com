# Blocked: task 1, csvstat acceptance 6 of 6

Written 2026-10-24 under the "three fix rounds" rule of `docs/GOAL-SUMMIT.md`.

## State
- Run `gm-2026-10-24-002` (uploaded from the browser, planned and built on development-jb with the mini's local model, Cline for some leaves). Planned: 13 of 13 requirements covered. Built: 6 of 11 leaves verified, 2 failed, 3 blocked, acceptance 0 of 6 (stop reason `blocked`).
- Reply path and evidence: `gophermind brief diagnose gm-2026-10-24-002 --repo /opt/gophermind/workspace` (run as `gmrun`), and `/opt/gophermind/workspace/.gophermind/gm-2026-10-24-002/attempts/<leaf>/<n>/`. Logs: `/home/gmrun/csvstat-server-run1{3,4,5,6,7,8,9}-plan.log` and `-run19-run.log`.

## What was fixed along the way (each deployed, each with tests)
Planning stopped five different ways, each a general defect and not a csvstat one: no `go` on the run user's PATH (the compile check of generated tests was silently skipped; `go` now symlinked into `/usr/local/bin` and documented), the refusal for a missing standard import, an invented helper name, a function assigned like a variable, a custom type assigned to os.Stdin/Stdout/Stderr, decompose `depends_on` written as Go names, and the test-writer reply limit (6000 to 12000 tokens). On the build side: the revise path now compile-checks a rewritten test and hands the refused output to the next rewrite, and a build failure now carries the contract's declared fields, callee signature and unused imports.

## What still fails
- `fn-parse-float`: 8 attempts. The generated test expects `parseFloat("1.8e+308")` to return `+Inf, true` (and the negative case `-Inf, true`); Go's parser reports a range error there, so a correct implementation fails the test. The model resubmits the same implementation (identical_reply) and its rewritten tests repeat the same boundary assumption. The contract text ("0 and false if s is not a valid float") does not settle overflow.
- `fn-summarize`: 9 attempts, 4 of them compile failures on contract names, then test failures.
- Test-writer calls on `fn-summarize-file` hung 4 times for 530 to 630 seconds with no reply (about 4 hours of waiting in one planning pass).

## Why it is blocked
The failed leaves are terminal in the run state: `brief run` on the same run id does not retry a node skipped at the gate, so another attempt means a new plan (about 40 minutes) and a new run (about 90 minutes), and the remaining failures are local-model quality (wrong boundary test, repeated identical replies), not an engine defect with a general fix.

## Next step for a person
1. Decide the contract text for overflow in `fn-parse-float` (say whether `1.8e+308` is valid), or accept a stronger model for test writing on non-graded runs.
2. Investigate why the mini's test-writer calls hang for 9 to 10 minutes (the provider call timeout is 1500 seconds in `~gmrun/.gophermind/gophermind.yaml`).
3. Plan again from a fresh repo and run; read the result with `brief diagnose`.

## Second attempt, 2026-10-24 (fresh plan `gm-2026-10-24-003`, repo `/home/gmrun/csvstat-run8`)
- Planned after three resumes at the test-writer (each stop was a model reply refused by the compile check; the refusal text is now recorded, see `brief diagnose`). Built: 7 of 10 leaves verified with no failed leaf; the run then stopped with `provider_unavailable` and was resumed once.
- The remaining leaf `fn-summarize-file` (reads a file path, or stdin for `-`) failed 11 attempts. Cline (`cline-pass/qwen3.7-max`) wrote code that failed the same two tests every time (`happy_path_stdin` and `error_summarize_invalid_csv`), then hit its rate limit (`rate_limited`), and the local model's attempts failed to compile 6 times. After that every model was cooling down and the run waited in one-minute steps (280 model calls ended in error).
- Likely cause for the two failing tests: the stdin case can only be tested by swapping `os.Stdin`, which the test writer does not do correctly, so the test expects something a correct implementation cannot give. This is the same class as `fn-parse-float` in the first run: a test-writer error that the build step cannot repair.
- Useful engine follow-ups (not done): when the same tests fail identically for two different providers, treat the tests as suspect and rewrite them before further build attempts; and do not fall back to a model that has already failed to compile the same leaf repeatedly.
