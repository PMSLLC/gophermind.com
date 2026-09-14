# Installation

## GopherMind's existing flat Markdown loader

The replacement files are in `../packs/`:

- `README.md`
- `code-review.md`
- `diagnosing-bugs.md`
- `gophermind-architecture.md`
- `gophermind-build-test.md`
- `gophermind-phaseflow.md`
- `tdd.md`

Locate the actual directory read by `Skills()` in your checkout. Its filesystem
path was not included in the supplied files, so this bundle does not guess it.
Back up that directory outside the loader's scan tree, inspect the replacement
diff, and copy these seven files into it. Replace the existing README rather than
adding the timestamped upload filename alongside it.

Keep `SKILL.md`, `agents/`, `references/`, `scripts/`, this installation document,
and backups OUTSIDE the auto-injected directory. Otherwise the supporting material
may undo the prompt-size savings or become unintended instructions. Preserve
applicable upstream license notices elsewhere in the repository, including the
bundled `mattpocock-skills-MIT.txt` for the three locally adapted upstream packs.

No YAML frontmatter has been added to the seven GopherMind files. No loader code
change is required by their format. Compatibility is based on the uploaded README's
description, not a runtime test. Determine whether skills are read live or embedded
at build time before deciding whether to reload or rebuild GopherMind.

Smoke-check that the rendered system prompt contains each replacement once, no
old duplicate copies, and none of the supporting documents. Run representative
acceptance checks before relying on the pack for state-changing work.

## Native skill wrapper

The archive also includes a conventional `SKILL.md` and `agents/openai.yaml` wrapper.
That wrapper loads the routing file and relevant workflow on demand in a compatible
skill host. It is packaging metadata, not a request to migrate GopherMind's loader.

## Maintenance validation

From the extracted `gophermind-engineering` directory:

```bash
python3 scripts/validate_pack.py --output references/validation-results.json
```

The full validation uses Python 3.9+, Git, Go, and gofmt in temporary directories.
It does not modify a user repository, run GopherMind, or contact a model provider.
A static-only run is available when these tools are absent:

```bash
python3 scripts/validate_pack.py --static-only
```

Static checks and command fixtures do not establish that an LLM follows the
instructions. Use [behavioral acceptance checks](ACCEPTANCE-CHECKS.md) separately.
