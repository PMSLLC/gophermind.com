# Vendored PhaseFlow prompt assets

These markdown files are vendored from **PhaseFlow** (`phaseflow-cc`),
<https://github.com/jbrahy/metaphaseflow>, and embedded into the gophermind
binary by `internal/phaseflow/assets.go`. They are the prompt artifacts that
drive gophermind's native spec-driven workflow:

- `commands/` — phase slash-command prompts (the loop steps)
- `agents/`   — phase subagent definitions
- `templates/` — workflow document templates (ROADMAP, STATE, PROJECT, …)

**License:** MIT, Copyright (c) 2025 Lex Christopherson. See `LICENSE.upstream`.

Do not edit these files by hand — re-vendor them from upstream to update.
