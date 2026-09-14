# Instructions for PhaseFlow

- Use the phaseflow skill when the user asks for PhaseFlow or uses a `phase-*` command.
- Treat `/phase-...` or `phase-...` as command invocations and load the matching file from `.github/skills/phase-*`.
- When a command says to spawn a subagent, prefer a matching custom agent from `.github/agents`.
- Do not apply PhaseFlow workflows unless the user explicitly asks for them.
- After completing any `phase-*` command (or any deliverable it triggers: feature, bug fix, tests, docs, etc.), ALWAYS: (1) offer the user the next step by prompting via `ask_user`; repeat this feedback loop until the user explicitly indicates they are done.
