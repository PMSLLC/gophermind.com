# Editable Stage Prompts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The 19 prompt templates of the planner and the executor's packer are editable files under `GOPHERMIND_PROMPTS_DIR`, live on the next model call, editable from `/app/prompts`, and recorded by hash in every run.

**Architecture:** A new package `build/promptdir` resolves a template by kind and name from the override directory, falling back to the embedded file; the planner's `render` and the packer's `renderTemplate` call it. The planner writes `_state/prompts.json` when a run starts. The server seeds the directory at start, serves a list page, an editor page and four JSON routes with the existing bearer plus HMAC wrap.

**Tech Stack:** Go 1.22 standard library (`embed`, `text/template`, `crypto/sha256`, `net/http`), the server's existing page and JSON helpers, one env line in `scripts/local-service.sh`.

**Spec:** `docs/superpowers/specs/2026-11-05-editable-prompts-design.md`

## Global Constraints

- Standard library only. No em dashes, no emojis. `gofmt` and `go vet` clean in `gophermind-lib`; `go test ./build/promptdir/ ./build/planner/ ./build/packer/ ./serve/` green.
- With `GOPHERMIND_PROMPTS_DIR` unset, behaviour is byte-for-byte what the embedded files give today. Every existing test runs with it unset.
- A custom prompt "must not close a data-section tag that the built-in file of the same name does not close"; a save over 256 KiB is refused.
- The run record is `_state/prompts.json`, entries `{"kind","name","source","sha256"}`, written once at run start.
- Commit per task with explicit `git add` paths and the trailers `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`. Never push.

## Review Focus

1. An override file that fails to parse at render time (edited on disk, not through the UI): the call must fail with the parse error naming the file, never fall back silently to the default. Pinned by `TestReadRefusesAnUnparsableOverride` (Task 1).
2. An override that closes a tag the default does not (`</brief>` added to `decompose.md`): refused on save and on render. Pinned by `TestSaveRefusesANewClosingTag` and `TestReadRefusesANewClosingTag` (Task 1).
3. A render while the file is being replaced: `Save` writes atomically (temp file plus rename) so a reader sees the old or the new text, never a partial one. Pinned by `TestSaveIsAtomic` (Task 1, checks a temp file never remains).
4. The seed must never overwrite an existing custom file on restart. Pinned by `TestSeedKeepsExistingFiles` (Task 1).
5. A kind or name with a path separator or `..` in the URL: 404, never a file outside the directory. Pinned by `TestPromptRoutesRejectBadNames` (Task 3).

---

### Task 1: The promptdir package

**Files:**
- Create: `gophermind-lib/build/promptdir/promptdir.go`
- Test: `gophermind-lib/build/promptdir/promptdir_test.go`

**Interfaces:**
- Produces:
  - `func Dir() string`: `os.Getenv("GOPHERMIND_PROMPTS_DIR")`.
  - `const KindPlanner = "planner"`, `const KindPacker = "packer"`, `const MaxBytes = 256 << 10`.
  - `func Read(kind, name string, embedded fs.FS) (text []byte, custom bool, err error)`: when `Dir()` is set and `<dir>/<kind>/<name>.md` exists, validates it with `check` and returns it with `custom == true`; otherwise returns `embedded.ReadFile("prompts/"+name+".md")`. A missing embedded file is an error.
  - `type Entry struct { Kind, Name string; Custom bool; SHA256 string; Text, Default string }` (JSON tags `kind`, `name`, `custom`, `sha256`, `text`, `default`).
  - `func List(kind string, embedded fs.FS) ([]Entry, error)`: one entry per embedded file, sorted by name, `Text` the current text (custom or default), `SHA256` the hex SHA-256 of `Text`.
  - `func Seed(kind string, embedded fs.FS) (written int, err error)`: with `Dir()` set, writes every embedded file that is missing under `<dir>/<kind>/`; existing files are never touched. With `Dir()` unset returns `0, nil`.
  - `func Save(kind, name string, text []byte, embedded fs.FS) error`: refuses when `Dir()` is unset (`ErrNoDir`), when the name is not an embedded prompt (`ErrNoSuchPrompt`), when `len(text) > MaxBytes` (`ErrTooLarge`), or when `check` fails; writes atomically (temp file in the same directory, rename).
  - `func Reset(kind, name string) error`: removes the custom file; no error when absent.
  - `func check(name string, text, dflt []byte) error`: `template.New(name).Option("missingkey=error").Parse` must succeed; every `</tag>` (regexp `</([a-z_]+)>`) in `text` must also occur in `dflt`, else `fmt.Errorf("prompt %s closes a data section tag the built-in prompt does not: </%s>", name, tag)`.
  - `func Record(kinds map[string]fs.FS) ([]RecordEntry, error)` with `type RecordEntry struct { Kind, Name, Source, SHA256 string }` (JSON tags lower case): every prompt of every kind, `Source` `"custom"` or `"default"`, sorted by kind then name.
- Names are validated by `^[a-z0-9_]+$` and kinds by the two constants; anything else is `ErrNoSuchPrompt`.

- [ ] **Step 1: Write the failing tests**

Use `fstest.MapFS{"prompts/a.md": {Data: []byte("Hello {{.Name}}\n<brief>\n{{.Brief}}\n</brief>\n")}}` as the embedded FS and `t.Setenv("GOPHERMIND_PROMPTS_DIR", t.TempDir())`.

```go
func TestReadFallsBackToTheEmbeddedFile(t *testing.T)   // no override: text == embedded, custom == false
func TestReadPrefersTheOverride(t *testing.T)            // write <dir>/planner/a.md "Custom {{.Name}}\n</brief>": text == it, custom == true
func TestReadWithNoDirUsesEmbeddedOnly(t *testing.T)     // env unset: custom == false even when a file exists elsewhere
func TestReadRefusesAnUnparsableOverride(t *testing.T)   // "{{.Name" -> err contains "a.md" and "template"
func TestReadRefusesANewClosingTag(t *testing.T)         // "</contract>" -> err contains "</contract>"
func TestSaveRefusesANewClosingTag(t *testing.T)         // same text through Save -> same error, no file written
func TestSaveRefusesTooLarge(t *testing.T)               // MaxBytes+1 bytes -> ErrTooLarge
func TestSaveIsAtomic(t *testing.T)                      // after Save, <dir>/planner has exactly a.md (no temp file)
func TestSeedKeepsExistingFiles(t *testing.T)            // write a.md "mine"; Seed -> written 0, file still "mine"; delete it; Seed -> written 1, file == embedded
func TestListReportsCustomAndHash(t *testing.T)          // one entry, Custom true after Save, SHA256 == hex(sha256(text)), Default == embedded
func TestResetRemovesTheOverride(t *testing.T)           // after Reset, Read gives embedded; Reset again is nil
func TestRecordListsEveryKind(t *testing.T)              // kinds planner and packer with one file each -> 2 entries sorted, Source values right
func TestBadNamesAreRefused(t *testing.T)                // "../a", "A", "a/b", kind "x" -> ErrNoSuchPrompt from Read, Save, Reset
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/promptdir/ -count=1`
Expected: FAIL (package does not build: undefined symbols).

- [ ] **Step 3: Implement `promptdir.go`**

One file, the functions above. `Read` runs `check` on an override; a failing override is an error, never a fallback.

- [ ] **Step 4: Run the tests**

Run: `cd gophermind-lib && go test ./build/promptdir/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/promptdir/promptdir.go gophermind-lib/build/promptdir/promptdir_test.go
git commit -m "feat(promptdir): prompt templates resolved from an override directory"
```

---

### Task 2: The planner and the packer read through promptdir; a run records its prompts

**Files:**
- Modify: `gophermind-lib/build/planner/prompts.go:18-20` (`render`), `:96-100` (`harnessContract`), and export `PromptFS` as a function `func EmbeddedPrompts() fs.FS` returning `promptFS` for the server's seed and list
- Modify: `gophermind-lib/build/packer/packer.go:429-431` (`renderTemplate`) and add `func EmbeddedPrompts() fs.FS`
- Modify: `gophermind-lib/build/planner/planner.go` where a new run is created in `p.load` (write the record once, not on resume); `state.go` gains `statePrompts = "_state/prompts.json"`
- Test: `gophermind-lib/build/planner/prompts_override_test.go` (package `planner_test`, uses the rig), `gophermind-lib/build/packer/packer_test.go` (one test added)

**Interfaces:**
- Consumes: `promptdir.Read`, `promptdir.Record`, `promptdir.KindPlanner`, `promptdir.KindPacker`.
- Produces: `planner.EmbeddedPrompts() fs.FS`, `packer.EmbeddedPrompts() fs.FS`; the file `_state/prompts.json` as `{"prompts":[RecordEntry...]}`.

- [ ] **Step 1: Write the failing tests**

```go
// planner, through the greeter rig
func TestAnOverrideReachesTheModel(t *testing.T)
// t.Setenv("GOPHERMIND_PROMPTS_DIR", dir); write dir/planner/clarify.md = the embedded clarify.md with its first line replaced by "CUSTOM PREAMBLE"
// (read the embedded file through planner.EmbeddedPrompts()); run g.mustPlan(StopAfter: "clarify");
// the clarify request's user message starts with "CUSTOM PREAMBLE"

func TestARunRecordsItsPrompts(t *testing.T)
// same rig, no override; after StopAfter "clarify" read <runDir>/_state/prompts.json:
// 19 entries, every Source "default", the planner/clarify entry's SHA256 equals hex sha256 of the embedded file;
// with the override from the previous test, planner/clarify has Source "custom"

func TestAResumeDoesNotRewriteThePromptRecord(t *testing.T)
// run to clarify, note the file's mtime and bytes, resume to contract: unchanged

// packer
func TestPackerRendersAnOverride(t *testing.T)
// t.Setenv to a dir with packer/implement.md = "OVERRIDE {{.Signature}}"; the rendered implement prompt starts with "OVERRIDE"
```

Pick the packer's public entry point that renders `implement.md` from an existing packer test; keep its data minimal.

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/planner/ -run 'TestAnOverrideReachesTheModel|TestARunRecordsItsPrompts|TestAResumeDoesNotRewrite' -count=1 && go test ./build/packer/ -run TestPackerRendersAnOverride -count=1`
Expected: FAIL (no override honoured, no prompts.json).

- [ ] **Step 3: Implement**

- `render`: `raw, _, err := promptdir.Read(promptdir.KindPlanner, name, promptFS)`. `harnessContract` the same with `"harness"`. `promptDataTags()` keeps reading the embedded files only (the override rule guarantees no new tags).
- packer `renderTemplate`: `promptdir.Read(promptdir.KindPacker, name, promptFS)`.
- `p.load` for a fresh run: `rec, err := promptdir.Record(map[string]fs.FS{planner: planner.EmbeddedPrompts(), packer: packer.EmbeddedPrompts()})`; the planner cannot import the packer if that makes a cycle (check `go list -deps`); if it does, `Deps` gains `ExtraPrompts map[string]fs.FS` that the server and the CLI fill with the packer's FS, and the planner records its own kind plus those. Write with `writeJSON(r.path(statePrompts), map[string]any{"prompts": rec})` only when the file does not exist.

- [ ] **Step 4: Run the planner, packer and promptdir packages**

Run: `cd gophermind-lib && go test ./build/promptdir/ ./build/packer/ ./build/planner/ -count=1 -timeout 20m`
Expected: PASS. Every pre-existing test runs with the env unset and sees the embedded files.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/prompts.go gophermind-lib/build/planner/planner.go gophermind-lib/build/planner/state.go gophermind-lib/build/planner/prompts_override_test.go gophermind-lib/build/packer/packer.go gophermind-lib/build/packer/packer_test.go
git commit -m "feat(planner,packer): prompts come from the override directory; a run records their hashes"
```

---

### Task 3: Server routes, pages, seed and the service env line

**Files:**
- Create: `gophermind-lib/serve/prompts_http.go`, `gophermind-lib/serve/assets/prompts.html`, `gophermind-lib/serve/assets/prompt_edit.html`
- Modify: `gophermind-lib/serve/webhook.go` (route table comment and registrations next to the library routes), `gophermind-lib/serve/assets/library.html:26` and `briefs.html:17` (a "Prompts" link in `.actions`), `gophermind-server/main.go` (seed at start), `scripts/local-service.sh:144-145` (env line)
- Test: `gophermind-lib/serve/prompts_http_test.go`
- Docs: `docs/prompts.md` (new, short operator doc), `CONTEXT.md` note

**Interfaces:**
- Consumes: `promptdir.List`, `Save`, `Reset`, `Seed`, `planner.EmbeddedPrompts`, `packer.EmbeddedPrompts`; the server's `briefWrap`, `briefJSON`, `briefError`, `pageHandler`.
- Produces routes:
  - `GET /app/prompts` (sign-in) list page; `GET /app/prompts/{kind}/{name}` editor page.
  - `GET /app/api/prompts` -> `{"dir": "<dir or empty>", "prompts": [Entry without text and default]}`.
  - `GET /app/api/prompts/{kind}/{name}` -> `Entry` (with `text` and `default`); 404 `no such prompt`.
  - `POST /app/api/prompts/{kind}/{name}/save` body `{"text": "..."}` -> 200 `{"sha256": "...", "custom": true}`; 400 with the check error text; 413 over `MaxBytes`; 409 `prompts directory not configured` when `Dir()` is empty.
  - `POST /app/api/prompts/{kind}/{name}/reset` -> 200 `{"custom": false, "sha256": "<default hash>"}`.
- The editor page shows the textarea, Save, Reset to default, the sha256, a line "Edits apply to the next model call; a run records the prompts it started with in _state/prompts.json", and the default text in a read-only block below.

- [ ] **Step 1: Write the failing handler tests**

Build the mux the way the existing library tests do (look for a test that calls the library list route with the bearer wrap) and `t.Setenv("GOPHERMIND_PROMPTS_DIR", t.TempDir())`:

```go
func TestPromptsListNamesEveryTemplate(t *testing.T)      // GET list: 19 entries, kinds planner and packer, all custom false
func TestPromptSaveThenGetThenReset(t *testing.T)         // save planner/clarify with the default text plus "\nCUSTOM"; GET shows custom true and the new text; reset -> custom false
func TestPromptSaveRefusesABadTemplate(t *testing.T)      // "{{.X" -> 400, body error contains "template"
func TestPromptRoutesRejectBadNames(t *testing.T)         // kind "x", name "../clarify", name "Clarify" -> 404 on GET and POST
func TestPromptSaveWithoutADirIs409(t *testing.T)         // env unset -> 409
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./serve/ -run 'TestPrompt' -count=1`
Expected: FAIL (routes missing).

- [ ] **Step 3: Implement**

Handlers in `prompts_http.go` over a `promptKinds` map `{planner: planner.EmbeddedPrompts(), packer: packer.EmbeddedPrompts()}`. Register the routes beside the library ones with the same wraps. Pages copy the structure of `library.html` and `library_edit.html` (same `gm.css`, same fetch pattern with `credentials: 'same-origin'`). `gophermind-server/main.go`: after the options are read, `promptdir.Seed` for both kinds, logging `prompts: seeded N files under <dir>` or the error (an error here is fatal only when the dir is set and not writable). `scripts/local-service.sh`: `export GOPHERMIND_PROMPTS_DIR="$BASE/prompts"` after the repos line.

- [ ] **Step 4: Run the serve package and the module checks**

Run: `cd gophermind-lib && go build ./... && go vet ./... && gofmt -l . && go test ./serve/ -count=1`
Expected: no build or vet output, `gofmt -l` prints nothing, PASS.

- [ ] **Step 5: Document**

`docs/prompts.md`: where the files live, the two kinds and 19 names, the save rules (template parse, no new closing tags, 256 KiB), the live-on-next-call behaviour, `_state/prompts.json`, and how to reset. One paragraph in `CONTEXT.md`.

- [ ] **Step 6: Commit**

```bash
git add gophermind-lib/serve/prompts_http.go gophermind-lib/serve/prompts_http_test.go gophermind-lib/serve/assets/prompts.html gophermind-lib/serve/assets/prompt_edit.html gophermind-lib/serve/webhook.go gophermind-lib/serve/assets/library.html gophermind-lib/serve/assets/briefs.html gophermind-server/main.go scripts/local-service.sh docs/prompts.md
git commit -m "feat(serve): edit the stage prompts at /app/prompts; the service seeds and serves the override directory"
```

---

## After the plan

Install needs John's word (`bash scripts/local-service.sh install` ends any active run). On first start the service seeds `~/.gophermind/local/prompts/planner/` and `packer/`; the clarify preamble John pasted is `planner/clarify.md`.
