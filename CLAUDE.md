# CLAUDE.md: gophermind

Read `PROJECT.md` and `CONTEXT.md` first. Global rules live in `~/.claude/CLAUDE.md`; this file adds what is specific to this repo.

## Where things live

- This checkout (`main`) holds the code and the docs. The unification branch was merged into `main` on 2026-10-20 (merge commits d869c51, a647ceb) and pushed; the remote branch is deleted.
- The linked worktree `../gophermind-planner-core` keeps the local planner-core branch (same code; `git -C ../gophermind-planner-core branch --show-current` names it) for the smoke-fix loop. One writer at a time; work merges back to `main` at checkpoints.
- Map of the unification work: `docs/unification-overview.md`. Goal: `docs/GOAL-UNIFICATION.md`. Run report: `docs/goals/unification/report.md`. Graded run: `docs/GOAL.md`.

## Build and test (from a module root, in this checkout or the worktree)

```
go build ./... && go vet ./... && gofmt -l .
go test ./...            # add -race for server, osx client, executor
```

Modules: `gophermind-lib`, `gophermind-server`, `desktop`, `gophermind-osx`, plus `cmd` and `gophermind`.

## Rules that bite here

- No em dashes, no emojis in anything written.
- Plans run subagent-driven: fresh subagent per task, spec and quality review, commit per task with explicit `git add` paths.
- Never push, merge, tag a release, deploy to the mini, or install the app unless John asks or an active goal document (`docs/GOAL-FINISH.md`) authorizes it. The git wrapper blocks `push`, `checkout`, `switch`, `stash`, `reset`, `clean`, `restore`, `branch`, `worktree` without `--gw-force`; never force without asking.
- Deploy by git only (`git fetch` + `git checkout <ref>` on the target). Services run on the Mac mini (`ssh home`), never the laptop. AWS is deprecated.
- Never print `~/.claude/private/*` or `credentials.md`. Secrets stay out of tracked files.
- Guard every recursive delete: assign the path, check it is non-empty, absolute and inside the intended dir, print it, then `rm -rf -- "${target:?}"`.
- The graded run uses only the mini's local model. No cloud models in it.
