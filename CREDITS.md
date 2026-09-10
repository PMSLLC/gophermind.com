# Credits & Acknowledgments

GopherMind stands on the shoulders of others. Thank you.

## Fortune database

The quotes shown under the startup banner come from the **Fortune Cookie
Database** collected by **Brian M. Clapper** —
<https://github.com/bmc/fortunes>. The database is used under the
**Creative Commons Attribution 4.0 International License**
(<https://creativecommons.org/licenses/by/4.0/>); the text is redistributed
unmodified. Thank you, Brian.

## PhaseFlow

GopherMind's native spec-driven workflow (`gophermind phase …` / `/phase`) is a
port of **PhaseFlow** by **Lex Christopherson** —
<https://github.com/jbrahy/metaphaseflow>. Its phase commands, subagent
definitions, and workflow templates are vendored and embedded under
`internal/phaseflow/assets/`, used under the **MIT License** (Copyright © 2025
Lex Christopherson; see `internal/phaseflow/assets/LICENSE.upstream`). Thank you.

## Free LLM provider registry

The free-provider table behind `gophermind free` and
[docs/free-providers.md](docs/free-providers.md) is vendored from
**awesome-free-llm-apis** by **mnfst** —
<https://github.com/mnfst/awesome-free-llm-apis>. The registry is used under
**CC0 1.0** (<https://creativecommons.org/publicdomain/zero/1.0/>) and vendored
verbatim at `internal/freellm/data.json`; refresh it with
`scripts/sync-free-providers.sh`. Thank you.

## Libraries

GopherMind's terminal experience is built on the excellent
[Charm](https://charm.sh) stack:

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — the TUI runtime
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — styling
- [Bubbles](https://github.com/charmbracelet/bubbles) — components
- [Glamour](https://github.com/charmbracelet/glamour) — Markdown rendering

Releases are cut with [GoReleaser](https://goreleaser.com).

## Contributors

Everyone who opens an issue, sends a PR, or improves the docs. See the
[contributor graph](https://github.com/jbrahy/gophermind.com/graphs/contributors)
and [CONTRIBUTING.md](CONTRIBUTING.md).
