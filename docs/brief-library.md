# Brief library

The web server keeps briefs in a library. A brief family holds versions of the same brief, a version that has run can never change, and every run starts from a preview screen. This page describes what the code does. Design record: `docs/superpowers/specs/2026-10-31-brief-library-design.md`.

The library is part of the brief routes, so it exists only when `GOPHERMIND_BRIEFS_DIR` is set (see `docs/SERVER-SETUP.md`).

## Families and versions

A family is named by the brief's `title:`, turned into a slug (lowercase `a-z`, `0-9` and `-`, at most 60 characters). Its versions are saved briefs:

- A new family starts at `v1`.
- Saving from the newest version of a line bumps the last number: `v1`, `v1.1`, `v1.2`.
- Saving from a version that already has a child starts a branch by adding a segment: a second save from `v1.1` (which already has `v1.2`) is `v1.1.1`, and a save from `v1.1.1` is `v1.1.2`.
- A later branch off the same version takes a letter so names never collide: the first branch off `v1.1` is `v1.1.1`, the second is `v1.1b.1`, the third `v1.1c.1`.
- Names are permanent. They are never renumbered or reused.
- A save can carry a short optional label such as "smaller scope".

A stored version holds only the brief text. The `id:` and `repo:` lines belong to a run and are added when a run starts; any `id:` or `repo:` line in a saved or uploaded brief is dropped.

## The lock

Any version that has ever run is locked forever. A run that failed or was stopped still locks its version. The server enforces this: a save in place onto a locked version is refused with 409 (`this version has run and is locked`), whatever the page shows. A version counts as locked when its lock flag is set or any run record names it. The lock is written when the run folder exists, so a refused start leaves the version a draft.

## Saving

Saving from the editor always makes a child of the version that is open. The one exception is a never-run draft, which can be saved in place (the "in place" save). A locked version opens read-only with a notice that saving makes a new version. The editor shows the version's parent, children and runs, and "Compare with parent" shows a plain line difference.

## Uploads and re-runs

- An upload whose text is byte for byte the same as a stored version of that family reuses that version and starts a new run of it. No new version is made.
- An upload with a title that is new creates the family at `v1`.
- An upload with a known title and changed text becomes a child of the family's newest version. It never makes a new family and never overwrites a version.
- The `id:` and `repo:` lines of an upload are ignored; the server assigns the run id.
- Resubmitting a run (the run page button, `POST /app/briefs/{id}/resubmit`, the chat `brief_resubmit` tool) starts the same version again under a new run name. An older `gm-` run with no library version goes through the upload flow above, which puts its text into the library first.
- A `?resubmit=1` query on an upload is ignored.

## Run names

A run is named `<family>-<version>-<yyyymmdd>-r<NNN>`, for example `csvstat-v1.1.2-20261029-r004`. The day is the UTC date. The counter is per version per day and starts at `r001`: it is one more than the highest `rNNN` already present for that family, version and day. Old `gm-YYYY-MM-DD-NNN` ids still load and still resolve (see Migration).

## The preview

"Run this version" in the editor opens `/app/library/{family}/{version}/preview`. Nothing starts and nothing locks until Continue. The page shows:

- the family, version, parent and children, and whether the version is locked;
- the brief as a readable page: the seven required sections (Overview, Features, Architecture, Data, Constraints, Out of scope, Acceptance) in that order as headings, a missing one shown empty, then any other sections in file order, and any text ahead of the first heading under "Before the first section";
- problems (the brief fails the section rules; Continue is off until they are fixed) and warnings (lint, which never block);
- the run name the run would get now. It is not reserved; the real name comes back when the run starts;
- a "Stop and ask me about open questions" checkbox, on by default.

Continue starts the run with the checkbox value as `ask`. Cancel returns to the editor and starts nothing.

The preview carries a token (the SHA-256 of the stored text). Continue sends it back, and the server refuses with 409 if the text changed since the preview, so the text you previewed is the text that runs.

## Pages and routes

Pages (signed in, in the navigation rail as "Library"):

| Route | Page |
|---|---|
| `GET /app/library` | Families as headings, versions as a tree with state, locked or editable and label; search; "New brief" |
| `GET /app/library/{family}/{version}` | Editor |
| `GET /app/library/{family}/{version}/preview` | Run preview |

JSON API (same auth as the other brief routes):

| Route | Body and reply |
|---|---|
| `GET /app/api/library` | `{"families":[{"name","version_count","last_run_at","versions":[{"name","parent","label","created","locked","state","run_count"}]}]}`. `state` is `draft`, `locked` (locked, no runs) or the phase of the newest run. |
| `GET /app/api/library/{family}/{version}` | `{"family","name","parent","children","label","created","locked","text","runs":[{"id","phase","started_at"}]}`; 404 `no such version`. |
| `POST /app/api/library/{family}/{version}/save` | Send `{"text","label","in_place"}`. Child save: 201 `{"name","locked","created"}`. `in_place` on a draft: 200; on a locked version: 409 with `locked: true`. 413 over 256 KiB. |
| `GET /app/api/library/{family}/{version}/diff?against=<version>` | Plain text diff from `against` to this version; 404 when either is missing. |
| `GET /app/api/library/{family}/{version}/preview` | `{"family","version","parent","children","locked","preview_token","sections":[{"heading","body"}],"problems","warnings","ask_default":true,"run_name"}`. Read only. |
| `POST /app/api/library/{family}/{version}/run` | Send `{"ask":true,"preview_token":"..."}` (`ask` defaults to true). 202 `{"run_id","url"}` plus `warnings`; 400 for a brief with problems; 409 for a changed preview token or when a run is already active. |

The chat tools never edit or save versions. `brief_create` and `brief_resubmit` still start runs (behind the approval card) through the upload and resubmit flow above.

## Where files live

```
<BriefsDir>/library/<family>/<version>/brief.md
<BriefsDir>/library/<family>/<version>/meta.json
<BriefsDir>/library/aliases.json          old run id -> new run id (written by the migration)
```

`meta.json` holds the parent, label, created time and the lock flag. Run folders stay where they were, `<BriefsDir>/<run name>/`.

## Migration

The runs the server made before the library have `gm-YYYY-MM-DD-NNN` ids. The one-time command `gophermind brief migrate-library` renames them, saves one locked `v1` per title (label `imported`), and writes `aliases.json` so old ids still resolve. **It must run before the new library is used on that data.** The real data (`~/.gophermind/local`) has no library yet.

### What it does

For every `gm-` run folder under `--briefs`, the command:

1. reads `brief.md` and `run.json`, takes the title, and makes the family slug;
2. gives the run the name `<family>-v1-<yyyymmdd>-r<NNN>`, using the date in the old id and numbering in old-id order per family and date;
3. backs up `briefs/` (without `library/`), `repos/` and the touched run records to a sibling folder named `library-migration-backup-<UTC timestamp>` next to the briefs dir;
4. saves each family's text (id and repo lines removed) as `v1` and locks it;
5. renames `briefs/<old>`, `repos/<old>`, the run folder `repos/<new>/.gophermind/<old>` and `<runs>/<old>.json`, and rewrites the old id and repo path inside `brief.md`, `run.json`, the run folder's `.json`, `.jsonl` and `brief.md` files (not `logs/`) and the absolute path git stored in `.git/config`, a `.git` file and worktree `gitdir` files;
6. writes `library/aliases.json`, then loads every migrated run the way the server does and prints counts.

Every step is journaled in `<briefs>/library/.migration-journal.jsonl`, so an interrupted `--apply` can be run again to resume, or rolled back.

### It refuses (nothing changes) when

- the server answers `/healthz` at `--server-url` (default `http://127.0.0.1:8090`);
- any run is live (phase `planning`, `running` or `waiting`);
- it is already migrated: the journal says complete, the library already holds versions, or `aliases.json` exists;
- the backup destination already exists, or would sit inside the briefs, repos or runs dir;
- free disk space looks too small for the backup (the size of `briefs/` and `repos/` plus 10% plus 64 MiB);
- a file or folder cannot be copied into the backup (unreadable, or not a file, directory or link);
- the brief text differs between folders of one title once `id:` and `repo:` are removed (the variants are listed);
- a folder has no readable `brief.md`, no title, a title that cannot be a slug, a `run.json` or `id:` that does not name the folder, or a `repo:` that is not `<repos>/<old id>`;
- a new run name is invalid, planned twice, or already exists.

The dry run (no flag) prints every `old -> new`, the families, skipped run records with no briefs folder, and every problem. It changes nothing and exits non-zero when `--apply` would refuse. It does not check the server, the disk space or the backup path; `--apply` does.

### Steps for John

The local server is the launchd agent `com.gophermind.local.server` (`scripts/local-service.sh` has install, status, restart and uninstall but no stop). The paths below are the local install's.

Migrate before the new server runs. Installing the new server and then uploading a brief or resubmitting a run creates library versions, and after that `--apply` refuses with "already migrated". The installed `gophermind` on PATH (`/opt/homebrew/bin/gophermind`) is the old release and has no `migrate-library` subcommand, so build the current binary from this repo and run that.

1. Stop the server so it does not answer `/healthz`: `launchctl bootout "gui/$(id -u)/com.gophermind.local.server"`. Check with `bash scripts/local-service.sh status` (`agent: not loaded`). Wait for any live run to finish first.
2. Build the migration binary from the repo root (the checkout you are about to install) into a fresh temporary folder, and check it has the subcommand:

   ```
   GM_MIG="$(mktemp -d)"
   go build -o "$GM_MIG/gm-migrate" ./cmd/gophermind
   "$GM_MIG/gm-migrate" brief migrate-library --help
   ```

   The help lists `-apply`, `-rollback` and `-force-rollback`. Use `"$GM_MIG/gm-migrate"` for every command below, in the same shell.
3. Make unreadable files readable. The real tree has one: `repos/gm-2026-10-30-054/.gophermind/gm-2026-10-30-054-scratch/.../noaccess`. The backup cannot copy it, so `--apply` refuses until its mode is fixed (`chmod u+r` on that file, and `u+rx` on a directory). The dry run lists every such path.
4. Dry run, and read the output:

   ```
   "$GM_MIG/gm-migrate" brief migrate-library \
     --briefs ~/.gophermind/local/briefs \
     --repos  ~/.gophermind/local/repos \
     --runs   ~/.gophermind/runs
   ```

   It must end with `0 problems`.
5. Apply with the same binary and flags plus `--apply`. It ends with a line like `migrated: families 13, runs migrated 145, skipped 0, errors 0; backup kept at ...`. If it stops partway, run the same command again to resume.
6. Only now install the new build and start the server: `bash scripts/local-service.sh install` (it rebuilds, loads the agent and waits for `/healthz`).
7. Open `http://127.0.0.1:8090/app/library` and check: each family has a locked `v1` labelled `imported`, and an old link such as `/app/briefs/gm-2026-10-29-007` still opens its run.
8. Keep the `library-migration-backup-*` folder until you are satisfied. The command never deletes it.

### Rollback

`"$GM_MIG/gm-migrate" brief migrate-library --briefs ... --repos ... --runs ... --rollback` (server stopped; the binary built in step 2, since the installed release may lack the subcommand) undoes the journal in reverse: it renames folders back, restores each rewritten file from the backup, removes the library entries the migration made and the journal, then compares the briefs, repos and touched run records with the backup and lists any path that differs (in which case the journal is kept).

Rollback refuses, changing nothing, when there is later work it would lose or leave behind: a file the migration wrote has since changed, the library holds a version other than the migrated `v1`, a `v1` is no longer locked or has other text, or a migrated family has a run folder the migration did not make. It prints each as `later work: ...`.

`--force-rollback` goes ahead anyway. It **destroys** library versions saved since, edits made since to files the migration wrote, and `v1` lock changes. Later run folders are left in place. Use it only when that loss is intended.

### Old ids

`library/aliases.json` maps each old id to its new name. The server resolves an old id in the run page and state, the events stream, answer, stop and resubmit routes, the chat tools and brief links. `ValidRunID` still accepts both id formats.

### Not renamed

Go module paths, git branch names, the `logs/` folders of runs, scratch directories, and any text in non-JSON files of a run folder keep their old content. The migration rewrites only the paths and ids listed in step 5 of "What it does".

Two names tied to the old id stay as they were: the git work branch `gm/<old id>` inside each run's repo, and the vault secret keys `run/<old id>`. A migrated run can be viewed and resubmitted, but a CLI resume (`gophermind brief resume`) or a clear-state (`scripts/clear-project-state.sh`) of a migrated failed run will not find its branch or secrets under the new id. Resubmit it instead.
