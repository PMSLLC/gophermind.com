# Example briefs

Starting points for `/project`'s optional brief argument. Each file here is a
short, deliberately incomplete project description -- a paragraph or two, not
a full spec. That's the point: pick one, run

```
/project <name> examples/phase-flow/briefs/01-cli-tool.md
```

and the model will ask you questions to fill in the gaps before it writes
`SPEC.md`, `ROADMAP.md`, and `assignments.json` into `.planning/`. Omit the
path and `/project` interviews you from scratch, exactly as before.

There's nothing special about these files' format. `/project` reads whatever
plain-text file you point it at -- these are just a few realistic starting
shapes if you don't already have your own spec or notes to feed it.

- `01-cli-tool.md` -- a small command-line utility
- `02-rest-api.md` -- a small backend service
- `03-menu-bar-app.md` -- a small native macOS utility, in the same spirit
  as gophermind-osx itself

These are the same three briefs gophermind-osx's GUI ships under **File >
Examples** (Pick Brief... button); this copy exists so a CLI/TUI user gets
the same starting points without needing the macOS app.
