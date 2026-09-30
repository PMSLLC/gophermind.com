# Goal: AI Venture Studio, built from the brief in one clean GopherMind run

Set by John on 2026-09-29. Delegated run: no review gates between tasks, one report at the end.

## Inputs

- Brief: `gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md` (tracked, 750 lines). `~/Downloads/ai-venture-studio-server-brief.md` is an older copy without the `env` block; the bodies are identical.
- Target repo: `~/OtherProjects/AIVentureStudio` (git, branch `main`). This file is the only copy of the goal; the target repo holds none. Its empty starting state is the tag `goal-baseline` (`a919593`). It has no remote.
- GopherMind source: this repo.
- Spec: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md`.
- Plan: `docs/superpowers/plans/2026-09-29-v2-planner-core.md`.

## Execution rules

- Subagent-driven development. Every subagent runs on Sonnet.
- GopherMind does its own model work on the Mac mini (Ollama `qwen3.6:35b-a3b` at `192.168.1.35:11434`). No cloud model stands in for it.
- The release is AIVentureStudio, not GopherMind. GopherMind is the tool that has to get good enough to build it.
- GopherMind builds AIVentureStudio. Claude never edits, fixes, or commits code in the target repo; a bug there is fixed by fixing GopherMind and running again.
- GopherMind changes go on a feature branch in a worktree of its repo. Commit and push after each task.
- Check the mini's `memory_pressure` and swap before adding load. Never load a second model.
- Tie-breaker for an ambiguous decision: the spec decides; where it is silent, choose what makes a dropped requirement impossible over what is faster, and record the ruling.

## Claude token budget

Claude tokens are the cost to keep down. The mini's tokens are free; spend those instead wherever the work allows.

- The orchestrator does not read the plan (13,956 lines), the spec, or the brief in full. It finds a task's line range with `grep -n '^## Task'` and hands the subagent the path and the range.
- One subagent per plan task. Its brief names paths and line ranges; it never pastes file contents.
- A subagent's report is 150 words or fewer: the commit hash, the test command and its pass or fail line, and any ruling it made. No diffs, no logs, no restated plan text.
- Test, build, and GopherMind output goes to a file under the scratchpad. Read it with `grep` or `tail` for the failing lines; never print a whole log into context.
- The orchestrator does not repeat a check a subagent already ran and reported. It runs the task's one verify command and moves on.
- A review reads the task's diff and its Review Focus lines, nothing else. One reviewer per task.
- No agent is spawned for something one command answers.
- When a rerun of the brief fails, only the failing stage's ledger rows and output are read, not the whole run.
- Compact at task boundaries, after the commit.

## Tasks

1. The brief validates with no warnings (`gophermind brief validate`). The two remaining warnings are scanner false alarms; fix the scanner.
2. Build the v2 planner from the 12-task plan.
3. Write the executor spec and plan (handoff build items 9 to 13), then build it.
4. Make `/project` the single entry point: given the brief, it plans and then builds with no second command and no human step. If the v2 planner and executor do the work, `/project` drives them; record the ruling on what happens to the v1 planner.
5. Run the clean-run loop (below) until a run is perfect.

## The clean-run loop

One attempt is one `/project` run on the brief in the target repo, from the cleared state to the finished build, in a single process with no restart, no resume, and no hand edit.

1. Clear the target repo (see "Clearing").
2. Run `/project` with the brief. Send all output to a log file under the scratchpad.
3. When it ends, run the checks in "Perfect release".
4. If everything passes, stop: that is the release.
5. If anything fails, that attempt is dead. Do not patch it and do not resume it.
   1. Determine what happened: the first error, the stage and task it came from, the ledger rows for it, and the cause in GopherMind (spec, prompt, or code).
   2. Write a record to `docs/goals/attempts/NN.md` in this repo: what failed, the cause, the fix. Twenty lines at most.
   3. Clear the target repo.
   4. Fix GopherMind here: update the spec if the spec was wrong, then the code, with a test that reproduces the failure. Commit and push on the feature branch. Rebuild and install the binary.
   5. Go to step 2.

An attempt also fails, even if the build looks right, when any of these happened during it:

- GopherMind printed an error, exited non-zero, skipped or abandoned a task, or stopped to ask a human.
- The coverage report is not `Requirements covered: N of N`.
- The run was restarted or resumed partway.

A model call that GopherMind retried by itself and then completed is not an error, but the ledger must show it.

### Clearing

Clearing removes all work and all plan state from `~/OtherProjects/AIVentureStudio`:

```
git -C ~/OtherProjects/AIVentureStudio reset --hard goal-baseline
git -C ~/OtherProjects/AIVentureStudio clean -fdx -e .remember
```

Also remove any plan, run directory, or database GopherMind keeps for this project outside the repo (under `~/.gophermind/`), so the next attempt starts with nothing carried over. Check that `git status` is clean and `HEAD` is `goal-baseline` before starting the next attempt.

## Perfect release (the run stops here)

The run stops, with its one report, when a single attempt gives all of these:

- `/project` went from the brief to the end with no error, by the rules above.
- `gophermind brief validate` on the brief prints no warning.
- The plan covers every constraint, acceptance bullet, and feature of the brief, shown by GopherMind's own coverage report.
- In the target repo, `go build ./...`, `go vet ./...`, and `go test -race ./...` succeed.
- Every acceptance bullet of the brief passes when run against the built server. A bug found here fails the attempt.
- The ledger shows which model did each call, by task type.
- GopherMind committed the result on `main` in the target repo. The orchestrator then tags that commit `v0.1.0`; the tag is the only thing Claude adds there.

## Stop and escalate

- The mini's model cannot produce usable contracts or code after the spec and prompts have been revised 20 times for the same failure. Log the evidence.
- The mini runs out of memory or swap, or Ollama stops answering and a restart through launchd does not bring it back.
