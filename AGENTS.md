# AGENTS.md: gophermind

Instructions for any coding agent working in this repo. `CLAUDE.md` has the same rules with more detail; `PROJECT.md` has conventions.

## Layout

- `main` checkout: code and docs (the unification branch was merged on 2026-10-20).
- Worktree `../gophermind-planner-core` (branch `feat/briefv2-planner-core`): smoke-fix loop, one writer at a time.
- Packages: `gophermind-lib/build` (planner and executor), `gophermind-lib/runview` (run view), `gophermind-server` (pipeline endpoints), `desktop` and `gophermind-osx` (clients).
- Pipeline contract: `docs/pipeline-contract.md` . Run state is browsable files in the run folder, not SQLite.

## Working agreement

1. Define a verifiable success check first; write the test, then the code.
2. One small step at a time; run it; commit with explicit `git add` paths and a descriptive message.
3. Surgical edits: match surrounding style, comment density and naming; do not refactor neighbors.
4. Keep the repo root minimal: docs in `docs/`, scripts in `scripts/`.
5. Stop and ask before anything outward-facing or hard to reverse: push, merge, tag, deploy, release, delete.

## Verify before you say done

```
go build ./... && go vet ./... && gofmt -l . && go test ./...
```

Report failures with the output; do not claim a pass you did not run.

## Do not

- Use em dashes or emojis in output or new docs.
- Put secrets in tracked files or print credential files.
- Copy files to servers with scp or rsync, or host anything from the laptop.
- Use cloud models in the graded run.
