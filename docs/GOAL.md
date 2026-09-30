# Goal: AI Venture Studio brief, from brief to a working build and a perfect release

Set by John on 2026-09-29. Delegated run: no review gates between tasks, one report at the end.

## Inputs

- Brief: `gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md` (tracked, 750 lines). `~/Downloads/ai-venture-studio-server-brief.md` is an older copy without the `env` block; the bodies are identical.
- Target repo: `~/OtherProjects/AIVentureStudio` (git, branch `main`). This file is the only copy of the goal; the target repo holds none.
- GopherMind source: this repo.
- Spec: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md`.
- Plan: `docs/superpowers/plans/2026-09-29-v2-planner-core.md`.

## Execution rules

- Subagent-driven development. Every subagent runs on Sonnet.
- GopherMind does its own model work on the Mac mini (Ollama `qwen3.6:35b-a3b` at `192.168.1.35:11434`). No cloud model stands in for it.
- The loop: run the brief, read what fell short, update the spec, rebuild GopherMind, run the brief again.
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
3. Run the brief through `gophermind brief plan` on the mini. Fix spec and code for every gap, rebuild, rerun, until `Requirements covered: N of N` with no gap.
4. The v1 `/project` planner on the same brief: close its gaps the same way, or record a ruling that v2 replaces it for this brief.
5. Write the executor spec and plan (handoff build items 9 to 13), then build it.
6. Execute the build in the target repo until it is complete.
7. Release GopherMind: merge the feature branch to `main` and cut the next version by the procedure in `~/.claude/projects/-Users-jbrahy-OtherProjects-PMSLLC-gophermind-com/memory/gophermind-release-procedure.md`. The run ends when the release is perfect (below).

## Definition of done

- `gophermind brief validate` on the brief prints no warning.
- The plan GopherMind generates covers every constraint, acceptance bullet, and feature of the brief, shown by its own coverage report.
- In the target repo, `go build ./...` and `go vet ./...` succeed and every acceptance bullet of the brief passes when run.
- The ledger shows which model did each call, by task type.

## Perfect release (the run stops here)

The run stops, with its one report, when all of these hold. A release that misses any of them is not perfect: fix it and release a patch version.

- Every Definition of done bullet above holds.
- The push to `main` passed the full gate with no skipped check.
- The tag, the GitHub release, and the Homebrew tap are published; every asset's digest matches the local sha256; notarization shows Accepted.
- `brew upgrade` installs the new version, and that installed binary (not a dev build) gives a clean `gophermind brief validate` and `Requirements covered: N of N` on the brief.
- The mini runs the released version of `gophermind-server`.
- npm publish needs John's OTP. It is not part of the stop condition; the report lists it as the one step left for John.

## Stop and escalate

- The mini's model cannot produce usable contracts or code after the spec and prompts have been revised 20 times for the same failure. Log the evidence.
- The mini runs out of memory or swap, or Ollama stops answering and a restart through launchd does not bring it back.
