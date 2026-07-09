# OpenCoven runtime manifest

[`gophermind.json`](gophermind.json) declares GopherMind as a runtime for
[OpenCoven's Coven](https://github.com/OpenCoven/coven-runtimes) — the system
that drives agent CLIs. It is validated against Coven's
`adapter-manifest.schema.json`.

## What GopherMind supports as a Coven runtime

| Capability | Supported | How |
|---|---|---|
| `stream` | ✅ | `--print --input-format stream-json --output-format stream-json` — a Claude-Code-compatible stream-json protocol |
| `preassigned_session_id` | ✅ | `--session-id <id>` (persisted) and `--resume <id>` |
| sandbox mapping | ✅ | `--permission-mode auto` (full) / `plan` (read-only: denies edits & shell) |
| system prompt | ✅ | `--append-system-prompt <text>` |
| model selection | ✅ | `--model <name>` |
| `think` / `speed` | ❌ | GopherMind has no equivalent toggles; declared `false` (honest baseline) |

## Verifying it

```sh
# Schema-validate (any JSON Schema validator):
python3 -c "import json,jsonschema; jsonschema.validate(json.load(open('coven/gophermind.json')), json.load(open('adapter-manifest.schema.json')))"

# Or with Coven's own toolkit, once built (cargo build in coven-runtimes):
conjure validate coven/gophermind.json --verbose
conjure test    coven/gophermind.json        # probes the real `gophermind` on PATH
```

## Submitting it (the "certification")

Acceptance = the manifest merged into `registry/runtimes/` of
[`OpenCoven/coven-runtimes`](https://github.com/OpenCoven/coven-runtimes) per its
`GOVERNANCE.md`. Open a PR there adding this manifest.

> **Note:** As of Coven v0.1, core still uses hardcoded `harness_id == "claude"`
> checks and does not yet read manifest `capabilities` (their planned follow-up
> PRs). So this manifest is validated and ready, but Coven won't drive GopherMind
> in stream mode until that core integration lands.
