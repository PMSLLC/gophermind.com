# Editable stage prompts

Date: 2026-11-05. Asked by John ("I want to migrate all of these preambles to be editable", then "go": no questions, my design calls below are marked as such).

## Intent

Every prompt the planner and the executor send to the model starts with a preamble written in a template file compiled into the binary (`gophermind-lib/build/planner/prompts/*.md`, 17 files, and `gophermind-lib/build/packer/prompts/*.md`, 2 files). Changing one word means a rebuild and a reinstall. John wants to edit them in place, next to the run that showed the problem, and have the next call use the new text.

## Goals

- Every prompt template is editable without a rebuild, from the web UI and from a file on disk.
- An edit takes effect on the next model call, including inside a running build.
- A run records which prompt text it started with, so a result can be traced to the prompt.
- Reset to the built-in default is one click.

## Non-goals

- Versions, branches or history of prompts (the brief library does that for briefs; prompts get a file and a hash).
- Per-brief or per-run prompt overrides.
- Editing the data the harness fills into a template (the `{{.Brief}}` and `<brief_section>` content).

## Anti-goals

- A custom prompt that can turn untrusted text into instructions. The planner neutralizes data-section tags it knows from the built-in prompts; a custom prompt must not introduce new ones.
- Tests that silently pick up a developer's local edits.

## Design (my calls)

- **Where the files live:** `GOPHERMIND_PROMPTS_DIR` (the local service sets `~/.gophermind/local/prompts`), with one subdirectory per kind: `planner/<name>.md` and `packer/<name>.md`. The name is the template's file name without `.md`. The directory is seeded at server start with every built-in file that is missing, so the editor shows all 19 from the first visit.
- **How a prompt is read:** a new package `gophermind-lib/build/promptdir` reads `<dir>/<kind>/<name>.md` on every render and falls back to the embedded file. No cache, so an edit is live on the next call. When the env var is unset (tests, the CLI without the service) only the embedded files are used.
- **Validation on save:** the text must parse as a Go `text/template` with `missingkey=error`, and must not close a data-section tag (`</name>`) that the built-in file of the same name does not close. A save that fails is refused with the parse error or the tag name. Over 256 KiB is refused.
- **What a run records:** at the start of a planner run, `_state/prompts.json` lists every prompt of both kinds with `{"kind","name","source":"custom"|"default","sha256"}`. An edit during a run is used by later calls but not re-recorded; the editor page says so.
- **Web UI:** `/app/prompts` lists both kinds with a custom or default badge and the hash; `/app/prompts/{kind}/{name}` is a textarea with Save, Reset to default, and the default text shown below for comparison. JSON under `/app/api/prompts` with the same bearer plus HMAC wrap as the library routes. Links from the library and runs pages.
- **CLI:** `gophermind brief plan` honours the same env var; nothing else changes.

## What is left to the builder

Page markup follows `library_edit.html`. Error envelope follows `briefError`. The seed step and the env line in `scripts/local-service.sh` follow the briefs and repos lines.
