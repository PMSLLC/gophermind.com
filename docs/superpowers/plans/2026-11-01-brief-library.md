# Brief library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A versioned brief library with an editor, a lock on any version that has run, dotted version names with branches, new run names (`csvstat-v1.1.2-20261029-r004`), a run preview screen, and a one-time migration of the 145 existing run folders.

**Architecture:** A new package `gophermind-lib/library` owns version naming and a file store (`<BriefsDir>/library/<family>/<version>/{brief.md,meta.json}`). `serve/briefs.go` starts runs from a stored version, stamping `id:` and `repo:` into a copy at run start. Run ids gain a second accepted shape in the one shared validator. Pages and JSON routes sit next to the existing brief routes. Migration is a restartable command with a dry run.

**Tech Stack:** Go (`gophermind-lib/library`, `serve`, `build/planner`, `build/rundir`, `cmd/gophermind`), plain HTML pages in `serve/assets`.

**Spec:** `docs/superpowers/specs/2026-10-31-brief-library-design.md` (approved by John 2026-11-01).

## Global Constraints

- No em dashes or emojis in anything written.
- A locked version's `brief.md` bytes never change. The lock is enforced in `library.Store`, not the page.
- Version names: `v1`; first child of `v1` is `v1.1`; first child of a multi-segment name bumps the last segment (`v1.1` to `v1.2`, `v1.1b.2` to `v1.1b.3`); a later child takes the first unused of `parent.1`, `parentb.1`, `parentc.1` and so on (letters `b` to `z`). Names are never renumbered or reused.
- Run id shape (new): `<family>-<version>-<yyyymmdd>-r<NNN>` (counter per family, version and date, at least 3 digits). The old shape `gm-YYYY-MM-DD-NNN` stays valid so unmigrated and rolled-back runs keep loading. Both are accepted in one place (`planner.ValidRunID`) and every other check delegates to it.
- The saved version text has no `id:` or `repo:` line; the run copy is stamped when the run starts.
- Secrets: reuse the existing chat-tool redaction (`chatTools.clean` style) for anything the preview shows from run data; no new scanner.
- Recursive deletes in any script or test helper use the guard: assign path, check non-empty and absolute and inside the intended directory, print it, `rm -rf -- "${target:?}"`.
- Commit per task with explicit `git add` paths; no push, deploy, install or migration run on John's data until John asks. From `gophermind-lib`: `go build ./... && go vet ./... && gofmt -l . && go test -race ./library ./serve ./build/planner ./build/rundir ./runview` (skip full `go test ./...`: `build/executor` hangs here). From `cmd`/`gophermind`: `go build ./... && go test ./...`.

## Review Focus

1. A save aimed at a version that locked after the editor loaded is refused, and two saves from the same parent at the same moment get different names (no overwrite).
2. A title or family such as `../x`, `A B/C`, unicode, or 300 characters produces a safe slug that cannot leave the library directory; empty title is rejected.
3. Old `gm-...` ids and new ids both load in the run pages, chat tools and `runs_digest`; `..`, `/`, uppercase and trailing garbage in an id are rejected.
4. Migration stopped halfway then run again finishes or rolls back; no run folder is lost, duplicated or left with the wrong `id:`; a dry run changes nothing.
5. Brief text over 256 KiB, CRLF line endings and a brief with no trailing newline survive save, reuse detection (identical text) and the preview.

## File Structure

- `gophermind-lib/library/names.go`, `store.go` (create): version naming, slugs, file store, lock.
- `gophermind-lib/build/planner/load.go`, `build/rundir/rundir.go`, `build/planner/state.go`, `build/projectrun/statepaths.go`, `cmd/gophermind/brief_diagnose.go`, `serve/chattools_read.go` (modify): run id shape.
- `gophermind-lib/serve/briefs.go`, `webhook.go` (modify); `serve/library.go`, `serve/library_preview.go` (create): run start from a version, routes, preview.
- `gophermind-lib/serve/assets/library.html`, `library_edit.html`, `library_preview.html` (create); `briefs.html` (modify: link to library).
- `cmd/gophermind/brief_migrate_library.go` (create): migration command.
- `CHANGELOG.md`, `docs/` (modify).

---

### Task 1: Accept the new run id shape

**Files:**
- Modify: `gophermind-lib/build/planner/load.go:18-21`, `build/rundir/rundir.go:18`, `build/planner/state.go:68`, `build/projectrun/statepaths.go:25`, `cmd/gophermind/brief_diagnose.go:46`, `serve/chattools_read.go:323`, `serve/briefs.go:704`
- Test: `gophermind-lib/build/planner/load_test.go`, `build/rundir/rundir_test.go`, `serve/chattools_read_test.go`

**Interfaces:**
- Produces: `planner.ValidRunID(s string) bool` true for both shapes; `rundir` uses `planner.ValidRunID` semantics (or an equal shared func); messages say "want <family>-<version>-<yyyymmdd>-rNNN or gm-YYYY-MM-DD-NNN".

- [ ] **Step 1: Write failing tests** `TestValidRunID_NewShape` accepting `csvstat-v1-20261029-r001`, `csvstat-v1.1.2-20261029-r004`, `csvstat-v1.1b.2-20261029-r1000`, `ledger-sync-v1-20261029-r001`, `gm-2026-10-29-004`; rejecting `csvstat-v1-20261029`, `Csvstat-v1-20261029-r001`, `a/../b-v1-20261029-r001`, `csvstat-v1..1-20261029-r001`, `csvstat-v1-20261029-r001/`, a 130-character id, empty string. Same table through `rundir` and through the `runIDInReasonRE` match in `chattools_read` (a failure reason naming a new id is grouped like an old one).
- [ ] **Step 2: Run to verify fail:** `go test ./build/planner ./build/rundir ./serve -run 'RunID|Digest' -v`; expect FAIL.
- [ ] **Step 3: Implement** the regex `^[a-z0-9]+(?:-[a-z0-9]+)*-v[0-9]+[a-z]?(?:\.[0-9]+[a-z]?)*-[0-9]{8}-r[0-9]{3,}$` OR the old one, max 120 characters, in `planner.ValidRunID`; make `rundir`, `statepaths`, `state.go`, `brief_diagnose` and `chattools_read` call it; fix the two messages.
- [ ] **Step 4: Verify pass** with the Global Constraints test command, plus `go test ./runview ./build/...` for packages that import it (skip `build/executor`).
- [ ] **Step 5: Commit** `git add` the files above; message `feat(runid): accept <family>-<version>-<date>-rNNN beside gm-YYYY-MM-DD-NNN`.

### Task 2: Version names and slugs

**Files:**
- Create: `gophermind-lib/library/names.go`, `gophermind-lib/library/names_test.go`

**Interfaces:**
- Produces: `func NextName(parent string, all []string, children []string) (string, error)` (`all` is every version name in the family, `children` those whose parent is `parent`); `func Slug(title string) (string, error)`; `func RunName(family, version string, day time.Time, n int) string`.

- [ ] **Step 1: Write failing tests** `TestNextName` with this table (`parent`, `children` to expected): (`""`, none) `v1`; (`v1`, none) `v1.1`; (`v1.1`, none) `v1.2`; (`v1.1`, `[v1.2]`) `v1.1.1`; (`v1.1`, `[v1.2 v1.1.1]`) `v1.1b.1`; (`v1.1`, `[v1.2 v1.1.1 v1.1b.1]`) `v1.1c.1`; (`v1.1.1`, none) `v1.1.2`; (`v1`, `[v1.1]`) `v1b.1`; (`v1.1b.2`, none) `v1.1b.3`; letters exhausted returns an error. `TestSlug`: `csvstat` stays, `AI Venture Studio` to `ai-venture-studio`, `../x` and `///` and `""` error or reduce to a safe non-empty slug, 300 characters cut to at most 60 without a trailing `-`, unicode letters dropped. `TestRunName`: `RunName("csvstat","v1.1.2",2026-10-29,4)` is `csvstat-v1.1.2-20261029-r004`; `n=1000` gives `r1000`; every result passes `planner.ValidRunID`.
- [ ] **Step 2: Run to verify fail:** `go test ./library`.
- [ ] **Step 3: Implement** the three functions per the Global Constraints naming rule (first unused candidate).
- [ ] **Step 4: Verify pass:** `go test -race ./library`.
- [ ] **Step 5: Commit** `feat(library): version names, slugs and run names`.

### Task 3: File store with the lock

**Files:**
- Create: `gophermind-lib/library/store.go`, `gophermind-lib/library/store_test.go`

**Interfaces:**
- Consumes: Task 2 functions.
- Produces: `type Version struct{ Family, Name, Parent, Label, Created string; Locked bool }`; `func Open(dir string) (*Store, error)`; `func (s *Store) Save(family, parent, label string, text []byte) (Version, error)` (parent `""` is allowed only for a family with no versions; saving a draft in place is `Update`); `func (s *Store) Update(family, name string, text []byte) error` (draft only, else `ErrLocked`); `func (s *Store) Get(family, name string) (Version, []byte, error)`; `func (s *Store) List() ([]Family, error)` (each `Family{Name string; Versions []Version}` in tree order); `func (s *Store) Lock(family, name string) error`; `func (s *Store) FindIdentical(family string, text []byte) (Version, bool)`; `var ErrLocked, ErrNoSuchVersion`.
- Layout: `<dir>/<family>/<version>/brief.md` and `meta.json`; writes go to a temp file then rename; one mutex per family.

- [ ] **Step 1: Write failing tests** in a `t.TempDir()`: `TestSaveChainAndBranch` (v1, v1.1, v1.2, then branch v1.1.1 from v1.1); `TestLockedRefusesUpdate` (after `Lock`, `Update` returns `ErrLocked` and the file bytes are unchanged); `TestConcurrentSavesSameParent` (20 goroutines save from `v1`, all names distinct, none lost, run with `-race`); `TestFindIdentical` (CRLF versus LF counts as different, trailing newline difference counts as different, exact bytes match); `TestUnsafeFamilyRefused` (`../x` is rejected, no directory created outside `dir`); `TestOversize` (over 256 KiB refused).
- [ ] **Step 2: Run to verify fail:** `go test ./library`.
- [ ] **Step 3: Implement** `Store` per the Interfaces block; `Lock` writes `locked_at` into `meta.json` under the family mutex.
- [ ] **Step 4: Verify pass:** `go test -race ./library`.
- [ ] **Step 5: Commit** `feat(library): file store with locked versions`.

### Task 4: Start a run from a version

**Files:**
- Modify: `gophermind-lib/serve/briefs.go` (`briefRecord` gains `Family`, `Version`; `start`, `freshID` and `withFreshID` are kept for old-shape callers), `serve/webhook.go` (build the `library.Store` next to `briefManager`)
- Create: `gophermind-lib/serve/library.go`
- Test: `serve/library_test.go`, `serve/briefs_test.go`

**Interfaces:**
- Consumes: `library.Store`, `library.RunName`, existing `m.start`.
- Produces: `func (m *briefManager) startVersion(family, version string, ask bool) (briefRecord, []brief.Problem, error)`: reads the text, finds the next counter by listing run folders for that family, version and date, stamps `id:` and `repo:` into a copy, locks the version, then calls `m.start`; `briefRecord` JSON gains `family` and `version` (omitted when empty). Lock and run-folder creation happen under the family mutex so a lock never exists without its run.

- [ ] **Step 1: Write failing tests** `TestStartVersionNamesRun` (run id is `csvstat-v1-<today>-r001`, second start of the same version after the first finishes is `r002`, a different version restarts at `r001`); `TestStartVersionLocks` (the version is locked afterwards and `Update` returns `ErrLocked`); `TestStartVersionBusyDoesNotLock` (when a run is active the call returns `*briefBusyError` and the version stays unlocked); `TestStartedBriefHasStamp` (the run copy has the new `id:` and `repo:` lines, the stored version text does not).
- [ ] **Step 2: Run to verify fail:** `go test ./serve -run StartVersion`.
- [ ] **Step 3: Implement** `startVersion`; counter by scanning `m.dir` entries with the run-name prefix and date.
- [ ] **Step 4: Verify pass** with the Global Constraints command.
- [ ] **Step 5: Commit** `feat(serve): start a run from a library version`.

### Task 5: Upload and chat create go through the library

**Files:**
- Modify: `serve/briefs.go` (`createTextOpt`), `serve/chattools_write.go` (`brief_create`, `brief_resubmit`), `serve/briefanswer.go` only if it reads the run id shape
- Test: `serve/briefs_test.go`, `serve/chattools_write_test.go`

**Interfaces:**
- Produces: `createTextOpt` strips `id:` and `repo:`, derives the family slug from `title:`, then: new family saves `v1`; an identical stored version is reused (no new version); different text on an existing family is saved as a child of the newest version; then `startVersion`. `POST /app/briefs` still answers `202 {run_id, url}`. `brief_resubmit` starts the same version again (new run, same version, no new text).

- [ ] **Step 1: Write failing tests** `TestUploadNewTitleMakesV1`, `TestUploadIdenticalReusesVersion` (two uploads, one version, runs `r001` and `r002` once the first is done), `TestUploadChangedTextMakesChild` (second upload with one edited word makes `v1.1`, `v1` stays unchanged), `TestUploadIgnoresClientID` (a pasted `id: gm-...` line does not become the run id). Update the existing create/resubmit tests that assert `gm-YYYY-MM-DD-NNN` ids.
- [ ] **Step 2: Run to verify fail:** `go test ./serve -run 'Upload|Create|Resubmit'`.
- [ ] **Step 3: Implement** the flow above; keep `brief.Check` on the stamped text so invalid briefs are refused as today.
- [ ] **Step 4: Verify pass** with the Global Constraints command.
- [ ] **Step 5: Commit** `feat(serve): uploads and chat create use the brief library`.

### Task 6: Library and editor routes and pages

**Files:**
- Modify: `serve/webhook.go` (routes behind the same auth wrapper as the brief routes), `serve/assets/briefs.html` (link)
- Create: `serve/library_http.go`, `serve/assets/library.html`, `serve/assets/library_edit.html`
- Test: `serve/library_http_test.go`, `serve/pages_test.go`

**Interfaces:**
- Routes: `GET /app/library` (page), `GET /app/library/{family}/{version}` (editor page), `GET /app/api/library` (JSON: families, versions, state, locked, label, runs), `GET /app/api/library/{family}/{version}` (version JSON with text, parent, children, runs), `POST /app/api/library/{family}/{version}/save` body `{text, label}` (locked or has runs: new child; draft with no runs: `Update` only when `in_place: true`), `GET /app/api/library/{family}/{version}/diff?against={version}` (plain unified text diff).
- Pages: family name 22px bold heading; counts and last run in small grey text; versions as a left-ruled monospace tree; a locked notice; the Run button links to the preview page only and never posts a run.

- [ ] **Step 1: Write failing tests** `TestLibraryListJSON`, `TestSaveLockedMakesChild`, `TestSaveDraftInPlace`, `TestSaveStaleLockRefused` (version locked between load and save returns 409 with a message offering a new version), `TestDiffPlain`, `TestLibraryPagesServed`, `TestEditorRunButtonOnlyLinksToPreview` (no `fetch` or form post to a run URL in `library_edit.html`).
- [ ] **Step 2: Run to verify fail:** `go test ./serve -run 'Library|Editor'`.
- [ ] **Step 3: Implement** routes, handlers and the two pages in the style of `briefs.html` / `brief_new.html` (plain JS, no framework).
- [ ] **Step 4: Verify pass** with the Global Constraints command; open `/app/library` on a throwaway server on another port and check both pages by eye.
- [ ] **Step 5: Commit** `feat(serve): brief library and editor pages`.

### Task 7: Run preview

**Files:**
- Create: `serve/library_preview.go`, `serve/assets/library_preview.html`
- Modify: `serve/webhook.go`
- Test: `serve/library_preview_test.go`

**Interfaces:**
- Routes: `GET /app/library/{family}/{version}/preview` (page), `GET /app/api/library/{family}/{version}/preview` (JSON `{family, version, parent, children, sections:[{heading, body}], problems, ask_default, run_name}`), `POST /app/api/library/{family}/{version}/run` body `{ask}` answers `202 {run_id, url}` via `startVersion`.
- The sections are the seven required headings from `assets/brief-spec.md`; `problems` is `brief.Check` on the stamped text with the provisional run name. Continue posts the run and goes to `/app/briefs/{run_id}`; Cancel returns to the editor and posts nothing. `ask_default` is true with a visible toggle.

- [ ] **Step 1: Write failing tests** `TestPreviewSections` (seven headings in order), `TestPreviewShowsProblems` (a brief missing `Acceptance` lists it), `TestPreviewRunNameMatchesNext` (preview name equals the id `startVersion` assigns when nothing else starts), `TestCancelStartsNothing` (page JS has no post on Cancel; no run folder exists after only a GET), `TestRunEndpointBusy` (409 with `active_run_id`).
- [ ] **Step 2: Run to verify fail:** `go test ./serve -run Preview`.
- [ ] **Step 3: Implement** the handlers and page.
- [ ] **Step 4: Verify pass** with the Global Constraints command; check the page by eye.
- [ ] **Step 5: Commit** `feat(serve): run preview with continue and cancel`.

### Task 8: Migration command and old-id lookup

**Files:**
- Create: `cmd/gophermind/brief_migrate_library.go`, `cmd/gophermind/brief_migrate_library_test.go`, `gophermind-lib/library/aliases.go`
- Modify: `serve/briefs.go`, `serve/chattools_read.go` (resolve an old id through the alias map before reading)

**Interfaces:**
- Command: `gophermind brief migrate-library --briefs DIR --repos DIR --runs DIR [--server-url URL] [--apply]`. Default is a dry run that prints `old -> new` for every run and changes nothing. With `--apply` it: refuses when `--server-url` (default `http://127.0.0.1:8090`) answers `/healthz` or any `run.json` is planning or running; copies `briefs/` and `repos/` to `<briefs>/../library-migration-backup-<stamp>/` first (refuses if the destination exists or free space is short); builds the families (title slug) and `v1` from the stripped text; renames each `briefs/<old>`, `repos/<old>` and `runs/<old>.json` to the new id; rewrites `id:` and `repo:` in `brief.md` and in `repos/<new>/.gophermind/<new>/brief.md`, renames `repos/<new>/.gophermind/<old>` to `<new>`, and rewrites the old id in `_state/calls.jsonl`; writes `library/aliases.json` (old to new); journals every step in `library/.migration-journal.jsonl` so a rerun resumes and `--rollback` undoes in reverse; ends by loading every run through `runview` and printing counts.
- `library.Aliases`: `func LoadAliases(dir string) (map[string]string, error)` and `func (a Aliases) Resolve(id string) string`.
- Order: per version per date, by old id (date then number); date from the old id.

- [ ] **Step 1: Write failing tests** on a temp copy of a fake tree (three titles, one with four runs across two dates, plus a `repos/` working copy with `.gophermind/<old>/_state/calls.jsonl`): `TestDryRunChangesNothing` (tree hash identical); `TestApplyRenamesAll` (names `csvstat-v1-20261029-r001..r004`, `id:` and `repo:` lines match, calls.jsonl rewritten, aliases written, every run loads); `TestRefusesWhenServerAnswers`; `TestRefusesWhenRunLive`; `TestInterruptedThenRerun` (kill after the second rename via an injected failure, rerun finishes, no run lost or duplicated); `TestRollbackRestores` (tree hash equals the pre-migration hash); `TestAliasResolvedByRoutes` (old id in `GET /app/briefs/{old}/state` and in `brief_state` returns the run).
- [ ] **Step 2: Run to verify fail:** `go test ./cmd/gophermind -run Migrate` and `go test ./serve -run Alias`.
- [ ] **Step 3: Implement** per the Interfaces block; every delete in the command uses the guarded pattern (assigned path, non-empty, absolute, inside the intended directory).
- [ ] **Step 4: Verify pass**, then run the dry run only against a copy of John's real folders (`cp -R` into a temp directory) and compare the printed list with the expected 145 renames across 13 families; do not run `--apply` on John's data.
- [ ] **Step 5: Commit** `feat(brief): migrate-library command and old-id aliases`.

### Task 9: Docs and changelog

**Files:**
- Modify: `CHANGELOG.md`; create `docs/brief-library.md` (what a version is, the naming rule, the lock, the preview, the migration steps and rollback, how to run the dry run)

- [ ] **Step 1: Write** the doc and changelog entry in the style of existing entries (no em dashes).
- [ ] **Step 2: Verify** every command in the doc by running its `--help` or dry-run form.
- [ ] **Step 3: Commit** `docs: brief library`.
