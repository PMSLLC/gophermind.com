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
| `humanizer.md` | [blader/humanizer](https://github.com/blader/humanizer) | MIT |

Edit upstream, not here. YAML frontmatter is stripped on import because
`Skills()` wraps each file in `<skill name="...">` and the frontmatter would
arrive as literal noise.
