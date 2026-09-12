<!-- Not a skill pack: Skills() reads *.md here, so this file is injected too.
     It is deliberately short. -->

# Vendored skill packs

Everything in this directory is concatenated into the system prompt of **every
gophermind turn**, so each file costs tokens on turns that will never use it.
Keep this set small and delete what you do not use.

| file | origin | licence |
|---|---|---|
| `tdd.md` | [mattpocock/skills](https://github.com/mattpocock/skills) | MIT |
| `code-review.md` | [mattpocock/skills](https://github.com/mattpocock/skills) | MIT |
| `diagnosing-bugs.md` | [mattpocock/skills](https://github.com/mattpocock/skills) | MIT |

`humanizer` used to live here and is now the `humanize` **tool**
(`internal/tools/humanize.go`), because its guidance is ~7k tokens and almost
no coding turn needs it. A tool pays that only when called. Anything here that
most turns do not use belongs in a tool for the same reason.

Edit upstream, not here. YAML frontmatter is stripped on import because
`Skills()` wraps each file in `<skill name="...">` and the frontmatter would
arrive as literal noise.
