# hello fixture: run hello-v1-20261105-r001

Origin: the local service on build 1e26c90 (Milestone 1 Tasks 1 to 3 not yet installed; the installed binary was 0932dc1), 2026-11-05 about 10:50 MST, model gemma-4-26b-a4b on the mini. The brief is `examples/briefs/14-hello.md` with `repo:` replaced by `REPO_DIR` and the run id kept.

The run ended `escalated (human_stop)`: fn-main failed implement 12 calls, then revise flagged the contract. Acceptance passed 0 of 6.

What the replies show (the defects `docs/superpowers/specs/2026-11-05-hello-pipeline-fixes-brief.md` names):

- The brief fixes `Greeting(name string) string` in `internal/greet`; the contract has `FormatGreeting` and `WriteGreeting` instead.
- The brief fixes `Run(args []string, stdout, stderr io.Writer) int`; the contract's `Run` gained `stdin io.Reader`, which the brief puts out of scope.
- Five private helpers (`validateArgs`, `formatUsageError`, `writeUsageError`, `FormatGreeting`, `WriteGreeting`) for a three-function program.
- `fn-run` and `fn-main` were emitted twice (`outline_duplicate_ignored`), the first kept.
- `main` must call `os.Exit`, which the executor's source policy forbids, so every implement attempt was rejected.

Files: one `.txt` per recorded planner reply, named `<stage with : as .>.txt`, with `.2.txt` and so on for repeats, as `planner.FixtureProvider` expects. Written by `scripts/extract-llm-fixtures.py`. A recorded file is never changed. A corrected reply a later test needs goes in the next `.N.txt` slot or under the stage name the fixed planner asks for, and is listed here with its reason.

Corrected replies: none yet.
