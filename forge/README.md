# Forge

Forge is a local Go-first coding agent that uses Ollama like a Claude Code-style workflow.

It can:
- inspect a repository
- summarize the repo
- plan a change
- search files with ripgrep
- read and write files
- apply exact block replacements
- run safe shell commands
- iterate on failures

## Requirements

- Go 1.24+
- Ollama running locally
- A pulled Ollama model, for example:

```bash
ollama pull gpt-oss:20b
```

## Quick start

From the project root:

```bash
go build -o forge ./cmd/forge
```

Run against another repo:

```bash
./forge --root /path/to/repo ask "Explain this repository"
./forge --root /path/to/repo plan "Add Stripe webhook support"
./forge --root /path/to/repo edit "Refactor auth middleware"
./forge --root /path/to/repo test
./forge --root /path/to/repo fix
```

You can also target the current directory:

```bash
./forge ask "How is auth handled?"
```

## Environment variables

- `FORGE_MODEL` default: `gpt-oss:20b`
- `FORGE_OLLAMA_BASE_URL` default: `http://localhost:11434`
- `FORGE_APPROVAL_MODE` default: `suggest`
- `FORGE_MAX_ITERATIONS` default: `3`
- `FORGE_LOG_LEVEL` default: `info`

## Approval modes

- `suggest` - generate plans and diffs but do not write files automatically
- `auto` - write files automatically and run safe commands

## Safe command policy

Forge allows safe commands like:
- `go test ./...`
- `go build ./...`
- `gofmt -w ...`
- `goimports -w ...`
- `rg ...`

It blocks obviously dangerous commands like:
- `rm -rf`
- `sudo`
- `git reset --hard`
- `git clean -fd`
- shell redirection that can overwrite arbitrary files

## Notes

This is a practical scaffold, not a finished Claude Code clone. It is designed to be easy to extend.

## Suggested next upgrades

- add TUI
- add AST-aware Go refactors
- add git commit message generation
- add context ranking and chunking
- add JSON schema repair retries
