---
name: gophermind-engineering
description: Apply the GopherMind engineering workflows for code review, bug diagnosis, test-driven development, architecture navigation, build verification, and explicitly requested PhaseFlow operations. Use when working in a GopherMind checkout or when installing or maintaining these GopherMind prompt packs. Preserve the custom flat Markdown loader format when deploying the bundled files.
---
# GopherMind Engineering

Read [shared routing and rules](packs/README.md), then only the relevant workflow:

- [Code review](packs/code-review.md)
- [Bug diagnosis](packs/diagnosing-bugs.md)
- [Test-driven development](packs/tdd.md)
- [Architecture navigation](packs/gophermind-architecture.md)
- [Build and test](packs/gophermind-build-test.md)
- [PhaseFlow](packs/gophermind-phaseflow.md)

Apply the workflow to the actual checkout. Do not assume the source tree, commands,
permissions, or capabilities described in the packs have been verified there.

For installation, read [installation notes](references/INSTALL.md). The seven files
in `packs/` are the GopherMind replacements; keep this wrapper and supporting files
outside the custom auto-injected directory. Do not modify the user's checkout
unless installation is requested and the loader directory is established.

For the rationale and intentional policy changes, read the
[review report](references/REVIEW.md). For maintenance, run
`python3 scripts/validate_pack.py` from this skill directory, and use the
[behavioral acceptance checks](references/ACCEPTANCE-CHECKS.md).
