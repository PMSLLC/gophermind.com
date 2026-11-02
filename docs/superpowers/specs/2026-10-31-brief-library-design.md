# Brief library: versioned briefs, an editor, and a run preview

Date: 2026-10-31. Status: draft for John's approval.

## Intent

Today every upload becomes its own folder (`gm-2026-10-29-004`) holding `brief.md`, `run.json` and `run.log`. There are 145 such folders on John's server but only 13 distinct briefs (cliphist, csvstat, hashsum, hookrelay, humandur, ledger-sync, linkcheck, notes-api, retryx, roman, slugify, taskboard, wordfreq). Within a brief the text is identical across folders except the `id:` and `repo:` lines the system stamps in on each run. Nothing links a brief to its edits, and nothing stops an edit from rewriting the text of something that already ran.

John wants a library of briefs, an editor, and a rule that a version that has run can never change. Edits become new versions, and variations of a version are linked. Starting a run goes through a preview screen.

## Goals

1. **Library page.** Every brief family is a heading (largest text on the row). Its versions sit underneath as a tree, each row showing state (draft, running, done, failed, stopped), locked or editable, and an optional label. Search by name. A "New brief" button.
2. **Editor.** Opens any version in a browser text editor. A locked version is shown read-only with a notice that saving makes a new version. Shows the version's parent, children and runs. A never-run draft can be edited and saved in place.
3. **Locking.** A version locks the moment any run is created from it, including runs that later fail or are stopped. Locked means its brief text can never change, enforced by the server, not only the page.
4. **Saving.** Saving from the editor always creates a child of the version that is open. There is no overwrite of a locked version.
5. **Version names, GitHub style dotted numbers.** A new brief starts at `v1`. Saving from the newest version of a line bumps the last number (`v1`, `v1.1`, `v1.2`). Saving from a version that already has a child starts a branch by adding a segment (`v1.1` already has `v1.2`, so a second save from `v1.1` is `v1.1.1`; a branch off `v1.1.1` continues as `v1.1.2`, `v1.1.3`). Later branches off the same version take a letter so names never collide: the first branch off `v1.1` is `v1.1.1`, `v1.1.2`; the second is `v1.1b.1`, `v1.1b.2`; the third is `v1.1c.1`. Names are permanent and never renumbered. A save may carry an optional short label such as "smaller scope".
6. **Run names.** `<family>-<version>-<yyyymmdd>-r<NNN>`, for example `csvstat-v1.1.2-20261029-r004`. The counter counts runs of that version on that date and starts at `r001`. This replaces `gm-YYYY-MM-DD-NNN` for all new runs.
7. **Run preview.** "Run this version" opens a new preview screen, not an immediate run. It shows: name, version and place in the tree (parent, children); the brief rendered as a readable page with the seven required sections (Overview, Features, Architecture, Data, Constraints, Out of scope, Acceptance) as headings; whether the brief passes the section rules, with problems listed; the run settings; the run name it will get. Continue starts the run. Cancel returns to the editor and starts nothing.
8. **Migration of existing runs.** All 145 existing folders get the new names. Each of the 13 families becomes `v1`, and every old folder becomes a run of `v1`, numbered `r001` upward in original order per date. Steps: confirm nothing is running; full backup of `briefs/` and `repos/`; dry run that prints old name next to new name and changes nothing; rename the brief and matching `repos/` folders and rewrite the `id:` and `repo:` lines; write an old-to-new name map so old ids found in logs, chats or links still resolve; verify every run still loads; keep the backup until John says delete it.

## Non-goals

- No change to the planner, executor or how a run behaves once started.
- No new storage engine: versions, links and the locked flag live in browsable files, matching the standing rule that run and node state is files, not SQLite.
- Not the CLI or desktop app in this round.
- No merging of branches.

## Anti-goals (failure even if it works)

- Any byte of a locked version's text ever changes.
- A run starts from the library or editor without passing through the preview.
- The migration loses, overwrites or orphans a run, or leaves a half-renamed state if it stops partway.
- A secret appears in the preview, the editor or the library.

## Constraints

- Repo rules: no em dashes or emojis, subagent-driven plan, commit per task with explicit paths, no push, deploy or install unless John asks.
- Run state stays in the existing folder-per-run layout; only the names and the library layer change.
- The migration is a one-time script with a mandatory dry run, run only on John's say-so.
- Every recursive delete in any script uses the guarded pattern (assign path, check non-empty, absolute and inside the intended directory, print it, `rm -rf -- "${target:?}"`).
- Success is verifiable: unit tests for version-name assignment (chain, branch, deep branch), for the lock (a save onto a locked version is refused at the server), and for run-name counters per version per date; a migration test on a copy of real folders; a manual check in the web page.

## Decided by John

- Any version that has ever run is locked, failed and stopped included.
- An upload whose text is identical to an existing version of the family reuses that version and starts a new run of it; it does not create a new version.
- Second and later branches off the same version are named with a letter (`v1.1b.1`).
- Versions and branches both exist. Names are GitHub-style dotted numbers.
- Run names include the version: `csvstat-v1.1.2-20261029-r004`. The counter is per version per day.
- Existing folders are renamed, not left alone. Organise the existing briefs as makes sense.
- "Run this version" opens a new preview screen with Continue and Cancel. The preview contents are those listed in Goal 7.

## Author's calls (John can overturn any of them)

- The saved version holds only the brief text John wrote. The `id:` and `repo:` stamp belongs to the run and is added when the run starts.
- A family is identified by the brief's `title:`. An upload whose title exists and whose text differs is saved as a new version of that family, as a child of its newest version, never as a new family and never over an old version.
- The upload form and the chat `brief_create` tool create a new family at `v1` when the title is new, and follow the same-title rule above otherwise. Saving versions from chat is out of scope this round; the chat tools only read the library.
- The preview's run settings default to stopping for John at open questions (the chat's default), with a visible toggle.
- A "Compare" action shows a plain text difference between two versions of a family.
- Family names cannot be renamed in this round.

## Settled after the builder check

Facts checked on John's server (`~/.gophermind/local`, the host that holds the 145 folders):

- Each of the 13 titles has exactly one distinct text once the `id:` and `repo:` lines are removed (checked by hash over all 145). The stamp is those two frontmatter lines. The imported `v1` text is the folder text with the two lines removed; the executor still gets a stamped copy when a run starts.
- An old id lives in more places than the brief folder: `briefs/<id>/{brief.md,run.json,run.log}`, `repos/<id>/` (the working copy, which also holds `.gophermind/<id>/brief.md` and `.gophermind/<id>/_state/calls.jsonl` naming the id), and `runs/<id>.json`. The migration must cover all three trees, and the builder must find any further reader of old ids before calling it safe.

Author's calls:

- Lock: the locked flag is written under a per-family lock in the same step that creates the run folder. A save aimed at a version that locked after the editor loaded is refused with a message offering "Save as new version"; it is never turned into an in-place change.
- Migration order and dates: runs are numbered per version per date in order of the old id (date, then old number), and the date comes from the old id. The migration is restartable: it records each rename in a journal, can roll back from it, and the server is stopped while it runs.
- Old-id lookups: a name map on disk plus a lookup in the server's brief routes and the chat tools, so an old id typed or found in a log still resolves.
- Secrets: reuse the existing redaction the chat tools use; library, editor and preview sit behind the existing web auth. No new scanner.
- The "no run without the preview" anti-goal covers the web library and editor. The chat's `brief_create` and `brief_resubmit` already stop at an approval card showing the full text; that card counts as their preview. CLI and desktop stay out of scope.

## Left to the builder

- File layout of the library (for example `<briefs>/<family>/<version>/brief.md` plus a small metadata file for parent, label, created time, locked flag) and how the old-to-new name map is stored.
- How the version tree and the locked flag are computed (stored, or derived from run folders).
- Whether the existing `/app/briefs` page grows into the library or a new page replaces it, and the route names.
- How the existing brief-spec check is reused for the preview's pass/fail.
- Handling of two saves at the same moment from the same parent.
