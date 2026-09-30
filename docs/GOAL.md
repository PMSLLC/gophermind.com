# Goal: AI Venture Studio brief, from brief to a working build

Set by John on 2026-09-29. Delegated run: no review gates between tasks, one report at the end.

## Inputs

- Brief: `gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md` (tracked, 750 lines). `~/Downloads/ai-venture-studio-server-brief.md` is an older copy without the `env` block; the bodies are identical.
- Target repo: `~/OtherProjects/AIVentureStudio` (git, branch `main`). It holds a copy of this goal at `docs/GOAL.md`.
- GopherMind source: this repo.
- Spec: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md`.
- Plan: `docs/superpowers/plans/2026-09-29-v2-planner-core.md`.

## Execution rules

- Subagent-driven development. Every subagent runs on Sonnet.
- GopherMind does its own model work on the Mac mini (Ollama `qwen3.6:35b-a3b` at `192.168.1.35:11434`). No cloud model stands in for it.
- The loop: run the brief, read what fell short, update the spec, rebuild GopherMind, run the brief again.
- GopherMind changes go on a feature branch in a worktree of its repo. Commit after each task. Push only when John says so.
- Check the mini's `memory_pressure` and swap before adding load. Never load a second model.
- Tie-breaker for an ambiguous decision: the spec decides; where it is silent, choose what makes a dropped requirement impossible over what is faster, and record the ruling.

## Tasks

1. The brief validates with no warnings (`gophermind brief validate`). The two remaining warnings are scanner false alarms; fix the scanner.
2. Build the v2 planner from the 12-task plan.
3. Run the brief through `gophermind brief plan` on the mini. Fix spec and code for every gap, rebuild, rerun, until `Requirements covered: N of N` with no gap.
4. The v1 `/project` planner on the same brief: close its gaps the same way, or record a ruling that v2 replaces it for this brief.
5. Write the executor spec and plan (handoff build items 9 to 13), then build it.
6. Execute the build in the target repo until it is complete.

## Definition of done

- `gophermind brief validate` on the brief prints no warning.
- The plan GopherMind generates covers every constraint, acceptance bullet, and feature of the brief, shown by its own coverage report.
- In the target repo, `go build ./...` and `go vet ./...` succeed and every acceptance bullet of the brief passes when run.
- The ledger shows which model did each call, by task type.

## Stop and escalate

- The mini's model cannot produce usable contracts or code after the spec and prompts have been revised three times for the same failure. Report the evidence.
- The mini runs out of memory or swap, or Ollama stops answering and a restart through launchd does not bring it back.
- Anything destructive or outward-facing: a push, a release, a deploy, deleting work that is not this run's own.
- A secret the brief declares (`DATABASE_URL` and others) is needed and is not in the vault.
