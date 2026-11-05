# Planner Reliability Milestone 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every node-scope planning packet (component contract, decompose, enrich, test writer, revise) carries the brief's complete feature text and the three interfaces the brief's Architecture section fixes, and the slugify run no longer builds a second, wrong entry point.

**Architecture:** Replace the feature-slug lookup `briefSection` with a source selector that sends the whole brief at level 0 and every feature section uncut at the shrink levels, and records which sources each component got in `_state/sources.json`. Map component packages to the directories the brief's Architecture section names, parse the fixed interfaces from that section, show them to the component pass and enforce them deterministically afterwards, and make the entry pass the only place an entry component is created. The recorded replies of run `slugify-v1-20261105-r001` become a planner fixture set and an end-to-end regression.

**Tech Stack:** Go 1.22 (`gophermind-lib/build/planner`, `brief`, `contract`), Go text templates under `planner/prompts`, the planner fixture provider (`planner/fixture.go`) and test rig (`planner/harness_test.go`), Python 3 for the one-off fixture extraction script.

**Spec:** `docs/superpowers/specs/2026-11-05-planner-reliability-implementation-brief.md`, sections 3.1, 3.2 (recorded association only), 3.3, 4.1, 4.3, 6 (entry rows and the fixed-architecture rule), 14 (rows: Full source retention, Component source overlap, Fixed signature drift, Existing entry replacement as "one active entry record") and 15 Milestone 1. Everything else in the spec is Milestones 2 to 5.

## Global Constraints

- Go only, standard library only in the planner (spec section 1 and the repo's rule).
- Retain function-level tasks and the current model-provider system; change the existing pipeline, do not add a parallel one (spec section 1).
- "Use the original slugify brief as a regression fixture; do not embed slugify-specific behavior in the general planner" (spec section 1). Every rule below is derived from the brief text at run time.
- "Do not silently truncate normative text, substitute Architecture for an unavailable feature section, or continue after dropping exact error messages" (spec 3.3).
- "The explicit Architecture section and supplied fixed interfaces are authoritative. Use generic architecture defaults only where the brief is silent" (spec 6).
- "Schedule entry-point creation exactly once in the dedicated entry pass. Remove the instruction that also creates it in the feature batch" (spec 6).
- Persisted contract shape is unchanged in this milestone (no schema or `spec_version` change); new state goes in a new file under `_state/`.
- No em dashes, no emojis in code, prompts, docs or commit messages. `go build ./... && go vet ./... && gofmt -l .` clean in `gophermind-lib`; `go test ./build/planner/...` green. Run module commands from `gophermind-lib`.
- Subagents never touch `~/.gophermind`. Fixture text is copied from the run folder once, by the script in Task 1, and committed.
- Commit per task with explicit `git add` paths. Trailer lines: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`.

## Review Focus

1. A brief with no Architecture section (the greeter fixture): no fixed interfaces, no package mapping, every packet still gets the whole brief. Pinned by the existing greeter end-to-end test staying green (Task 2) and `TestFixedInterfacesOfEmptyArchitecture` (Task 4).
2. An Architecture section with two `cmd/` directories: a `main` package cannot be mapped and must stay as written for the existing entry check to handle. Pinned by `TestMapComponentPackagesAmbiguousMain` (Task 3).
3. A fixed interface the model wrote with different parameter names (`Run(args []string, in io.Reader, ...)`): the signature is restored to the brief's text and an event says so, no re-ask. Pinned by `TestFixedInterfaceSignatureRestored` (Task 4).
4. A component package written with the module prefix (`example.com/slugify/internal/cli`): mapped to the bare directory, not treated as unknown. Pinned by `TestMapComponentPackagesStripsModule` (Task 3).
5. A prompt that does not fit even at the last level: the error must say the brief sources were kept whole, so nobody reads it as a cut. Pinned by `TestTooLongErrNamesWholeSources` (Task 2).

---

### Task 1: Capture the slugify run as a fixture set and let the rig run any brief

**Files:**
- Create: `scripts/extract-llm-fixtures.py`
- Create: `gophermind-lib/build/planner/testdata/slugify/README.md`, `brief.md`, and one `.txt` per recorded planner reply (list below)
- Modify: `gophermind-lib/build/planner/harness_test.go:106-150` (`newRig`, `writeBrief`)
- Test: `gophermind-lib/build/planner/slugify_fixture_test.go`

**Interfaces:**
- Produces: `newRigFor(t *testing.T, gate human.Gate, briefDir, id string, dirs ...string) *rig` in `harness_test.go`; `newRig` becomes `newRigFor(t, gate, greeter, greeterID, dirs...)`. `writeBrief(t, repo, edit)` gains a `dir string` first parameter (the testdata dir holding `brief.md`). Constant `slugifyID = "slugify-v1-20261105-r001"` and `slugify = "testdata/slugify"`.
- Produces: the fixture set `testdata/slugify` that Tasks 5 and 6 run against.

- [ ] **Step 1: Write the extraction script**

`scripts/extract-llm-fixtures.py STATE_DIR OUT_DIR`: for every `STATE_DIR/llm/N.json` whose `stage` does not start with `implement:`, write `response` to `OUT_DIR/<name>` where `<name>` is the stage with `:` replaced by `.`, plus `.txt`; the second and later files for the same stage get `.2.txt`, `.3.txt` and so on in id order (the fixture provider's rule, `planner/fixture.go:26-33`). Print one line per file: `N.json -> name`. The script takes no other input and never reads outside STATE_DIR. Run it with `python3 -I`.

- [ ] **Step 2: Run it against r001 and copy the brief**

Run: `python3 -I scripts/extract-llm-fixtures.py ~/.gophermind/local/repos/slugify-v1-20261105-r001/.gophermind/slugify-v1-20261105-r001/_state gophermind-lib/build/planner/testdata/slugify`
Expected: 35 files, among them `clarify.txt`, `contract.outline.1.txt`, `contract.outline.2.txt`, `contract.outline.entry.txt`, `contract.outline.entry.2.txt`, `contract.slug-logic.txt`, `contract.cli-logic.txt`, `contract.entry-point.txt`, `contract._repair.txt`, `decompose.slug-logic.txt`, `decompose.cli-logic.txt`, `decompose.entry-point-main.txt`, `enrich.cli-logic.txt`, `enrich.cli-logic.2.txt`, `enrich._fix.txt`, `enrich_comp.*.txt`, `enrich_root.txt`, `coverage.txt`, `testwrite.fn-make.txt` through `testwrite.fn-make.4.txt`, `testwrite.fn-make.fix.txt`, `testwrite.fn-run.txt`.

Copy `~/.gophermind/local/briefs/slugify-v1-20261105-r001/brief.md` to `testdata/slugify/brief.md` and replace the `repo:` value with `REPO_DIR` (the rig substitutes it). Keep `id: slugify-v1-20261105-r001`.

Write `README.md`: origin (run id, 2026-11-05, build 0932dc1, model gemma-4-26b-a4b on the mini), the defects these replies show (feature text missing from node packets, packages `slug`/`cli`/`main` instead of directories, a second entry component `entry-point-main`), and the rule for later edits: a recorded file is never changed; a corrected reply a later task needs goes in the next `.N.txt` slot or under the stage name the fixed planner now asks for, and is listed here with its reason.

- [ ] **Step 3: Generalize the rig**

In `harness_test.go` add `newRigFor` with the signature above; `newRig` calls it with the greeter values. `writeBrief` reads `filepath.Join(dir, "brief.md")`. `g.runDir` uses `id`. Keep `greeter` last in the fixture search path only for `newRig`; `newRigFor` searches exactly `dirs` then `briefDir`.

- [ ] **Step 4: Write the smoke test**

```go
func TestSlugifyFixtureAnswersTheClarifyStage(t *testing.T) {
	g := newRigFor(t, approving(), slugify, slugifyID)
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "stage: clarify"}}}
	// use the exported helper if StageOf needs the real prefix; the fake must return clarify.txt's bytes
	resp, err := g.fake.Complete(context.Background(), req)
	if err != nil { t.Fatal(err) }
	want, _ := os.ReadFile(filepath.Join(slugify, "clarify.txt"))
	if resp.Text != string(want) { t.Fatalf("clarify reply = %q, want the fixture", resp.Text) }
}
```

Use the planner's real system-line prefix (look at `stagePrefixSystem` through an existing test that builds a request, or call `planner.StageOf` on a request the rig produced) so the request matches what `FixtureProvider` expects.

- [ ] **Step 5: Run the planner tests**

Run: `cd gophermind-lib && go test ./build/planner/ -run 'TestSlugifyFixture|TestPlanEndToEnd' -count=1`
Expected: PASS for both.

- [ ] **Step 6: Commit**

```bash
git add scripts/extract-llm-fixtures.py gophermind-lib/build/planner/testdata/slugify gophermind-lib/build/planner/harness_test.go gophermind-lib/build/planner/slugify_fixture_test.go
git commit -m "test(planner): the slugify r001 replies as a fixture set; the rig runs any brief"
```

---

### Task 2: Node-scope packets carry the whole brief, never a cut feature

**Files:**
- Create: `gophermind-lib/build/planner/sources.go`
- Modify: `gophermind-lib/build/planner/contract_stage.go:407` (component pass), `:1416-1424` (delete `briefSection`), and where `st.OutlineDone = true` is set (`:367`) to write `sources.json`
- Modify: `gophermind-lib/build/planner/promptbudget.go:81-84` (`tooLongErr`), `:158-170` (`featuresOfComponents`), `:218-225` (delete `briefSectionAt`)
- Modify: `gophermind-lib/build/planner/decompose.go:235`, `enrich.go:450`, `enrich.go:718`, `revise.go:225`, `testwriter.go:554`
- Test: `gophermind-lib/build/planner/sources_test.go`

**Interfaces:**
- Produces, in `sources.go`:
  - `type sourceRef struct { ID, Section string; Bytes int }` with JSON tags `id`, `section`, `bytes`.
  - `func briefSources(b *brief.Brief) []sourceRef`: one entry per main section in brief order with id `section-<slug(name)>` and section `<name>`, and one per feature with id `feature-<slug(name)>` and section `Features / <name>`.
  - `func componentBrief(r *run, component string, level int) string`: level 0 returns `string(r.src)` (the whole brief, as the outline pass sends it); level 1 and above return the Architecture, Data and Constraints sections (each under its `## ` heading, only when present) followed by every feature section under its `### ` heading, in brief order, never cut. The component argument is only recorded, not used to select text in this milestone.
  - `func componentSources(r *run, component string) []string`: the ids of what `componentBrief` sends at level 1 (same order).
  - `func writeSources(r *run, doc map[string]any) error`: writes `r.path("sources.json")` as `{"sources":[sourceRef...],"components":{"<id>":[ids...]}}` for every component of the doc.
- Consumes: `slug` (`contract_stage.go:1412`), `r.src`, `r.brief`, `r.path`.

- [ ] **Step 1: Write the failing tests**

Parse `testdata/slugify/brief.md` with `brief.Parse` into a `run` the way other internal tests build one (look for a test that sets `r.brief` and `r.src` directly; `run` is a package-internal struct). Then:

```go
func TestComponentBriefLevel0IsTheWholeBrief(t *testing.T)
// componentBrief(r, "cli-logic", 0) == string(r.src)

func TestComponentBriefAtShrinkLevelsKeepsEveryFeatureWhole(t *testing.T)
// for level in 1, 2, 3: s := componentBrief(r, "cli-logic", level)
//   contains "### Make a slug", "### Report errors",
//   contains "`--max 6` on `Hello, World!` gives `hello`",
//   contains "prints `usage: slugify [--max N] [FILE]`",
//   contains "## Architecture", "## Constraints",
//   does not contain "## Acceptance", "## Overview", "(cut)"
//   and componentBrief(r, "cli-logic", 3) == componentBrief(r, "cli-logic", 1)

func TestComponentSourcesListsEveryFeature(t *testing.T)
// componentSources(r, "slug-logic") == []string{"section-architecture", "section-data", "section-constraints", "feature-make-a-slug", "feature-report-errors"}

func TestBriefSourcesIdsAndSections(t *testing.T)
// briefSources has an entry {ID: "feature-report-errors", Section: "Features / Report errors"} with Bytes == len(body)

func TestTooLongErrNamesWholeSources(t *testing.T)
// tooLongErr("contract:x", 1, 2, 3).Error() contains "the brief sources were kept whole"
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/planner/ -run 'TestComponentBrief|TestComponentSources|TestBriefSources|TestTooLongErr' -count=1`
Expected: FAIL, undefined `componentBrief` and friends.

- [ ] **Step 3: Implement `sources.go` and switch every caller**

- Component pass (`contract_stage.go:407`): `"BriefSection": componentBrief(r, id, level)`.
- `decompose.go:235`, `enrich.go:450`, `enrich.go:718`, `revise.go:225`, `testwriter.go:554`: `componentBrief(r, <component id>, 0)`. In `testwriter.go` the component id is `parent`.
- `featuresOfComponents` returns every feature name (the repair excerpts then carry every feature, capped as they are today; repair prompts are outside this milestone).
- Delete `briefSection` and `briefSectionAt`.
- `tooLongErr` message: append "; the brief sources were kept whole and never cut".
- Where the outline finishes (`st.OutlineDone = true`), call `writeSources(r, st.Doc)` and emit one event per component at a non-warning kind (`events.KindInfo` if it exists, else the kind `prompt_tokens_estimate` uses), stage `contract`, text `component_sources: <id> <- <ids joined by ", ">`. It is a record, not a problem, so it must not show as a `warning:` line in run.log.

- [ ] **Step 4: Run the tests and the whole planner package**

Run: `cd gophermind-lib && go test ./build/planner/ -count=1`
Expected: PASS. The greeter end-to-end test still passes with the whole brief in every packet.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/sources.go gophermind-lib/build/planner/sources_test.go gophermind-lib/build/planner/contract_stage.go gophermind-lib/build/planner/promptbudget.go gophermind-lib/build/planner/decompose.go gophermind-lib/build/planner/enrich.go gophermind-lib/build/planner/revise.go gophermind-lib/build/planner/testwriter.go
git commit -m "fix(planner): node packets carry the whole brief, never one feature or a cut one"
```

---

### Task 3: Component packages are the directories the Architecture section names

**Files:**
- Create: `gophermind-lib/build/planner/packagemap.go`
- Modify: `gophermind-lib/build/planner/contract_stage.go:263-268` (after `mergeOutline` in the outline parse callback) and `localChecks` (`:1226-1240`)
- Modify: `gophermind-lib/build/planner/prompts/contract_outline.md` (the `package` wording)
- Test: `gophermind-lib/build/planner/packagemap_test.go`

**Interfaces:**
- Produces, in `packagemap.go`:
  - `func archDirs(arch string) []string`: the directory of every Architecture bullet (`archBulletRE` from `archuses.go`), in order, duplicates dropped.
  - `func mapComponentPackages(doc map[string]any, arch, module string) []string`: for each component, in this order: a package with the module prefix is stripped with `stripModule`; a package equal to the last path element of exactly one Architecture directory becomes that directory; a package `main` becomes the one Architecture directory under `cmd/` when exactly one exists. Returns `"<id>: <old> -> <new>"` for each change. A package that is already one of the directories, or matches nothing or more than one, is unchanged.
- Consumes: `archBulletRE`, `stripModule`, `objects`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMapComponentPackagesToArchitectureDirs(t *testing.T)
// arch = the Architecture section of testdata/slugify/brief.md
// components slug-logic/slug, cli-logic/cli, entry-point/main
// after: internal/slug, internal/cli, cmd/slugify; changes has 3 entries, the first "slug-logic: slug -> internal/slug"

func TestMapComponentPackagesAmbiguousMain(t *testing.T)
// arch with bullets `cmd/a` and `cmd/b`; a component with package main is unchanged; changes empty

func TestMapComponentPackagesStripsModule(t *testing.T)
// module "example.com/slugify", package "example.com/slugify/internal/cli" -> "internal/cli"

func TestMapComponentPackagesLeavesUnknownNames(t *testing.T)
// package "greet" with the slugify arch is unchanged

func TestLocalChecksRefuseMainAsAComponentPackage(t *testing.T)
// a contract whose component package is "main" fails localChecks with a message containing "package main is a Go package name, not a directory"
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/planner/ -run 'TestMapComponentPackages|TestLocalChecksRefuseMain' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `packagemap.go` as specified.
- In the outline parse callback, right after `mergeOutline` succeeds and before `st.Doc` is assigned: `changes := mapComponentPackages(doc, r.brief.Sections["Architecture"], module)` where module is `doc["module"]`; emit one warning per run of changes: `component_package_mapped: <n> component packages were Go names, mapped to the directories the Architecture section names: <changes joined by "; ">`.
- `localChecks`: after the existing component id checks, `comp.Package == "main"` returns `fmt.Errorf("contract: component %s: package main is a Go package name, not a directory; use the cmd/<name> directory", boundedID(comp.ID))`. The mapping runs first, so this only fires when no single `cmd/` directory exists to map to.
- `contract_outline.md`: change the component rule to read: "A component's `package` is the directory of its Go package relative to the repository root (`internal/slug`, `cmd/slugify`), never the Go package name alone (`slug`, `main`). The Go name is the last element of the directory; a directory under `cmd/` is package main."

- [ ] **Step 4: Run the planner tests**

Run: `cd gophermind-lib && go test ./build/planner/ -count=1`
Expected: PASS. The greeter fixture's `greet` package has no Architecture match and stays as it is.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/packagemap.go gophermind-lib/build/planner/packagemap_test.go gophermind-lib/build/planner/contract_stage.go gophermind-lib/build/planner/prompts/contract_outline.md
git commit -m "fix(planner): component packages map to the directories the Architecture section names"
```

---

### Task 4: The brief's fixed interfaces reach the component pass and are enforced

**Files:**
- Create: `gophermind-lib/build/planner/fixediface.go`
- Modify: `gophermind-lib/build/planner/prompts/contract_component.md:15-16` (the entry paragraph) and the block before `Component:`
- Modify: `gophermind-lib/build/planner/contract_stage.go:380-470` (component pass: prompt data, surface target, post-pass check), `contractState` (new `FixedRepairs map[string]int`)
- Modify: `gophermind-lib/build/planner/cli_surface.go:189` (`runFunction` takes the fixed call name)
- Test: `gophermind-lib/build/planner/fixediface_test.go`

**Interfaces:**
- Produces, in `fixediface.go`:
  - `type fixedIface struct { Dir, Name, Signature string }`.
  - `func fixedInterfaces(arch string) []fixedIface`: for every Architecture bullet with a directory, every backtick span in that line matching `^([A-Z][A-Za-z0-9_]*)\((.*)\)\s*(.*)$` becomes `{Dir, Name: group 1, Signature: "func " + span}`; a `cmd/` directory always contributes `{Dir, "main", "func main()"}`. Order: bullet order, then span order.
  - `func entryCall(arch string) (pkg, name string)`: in the first `cmd/` bullet, the first `pkg.Func` match of `archQualRE`; `"", ""` when none.
  - `func fixedInterfacesText(ifs []fixedIface) string`: one line per interface, `<Dir>: <Signature>`.
  - `func applyFixedInterfaces(doc map[string]any, component, dir string, ifs []fixedIface) (applied []string, missing []fixedIface)`: for each interface whose `Dir` equals `dir`, find a function of the component whose signature name (`funcSigNameRE`) equals `Name` case-insensitively; when found and the signature differs, set `signature` to the fixed one and, when the name differs, the name, and record `"<id>: <old sig> -> <new sig>"`; when none is found, it is missing.
  - `func fixedDefectText(missing []fixedIface) string`: "the brief's Architecture section fixes these interfaces for this package and the component does not declare them: <Signature list>. Write each with exactly that name and signature".
- Consumes: `archBulletRE`, `archQualRE`, `funcSigNameRE` (`archuses.go`), `objects`, `stripModule`.

- [ ] **Step 1: Write the failing tests**

```go
func TestFixedInterfacesOfSlugifyArchitecture(t *testing.T)
// three entries in order: {internal/slug, Make, "func Make(s string, max int) string"},
// {internal/cli, Run, "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"},
// {cmd/slugify, main, "func main()"}  (bullet order puts cmd first; assert by Dir, not index)
// entryCall == ("cli", "Run")

func TestFixedInterfacesOfEmptyArchitecture(t *testing.T)
// fixedInterfaces("") is empty; entryCall("") is "", ""

func TestFixedInterfaceSignatureRestored(t *testing.T)
// a function of cli-logic named run with signature "func run(args []string, in io.Reader, out, errw io.Writer) int"
// after applyFixedInterfaces: name "Run", signature exactly the brief's; applied has one entry; missing empty

func TestFixedInterfaceMissingIsADefect(t *testing.T)
// cli-logic with only fn-parse-args: missing has the Run interface; fixedDefectText contains "func Run(args []string"

func TestComponentPromptNamesTheFixedEntryCall(t *testing.T)
// render("contract_component", data with EntryCall "cli.Run", Fixed text): output contains "calls `os.Exit` with the result of `cli.Run`"
// and does not contain "`run` has the signature"
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/planner/ -run 'TestFixedInterface|TestComponentPromptNames' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `fixediface.go` as specified.
- `contract_component.md`: the entry bullet becomes two branches. With `{{.EntryCall}}` (for example `cli.Run`): "A component whose package is under `cmd/` is the entry point and is package main. It must contain `fn-main` named `main` with the signature exactly `func main()`. `main` only calls `os.Exit` with the result of `{{.EntryCall}}`, passing `os.Args[1:]`, `os.Stdin`, `os.Stdout` and `os.Stderr`; `{{.EntryCall}}` is declared by another component, list its id in `uses`. Write no `run` function." Without it, the existing generic `run` text, unchanged. Add before `Component:`: `{{if .Fixed}}Fixed interfaces of this component's package, from the brief's Architecture section. Declare each with exactly this name and signature, in this package. Do not rename, move or change parameters; private helpers may be added:\n<fixed_interfaces>\n{{.Fixed}}\n</fixed_interfaces>\n\n{{end}}`. Then the spec's two rule lines, verbatim, in the Requirements list: "The explicit Architecture section and supplied fixed interfaces are authoritative. Use generic architecture defaults only where the brief is silent." and "Package directories, owning component IDs, and fixed signatures are supplied facts. If they conflict, report the conflict; do not resolve it by renaming or relocating a function."
- Component pass: compute `ifs := fixedInterfaces(arch)` and `callPkg, callName := entryCall(arch)` once before the loop. Prompt data gains `"Fixed": fixedInterfacesText(ifs filtered to this component's dir)` and `"EntryCall": callPkg + "." + callName` when the component is the entry package and a call exists (else `""`).
- Surface target: when `callName != ""`, the surface check applies to the component whose package's last element equals `callPkg`, and `runFunction(doc, component, callName)` matches `func <callName>(` instead of `func run(`. `runFunction` gains the name parameter; the existing callers pass `"run"`.
- Post-pass, when `!more`, after the main-defect check: `applied, missing := applyFixedInterfaces(st.Doc, id, dir, ifs)`; emit `fixed_interface_applied: <entries>` as a warning when any; when `missing` is not empty and `st.FixedRepairs[id] == 0`, increment it and set `defect, more = fixedDefectText(missing), true`; when missing and already repaired once, return `fmt.Errorf("contract: component %s does not declare the fixed interface %s from the brief's Architecture section", boundedID(id), missing[0].Signature)`.

- [ ] **Step 4: Run the planner tests**

Run: `cd gophermind-lib && go test ./build/planner/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/fixediface.go gophermind-lib/build/planner/fixediface_test.go gophermind-lib/build/planner/contract_stage.go gophermind-lib/build/planner/cli_surface.go gophermind-lib/build/planner/prompts/contract_component.md
git commit -m "fix(planner): the brief's fixed interfaces reach the component pass and are enforced"
```

---

### Task 5: The entry component is created once, in the entry pass

**Files:**
- Modify: `gophermind-lib/build/planner/contract_stage.go:236-242` (the `entryName = binName` injection)
- Modify: `gophermind-lib/build/planner/prompts/contract_outline.md` (the `{{if .EntryName}}` paragraph)
- Test: `gophermind-lib/build/planner/contract_stage_test.go` (or the file that renders outline prompts today)

**Interfaces:**
- Consumes: Task 3's mapping, which turns a `main` component the batch pass writes on its own into the `cmd/` directory so `hasEntryPoint` is true and the entry pass is skipped.

- [ ] **Step 1: Write the failing test**

```go
func TestLastBatchPassDoesNotWriteTheEntryPoint(t *testing.T)
// render("contract_outline", data for a batch pass with Batch "Make a slug" and EntryName "slugify", Entry ""):
// output does not contain "this pass also writes the entry point"
// and the rendered entry pass (Entry "slugify", EntryName "slugify") still contains "package `cmd/slugify`"
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd gophermind-lib && go test ./build/planner/ -run TestLastBatchPassDoesNotWriteTheEntryPoint -count=1`
Expected: FAIL on the first assertion.

- [ ] **Step 3: Implement**

Delete the `if entryName == "" && runnable && ...` block so `EntryName` is `entry` only. Delete the `{{if .EntryName}}- The brief describes something that is run, so this pass also writes the entry point ... {{end}}` paragraph from `contract_outline.md`; the `{{if .Entry}}` block at the end keeps `cmd/{{.EntryName}}`.

- [ ] **Step 4: Run the planner tests**

Run: `cd gophermind-lib && go test ./build/planner/ -count=1`
Expected: PASS. The greeter fixture is a library and never had an entry pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/contract_stage.go gophermind-lib/build/planner/prompts/contract_outline.md gophermind-lib/build/planner/contract_stage_test.go
git commit -m "fix(planner): the entry component is created once, in the entry pass"
```

---

### Task 6: The slugify regression: packets carry the features, one entry, fixed interfaces

**Files:**
- Create: `gophermind-lib/build/planner/slugify_test.go`
- Modify: `gophermind-lib/build/planner/testdata/slugify/README.md` and any `.N.txt` fixture the run now needs
- Modify: `docs/unification-overview.md` section 6 (one paragraph)

**Interfaces:**
- Consumes: `newRigFor`, `slugify`, `slugifyID` (Task 1); the request log `g.fake.Requests()` and `planner.StageOf`.

- [ ] **Step 1: Write the test**

```go
func TestSlugifyPacketsCarryTheFeatureText(t *testing.T) {
	gate := human.NewProgrammatic()  // answer as TestPlanEndToEnd does: approve, confirm, answer "Yes"
	g := newRigFor(t, gate, slugify, slugifyID)
	_, _ = g.plan(planner.Options{}) // the outcome is not the subject; the packets are
	byStage := map[string][]string{}  // stage -> user message of each request
	for _, req := range g.fake.Requests() { ... }
	mustHave := []string{"contract:slug-logic", "contract:cli-logic", "decompose:slug-logic", "decompose:cli-logic", "enrich:cli-logic", "testwrite:fn-make", "testwrite:fn-run"}
	for _, stage := range mustHave {
		msgs := byStage[stage]
		if len(msgs) == 0 { t.Fatalf("stage %s was never asked; stages: %v", stage, g.stagesCalled()) }
		for _, want := range []string{"### Make a slug", "### Report errors",
			"`--max 6` on `Hello, World!` gives `hello`",
			"prints `usage: slugify [--max N] [FILE]`",
			"`Make(s string, max int) string`"} {  // the Architecture bullet's own text
			if !strings.Contains(msgs[0], want) { t.Errorf("%s packet lacks %q", stage, want) }
		}
	}
	if contains(g.stagesCalled(), "contract:outline:entry") { t.Error("an entry pass ran although the batch pass's main component was mapped to cmd/slugify") }
	c := g.contracts()
	// exactly one component under cmd/: id entry-point, package cmd/slugify
	// component packages are internal/slug, internal/cli, cmd/slugify
	// fn-make: package internal/slug, signature exactly "func Make(s string, max int) string"
	// fn-run: component cli-logic, signature exactly "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int"
	// fn-main: component entry-point, signature "func main()"
	// sources.json exists under the run's _state and its components map has three keys each listing feature-make-a-slug and feature-report-errors
}
```

Read `g.contracts()` in `contract_stage_test.go:16` for the accessor and `e2e_test.go:70-100` for the gate loop.

- [ ] **Step 2: Run it**

Run: `cd gophermind-lib && go test ./build/planner/ -run TestSlugifyPacketsCarryTheFeatureText -count=1 -v`
Expected: the first run most likely fails on a stage that was never asked, because a recorded reply answered a stage the fixed planner no longer asks (`decompose:entry-point-main` is now `decompose:entry-point`) or a recorded reply is now refused (for example a `run` function the fixed-interface check renames, then a second pass the fixture must answer).

- [ ] **Step 3: Supply the equivalent replies the fixed planner asks for**

For each stage the run stops at: copy the recorded reply for the old stage under the new stage name with only the ids adjusted (`entry-point-main` to `entry-point`), or add the next `.N.txt` slot with the smallest correction that answers the defect text. Never edit a recorded file. List every added file in `README.md` with the stage, the source file and the reason. Repeat until the test's `mustHave` stages are all asked. If the run cannot reach `testwrite:fn-run` without changing what a Milestone 1 task built, stop and report which check refuses which reply; that is a finding for the plan, not a fixture to fake.

- [ ] **Step 4: Run the whole planner package and the module checks**

Run: `cd gophermind-lib && go build ./... && go vet ./... && gofmt -l . && go test ./build/planner/ -count=1`
Expected: no build or vet output, `gofmt -l` prints nothing, tests PASS.

- [ ] **Step 5: Document**

Add one paragraph to `docs/unification-overview.md` section 6: node packets carry the whole brief (level 0) or every feature uncut (shrink levels), `sources.json` records what each component got, component packages map to the Architecture directories, fixed interfaces are shown and enforced (`fixed_interface_applied`, defect re-ask, then an error), the entry component is created only in the entry pass, and `testdata/slugify` is the r001 regression. Match the file's existing style.

- [ ] **Step 6: Commit**

```bash
git add gophermind-lib/build/planner/slugify_test.go gophermind-lib/build/planner/testdata/slugify docs/unification-overview.md
git commit -m "test(planner): slugify r001 regression: every packet carries the features, one entry, fixed interfaces"
```

---

## Out of this milestone (recorded so nobody adds them by accident)

- Feature-to-component association by content (spec 3.2 beyond recording): every component gets every feature in this milestone. Large briefs pay prompt tokens for it; the association filter is Milestone 2 or later work once a brief that needs it exists.
- Splitting a pass by function count when a packet does not fit (spec 3.3, second sentence).
- Decisions and assumptions as a separate packet block for the test writer (spec 3.1, last paragraph): the decompose and enrich prompts already carry `Decisions`; the test writer gets them in Milestone 4.
- Declaration placement checks (4.2), whole-candidate validation and duplicate rules (5), repair transactions (7), no-progress detection (8), task materialization (9), enrichment limits (10), tests from requirements (11), resume fingerprints (12), the regression cases of 13.4.
- The executor's `provider_unavailable` misclassification of a truncated implement reply (seen in r001; separate fix).
