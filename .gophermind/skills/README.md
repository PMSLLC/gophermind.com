<!-- Not a skill pack: Skills() reads *.md here, so this file is injected too.
     It is deliberately short. -->

# Vendored skill packs

Everything in this directory is concatenated into the system prompt of **every
gophermind turn**, so each file costs tokens on turns that will never use it.
Keep this set small and delete what you do not use.

## Upstream skills (mattpocock/skills)

| file | origin | licence | purpose |
|---|---|---|---|
| `tdd.md` | [mattpocock/skills](https://github.com/mattpocock/skills) | MIT | Test-driven development workflow |
| `code-review.md` | [mattpocock/skills](https://github.com/mattpocock/skills) | MIT | Code review checklist |
| `diagnosing-bugs.md` | [mattpocock/skills](https://github.com/mattpocock/skills) | MIT | Systematic bug diagnosis |

## Project-specific skills (gophermind.com)

| file | purpose |
|---|---|
| `gophermind-build-test.md` | Build, test, verify workflows for gophermind codebase |
| `gophermind-architecture.md` | Architecture reference, code navigation, task-to-file mapping |
| `gophermind-phaseflow.md` | PhaseFlow workflow reference for multi-step projects |

## Design Notes

`humanizer` used to live here and is now the `humanize` **tool** (`internal/tools/humanize.go`), because its guidance is ~7k tokens and almost no coding turn needs it. A tool pays that only when called. Anything here that most turns do not use belongs in a tool for the same reason.

Upstream skills should be edited there, not in this copy. YAML frontmatter is stripped on import because `Skills()` wraps each file in `<skill name="...">` and the frontmatter would arrive as literal noise.
