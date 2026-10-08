# Run View and Retire v1 Implementation Plan (sub-projects D and G)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the server and the desktop app one new node-based pipeline contract read from the run folder, then delete v1 (`plantree`, `orchestrate`, the PhaseFlow executor and `/phase`) and drop the `briefv2` name.

**Architecture:** A new library `runview` reads a run folder (plan nodes, runtime files, `_state/events.jsonl`, `calls.jsonl`, `report.json`, `decisions`, `attempts/`) and follows it live; it is the only reader that knows the layout, and it never writes. The server's pipeline handlers become thin wrappers over `runview`; the event hub and the file watcher are deleted because every SSE stream follows the event log itself, with replay from a line number. The desktop app decodes the same `runview` types; its plain-Go state, pump, follower and formatters get tests, the cgo panel only calls them. Then three removal tasks, each behind a Go test that fails while the thing it forbids still exists, and last one mechanical rename commit.

**Tech Stack:** Go (modules `gophermind` root, `gophermind/gophermind-lib`, `gophermind/gophermind-osx`, joined by `go.work`), standard library only for the new code, existing packages `tree`, `blackboard`, `ledger`, `report`, `planner`, `runfs`, `artifacts`; one embedded HTML page (vanilla JS); bash for the smoke script.

**Spec:** `docs/superpowers/specs/2026-10-08-retire-v1-and-upgrade-consumers-design.md` (binding). Inventory: `docs/reviews/2026-10-08-v1-caller-inventory.md`. Scope in the goal: tasks 7 to 9 of `docs/GOAL-UNIFICATION.md`.

## Global Constraints

- **Preconditions.** Sub-projects A and B are built on the branch (this plan reads their files: `*.runtime.json`, `_state/events.jsonl`, `_state/calls.jsonl`, `attempts/`, `decisions` inside nodes). Tasks 1 to 12 (G1 to G4) need only A and B. **Tasks 13 to 15 (G5 to G7) need sub-project C built** (`/project` is the v2 flow in the TUI and the v1 commands are renamed `/plan-v1` and `/plan-v1-execute`); Task 13 step 1 checks it. In the worktree today `gophermind-lib/tui` imports no `briefv2` package, so C is not built yet.
- All code work happens in the linked worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core` on branch `feat/briefv2-planner-core`, one writer at a time. This plan file lives in the docs-only checkout (`/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com`). Never run `git worktree add`, `git checkout`, `git stash` or `--gw-force`. Paths below are relative to the worktree root unless they start with `docs/superpowers/` in the main checkout (said explicitly).
- Run tests with a throwaway home. The disk is nearly full: the helper deletes its home even when the tests fail, and no step may leave a temp home, a built binary or a bundle behind. Define once per shell:

```bash
gt() { local gc gm h rc; gc="$(go env GOCACHE)"; gm="$(go env GOMODCACHE)"; h="$(mktemp -d)"; HOME="$h" GOCACHE="$gc" GOMODCACHE="$gm" go test "$@"; rc=$?; rm -rf -- "${h:?}"; return $rc; }
```

  Run `gt` from the worktree root for `./gophermind-lib/...`, `./cmd/...` and `./gophermind-server/...`. For the desktop app, `cd gophermind-osx` first (it is its own module): `gt ./client/... ./ui/... -count=1`. The package `gophermind-osx` itself (the cgo panel, `package main`) needs libui; its tests run through the project's `runOnUIThread` harness and are run once in Task 11. The e2e tests carry the build tag `e2e`.
- Run `gofmt -w` on every Go file you create or change before running its tests; `gofmt -l` over the touched folders must print nothing at each commit.
- A test that shells out to `git` strips every `GIT_*` variable first (a past incident wrote into the repo's `.git/config`).
- **Do not** install or notarise the desktop app, do not deploy to the mini, do not merge, push `main` or tag a GopherMind release (those are John's, `docs/GOAL-UNIFICATION.md` execution rules). The app is built unsigned and locally only (Task 12), and the built bundle is deleted afterwards.
- **Privacy.** Pipeline endpoints return plan text, decisions, runtime files and saved attempt artifacts to a bearer-token holder. They never read or return `credentials.md`, the vault, `approval.json`'s hash fields beyond what `runview` types name, a prompt, or anything outside `<root>/.gophermind/<run>/`. Artifacts are already scrubbed by the executor (A, Task 5); the server adds no raw read of a model reply anywhere else.
- **Path safety.** A run id must match the planner's pattern, a node id `^[a-z0-9][a-z0-9-]*$`, an artifact `file` one of four names, `attempt` an integer at least 1. A folder, or a parent of it, that is a symbolic link is refused. Error bodies never echo a path or a request value.
- No em dashes and no emojis in new text, code comments or messages. Terminal and script output uses plain labels.
- Each commit message ends with these two lines after a blank line:
  `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN`
- Any recursive delete of a computed path follows the guard: assign the target, check it is absolute, non-empty and inside the intended folder, print it, then `rm -rf -- "${target:?}"`. Prefer `git rm` for tracked files and `os.Remove` for single files.
- Where a spec is silent, choose what makes a silent failure or a dropped requirement impossible over what is faster, and record the ruling in the task's commit message (`docs/GOAL-UNIFICATION.md`).

## Rulings taken while grounding the plan in the code

The spec is binding. Where it was silent or two statements could not both hold, this plan chose as follows. Each is repeated in the task that applies it.

1. **`schema_version` and the report.** Every JSON body carries `"schema_version": 1` except `GET /pipeline/report`: its final body is the executor's `report.json` verbatim and keeps the report's own `schema_version` (2 today), so a second field cannot be added without breaking "verbatim". The report response carries the header `X-Gophermind-Pipeline-Schema: 1` like every response, and the client checks the header for that one endpoint (Task 9).
2. **`GET /pipeline/state` gains `event_seq` and `warnings`.** `event_seq` is the number of lines in `_state/events.jsonl` read before the nodes were, so a client can resume the stream at exactly the snapshot (`since=<event_seq>`) with no gap. `warnings` (omitted when empty) reports what was skipped: a corrupt event line, an unreadable runtime file, an unreadable plan. Both are additive, and the contract is not released yet, so `schema_version` stays 1.
3. **Stream start.** `GET /pipeline/events` without `since` and without `Last-Event-ID` follows from the current end of the log (live only, as the old hub did). `since=0` replays everything. A client that wants no gap reads `event_seq` from the state and passes it.
4. **Derived events.** The event log holds only blackboard changes (`status`, `attempt`, `result`). `node-status`, `node-attempt` and `node-result` map one to one from log lines. `wave-changed`, `run-status` and `run-report` are derived by `Follow` from state, never replayed: their baseline is taken after the replay, so a late client is not told old news. Their `id:` is the line number current when they were noticed.
5. **Attempt events.** The n-th `attempt` line of a node maps to the n-th attempt in its runtime file, so a replay shows each attempt once with its own data.
6. **`counts`.** `counts.nodes` and `counts.by_status` count function nodes (the leaves the executor builds). Root and component nodes are structure and appear in the `nodes` list only. `NodeSummary.last_attempt` is the full attempt shape (a superset of the spec's field list).
7. **Newest run.** "Newest first" sorts by run id descending (ids embed the date and a sequence). With no run, `GET /pipeline/state` returns 200 with `"run": null` and an empty `nodes`; the other run-scoped endpoints return 404.
8. **Partial report.** The running report is the executor's `report.Build` over the ledger calls and runtime rows with `"complete": false`; the final report has no `complete` key, and a reader treats an absent key as complete.
9. **Runs not pruned, stream cap 16, poll 250 ms** (decided in `docs/GOAL-UNIFICATION.md`). The cap is `serve.PipelineDeps.MaxStreams` (0 means 16) fed from `GOPHERMIND_PIPELINE_MAX_STREAMS` in the server and the CLI.
10. **`.gitignore` swallows `build/`.** Line 26 of `.gitignore` is `build/`, which would silently ignore `gophermind-lib/build/` and `docs/build/` for every new file. Task 15 adds the two negations before it moves anything, as `desktop/build` already does.
11. **Stub version.** The `/phase` notice names the release it was removed in. John tags releases, so the version is one constant (`tui.PhaseRemovedIn`) set to `0.10.0`; changing it is a one-line edit. The stub exists for one release and is removed by a later change, not by this plan.
12. **`WatchEvents` had no caller.** `gophermind-osx/pipelinepanel.go` defines `WatchEvents` but nothing calls it, so the live half of the panel never ran. Task 11 wires the follower for real.
13. **Skill pack validator.** `.gophermind/skills-packaging/scripts/validate_pack.py` reads a `packs/` folder that does not exist in the tree, so it cannot run today. Task 14 edits it (the file list, one check) and verifies it only with `py_compile`.
14. **Left alone, reported.** `gophermind-lib/projectctx/projectctx.go` still reads `.planning/SPEC.md` and `ROADMAP.md` as optional context. Nothing will write them any more, but the spec does not list the file; it is not changed here.
15. **`examples/phase-flow/`.** The spec's removal table does not list it. `ai-interview/` is a sample `.planning/` project for the removed workflow, so it goes with it; `briefs/` holds plain brief files that `/project` still takes, so it stays with its README command line updated (Task 14).
16. **The stub is checked in the compiled binary.** `gophermind phase` is dispatched before configuration is validated, like `free` and `brief`, so it prints the notice on a machine with no endpoint; the dispatch test drives the real binary (Task 14).

## Review Focus

1. A request value reaching the file system: run id, node id, artifact file, attempt number, a symlinked run or `.gophermind` folder (Tasks 1, 4, 6).
2. A reader writing: `runview` must not create, rename or touch any file of the run folder (Task 3 digest test).
3. The stream: replay from a line number gives each event once, a corrupt line is skipped and reported, a cancelled client frees its slot, the 17th stream gets 503 (Tasks 5 and 7).
4. Server and app disagreeing: a body or header with another `schema_version` becomes a typed error and a message, never a half-decoded panel (Tasks 9 to 11).
5. Removal gates really fail while the thing exists: write the gate test, watch it fail, then remove (Tasks 13 to 15).
6. The rename commit contains nothing but the rename (Task 15).

## File Structure

```text
gophermind-lib/runview/
  types.go, types_test.go                  NEW  wire types, constants, errors (Task 1)
  safe.go, safe_test.go                    NEW  id checks, symlink-refusing readers (Task 1)
  runviewtest/build.go                     NEW  builds real run folders for tests (Task 2)
  runview.go, runview_test.go              NEW  Runs, Open, Summary, State (Task 3)
  detail.go, detail_test.go                NEW  Node, ArtifactNames, Artifact, Decisions, Report (Task 4)
  follow.go, follow_test.go                NEW  Follow (Task 5)
  testdata/golden/*.json                   NEW  contract goldens read by the desktop client tests (Tasks 4, 9)
gophermind-lib/serve/
  pipeline_api.go, pipeline_api_test.go    NEW  JSON endpoints (Task 6)
  pipeline.go, pipeline_test.go            MODIFIED  hub deleted, events handler rewritten (Task 7); dashboard kept
  pipeline_watch.go, pipeline_watch_test.go DELETED (Task 7)
  sse.go, events.go, webhook.go            MODIFIED  frame with id, event types, routes (Tasks 6, 7)
  assets/pipeline.html                     MODIFIED  rewritten for nodes (Task 8)
gophermind-server/server.go, main.go, server_test.go, server_integration_test.go  MODIFIED  wiring (Task 7), endpoint tests over a run folder and an opt-in real-repo smoke (Task 12)
cmd/gophermind/main.go                     MODIFIED  wiring (Task 7), /phase stub (Task 14)
gophermind-osx/client/service.go, sse.go (+ tests)  MODIFIED  new calls, typed schema error (Task 9)
gophermind-osx/ui/pipeline.go, pipelinepump.go, pipelinefollow.go, pipelineformat.go (+ tests)  MODIFIED/NEW  (Task 10)
gophermind-osx/pipelinepanel.go, main.go, e2e_pipeline_test.go, pipelinepanel_test.go  MODIFIED  (Task 11)
gophermind-lib/gates/removal_test.go       NEW  import, directory and name gates (Tasks 13 to 15)
gophermind-lib/tui/*                       MODIFIED/DELETED  v1 flow removed (Task 13)
gophermind-lib/plantree/, orchestrate/     DELETED  (Task 13)
gophermind-lib/phaseflow/                  DELETED  (Task 14)
cmd/gophermind/phase.go                    REWRITTEN  notice stub (Task 14)
README.md, PROJECT.md, GOPHERMIND.toml, CHANGELOG.md, CREDITS.md, INDEX.md, docs/*, .gophermind/skills*, desktop/frontend/src/App.tsx  MODIFIED  (Task 14)
gophermind-lib/briefv2/ -> gophermind-lib/build/, docs/briefv2/ -> docs/build/, .gitignore  RENAMED/MODIFIED  (Task 15)
```

Steps G1 to G7 of the spec map to tasks as: G1 = Tasks 1 to 5, G2 = Tasks 6 to 8, G3 = Tasks 9 to 11, G4 = Task 12, G5 = Task 13, G6 = Task 14, G7 = Task 15.

---

### Task 1: `runview` wire types and path safety

**Files:**
- Create: `gophermind-lib/runview/types.go`, `types_test.go`, `safe.go`, `safe_test.go`

**Interfaces:**
- Produces: `const SchemaVersion = 1`; status consts `StatusPlanning`, `StatusWaiting`, `StatusPlanned`, `StatusBuilding`, `StatusVerified`, `StatusFailed`, `StatusEscalated`, `StatusInterrupted`, `StatusHarnessFault`; event type consts `EventNodeStatus` ... `EventRunReport`; wire structs `RunSummary`, `Counts`, `CoverageCount`, `Attempt`, `NodeResult`, `TestCount`, `NodeSummary`, `RuntimeDoc`, `ArtifactEntry`, `NodeBody`, `DecisionsBody`, `RunsBody`, `StateBody`, `ReportBody`, `Event` and the event data structs; errors `ErrBadID`, `ErrNotFound`; `ValidRunID`, `ValidNodeID`, `ValidArtifactName`; unexported `runDirOf`, `realDir`, `realFile`, `readLimited`.
- Consumes: `report.Report` (embedded in `ReportBody`).

- [ ] **Step 1: Write the failing tests**

`gophermind-lib/runview/safe_test.go`:

```go
package runview

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIDValidators(t *testing.T) {
	for _, id := range []string{"gm-2026-10-08-001", "gm-1999-01-31-999"} {
		if !ValidRunID(id) {
			t.Errorf("ValidRunID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "gm-2026-10-08-01", "../gm-2026-10-08-001", "gm-2026-10-08-001/..", "GM-2026-10-08-001", "gm-2026-10-08-001\n", "gm-2026-10-08-001 "} {
		if ValidRunID(id) {
			t.Errorf("ValidRunID(%q) = true", id)
		}
	}
	for _, id := range []string{"fn-greet", "a", "0abc", "gm-2026-10-08-001"} {
		if !ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "-a", "A", "a_b", "a/b", "..", "a.b", "a\n"} {
		if ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = true", id)
		}
	}
	for _, n := range []string{"reply.go", "reply.txt", "check-output.txt", "attempt.json"} {
		if !ValidArtifactName(n) {
			t.Errorf("ValidArtifactName(%q) = false", n)
		}
	}
	for _, n := range []string{"", "../reply.go", "reply.go/", "REPLY.GO", "prompt.txt", "attempt.json.bak", "reply.go\x00"} {
		if ValidArtifactName(n) {
			t.Errorf("ValidArtifactName(%q) = true", n)
		}
	}
}

func mkRun(t *testing.T, root, id string) string {
	t.Helper()
	dir := filepath.Join(root, ".gophermind", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.md"), []byte("# b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunDirOf(t *testing.T) {
	root := t.TempDir()
	want := mkRun(t, root, "gm-2026-10-08-001")
	got, err := runDirOf(root, "gm-2026-10-08-001")
	if err != nil || got != want {
		t.Fatalf("runDirOf = %q, %v; want %q", got, err, want)
	}
	if _, err := runDirOf(root, "gm-2026-10-08-002"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing run: %v, want ErrNotFound", err)
	}
	for _, id := range []string{"..", "../x", "gm-2026-10-08-001/../..", ""} {
		if _, err := runDirOf(root, id); !errors.Is(err, ErrBadID) {
			t.Errorf("runDirOf(%q) = %v, want ErrBadID", id, err)
		}
	}
	// A run folder that is a symbolic link is refused.
	if err := os.Symlink(want, filepath.Join(root, ".gophermind", "gm-2026-10-08-003")); err != nil {
		t.Fatal(err)
	}
	if _, err := runDirOf(root, "gm-2026-10-08-003"); !errors.Is(err, ErrNotFound) {
		t.Errorf("symlinked run: %v, want ErrNotFound", err)
	}
	// So is a symlinked .gophermind folder.
	other := t.TempDir()
	if err := os.Symlink(filepath.Join(root, ".gophermind"), filepath.Join(other, ".gophermind")); err != nil {
		t.Fatal(err)
	}
	if _, err := runDirOf(other, "gm-2026-10-08-001"); !errors.Is(err, ErrNotFound) {
		t.Errorf("symlinked .gophermind: %v, want ErrNotFound", err)
	}
	// A folder without brief.md is not a run.
	if err := os.MkdirAll(filepath.Join(root, ".gophermind", "gm-2026-10-08-004"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runDirOf(root, "gm-2026-10-08-004"); !errors.Is(err, ErrNotFound) {
		t.Errorf("folder without brief.md: %v, want ErrNotFound", err)
	}
}

func TestReadLimited(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, cut, err := readLimited(p, 100)
	if err != nil || cut || string(data) != "0123456789" {
		t.Fatalf("readLimited = %q, %v, %v", data, cut, err)
	}
	data, cut, err = readLimited(p, 4)
	if err != nil || !cut || string(data) != "0123" {
		t.Fatalf("readLimited(4) = %q, %v, %v", data, cut, err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readLimited(link, 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("symlink: %v, want ErrNotFound", err)
	}
	if _, _, err := readLimited(filepath.Join(dir, "missing"), 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v, want ErrNotFound", err)
	}
}
```

`gophermind-lib/runview/types_test.go` (pins the contract's field names, so a rename shows up as a red test):

```go
package runview

import (
	"encoding/json"
	"sort"
	"testing"
)

func keysOf(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWireFieldNamesMatchTheSpec(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
	}{
		{"RunSummary", RunSummary{}, []string{"counts", "coverage", "planned_at", "repo_name", "run_id", "started_at", "status", "stop_reason", "title", "updated_at", "waves"}},
		{"NodeSummary", NodeSummary{}, []string{"attempt_count", "claimed_by", "depends_on", "id", "kind", "last_attempt", "model_tier", "node_class", "parent", "requirement_ids", "result", "revision", "status", "tests", "title", "wave"}},
		{"Attempt", Attempt{}, []string{"duration_ms", "failed_tests", "failure_reason", "model", "order", "provider", "revision", "started_at", "tests_passed", "tests_total", "verdict"}},
		{"StateBody", StateBody{}, []string{"event_seq", "generated_at", "nodes", "run", "schema_version"}},
		{"RunsBody", RunsBody{}, []string{"runs", "schema_version"}},
		{"NodeBody", NodeBody{}, []string{"artifacts", "node", "runtime", "schema_version"}},
		{"DecisionsBody", DecisionsBody{}, []string{"decisions", "schema_version"}},
		{"Counts", Counts{}, []string{"by_status", "nodes"}},
	}
	for _, c := range cases {
		if got := keysOf(t, c.v); !eq(got, c.want) {
			t.Errorf("%s keys = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReportBodyCompleteKey(t *testing.T) {
	var b ReportBody
	if !b.IsComplete() {
		t.Error("a report with no complete key must read as complete")
	}
	no := false
	b.Complete = &no
	if b.IsComplete() {
		t.Error("complete:false must read as incomplete")
	}
	raw, _ := json.Marshal(b)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["complete"] != false {
		t.Errorf("complete = %v, want false", m["complete"])
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/runview/... -count=1`
Expected: compile errors (`undefined: ValidRunID`, `RunSummary`, ...), package does not build.

- [ ] **Step 3: Write `types.go`**

```go
// Package runview reads a run folder (.gophermind/<run-id>/ inside a repo) and
// follows it while it changes. It is the one place that knows the folder's
// layout for readers: plan nodes, node runtime files, _state/events.jsonl,
// _state/calls.jsonl, report.json, attempts/. It owns the wire types of the
// pipeline contract (schema_version 1) that the server serves and the desktop
// app decodes. It never writes to the run folder.
package runview

import (
	"encoding/json"
	"errors"

	"gophermind/gophermind-lib/briefv2/report"
)

// SchemaVersion is the pipeline contract version. Every JSON body carries it
// (except the final report, which keeps the report's own version), and every
// response carries it in the header X-Gophermind-Pipeline-Schema.
const SchemaVersion = 1

// Run statuses (RunSummary.Status).
const (
	StatusPlanning     = "planning"
	StatusWaiting      = "waiting"
	StatusPlanned      = "planned"
	StatusBuilding     = "building"
	StatusVerified     = "verified"
	StatusFailed       = "failed"
	StatusEscalated    = "escalated"
	StatusInterrupted  = "interrupted"
	StatusHarnessFault = "harness_fault"
)

// Event types on the stream (GET /pipeline/events).
const (
	EventNodeStatus  = "node-status"
	EventNodeAttempt = "node-attempt"
	EventNodeResult  = "node-result"
	EventWaveChanged = "wave-changed"
	EventRunStatus   = "run-status"
	EventRunReport   = "run-report"
)

var (
	// ErrBadID is returned for a run id, node id, attempt number or artifact
	// name that does not match its pattern.
	ErrBadID = errors.New("runview: invalid id")
	// ErrNotFound is returned when the run, node or file does not exist, or is
	// refused because it (or a parent) is a symbolic link.
	ErrNotFound = errors.New("runview: not found")
)

// Counts summarizes the function nodes of a run (the leaves the executor builds).
type Counts struct {
	Nodes    int            `json:"nodes"`
	ByStatus map[string]int `json:"by_status"`
}

// CoverageCount is how many of the brief's requirements the plan covers.
type CoverageCount struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

// RunSummary is one run in a list and the header of a state response.
type RunSummary struct {
	RunID      string        `json:"run_id"`
	Title      string        `json:"title"`
	RepoName   string        `json:"repo_name"` // the folder name, never a path
	Status     string        `json:"status"`
	StopReason string        `json:"stop_reason"`
	PlannedAt  string        `json:"planned_at"`
	StartedAt  string        `json:"started_at"`
	UpdatedAt  string        `json:"updated_at"`
	Waves      int           `json:"waves"`
	Counts     Counts        `json:"counts"`
	Coverage   CoverageCount `json:"coverage"`
}

// Attempt is one model attempt at a node. NodeSummary.LastAttempt and the
// node-attempt event carry the same shape.
type Attempt struct {
	Model         string   `json:"model"`
	Provider      string   `json:"provider"`
	Verdict       string   `json:"verdict"`
	FailureReason string   `json:"failure_reason"`
	DurationMS    int64    `json:"duration_ms"`
	StartedAt     string   `json:"started_at"`
	Order         int      `json:"order"`
	Revision      int      `json:"revision"`
	TestsPassed   int      `json:"tests_passed"`
	TestsTotal    int      `json:"tests_total"`
	FailedTests   []string `json:"failed_tests"`
}

// NodeResult is what a verified node landed.
type NodeResult struct {
	Commit       string   `json:"commit"`
	FilesChanged []string `json:"files_changed"`
}

// TestCount is how many tests a node declares.
type TestCount struct {
	Total int `json:"total"`
}

// NodeSummary is one node in the list view. The groups and the embedded
// decisions are not here; they come from GET /pipeline/node.
type NodeSummary struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	Parent         string      `json:"parent"`
	Title          string      `json:"title"`
	Status         string      `json:"status"`
	Wave           int         `json:"wave"`
	ModelTier      string      `json:"model_tier"`
	NodeClass      string      `json:"node_class"`
	DependsOn      []string    `json:"depends_on"`
	RequirementIDs []string    `json:"requirement_ids"`
	Revision       int         `json:"revision"`
	ClaimedBy      string      `json:"claimed_by"`
	AttemptCount   int         `json:"attempt_count"`
	LastAttempt    *Attempt    `json:"last_attempt"`
	Result         *NodeResult `json:"result"`
	Tests          TestCount   `json:"tests"`
}

// RuntimeDoc is a node's runtime file (status, claim, attempts, result).
type RuntimeDoc struct {
	Status      string      `json:"status"`
	Revision    int         `json:"revision"`
	ClaimedBy   string      `json:"claimed_by"`
	HeartbeatAt string      `json:"heartbeat_at"`
	UpdatedAt   string      `json:"updated_at"`
	Attempts    []Attempt   `json:"attempts"`
	Result      *NodeResult `json:"result"`
}

// ArtifactEntry lists the saved files of one attempt folder (attempts/<node>/<n>/).
type ArtifactEntry struct {
	Attempt int      `json:"attempt"`
	Files   []string `json:"files"`
}

// NodeBody is GET /pipeline/node: the full plan node as stored (all groups and
// its embedded decisions), its runtime file (nil before the executor started),
// and its saved attempt artifacts.
type NodeBody struct {
	SchemaVersion int             `json:"schema_version"`
	Node          json.RawMessage `json:"node"`
	Runtime       *RuntimeDoc     `json:"runtime"`
	Artifacts     []ArtifactEntry `json:"artifacts"`
}

// DecisionsBody is GET /pipeline/decisions: the root node's records.
type DecisionsBody struct {
	SchemaVersion int               `json:"schema_version"`
	Decisions     []json.RawMessage `json:"decisions"`
}

// RunsBody is GET /pipeline/runs, newest first.
type RunsBody struct {
	SchemaVersion int          `json:"schema_version"`
	Runs          []RunSummary `json:"runs"`
}

// StateBody is GET /pipeline/state. Run is null when the repo has no run.
// EventSeq is the line count of _state/events.jsonl read before the nodes, so
// a client resumes the stream at exactly this snapshot with since=EventSeq.
type StateBody struct {
	SchemaVersion int           `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	EventSeq      int64         `json:"event_seq"`
	Run           *RunSummary   `json:"run"`
	Nodes         []NodeSummary `json:"nodes"`
	Warnings      []string      `json:"warnings,omitempty"`
}

// ReportBody is the shape of GET /pipeline/report. The final body is the
// executor's report.json (no complete key); the running body is built from the
// call log and runtime files and carries "complete": false.
type ReportBody struct {
	report.Report
	Complete *bool `json:"complete,omitempty"`
}

// IsComplete reports whether the report is final: an absent key means complete.
func (b ReportBody) IsComplete() bool { return b.Complete == nil || *b.Complete }

// Event is one event of the stream. Seq is the 1-based line number of
// _state/events.jsonl (the SSE id). Data is the JSON payload.
type Event struct {
	Seq  int64
	Type string
	Data json.RawMessage
}

// NodeStatusData is the payload of node-status.
type NodeStatusData struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Wave   int    `json:"wave"`
}

// NodeAttemptData is the payload of node-attempt.
type NodeAttemptData struct {
	ID      string  `json:"id"`
	Attempt Attempt `json:"attempt"`
}

// NodeResultData is the payload of node-result.
type NodeResultData struct {
	ID     string     `json:"id"`
	Result NodeResult `json:"result"`
}

// WaveChangedData is the payload of wave-changed; State is "started" or "finished".
type WaveChangedData struct {
	Wave  int    `json:"wave"`
	State string `json:"state"`
}

// RunStatusData is the payload of run-status.
type RunStatusData struct {
	Status     string `json:"status"`
	StopReason string `json:"stop_reason"`
}
```

- [ ] **Step 4: Write `safe.go`**

```go
package runview

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// runIDRE is the planner's run id pattern (briefv2/planner/load.go runIDRE and
// rundir.idRE). Keep the three equal.
var runIDRE = regexp.MustCompile(`^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$`)

// nodeIDRE is the node schema's id pattern.
var nodeIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// artifactFiles are the only files an attempt folder serves.
var artifactFiles = []string{"reply.go", "reply.txt", "check-output.txt", "attempt.json"}

// maxReadBytes caps any single file read for a response.
const maxReadBytes = 4 << 20

// ValidRunID reports whether s is a run id.
func ValidRunID(s string) bool { return runIDRE.MatchString(s) }

// ValidNodeID reports whether s is a node id.
func ValidNodeID(s string) bool { return nodeIDRE.MatchString(s) }

// ValidArtifactName reports whether s is one of the four servable artifact files.
func ValidArtifactName(s string) bool {
	for _, n := range artifactFiles {
		if s == n {
			return true
		}
	}
	return false
}

// realDir reports whether path is a directory and not a symbolic link.
func realDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}

// realFile reports whether path is a regular file and not a symbolic link.
func realFile(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// runDirOf maps a run id to <root>/.gophermind/<id>. It refuses a bad id
// (ErrBadID) and a missing run, a symlinked .gophermind or run folder, or a
// folder without brief.md (ErrNotFound).
func runDirOf(root, runID string) (string, error) {
	if !ValidRunID(runID) {
		return "", ErrBadID
	}
	base := filepath.Join(root, ".gophermind")
	dir := filepath.Join(base, runID)
	if !realDir(base) || !realDir(dir) || !realFile(filepath.Join(dir, "brief.md")) {
		return "", ErrNotFound
	}
	return dir, nil
}

// readLimited reads a regular file (never a link) up to max bytes and says
// whether it cut the file short.
func readLimited(path string, max int64) (data []byte, truncated bool, err error) {
	if !realFile(path) {
		return nil, false, ErrNotFound
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, ErrNotFound
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > max {
		return data[:max], true, nil
	}
	return data, false, nil
}
```

- [ ] **Step 5: Run to confirm green**

Run: `gt ./gophermind-lib/runview/... -count=1`
Expected: `ok  gophermind/gophermind-lib/runview`.

- [ ] **Step 6: Commit**

```bash
git add gophermind-lib/runview
git commit -m "feat(runview): wire types of the pipeline contract and path-safe readers" -m "Ruling: the final report keeps the report's own schema_version; every other body carries schema_version 1." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 2: `runviewtest`, a builder of real run folders

Tests for `runview`, `serve`, the server and the desktop app all need run folders. This helper builds them with the real writers (`tree.Store`, `blackboard.FS`, `ledger.FS`, `artifacts.Writer`, `report.Write`), so a layout change in A or B breaks these tests instead of hiding behind a hand-written copy.

**Files:**
- Create: `gophermind-lib/runview/runviewtest/build.go`, `build_test.go`

**Interfaces:**
- Produces: `const RunID = "gm-2026-10-08-001"`; `var T0 time.Time`; `type Stage int` with `Planning`, `Waiting`, `Planned`, `Building`, `Verified`, `Failed`, `HarnessFault`; `type Fixture struct{ Root, Dir, RunID string; BB *blackboard.FS }`; `Build(t testing.TB, root string, stage Stage) Fixture`; `BuildID(t testing.TB, root, runID string, stage Stage) Fixture`; `Verify(t testing.TB, f Fixture, id string)`; `WriteReport(t testing.TB, f Fixture, status, reason string, code int, n report.NodeCounts)`; `Digest(t testing.TB, dir string) string`.
- Consumes: `tree.ParseNode`, `tree.NewStore(dir).Write`, `blackboard.NewFS`, `ledger.NewFS`, `artifacts.New`, `report.Write`, `planner.Requirement`, `planner.CoverageFile`, `runfs.Fixed`, `runfs.WriteFileAtomic`.

The run it builds is a greeter: a root node (id = run id, one decision record), a component `greeting`, and three function nodes: `fn-greet` (wave 0, groups filled), `fn-bye` and `fn-late` (wave 1, depend on `fn-greet`).

| Stage | Files present | Runtime state |
|---|---|---|
| `Planning` | `brief.md`, `_state/status.json` (waiting empty) | none |
| `Waiting` | same, `waiting: "clarify"` | none |
| `Planned` | plan, `requirements.json`, `coverage.json`, `approval.json`, `_state/status.json` with `planned_at` | none (the executor has not started) |
| `Building` | `Planned` plus `_state/executor.json`, `calls.jsonl` | `fn-greet` verified after a failed and a passed attempt; `fn-bye` in progress with one failed attempt and a saved artifact; `fn-late` pending |
| `Verified` | `Building` files plus `report.json` (status verified) | all three verified |
| `Failed` | as `Building` plus `report.json` (failed, `stop_reason` `leaf_failed`) | `fn-bye` failed, `fn-late` pending |
| `HarnessFault` | as `Failed`, `stop_reason` `harness_fault` | as `Failed` |

- [ ] **Step 1: Write the failing test**

`gophermind-lib/runview/runviewtest/build_test.go`:

```go
package runviewtest

import (
	"os"
	"path/filepath"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

func TestBuildProducesLoadablePlansAtEveryStage(t *testing.T) {
	for _, st := range []Stage{Planning, Waiting, Planned, Building, Verified, Failed, HarnessFault} {
		root := t.TempDir()
		f := Build(t, root, st)
		if f.RunID != RunID || filepath.Base(f.Dir) != RunID {
			t.Fatalf("stage %d: fixture = %+v", st, f)
		}
		if _, err := os.Stat(filepath.Join(f.Dir, "brief.md")); err != nil {
			t.Fatalf("stage %d: %v", st, err)
		}
		tr, err := tree.NewStore(f.Dir).Load()
		if err != nil {
			t.Fatalf("stage %d: plan does not load: %v", st, err)
		}
		want := 5 // root, one component, three functions
		if st == Planning || st == Waiting {
			want = 0
		}
		if len(tr.Nodes) != want {
			t.Errorf("stage %d: %d nodes, want %d", st, len(tr.Nodes), want)
		}
	}
}

func TestBuildWritesRuntimeAndArtifacts(t *testing.T) {
	root := t.TempDir()
	f := Build(t, root, Building)
	for _, p := range []string{
		"greeting/fn-greet.runtime.json", "greeting/fn-bye.runtime.json", "greeting/fn-late.runtime.json",
		"_state/events.jsonl", "_state/calls.jsonl", "_state/executor.json", "approval.json",
		"attempts/fn-bye/1/check-output.txt", "attempts/fn-bye/1/attempt.json",
	} {
		if _, err := os.Stat(filepath.Join(f.Dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "report.json")); err == nil {
		t.Error("a building run must not have report.json")
	}
	v := Build(t, t.TempDir(), Verified)
	if _, err := os.Stat(filepath.Join(v.Dir, "report.json")); err != nil {
		t.Errorf("verified run has no report.json: %v", err)
	}
}

func TestDigestSeesAWrite(t *testing.T) {
	f := Build(t, t.TempDir(), Planned)
	before := Digest(t, f.Dir)
	if Digest(t, f.Dir) != before {
		t.Fatal("digest is not stable")
	}
	if err := os.WriteFile(filepath.Join(f.Dir, "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Digest(t, f.Dir) == before {
		t.Fatal("digest did not change after a write")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/runview/runviewtest/... -count=1`
Expected: build failure, `undefined: Build`.

- [ ] **Step 3: Write `build.go`**

```go
// Package runviewtest builds run folders for tests with the real writers, so a
// change to the layout of a run folder breaks the tests of every reader.
package runviewtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/artifacts"
	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

// RunID is the id of every fixture run unless BuildID is used.
const RunID = "gm-2026-10-08-001"

// T0 is the fixed clock of the fixtures' attempts and files.
var T0 = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// Stage says how far the fixture run got.
type Stage int

const (
	Planning Stage = iota
	Waiting
	Planned
	Building
	Verified
	Failed
	HarnessFault
)

// Fixture is a built run folder. BB writes more runtime state into it, which
// also appends to its _state/events.jsonl.
type Fixture struct {
	Root, Dir, RunID string
	BB               *blackboard.FS
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	must(t, runfs.WriteFileAtomic(path, data))
}

func writeJSON(t testing.TB, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	must(t, err)
	writeFile(t, path, append(b, '\n'))
}

// Build builds the fixture run RunID under root/.gophermind.
func Build(t testing.TB, root string, stage Stage) Fixture { return BuildID(t, root, RunID, stage) }

// BuildID builds a fixture run with the given id.
func BuildID(t testing.TB, root, runID string, stage Stage) Fixture {
	t.Helper()
	dir := filepath.Join(root, ".gophermind", runID)
	must(t, os.MkdirAll(filepath.Join(dir, "_state"), 0o700))
	writeFile(t, filepath.Join(dir, "brief.md"), []byte("# Greeter\n\n## Features\n\n### Greeting\n- Greets a visitor by name.\n"))
	f := Fixture{Root: root, Dir: dir, RunID: runID}

	if stage == Planning || stage == Waiting {
		waiting := ""
		if stage == Waiting {
			waiting = "clarify"
		}
		writeJSON(t, filepath.Join(dir, "_state", "status.json"),
			map[string]any{"allow_public": false, "ledger_errors": 0, "waiting": waiting, "planned_at": ""})
		return f
	}

	writePlan(t, dir, runID)
	writeJSON(t, filepath.Join(dir, "requirements.json"),
		[]planner.Requirement{{ID: "F1", Kind: planner.ReqFeature, Name: "Greeting", Text: "Greets a visitor by name.", Line: 5}})
	writeJSON(t, filepath.Join(dir, "coverage.json"),
		planner.CoverageFile{Covered: []planner.Covered{{Requirement: "F1", Nodes: []string{"fn-greet"}}}, RootTests: []planner.RootTest{}, Warnings: []string{}})
	writeJSON(t, filepath.Join(dir, "_state", "status.json"),
		map[string]any{"allow_public": false, "ledger_errors": 0, "waiting": "", "planned_at": runfs.TS(T0)})
	writeJSON(t, filepath.Join(dir, "approval.json"),
		map[string]any{"approved_at": runfs.TS(T0.Add(time.Minute)), "approved_by": "test", "plan_hash": "abc"})
	if stage == Planned {
		return f
	}

	writeJSON(t, filepath.Join(dir, "_state", "executor.json"), map[string]any{"started_at": runfs.TS(T0.Add(2 * time.Minute))})
	ctx := context.Background()
	bb := blackboard.NewFS(runfs.Fixed(dir))
	f.BB = bb
	must(t, bb.InitRun(ctx, runID, []string{"fn-greet", "fn-bye", "fn-late"}, map[string]int{"fn-greet": 0, "fn-bye": 1, "fn-late": 1}))

	// fn-greet: one failed attempt, then a passing one, then verified.
	toInProgress(t, bb, runID, "fn-greet")
	must(t, bb.AppendAttempt(ctx, runID, "fn-greet", attempt(1, blackboard.VerdictFail, "compile: undefined: name")))
	must(t, bb.AppendAttempt(ctx, runID, "fn-greet", attempt(2, blackboard.VerdictPass, "")))
	must(t, bb.SetResult(ctx, runID, "fn-greet", blackboard.Result{FilesChanged: []string{"greeting/fn-greet.go"}, Commit: "abc1234"}))
	must(t, bb.SetStatus(ctx, runID, "fn-greet", blackboard.StatusInProgress, blackboard.StatusVerified))

	w := artifacts.New(dir, true, 0, nil)
	_, err := w.Save("fn-greet", artifacts.Meta{Provider: "mini", Model: "qwen3.6", Order: 1, Verdict: "fail", Class: "compile", Reason: "undefined: name", StartedAt: T0, DurationMS: 900},
		artifacts.Files{Raw: "reply one", CheckOutput: "greeting/fn-greet.go:3: undefined: name"})
	must(t, err)

	led := ledger.NewFS(runfs.Fixed(dir))
	for i, n := range []string{"fn-greet", "fn-greet"} {
		c := ledger.Call{RunID: runID, At: T0.Add(time.Duration(i) * time.Minute), Stage: "implement:" + n, NodeID: n, Scope: "node",
			TaskType: "implement", NodeClass: "pure", Provider: "mini", ModelRequested: "qwen3.6", ModelServed: "qwen3.6",
			DurationMS: 900, Outcome: ledger.OutcomeOK}
		must(t, led.Record(ctx, &c))
	}

	switch stage {
	case Building:
		toInProgress(t, bb, runID, "fn-bye")
		must(t, bb.AppendAttempt(ctx, runID, "fn-bye", attempt(1, blackboard.VerdictFail, "test: TestBye failed")))
		_, err := w.Save("fn-bye", artifacts.Meta{Provider: "mini", Model: "qwen3.6", Order: 1, Verdict: "fail", Class: "test", Reason: "TestBye failed", StartedAt: T0, DurationMS: 1300},
			artifacts.Files{Raw: "reply for bye", CheckOutput: "--- FAIL: TestBye\n    bye_test.go:9: want bye, got hello"})
		must(t, err)
	case Verified:
		for _, id := range []string{"fn-bye", "fn-late"} {
			toInProgress(t, bb, runID, id)
			must(t, bb.AppendAttempt(ctx, runID, id, attempt(1, blackboard.VerdictPass, "")))
			must(t, bb.SetResult(ctx, runID, id, blackboard.Result{FilesChanged: []string{"greeting/" + id + ".go"}, Commit: "def5678"}))
			must(t, bb.SetStatus(ctx, runID, id, blackboard.StatusInProgress, blackboard.StatusVerified))
		}
		WriteReport(t, f, "verified", "", 0, report.NodeCounts{Total: 3, Verified: 3})
	case Failed, HarnessFault:
		toInProgress(t, bb, runID, "fn-bye")
		must(t, bb.AppendAttempt(ctx, runID, "fn-bye", attempt(1, blackboard.VerdictFail, "test: TestBye failed")))
		must(t, bb.SetStatus(ctx, runID, "fn-bye", blackboard.StatusInProgress, blackboard.StatusFailed))
		reason, code := "leaf_failed", 1
		if stage == HarnessFault {
			reason, code = "harness_fault", 7
		}
		WriteReport(t, f, "failed", reason, code, report.NodeCounts{Total: 3, Verified: 1, Failed: 1, Blocked: 1})
	}
	return f
}

// WriteReport writes report.json into the fixture, as the executor does when a
// run ends. A test uses it to finish a Building run while a reader is watching.
func WriteReport(t testing.TB, f Fixture, status, reason string, code int, n report.NodeCounts) {
	t.Helper()
	rep := report.Report{
		SchemaVersion: report.SchemaVersion, RunID: f.RunID,
		StartedAt: runfs.TS(T0.Add(2 * time.Minute)), FinishedAt: runfs.TS(T0.Add(20 * time.Minute)),
		Status: status, StopReason: reason, ExitCode: code, Sandbox: "none",
		Requirements: report.Coverage{Covered: 1, Total: 1}, Waves: 2, Nodes: n,
		ByTaskType: []report.TaskModel{}, Leaves: []report.LeafModel{},
	}
	must(t, report.Write(f.Dir, rep, report.WriteOptions{RepoRoot: f.Root}))
}

// Verify takes one node of a built fixture from its current pending state to
// verified through the real transitions (each appends to the event log).
func Verify(t testing.TB, f Fixture, id string) {
	t.Helper()
	ctx := context.Background()
	toInProgress(t, f.BB, f.RunID, id)
	must(t, f.BB.AppendAttempt(ctx, f.RunID, id, attempt(1, blackboard.VerdictPass, "")))
	must(t, f.BB.SetResult(ctx, f.RunID, id, blackboard.Result{FilesChanged: []string{"greeting/" + id + ".go"}, Commit: "fed9876"}))
	must(t, f.BB.SetStatus(ctx, f.RunID, id, blackboard.StatusInProgress, blackboard.StatusVerified))
}

func toInProgress(t testing.TB, bb *blackboard.FS, runID, id string) {
	t.Helper()
	ctx := context.Background()
	must(t, bb.SetStatus(ctx, runID, id, blackboard.StatusPending, blackboard.StatusReady))
	ok, err := bb.Claim(ctx, runID, id, "worker-1")
	must(t, err)
	if !ok {
		t.Fatalf("claim of %s lost", id)
	}
	must(t, bb.SetStatus(ctx, runID, id, blackboard.StatusClaimed, blackboard.StatusInProgress))
}

func attempt(order int, v blackboard.Verdict, reason string) blackboard.Attempt {
	a := blackboard.Attempt{
		Model: "qwen3.6", Provider: "mini", Order: order, StartedAt: T0.Add(time.Duration(order) * time.Minute),
		DurationMS: 1200, Verdict: v, TestsTotal: 2, FailureReason: reason,
	}
	if v == blackboard.VerdictPass {
		a.TestsPassed = 2
	} else {
		a.TestsPassed = 1
		a.FailedTests = []string{"TestBye"}
	}
	return a
}

func writePlan(t testing.TB, dir, runID string) {
	t.Helper()
	st := tree.NewStore(dir)
	greetExtra := `,"model_tier":"standard","node_class":"pure","requirement_ids":["F1"],` +
		`"rationale":"Serves the greeting feature so a visitor is welcomed by name.",` +
		`"construction":{"approach_chosen":"Concatenate a fixed salutation and the name.","steps":["Trim the name","Return the greeting"]}`
	for _, js := range []string{
		fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"root","children":["greeting"],"title":"Greeter","description":"A tiny greeting service.","brief_ref":"#overview","status":"pending","wave":0,`+
			`"decisions":[{"id":"q1","question":"Which store?","kind":"decision","options":["memory","postgres"],"recommended":"memory","answer":"memory","answered_by":"accepted","round":1,"raised_by":"clarify","settled_at":"2026-10-08T00:00:00Z"}]}`, runID),
		fmt.Sprintf(`{"spec_version":"2.0","id":"greeting","kind":"component","parent":%q,"children":["fn-greet","fn-bye","fn-late"],"title":"Greeting","description":"Greeting functions.","brief_ref":"#greeting","status":"pending","wave":0}`, runID),
		fnJSON("fn-greet", 0, nil, greetExtra),
		fnJSON("fn-bye", 1, []string{"fn-greet"}, ""),
		fnJSON("fn-late", 1, []string{"fn-greet"}, ""),
	} {
		n, err := tree.ParseNode([]byte(js))
		must(t, err)
		must(t, st.Write(n))
	}
}

func fnJSON(id string, wave int, deps []string, extra string) string {
	if deps == nil {
		deps = []string{}
	}
	d, _ := json.Marshal(deps)
	return fmt.Sprintf(`{"spec_version":"2.0","id":%q,"kind":"function","parent":"greeting","title":"Function %s","description":"Implements %s.","brief_ref":"#greeting","status":"pending","wave":%d,"depends_on":%s,`+
		`"contract":{"package":"greeting","file":"greeting/%s.go","signature":"func F()","inputs":[],"outputs":[]},`+
		`"tests":[{"name":"t","level":"unit","given":"g","expect":"e","command":"go test ./greeting"}]%s}`, id, id, id, wave, d, id, extra)
}

// Digest hashes the names, sizes, modification times and contents of every
// file under dir, so a test can prove a reader wrote nothing.
func Digest(t testing.TB, dir string) string {
	t.Helper()
	h := sha256.New()
	var paths []string
	must(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	}))
	sort.Strings(paths)
	for _, p := range paths {
		fi, err := os.Lstat(p)
		must(t, err)
		fmt.Fprintf(h, "%s|%d|%d|%v\n", p, fi.Size(), fi.ModTime().UnixNano(), fi.Mode())
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			must(t, err)
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

If `tree.ParseNode` rejects a field the nodes carry (for example a B-plan group name differs from this worktree's schema), the failure names the field: fix the fixture JSON to the schema's current properties (`python3 -c "import json;print(list(json.load(open('gophermind-lib/briefv2/schema/task-node.schema.json'))['properties']))"`), never loosen the schema.

- [ ] **Step 4: Run to confirm green**

Run: `gt ./gophermind-lib/runview/... -count=1`
Expected: `ok` for `runview` and `runview/runviewtest`.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/runview
git commit -m "test(runview): runviewtest builds run folders with the real writers" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```


---

### Task 3: `Runs`, `Open`, `Summary` and `State`

**Files:**
- Create: `gophermind-lib/runview/runview.go`, `runview_test.go`

**Interfaces:**
- Produces: `type View`; `type Option func(*View)`; `WithPoll(time.Duration) Option`, `WithClock(func() time.Time) Option`; `Runs(root string) ([]RunSummary, error)`; `Open(root, runID string, opts ...Option) (*View, error)`; `(*View).Summary() RunSummary`; `(*View).State() StateBody`; `(*View).EventSeq() int64`; `(*View).Warnings() []string`; unexported `loaded`, `(*View).load`, `(*View).summarize`, `(*View).status`, `(*View).readJSON`, `docOf`, `str`, `strs`, `num`, `wireAttempt`, `wireResult`, `ts`, `nonNil`.
- Consumes: `tree.NewStore(dir).Load`, `blackboard.NewFS(runfs.Fixed(dir)).Get`, `report.Read`, `planner.ReadRequirements`, `planner.ReadCoverage`, `runfs.ReadLines`.

Status derivation (spec 5): no `approval.json` is `planning`, or `waiting` when `_state/status.json` names a stage in `waiting`; an approval and no `_state/executor.json` is `planned`; `_state/executor.json` and no `report.json` is `building`; a `report.json` gives its status and stop reason, except `failed` with `harness_fault`, which is the run status `harness_fault`. A `report.json` that does not validate is `failed` with stop reason `report_unreadable` and a warning, never a panic.

- [ ] **Step 1: Write the failing tests**

`gophermind-lib/runview/runview_test.go`:

```go
package runview_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

func TestRunsStatusPerStage(t *testing.T) {
	cases := []struct {
		stage          runviewtest.Stage
		status, reason string
	}{
		{runviewtest.Planning, "planning", ""},
		{runviewtest.Waiting, "waiting", ""},
		{runviewtest.Planned, "planned", ""},
		{runviewtest.Building, "building", ""},
		{runviewtest.Verified, "verified", ""},
		{runviewtest.Failed, "failed", "leaf_failed"},
		{runviewtest.HarnessFault, "harness_fault", "harness_fault"},
	}
	for _, c := range cases {
		root := t.TempDir()
		runviewtest.Build(t, root, c.stage)
		runs, err := runview.Runs(root)
		if err != nil || len(runs) != 1 {
			t.Fatalf("stage %d: %v, %d runs", c.stage, err, len(runs))
		}
		if runs[0].Status != c.status || runs[0].StopReason != c.reason {
			t.Errorf("stage %d: status %q reason %q, want %q %q", c.stage, runs[0].Status, runs[0].StopReason, c.status, c.reason)
		}
	}
}

func TestRunsNewestFirstAndEmpty(t *testing.T) {
	root := t.TempDir()
	runs, err := runview.Runs(root)
	if err != nil || runs == nil || len(runs) != 0 {
		t.Fatalf("no .gophermind folder: %v, %v (want an empty, non-nil list)", runs, err)
	}
	for _, id := range []string{"gm-2026-10-08-001", "gm-2026-10-09-001", "gm-2026-10-08-002"} {
		runviewtest.BuildID(t, root, id, runviewtest.Planned)
	}
	// Folders that are not runs are ignored: a stray directory and a symlink.
	_ = os.MkdirAll(filepath.Join(root, ".gophermind", "notes"), 0o700)
	_ = os.Symlink(filepath.Join(root, ".gophermind", "gm-2026-10-08-001"), filepath.Join(root, ".gophermind", "gm-2026-10-10-001"))
	runs, err = runview.Runs(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range runs {
		got = append(got, r.RunID)
	}
	want := "gm-2026-10-09-001,gm-2026-10-08-002,gm-2026-10-08-001"
	if strings.Join(got, ",") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}

func TestSummaryOfABuildingRun(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	v, err := runview.Open(root, runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	s := v.Summary()
	if s.Title != "Greeter" || s.RepoName != filepath.Base(root) || s.Status != "building" {
		t.Errorf("summary = %+v", s)
	}
	if s.Waves != 2 || s.Counts.Nodes != 3 {
		t.Errorf("waves %d nodes %d, want 2 and 3", s.Waves, s.Counts.Nodes)
	}
	if s.Counts.ByStatus["verified"] != 1 || s.Counts.ByStatus["in_progress"] != 1 || s.Counts.ByStatus["pending"] != 1 {
		t.Errorf("by_status = %v", s.Counts.ByStatus)
	}
	if s.Coverage.Covered != 1 || s.Coverage.Total != 1 {
		t.Errorf("coverage = %+v", s.Coverage)
	}
	if s.PlannedAt == "" || s.StartedAt == "" || s.UpdatedAt == "" {
		t.Errorf("times = %q %q %q", s.PlannedAt, s.StartedAt, s.UpdatedAt)
	}
	if strings.Contains(s.RepoName, string(filepath.Separator)) {
		t.Errorf("repo_name %q must be a folder name, not a path", s.RepoName)
	}
}

func TestStateNodesInPlanOrder(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	v, err := runview.Open(root, runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	st := v.State()
	if st.SchemaVersion != 1 || st.Run == nil || st.GeneratedAt == "" {
		t.Fatalf("state = %+v", st)
	}
	var ids []string
	byID := map[string]runview.NodeSummary{}
	for _, n := range st.Nodes {
		ids = append(ids, n.ID)
		byID[n.ID] = n
	}
	if got := strings.Join(ids, ","); got != runviewtest.RunID+",greeting,fn-greet,fn-bye,fn-late" {
		t.Errorf("node order = %s", got)
	}
	g := byID["fn-greet"]
	if g.Status != "verified" || g.AttemptCount != 2 || g.LastAttempt == nil || g.LastAttempt.Verdict != "pass" ||
		g.Result == nil || g.Result.Commit != "abc1234" || g.ModelTier != "standard" || g.NodeClass != "pure" ||
		len(g.RequirementIDs) != 1 || g.RequirementIDs[0] != "F1" || g.Tests.Total != 1 {
		t.Errorf("fn-greet = %+v", g)
	}
	b := byID["fn-bye"]
	if b.Status != "in_progress" || b.ClaimedBy != "worker-1" || b.AttemptCount != 1 || b.LastAttempt == nil ||
		b.LastAttempt.Verdict != "fail" || b.LastAttempt.FailureReason == "" || len(b.DependsOn) != 1 || b.Wave != 1 {
		t.Errorf("fn-bye = %+v", b)
	}
	l := byID["fn-late"]
	if l.Status != "pending" || l.LastAttempt != nil || l.Result != nil || l.DependsOn == nil || l.RequirementIDs == nil {
		t.Errorf("fn-late = %+v (lists must be empty, never null)", l)
	}
	raw, _ := os.ReadFile(filepath.Join(f.Dir, "_state", "events.jsonl"))
	if st.EventSeq != int64(bytes.Count(raw, []byte("\n"))) || st.EventSeq == 0 {
		t.Errorf("event_seq = %d, log has %d lines", st.EventSeq, bytes.Count(raw, []byte("\n")))
	}
}

func TestStateOfAPlanWithoutRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	v, _ := runview.Open(root, runviewtest.RunID)
	st := v.State()
	if st.EventSeq != 0 || len(st.Nodes) != 5 {
		t.Fatalf("event_seq %d, %d nodes", st.EventSeq, len(st.Nodes))
	}
	for _, n := range st.Nodes {
		if n.Status != "pending" {
			t.Errorf("%s status %q, want the plan file's pending", n.ID, n.Status)
		}
	}
}

func TestStateSurvivesDamagedFiles(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	if err := os.WriteFile(filepath.Join(f.Dir, "greeting", "fn-bye.runtime.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, _ := runview.Open(root, runviewtest.RunID)
	st := v.State()
	var bye runview.NodeSummary
	for _, n := range st.Nodes {
		if n.ID == "fn-bye" {
			bye = n
		}
	}
	if bye.Status != "pending" {
		t.Errorf("a damaged runtime file falls back to the plan status, got %q", bye.Status)
	}
	if len(st.Warnings) == 0 || !strings.Contains(strings.Join(st.Warnings, "|"), "fn-bye") {
		t.Errorf("warnings = %v, want one naming fn-bye", st.Warnings)
	}
	// A damaged plan file leaves an empty node list and a warning, not a failure.
	if err := os.WriteFile(filepath.Join(f.Dir, "greeting", "fn-late.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	st = v.State()
	if len(st.Nodes) != 0 || !strings.Contains(strings.Join(st.Warnings, "|"), "plan") {
		t.Errorf("nodes %d, warnings %v", len(st.Nodes), st.Warnings)
	}
}

func TestUnreadableReportIsFailedNotAPanic(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Verified)
	if err := os.WriteFile(filepath.Join(f.Dir, "report.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, _ := runview.Open(root, runviewtest.RunID)
	s := v.Summary()
	if s.Status != "failed" || s.StopReason != "report_unreadable" {
		t.Errorf("summary = %q %q", s.Status, s.StopReason)
	}
}

func TestStateAndRunsNeverWrite(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	before := runviewtest.Digest(t, root)
	if _, err := runview.Runs(root); err != nil {
		t.Fatal(err)
	}
	v, _ := runview.Open(root, runviewtest.RunID)
	_ = v.State()
	_ = v.Summary()
	_ = v.EventSeq()
	if runviewtest.Digest(t, root) != before {
		t.Fatal("a reader changed the run folder")
	}
	_ = f
}

func TestOpenRefusesBadIDs(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	for _, id := range []string{"..", "../gm-2026-10-08-001", "gm-2026-10-08-001/../..", "x"} {
		if _, err := runview.Open(root, id); err == nil {
			t.Errorf("Open(%q) succeeded", id)
		}
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/runview/ -count=1`
Expected: build failure, `undefined: runview.Runs`.

- [ ] **Step 3: Write `runview.go`**

```go
package runview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

const maxWarnings = 20

// View reads one run folder. Every method reads fresh from disk (there is no
// cache to go stale) and none writes.
type View struct {
	root, dir, runID string
	bb               *blackboard.FS
	poll             time.Duration
	now              func() time.Time

	mu       sync.Mutex
	warnings []string
}

// Option configures Open.
type Option func(*View)

// WithPoll sets how often Follow looks for new event lines (default 250 ms).
func WithPoll(d time.Duration) Option {
	return func(v *View) {
		if d > 0 {
			v.poll = d
		}
	}
}

// WithClock replaces the clock used for generated_at and the running report.
func WithClock(now func() time.Time) Option { return func(v *View) { v.now = now } }

// Open returns a view of the run runID under root (the repo root).
func Open(root, runID string, opts ...Option) (*View, error) {
	dir, err := runDirOf(root, runID)
	if err != nil {
		return nil, err
	}
	v := &View{
		root: root, dir: dir, runID: runID,
		bb:   blackboard.NewFS(runfs.Fixed(dir)),
		poll: 250 * time.Millisecond,
		now:  func() time.Time { return time.Now().UTC() },
	}
	for _, o := range opts {
		o(v)
	}
	return v, nil
}

// Runs lists the runs under root/.gophermind, newest first (run ids sort by
// date and sequence). A repo with none gives an empty, non-nil list. A folder
// that is not a run, or is a link, is skipped.
func Runs(root string) ([]RunSummary, error) {
	base := filepath.Join(root, ".gophermind")
	out := []RunSummary{}
	if !realDir(base) {
		return out, nil
	}
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if !ValidRunID(e.Name()) {
			continue
		}
		v, err := Open(root, e.Name())
		if err != nil {
			continue
		}
		out = append(out, v.Summary())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID > out[j].RunID })
	return out, nil
}

func (v *View) eventsPath() string { return filepath.Join(v.dir, "_state", "events.jsonl") }

// warn records something that was skipped, once, without echoing any content.
func (v *View) warn(msg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, w := range v.warnings {
		if w == msg {
			return
		}
	}
	if len(v.warnings) < maxWarnings {
		v.warnings = append(v.warnings, msg)
	}
}

// Warnings returns what this view skipped so far: a corrupt event line, an
// unreadable runtime file or plan. Messages carry ids and line numbers only.
func (v *View) Warnings() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.warnings...)
}

// EventSeq is the number of lines in _state/events.jsonl.
func (v *View) EventSeq() int64 {
	lines, _, err := runfs.ReadLines(v.eventsPath(), 0)
	if err != nil {
		return 0
	}
	return int64(len(lines))
}

type loaded struct {
	t    *tree.Tree
	rows map[string]blackboard.Row
}

// load reads the plan and every node's runtime file. A plan that cannot be
// read is an empty tree plus a warning; a runtime file that cannot be read
// leaves that node on its plan status plus a warning.
func (v *View) load(ctx context.Context) loaded {
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		v.warn("plan: the node files cannot be read")
		t = &tree.Tree{Nodes: map[string]tree.Node{}}
	}
	rows := make(map[string]blackboard.Row, len(t.Nodes))
	for id := range t.Nodes {
		r, err := v.bb.Get(ctx, v.runID, id)
		switch {
		case err == nil:
			rows[id] = r
		case errors.Is(err, blackboard.ErrNotFound):
		default:
			v.warn("node " + id + ": the runtime file cannot be read")
		}
	}
	return loaded{t: t, rows: rows}
}

func kindRank(k tree.Kind) int {
	switch k {
	case tree.KindRoot:
		return 0
	case tree.KindComponent:
		return 1
	}
	return 2
}

func (v *View) summarize(l loaded) []NodeSummary {
	out := make([]NodeSummary, 0, len(l.t.Nodes))
	for id, n := range l.t.Nodes {
		doc := docOf(n)
		ns := NodeSummary{
			ID: id, Kind: string(n.Kind), Parent: n.Parent, Title: str(doc, "title"), Status: str(doc, "status"),
			ModelTier: str(doc, "model_tier"), NodeClass: str(doc, "node_class"),
			DependsOn: nonNil(n.DependsOn), RequirementIDs: strs(doc, "requirement_ids"),
			Revision: num(doc, "revision"), Tests: TestCount{Total: arrLen(doc, "tests")},
		}
		if n.Wave != nil {
			ns.Wave = *n.Wave
		}
		if r, ok := l.rows[id]; ok {
			ns.Status, ns.Revision = string(r.Status), r.Revision
			if r.Claim != nil {
				ns.ClaimedBy = r.Claim.Worker
			}
			ns.AttemptCount = len(r.Attempts)
			if len(r.Attempts) > 0 {
				a := wireAttempt(r.Attempts[len(r.Attempts)-1])
				ns.LastAttempt = &a
			}
			if r.Result != nil {
				res := wireResult(r.Result)
				ns.Result = &res
			}
		}
		out = append(out, ns)
	}
	rank := func(k string) int { return kindRank(tree.Kind(k)) }
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) < rank(b.Kind)
		}
		if a.Wave != b.Wave {
			return a.Wave < b.Wave
		}
		return a.ID < b.ID
	})
	return out
}

type statusFile struct {
	Waiting   string `json:"waiting"`
	PlannedAt string `json:"planned_at"`
}

type execFile struct {
	StartedAt string `json:"started_at"`
}

// readJSON decodes a small run file; false when it is missing or invalid.
func (v *View) readJSON(rel string, out any) bool {
	data, _, err := readLimited(filepath.Join(v.dir, filepath.FromSlash(rel)), 1<<20)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, out) == nil
}

func (v *View) exists(rel string) bool { return realFile(filepath.Join(v.dir, filepath.FromSlash(rel))) }

// status derives the run status from which files exist (spec section 5).
func (v *View) status() (status, reason string) {
	if v.exists(report.FileName) {
		rep, err := report.Read(v.dir)
		if err != nil {
			v.warn("report.json: it cannot be read")
			return StatusFailed, "report_unreadable"
		}
		if rep.Status == "failed" && rep.StopReason == "harness_fault" {
			return StatusHarnessFault, rep.StopReason
		}
		return rep.Status, rep.StopReason
	}
	if v.exists("_state/executor.json") {
		return StatusBuilding, ""
	}
	if v.exists("approval.json") {
		return StatusPlanned, ""
	}
	var st statusFile
	if v.readJSON("_state/status.json", &st) && st.Waiting != "" {
		return StatusWaiting, ""
	}
	return StatusPlanning, ""
}

func (v *View) title(l loaded) string {
	for _, n := range l.t.Nodes {
		if n.Kind == tree.KindRoot {
			if s := str(docOf(n), "title"); s != "" {
				return s
			}
		}
	}
	if data, _, err := readLimited(filepath.Join(v.dir, "brief.md"), 64<<10); err == nil {
		for _, ln := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(ln, "# ") {
				return strings.TrimSpace(ln[2:])
			}
		}
	}
	return v.runID
}

func (v *View) repoName() string {
	abs, err := filepath.Abs(v.root)
	if err != nil {
		abs = v.root
	}
	return filepath.Base(abs)
}

func (v *View) updatedAt() string {
	var newest time.Time
	for _, rel := range []string{"brief.md", "approval.json", "report.json", "_state/events.jsonl", "_state/status.json", "_state/executor.json"} {
		if fi, err := os.Stat(filepath.Join(v.dir, filepath.FromSlash(rel))); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return ts(newest)
}

func (v *View) summaryOf(l loaded, nodes []NodeSummary) RunSummary {
	s := RunSummary{RunID: v.runID, Title: v.title(l), RepoName: v.repoName(), Counts: Counts{ByStatus: map[string]int{}}}
	s.Status, s.StopReason = v.status()
	waves := map[int]bool{}
	for _, n := range nodes {
		if n.Kind != string(tree.KindFunction) {
			continue
		}
		s.Counts.Nodes++
		s.Counts.ByStatus[n.Status]++
		waves[n.Wave] = true
	}
	s.Waves = len(waves)
	var st statusFile
	if v.readJSON("_state/status.json", &st) {
		s.PlannedAt = st.PlannedAt
	}
	var ex execFile
	if v.readJSON("_state/executor.json", &ex) {
		s.StartedAt = ex.StartedAt
	}
	if s.StartedAt == "" && v.exists(report.FileName) {
		if rep, err := report.Read(v.dir); err == nil {
			s.StartedAt = rep.StartedAt
		}
	}
	if reqs, err := planner.ReadRequirements(v.dir); err == nil {
		s.Coverage.Total = len(reqs)
	}
	if cov, err := planner.ReadCoverage(v.dir); err == nil {
		s.Coverage.Covered = len(cov.Covered)
	}
	s.UpdatedAt = v.updatedAt()
	return s
}

// Summary is the run's header: status, times, waves, counts and coverage.
func (v *View) Summary() RunSummary {
	l := v.load(context.Background())
	return v.summaryOf(l, v.summarize(l))
}

// State is GET /pipeline/state: the summary plus every node. The event count
// is read first, so resuming the stream at EventSeq misses nothing.
func (v *View) State() StateBody {
	seq := v.EventSeq()
	l := v.load(context.Background())
	nodes := v.summarize(l)
	sum := v.summaryOf(l, nodes)
	return StateBody{
		SchemaVersion: SchemaVersion, GeneratedAt: v.now().UTC().Format(time.RFC3339Nano),
		EventSeq: seq, Run: &sum, Nodes: nodes, Warnings: v.Warnings(),
	}
}

func docOf(n tree.Node) map[string]any {
	raw, err := n.Marshal()
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func num(m map[string]any, k string) int { f, _ := m[k].(float64); return int(f) }

func arrLen(m map[string]any, k string) int { a, _ := m[k].([]any); return len(a) }

func strs(m map[string]any, k string) []string {
	arr, _ := m[k].([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return runfs.TS(t)
}

func wireAttempt(a blackboard.Attempt) Attempt {
	return Attempt{
		Model: a.Model, Provider: a.Provider, Verdict: string(a.Verdict), FailureReason: a.FailureReason,
		DurationMS: a.DurationMS, StartedAt: ts(a.StartedAt), Order: a.Order, Revision: a.Revision,
		TestsPassed: a.TestsPassed, TestsTotal: a.TestsTotal, FailedTests: nonNil(a.FailedTests),
	}
}

func wireResult(r *blackboard.Result) NodeResult {
	return NodeResult{Commit: r.Commit, FilesChanged: nonNil(r.FilesChanged)}
}
```

- [ ] **Step 4: Run to confirm green**

Run: `gt ./gophermind-lib/runview/... -count=1`
Expected: `ok` for all three packages.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/runview
git commit -m "feat(runview): Runs, Open, Summary and State read a run folder without writing" -m "Rulings: counts cover function nodes; newest run is the highest run id; state carries event_seq and warnings." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 4: `Node`, `ArtifactNames`, `Artifact`, `Decisions`, `Report` and the contract goldens

**Files:**
- Create: `gophermind-lib/runview/detail.go`, `detail_test.go`, `testdata/golden/*.json` (written by the test with `-update`)

**Interfaces:**
- Produces: `(*View).Node(id string) (NodeBody, error)`; `(*View).ArtifactNames(id string) []ArtifactEntry`; `(*View).Artifact(id string, attempt int, file string) (string, error)`; `(*View).Decisions() (DecisionsBody, error)`; `(*View).Report() ([]byte, error)`.
- Consumes: Task 3's `load`, `docOf`, `readLimited`, `wireAttempt`, `wireResult`, `v.status`; `ledger.NewFS(...).List`; `report.Build`.

- [ ] **Step 1: Write the failing tests**

`gophermind-lib/runview/detail_test.go`:

```go
package runview_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

func fixedClock() func() time.Time {
	at := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func open(t *testing.T, st runviewtest.Stage) (*runview.View, runviewtest.Fixture) {
	t.Helper()
	root := t.TempDir()
	f := runviewtest.Build(t, root, st)
	v, err := runview.Open(root, runviewtest.RunID, runview.WithClock(fixedClock()))
	if err != nil {
		t.Fatal(err)
	}
	return v, f
}

func TestNodeBodyCarriesGroupsRuntimeAndArtifacts(t *testing.T) {
	v, _ := open(t, runviewtest.Building)
	b, err := v.Node("fn-bye")
	if err != nil {
		t.Fatal(err)
	}
	if b.SchemaVersion != 1 || b.Runtime == nil || b.Runtime.Status != "in_progress" || len(b.Runtime.Attempts) != 1 {
		t.Fatalf("body = %+v", b)
	}
	if len(b.Artifacts) != 1 || b.Artifacts[0].Attempt != 1 ||
		strings.Join(b.Artifacts[0].Files, ",") != "reply.txt,check-output.txt,attempt.json" {
		t.Errorf("artifacts = %+v", b.Artifacts)
	}
	var doc map[string]any
	if err := json.Unmarshal(b.Node, &doc); err != nil || doc["id"] != "fn-bye" {
		t.Errorf("node doc = %v, %v", doc, err)
	}
	g, err := v.Node("fn-greet")
	if err != nil {
		t.Fatal(err)
	}
	var gd map[string]any
	_ = json.Unmarshal(g.Node, &gd)
	if gd["rationale"] == nil || gd["construction"] == nil {
		t.Errorf("the full node, groups included, must be returned: %v", gd)
	}
	r, err := v.Node(runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var rd map[string]any
	_ = json.Unmarshal(r.Node, &rd)
	if ds, _ := rd["decisions"].([]any); len(ds) != 1 {
		t.Errorf("the root node must carry its decisions: %v", rd["decisions"])
	}
}

func TestNodeBeforeTheExecutorStartedHasNoRuntime(t *testing.T) {
	v, _ := open(t, runviewtest.Planned)
	b, err := v.Node("fn-greet")
	if err != nil || b.Runtime != nil || b.Artifacts == nil || len(b.Artifacts) != 0 {
		t.Errorf("body = %+v, %v (runtime nil, artifacts an empty list)", b, err)
	}
}

func TestNodeRefusesBadAndUnknownIDs(t *testing.T) {
	v, _ := open(t, runviewtest.Building)
	for _, id := range []string{"", "../x", "Fn", "a/b", ".."} {
		if _, err := v.Node(id); !errors.Is(err, runview.ErrBadID) {
			t.Errorf("Node(%q) = %v, want ErrBadID", id, err)
		}
	}
	if _, err := v.Node("nope"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("unknown node = %v, want ErrNotFound", err)
	}
}

func TestArtifact(t *testing.T) {
	v, f := open(t, runviewtest.Building)
	txt, err := v.Artifact("fn-bye", 1, "check-output.txt")
	if err != nil || !strings.Contains(txt, "want bye, got hello") {
		t.Fatalf("artifact = %q, %v", txt, err)
	}
	bad := []struct {
		id      string
		attempt int
		file    string
	}{
		{"fn-bye", 0, "reply.txt"}, {"fn-bye", -1, "reply.txt"}, {"fn-bye", 1, "prompt.txt"},
		{"fn-bye", 1, "../reply.txt"}, {"../fn-bye", 1, "reply.txt"}, {"fn-bye", 1, ""},
	}
	for _, c := range bad {
		if _, err := v.Artifact(c.id, c.attempt, c.file); !errors.Is(err, runview.ErrBadID) {
			t.Errorf("Artifact(%+v) = %v, want ErrBadID", c, err)
		}
	}
	if _, err := v.Artifact("fn-bye", 9, "reply.txt"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("missing attempt = %v, want ErrNotFound", err)
	}
	if _, err := v.Artifact("fn-bye", 1, "reply.go"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("file not saved = %v, want ErrNotFound", err)
	}
	// A symbolic link in the attempt folder is refused and not listed.
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(f.Dir, "attempts", "fn-bye", "1", "reply.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Artifact("fn-bye", 1, "reply.go"); !errors.Is(err, runview.ErrNotFound) {
		t.Errorf("symlinked artifact = %v, want ErrNotFound", err)
	}
	for _, a := range v.ArtifactNames("fn-bye") {
		for _, name := range a.Files {
			if name == "reply.go" {
				t.Error("a symlinked file must not be listed")
			}
		}
	}
}

func TestDecisionsAreTheRootRecords(t *testing.T) {
	v, _ := open(t, runviewtest.Building)
	d, err := v.Decisions()
	if err != nil || d.SchemaVersion != 1 || len(d.Decisions) != 1 {
		t.Fatalf("decisions = %+v, %v", d, err)
	}
	var rec map[string]any
	_ = json.Unmarshal(d.Decisions[0], &rec)
	if rec["id"] != "q1" || rec["answer"] != "memory" {
		t.Errorf("record = %v", rec)
	}
}

func TestReportFinalIsVerbatimAndRunningIsPartial(t *testing.T) {
	v, f := open(t, runviewtest.Verified)
	got, err := v.Report()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(f.Dir, "report.json"))
	if !bytes.Equal(got, want) {
		t.Error("the final report must be served byte for byte")
	}
	var fin runview.ReportBody
	if err := json.Unmarshal(got, &fin); err != nil || !fin.IsComplete() || fin.Status != "verified" {
		t.Errorf("final = %+v, %v", fin, err)
	}

	v2, _ := open(t, runviewtest.Building)
	part, err := v2.Report()
	if err != nil {
		t.Fatal(err)
	}
	var pr runview.ReportBody
	if err := json.Unmarshal(part, &pr); err != nil {
		t.Fatal(err)
	}
	if pr.IsComplete() || pr.RunID != runviewtest.RunID || pr.Status != "building" || len(pr.ByTaskType) == 0 {
		t.Errorf("partial = complete %v, run %q, status %q, %d task rows", pr.IsComplete(), pr.RunID, pr.Status, len(pr.ByTaskType))
	}
}

func TestReportWithAnUnreadableFileIsAnErrorNotAGuess(t *testing.T) {
	v, f := open(t, runviewtest.Verified)
	if err := os.WriteFile(filepath.Join(f.Dir, "report.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Report(); err == nil || errors.Is(err, runview.ErrNotFound) {
		t.Errorf("Report = %v, want a plain error", err)
	}
}

func TestDetailReadersNeverWrite(t *testing.T) {
	v, f := open(t, runviewtest.Building)
	before := runviewtest.Digest(t, f.Root)
	_, _ = v.Node("fn-bye")
	_ = v.ArtifactNames("fn-bye")
	_, _ = v.Artifact("fn-bye", 1, "reply.txt")
	_, _ = v.Decisions()
	_, _ = v.Report()
	if runviewtest.Digest(t, f.Root) != before {
		t.Fatal("a reader changed the run folder")
	}
}

// golden compares v, marshaled as indented JSON, with testdata/golden/<name>.
// The desktop client tests decode the same files, so a change to the contract
// is a visible diff here and a red test there. With -update the file is
// rewritten; review the diff before committing it. A field that varies between
// runs is zeroed by the caller, never by adding a clock to production code.
func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s: run `go test ./gophermind-lib/runview -run TestGolden -update`", name)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from its golden; if the change is intended run with -update and review the diff", name)
	}
}

func TestGoldenContract(t *testing.T) {
	v, f := open(t, runviewtest.Building)

	runs, _ := runview.Runs(f.Root)
	for i := range runs {
		runs[i].UpdatedAt = "" // file modification time
		runs[i].RepoName = "repo"
	}
	golden(t, "runs.json", runview.RunsBody{SchemaVersion: runview.SchemaVersion, Runs: runs})

	st := v.State()
	st.Run.UpdatedAt, st.Run.RepoName = "", "repo"
	golden(t, "state.json", st)

	nb, err := v.Node("fn-bye")
	if err != nil {
		t.Fatal(err)
	}
	nb.Runtime.UpdatedAt, nb.Runtime.HeartbeatAt = "", "" // written at run time
	golden(t, "node-fn-bye.json", nb)

	db, _ := v.Decisions()
	golden(t, "decisions.json", db)

	partial, err := v.Report()
	if err != nil {
		t.Fatal(err)
	}
	var pr runview.ReportBody
	if err := json.Unmarshal(partial, &pr); err != nil {
		t.Fatal(err)
	}
	golden(t, "report-partial.json", pr)

	fv, _ := open(t, runviewtest.Verified)
	final, err := fv.Report()
	if err != nil {
		t.Fatal(err)
	}
	var fr runview.ReportBody
	if err := json.Unmarshal(final, &fr); err != nil {
		t.Fatal(err)
	}
	golden(t, "report-final.json", fr)
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/runview/ -count=1`
Expected: build failure, `v.Node undefined`.

- [ ] **Step 3: Write `detail.go`**

```go
package runview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

func runtimeOf(r blackboard.Row) RuntimeDoc {
	d := RuntimeDoc{
		Status: string(r.Status), Revision: r.Revision, HeartbeatAt: ts(r.HeartbeatAt), UpdatedAt: ts(r.UpdatedAt),
		Attempts: make([]Attempt, 0, len(r.Attempts)),
	}
	if r.Claim != nil {
		d.ClaimedBy = r.Claim.Worker
	}
	for _, a := range r.Attempts {
		d.Attempts = append(d.Attempts, wireAttempt(a))
	}
	if r.Result != nil {
		res := wireResult(r.Result)
		d.Result = &res
	}
	return d
}

// Node is GET /pipeline/node: the node file as stored (all groups, embedded
// decisions), its runtime file, and its saved artifacts.
func (v *View) Node(id string) (NodeBody, error) {
	if !ValidNodeID(id) {
		return NodeBody{}, ErrBadID
	}
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		return NodeBody{}, errors.New("runview: the plan cannot be read")
	}
	n, ok := t.Nodes[id]
	if !ok {
		return NodeBody{}, ErrNotFound
	}
	raw, err := n.Marshal()
	if err != nil {
		return NodeBody{}, errors.New("runview: the node cannot be encoded")
	}
	body := NodeBody{SchemaVersion: SchemaVersion, Node: raw, Artifacts: v.ArtifactNames(id)}
	row, err := v.bb.Get(context.Background(), v.runID, id)
	switch {
	case err == nil:
		rt := runtimeOf(row)
		body.Runtime = &rt
	case errors.Is(err, blackboard.ErrNotFound):
	default:
		v.warn("node " + id + ": the runtime file cannot be read")
	}
	return body, nil
}

// ArtifactNames lists the saved attempt folders of a node and which of the four
// servable files each holds. Links are not listed.
func (v *View) ArtifactNames(id string) []ArtifactEntry {
	out := []ArtifactEntry{}
	attempts := filepath.Join(v.dir, "attempts")
	base := filepath.Join(attempts, id)
	if !ValidNodeID(id) || !realDir(attempts) || !realDir(base) {
		return out
	}
	ents, err := os.ReadDir(base)
	if err != nil {
		return out
	}
	for _, e := range ents {
		n, err := strconv.Atoi(e.Name())
		if err != nil || n < 1 || strconv.Itoa(n) != e.Name() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if !realDir(dir) {
			continue
		}
		entry := ArtifactEntry{Attempt: n, Files: []string{}}
		for _, f := range artifactFiles {
			if realFile(filepath.Join(dir, f)) {
				entry.Files = append(entry.Files, f)
			}
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Attempt < out[j].Attempt })
	return out
}

// Artifact returns the text of one saved file (already scrubbed by the
// executor). id, attempt and file are validated; links are refused.
func (v *View) Artifact(id string, attempt int, file string) (string, error) {
	if !ValidNodeID(id) || attempt < 1 || !ValidArtifactName(file) {
		return "", ErrBadID
	}
	attempts := filepath.Join(v.dir, "attempts")
	nodeDir := filepath.Join(attempts, id)
	dir := filepath.Join(nodeDir, strconv.Itoa(attempt))
	if !realDir(attempts) || !realDir(nodeDir) || !realDir(dir) {
		return "", ErrNotFound
	}
	data, cut, err := readLimited(filepath.Join(dir, file), maxReadBytes)
	if err != nil {
		return "", err
	}
	if cut {
		data = append(data, []byte("\n[truncated by the server]\n")...)
	}
	return string(data), nil
}

// Decisions returns the root node's records: the whole conversation.
func (v *View) Decisions() (DecisionsBody, error) {
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		return DecisionsBody{}, errors.New("runview: the plan cannot be read")
	}
	out := DecisionsBody{SchemaVersion: SchemaVersion, Decisions: []json.RawMessage{}}
	for _, n := range t.Nodes {
		if n.Kind != tree.KindRoot {
			continue
		}
		arr, _ := docOf(n)["decisions"].([]any)
		for _, x := range arr {
			if b, err := json.Marshal(x); err == nil {
				out.Decisions = append(out.Decisions, b)
			}
		}
	}
	return out, nil
}

// Report is GET /pipeline/report. A finished run returns report.json byte for
// byte (after it validates); a running one returns a partial report built from
// the call log and the runtime files with "complete": false.
func (v *View) Report() ([]byte, error) {
	if v.exists(report.FileName) {
		if _, err := report.Read(v.dir); err != nil {
			return nil, errors.New("runview: report.json cannot be read")
		}
		data, _, err := readLimited(filepath.Join(v.dir, report.FileName), maxReadBytes)
		return data, err
	}
	return v.partialReport()
}

func (v *View) partialReport() ([]byte, error) {
	ctx := context.Background()
	l := v.load(ctx)
	rows := make([]blackboard.Row, 0, len(l.rows))
	for _, r := range l.rows {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].NodeID < rows[j].NodeID })
	calls, err := ledger.NewFS(runfs.Fixed(v.dir)).List(ctx, v.runID, ledger.Filter{})
	if err != nil {
		v.warn("calls.jsonl: it cannot be read")
		calls = nil
	}
	reqs, _ := planner.ReadRequirements(v.dir)
	cov, _ := planner.ReadCoverage(v.dir)
	status, reason := v.status()
	var started = v.now()
	var ex execFile
	if v.readJSON("_state/executor.json", &ex) {
		if at, err := runfs.ParseTS(ex.StartedAt); err == nil && !at.IsZero() {
			started = at
		}
	}
	waves := map[int]bool{}
	for _, n := range l.t.Nodes {
		if n.Kind == tree.KindFunction && n.Wave != nil {
			waves[*n.Wave] = true
		}
	}
	rep, err := report.Build(report.Input{
		RunID: v.runID, StartedAt: started, FinishedAt: v.now(), Status: status, StopReason: reason,
		Calls: calls, Rows: rows, Requirements: reqs, Coverage: cov, Waves: len(waves),
	})
	if err != nil {
		return nil, errors.New("runview: the partial report cannot be built")
	}
	incomplete := false
	return json.MarshalIndent(ReportBody{Report: rep, Complete: &incomplete}, "", "  ")
}
```

- [ ] **Step 4: Write the goldens and run**

Run: `gt ./gophermind-lib/runview/ -run TestGoldenContract -update -count=1` then open the six files under `gophermind-lib/runview/testdata/golden/` and check by eye: no absolute path (`/var/...`, `/Users/...`), no timestamp that depends on the run (only the fixed `2026-10-08` values), `"schema_version": 1` in all but the two reports (`2`), `"complete": false` only in `report-partial.json`. If a path or a varying time appears, zero it in `TestGoldenContract` and rerun with `-update`.
Then: `gt ./gophermind-lib/runview/... -count=1`
Expected: `ok` for all three packages, with the goldens compared (not rewritten).

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/runview
git commit -m "feat(runview): Node, Artifact, Decisions and Report, with golden contract files" -m "Rulings: the final report is served verbatim; a running run gets a partial report with complete:false." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 5: `Follow`, the live event stream

**Files:**
- Create: `gophermind-lib/runview/follow.go`, `follow_test.go`

**Interfaces:**
- Produces: `(*View).Follow(ctx context.Context, since int64) (<-chan Event, error)`.
- Consumes: Task 3's `load`, `status`, `warn`, `wireAttempt`, `wireResult`; `runfs.ReadLines`; `blackboard.FS.Get`.

Behavior (rulings 3 to 5 of this plan): lines of `_state/events.jsonl` are numbered from 1. Lines `<= since` are read to keep the per-node attempt counters right but produce no event. Each later line becomes `node-status`, `node-attempt` (the n-th attempt line of a node maps to its n-th attempt) or `node-result`, built from the node's runtime file as it is now. A line that is not JSON, names no valid node, has an unknown kind, or whose runtime file is unreadable is skipped and recorded in `Warnings()` by line number. After the replay a baseline of run status, active wave and report presence is taken without emitting anything; later changes emit `run-status`, `wave-changed` (`finished` for the old wave, then `started` for the new) and `run-report` (the body of `Report()`, once). The channel closes when `ctx` ends.

- [ ] **Step 1: Write the failing tests**

`gophermind-lib/runview/follow_test.go`:

```go
package runview_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

func follow(t *testing.T, st runviewtest.Stage, since func(*runview.View) int64) (*runview.View, runviewtest.Fixture, <-chan runview.Event, context.CancelFunc) {
	t.Helper()
	root := t.TempDir()
	f := runviewtest.Build(t, root, st)
	v, err := runview.Open(root, runviewtest.RunID, runview.WithPoll(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch, err := v.Follow(ctx, since(v))
	if err != nil {
		t.Fatal(err)
	}
	return v, f, ch, cancel
}

func next(t *testing.T, ch <-chan runview.Event) runview.Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("stream closed")
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
	}
	return runview.Event{}
}

// until reads events until pred accepts one, returning it and everything before.
func until(t *testing.T, ch <-chan runview.Event, pred func(runview.Event) bool) (runview.Event, []runview.Event) {
	t.Helper()
	var seen []runview.Event
	for {
		e := next(t, ch)
		if pred(e) {
			return e, seen
		}
		seen = append(seen, e)
	}
}

func ofType(typ string) func(runview.Event) bool {
	return func(e runview.Event) bool { return e.Type == typ }
}

func TestFollowFromZeroReplaysEveryLineOnce(t *testing.T) {
	v, _, ch, _ := follow(t, runviewtest.Building, func(*runview.View) int64 { return 0 })
	n := v.EventSeq()
	if n < 8 {
		t.Fatalf("fixture log has only %d lines", n)
	}
	var attempts int
	for i := int64(1); i <= n; i++ {
		e := next(t, ch)
		if e.Seq != i {
			t.Fatalf("event %d has seq %d", i, e.Seq)
		}
		if !strings.HasPrefix(e.Type, "node-") {
			t.Fatalf("replay produced %q; only node events are replayed", e.Type)
		}
		if e.Type == runview.EventNodeAttempt {
			var d runview.NodeAttemptData
			if err := json.Unmarshal(e.Data, &d); err != nil || d.Attempt.Model == "" {
				t.Fatalf("attempt data = %s, %v", e.Data, err)
			}
			attempts++
		}
	}
	if attempts != 3 { // fn-greet fail and pass, fn-bye fail
		t.Errorf("%d attempt events, want 3", attempts)
	}
	select {
	case e := <-ch:
		t.Fatalf("an extra event after the replay: %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFollowReplayShowsEachAttemptWithItsOwnData(t *testing.T) {
	_, _, ch, _ := follow(t, runviewtest.Building, func(*runview.View) int64 { return 0 })
	var verdicts []string
	for len(verdicts) < 3 {
		e := next(t, ch)
		if e.Type != runview.EventNodeAttempt {
			continue
		}
		var d runview.NodeAttemptData
		_ = json.Unmarshal(e.Data, &d)
		verdicts = append(verdicts, d.ID+":"+d.Attempt.Verdict)
	}
	if got := strings.Join(verdicts, ","); got != "fn-greet:fail,fn-greet:pass,fn-bye:fail" {
		t.Errorf("attempt events = %s (the n-th attempt line must map to the n-th attempt)", got)
	}
}

func TestFollowSinceSkipsEarlierLines(t *testing.T) {
	var n int64
	_, _, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { n = v.EventSeq(); return n - 2 })
	e1, e2 := next(t, ch), next(t, ch)
	if e1.Seq != n-1 || e2.Seq != n {
		t.Errorf("seqs = %d, %d; want %d, %d", e1.Seq, e2.Seq, n-1, n)
	}
}

func TestFollowLiveRunToTheEnd(t *testing.T) {
	_, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	ctx := context.Background()

	// fn-bye passes its tests.
	if err := f.BB.SetStatus(ctx, f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	e := next(t, ch)
	var st runview.NodeStatusData
	_ = json.Unmarshal(e.Data, &st)
	if e.Type != runview.EventNodeStatus || st.ID != "fn-bye" || st.Status != "verified" || st.Wave != 1 {
		t.Fatalf("event = %s %s", e.Type, e.Data)
	}

	// The last node finishes: wave 1 is over.
	runviewtest.Verify(t, f, "fn-late")
	w, _ := until(t, ch, ofType(runview.EventWaveChanged))
	var wc runview.WaveChangedData
	_ = json.Unmarshal(w.Data, &wc)
	if wc.Wave != 1 || wc.State != "finished" {
		t.Errorf("wave event = %+v, want wave 1 finished", wc)
	}

	// The executor writes its report: run-status, then run-report.
	runviewtest.WriteReport(t, f, "verified", "", 0, report.NodeCounts{Total: 3, Verified: 3})
	rs, _ := until(t, ch, ofType(runview.EventRunStatus))
	var rd runview.RunStatusData
	_ = json.Unmarshal(rs.Data, &rd)
	if rd.Status != "verified" {
		t.Errorf("run-status = %+v", rd)
	}
	rr, _ := until(t, ch, ofType(runview.EventRunReport))
	var body runview.ReportBody
	if err := json.Unmarshal(rr.Data, &body); err != nil || !body.IsComplete() || body.Status != "verified" {
		t.Errorf("run-report = %s, %v", rr.Data, err)
	}
	select {
	case e := <-ch:
		t.Errorf("run-report must come once, got %s after it", e.Type)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFollowSkipsACorruptLineAndReportsIt(t *testing.T) {
	v, f, ch, _ := follow(t, runviewtest.Building, func(v *runview.View) int64 { return v.EventSeq() })
	bad := v.EventSeq() + 1
	if err := runfs.AppendLine(filepath.Join(f.Dir, "_state", "events.jsonl"), []byte("this is not json")); err != nil {
		t.Fatal(err)
	}
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	e := next(t, ch)
	if e.Type != runview.EventNodeStatus || e.Seq != bad+1 {
		t.Fatalf("event after the corrupt line = %s seq %d, want node-status seq %d (line numbers stay true)", e.Type, e.Seq, bad+1)
	}
	joined := strings.Join(v.Warnings(), "|")
	if !strings.Contains(joined, "line "+itoa(bad)) || strings.Contains(joined, "this is not json") {
		t.Errorf("warnings = %q: want the line number, never the content", joined)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestFollowStopsWhenTheContextEnds(t *testing.T) {
	_, _, ch, cancel := follow(t, runviewtest.Planned, func(*runview.View) int64 { return 0 })
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("an event after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the channel did not close after the context was cancelled")
	}
}

func TestFollowNeverWrites(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	before := runviewtest.Digest(t, root)
	v, _ := runview.Open(root, runviewtest.RunID, runview.WithPoll(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := v.Follow(ctx, 0)
	for i := int64(0); i < v.EventSeq(); i++ {
		next(t, ch)
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
	if runviewtest.Digest(t, root) != before {
		t.Fatal("Follow changed the run folder")
	}
	_ = f
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/runview/ -run TestFollow -count=1`
Expected: build failure, `v.Follow undefined`.

- [ ] **Step 3: Write `follow.go`**

```go
package runview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

// logLine is one line of _state/events.jsonl, as blackboard.FS writes it.
type logLine struct {
	NodeID string `json:"node_id"`
	Kind   string `json:"kind"`
}

// Follow streams the run's events. Lines of _state/events.jsonl after the
// first `since` are turned into node events; run-status, wave-changed and
// run-report are derived from state as it changes after that. The channel is
// closed when ctx ends. Follow never writes.
func (v *View) Follow(ctx context.Context, since int64) (<-chan Event, error) {
	if since < 0 {
		since = 0
	}
	ch := make(chan Event)
	f := &follower{v: v, ctx: ctx, ch: ch, since: since, attempts: map[string]int{}, first: true, wave: -1}
	go func() {
		defer close(ch)
		f.run()
	}()
	return ch, nil
}

type follower struct {
	v     *View
	ctx   context.Context
	ch    chan<- Event
	since int64

	off, seq int64
	attempts map[string]int // attempt lines seen per node, replayed ones included
	waves    map[string]int

	first          bool
	status, reason string
	wave           int
	reported       bool
}

func (f *follower) run() {
	tick := time.NewTicker(f.v.poll)
	defer tick.Stop()
	for {
		if !f.step() {
			return
		}
		select {
		case <-f.ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (f *follower) send(e Event) bool {
	select {
	case f.ch <- e:
		return true
	case <-f.ctx.Done():
		return false
	}
}

func (f *follower) emit(typ string, data any) bool {
	b, err := json.Marshal(data)
	if err != nil {
		return true
	}
	return f.send(Event{Seq: f.seq, Type: typ, Data: b})
}

// step reads the new lines, emits their events, then the derived ones.
func (f *follower) step() bool {
	lines, next, err := runfs.ReadLines(f.v.eventsPath(), f.off)
	progressed := false
	if err == nil {
		f.off = next
		for _, ln := range lines {
			f.seq++
			progressed = true
			if !f.line(ln) {
				return false
			}
		}
	}
	return f.derived(progressed)
}

func (f *follower) line(ln []byte) bool {
	if len(bytes.TrimSpace(ln)) == 0 {
		return true
	}
	var l logLine
	if json.Unmarshal(ln, &l) != nil || !ValidNodeID(l.NodeID) {
		f.v.warn(fmt.Sprintf("events.jsonl line %d: it is not an event", f.seq))
		return true
	}
	if l.Kind == "attempt" {
		f.attempts[l.NodeID]++
	}
	if f.seq <= f.since {
		return true
	}
	row, err := f.v.bb.Get(f.ctx, f.v.runID, l.NodeID)
	if err != nil {
		f.v.warn(fmt.Sprintf("events.jsonl line %d: the node has no readable runtime file", f.seq))
		return true
	}
	switch l.Kind {
	case "status":
		return f.emit(EventNodeStatus, NodeStatusData{ID: l.NodeID, Status: string(row.Status), Wave: f.waveOf(l.NodeID)})
	case "attempt":
		if len(row.Attempts) == 0 {
			return true
		}
		i := f.attempts[l.NodeID] - 1
		if i >= len(row.Attempts) {
			i = len(row.Attempts) - 1
		}
		return f.emit(EventNodeAttempt, NodeAttemptData{ID: l.NodeID, Attempt: wireAttempt(row.Attempts[i])})
	case "result":
		if row.Result == nil {
			return true
		}
		return f.emit(EventNodeResult, NodeResultData{ID: l.NodeID, Result: wireResult(row.Result)})
	}
	f.v.warn(fmt.Sprintf("events.jsonl line %d: unknown event kind", f.seq))
	return true
}

func (f *follower) waveOf(id string) int {
	if w, ok := f.waves[id]; ok {
		return w
	}
	f.waves = f.v.waveMap()
	return f.waves[id]
}

func (v *View) waveMap() map[string]int {
	m := map[string]int{}
	t, err := tree.NewStore(v.dir).Load()
	if err != nil {
		return m
	}
	for id, n := range t.Nodes {
		if n.Wave != nil {
			m[id] = *n.Wave
		}
	}
	return m
}

// derived emits run-status, wave-changed and run-report when state moved. The
// first call only records the baseline, so a client that joins late is not told
// old news. The active wave is recomputed only when new lines arrived: reading
// every runtime file on every tick would cost more than it tells.
func (f *follower) derived(progressed bool) bool {
	status, reason := f.v.status()
	reported := f.v.exists(report.FileName)
	wave := f.wave
	if progressed || f.first {
		wave = f.v.activeWave(f.ctx)
	}
	if f.first {
		f.first, f.status, f.reason, f.wave, f.reported = false, status, reason, wave, reported
		return true
	}
	if status != f.status || reason != f.reason {
		f.status, f.reason = status, reason
		if !f.emit(EventRunStatus, RunStatusData{Status: status, StopReason: reason}) {
			return false
		}
	}
	if wave != f.wave {
		for _, w := range waveChanges(f.wave, wave) {
			if !f.emit(EventWaveChanged, w) {
				return false
			}
		}
		f.wave = wave
	}
	if reported && !f.reported {
		f.reported = true
		if body, err := f.v.Report(); err == nil {
			return f.send(Event{Seq: f.seq, Type: EventRunReport, Data: json.RawMessage(body)})
		}
	}
	return true
}

// waveChanges is the events for the active wave moving from prev to cur
// (-1 means no wave is active).
func waveChanges(prev, cur int) []WaveChangedData {
	var out []WaveChangedData
	if prev >= 0 {
		out = append(out, WaveChangedData{Wave: prev, State: "finished"})
	}
	if cur >= 0 {
		out = append(out, WaveChangedData{Wave: cur, State: "started"})
	}
	return out
}

// activeWave is the lowest wave that still has a function node to finish, or -1.
func (v *View) activeWave(ctx context.Context) int {
	l := v.load(ctx)
	active := -1
	for id, n := range l.t.Nodes {
		if n.Kind != tree.KindFunction || n.Wave == nil {
			continue
		}
		st := blackboard.StatusPending
		if r, ok := l.rows[id]; ok {
			st = r.Status
		}
		switch st {
		case blackboard.StatusVerified, blackboard.StatusFailed, blackboard.StatusNeedsRevision, blackboard.StatusEscalated:
			continue
		}
		if active == -1 || *n.Wave < active {
			active = *n.Wave
		}
	}
	return active
}
```

Import order: `gofmt` will sort the imports; run `gofmt -w gophermind-lib/runview` before the test run.

- [ ] **Step 4: Run to confirm green**

Run: `gofmt -l gophermind-lib/runview` (prints nothing after `gofmt -w`), then `gt ./gophermind-lib/runview/... -count=1 -race`
Expected: `ok` for all three packages, no data race.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/runview
git commit -m "feat(runview): Follow streams node events with replay from a line and derives run, wave and report events" -m "Rulings: derived events are baselined after the replay; the n-th attempt line maps to the n-th attempt; a bad line is skipped and reported by number." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 6: The JSON endpoints of the server (`runs`, `state`, `node`, `artifact`, `decisions`, `report`)

**Files:**
- Create: `gophermind-lib/serve/pipeline_api.go`, `gophermind-lib/serve/pipeline_api_test.go`
- Modify: `gophermind-lib/serve/pipeline.go` (delete the old state and report handlers; add `MaxStreams` to `PipelineDeps`), `gophermind-lib/serve/pipeline_test.go` (delete the two tests of the old handlers), `gophermind-lib/serve/webhook.go` (routes and the route table comment)

**Interfaces:**
- Produces: `const PipelineSchemaHeader = "X-Gophermind-Pipeline-Schema"`; `PipelineDeps{Root string; Hub *PipelineHub; MaxStreams int}` (the `Hub` field goes in Task 7); `type pipelineAPI`; `newPipelineAPI(PipelineDeps) *pipelineAPI` with methods `runs`, `state`, `node`, `artifact`, `decisions`, `report` (each an `http.HandlerFunc`-shaped method); `withPipelineSchema(http.Handler) http.Handler`; `pipelineJSON`, `pipelineError`.
- Consumes: `runview.Runs`, `runview.Open`, the `View` methods of Tasks 3 and 4.

The events route stays on the old hub until Task 7, so the server keeps building and the old event tests keep passing in between.

- [ ] **Step 1: Write the failing tests**

`gophermind-lib/serve/pipeline_api_test.go`:

```go
package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

func apiServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: root}}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func apiGet(t *testing.T, srv *httptest.Server, path, token string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestPipelineRoutesRequireTheTokenAndCarryTheSchemaHeader(t *testing.T) {
	srv := apiServer(t, t.TempDir())
	for _, p := range []string{"/pipeline/runs", "/pipeline/state", "/pipeline/node?id=x", "/pipeline/artifact", "/pipeline/decisions", "/pipeline/report"} {
		resp, _ := apiGet(t, srv, p, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a token: %d, want 401", p, resp.StatusCode)
		}
		if resp.Header.Get(PipelineSchemaHeader) != "1" {
			t.Errorf("%s: a 401 must still carry %s: 1", p, PipelineSchemaHeader)
		}
	}
	resp, _ := apiGet(t, srv, "/pipeline/runs", "wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
}

func TestPipelineRunsAndState(t *testing.T) {
	root := t.TempDir()
	runviewtest.BuildID(t, root, "gm-2026-10-07-001", runviewtest.Verified)
	runviewtest.BuildID(t, root, "gm-2026-10-08-001", runviewtest.Building)
	srv := apiServer(t, root)

	resp, body := apiGet(t, srv, "/pipeline/runs", "t")
	var runs runview.RunsBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &runs) != nil || runs.SchemaVersion != 1 ||
		len(runs.Runs) != 2 || runs.Runs[0].RunID != "gm-2026-10-08-001" {
		t.Fatalf("runs = %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get(PipelineSchemaHeader) != "1" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("headers = %v", resp.Header)
	}

	// Default run is the newest.
	resp, body = apiGet(t, srv, "/pipeline/state", "t")
	var st runview.StateBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &st) != nil || st.Run == nil || st.Run.RunID != "gm-2026-10-08-001" ||
		st.SchemaVersion != 1 || len(st.Nodes) != 5 || st.EventSeq == 0 {
		t.Fatalf("state = %d %s", resp.StatusCode, body)
	}
	// An explicit run.
	_, body = apiGet(t, srv, "/pipeline/state?run=gm-2026-10-07-001", "t")
	_ = json.Unmarshal(body, &st)
	if st.Run == nil || st.Run.Status != "verified" {
		t.Errorf("explicit run = %s", body)
	}
	// Unknown but well-formed: 404. Malformed: 400.
	if resp, _ := apiGet(t, srv, "/pipeline/state?run=gm-2026-10-09-009", "t"); resp.StatusCode != 404 {
		t.Errorf("unknown run: %d, want 404", resp.StatusCode)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/state?run=..%2F..%2Fetc", "t"); resp.StatusCode != 400 {
		t.Errorf("malformed run: %d, want 400", resp.StatusCode)
	}
}

func TestPipelineStateWithNoRunIsEmptyNotAnError(t *testing.T) {
	srv := apiServer(t, t.TempDir())
	resp, body := apiGet(t, srv, "/pipeline/state", "t")
	var st runview.StateBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &st) != nil || st.Run != nil || st.Nodes == nil || len(st.Nodes) != 0 || st.SchemaVersion != 1 {
		t.Fatalf("empty state = %d %s", resp.StatusCode, body)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/node?id=fn-greet", "t"); resp.StatusCode != 404 {
		t.Errorf("node with no run: %d, want 404", resp.StatusCode)
	}
}

func TestPipelineNodeArtifactDecisions(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)

	resp, body := apiGet(t, srv, "/pipeline/node?id=fn-bye", "t")
	var nb runview.NodeBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &nb) != nil || nb.Runtime == nil || nb.Runtime.Status != "in_progress" || len(nb.Artifacts) != 1 {
		t.Fatalf("node = %d %s", resp.StatusCode, body)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/node?id=..%2Fx", "t"); resp.StatusCode != 400 {
		t.Errorf("bad node id: %d, want 400", resp.StatusCode)
	}
	if resp, _ := apiGet(t, srv, "/pipeline/node?id=nope", "t"); resp.StatusCode != 404 {
		t.Errorf("unknown node: %d, want 404", resp.StatusCode)
	}

	resp, body = apiGet(t, srv, "/pipeline/artifact?id=fn-bye&attempt=1&file=check-output.txt", "t")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "want bye, got hello") ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("artifact = %d %q %v", resp.StatusCode, body, resp.Header)
	}
	for q, want := range map[string]int{
		"id=fn-bye&attempt=1&file=prompt.txt":      400,
		"id=fn-bye&attempt=x&file=reply.txt":       400,
		"id=fn-bye&attempt=0&file=reply.txt":       400,
		"id=fn-bye&attempt=1&file=..%2Freply.txt":  400,
		"id=fn-bye&attempt=7&file=reply.txt":       404,
		"id=fn-late&attempt=1&file=reply.txt":      404,
		"attempt=1&file=reply.txt":                 400,
	} {
		if resp, _ := apiGet(t, srv, "/pipeline/artifact?"+q, "t"); resp.StatusCode != want {
			t.Errorf("artifact?%s: %d, want %d", q, resp.StatusCode, want)
		}
	}

	resp, body = apiGet(t, srv, "/pipeline/decisions", "t")
	var db runview.DecisionsBody
	if resp.StatusCode != 200 || json.Unmarshal(body, &db) != nil || len(db.Decisions) != 1 || db.SchemaVersion != 1 {
		t.Fatalf("decisions = %d %s", resp.StatusCode, body)
	}
}

func TestPipelineReportPartialThenVerbatim(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)
	_, body := apiGet(t, srv, "/pipeline/report", "t")
	var rb runview.ReportBody
	if json.Unmarshal(body, &rb) != nil || rb.IsComplete() || rb.Status != "building" {
		t.Fatalf("running report = %s", body)
	}

	root2 := t.TempDir()
	f2 := runviewtest.Build(t, root2, runviewtest.Verified)
	srv2 := apiServer(t, root2)
	resp, body := apiGet(t, srv2, "/pipeline/report", "t")
	want, _ := os.ReadFile(filepath.Join(f2.Dir, "report.json"))
	if resp.StatusCode != 200 || string(body) != string(want) {
		t.Errorf("the finished report must be served byte for byte")
	}
	if resp.Header.Get(PipelineSchemaHeader) != "1" {
		t.Errorf("the report response needs the schema header (its body keeps the report's own version)")
	}
	_ = f
}

func TestPipelineErrorBodiesNeverEchoTheRequest(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	srv := apiServer(t, root)
	for _, p := range []string{
		"/pipeline/state?run=..%2F..%2Fetc%2Fpasswd", "/pipeline/node?id=%3Cscript%3E", "/pipeline/node?id=secretnode",
		"/pipeline/artifact?id=fn-bye&attempt=1&file=%2Fetc%2Fpasswd",
	} {
		_, body := apiGet(t, srv, p, "t")
		for _, bad := range []string{"passwd", "script", "secretnode", root} {
			if strings.Contains(string(body), bad) {
				t.Errorf("%s: the error body echoes %q: %s", p, bad, body)
			}
		}
	}
}

func TestPipelineRoutesAreGetOnly(t *testing.T) {
	srv := apiServer(t, t.TempDir())
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/pipeline/state", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /pipeline/state: %d, want 405", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/serve/ -run 'TestPipelineRoutes|TestPipelineRunsAndState|TestPipelineStateWithNoRun|TestPipelineNodeArtifact|TestPipelineReportPartial|TestPipelineErrorBodies' -count=1`
Expected: build failure (`undefined: PipelineSchemaHeader`) or, once it compiles, 404s for the new routes.

- [ ] **Step 3: Write `pipeline_api.go`**

```go
package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"gophermind/gophermind-lib/runview"
)

// PipelineSchemaHeader is on every pipeline response, errors included. A
// client that sees another number shows "this server and this app disagree"
// instead of decoding a body it does not understand.
const PipelineSchemaHeader = "X-Gophermind-Pipeline-Schema"

// defaultPipelineStreams is how many GET /pipeline/events streams may be open
// at once when PipelineDeps.MaxStreams is zero.
const defaultPipelineStreams = 16

// pipelineAPI serves the run view of the repo at root. One value is shared by
// every route, because the stream slots are shared.
type pipelineAPI struct {
	root  string
	slots chan struct{}
}

func newPipelineAPI(d PipelineDeps) *pipelineAPI {
	n := d.MaxStreams
	if n < 1 {
		n = defaultPipelineStreams
	}
	return &pipelineAPI{root: d.Root, slots: make(chan struct{}, n)}
}

// withPipelineSchema stamps the schema header before anything else can answer,
// so a 401 from the auth layer carries it too.
func withPipelineSchema(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(PipelineSchemaHeader, strconv.Itoa(runview.SchemaVersion))
		next.ServeHTTP(w, r)
	})
}

func pipelineJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// pipelineError answers with a fixed message. It never echoes the request or a
// path: a bad id is "invalid id or parameter", a missing thing "not found".
func pipelineError(w http.ResponseWriter, err error) {
	status, msg := http.StatusInternalServerError, "internal error"
	switch {
	case errors.Is(err, runview.ErrBadID):
		status, msg = http.StatusBadRequest, "invalid id or parameter"
	case errors.Is(err, runview.ErrNotFound):
		status, msg = http.StatusNotFound, "not found"
	}
	pipelineJSON(w, status, map[string]any{"schema_version": runview.SchemaVersion, "error": msg})
}

// view opens the run named by ?run=, or the newest one.
func (a *pipelineAPI) view(r *http.Request, opts ...runview.Option) (*runview.View, error) {
	id := r.URL.Query().Get("run")
	if id == "" {
		runs, err := runview.Runs(a.root)
		if err != nil {
			return nil, err
		}
		if len(runs) == 0 {
			return nil, runview.ErrNotFound
		}
		id = runs[0].RunID
	}
	return runview.Open(a.root, id, opts...)
}

func (a *pipelineAPI) runs(w http.ResponseWriter, r *http.Request) {
	runs, err := runview.Runs(a.root)
	if err != nil {
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, runview.RunsBody{SchemaVersion: runview.SchemaVersion, Runs: runs})
}

func (a *pipelineAPI) state(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("run") == "" {
		runs, err := runview.Runs(a.root)
		if err != nil {
			pipelineError(w, err)
			return
		}
		if len(runs) == 0 {
			pipelineJSON(w, http.StatusOK, runview.StateBody{
				SchemaVersion: runview.SchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Nodes: []runview.NodeSummary{},
			})
			return
		}
	}
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, v.State())
}

func (a *pipelineAPI) node(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	body, err := v.Node(r.URL.Query().Get("id"))
	if err != nil {
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, body)
}

func (a *pipelineAPI) artifact(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	q := r.URL.Query()
	attempt, aerr := strconv.Atoi(q.Get("attempt"))
	if aerr != nil {
		pipelineError(w, runview.ErrBadID)
		return
	}
	text, err := v.Artifact(q.Get("id"), attempt, q.Get("file"))
	if err != nil {
		pipelineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(text))
}

func (a *pipelineAPI) decisions(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	body, err := v.Decisions()
	if err != nil {
		pipelineError(w, err)
		return
	}
	pipelineJSON(w, http.StatusOK, body)
}

func (a *pipelineAPI) report(w http.ResponseWriter, r *http.Request) {
	v, err := a.view(r)
	if err != nil {
		pipelineError(w, err)
		return
	}
	body, err := v.Report()
	if err != nil {
		pipelineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
```

- [ ] **Step 4: Edit `pipeline.go`**

In `gophermind-lib/serve/pipeline.go`:
1. Delete the type `pipelineStateResponse`, the function `pipelineStateHandler` and the function `pipelineReportHandler`, each with its doc comment.
2. Remove `"time"` from the imports (nothing else in the file uses it after step 1).
3. Replace the `PipelineDeps` declaration and its doc comment with:

```go
// PipelineDeps configures the pipeline routes. Root is the repo root whose
// .gophermind/<run>/ folders the routes read. Hub still feeds
// GET /pipeline/events until the stream is rewritten to follow the run folder
// itself. MaxStreams caps concurrent event streams; zero means 16.
type PipelineDeps struct {
	Root       string
	Hub        *PipelineHub
	MaxStreams int
}
```

In `gophermind-lib/serve/pipeline_test.go` delete `TestPipelineStateReturnsRealTasksAndRequiresToken` and `TestPipelineReportRequiresToken` (each from its doc comment to its closing brace). Remove any import the compiler then reports unused.

- [ ] **Step 5: Edit `webhook.go`**

Replace the route table lines

```go
//	GET     /pipeline/state                bearer+HMAC  d.Pipeline
//	GET     /pipeline/events               bearer+HMAC  d.Pipeline (SSE)
//	GET     /pipeline/report               bearer+HMAC  d.Pipeline
```

with

```go
//	GET     /pipeline/runs                 bearer+HMAC  d.Pipeline
//	GET     /pipeline/state                bearer+HMAC  d.Pipeline
//	GET     /pipeline/node                 bearer+HMAC  d.Pipeline
//	GET     /pipeline/artifact             bearer+HMAC  d.Pipeline
//	GET     /pipeline/decisions            bearer+HMAC  d.Pipeline
//	GET     /pipeline/events               bearer+HMAC  d.Pipeline (SSE)
//	GET     /pipeline/report               bearer+HMAC  d.Pipeline
```

and replace the body of `if d.Pipeline != nil { ... }` from `hub := d.Pipeline.Hub` to the last `mux.Handle("GET /pipeline/report", ...)` with:

```go
		hub := d.Pipeline.Hub
		if hub == nil {
			hub = NewPipelineHub()
		}
		api := newPipelineAPI(*d.Pipeline)
		mux.Handle("GET /pipeline", withPipelineSchema(pipelineDashboardHandler()))
		pipeWrap := func(h http.Handler) http.Handler { return withPipelineSchema(limited(sessionAuth(token, h))) }
		mux.Handle("GET /pipeline/runs", pipeWrap(http.HandlerFunc(api.runs)))
		mux.Handle("GET /pipeline/state", pipeWrap(http.HandlerFunc(api.state)))
		mux.Handle("GET /pipeline/node", pipeWrap(http.HandlerFunc(api.node)))
		mux.Handle("GET /pipeline/artifact", pipeWrap(http.HandlerFunc(api.artifact)))
		mux.Handle("GET /pipeline/decisions", pipeWrap(http.HandlerFunc(api.decisions)))
		mux.Handle("GET /pipeline/events", pipeWrap(pipelineEventsHandler(hub)))
		mux.Handle("GET /pipeline/report", pipeWrap(http.HandlerFunc(api.report)))
```

(`pipelineDashboardHandler()` returns an `http.HandlerFunc`, which is an `http.Handler`; the old `mux.HandleFunc("GET /pipeline", pipelineDashboardHandler())` line goes.)

- [ ] **Step 6: Run to confirm green**

Run: `gofmt -l gophermind-lib/serve` (prints nothing), then `gt ./gophermind-lib/serve/... -count=1`
Expected: `ok`. The old hub, watcher and dashboard tests still pass.

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/serve
git commit -m "feat(serve): pipeline runs, state, node, artifact, decisions and report read the run folder" -m "Rulings: the schema header is on every response including 401; error bodies never echo the request; with no run the state is 200 with run null." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 7: The event stream follows the run folder; delete the hub and the watcher

**Files:**
- Modify: `gophermind-lib/serve/pipeline.go` (delete the hub, add the events method and `PipelineMaxStreamsFromEnv`), `gophermind-lib/serve/sse.go` (`writeSSEFrame`), `gophermind-lib/serve/webhook.go` (route), `gophermind-lib/serve/pipeline_test.go` (drop the hub tests)
- Delete: `gophermind-lib/serve/pipeline_watch.go`, `gophermind-lib/serve/pipeline_watch_test.go`
- Create: `gophermind-lib/serve/pipeline_events_test.go`
- Modify: `gophermind-server/server.go`, `gophermind-server/main.go`, `gophermind-server/server_test.go`, `cmd/gophermind/main.go`

**Interfaces:**
- Produces: `(*pipelineAPI).events`; `writeSSEFrame(w io.Writer, flusher http.Flusher, id int64, event, data string)`; `PipelineMaxStreamsFromEnv() int`; `var pipelineKeepalive = 15 * time.Second`, `var pipelinePoll = 250 * time.Millisecond` (variables so tests can shorten them); `PipelineDeps{Root string; MaxStreams int}` (the `Hub` field and `PipelineHub` are deleted); `buildDeps(...) (serve.Deps, error)`.
- Consumes: `runview.View.Follow`, `EventSeq`.

The serve event types `TaskStatusEvent`, `TaskAttemptEvent`, `WaveChangedEvent` in `events.go` stay until Task 9 stops the desktop client using them.

- [ ] **Step 1: Write the failing tests**

`gophermind-lib/serve/pipeline_events_test.go`:

```go
package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

type frame struct {
	id, event, data string
}

// readFrame reads one frame, skipping comment-only frames (keepalives).
func readFrame(t *testing.T, br *bufio.Reader) frame {
	t.Helper()
	var f frame
	has := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "":
			if has {
				return f
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "id: "):
			f.id, has = strings.TrimPrefix(line, "id: "), true
		case strings.HasPrefix(line, "event: "):
			f.event, has = strings.TrimPrefix(line, "event: "), true
		case strings.HasPrefix(line, "data: "):
			if f.data != "" {
				f.data += "\n"
			}
			f.data += strings.TrimPrefix(line, "data: ")
			has = true
		}
	}
}

func openStream(t *testing.T, srv *httptest.Server, path string, hdr map[string]string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer t")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, bufio.NewReader(resp.Body)
}

func TestEventsReplayFromZeroWithIDs(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)
	v, _ := runview.Open(root, runviewtest.RunID)
	n := v.EventSeq()

	resp, br := openStream(t, srv, "/pipeline/events?since=0", nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") || resp.Header.Get(PipelineSchemaHeader) != "1" {
		t.Fatalf("status %d headers %v", resp.StatusCode, resp.Header)
	}
	for i := int64(1); i <= n; i++ {
		f := readFrame(t, br)
		if f.id != strconv.FormatInt(i, 10) || !strings.HasPrefix(f.event, "node-") {
			t.Fatalf("frame %d = %+v", i, f)
		}
		if !json.Valid([]byte(f.data)) {
			t.Fatalf("frame %d data is not JSON: %q", i, f.data)
		}
	}
}

func TestEventsResumeFromLastEventID(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	srv := apiServer(t, root)
	v, _ := runview.Open(root, runviewtest.RunID)
	n := v.EventSeq()
	_, br := openStream(t, srv, "/pipeline/events", map[string]string{"Last-Event-ID": strconv.FormatInt(n-1, 10)})
	f := readFrame(t, br)
	if f.id != strconv.FormatInt(n, 10) {
		t.Errorf("resumed at id %s, want %d (the line after Last-Event-ID)", f.id, n)
	}
}

func TestEventsWithoutSinceFollowFromTheEnd(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	oldPoll := pipelinePoll
	pipelinePoll = 10 * time.Millisecond
	t.Cleanup(func() { pipelinePoll = oldPoll })
	srv := apiServer(t, root)
	_, br := openStream(t, srv, "/pipeline/events", nil)
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-bye", "in_progress", "verified"); err != nil {
		t.Fatal(err)
	}
	fr := readFrame(t, br)
	var d runview.NodeStatusData
	_ = json.Unmarshal([]byte(fr.data), &d)
	if fr.event != "node-status" || d.ID != "fn-bye" || d.Status != "verified" {
		t.Errorf("first live frame = %+v", fr)
	}
}

func TestEventsRejectBadSinceUnknownRunAndMissingToken(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	srv := apiServer(t, root)
	for path, want := range map[string]int{
		"/pipeline/events?since=abc": 400, "/pipeline/events?since=-1": 400,
		"/pipeline/events?run=gm-2026-10-09-009": 404, "/pipeline/events?run=..%2Fx": 400,
	} {
		resp, _ := apiGet(t, srv, path, "t")
		if resp.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, resp.StatusCode, want)
		}
	}
	if resp, _ := apiGet(t, srv, "/pipeline/events", ""); resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

func TestEventsKeepaliveComment(t *testing.T) {
	old := pipelineKeepalive
	pipelineKeepalive = 30 * time.Millisecond
	t.Cleanup(func() { pipelineKeepalive = old })
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	srv := apiServer(t, root)
	_, br := openStream(t, srv, "/pipeline/events", nil)
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, ": keepalive") {
		t.Fatalf("first line = %q, %v; want a keepalive comment", line, err)
	}
}

func TestEventsStreamCapAndSlotRelease(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: root, MaxStreams: 2}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	r1, _ := openStream(t, srv, "/pipeline/events", nil)
	_, _ = openStream(t, srv, "/pipeline/events", nil)
	r3, _ := openStream(t, srv, "/pipeline/events", nil)
	if r3.StatusCode != http.StatusServiceUnavailable || r3.Header.Get("Retry-After") == "" || r3.Header.Get(PipelineSchemaHeader) != "1" {
		t.Fatalf("third stream: %d %v, want 503 with Retry-After", r3.StatusCode, r3.Header)
	}
	r1.Body.Close() // a client goes away: its slot must come back
	deadline := time.Now().Add(3 * time.Second)
	for {
		r4, _ := openStream(t, srv, "/pipeline/events", nil)
		if r4.StatusCode == 200 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not released after a disconnect, last status %d", r4.StatusCode)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPipelineMaxStreamsFromEnv(t *testing.T) {
	for v, want := range map[string]int{"": 0, "x": 0, "0": 0, "-3": 0, "4": 4} {
		t.Setenv("GOPHERMIND_PIPELINE_MAX_STREAMS", v)
		if got := PipelineMaxStreamsFromEnv(); got != want {
			t.Errorf("%q -> %d, want %d", v, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/serve/ -run 'TestEvents|TestPipelineMaxStreams' -count=1`
Expected: build failure (`undefined: pipelinePoll`).

- [ ] **Step 3: `sse.go`, add the id-carrying frame**

Append to `gophermind-lib/serve/sse.go`:

```go
// writeSSEFrame is writeSSEEvent with an "id:" line first. A client sends the
// last id it saw back as Last-Event-ID (or since=) to resume the stream. An id
// of zero writes no id line.
func writeSSEFrame(w io.Writer, flusher http.Flusher, id int64, event, data string) {
	if id > 0 {
		fmt.Fprintf(w, "id: %d\n", id)
	}
	writeSSEEvent(w, flusher, event, data)
}
```

- [ ] **Step 4: `pipeline.go`, replace the hub with the events method**

In `gophermind-lib/serve/pipeline.go` delete: `pipelineEvent`, `pipelineSubBuffer`, `PipelineHub`, `NewPipelineHub`, `subscribe`, `subCount`, `publish`, `TaskStatus`, `TaskAttempt`, `WaveChanged`, `RunReport` and `pipelineEventsHandler` (each with its comment), and the imports `encoding/json`, `sync` and `gophermind/gophermind-lib/phaseflow`. Rewrite the file's header comment (the block that begins "This file implements the server side of the live pipeline view") and `PipelineDeps` as:

```go
// This file holds the dashboard page and the shared pieces of the pipeline
// routes. pipeline_api.go has the JSON routes; the event stream is below. Every
// route reads the run folder through gophermind-lib/runview: there is no
// cache, no hub and no watcher, so a stream and a fresh state call cannot
// disagree about what happened.

// PipelineDeps configures the pipeline routes. Root is the repo root whose
// .gophermind/<run>/ folders the routes read. MaxStreams caps concurrent event
// streams (zero means 16; the server reads GOPHERMIND_PIPELINE_MAX_STREAMS).
type PipelineDeps struct {
	Root       string
	MaxStreams int
}

// PipelineMaxStreamsFromEnv reads GOPHERMIND_PIPELINE_MAX_STREAMS. Unset,
// invalid or below 1 gives 0, which means the default.
func PipelineMaxStreamsFromEnv() int {
	n, err := strconv.Atoi(os.Getenv("GOPHERMIND_PIPELINE_MAX_STREAMS"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// pipelineKeepalive and pipelinePoll are variables so tests can shorten them.
var (
	pipelineKeepalive = 15 * time.Second
	pipelinePoll      = 250 * time.Millisecond
)

// sinceOf is where an event stream starts. ?since=N wins, then Last-Event-ID;
// with neither the stream starts at the current end of the log (live only).
func sinceOf(r *http.Request, v *runview.View) (int64, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		return v.EventSeq(), nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, runview.ErrBadID
	}
	return n, nil
}

// events handles GET /pipeline/events: an SSE stream of the run's events, each
// with its line number as id. At most MaxStreams streams run at once; one more
// gets 503. A client that goes away (the request context ends) frees its slot.
func (a *pipelineAPI) events(w http.ResponseWriter, r *http.Request) {
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		w.Header().Set("Retry-After", "5")
		pipelineJSON(w, http.StatusServiceUnavailable,
			map[string]any{"schema_version": runview.SchemaVersion, "error": "too many event streams"})
		return
	}
	v, err := a.view(r, runview.WithPoll(pipelinePoll))
	if err != nil {
		pipelineError(w, err)
		return
	}
	since, err := sinceOf(r, v)
	if err != nil {
		pipelineError(w, err)
		return
	}
	ctx := r.Context()
	ch, err := v.Follow(ctx, since)
	if err != nil {
		pipelineError(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	// Headers go out before the first event so a client whose Do blocks until a
	// response arrives is connected at once.
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}
	ka := time.NewTicker(pipelineKeepalive)
	defer ka.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			var compact bytes.Buffer
			data := string(e.Data)
			if json.Compact(&compact, e.Data) == nil {
				data = compact.String()
			}
			writeSSEFrame(w, flusher, e.Seq, e.Type, data)
		case <-ka.C:
			fmt.Fprint(w, ": keepalive\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case <-ctx.Done():
			return
		}
	}
}
```

Adjust the import block of `pipeline.go` to what is used now: `bytes`, `embed`, `encoding/json`, `fmt`, `io`, `net/http`, `os`, `strconv`, `time`, `gophermind/gophermind-lib/runview`. Keep the `pipelineAssets` embed declaration and `pipelineDashboardHandler`.

- [ ] **Step 5: Route, deletions, callers**

In `webhook.go` replace the hub lines. The block now reads:

```go
		api := newPipelineAPI(*d.Pipeline)
		mux.Handle("GET /pipeline", withPipelineSchema(pipelineDashboardHandler()))
		pipeWrap := func(h http.Handler) http.Handler { return withPipelineSchema(limited(sessionAuth(token, h))) }
		mux.Handle("GET /pipeline/runs", pipeWrap(http.HandlerFunc(api.runs)))
		mux.Handle("GET /pipeline/state", pipeWrap(http.HandlerFunc(api.state)))
		mux.Handle("GET /pipeline/node", pipeWrap(http.HandlerFunc(api.node)))
		mux.Handle("GET /pipeline/artifact", pipeWrap(http.HandlerFunc(api.artifact)))
		mux.Handle("GET /pipeline/decisions", pipeWrap(http.HandlerFunc(api.decisions)))
		mux.Handle("GET /pipeline/events", pipeWrap(http.HandlerFunc(api.events)))
		mux.Handle("GET /pipeline/report", pipeWrap(http.HandlerFunc(api.report)))
```

Update the comment on `Deps.Pipeline` (webhook.go, "registers the live pipeline view routes") to name the seven routes. Then:

```bash
git rm gophermind-lib/serve/pipeline_watch.go gophermind-lib/serve/pipeline_watch_test.go
```

In `gophermind-lib/serve/pipeline_test.go` delete `TestPipelineHubTaskStatusEvent`, `TestPipelineHubTaskAttemptEvent`, `TestPipelineHubEventsArriveInOrder`, `TestPipelineEventsDisconnectDoesNotLeak`, `waitForSubscribers` and `readOneSSEFrame` (check with `grep -n readOneSSEFrame gophermind-lib/serve/*_test.go` that nothing else uses it; the new tests use `readFrame`). Remove unused imports.

`gophermind-server/server.go`: change the signature to `func buildDeps(cfg serverConfig, root string, logger *slog.Logger) (serve.Deps, error)`; delete the paragraph of its doc comment that begins "buildDeps' second return value is the pipeline hub"; delete the line `pipelineHub := serve.NewPipelineHub()`; set `Pipeline: &serve.PipelineDeps{Root: root, MaxStreams: serve.PipelineMaxStreamsFromEnv()},`; change the final `return d, pipelineHub, nil` to `return d, nil` and every other `return serve.Deps{}, nil, err` style return to two values (`grep -n 'return' gophermind-server/server.go` inside `buildDeps`).

`gophermind-server/main.go`: `svcDeps, err := buildDeps(cfg, cfg.Root, logger)`; delete the five comment lines and the line `serve.StartPipelineWatcher(ctx, cfg.Root, pipelineHub)` (the block that begins "Feeds the pipeline hub").

`gophermind-server/server_test.go`: line 82 `d, _, err := buildDeps(cfg, root, logger)` becomes `d, err := buildDeps(cfg, root, logger)`; in `TestBuildDeps_Compiles` use `d, err := buildDeps(...)` and delete the `hub == nil` check.

`cmd/gophermind/main.go` (the `serve` case): delete `pipelineHub := serve.NewPipelineHub()`; replace the `Pipeline:` entry and its comment with

```go
			// The pipeline routes read cfg.RootDir/.gophermind/<run>/ through
			// gophermind-lib/runview: runs, state, node, artifact, decisions,
			// report and a followed event stream.
			Pipeline: &serve.PipelineDeps{Root: cfg.RootDir, MaxStreams: serve.PipelineMaxStreamsFromEnv()},
```

delete the comment block and the call `serve.StartPipelineWatcher(serveCtx, cfg.RootDir, pipelineHub)`; in the usage text replace

```text
                                pipeline: GET /pipeline (dashboard), GET /pipeline/state,
                                GET /pipeline/events (SSE), GET /pipeline/report
```

with

```text
                                pipeline: GET /pipeline (dashboard), GET /pipeline/runs,
                                /pipeline/state, /pipeline/node, /pipeline/artifact,
                                /pipeline/decisions, /pipeline/report, GET /pipeline/events (SSE)
```

- [ ] **Step 6: Run to confirm green**

Run: `gofmt -l gophermind-lib/serve gophermind-server cmd/gophermind` (prints nothing); `go build ./... ` from the worktree root is not run for the desktop app here (it still compiles against `serve.TaskStatusEvent`, which stays). Then:
`gt ./gophermind-lib/serve/... ./gophermind-server/... -count=1 -race` and `gt ./cmd/gophermind/ -run 'Serve|Dispatch|Route' -count=1`
Expected: `ok`. `grep -rn 'PipelineHub\|StartPipelineWatcher\|pipeline_watch' --include='*.go' . | grep -v '^./gophermind-osx/e2e_pipeline_test.go'` prints nothing (the e2e comment is rewritten in Task 11).

- [ ] **Step 7: Commit**

```bash
git add -A gophermind-lib/serve gophermind-server cmd/gophermind
git commit -m "feat(serve): the event stream follows the run folder with replay; hub and watcher deleted" -m "Rulings: no since and no Last-Event-ID starts at the end of the log; the cap is PipelineDeps.MaxStreams (default 16, env GOPHERMIND_PIPELINE_MAX_STREAMS), 503 with Retry-After beyond it; keepalive every 15 s." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 8: The dashboard page, rewritten for nodes

**Files:**
- Modify: `gophermind-lib/serve/assets/pipeline.html` (lines 1 to 304 kept; the rest replaced), `gophermind-lib/serve/pipeline_test.go` (two new tests)

**Interfaces:**
- Produces: the page served by `GET /pipeline`. It calls `/pipeline/runs`, `/pipeline/state`, `/pipeline/node`, `/pipeline/artifact`, `/pipeline/decisions`, `/pipeline/report` and `/pipeline/events`.
- The security rules of the old page are kept and still pinned by the existing tests: the token comes only from the URL fragment, `<meta name="referrer" content="no-referrer">`, the strict CSP, `connect-src 'self'`, and no `esc(...).toUpperCase()`.

- [ ] **Step 1: Add the failing tests**

Append to `gophermind-lib/serve/pipeline_test.go` (add `os`, `os/exec` and `path/filepath` to its imports if missing):

```go
// TestPipelineDashboardUsesTheNodeContract pins that the page talks to the new
// routes and no longer speaks the old task vocabulary.
func TestPipelineDashboardUsesTheNodeContract(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: t.TempDir()}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get(PipelineSchemaHeader) != "1" {
		t.Errorf("the page response needs %s: 1", PipelineSchemaHeader)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	for _, want := range []string{
		"/pipeline/runs", "/pipeline/state", "/pipeline/node", "/pipeline/artifact", "/pipeline/decisions",
		"/pipeline/report", "/pipeline/events", "since", "X-Gophermind-Pipeline-Schema", "node-status", "run-status",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not mention %q", want)
		}
	}
	for _, gone := range []string{"task-status", "task-attempt", "acceptance_criteria", "revision_rounds", "contract_flagged"} {
		if strings.Contains(body, gone) {
			t.Errorf("the page still uses the old vocabulary %q", gone)
		}
	}
}

// TestPipelineDashboardScriptParses runs `node --check` on the page's script
// when node is installed, so a syntax slip is a red test and not a blank page.
func TestPipelineDashboardScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	page, err := os.ReadFile(filepath.Join("assets", "pipeline.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	i, j := strings.Index(s, "<script>"), strings.LastIndex(s, "</script>")
	if i < 0 || j < i {
		t.Fatal("no script block")
	}
	file := filepath.Join(t.TempDir(), "pipeline.js")
	if err := os.WriteFile(file, []byte(s[i+len("<script>"):j]), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
		t.Fatalf("node --check failed: %v\n%s", err, out)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `gt ./gophermind-lib/serve/ -run 'TestPipelineDashboard' -count=1`
Expected: `TestPipelineDashboardUsesTheNodeContract` fails (the page still uses `task-status`); the older dashboard tests pass.

- [ ] **Step 3: Splice the page**

Check the cut point: `sed -n '305,306p' gophermind-lib/serve/assets/pipeline.html` must print `</style>` and `</head>`. Then write the new tail and join:

```bash
f=gophermind-lib/serve/assets/pipeline.html
tmp="$(mktemp -d)"; case "$tmp" in /*) ;; *) echo "bad tmp"; exit 1;; esac
head -n 304 "$f" > "$tmp/head.html"
cat > "$tmp/tail.html" <<'HTML_EOF'
  select{background:var(--panel); color:var(--ink); border:1px solid var(--line); font:inherit; padding:2px 4px;}
  .artifact-link{color:var(--teal); cursor:pointer; text-decoration:underline;}
  .check-output{white-space:pre-wrap; background:var(--void); border:1px solid var(--line); padding:8px; margin:6px 0; color:var(--muted); font-size:12px;}
</style>
</head>
<body>

<div class="app">
  <header>
    <div class="masthead">TASK RUNNER<span>/ live pipeline</span></div>
    <div class="runmeta">
      <div class="connect">
        <input id="tokenInput" type="password" placeholder="bearer token" autocomplete="off">
        <button id="connectBtn" type="button">connect</button>
        <span id="connState" class="connstate off">disconnected</span>
      </div>
      <div>RUN <select id="runSelect"></select></div>
      <div>STATUS <b id="runStatus">-</b></div>
      <div>NODES <b id="taskIndicator">-</b></div>
      <div>COVERAGE <b id="coverage">-</b></div>
      <div>UPDATED <b id="updatedAt">-</b></div>
      <button class="replay" id="decisionsBtn" type="button">decisions</button>
      <button class="replay" id="reportBtn" type="button">run report</button>
    </div>
  </header>

  <div class="board">
    <div class="waves" id="waves"></div>
    <div class="detail">
      <div class="detail-head">
        <div class="tag" id="detailTag">-</div>
        <h2 id="detailTitle">Connect to see the live run</h2>
        <div class="spec-row">
          <div>
            <div class="k">DELIVERABLE</div>
            <div class="v" id="detailDeliverable">-</div>
          </div>
          <div>
            <div class="k">TESTS</div>
            <div class="v" id="detailTest">-</div>
          </div>
        </div>
        <div class="spec-row">
          <div>
            <div class="k">GROUPS</div>
            <div class="v" id="detailGroups">-</div>
          </div>
          <div>
            <div class="k">DECISIONS</div>
            <div class="v" id="detailDecisions">-</div>
          </div>
        </div>
      </div>
      <div class="log" id="log"></div>
      <div class="foot">
        <div class="hint" id="hint">enter the server's bearer token above and click connect</div>
      </div>
    </div>
  </div>
</div>

<div class="report-overlay" id="reportOverlay" hidden>
  <div class="report-panel">
    <div class="report-head">
      <h2 id="overlayTitle">Run Report</h2>
      <button class="close-report" id="closeReportBtn" type="button">close</button>
    </div>
    <div class="report-body" id="reportBody">-</div>
  </div>
</div>

<script>
// The engine's node statuses (the blackboard statuses) map to the dot and pill
// styling here and nowhere else. The label shown next to a node is always the
// engine's own status string; this map only picks how it is drawn. failed and
// escalated get their own treatment because they are the states most likely to
// need a person.
const STATUS_META = {
  pending:        { dot: 'queued' },
  ready:          { dot: 'queued' },
  claimed:        { dot: 'running' },
  in_progress:    { dot: 'running' },
  verified:       { dot: 'verified' },
  needs_revision: { dot: 'revisit' },
  failed:         { dot: 'failed', pill: 'pill-failed' },
  escalated:      { dot: 'escalated', pill: 'pill-escalated' },
};
function statusMetaFor(status){
  return STATUS_META[status] || { dot: 'unknown' };
}

function esc(s){
  return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
    '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'
  }[c]));
}

let nodes = [];        // NodeSummary list of the selected run, never demo data
let current = null;    // selected node id
let runId = '';
let authToken = '';
let lastSeq = 0;       // last event id seen; sent as since= on every (re)connect
let eventsAbort = null;
let eventsGen = 0;

const $ = id => document.getElementById(id);
const wavesEl = $('waves'), logEl = $('log'), titleEl = $('detailTitle'), tagEl = $('detailTag');
const delivEl = $('detailDeliverable'), testEl = $('detailTest'), groupsEl = $('detailGroups'), decEl = $('detailDecisions');
const taskIndicator = $('taskIndicator'), updatedAtEl = $('updatedAt'), statusEl = $('runStatus'), coverageEl = $('coverage');
const connStateEl = $('connState'), tokenInput = $('tokenInput'), connectBtn = $('connectBtn'), hintEl = $('hint');
const runSelect = $('runSelect'), reportBtn = $('reportBtn'), decisionsBtn = $('decisionsBtn');
const reportOverlay = $('reportOverlay'), reportBody = $('reportBody'), overlayTitle = $('overlayTitle'), closeReportBtn = $('closeReportBtn');

function authHeaders(){
  return authToken ? { 'Authorization': 'Bearer ' + authToken } : {};
}

function withQuery(path, params){
  const u = new URLSearchParams();
  Object.keys(params || {}).forEach(k => { if(params[k] !== '' && params[k] != null) u.set(k, params[k]); });
  const s = u.toString();
  return s ? path + '?' + s : path;
}

// getJSON checks the schema header before it trusts a body: a server that
// speaks another contract version is reported, not half-rendered.
async function getJSON(path, params){
  const resp = await fetch(withQuery(path, params), { headers: authHeaders() });
  if(resp.headers.get('X-Gophermind-Pipeline-Schema') !== '1'){
    throw new Error('this server and this page disagree about the pipeline contract; update the older one');
  }
  if(!resp.ok){
    throw new Error('GET ' + path + ': ' + resp.status);
  }
  return resp.json();
}

function findNode(id){ return nodes.find(n => n.id === id); }
function leafNodes(){ return nodes.filter(n => n.kind === 'function'); }

function renderHeader(run){
  statusEl.textContent = run ? run.status + (run.stop_reason ? ' / ' + run.stop_reason : '') : '-';
  coverageEl.textContent = run ? run.coverage.covered + '/' + run.coverage.total : '-';
}

// The tree: one block per component, its function nodes below it.
function renderTree(){
  wavesEl.innerHTML = '';
  const leaves = leafNodes();
  taskIndicator.textContent = leaves.filter(n => n.status === 'verified').length + '/' + leaves.length;
  nodes.filter(n => n.kind === 'component').forEach(c => {
    const block = document.createElement('div');
    block.className = 'wave-block';
    const label = document.createElement('div');
    label.className = 'wave-label';
    label.innerHTML = `${esc(c.title || c.id)}<div class="rule"></div>`;
    block.appendChild(label);
    leaves.filter(n => n.parent === c.id).forEach(n => {
      const meta = statusMetaFor(n.status);
      const pill = document.createElement('div');
      pill.className = 'task-pill' + (n.id === current ? ' active' : '') + (meta.pill ? ' ' + meta.pill : '');
      pill.dataset.id = n.id;
      pill.innerHTML = `<div class="dot ${meta.dot}"></div><div class="tname">${esc(n.title || n.id)} (w${esc(n.wave)}, ${esc(n.attempt_count)} att)</div><div class="tstatus">${esc(n.status)}</div>`;
      pill.addEventListener('click', () => loadNode(n.id));
      block.appendChild(pill);
    });
    wavesEl.appendChild(block);
  });
}

function renderAttemptLine(a, idx){
  const line = document.createElement('div');
  line.className = 'log-line';
  // Transform the raw value, then escape it: escaping last keeps the shown
  // text right ("&" must not become "&AMP;").
  const verdictLabel = a.verdict === 'pass' ? 'PASSED' : (a.verdict === 'fail' ? 'FAILED' : esc(String(a.verdict == null ? '' : a.verdict).toUpperCase()));
  line.innerHTML = `<div class="idx">${String(idx+1).padStart(2,'0')}</div><div class="model">${esc(a.model)}</div><div class="reason">${esc(a.failure_reason)}</div><div class="verdict ${esc(a.verdict)}">${verdictLabel}</div>`;
  return line;
}

function groupSummary(doc){
  const parts = [];
  if(doc.rationale) parts.push('why: ' + doc.rationale);
  if(doc.construction && doc.construction.approach_chosen) parts.push('how: ' + doc.construction.approach_chosen);
  if(doc.security && doc.security.trust_boundary) parts.push('trust boundary: ' + doc.security.trust_boundary);
  if(doc.performance && doc.performance.complexity) parts.push('complexity: ' + doc.performance.complexity);
  return parts.join(' | ') || '(no groups)';
}

async function showArtifact(after, id, attempt, file){
  const resp = await fetch(withQuery('/pipeline/artifact', { run: runId, id: id, attempt: attempt, file: file }), { headers: authHeaders() });
  const pre = document.createElement('pre');
  pre.className = 'check-output';
  pre.textContent = resp.ok ? await resp.text() : 'GET artifact failed: ' + resp.status;
  after.insertAdjacentElement('afterend', pre);
}

function renderLog(body){
  logEl.innerHTML = '';
  const attempts = (body.runtime && body.runtime.attempts) || [];
  if(!attempts.length){
    const empty = document.createElement('div');
    empty.className = 'log-line';
    empty.style.gridTemplateColumns = '1fr';
    empty.innerHTML = '<div class="reason">' + (body.runtime ? 'No attempts recorded yet.' : 'Queued - the executor has not started this node.') + '</div>';
    logEl.appendChild(empty);
  }
  attempts.forEach((a, i) => logEl.appendChild(renderAttemptLine(a, i)));
  (body.artifacts || []).forEach(art => {
    (art.files || []).filter(f => f === 'check-output.txt' || f === 'reply.txt').forEach(f => {
      const link = document.createElement('div');
      link.className = 'log-line artifact-link';
      link.style.gridTemplateColumns = '1fr';
      link.textContent = 'saved attempt ' + art.attempt + ': ' + f;
      link.addEventListener('click', () => showArtifact(link, body.node.id, art.attempt, f));
      logEl.appendChild(link);
    });
  });
  logEl.scrollTop = logEl.scrollHeight;
}

async function loadNode(id){
  current = id;
  renderTree();
  let body;
  try{
    body = await getJSON('/pipeline/node', { run: runId, id: id });
  } catch(err){
    hintEl.textContent = err.message;
    return;
  }
  if(current !== id) return;
  const doc = body.node || {};
  const summary = findNode(id) || {};
  tagEl.textContent = 'WAVE ' + (summary.wave || 0) + ' · ' + String(doc.kind || '').toUpperCase() +
    (doc.model_tier ? ' · ' + doc.model_tier : '') + (doc.node_class ? ' · ' + doc.node_class : '');
  titleEl.textContent = doc.title || id;
  delivEl.textContent = doc.description || '(no description)';
  const tests = (doc.tests || []).map(t => t.name);
  testEl.textContent = tests.length ? tests.join('; ') : '(no tests)';
  groupsEl.textContent = groupSummary(doc);
  decEl.textContent = (doc.decisions || []).map(d => d.question + ' -> ' + d.answer).join('; ') || '(none)';
  renderLog(body);
}

async function loadRuns(){
  const body = await getJSON('/pipeline/runs');
  runSelect.innerHTML = '';
  body.runs.forEach(r => {
    const o = document.createElement('option');
    o.value = r.run_id;
    o.textContent = r.run_id + ' ' + r.status;
    runSelect.appendChild(o);
  });
  if(!runId && body.runs.length) runId = body.runs[0].run_id;
  runSelect.value = runId;
}

async function loadState(){
  const body = await getJSON('/pipeline/state', { run: runId });
  nodes = body.nodes || [];
  lastSeq = Math.max(lastSeq, body.event_seq || 0);
  updatedAtEl.textContent = new Date(body.generated_at).toLocaleTimeString();
  renderHeader(body.run);
  if(!current){
    const first = leafNodes()[0];
    if(first) current = first.id;
  }
  renderTree();
  if(current) await loadNode(current);
}

function renderTable(headers, rows){
  let html = '<table class="model-stats"><thead><tr>' + headers.map(h => `<th>${esc(h)}</th>`).join('') + '</tr></thead><tbody>';
  rows.forEach(r => { html += '<tr>' + r.map(c => `<td>${esc(c)}</td>`).join('') + '</tr>'; });
  return html + '</tbody></table>';
}

function renderReport(r){
  const n = r.nodes || {}, cov = r.requirements_covered || {};
  let html = `<div style="margin-bottom:10px;">${esc(r.status)}${r.complete === false ? ' (running)' : ''} | nodes ${esc(n.verified)}/${esc(n.total)} verified, ${esc(n.failed)} failed, ${esc(n.escalated)} escalated | requirements ${esc(cov.covered)}/${esc(cov.total)}</div>`;
  html += renderTable(['task', 'model', 'calls', 'ok', 'malformed', 'retries', 'model esc.', 'avg ms'],
    (r.by_task_type || []).map(m => [m.task_type, m.model, m.calls, m.ok, m.malformed, m.retries, m.model_escalations, m.avg_duration_ms]));
  html += renderTable(['leaf model', 'attempts', 'passes', 'pass rate', 'first try'],
    (r.leaves || []).map(m => [m.model, m.attempts, m.passes, ((m.pass_rate || 0) * 100).toFixed(0) + '%', m.first_try_wins]));
  const fails = r.failures || [];
  if(fails.length){
    html += '<div class="k" style="color:var(--dim); margin:8px 0 6px;">FAILURES</div>';
    fails.forEach(f => { html += `<div>${esc(f)}</div>`; });
  }
  overlayTitle.textContent = 'Run Report';
  reportBody.innerHTML = html;
  reportOverlay.hidden = false;
}

async function showReport(){
  try{
    renderReport(await getJSON('/pipeline/report', { run: runId }));
  } catch(err){
    overlayTitle.textContent = 'Run Report';
    reportBody.textContent = err.message;
    reportOverlay.hidden = false;
  }
}

async function showDecisions(){
  overlayTitle.textContent = 'Decisions';
  try{
    const body = await getJSON('/pipeline/decisions', { run: runId });
    let html = '';
    (body.decisions || []).forEach(d => {
      html += `<div style="margin-bottom:8px;"><b>${esc(d.id)}</b> (round ${esc(d.round)}, ${esc(d.answered_by)}): ${esc(d.question)}<br><span style="color:var(--muted);">answer: ${esc(d.answer)}</span></div>`;
    });
    reportBody.innerHTML = html || '<div style="color:var(--muted);">No decisions recorded.</div>';
  } catch(err){
    reportBody.textContent = err.message;
  }
  reportOverlay.hidden = false;
}

function applyEvent(event, data){
  if(event === 'node-status'){
    const n = findNode(data.id);
    if(n){ n.status = data.status; n.wave = data.wave; }
    renderTree();
    if(current === data.id) loadNode(data.id);
  } else if(event === 'node-attempt'){
    const n = findNode(data.id);
    if(n && !(n.last_attempt && n.last_attempt.started_at === data.attempt.started_at)){
      n.last_attempt = data.attempt;
      n.attempt_count = (n.attempt_count || 0) + 1;
    }
    renderTree();
    if(current === data.id) loadNode(data.id);
  } else if(event === 'node-result'){
    const n = findNode(data.id);
    if(n) n.result = data.result;
  } else if(event === 'wave-changed'){
    hintEl.textContent = 'wave ' + data.wave + ' ' + data.state;
  } else if(event === 'run-status'){
    loadRuns().catch(() => {});
    loadState().catch(() => {});
  } else if(event === 'run-report'){
    renderReport(data);
  }
}

// parseSSEStream reads a fetch Response body as an SSE stream and calls
// onFrame(event, id, data) for each frame. A plain browser EventSource cannot
// carry an Authorization header, and every pipeline route requires the bearer
// token, so the stream is read through fetch. Comment frames (keepalives) have
// no event line and are skipped.
async function parseSSEStream(resp, onFrame){
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  while(true){
    const { value, done } = await reader.read();
    if(done) break;
    buf += decoder.decode(value, { stream: true });
    let sep;
    while((sep = buf.indexOf('\n\n')) !== -1){
      const frame = buf.slice(0, sep);
      buf = buf.slice(sep + 2);
      let event = '', id = '';
      const dataLines = [];
      frame.split('\n').forEach(line => {
        if(line.startsWith('event: ')) event = line.slice(7);
        else if(line.startsWith('id: ')) id = line.slice(4);
        else if(line.startsWith('data: ')) dataLines.push(line.slice(6));
      });
      if(event) onFrame(event, id, dataLines.join('\n'));
    }
  }
}

// connectEvents keeps one stream open for the selected run and reconnects
// after a drop, resuming at the last id it saw (since=), so nothing is missed
// and nothing is applied twice.
async function connectEvents(){
  if(eventsAbort) eventsAbort.abort();
  const gen = ++eventsGen;
  eventsAbort = new AbortController();
  const signal = eventsAbort.signal;
  while(gen === eventsGen){
    connStateEl.textContent = 'connecting';
    connStateEl.className = 'connstate off';
    try{
      const resp = await fetch(withQuery('/pipeline/events', { run: runId, since: String(lastSeq) }), { headers: authHeaders(), signal: signal });
      if(resp.headers.get('X-Gophermind-Pipeline-Schema') !== '1' || !resp.ok){
        connStateEl.textContent = 'error ' + resp.status;
        connStateEl.className = 'connstate err';
      } else {
        connStateEl.textContent = 'live';
        connStateEl.className = 'connstate live';
        await parseSSEStream(resp, (event, id, data) => {
          if(id) lastSeq = Math.max(lastSeq, Number(id));
          let parsed;
          try{ parsed = JSON.parse(data); } catch(e){ return; }
          applyEvent(event, parsed);
        });
      }
    } catch(err){
      if(signal.aborted) return;
      connStateEl.textContent = 'error';
      connStateEl.className = 'connstate err';
    }
    if(gen !== eventsGen) return;
    await new Promise(r => setTimeout(r, 2000));
  }
}

async function connect(){
  authToken = tokenInput.value.trim();
  hintEl.textContent = 'click a node on the left to inspect it';
  try{
    await loadRuns();
    await loadState();
  } catch(err){
    hintEl.textContent = 'failed to load the pipeline: ' + err.message;
    return;
  }
  connectEvents();
}

connectBtn.addEventListener('click', connect);
tokenInput.addEventListener('keydown', e => { if(e.key === 'Enter') connect(); });
reportBtn.addEventListener('click', showReport);
decisionsBtn.addEventListener('click', showDecisions);
closeReportBtn.addEventListener('click', () => { reportOverlay.hidden = true; });
runSelect.addEventListener('change', async () => {
  runId = runSelect.value;
  current = null;
  lastSeq = 0;
  try{ await loadState(); } catch(err){ hintEl.textContent = err.message; return; }
  connectEvents();
});

// Convenience: a token passed in the URL fragment (#token=...) auto-connects.
// The fragment, unlike the query string, is never sent to the server, never
// appears in a Referer header, and is not written to access logs; a bearer
// token here can run shell commands, so a leak of it is a leak of code
// execution. It is read once and the address bar is cleared at once. There is
// no query-string fallback: a fallback that works is a fallback people will use.
if(location.hash){
  const fragToken = new URLSearchParams(location.hash.slice(1)).get('token');
  history.replaceState(null, '', location.pathname);
  if(fragToken){
    tokenInput.value = fragToken;
    connect();
  }
}
</script>
</body>
</html>
HTML_EOF
cat "$tmp/head.html" "$tmp/tail.html" > "$f"
rm -rf -- "${tmp:?}"
```

The check `sed -n '305,306p'` above guarantees the cut is at `</style>`; the new tail starts with three CSS rules and then re-emits `</style></head>`. If `head -n 304` is not the last CSS rule of the old file, the page's first CSS rule is cut: check `tail -n 5 "$tmp/head.html"` before the `cat` and adjust the number so that it ends on a complete rule.

- [ ] **Step 4: Run to confirm green**

Run: `gt ./gophermind-lib/serve/ -run 'TestPipelineDashboard' -count=1 -v`
Expected: all dashboard tests pass (the three older security tests, the contract test, and the script check when `node` exists; otherwise the last shows SKIP).
Manual look (once, not automated): serve a fixture and open the page: `go run ./cmd/gophermind serve` needs a model, so use the Task 12 smoke script's server instead, and open `http://127.0.0.1:<port>/pipeline#token=<token>` against a run folder. Record in the commit message whether it was looked at.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/serve
git commit -m "feat(serve): the pipeline dashboard shows runs, the node tree, decisions, attempts and saved check output" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 9: The desktop client decodes the new contract (`gophermind-osx/client`)

**Files:**
- Modify: `gophermind-osx/client/service.go`, `gophermind-osx/client/sse.go`, `gophermind-osx/client/service_test.go`
- Modify: `gophermind-lib/serve/events.go` (delete the three task event types the client no longer uses)
- Create: `gophermind-osx/client/pipeline_test.go`

**Interfaces:**
- Produces: `type SchemaMismatchError struct{ Got, Want int }`; `const SchemaMismatchMessage`; `(*Client).PipelineRuns(ctx) ([]runview.RunSummary, error)`; `PipelineState(ctx, run string) (runview.StateBody, error)`; `PipelineNode(ctx, run, id string) (runview.NodeBody, error)`; `PipelineArtifact(ctx, run, id string, attempt int, file string) (string, error)`; `PipelineDecisions(ctx, run string) (runview.DecisionsBody, error)`; `PipelineReport(ctx, run string) (runview.ReportBody, error)`; `PipelineEvents(ctx, run string, since int64) (*EventStream, error)` (`since < 0` omits the parameter); `Event.ID int64`; `(*EventStream).LastID() int64`; `(*EventStream).Header() http.Header`; `Event.NodeStatus()`, `NodeAttempt()`, `NodeResult()`, `WaveChanged()`, `RunStatus()`, `RunReport()`.
- Consumes: `runview` types (Task 1), the golden files of Task 4, `serve.PipelineSchemaHeader`.

An empty `run` means the newest run (the server chooses).

- [ ] **Step 1: Write the failing tests**

`gophermind-osx/client/pipeline_test.go`:

```go
package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/runview"
)

const goldenDir = "../../gophermind-lib/runview/testdata/golden"

func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatalf("golden %s: %v (run `go test ./gophermind-lib/runview -run TestGolden -update` from the repo root)", name, err)
	}
	return b
}

// sameJSON fails unless a and b are the same JSON value. Decoding a golden
// file into a client type and encoding it again must lose nothing: a field the
// server sends and the client type lacks is caught here.
func sameJSON(t *testing.T, name string, a, b []byte) {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		t.Fatalf("%s: not JSON", name)
	}
	if !reflect.DeepEqual(x, y) {
		t.Errorf("%s: decoding into the client types and encoding again changed the document; the client lacks a field the server sends", name)
	}
}

func goldenServer(t *testing.T, header string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header != "" {
			w.Header().Set("X-Gophermind-Pipeline-Schema", header)
		}
		files := map[string]string{
			"/pipeline/runs": "runs.json", "/pipeline/state": "state.json", "/pipeline/node": "node-fn-bye.json",
			"/pipeline/decisions": "decisions.json", "/pipeline/report": "report-partial.json",
		}
		if r.URL.Path == "/pipeline/artifact" {
			io.WriteString(w, "--- FAIL: TestBye\n")
			return
		}
		if r.URL.Path == "/pipeline/events" {
			w.Header().Set("Content-Type", "text/event-stream")
			return
		}
		name, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(golden(t, name))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPipelineClientDecodesTheServersGoldenFiles(t *testing.T) {
	srv := goldenServer(t, "1")
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	ctx := context.Background()

	runs, err := c.PipelineRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].Status != "building" {
		t.Fatalf("PipelineRuns = %+v, %v", runs, err)
	}
	b, _ := json.Marshal(runview.RunsBody{SchemaVersion: 1, Runs: runs})
	sameJSON(t, "runs.json", golden(t, "runs.json"), b)

	st, err := c.PipelineState(ctx, "")
	if err != nil || st.Run == nil || len(st.Nodes) != 5 || st.EventSeq == 0 {
		t.Fatalf("PipelineState = %+v, %v", st, err)
	}
	b, _ = json.Marshal(st)
	sameJSON(t, "state.json", golden(t, "state.json"), b)

	nb, err := c.PipelineNode(ctx, "", "fn-bye")
	if err != nil || nb.Runtime == nil || len(nb.Artifacts) != 1 {
		t.Fatalf("PipelineNode = %+v, %v", nb, err)
	}
	b, _ = json.Marshal(nb)
	sameJSON(t, "node-fn-bye.json", golden(t, "node-fn-bye.json"), b)

	db, err := c.PipelineDecisions(ctx, "")
	if err != nil || len(db.Decisions) != 1 {
		t.Fatalf("PipelineDecisions = %+v, %v", db, err)
	}
	b, _ = json.Marshal(db)
	sameJSON(t, "decisions.json", golden(t, "decisions.json"), b)

	rb, err := c.PipelineReport(ctx, "")
	if err != nil || rb.IsComplete() || rb.Status != "building" {
		t.Fatalf("PipelineReport = %+v, %v", rb, err)
	}
	b, _ = json.Marshal(rb)
	sameJSON(t, "report-partial.json", golden(t, "report-partial.json"), b)

	txt, err := c.PipelineArtifact(ctx, "", "fn-bye", 1, "check-output.txt")
	if err != nil || !strings.Contains(txt, "TestBye") {
		t.Fatalf("PipelineArtifact = %q, %v", txt, err)
	}
}

func TestPipelineClientSchemaMismatch(t *testing.T) {
	for name, header := range map[string]string{"no header (an old server)": "", "another version": "2"} {
		srv := goldenServer(t, header)
		c := New(Config{BaseURL: srv.URL, Token: "t"})
		calls := map[string]func() error{
			"runs":      func() error { _, err := c.PipelineRuns(context.Background()); return err },
			"state":     func() error { _, err := c.PipelineState(context.Background(), ""); return err },
			"node":      func() error { _, err := c.PipelineNode(context.Background(), "", "x"); return err },
			"decisions": func() error { _, err := c.PipelineDecisions(context.Background(), ""); return err },
			"report":    func() error { _, err := c.PipelineReport(context.Background(), ""); return err },
			"artifact":  func() error { _, err := c.PipelineArtifact(context.Background(), "", "x", 1, "reply.txt"); return err },
			"events":    func() error { _, err := c.PipelineEvents(context.Background(), "", -1); return err },
		}
		for call, f := range calls {
			var mm *SchemaMismatchError
			if err := f(); !errors.As(err, &mm) || mm.Want != 1 {
				t.Errorf("%s / %s: err = %v, want *SchemaMismatchError", name, call, err)
			}
		}
	}
}

func TestPipelineClientBodyVersionIsCheckedToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		io.WriteString(w, `{"tasks":[],"generated_at":"2026-01-01T00:00:00Z"}`) // the old shape
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	var mm *SchemaMismatchError
	if _, err := c.PipelineState(context.Background(), ""); !errors.As(err, &mm) || mm.Got != 0 {
		t.Errorf("err = %v, want a mismatch with Got 0", err)
	}
}

func TestPipelineClientSendsRunAndQueryEscaped(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		io.WriteString(w, `{"schema_version":1,"node":{},"runtime":null,"artifacts":[]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	if _, err := c.PipelineNode(context.Background(), "gm-2026-10-08-001", "fn a&b"); err != nil {
		t.Fatal(err)
	}
	if got != "id=fn+a%26b&run=gm-2026-10-08-001" {
		t.Errorf("query = %q", got)
	}
}

func sseServer(t *testing.T, header string, onReq func(*http.Request), body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onReq != nil {
			onReq(r)
		}
		if header != "" {
			w.Header().Set("X-Gophermind-Pipeline-Schema", header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPipelineEventsParsesIDsAndDecodesEveryType(t *testing.T) {
	var query string
	body := ": keepalive\n\n" +
		"id: 7\nevent: node-status\ndata: {\"id\":\"fn-a\",\"status\":\"verified\",\"wave\":1}\n\n" +
		"id: 8\nevent: node-attempt\ndata: {\"id\":\"fn-a\",\"attempt\":{\"model\":\"m\",\"verdict\":\"pass\",\"started_at\":\"t\"}}\n\n" +
		"id: 8\nevent: node-result\ndata: {\"id\":\"fn-a\",\"result\":{\"commit\":\"abc\",\"files_changed\":[\"a.go\"]}}\n\n" +
		"id: 8\nevent: wave-changed\ndata: {\"wave\":1,\"state\":\"finished\"}\n\n" +
		"id: 8\nevent: run-status\ndata: {\"status\":\"verified\",\"stop_reason\":\"\"}\n\n" +
		"id: 8\nevent: run-report\ndata: {\"schema_version\":2,\"run_id\":\"r\",\"status\":\"verified\"}\n\n"
	srv := sseServer(t, "1", func(r *http.Request) { query = r.URL.RawQuery }, body)
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	s, err := c.PipelineEvents(context.Background(), "gm-2026-10-08-001", 6)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if query != "run=gm-2026-10-08-001&since=6" {
		t.Errorf("query = %q", query)
	}
	if s.Header().Get("X-Gophermind-Pipeline-Schema") != "1" {
		t.Error("the stream must expose the response header")
	}
	next := func() Event {
		ev, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	ev := next()
	ns, err := ev.NodeStatus()
	if ev.ID != 7 || ev.Type != "node-status" || err != nil || ns.ID != "fn-a" || ns.Status != "verified" || ns.Wave != 1 {
		t.Errorf("node-status = %+v %+v %v", ev, ns, err)
	}
	if s.LastID() != 7 {
		t.Errorf("LastID = %d, want 7", s.LastID())
	}
	na, err := next().NodeAttempt()
	if err != nil || na.Attempt.Model != "m" || na.Attempt.Verdict != "pass" {
		t.Errorf("node-attempt = %+v %v", na, err)
	}
	nr, err := next().NodeResult()
	if err != nil || nr.Result.Commit != "abc" {
		t.Errorf("node-result = %+v %v", nr, err)
	}
	wc, err := next().WaveChanged()
	if err != nil || wc.Wave != 1 || wc.State != "finished" {
		t.Errorf("wave-changed = %+v %v", wc, err)
	}
	rs, err := next().RunStatus()
	if err != nil || rs.Status != "verified" {
		t.Errorf("run-status = %+v %v", rs, err)
	}
	rr, err := next().RunReport()
	if err != nil || rr.RunID != "r" || !rr.IsComplete() {
		t.Errorf("run-report = %+v %v", rr, err)
	}
	if s.LastID() != 8 {
		t.Errorf("LastID = %d, want 8", s.LastID())
	}
}

func TestPipelineEventsOmitsSinceWhenNegative(t *testing.T) {
	var query string
	srv := sseServer(t, "1", func(r *http.Request) { query = r.URL.RawQuery }, "")
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	s, err := c.PipelineEvents(context.Background(), "", -1)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if query != "" {
		t.Errorf("query = %q, want none", query)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run (from `gophermind-osx`): `gt ./client/ -count=1`
Expected: build failure (`c.PipelineRuns undefined`).

- [ ] **Step 3: `service.go`**

Imports: remove `gophermind/gophermind-lib/phaseflow`; add `net/url`, `strconv` and `gophermind/gophermind-lib/runview`. Replace `do` and `attempt` (they change shape to return the response header) with:

```go
// do executes one request with retry-with-backoff for transient failures
// (network errors, 5xx) and returns the body. doH is the same but also returns
// the response header, which the pipeline calls need for the schema check.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	b, _, err := c.doH(ctx, method, path, body)
	return b, err
}

func (c *Client) doH(ctx context.Context, method, path string, body []byte) ([]byte, http.Header, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retry.MaxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(c.retry.backoff(attempt - 1)):
			}
		}

		respBody, status, hdr, err := c.attempt(ctx, method, path, body)
		if err == nil && !isRetryableStatus(status) {
			if status < 200 || status >= 300 {
				return nil, hdr, &StatusError{StatusCode: status, Body: string(respBody)}
			}
			return respBody, hdr, nil
		}
		if err == nil {
			lastErr = &StatusError{StatusCode: status, Body: string(respBody)}
		} else {
			lastErr = err
			if ctx.Err() != nil {
				return nil, nil, lastErr // context cancelled/deadline: don't keep retrying
			}
		}
	}
	return nil, nil, fmt.Errorf("after %d attempts: %w", c.retry.MaxAttempts, lastErr)
}

// attempt performs exactly one HTTP round trip. It returns (body, status,
// header, nil) for any response actually received (a non-2xx one included:
// retrying is the caller's decision), or an error for a network-level failure.
func (c *Client) attempt(ctx context.Context, method, path string, body []byte) ([]byte, int, http.Header, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, 0, nil, err
	}
	c.authorize(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, 0, nil, err
	}
	return respBody, resp.StatusCode, resp.Header, nil
}
```

Keep the doc comment of the old `do` (about reading the body into memory) above `do`. Replace the whole `// --- Pipeline ---` section (`PipelineState`, `PipelineReport`, `PipelineEvents`) with:

```go
// --- Pipeline ---

// SchemaMismatchMessage is what the pipeline panel shows when the server and
// this app speak different versions of the pipeline contract.
const SchemaMismatchMessage = "This server and this app disagree about the pipeline contract. Update the older one."

// SchemaMismatchError is returned by every pipeline call when the server's
// contract version (header X-Gophermind-Pipeline-Schema, or the body's
// schema_version) is not the one this app was built for. An old server that
// sends neither gives Got 0. Chat does not use the pipeline contract, so it
// keeps working.
type SchemaMismatchError struct{ Got, Want int }

func (e *SchemaMismatchError) Error() string {
	return fmt.Sprintf("pipeline contract mismatch: server speaks version %d, this app speaks %d; update the older one", e.Got, e.Want)
}

func checkSchemaHeader(h http.Header) error {
	got, _ := strconv.Atoi(h.Get(serve.PipelineSchemaHeader))
	if got != runview.SchemaVersion {
		return &SchemaMismatchError{Got: got, Want: runview.SchemaVersion}
	}
	return nil
}

func pipelineQuery(path string, kv ...string) string {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			q.Set(kv[i], kv[i+1])
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// getPipeline fetches a pipeline JSON route, checks the schema header and,
// when checkBody is set, the body's own schema_version, then decodes it.
func getPipeline[T any](ctx context.Context, c *Client, path string, checkBody bool) (T, error) {
	var zero T
	body, hdr, err := c.doH(ctx, http.MethodGet, path, nil)
	if err != nil {
		return zero, err
	}
	if err := checkSchemaHeader(hdr); err != nil {
		return zero, err
	}
	if checkBody {
		var v struct {
			SchemaVersion int `json:"schema_version"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return zero, err
		}
		if v.SchemaVersion != runview.SchemaVersion {
			return zero, &SchemaMismatchError{Got: v.SchemaVersion, Want: runview.SchemaVersion}
		}
	}
	return decodeJSON[T](body)
}

// PipelineRuns backs GET /pipeline/runs, newest first.
func (c *Client) PipelineRuns(ctx context.Context) ([]runview.RunSummary, error) {
	b, err := getPipeline[runview.RunsBody](ctx, c, "/pipeline/runs", true)
	return b.Runs, err
}

// PipelineState backs GET /pipeline/state. An empty run is the newest run.
func (c *Client) PipelineState(ctx context.Context, run string) (runview.StateBody, error) {
	return getPipeline[runview.StateBody](ctx, c, pipelineQuery("/pipeline/state", "run", run), true)
}

// PipelineNode backs GET /pipeline/node.
func (c *Client) PipelineNode(ctx context.Context, run, id string) (runview.NodeBody, error) {
	return getPipeline[runview.NodeBody](ctx, c, pipelineQuery("/pipeline/node", "run", run, "id", id), true)
}

// PipelineDecisions backs GET /pipeline/decisions.
func (c *Client) PipelineDecisions(ctx context.Context, run string) (runview.DecisionsBody, error) {
	return getPipeline[runview.DecisionsBody](ctx, c, pipelineQuery("/pipeline/decisions", "run", run), true)
}

// PipelineReport backs GET /pipeline/report. The body is the executor's
// report.json, which keeps its own schema_version, so only the header is
// checked. ReportBody.IsComplete says whether the run has finished.
func (c *Client) PipelineReport(ctx context.Context, run string) (runview.ReportBody, error) {
	return getPipeline[runview.ReportBody](ctx, c, pipelineQuery("/pipeline/report", "run", run), false)
}

// PipelineArtifact backs GET /pipeline/artifact: the text of one saved file of
// one attempt (file is reply.go, reply.txt, check-output.txt or attempt.json).
func (c *Client) PipelineArtifact(ctx context.Context, run, id string, attempt int, file string) (string, error) {
	body, hdr, err := c.doH(ctx, http.MethodGet,
		pipelineQuery("/pipeline/artifact", "run", run, "id", id, "attempt", strconv.Itoa(attempt), "file", file), nil)
	if err != nil {
		return "", err
	}
	if err := checkSchemaHeader(hdr); err != nil {
		return "", err
	}
	return string(body), nil
}

// PipelineEvents backs GET /pipeline/events: the live SSE feed of node-status,
// node-attempt, node-result, wave-changed, run-status and run-report events,
// each with an id. since resumes after that event id; a negative since omits it
// (the server then follows from the current end of the log). Not retried: see
// the package doc comment. A server with another contract version gives a
// *SchemaMismatchError and no stream.
func (c *Client) PipelineEvents(ctx context.Context, run string, since int64) (*EventStream, error) {
	kv := []string{"run", run}
	if since >= 0 {
		kv = append(kv, "since", strconv.FormatInt(since, 10))
	}
	s, err := c.openSSE(ctx, http.MethodGet, pipelineQuery("/pipeline/events", kv...), nil)
	if err != nil {
		return nil, err
	}
	if err := checkSchemaHeader(s.Header()); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
```

Add `"gophermind/gophermind-lib/serve"` to the imports of `service.go` (the package already imports it in `sse.go`). Update the package doc comment's mention of `PipelineEvents` only if it names the old signature (it names the method, not the signature: leave it).

- [ ] **Step 4: `sse.go`**

Replace the imports' `phaseflow` with `runview`. Add the id to `Event` and the stream:

```go
type Event struct {
	Type string
	Data string
	// ID is the frame's "id:" value (the pipeline stream sends the line number
	// of the run's event log), or 0 when the frame has none.
	ID int64
}
```

In `EventStream` add fields `header http.Header` and `lastID int64`, and methods:

```go
// Header is the response header the stream was opened with.
func (s *EventStream) Header() http.Header { return s.header }

// LastID is the id of the last frame Next returned that carried one. A caller
// that reconnects passes it as since.
func (s *EventStream) LastID() int64 { return s.lastID }
```

In `Next`, declare `var id int64` beside `event`, and add this branch before the "Any other field" comment:

```go
		if v, ok := sseField(line, "id"); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				id = n
			}
			continue
		}
```

Both `return Event{Type: event, Data: ...}, nil` statements become `return s.frame(event, data, id), nil` with

```go
func (s *EventStream) frame(event string, data []string, id int64) Event {
	if id > 0 {
		s.lastID = id
	}
	return Event{Type: event, Data: strings.Join(data, "\n"), ID: id}
}
```

In `openSSE` build the stream with `&EventStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body), header: resp.Header}`. Add `strconv` to the imports. Replace `TaskStatus`, `TaskAttempt`, `WaveChanged` and `RunReport` (and their comment) with:

```go
// NodeStatus, NodeAttempt, NodeResult, WaveChanged, RunStatus and RunReport
// decode Data for PipelineEvents' stream, whose vocabulary (node-status,
// node-attempt, node-result, wave-changed, run-status, run-report) differs from
// a session or run stream's. The types come from gophermind-lib/runview.
func (e Event) NodeStatus() (runview.NodeStatusData, error) {
	var v runview.NodeStatusData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) NodeAttempt() (runview.NodeAttemptData, error) {
	var v runview.NodeAttemptData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) NodeResult() (runview.NodeResultData, error) {
	var v runview.NodeResultData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) WaveChanged() (runview.WaveChangedData, error) {
	var v runview.WaveChangedData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

func (e Event) RunStatus() (runview.RunStatusData, error) {
	var v runview.RunStatusData
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}

// RunReport decodes the run-report event: the same body GET /pipeline/report
// returns.
func (e Event) RunReport() (runview.ReportBody, error) {
	var v runview.ReportBody
	err := json.Unmarshal([]byte(e.Data), &v)
	return v, err
}
```

- [ ] **Step 5: Old tests and `serve/events.go`**

In `service_test.go`: in `TestModelsAndCatalogueAndPipelineAndSkills` delete the two `case` blocks for `/pipeline/state` and `/pipeline/report` and the two `if ... PipelineState ... PipelineReport` blocks (they move to `pipeline_test.go`); delete `TestPipelineEvents_StreamsTaskStatus` (replaced by `TestPipelineEventsParsesIDsAndDecodesEveryType`). Remove imports the compiler reports unused.

In `gophermind-lib/serve/events.go` delete `TaskStatusEvent`, `TaskAttemptEvent`, `WaveChangedEvent` and the comment above `RunReportEvent` that names `phaseflow.RunReport`, and put in their place:

```go
// The pipeline stream (GET /pipeline/events) has its own event vocabulary:
// node-status, node-attempt, node-result, wave-changed, run-status and
// run-report. Its payload types live in gophermind-lib/runview (events.go
// would otherwise import the run folder reader).
```

- [ ] **Step 6: Run to confirm green**

Run: from the worktree root `gt ./gophermind-lib/serve/... -count=1`; then `cd gophermind-osx && gt ./client/ -count=1`
Expected: `ok` for both. `go vet ./client/` clean.
Note: `gophermind-osx/ui` and the `main` package still import `phaseflow` and the old client calls until Tasks 10 and 11; they do not build until then. Do not run `go build ./...` in `gophermind-osx` between Tasks 9 and 11.

- [ ] **Step 7: Commit**

```bash
git add -A gophermind-osx/client gophermind-lib/serve/events.go
git commit -m "feat(osx): the client speaks the node contract, parses event ids and reports a schema mismatch as a typed error" -m "Ruling: /pipeline/report is checked by header only because its body keeps the report's own schema_version." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 10: The desktop app's plain-Go pipeline state, pump, follower and formatters (`gophermind-osx/ui`)

All logic the panel needs is plain Go in `ui` so it is tested without cgo. The cgo panel (Task 11) only calls it.

**Files:**
- Modify (rewrite): `gophermind-osx/ui/pipeline.go`, `gophermind-osx/ui/pipelinepump.go`, `gophermind-osx/ui/pipeline_test.go`, `gophermind-osx/ui/pipelinepump_test.go`
- Create: `gophermind-osx/ui/pipelinefollow.go`, `gophermind-osx/ui/pipelinefollow_test.go`, `gophermind-osx/ui/pipelineformat.go`, `gophermind-osx/ui/pipelineformat_test.go`

**Interfaces:**
- Produces: `PipelineState` with `NewPipelineState()`, `OnChange`, `SetState(runview.StateBody)`, `Run() *runview.RunSummary`, `RunID() string`, `Nodes() []runview.NodeSummary`, `GeneratedAt() string`, `LastSeq() int64`, `SetLastSeq(int64)`, `ApplyNodeStatus(id, status string, wave int)`, `ApplyNodeAttempt(id string, a runview.Attempt)`, `ApplyNodeResult(id string, r runview.NodeResult)`, `ApplyWaveChanged(wave int, state string)`, `CurrentWave() (int, string)`, `ApplyRunStatus(status, reason string)`, `SetReport(runview.ReportBody)`, `Report() *runview.ReportBody`, `SetMessage(string)`, `Message() string`; `PipelinePump{State}` with `Run(ctx, *client.EventStream) error`; `type OpenEventsFunc func(ctx context.Context, run string, since int64) (*client.EventStream, error)`; `PipelineFollower{State, Open, Wait}` with `Run(ctx) error`; `FormatRunHeader`, `FormatNodeLine`, `FormatNodeTree`, `FormatReport`, `FormatNodeDetail`, `NodeSource`, `LoadNodeDetail`.
- Consumes: Task 9's client and `runview` types.

- [ ] **Step 1: Write the failing tests**

`gophermind-osx/ui/pipeline_test.go` (replaces the file):

```go
package ui

import (
	"sync/atomic"
	"testing"

	"gophermind/gophermind-lib/runview"
)

func sampleState(run string, seq int64) runview.StateBody {
	return runview.StateBody{
		SchemaVersion: 1, GeneratedAt: "2026-10-08T13:00:00Z", EventSeq: seq,
		Run: &runview.RunSummary{RunID: run, Status: "building"},
		Nodes: []runview.NodeSummary{
			{ID: "greeting", Kind: "component", Title: "Greeting", Status: "pending"},
			{ID: "fn-a", Kind: "function", Parent: "greeting", Status: "pending", Wave: 0},
			{ID: "fn-b", Kind: "function", Parent: "greeting", Status: "in_progress", Wave: 1},
		},
	}
}

func TestPipelineState_SetStateStoresRunNodesAndSeq(t *testing.T) {
	p := NewPipelineState()
	var changes int32
	p.OnChange(func() { atomic.AddInt32(&changes, 1) })
	p.SetState(sampleState("gm-2026-10-08-001", 12))
	if p.RunID() != "gm-2026-10-08-001" || len(p.Nodes()) != 3 || p.LastSeq() != 12 || p.GeneratedAt() == "" {
		t.Errorf("state = %q %d nodes seq %d", p.RunID(), len(p.Nodes()), p.LastSeq())
	}
	if atomic.LoadInt32(&changes) != 1 {
		t.Errorf("OnChange fired %d times, want 1", changes)
	}
	// The returned slices are copies.
	p.Nodes()[0].Status = "x"
	if p.Nodes()[0].Status == "x" {
		t.Error("Nodes() must return a copy")
	}
}

func TestPipelineState_RefetchKeepsTheSeqOfTheSameRunAndResetsOnANewRun(t *testing.T) {
	p := NewPipelineState()
	p.SetState(sampleState("gm-2026-10-08-001", 10))
	p.SetLastSeq(25) // the stream has moved on
	p.SetState(sampleState("gm-2026-10-08-001", 20))
	if p.LastSeq() != 25 {
		t.Errorf("LastSeq = %d, want 25 (an older snapshot must not rewind the stream)", p.LastSeq())
	}
	p.SetState(sampleState("gm-2026-10-08-001", 40))
	if p.LastSeq() != 40 {
		t.Errorf("LastSeq = %d, want 40", p.LastSeq())
	}
	p.SetState(sampleState("gm-2026-10-09-001", 3))
	if p.LastSeq() != 3 {
		t.Errorf("LastSeq = %d, want 3 after a different run", p.LastSeq())
	}
	p.SetLastSeq(1)
	if p.LastSeq() != 3 {
		t.Error("SetLastSeq must never move backwards")
	}
}

func TestPipelineState_ApplyNodeEvents(t *testing.T) {
	p := NewPipelineState()
	p.SetState(sampleState("gm-2026-10-08-001", 1))

	p.ApplyNodeStatus("fn-a", "verified", 0)
	p.ApplyNodeStatus("nope", "verified", 0) // unknown id: ignored
	a1 := runview.Attempt{Model: "m", Verdict: "fail", StartedAt: "t1"}
	p.ApplyNodeAttempt("fn-a", a1)
	p.ApplyNodeAttempt("fn-a", a1) // the same attempt twice (a replay): counted once
	p.ApplyNodeAttempt("fn-a", runview.Attempt{Model: "m", Verdict: "pass", StartedAt: "t2"})
	p.ApplyNodeResult("fn-a", runview.NodeResult{Commit: "abc"})
	n := p.Nodes()[1]
	if n.Status != "verified" || n.AttemptCount != 2 || n.LastAttempt == nil || n.LastAttempt.Verdict != "pass" || n.Result == nil || n.Result.Commit != "abc" {
		t.Errorf("fn-a = %+v", n)
	}
	p.ApplyWaveChanged(1, "started")
	if w, s := p.CurrentWave(); w != 1 || s != "started" {
		t.Errorf("wave = %d %q", w, s)
	}
	p.ApplyRunStatus("verified", "")
	if p.Run().Status != "verified" {
		t.Errorf("run status = %q", p.Run().Status)
	}
}

func TestPipelineState_ReportAndMessage(t *testing.T) {
	p := NewPipelineState()
	if p.Report() != nil || p.Message() != "" {
		t.Fatal("a new state has no report and no message")
	}
	var r runview.ReportBody
	r.Status = "verified"
	p.SetReport(r)
	p.SetMessage("this server and this app disagree")
	if p.Report() == nil || p.Report().Status != "verified" || p.Message() == "" {
		t.Errorf("report %+v message %q", p.Report(), p.Message())
	}
}
```

`gophermind-osx/ui/pipelinepump_test.go` (replaces the file):

```go
package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gophermind/gophermind-osx/client"
)

func eventsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPipelinePump_RunAppliesEveryEventTypeAndTracksTheLastID(t *testing.T) {
	srv := eventsServer(t,
		"id: 5\nevent: node-status\ndata: {\"id\":\"fn-a\",\"status\":\"in_progress\",\"wave\":0}\n\n"+
			"id: 6\nevent: node-attempt\ndata: {\"id\":\"fn-a\",\"attempt\":{\"model\":\"strong\",\"verdict\":\"pass\",\"started_at\":\"t\"}}\n\n"+
			"id: 7\nevent: node-result\ndata: {\"id\":\"fn-a\",\"result\":{\"commit\":\"abc\",\"files_changed\":[]}}\n\n"+
			"id: 8\nevent: wave-changed\ndata: {\"wave\":0,\"state\":\"finished\"}\n\n"+
			"id: 8\nevent: run-status\ndata: {\"status\":\"verified\",\"stop_reason\":\"\"}\n\n"+
			"id: 8\nevent: run-report\ndata: {\"schema_version\":2,\"run_id\":\"r\",\"status\":\"verified\"}\n\n")
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.PipelineEvents(context.Background(), "", -1)
	if err != nil {
		t.Fatal(err)
	}
	state := NewPipelineState()
	state.SetState(sampleState("gm-2026-10-08-001", 4))

	if err := (&PipelinePump{State: state}).Run(context.Background(), stream); err != nil {
		t.Fatalf("Run: %v", err)
	}
	n := state.Nodes()[1]
	if n.Status != "in_progress" || n.AttemptCount != 1 || n.LastAttempt == nil || n.LastAttempt.Model != "strong" || n.Result == nil {
		t.Errorf("fn-a = %+v", n)
	}
	if w, s := state.CurrentWave(); w != 0 || s != "finished" {
		t.Errorf("wave = %d %q", w, s)
	}
	if state.Run().Status != "verified" {
		t.Errorf("run status = %q", state.Run().Status)
	}
	if r := state.Report(); r == nil || r.RunID != "r" {
		t.Errorf("report = %+v", r)
	}
	if state.LastSeq() != 8 {
		t.Errorf("LastSeq = %d, want 8", state.LastSeq())
	}
}

func TestPipelinePump_RunReturnsNilOnCleanEOF(t *testing.T) {
	srv := eventsServer(t, "")
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.PipelineEvents(context.Background(), "", -1)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&PipelinePump{State: NewPipelineState()}).Run(context.Background(), stream); err != nil {
		t.Errorf("Run: %v, want nil for a clean stream end", err)
	}
}

func TestPipelinePump_AnUndecodableFrameStillAdvancesTheSeq(t *testing.T) {
	srv := eventsServer(t, "id: 9\nevent: node-status\ndata: {broken\n\n")
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, _ := c.PipelineEvents(context.Background(), "", -1)
	state := NewPipelineState()
	if err := (&PipelinePump{State: state}).Run(context.Background(), stream); err != nil {
		t.Fatal(err)
	}
	if state.LastSeq() != 9 {
		t.Errorf("LastSeq = %d, want 9: the line was consumed, so a reconnect must not replay it", state.LastSeq())
	}
}
```

`gophermind-osx/ui/pipelinefollow_test.go`:

```go
package ui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-osx/client"
)

func fastWait(int) time.Duration { return time.Millisecond }

func TestPipelineFollower_ReconnectsAtTheLastIDAfterADrop(t *testing.T) {
	var mu sync.Mutex
	var sinces []string
	var conns int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns++
		n := conns
		sinces = append(sinces, r.URL.Query().Get("since")+"/"+r.URL.Query().Get("run"))
		mu.Unlock()
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		switch n {
		case 1: // the first connection delivers two events, then drops
			io.WriteString(w, "id: 11\nevent: node-status\ndata: {\"id\":\"fn-a\",\"status\":\"in_progress\",\"wave\":0}\n\n")
			io.WriteString(w, "id: 12\nevent: wave-changed\ndata: {\"wave\":0,\"state\":\"started\"}\n\n")
		default: // the second stays up until the test ends
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	state := NewPipelineState()
	state.SetState(sampleState("gm-2026-10-08-001", 10))
	f := &PipelineFollower{State: state, Wait: fastWait, Open: func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		return c.PipelineEvents(ctx, run, since)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := len(sinces)
		mu.Unlock()
		if got >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the follower did not reconnect")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if sinces[0] != "10/gm-2026-10-08-001" || sinces[1] != "12/gm-2026-10-08-001" {
		t.Errorf("requests = %v, want since=10 then since=12 for the same run", sinces)
	}
}

func TestPipelineFollower_RetriesAfterAnOpenError(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		if n < 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	f := &PipelineFollower{State: NewPipelineState(), Wait: fastWait, Open: func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		return c.PipelineEvents(ctx, run, since)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := hits
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no retry")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
}

func TestPipelineFollower_ASchemaMismatchStopsAndSetsTheMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "2")
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer srv.Close()
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	state := NewPipelineState()
	f := &PipelineFollower{State: state, Wait: fastWait, Open: func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
		return c.PipelineEvents(ctx, run, since)
	}}
	err := f.Run(context.Background())
	var mm *client.SchemaMismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("Run = %v, want a *SchemaMismatchError", err)
	}
	if state.Message() != client.SchemaMismatchMessage {
		t.Errorf("message = %q", state.Message())
	}
}
```

`gophermind-osx/ui/pipelineformat_test.go`:

```go
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/runview"
)

func TestFormatNodeTree_GroupsByComponentWithStatusWaveModelAndAttempts(t *testing.T) {
	nodes := []runview.NodeSummary{
		{ID: "root", Kind: "root", Title: "R"},
		{ID: "greeting", Kind: "component", Title: "Greeting"},
		{ID: "fn-ok", Kind: "function", Parent: "greeting", Status: "verified", Wave: 0, ModelTier: "standard", AttemptCount: 2,
			LastAttempt: &runview.Attempt{Model: "qwen", Verdict: "pass"}},
		{ID: "fn-bad", Kind: "function", Parent: "greeting", Status: "failed", Wave: 1, AttemptCount: 3,
			LastAttempt: &runview.Attempt{Model: "qwen", Verdict: "fail", FailureReason: "test: TestBye failed"}},
		{ID: "fn-esc", Kind: "function", Parent: "greeting", Status: "escalated", Wave: 1},
		{ID: "fn-lost", Kind: "function", Parent: "gone", Status: "pending"},
	}
	lines := FormatNodeTree(nodes)
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"== Greeting", "[verified] fn-ok (wave 0, standard, 2 attempt(s))", "last: qwen pass",
		"[failed] fn-bad (wave 1, 3 attempt(s))", "last: qwen fail - test: TestBye failed",
		"[escalated] fn-esc", "fn-lost",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("tree lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "fn-ok") > strings.Index(text, "fn-bad") {
		t.Errorf("nodes must keep the order given:\n%s", text)
	}
	if strings.Contains(text, "root") {
		t.Errorf("the root node is not a tree row:\n%s", text)
	}
}

func TestFormatRunHeader(t *testing.T) {
	if got := FormatRunHeader(nil); got != "No run yet" {
		t.Errorf("nil run = %q", got)
	}
	got := FormatRunHeader(&runview.RunSummary{RunID: "gm-2026-10-08-001", Status: "failed", StopReason: "leaf_failed", Waves: 2,
		Counts: runview.Counts{Nodes: 3}, Coverage: runview.CoverageCount{Covered: 1, Total: 2}})
	for _, want := range []string{"gm-2026-10-08-001", "failed (leaf_failed)", "waves 2", "nodes 3", "coverage 1/2"} {
		if !strings.Contains(got, want) {
			t.Errorf("header lacks %q: %q", want, got)
		}
	}
}

func TestFormatReport(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "gophermind-lib", "runview", "testdata", "golden", "report-partial.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r runview.ReportBody
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	got := FormatReport(r)
	for _, want := range []string{"building", "(running)", "requirements 1/1", "implement", "qwen3.6"} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
	r.Complete = nil
	r.Status = "failed"
	r.Failures = []string{"fn-bye: test: TestBye"}
	got = FormatReport(r)
	if strings.Contains(got, "(running)") || !strings.Contains(got, "fn-bye: test: TestBye") {
		t.Errorf("final report text wrong:\n%s", got)
	}
}

func TestFormatNodeDetail(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "gophermind-lib", "runview", "testdata", "golden", "node-fn-bye.json"))
	if err != nil {
		t.Fatal(err)
	}
	var nb runview.NodeBody
	if err := json.Unmarshal(raw, &nb); err != nil {
		t.Fatal(err)
	}
	got := FormatNodeDetail(nb, "--- FAIL: TestBye\n    want bye")
	for _, want := range []string{"Function fn-bye", "in_progress", "1. qwen3.6 fail", "saved: attempt 1: reply.txt, check-output.txt, attempt.json", "--- FAIL: TestBye"} {
		if !strings.Contains(got, want) {
			t.Errorf("detail lacks %q:\n%s", want, got)
		}
	}
}

type fakeSource struct {
	body      runview.NodeBody
	artifacts map[string]string
	asked     []string
	err       error
}

func (f *fakeSource) PipelineNode(ctx context.Context, run, id string) (runview.NodeBody, error) {
	return f.body, f.err
}

func (f *fakeSource) PipelineArtifact(ctx context.Context, run, id string, attempt int, file string) (string, error) {
	f.asked = append(f.asked, file)
	if t, ok := f.artifacts[file]; ok {
		return t, nil
	}
	return "", errors.New("missing")
}

func TestLoadNodeDetailFetchesTheNewestCheckOutput(t *testing.T) {
	src := &fakeSource{
		body: runview.NodeBody{
			Node:      json.RawMessage(`{"id":"fn-x","title":"X"}`),
			Artifacts: []runview.ArtifactEntry{{Attempt: 1, Files: []string{"check-output.txt"}}, {Attempt: 2, Files: []string{"reply.txt", "check-output.txt"}}},
		},
		artifacts: map[string]string{"check-output.txt": "boom"},
	}
	got, err := LoadNodeDetail(context.Background(), src, "r", "fn-x")
	if err != nil || !strings.Contains(got, "boom") || len(src.asked) != 1 {
		t.Fatalf("detail = %q, %v, asked %v", got, err, src.asked)
	}
	src.body.Artifacts = nil
	src.asked = nil
	got, err = LoadNodeDetail(context.Background(), src, "r", "fn-x")
	if err != nil || len(src.asked) != 0 || strings.Contains(got, "boom") {
		t.Errorf("with no artifacts nothing is fetched: %q %v %v", got, err, src.asked)
	}
	src.err = errors.New("down")
	if _, err := LoadNodeDetail(context.Background(), src, "r", "fn-x"); err == nil {
		t.Error("a node fetch error must be returned")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run (from `gophermind-osx`): `gt ./ui/ -count=1`
Expected: build failure (`undefined: runview` usage in old files, `SetState` etc.).

- [ ] **Step 3: `pipeline.go`**

```go
package ui

import (
	"sync"

	"gophermind/gophermind-lib/runview"
)

// PipelineState is the pipeline panel's plain-Go state: the newest run's
// summary and node list from GET /pipeline/state, kept live by applying the
// GET /pipeline/events frames on top, the current wave, the run report once one
// arrives, and a message for the panel (a schema mismatch). It also remembers
// the last event id applied, which is where a reconnect resumes. Same split as
// Transcript/ApprovalTracker/ModelPickerState: mutex-protected, cgo-free,
// OnChange-driven.
type PipelineState struct {
	mu          sync.Mutex
	run         *runview.RunSummary
	nodes       []runview.NodeSummary
	generatedAt string
	lastSeq     int64
	wave        int
	waveState   string
	report      *runview.ReportBody
	message     string
	onChange    func()
}

// NewPipelineState returns an empty pipeline state.
func NewPipelineState() *PipelineState { return &PipelineState{} }

// OnChange registers f to be called after every mutation. Same non-blocking,
// non-reentrant contract as Transcript.OnChange.
func (p *PipelineState) OnChange(f func()) {
	p.mu.Lock()
	p.onChange = f
	p.mu.Unlock()
}

func (p *PipelineState) notify() {
	p.mu.Lock()
	f := p.onChange
	p.mu.Unlock()
	if f != nil {
		f()
	}
}

// SetState replaces the run and its nodes with a GET /pipeline/state snapshot.
// The stream position becomes the snapshot's event_seq for a new run; for the
// same run it only moves forward, so an older snapshot cannot rewind a stream
// that has already delivered later events.
func (p *PipelineState) SetState(b runview.StateBody) {
	p.mu.Lock()
	sameRun := p.run != nil && b.Run != nil && p.run.RunID == b.Run.RunID
	if b.Run != nil {
		r := *b.Run
		p.run = &r
	} else {
		p.run = nil
	}
	p.nodes = append([]runview.NodeSummary(nil), b.Nodes...)
	p.generatedAt = b.GeneratedAt
	if !sameRun || b.EventSeq > p.lastSeq {
		p.lastSeq = b.EventSeq
	}
	p.mu.Unlock()
	p.notify()
}

// Run returns a copy of the current run summary, or nil.
func (p *PipelineState) Run() *runview.RunSummary {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return nil
	}
	r := *p.run
	return &r
}

// RunID is the current run's id, or "".
func (p *PipelineState) RunID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return ""
	}
	return p.run.RunID
}

// Nodes returns a copy of the node list.
func (p *PipelineState) Nodes() []runview.NodeSummary {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]runview.NodeSummary(nil), p.nodes...)
}

// GeneratedAt is the timestamp of the last SetState snapshot.
func (p *PipelineState) GeneratedAt() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.generatedAt
}

// LastSeq is the id of the last event applied: where a reconnect resumes.
func (p *PipelineState) LastSeq() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSeq
}

// SetLastSeq moves the stream position forward; it never moves it back.
func (p *PipelineState) SetLastSeq(n int64) {
	p.mu.Lock()
	if n > p.lastSeq {
		p.lastSeq = n
	}
	p.mu.Unlock()
}

// find returns a pointer into p.nodes, or nil. Caller must hold p.mu.
func (p *PipelineState) find(id string) *runview.NodeSummary {
	for i := range p.nodes {
		if p.nodes[i].ID == id {
			return &p.nodes[i]
		}
	}
	return nil
}

// ApplyNodeStatus applies a node-status event. An id this state has not seen
// (an event before the first snapshot) is ignored, and nothing fires.
func (p *PipelineState) ApplyNodeStatus(id, status string, wave int) {
	p.mu.Lock()
	n := p.find(id)
	if n == nil {
		p.mu.Unlock()
		return
	}
	n.Status, n.Wave = status, wave
	p.mu.Unlock()
	p.notify()
}

// ApplyNodeAttempt applies a node-attempt event. An attempt with the same
// started_at as the last one is a replay and is not counted twice.
func (p *PipelineState) ApplyNodeAttempt(id string, a runview.Attempt) {
	p.mu.Lock()
	n := p.find(id)
	if n == nil || (n.LastAttempt != nil && n.LastAttempt.StartedAt == a.StartedAt && a.StartedAt != "") {
		p.mu.Unlock()
		return
	}
	n.LastAttempt = &a
	n.AttemptCount++
	p.mu.Unlock()
	p.notify()
}

// ApplyNodeResult applies a node-result event.
func (p *PipelineState) ApplyNodeResult(id string, r runview.NodeResult) {
	p.mu.Lock()
	n := p.find(id)
	if n == nil {
		p.mu.Unlock()
		return
	}
	n.Result = &r
	p.mu.Unlock()
	p.notify()
}

// ApplyWaveChanged applies a wave-changed event.
func (p *PipelineState) ApplyWaveChanged(wave int, state string) {
	p.mu.Lock()
	p.wave, p.waveState = wave, state
	p.mu.Unlock()
	p.notify()
}

// CurrentWave returns the most recently reported wave and its state
// ("started" or "finished").
func (p *PipelineState) CurrentWave() (wave int, state string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wave, p.waveState
}

// ApplyRunStatus applies a run-status event to the run summary.
func (p *PipelineState) ApplyRunStatus(status, reason string) {
	p.mu.Lock()
	if p.run == nil {
		p.mu.Unlock()
		return
	}
	p.run.Status, p.run.StopReason = status, reason
	p.mu.Unlock()
	p.notify()
}

// SetReport applies a run-report event or a direct report fetch.
func (p *PipelineState) SetReport(r runview.ReportBody) {
	p.mu.Lock()
	p.report = &r
	p.mu.Unlock()
	p.notify()
}

// Report returns the current report, or nil if none has arrived.
func (p *PipelineState) Report() *runview.ReportBody {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.report
}

// SetMessage sets the text the panel shows in place of the run header (a
// schema mismatch); an empty string clears it.
func (p *PipelineState) SetMessage(m string) {
	p.mu.Lock()
	p.message = m
	p.mu.Unlock()
	p.notify()
}

// Message returns the panel message.
func (p *PipelineState) Message() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.message
}
```

- [ ] **Step 4: `pipelinepump.go`**

```go
package ui

import (
	"context"
	"errors"
	"io"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-osx/client"
)

// PipelinePump reads a live client.EventStream from client.Client.
// PipelineEvents and applies each frame to a PipelineState -- the pipeline
// counterpart to StreamPump (stream.go), same shape: Run blocks and is meant
// to be called in its own goroutine, and always closes stream before
// returning. The pipeline stream has no "done" event and no natural end while
// the server is up; Run returns nil on a clean EOF (the connection closing)
// and the caller (PipelineFollower) reconnects from State.LastSeq().
type PipelinePump struct {
	State *PipelineState
}

// Run reads events from stream until it ends (io.EOF) or ctx is done,
// applying each to State. See StreamPump.Run's doc comment for why each Next()
// call runs in its own goroutine raced against ctx.Done().
func (p *PipelinePump) Run(ctx context.Context, stream *client.EventStream) error {
	defer stream.Close()

	type result struct {
		ev  client.Event
		err error
	}

	for {
		nextCh := make(chan result, 1)
		go func() {
			ev, err := stream.Next()
			nextCh <- result{ev, err}
		}()

		select {
		case <-ctx.Done():
			stream.Close()
			<-nextCh
			return ctx.Err()
		case r := <-nextCh:
			if errors.Is(r.err, io.EOF) {
				return nil
			}
			if r.err != nil {
				return r.err
			}
			p.apply(r.ev)
		}
	}
}

// apply decodes one frame into State and then advances the stream position, even
// for a frame it could not decode: the line was consumed, and a reconnect must
// not replay it forever.
func (p *PipelinePump) apply(ev client.Event) {
	switch ev.Type {
	case runview.EventNodeStatus:
		if e, err := ev.NodeStatus(); err == nil {
			p.State.ApplyNodeStatus(e.ID, e.Status, e.Wave)
		}
	case runview.EventNodeAttempt:
		if e, err := ev.NodeAttempt(); err == nil {
			p.State.ApplyNodeAttempt(e.ID, e.Attempt)
		}
	case runview.EventNodeResult:
		if e, err := ev.NodeResult(); err == nil {
			p.State.ApplyNodeResult(e.ID, e.Result)
		}
	case runview.EventWaveChanged:
		if e, err := ev.WaveChanged(); err == nil {
			p.State.ApplyWaveChanged(e.Wave, e.State)
		}
	case runview.EventRunStatus:
		if e, err := ev.RunStatus(); err == nil {
			p.State.ApplyRunStatus(e.Status, e.StopReason)
		}
	case runview.EventRunReport:
		if r, err := ev.RunReport(); err == nil {
			p.State.SetReport(r)
		}
	}
	p.State.SetLastSeq(ev.ID)
}
```

- [ ] **Step 5: `pipelinefollow.go`**

```go
package ui

import (
	"context"
	"errors"
	"time"

	"gophermind/gophermind-osx/client"
)

// OpenEventsFunc opens the pipeline event stream of a run from an event id,
// normally client.PipelineEvents behind the live connection.
type OpenEventsFunc func(ctx context.Context, run string, since int64) (*client.EventStream, error)

// PipelineFollower keeps a PipelinePump running: it opens the stream at
// State.LastSeq(), pumps it until it drops, waits with a backoff and opens it
// again at the new LastSeq, so a dropped connection loses and repeats nothing.
// A schema mismatch is not retried: it sets State's message and returns the
// error. Run returns ctx's error when ctx ends.
type PipelineFollower struct {
	State *PipelineState
	Open  OpenEventsFunc
	// Wait is the delay before reconnect attempt n (0-based). Nil means 1s,
	// doubling to 15s. Progress (a new event id) resets n.
	Wait func(n int) time.Duration
}

func (f *PipelineFollower) wait(n int) time.Duration {
	if f.Wait != nil {
		return f.Wait(n)
	}
	d := time.Second << uint(n)
	if d <= 0 || d > 15*time.Second {
		d = 15 * time.Second
	}
	return d
}

// Run follows until ctx ends or the server speaks another contract version.
func (f *PipelineFollower) Run(ctx context.Context) error {
	pump := &PipelinePump{State: f.State}
	for n := 0; ; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		stream, err := f.Open(ctx, f.State.RunID(), f.State.LastSeq())
		if err != nil {
			var mm *client.SchemaMismatchError
			if errors.As(err, &mm) {
				f.State.SetMessage(client.SchemaMismatchMessage)
				return err
			}
		} else {
			before := f.State.LastSeq()
			_ = pump.Run(ctx, stream)
			if f.State.LastSeq() > before {
				n = 0
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.wait(n)):
		}
	}
}
```

- [ ] **Step 6: `pipelineformat.go`**

```go
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/runview"
)

// FormatRunHeader is the panel's run line: id, status, waves, nodes, coverage.
func FormatRunHeader(r *runview.RunSummary) string {
	if r == nil {
		return "No run yet"
	}
	status := r.Status
	if r.StopReason != "" {
		status += " (" + r.StopReason + ")"
	}
	return fmt.Sprintf("%s: %s\nwaves %d, nodes %d, coverage %d/%d", r.RunID, status, r.Waves, r.Counts.Nodes, r.Coverage.Covered, r.Coverage.Total)
}

// FormatNodeLine is one function node: status, id, wave, model tier, attempt
// count, and its last attempt with the failure reason when there is one.
func FormatNodeLine(n runview.NodeSummary) string {
	line := fmt.Sprintf("  [%s] %s (wave %d", n.Status, n.ID, n.Wave)
	if n.ModelTier != "" {
		line += ", " + n.ModelTier
	}
	if n.AttemptCount > 0 {
		line += fmt.Sprintf(", %d attempt(s)", n.AttemptCount)
	}
	line += ")"
	if a := n.LastAttempt; a != nil {
		line += fmt.Sprintf(" last: %s %s", a.Model, a.Verdict)
		if a.FailureReason != "" {
			line += " - " + a.FailureReason
		}
	}
	return line
}

// FormatNodeTree lists the function nodes grouped under their component, in
// the order the server sent them. Function nodes whose component is not in the
// list come last under "== (no component)". Root nodes are not rows.
func FormatNodeTree(nodes []runview.NodeSummary) []string {
	byParent := map[string][]runview.NodeSummary{}
	comps := map[string]bool{}
	for _, n := range nodes {
		switch n.Kind {
		case "component":
			comps[n.ID] = true
		case "function":
			byParent[n.Parent] = append(byParent[n.Parent], n)
		}
	}
	var out []string
	for _, c := range nodes {
		if c.Kind != "component" {
			continue
		}
		title := c.Title
		if title == "" {
			title = c.ID
		}
		out = append(out, "== "+title)
		for _, n := range byParent[c.ID] {
			out = append(out, FormatNodeLine(n))
		}
	}
	var orphans []string
	for _, n := range nodes {
		if n.Kind == "function" && !comps[n.Parent] {
			orphans = append(orphans, FormatNodeLine(n))
		}
	}
	if len(orphans) > 0 {
		out = append(out, "== (no component)")
		out = append(out, orphans...)
	}
	return out
}

// FormatReport renders a report: status, node counts, requirements, calls per
// task type and model, leaf pass rates, and the failure lines.
func FormatReport(r runview.ReportBody) string {
	var b strings.Builder
	state := r.Status
	if !r.IsComplete() {
		state += " (running)"
	}
	fmt.Fprintf(&b, "Run report: %s\n", state)
	fmt.Fprintf(&b, "nodes: %d verified of %d (%d failed, %d escalated, %d blocked)\n",
		r.Nodes.Verified, r.Nodes.Total, r.Nodes.Failed, r.Nodes.Escalated, r.Nodes.Blocked)
	fmt.Fprintf(&b, "requirements %d/%d\n", r.Requirements.Covered, r.Requirements.Total)
	if len(r.ByTaskType) > 0 {
		b.WriteString("by task type:\n")
		for _, m := range r.ByTaskType {
			fmt.Fprintf(&b, "  %s  %s: %d calls, %d ok, %d malformed, %d retries\n", m.TaskType, m.Model, m.Calls, m.OK, m.Malformed, m.Retries)
		}
	}
	if len(r.Leaves) > 0 {
		b.WriteString("leaf models:\n")
		for _, m := range r.Leaves {
			fmt.Fprintf(&b, "  %s: %d/%d passed\n", m.Model, m.Passes, m.Attempts)
		}
	}
	if len(r.Failures) > 0 {
		b.WriteString("failures:\n")
		for _, f := range r.Failures {
			b.WriteString("  " + f + "\n")
		}
	}
	return b.String()
}

// FormatNodeDetail renders a node for the detail box: title, description, the
// reasoning groups that exist, its decisions, its tests, its runtime state and
// attempts, the saved artifacts, and the check output of the newest saved
// attempt when the caller fetched it.
func FormatNodeDetail(n runview.NodeBody, checkOutput string) string {
	var doc map[string]any
	_ = json.Unmarshal(n.Node, &doc)
	s := func(k string) string { v, _ := doc[k].(string); return v }
	sub := func(k, f string) string {
		m, _ := doc[k].(map[string]any)
		v, _ := m[f].(string)
		return v
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", s("title"), s("id"))
	if d := s("description"); d != "" {
		b.WriteString(d + "\n")
	}
	for _, g := range [][2]string{{"why", s("rationale")}, {"how", sub("construction", "approach_chosen")}, {"trust boundary", sub("security", "trust_boundary")}, {"complexity", sub("performance", "complexity")}} {
		if g[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", g[0], g[1])
		}
	}
	if ds, _ := doc["decisions"].([]any); len(ds) > 0 {
		b.WriteString("decisions:\n")
		for _, d := range ds {
			m, _ := d.(map[string]any)
			fmt.Fprintf(&b, "  %v: %v -> %v\n", m["id"], m["question"], m["answer"])
		}
	}
	if ts, _ := doc["tests"].([]any); len(ts) > 0 {
		var names []string
		for _, t := range ts {
			m, _ := t.(map[string]any)
			names = append(names, fmt.Sprint(m["name"]))
		}
		b.WriteString("tests: " + strings.Join(names, ", ") + "\n")
	}
	if rt := n.Runtime; rt != nil {
		fmt.Fprintf(&b, "status: %s (revision %d)\n", rt.Status, rt.Revision)
		for i, a := range rt.Attempts {
			fmt.Fprintf(&b, "  %d. %s %s", i+1, a.Model, a.Verdict)
			if a.FailureReason != "" {
				b.WriteString(" - " + a.FailureReason)
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("status: not started\n")
	}
	for _, a := range n.Artifacts {
		fmt.Fprintf(&b, "saved: attempt %d: %s\n", a.Attempt, strings.Join(a.Files, ", "))
	}
	if checkOutput != "" {
		b.WriteString("check output of the newest saved attempt:\n" + checkOutput + "\n")
	}
	return b.String()
}

// NodeSource is the part of client.Client LoadNodeDetail needs.
type NodeSource interface {
	PipelineNode(ctx context.Context, run, id string) (runview.NodeBody, error)
	PipelineArtifact(ctx context.Context, run, id string, attempt int, file string) (string, error)
}

// LoadNodeDetail fetches a node and, when it has a saved check output, the
// newest one, and formats them. A failed artifact fetch leaves the check
// output out; a failed node fetch is the error.
func LoadNodeDetail(ctx context.Context, src NodeSource, run, id string) (string, error) {
	body, err := src.PipelineNode(ctx, run, id)
	if err != nil {
		return "", err
	}
	check := ""
	for i := len(body.Artifacts) - 1; i >= 0 && check == ""; i-- {
		for _, f := range body.Artifacts[i].Files {
			if f == "check-output.txt" {
				if t, err := src.PipelineArtifact(ctx, run, id, body.Artifacts[i].Attempt, f); err == nil {
					check = t
				}
				break
			}
		}
	}
	return FormatNodeDetail(body, check), nil
}
```

- [ ] **Step 7: Run to confirm green**

Run (from `gophermind-osx`): `gofmt -l ui client` (prints nothing), `gt ./ui/ ./client/ -count=1 -race`
Expected: `ok` for both. `go vet ./ui/ ./client/` is clean.

- [ ] **Step 8: Commit**

```bash
git add -A gophermind-osx/ui
git commit -m "feat(osx): pipeline state, pump, reconnecting follower and formatters for the node contract" -m "Rulings: an older snapshot never rewinds the stream position; a replayed attempt is counted once; a frame that cannot be decoded still advances the position." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 11: The pipeline panel, `main.go` wiring and the e2e test (`gophermind-osx`)

The panel (`pipelinepanel.go`, cgo) now only calls the tested plain-Go layer of Task 10. It shows a run header (status, waves, coverage), the node tree grouped by component, the wave, a report block, and a detail box for the node whose id is typed into an entry. Nothing called `WatchEvents` before (finding 12 of the rulings), so the live half never ran; the panel now starts a `PipelineFollower` after its first successful state fetch.

**Files:**
- Modify: `gophermind-osx/pipelinepanel.go`, `gophermind-osx/pipelinepanel_test.go`, `gophermind-osx/main.go`
- Modify (rewrite): `gophermind-osx/e2e_pipeline_test.go`

**Interfaces:**
- Produces: `type FetchPipelineFunc func(ctx) (runview.StateBody, error)`; `type FetchReportFunc func(ctx, runID string) (runview.ReportBody, error)`; `type FetchNodeFunc func(ctx, runID, nodeID string) (string, error)`; `type PipelineLive struct{ Start StartBreakdownFunc; Fetch FetchPipelineFunc; Report FetchReportFunc; Node FetchNodeFunc; Open appui.OpenEventsFunc }`; `(*pipelinePanel).setLiveFuncs(PipelineLive)`; `seed()`, `follow()`, `doShow()`.
- Consumes: Task 10's `PipelineState`, `PipelineFollower`, `FormatNodeTree`, `FormatRunHeader`, `FormatReport`, `LoadNodeDetail`; Task 9's client.

- [ ] **Step 1: Write the failing tests**

In `gophermind-osx/pipelinepanel_test.go`: replace the `phaseflow` import with `gophermind/gophermind-lib/runview` (and drop `strings`, `time` if no longer used), replace `TestPipelinePanel_RefreshRebuildsTaskListOnStateChange` with the version below, delete `TestFormatTaskLine_IncludesStatusAndLatestAttempt` and `TestFormatRunReport_IncludesModelStats` (their replacements are `TestFormatNodeTree_...` and `TestFormatReport` in `ui`), and add the last test.

```go
func TestPipelinePanel_RefreshRebuildsTheNodeListOnStateChange(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		pp := newPipelinePanel(state, app.window, nil, nil, nil)
		attachControlForTest(app.window, pp.Control())

		// state.OnChange dispatches pp.refresh via queueMain, which only runs
		// once uiMain is pumping (see chatview_test.go), so this calls
		// refresh() directly, the same thing queueMain would eventually run.
		comp := runview.NodeSummary{ID: "greeting", Kind: "component", Title: "Greeting"}
		state.SetState(runview.StateBody{
			Run:   &runview.RunSummary{RunID: "gm-2026-10-08-001", Status: "building"},
			Nodes: []runview.NodeSummary{comp, {ID: "fn-a", Kind: "function", Parent: "greeting", Status: "in_progress"}},
		})
		pp.refresh()
		if len(pp.nodeLabels) != 2 { // the component row and one node row
			t.Errorf("nodeLabels after one node = %d, want 2", len(pp.nodeLabels))
		}

		state.SetState(runview.StateBody{
			Run: &runview.RunSummary{RunID: "gm-2026-10-08-001", Status: "building"},
			Nodes: []runview.NodeSummary{comp,
				{ID: "fn-a", Kind: "function", Parent: "greeting"}, {ID: "fn-b", Kind: "function", Parent: "greeting"}},
		})
		pp.refresh()
		if len(pp.nodeLabels) != 3 {
			t.Errorf("nodeLabels after two nodes = %d, want 3", len(pp.nodeLabels))
		}
	})
}

func TestPipelinePanel_ShowNodeWithoutALiveConnectionDoesNotPanic(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		var said string
		pp := newPipelinePanel(appui.NewPipelineState(), app.window, func(s string) { said = s }, nil, nil)
		attachControlForTest(app.window, pp.Control())
		pp.doShow() // no run, no node source: say so, do not panic
		if said == "" {
			t.Error("doShow with nothing to show must tell the user why")
		}
	})
}
```

Replace `gophermind-osx/e2e_pipeline_test.go` with the file below. The first test keeps the original breakdown-seed round trip (it needs the model, as before); the second is new and needs no model: it serves a real run folder.

```go
//go:build e2e

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/runview/runviewtest"
	"gophermind/gophermind-osx/client"
	"gophermind/gophermind-osx/connection"
	appui "gophermind/gophermind-osx/ui"
)

// e2eConnectLocalWithRoot is e2eConnectLocal (e2e_local_test.go) but also
// returns the workspace root: the pipeline routes read root/.gophermind/<run>/
// of the spawned gophermind-server.
func e2eConnectLocalWithRoot(t *testing.T) (*connection.Connection, string) {
	t.Helper()
	bin := e2eBuildServerBinary(t)
	root := t.TempDir()

	conn := connection.New(connection.BackendConfig{
		Name: "e2e-pipeline",
		Mode: connection.ModeLocal,
		Local: connection.LocalConfig{
			ServerBinaryPath: bin,
			Root:             root,
			StartupTimeout:   15 * time.Second,
			// A real breakdown turn can run several LLM round trips plus tool
			// calls, easily exceeding client.DefaultTimeout (30s).
			ClientTimeout: 3 * time.Minute,
		},
		HealthInterval: 500 * time.Millisecond,
	})
	t.Cleanup(conn.Disconnect)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn.Status() != connection.StatusConnected {
		t.Fatalf("Status() = %v, want Connected", conn.Status())
	}
	return conn, root
}

// TestE2E_Pipeline_BreakdownSeed is the "start breakdown" half of the old
// pipeline e2e: create a session, send appui.BreakdownSeedPrompt through a real
// Stream call and confirm the round trip completes. Whether the model's
// answer is a good plan is model behavior and is not asserted.
func TestE2E_Pipeline_BreakdownSeed(t *testing.T) {
	conn, _ := e2eConnectLocalWithRoot(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seed := appui.BreakdownSeedPrompt("brief.md", "Build a tiny CLI that reverses stdin.")
	stream, err := cl.Stream(ctx, sessionID, seed)
	if err != nil {
		t.Fatalf("Stream (breakdown seed): %v", err)
	}
	// A breakdown turn may include gated tool calls; auto-approve them the way
	// TestE2E_LocalMode_ApprovalFlow does, so an unresolved approval never
	// stalls the stream.
	tracker := appui.NewApprovalTracker(cl.Approve)
	sawDone := false
	deadline := time.After(2 * time.Minute)
readSeed:
	for {
		select {
		case <-deadline:
			break readSeed
		default:
		}
		ev, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break readSeed
			}
			t.Fatalf("stream.Next (breakdown seed): %v", err)
		}
		if ev.Type == "approval-needed" {
			if an, err := ev.ApprovalNeeded(); err == nil {
				tracker.Add(sessionID, an.ApprovalID, an.Tool, an.Args)
				_ = tracker.Resolve(ctx, an.ApprovalID, true)
			}
		}
		if ev.Type == "done" {
			sawDone = true
			break readSeed
		}
	}
	stream.Close()
	if !sawDone {
		t.Fatal("breakdown-seed stream never reached done")
	}
}

type evResult struct {
	ev  client.Event
	err error
}

// nextEvent reads one event with a deadline; the read runs in its own
// goroutine because Next blocks on the connection.
func nextEvent(t *testing.T, s *client.EventStream, d time.Duration) client.Event {
	t.Helper()
	ch := make(chan evResult, 1)
	go func() {
		ev, err := s.Next()
		ch <- evResult{ev, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Next: %v", r.err)
		}
		return r.ev
	case <-time.After(d):
		t.Fatal("timed out waiting for a pipeline event")
	}
	return client.Event{}
}

// TestE2E_Pipeline_RunFolder serves a real run folder from the spawned
// server and reads every endpoint through the app's client, then watches a node
// change arrive on the event stream. No model is involved.
func TestE2E_Pipeline_RunFolder(t *testing.T) {
	conn, root := e2eConnectLocalWithRoot(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	fx := runviewtest.Build(t, root, runviewtest.Building)

	runs, err := cl.PipelineRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].RunID != fx.RunID || runs[0].Status != "building" {
		t.Fatalf("PipelineRuns = %+v, %v", runs, err)
	}
	st, err := cl.PipelineState(ctx, "")
	if err != nil || st.Run == nil || st.Run.RunID != fx.RunID || len(st.Nodes) != 5 || st.EventSeq == 0 {
		t.Fatalf("PipelineState = %+v, %v", st, err)
	}

	// The stream resumes exactly at the snapshot.
	events, err := cl.PipelineEvents(ctx, "", st.EventSeq)
	if err != nil {
		t.Fatalf("PipelineEvents: %v", err)
	}
	defer events.Close()

	if err := fx.BB.SetStatus(ctx, fx.RunID, "fn-bye", blackboard.StatusInProgress, blackboard.StatusVerified); err != nil {
		t.Fatal(err)
	}
	ev := nextEvent(t, events, 15*time.Second)
	ns, err := ev.NodeStatus()
	if ev.Type != "node-status" || err != nil || ns.ID != "fn-bye" || ns.Status != "verified" || ev.ID != st.EventSeq+1 {
		t.Fatalf("event = %+v %+v %v; want node-status fn-bye verified with id %d", ev, ns, err, st.EventSeq+1)
	}

	nb, err := cl.PipelineNode(ctx, "", "fn-bye")
	if err != nil || nb.Runtime == nil || nb.Runtime.Status != "verified" {
		t.Fatalf("PipelineNode = %+v, %v", nb, err)
	}
	txt, err := cl.PipelineArtifact(ctx, "", "fn-bye", 1, "check-output.txt")
	if err != nil || !strings.Contains(txt, "want bye, got hello") {
		t.Fatalf("PipelineArtifact = %q, %v", txt, err)
	}
	db, err := cl.PipelineDecisions(ctx, "")
	if err != nil || len(db.Decisions) != 1 {
		t.Fatalf("PipelineDecisions = %+v, %v", db, err)
	}
	rb, err := cl.PipelineReport(ctx, "")
	if err != nil || rb.IsComplete() {
		t.Fatalf("PipelineReport = %+v, %v (a running run gives a partial report)", rb, err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run (from `gophermind-osx`): `go vet . 2>&1 | head -20`
Expected: errors naming `phaseflow` and `nodeLabels` (the panel and `main.go` still use the old types). The tests cannot run until the next steps compile.

- [ ] **Step 3: Edit `pipelinepanel.go`**

E1 imports: replace `"gophermind/gophermind-lib/phaseflow"` with `"gophermind/gophermind-lib/runview"`, and add `"errors"` to the standard-library group.

E2 C preamble: add after the `attachStartClicked` helper:

```c
extern void goPipelineShowClicked(void *b, void *data);

static inline void attachShowClicked(uiButton *b, long long h) {
	uiButtonOnClicked(b, (void (*)(uiButton *, void *))goPipelineShowClicked, (void *)h);
}
```

E3 replace the `FetchPipelineFunc` declaration and its comment with:

```go
// FetchPipelineFunc fetches the newest run's state, normally
// client.PipelineState with an empty run id (the server picks the newest). A
// nil body and nil error mean "no backend yet": the panel stays empty.
type FetchPipelineFunc func(ctx context.Context) (runview.StateBody, error)

// FetchReportFunc fetches a run's report, normally client.PipelineReport.
type FetchReportFunc func(ctx context.Context, runID string) (runview.ReportBody, error)

// FetchNodeFunc returns one node's detail text, normally appui.LoadNodeDetail
// over the live client.
type FetchNodeFunc func(ctx context.Context, runID, nodeID string) (string, error)

// PipelineLive is what the panel needs from a live connection. The panel is
// built before any connection exists (chatinput.go), so main.go supplies these
// later through setLiveFuncs. Any field may be nil.
type PipelineLive struct {
	Start  StartBreakdownFunc
	Fetch  FetchPipelineFunc
	Report FetchReportFunc
	Node   FetchNodeFunc
	Open   appui.OpenEventsFunc
}
```

E4 in the `pipelinePanel` struct replace the lines `taskListBox  *C.uiBox`, `taskLabels   []*C.uiLabel` with `nodeListBox  *C.uiBox` and `nodeLabels   []*C.uiLabel`, and add after `reportEntry`:

```go
	runLabel    *C.uiLabel
	nodeEntry   *C.uiEntry
	showButton  *C.uiButton
	detailEntry *C.uiMultilineEntry

	fetchReport FetchReportFunc
	fetchNode   FetchNodeFunc
	openEvents  appui.OpenEventsFunc

	followMu     sync.Mutex
	followCancel context.CancelFunc
```

E5 in `newPipelinePanel`: rename `taskListBox` to `nodeListBox` in its declaration, the `C.uiBoxAppend` and the struct literal; add the run label right after the start button is appended:

```go
	runLabel := C.uiNewLabel(C.CString("No run yet"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(runLabel)), 0)
```

after the `reportEntry` is appended add:

```go
	nodeEntry := C.uiNewEntry()
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(nodeEntry)), 0)
	showButton := C.uiNewButton(C.CString("Show Node"))
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(showButton)), 0)
	detailEntry := C.uiNewMultilineEntry()
	C.uiMultilineEntrySetReadOnly(detailEntry, 1)
	C.uiBoxAppend(box, (*C.uiControl)(unsafe.Pointer(detailEntry)), 1)
```

add `runLabel: runLabel, nodeEntry: nodeEntry, showButton: showButton, detailEntry: detailEntry,` to the struct literal, `C.attachShowClicked(showButton, h)` after `C.attachStartClicked(startButton, h)`, and replace the whole `if fetchPipeline != nil { go func() { ... }() }` block (and the comment above it) with `pp.seed()`.

E6 replace `setLiveFuncs` (and its comment) with:

```go
// setLiveFuncs supplies the connection-dependent callbacks after construction
// and seeds the panel from the newest run, as the constructor does when it is
// handed a fetch up front. The panel is built in chatinput.go before any
// connection exists, so Start Breakdown stays inert until this runs (main.go,
// once the connection manager is up).
func (pp *pipelinePanel) setLiveFuncs(l PipelineLive) {
	pp.startBreakdown = l.Start
	pp.fetchPipeline = l.Fetch
	pp.fetchReport = l.Report
	pp.fetchNode = l.Node
	pp.openEvents = l.Open
	pp.seed()
}

// seed fetches the newest run's state in the background. On success it also
// fetches the report (best effort) and starts following the event stream. A
// server speaking another contract version sets the panel message instead.
// state.SetState fires OnChange, which is wired to queueMain, so this is safe
// to call from a goroutine.
func (pp *pipelinePanel) seed() {
	fetch := pp.fetchPipeline
	if fetch == nil {
		return
	}
	go func() {
		body, err := fetch(context.Background())
		if err != nil {
			var mm *client.SchemaMismatchError
			if errors.As(err, &mm) {
				pp.state.SetMessage(client.SchemaMismatchMessage)
			} else if pp.notify != nil {
				queueMain(func() { pp.notify("error fetching pipeline state: " + err.Error()) })
			}
			return
		}
		pp.state.SetMessage("")
		pp.state.SetState(body)
		if pp.fetchReport != nil && body.Run != nil {
			if r, err := pp.fetchReport(context.Background(), body.Run.RunID); err == nil {
				pp.state.SetReport(r)
			}
		}
		pp.follow()
	}()
}

// follow starts one PipelineFollower for the current run, unless one is
// already running or there is no run to follow. The follower reconnects at the
// last event id after a drop (appui.PipelineFollower).
func (pp *pipelinePanel) follow() {
	pp.followMu.Lock()
	defer pp.followMu.Unlock()
	if pp.openEvents == nil || pp.state.RunID() == "" || pp.followCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	pp.followCancel = cancel
	f := &appui.PipelineFollower{State: pp.state, Open: pp.openEvents}
	go func() {
		_ = f.Run(ctx)
		pp.followMu.Lock()
		pp.followCancel = nil
		pp.followMu.Unlock()
	}()
}
```

E7 delete `WatchEvents` (the follower replaces it; nothing called it).

E8 replace `refresh` (and its comment) with:

```go
// refresh rebuilds the node list and updates the run, wave and report text to
// match pp.state. Safe to call from state.OnChange (dispatched through
// queueMain, since an event can arrive from the follower's goroutine).
func (pp *pipelinePanel) refresh() {
	for i := len(pp.nodeLabels) - 1; i >= 0; i-- {
		C.uiBoxDelete(pp.nodeListBox, C.int(i))
		C.uiControlDestroy((*C.uiControl)(unsafe.Pointer(pp.nodeLabels[i])))
	}
	pp.nodeLabels = pp.nodeLabels[:0]
	for _, line := range appui.FormatNodeTree(pp.state.Nodes()) {
		cs := C.CString(line)
		label := C.uiNewLabel(cs)
		C.free(unsafe.Pointer(cs))
		C.uiBoxAppend(pp.nodeListBox, (*C.uiControl)(unsafe.Pointer(label)), 0)
		pp.nodeLabels = append(pp.nodeLabels, label)
	}

	head := pp.state.Message()
	if head == "" {
		head = appui.FormatRunHeader(pp.state.Run())
	}
	setLabel(pp.runLabel, head)

	wave, waveState := pp.state.CurrentWave()
	if waveState == "" {
		setLabel(pp.waveLabel, "No wave in progress")
	} else {
		setLabel(pp.waveLabel, fmt.Sprintf("Wave %d: %s", wave, waveState))
	}

	if r := pp.state.Report(); r != nil {
		cs := C.CString(appui.FormatReport(*r))
		C.uiMultilineEntrySetText(pp.reportEntry, cs)
		C.free(unsafe.Pointer(cs))
	}
}

// setLabel sets a label's text and frees the C copy (libui copies it).
func setLabel(l *C.uiLabel, s string) {
	cs := C.CString(s)
	C.uiLabelSetText(l, cs)
	C.free(unsafe.Pointer(cs))
}
```

E9 delete `formatTaskLine` and `formatRunReport` with their comments (replaced by `appui.FormatNodeTree` and `appui.FormatReport`).

E10 add, next to `doStart` and its exported callback:

```go
// doShow loads the node whose id is typed into the entry and shows its detail
// (groups, decisions, attempts, saved check output) in the detail box.
func (pp *pipelinePanel) doShow() {
	cText := C.uiEntryText(pp.nodeEntry)
	id := strings.TrimSpace(C.GoString(cText))
	C.uiFreeText(cText)
	run := pp.state.RunID()
	if pp.fetchNode == nil || run == "" || id == "" {
		if pp.notify != nil {
			pp.notify("Connect to a backend with a run, then type a node id such as fn-greet and click Show Node.")
		}
		return
	}
	go func() {
		text, err := pp.fetchNode(context.Background(), run, id)
		queueMain(func() {
			if err != nil {
				text = "error: " + err.Error()
			}
			cs := C.CString(text)
			C.uiMultilineEntrySetText(pp.detailEntry, cs)
			C.free(unsafe.Pointer(cs))
		})
	}()
}

//export goPipelineShowClicked
func goPipelineShowClicked(b unsafe.Pointer, data unsafe.Pointer) {
	h := C.longlong(uintptr(data))
	pipelinePanelMu.Lock()
	pp, ok := pipelinePanels[h]
	pipelinePanelMu.Unlock()
	if !ok {
		return
	}
	pp.doShow()
}
```

Update the file's header comment where it says "The task list is a plain rebuilt stack of labels" to say "node list". Other `C.CString` uses of the existing widgets are left as they are (not part of this change).

- [ ] **Step 4: Edit `main.go`**

Replace the import `"gophermind/gophermind-lib/phaseflow"` with `"gophermind/gophermind-lib/runview"`. In the `chat.pipelineUI.setLiveFuncs(` call, the first argument (the start-breakdown closure) keeps its body; wrap the whole call as `chat.pipelineUI.setLiveFuncs(PipelineLive{ Start: <that closure>, Fetch: ..., Report: ..., Node: ..., Open: ... })` and replace the second closure (the one returning `[]phaseflow.Task`) with:

```go
		Fetch: func(ctx context.Context) (runview.StateBody, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				// Not an error worth showing: with no backend the panel just
				// starts empty, which is its documented state, and the user
				// already gets a connect hint above.
				return runview.StateBody{}, nil
			}
			return cl.PipelineState(ctx, "")
		},
		Report: func(ctx context.Context, runID string) (runview.ReportBody, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				return runview.ReportBody{}, err
			}
			return cl.PipelineReport(ctx, runID)
		},
		Node: func(ctx context.Context, runID, nodeID string) (string, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				return "", err
			}
			return appui.LoadNodeDetail(ctx, cl, runID, nodeID)
		},
		Open: func(ctx context.Context, runID string, since int64) (*client.EventStream, error) {
			cl, err := liveClient(connMgr)
			if err != nil {
				return nil, err
			}
			return cl.PipelineEvents(ctx, runID, since)
		},
```

with the first field written as `Start: func(ctx context.Context, briefPath, prompt string) (string, error) { ... same body ... },`. (`appui` and `client` are already imported in `main.go`.)

- [ ] **Step 5: Run to confirm green**

Run (from `gophermind-osx`):
`gofmt -l .` (prints nothing); `go vet . ./client/ ./ui/`; `gt . -run 'Pipeline' -count=1`; `gt ./client/ ./ui/ -count=1 -race`
Then the e2e, which spawns the real server: `gt -tags e2e . -run 'TestE2E_Pipeline_RunFolder' -count=1 -timeout 5m`
Expected: all `ok`. `TestE2E_Pipeline_BreakdownSeed` needs the model and is not part of this step.
Clean up what the e2e helper leaves: `target="$(go env GOTMPDIR)"; [ -n "$target" ] || target="${TMPDIR:-/tmp}"; target="${target%/}/gophermind-e2e-test-bin"; case "$target" in /*gophermind-e2e-test-bin) echo "$target"; rm -rf -- "${target:?}";; *) echo "refusing $target";; esac`.
Also: `grep -rn 'phaseflow' gophermind-osx --include='*.go'` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add -A gophermind-osx
git commit -m "feat(osx): the pipeline panel shows the run, the node tree, the report and a node detail, and follows the stream" -m "Ruling: the panel never started the event follower before (WatchEvents had no caller); it now starts one after the first successful state fetch." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 12: The joint release gate (G4): both sides against one run folder, and an unsigned local app build

"Release together" is a process, not code: the server is deployed by John with `git pull` on the mini and the app is signed and notarised by John. What this task proves is that the two halves in this branch agree, so there is something safe to release. Nothing here deploys, installs or notarises.

**Files:**
- Modify: `gophermind-server/server_integration_test.go` (two tests)
- No other file changes; the app bundle is built and then deleted.

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Write the tests**

Append to `gophermind-server/server_integration_test.go` (add imports `bufio`, `encoding/json` if missing, `os`, `gophermind/gophermind-lib/runview`, `gophermind/gophermind-lib/runview/runviewtest` and `gophermind/gophermind-lib/serve`):

```go
// pipelinePaths is every pipeline route the contract has, with the query that
// makes it answer for the built fixture run.
var pipelinePaths = []string{
	"/pipeline/runs", "/pipeline/state", "/pipeline/node?id=fn-bye",
	"/pipeline/artifact?id=fn-bye&attempt=1&file=check-output.txt", "/pipeline/decisions", "/pipeline/report",
}

func getAuthed(t *testing.T, url, token string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, url, token, ""))
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// TestPipelineEndpointsAgainstARunFolder starts the real mux (buildDeps plus
// serve.NewMux) on a loopback listener over a run folder built by the real
// writers, and fetches every pipeline endpoint with a token. This is the
// server half of "released together": the desktop client tests decode the same
// bodies through the golden files.
func TestPipelineEndpointsAgainstARunFolder(t *testing.T) {
	srv, root := testServer(t, []string{finalAnswerBody("hi")}, "t")
	runviewtest.Build(t, root, runviewtest.Building)

	for _, p := range pipelinePaths {
		if resp, _ := getAuthed(t, srv.URL+p, ""); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a token: %d, want 401", p, resp.StatusCode)
		}
		resp, body := getAuthed(t, srv.URL+p, "t")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d %s", p, resp.StatusCode, body)
			continue
		}
		if resp.Header.Get(serve.PipelineSchemaHeader) != "1" {
			t.Errorf("%s: schema header = %q", p, resp.Header.Get(serve.PipelineSchemaHeader))
		}
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") && !json.Valid(body) {
			t.Errorf("%s: invalid JSON", p)
		}
	}

	var st runview.StateBody
	_, body := getAuthed(t, srv.URL+"/pipeline/state", "t")
	if json.Unmarshal(body, &st) != nil || st.Run == nil || st.Run.Status != "building" || len(st.Nodes) != 5 {
		t.Fatalf("state = %s", body)
	}

	// The event stream: replay from the start and read the first frame.
	resp, err := http.DefaultClient.Do(authedReq(t, http.MethodGet, srv.URL+"/pipeline/events?since=0", "t", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	var sawID, sawEvent bool
	for i := 0; i < 8 && !(sawID && sawEvent); i++ {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("event stream: %v", err)
		}
		sawID = sawID || strings.HasPrefix(line, "id: 1")
		sawEvent = sawEvent || strings.HasPrefix(line, "event: node-")
	}
	if !sawID || !sawEvent {
		t.Error("the first replayed frame must carry id: 1 and a node-* event")
	}
}

// TestPipelineSmokeRealRoot serves a real repo's runs and fetches every
// endpoint, printing what it found. It is skipped unless
// GOPHERMIND_SMOKE_ROOT names a repo that holds .gophermind/<run>/ (for
// example the csvstat repo after a /project run). The listener is
// 127.0.0.1 only; the token is the test's own.
func TestPipelineSmokeRealRoot(t *testing.T) {
	root := os.Getenv("GOPHERMIND_SMOKE_ROOT")
	if root == "" {
		t.Skip("set GOPHERMIND_SMOKE_ROOT to a repo with a run folder to run this")
	}
	llm := fakeLLM(t, []string{finalAnswerBody("hi")})
	defer llm.Close()
	d, err := buildDeps(serverConfig{Token: "t", LLMEndpoint: llm.URL, Root: root}, root, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	mux, err := serve.NewMux(d, serve.Options{Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, body := getAuthed(t, srv.URL+"/pipeline/runs", "t")
	var runs runview.RunsBody
	if json.Unmarshal(body, &runs) != nil || len(runs.Runs) == 0 {
		t.Fatalf("no runs under %s/.gophermind: %s", root, body)
	}
	newest := runs.Runs[0]
	t.Logf("newest run %s: %s (%s), %d function nodes, coverage %d/%d", newest.RunID, newest.Status, newest.StopReason,
		newest.Counts.Nodes, newest.Coverage.Covered, newest.Coverage.Total)
	for _, p := range []string{"/pipeline/state", "/pipeline/decisions", "/pipeline/report"} {
		resp, b := getAuthed(t, srv.URL+p+"?run="+newest.RunID, "t")
		if resp.StatusCode != http.StatusOK || resp.Header.Get(serve.PipelineSchemaHeader) != "1" || !json.Valid(b) {
			t.Errorf("%s: %d %s", p, resp.StatusCode, b)
		}
	}
	var st runview.StateBody
	_, b := getAuthed(t, srv.URL+"/pipeline/state?run="+newest.RunID, "t")
	_ = json.Unmarshal(b, &st)
	for _, n := range st.Nodes {
		if n.Kind != "function" {
			continue
		}
		resp, nb := getAuthed(t, srv.URL+"/pipeline/node?run="+newest.RunID+"&id="+n.ID, "t")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("node %s: %d %s", n.ID, resp.StatusCode, nb)
		}
	}
	for _, w := range st.Warnings {
		t.Logf("warning: %s", w)
	}
}
```

- [ ] **Step 2: Run the server tests**

Run: `gt ./gophermind-server/... -count=1 -race`
Expected: `ok`; `TestPipelineSmokeRealRoot` shows SKIP.
Real-root smoke (once the goal's task 11.1 has produced a run; skip it until then): `GOPHERMIND_SMOKE_ROOT=/path/to/csvstat-repo gt ./gophermind-server/ -run TestPipelineSmokeRealRoot -count=1 -v`
Expected: the log line for the newest run, no `warning:` lines other than ones you can explain, `ok`.

- [ ] **Step 3: Cross-check the three suites**

Run, in order, each to a file you read with `tail`: `gt ./gophermind-lib/runview/... ./gophermind-lib/serve/... -count=1 -race`; `(cd gophermind-osx && gt ./client/ ./ui/ -count=1 -race)`; `(cd gophermind-osx && gt -tags e2e . -run TestE2E_Pipeline_RunFolder -count=1)`. All `ok`. Then check that no side still speaks the old shape: `grep -rn 'assignments.json\|phaseflow.Task\|phaseflow.RunReport\|task-status\|task-attempt' --include='*.go' --include='*.html' gophermind-lib/serve gophermind-server gophermind-osx cmd/gophermind/main.go` prints nothing.

- [ ] **Step 4: Build the app, unsigned, and delete it**

Do not install and do not set `MACOS_SIGN_IDENTITY`. From `gophermind-osx`: `bash build-app.sh` (no argument; `install` would copy it to `/Applications`). Expected: `Bundle OK.` and `WARNING: MACOS_SIGN_IDENTITY unset, skipping signing (dev build).` If the script stops with `libui not found`, the machine lacks libui (see `gophermind-osx/README.md`): record that sentence in the commit message, run `go build ./... && go vet ./...` in `gophermind-osx` as the substitute, and report it; do not install libui here.
Check the bundle holds both binaries from this commit: `ls build/GopherMind.app/Contents/MacOS build/GopherMind.app/Contents/Resources/gophermind-server`.
Then free the disk (build output is git-ignored, but large): 

```bash
target="$PWD/build"; case "$target" in /*/gophermind-osx/build) echo "removing $target"; rm -rf -- "${target:?}";; *) echo "refusing $target";; esac
```

- [ ] **Step 5: Commit**

```bash
git add gophermind-server
git commit -m "test(server): every pipeline endpoint against a run folder built by the real writers; opt-in smoke over a real repo" -m "G4 gate: server, desktop client and app build agree on schema_version 1. Not deployed, not signed, not installed (John's steps)." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 13: Remove v1 (G5): the TUI flow, `plantree` and `orchestrate`

**Precondition: sub-project C is built.** Everything removed here is what `/project` used before C replaced it. The steps below name symbols and give the text found in the worktree today (before C); C renames the v1 commands to `/plan-v1` and `/plan-v1-execute` and adds `project_v2.go`, which shifts lines. Locate each edit by the quoted text, not by line number.

**Files:**
- Create: `gophermind-lib/gates/removal_test.go`, `gophermind-lib/tui/phase_removed.go`, `gophermind-lib/tui/phase_removed_test.go`, `gophermind-lib/tui/helpers_test.go`
- Delete: `gophermind-lib/plantree/` (with `plan/` and `export/`), `gophermind-lib/orchestrate/`, and in `gophermind-lib/tui/`: `project.go`, `questions.go`, `question_round.go`, `approve.go`, `execute.go` and the tests `project_test.go`, `project_flow_test.go`, `project_fixwave_test.go`, `questions_test.go`, `question_round_test.go`, `execute_test.go`, `execute_guard_test.go`, `fixwave_test.go`, `phase_test.go`
- Modify: `gophermind-lib/tui/update.go`, `model.go`, `view.go`, `commands.go`, `commands_registry.go`, `attention_test.go`, `completion_providers_test.go` (only if the compiler asks)

**Interfaces:**
- Produces: `tui.PhaseRemovedIn` (const `"0.10.0"`), `tui.PhaseRemovedNotice() string`; the gate tests `TestNoPackageImportsARemovedPackage` and `TestRemovedPackageDirectoriesAreGone`.
- Consumes: nothing new.

The `/phase` slash command in the TUI is not deleted outright: it prints the notice (the one-release stub), because `handlePhaseCommand` imports `phaseflow`, which Task 14 deletes. The CLI half of the stub is Task 14.

- [ ] **Step 1: Check the precondition**

Run: `grep -ln 'briefv2/projectrun' gophermind-lib/tui/*.go && grep -n '"/plan-v1"' gophermind-lib/tui/commands_registry.go`
Expected: the first lists `project_v2.go` (and others), the second prints one registry line. If either prints nothing, C is not built: stop and report; do not continue with Tasks 13 to 15.

- [ ] **Step 2: Write the gate test and watch it fail**

`gophermind-lib/gates/removal_test.go`:

```go
// Package gates holds tests that fail while something that was removed (or
// renamed) still exists. They read the whole repository, so they live in their
// own package instead of beside any one package.
package gates

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is the folder holding go.work, two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("no go.work at %s: the gates package moved?", root)
	}
	return root
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "testdata": true, "libui-ng": true, "dist": true}

// eachGoFile calls fn with the repo-relative path and the import paths of
// every .go file outside the skipped folders. A file that does not parse is
// skipped: the compiler is the judge of that.
func eachGoFile(t *testing.T, fn func(rel string, imports []string)) {
	t.Helper()
	root := repoRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		var imps []string
		for _, s := range f.Imports {
			imps = append(imps, strings.Trim(s.Path.Value, `"`))
		}
		rel, _ := filepath.Rel(root, p)
		fn(filepath.ToSlash(rel), imps)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// removedImports are the packages that no longer exist. Add a line here in the
// same commit that deletes a package.
var removedImports = []string{
	"gophermind/gophermind-lib/plantree",
	"gophermind/gophermind-lib/orchestrate",
}

// removedDirs are the folders that must not exist.
var removedDirs = []string{
	"gophermind-lib/plantree",
	"gophermind-lib/orchestrate",
}

func TestNoPackageImportsARemovedPackage(t *testing.T) {
	eachGoFile(t, func(rel string, imports []string) {
		for _, imp := range imports {
			for _, gone := range removedImports {
				if imp == gone || strings.HasPrefix(imp, gone+"/") {
					t.Errorf("%s imports %s, which was removed", rel, imp)
				}
			}
		}
	})
}

func TestRemovedPackageDirectoriesAreGone(t *testing.T) {
	root := repoRoot(t)
	for _, d := range removedDirs {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err == nil {
			t.Errorf("%s still exists", d)
		}
	}
}
```

Run: `gt ./gophermind-lib/gates/ -count=1`
Expected: FAIL, naming files of `gophermind-lib/tui` and `gophermind-lib/orchestrate` that import `plantree` or `orchestrate`, and both directories.

- [ ] **Step 3: Write the notice and its tests**

`gophermind-lib/tui/phase_removed.go`:

```go
package tui

// PhaseRemovedIn is the release that removed `gophermind phase` and /phase.
// The stub that prints PhaseRemovedNotice exists for that one release; a later
// change deletes the stub.
const PhaseRemovedIn = "0.10.0"

// PhaseRemovedNotice is what `gophermind phase ...` (exit code 2) and the /phase
// slash command print. One place, so the CLI and the TUI cannot drift apart.
func PhaseRemovedNotice() string {
	return "gophermind phase was removed in " + PhaseRemovedIn + ". " +
		"Plan and build a brief with \"gophermind brief plan\" and \"gophermind brief run\", or with /project. " +
		"Existing .planning/ folders are not read."
}
```

`gophermind-lib/tui/phase_removed_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestPhaseRemovedNoticeText(t *testing.T) {
	n := PhaseRemovedNotice()
	for _, want := range []string{"gophermind phase was removed in " + PhaseRemovedIn, `"gophermind brief plan"`, `"gophermind brief run"`, "/project", ".planning/ folders are not read"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice lacks %q: %s", want, n)
		}
	}
	if strings.ContainsRune(n, '\u2014') {
		t.Error("the notice must not contain an em dash")
	}
}

func TestPhaseTypedInASessionPrintsTheNoticeAndSendsNothing(t *testing.T) {
	m := sizedModel(t, 80, 24)
	next := submit(t, m, "/phase status")
	if !strings.Contains(next.content, PhaseRemovedNotice()) {
		t.Errorf("the transcript lacks the notice:\n%s", next.content)
	}
	if next.st != stateIdle {
		t.Errorf("state = %v: a retired command must not start an agent turn", next.st)
	}
}

func TestRegistryHasNoRetiredCommands(t *testing.T) {
	for _, name := range commandNames() {
		switch name {
		case "/phase", "/project-execute", "/plan-v1", "/plan-v1-execute", "/questions":
			t.Errorf("the registry still lists %s", name)
		}
	}
	if !strings.Contains(helpLine(), "/project") {
		t.Error("/project must stay in the help line")
	}
}
```

`gophermind-lib/tui/helpers_test.go` (helpers that lived in deleted test files and are still used):

```go
package tui

import (
	"os"
	"testing"
)

// withWorkdir runs fn with the process working directory temporarily set to dir.
func withWorkdir(t *testing.T, dir string, fn func()) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(prev) }()
	fn()
}

// submit runs one line through handleSubmit.
func submit(t *testing.T, m model, line string) model {
	t.Helper()
	m.input.SetValue(line)
	next, _ := m.handleSubmit()
	return next
}
```

If the compiler later reports another helper defined in a deleted test file and used by a surviving one (check `testModel`, `keys`, `settle` with `grep -n 'func testModel\|func keys\|func settle' gophermind-lib/tui/*_test.go` before deleting), move that function unchanged into `helpers_test.go`; do not rewrite a helper to fit.

- [ ] **Step 4: Delete and edit**

```bash
git rm -r gophermind-lib/plantree gophermind-lib/orchestrate
cd gophermind-lib/tui
git rm project.go questions.go question_round.go approve.go execute.go \
  project_test.go project_flow_test.go project_fixwave_test.go questions_test.go question_round_test.go \
  execute_test.go execute_guard_test.go fixwave_test.go phase_test.go
cd ../..
```

`withWorkdir` was defined in `phase_test.go` and `submit` in `questions_test.go`; both are now in `helpers_test.go` (Step 3). `keepBottomRows` (in `view.go`) was tested only by the deleted `fixwave_test.go` and used only by the question round's panel: it goes in the `view.go` edit below.

Edit `gophermind-lib/tui/update.go`:

1. Imports: remove `"gophermind/gophermind-lib/orchestrate"` and `"gophermind/gophermind-lib/phaseflow"`.
2. In `case tea.WindowSizeMsg:` delete the block that follows `m.input.SetWidth(msg.Width - 4)`: `if m.qphase == qAsking { ... }` (it sizes the question round).
3. Delete these cases in `Update`'s `switch`: `case execEventMsg:`, `case execProgressMsg:`, `case questionsProgressMsg, projectProgressMsg:`, `case questionsDoneMsg:`, `case projectPassesDoneMsg:`, `case execDoneMsg:` (each with its body and comments). If C added a `project_v2.go` message type with its own `case`, keep that case.
4. In `case errMsg:` the `if errors.Is(msg.err, context.Canceled) { ... } else { ... }` has a first branch that tests `len(m.execOutcomes) > 0` and prints a tally of the finished executor tasks, and an inner `else` that prints the plain cancelled line. Keep only the plain line, so the case reads:

```go
		if errors.Is(msg.err, context.Canceled) {
			m.appendLine(renderError("⨯ cancelled"))
		} else {
			m.appendLine(renderError("error: " + msg.err.Error()))
		}
```

(the `else` branch is unchanged) and delete the block after `m.cancel = nil` in that case that starts with the comment "A cancelled or failed pass ends the round, and the /project flow" through the closing brace of `if m.proj == projRunning { ... }` (both ifs: `m.qphase == qRunning` and `m.proj == projRunning`).
5. In the `tea.KeyEsc` case delete `if m.qphase == qAsking { return m.handleRoundKey(msg) }` and its comment (the paragraph beginning "The question round takes Esc while it is showing"). After the key switch delete the paragraph "While the question round is showing it owns the keyboard." with its `if m.qphase == qAsking { return m.handleRoundKey(msg) }`.
6. In `handleSubmit` delete the block that begins with the comment "While a /project flow is active, its state machine consumes input" through the end of `if m.proj != projNone { ... m = nm }`.
7. Replace the `/phase` block

```go
	// "/phase ..." runs the PhaseFlow workflow. State commands print to the
	// transcript and return; a loop step yields a seeded prompt sent to the agent
	// (the /phase line stays as the shown user prompt).
	sendText := text
	if strings.Fields(text)[0] == "/phase" {
		reply, agentTask := m.handlePhaseCommand(text)
		if agentTask == "" {
			m.appendLine(reply)
			m.sync()
			return m, nil
		}
		sendText = agentTask
	}
```

with

```go
	// "/phase" was retired: print what replaced it and send nothing to the agent.
	sendText := text
	if strings.Fields(text)[0] == "/phase" {
		m.appendLine(PhaseRemovedNotice())
		m.sync()
		return m, nil
	}
```

8. Delete the routing blocks for the v1 commands: `/plan-v1`, `/plan-v1-execute` and `/questions` (before C: `/project` -> `handleProjectCommand`, `/project-execute` -> `handleProjectExecuteCommand`, `/questions` -> `handleQuestionsCommand`). Keep the `/project` block that calls `handleProjectV2Command`.
9. In the comment before `goalPreamble`, change `Slash-command-derived sends (e.g. a "/phase" loop step)\n// are excluded by the leading "/" check on text.` to `Slash commands are excluded by the leading "/" check on text.`

Edit `model.go`: remove the imports of `phaseflow` and `plantree/plan`, and the fields (with their comments) `proj`, `projName`, `planWindow`, `qphase`, `round`, `completer`, `execOutcomes`. Keep the fields C added for `/project`.

Edit `view.go`: in the two `m.st == stateIdle && m.proj == projNone && m.qphase == qNone && m.complete.Mode() == ...` conditions drop `m.proj == projNone && m.qphase == qNone &&`; delete the block `if m.qphase != qNone { ... }` (the question round's panel) and the block `if m.proj != projNone { ... }` (the guided-flow dialog); delete `keepBottomRows` with its comment.

Edit `commands.go`: delete `handlePhaseCommand` and `phaseSlashHelp` with their comments and the imports they alone used (`phaseflow`, `strconv`; the compiler lists any other). If nothing else remains in the file, `git rm` it. Edit `commands_registry.go`: delete the entries `/project-execute`, `/questions`, `/phase` (and `/plan-v1`, `/plan-v1-execute` if C added them), keep `/project`, and delete the registry's comment paragraph that begins `"/phase" sub-commands have their own help string`.

Edit `attention_test.go`: remove the `phaseflow` import; in `TestTurnCompletionInvertsTheScreen` delete the `{"executor", execDoneMsg{summary: phaseflow.RunSummary{}}},` row; delete `TestPlanningProgressDoesNotInvert` (it tests `projectProgressMsg`, which is gone).

Run `go vet ./gophermind-lib/tui/` and fix what it reports by the rule: a symbol defined in a deleted file and used by a surviving non-test file means the edit above missed a v1 branch (delete the branch); a surviving test that exists only to test v1 behavior is deleted; a surviving test of other behavior that merely used a v1 helper is edited to stop needing it.

- [ ] **Step 5: Verify**

Run: `gofmt -l gophermind-lib cmd` (prints nothing); `gt ./gophermind-lib/gates/ ./gophermind-lib/tui/ -count=1`; `go build ./... && go vet ./...` from the worktree root, then `(cd gophermind-lib && go build ./... && go vet ./...)`.
Expected: the two gate tests pass, the `tui` suite passes, builds and vet are clean. `grep -rn 'plantree\|orchestrate' --include='*.go' . | grep -v '^./gophermind-lib/gates/'` prints only matches inside comments you then reword (for example `gophermind-lib/llm/client.go:617`, which names `phaseflow.FallbackRunner`; reword it to say "the executor's router" and do the same for `clone_test.go:13`).

- [ ] **Step 6: Commit**

```bash
git add -A gophermind-lib cmd
git commit -m "refactor: remove the v1 planner flow (plantree, orchestrate and the TUI's project, question and execute flow)" -m "Gate: a Go test fails if any package imports plantree or orchestrate or their folders exist. /phase in the TUI prints the removal notice until Task 14 finishes the CLI stub." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 14: Retire `/phase` (G6): the notice stub, delete `phaseflow`, the docs and the skill pack

**Files:**
- Modify: `gophermind-lib/gates/removal_test.go`; `cmd/gophermind/phase.go` (rewrite), `cmd/gophermind/main.go`, `cmd/gophermind/dispatch_test.go`
- Delete: `gophermind-lib/phaseflow/` (with `assets/` and `LICENSE.upstream`), `.gophermind/skills/gophermind-phaseflow.md`, `examples/phase-flow/ai-interview/`
- Modify: `README.md`, `PROJECT.md`, `GOPHERMIND.toml`, `CHANGELOG.md`, `CREDITS.md`, `docs/ARCHITECTURE-INDEX.md`, `docs/GOPHERMIND-SETUP.md`, `docs/CONTEXT-HANDOFF.md`, `desktop/frontend/src/App.tsx`, `.gophermind/skills/README.md`, `.gophermind/skills/gophermind-architecture.md`, `.gophermind/skills-packaging/SKILL.md`, `.gophermind/skills-packaging/references/INSTALL.md`, `.gophermind/skills-packaging/scripts/validate_pack.py`, `examples/phase-flow/briefs/README.md`, `gophermind-lib/llm/client.go` and `clone_test.go` (comments), `INDEX.md` (regenerated)

**Interfaces:**
- Produces: `runPhaseRemoved(w io.Writer) int` in `cmd/gophermind/phase.go`; `removedImports` and `removedDirs` in the gate test gain `phaseflow`.
- Consumes: `tui.PhaseRemovedNotice()` (Task 13).

- [ ] **Step 1: Extend the gate and add the stub test, and watch both fail**

In `gophermind-lib/gates/removal_test.go` add `"gophermind/gophermind-lib/phaseflow"` to `removedImports` and `"gophermind-lib/phaseflow"` to `removedDirs`.

Append to `cmd/gophermind/dispatch_test.go`:

```go
// runDispatchExit runs the compiled binary and returns its combined output and
// exit code, without failing the test on a non-zero exit.
func runDispatchExit(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(dispatchBinPath, args...)
	cmd.Env = dispatchEnv(t)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	t.Fatalf("gophermind %s: %v", strings.Join(args, " "), err)
	return "", -1
}

// TestPhaseStubPrintsTheNoticeAndExits2 pins the one-release stub: every form
// of `gophermind phase` prints the notice and exits 2, with no endpoint
// configured, so it works on a fresh machine.
func TestPhaseStubPrintsTheNoticeAndExits2(t *testing.T) {
	for _, args := range [][]string{{"phase"}, {"phase", "status"}, {"phase", "init", "Demo"}, {"-q", "phase", "execute", "1"}} {
		out, code := runDispatchExit(t, args...)
		if code != 2 {
			t.Errorf("gophermind %s: exit %d, want 2\n%s", strings.Join(args, " "), code, out)
		}
		if !strings.Contains(out, "gophermind phase was removed in") || !strings.Contains(out, "gophermind brief plan") {
			t.Errorf("gophermind %s: no notice:\n%s", strings.Join(args, " "), out)
		}
	}
}

func TestRunPhaseRemovedWritesTheSharedNotice(t *testing.T) {
	var b strings.Builder
	if code := runPhaseRemoved(&b); code != 2 || strings.TrimSpace(b.String()) != tui.PhaseRemovedNotice() {
		t.Errorf("code %d, text %q", code, b.String())
	}
}
```

(add `"gophermind/gophermind-lib/tui"` to that file's imports.)

Run: `gt ./gophermind-lib/gates/ ./cmd/gophermind/ -run 'TestNoPackageImports|TestRemovedPackage|TestPhaseStub|TestRunPhaseRemoved' -count=1`
Expected: FAIL: the gate names `cmd/gophermind/phase.go` and `gophermind-lib/phaseflow`; the stub tests fail to compile (`undefined: runPhaseRemoved`).

- [ ] **Step 2: The stub**

Replace the whole of `cmd/gophermind/phase.go` with:

```go
package main

import (
	"fmt"
	"io"

	"gophermind/gophermind-lib/tui"
)

// runPhaseRemoved is what `gophermind phase ...` does since PhaseFlow's roadmap
// workflow was removed: print the notice (shared with the TUI's /phase) and
// return exit code 2. The stub exists for one release.
func runPhaseRemoved(w io.Writer) int {
	fmt.Fprintln(w, tui.PhaseRemovedNotice())
	return 2
}
```

In `cmd/gophermind/main.go` delete the whole block that begins with the comment "`gophermind phase ...` runs the PhaseFlow spec-driven workflow" and ends with its `cmd = "run"` closing brace (the `if cmd == "phase" { handled, seeded, err := runPhase(...) ... }` block), and add, beside the early dispatches of `free` and `brief` (before `cfg.Validate()`), so it works with no endpoint configured:

```go
	// `gophermind phase ...` was removed with PhaseFlow's roadmap workflow. It
	// needs no configured endpoint to say so.
	if cmd == "phase" {
		os.Exit(runPhaseRemoved(os.Stderr))
	}
```

Also remove `phase` from any usage text in `main.go` (`grep -n 'phase' cmd/gophermind/main.go`).

- [ ] **Step 3: Delete PhaseFlow and what only it used**

```bash
git rm -r gophermind-lib/phaseflow examples/phase-flow/ai-interview .gophermind/skills/gophermind-phaseflow.md
```

`examples/phase-flow/ai-interview/` was the sample `.planning/` project for the removed workflow. `examples/phase-flow/briefs/` stays: those are brief files, not PhaseFlow state.

- [ ] **Step 4: Update the live text**

`README.md`: delete the section "## PhaseFlow: spec-driven workflow" (from the heading through the paragraph ending ``[`internal/phaseflow`](internal/phaseflow).``) and the section "### Autonomous execution: `/project-execute`" (through "...this command requires an approved plan."). In their place:

```markdown
## Plan and build from a brief

`gophermind brief plan <brief.md>` plans a brief into a tree of nodes under `.gophermind/<run-id>/` in your repo, `gophermind brief run <run-id>` builds it, and `/project <brief>` in the TUI (or `gophermind project <brief>`) does both in one run. Everything a run does is a browsable file in that folder: the nodes, their runtime state, the event log, the call log, the saved attempts and the report. `gophermind serve` and the desktop app show a run live (`GET /pipeline`). See `docs/briefv2/README.md`.

`gophermind phase` and `/phase` were removed in 0.10.0. PhaseFlow's roadmap workflow has no replacement; `.planning/` folders are not read.
```

`PROJECT.md`: in "What this is", the sentence that ends with the clause about "the PhaseFlow work" and a native spec-driven workflow (`internal/phaseflow`) becomes a list item "and a brief-to-build planner and executor (`gophermind-lib/briefv2`)."; in the core packages list replace the item `` `internal/phaseflow` (spec-driven workflow). `` with `` `gophermind-lib/briefv2` (brief planner and executor). ``; delete the section "## PhaseFlow (internal/phaseflow)" (through "See CREDITS.md for attribution.").

`GOPHERMIND.toml`: delete the line `  "internal/phaseflow",  # Spec-driven workflow`; in the description change `personas, prompt registry, and native spec-driven workflow (PhaseFlow).` to `personas, prompt registry, and a brief-to-build planner and executor.`; delete the block that starts `PhaseFlow (internal/phaseflow):` and ends `  - Embedded command support` (and the blank line after it).

`docs/ARCHITECTURE-INDEX.md`: delete the four tree lines of `phaseflow/` (``│   ├── phaseflow/ ...`` through ``│   │   └── README.md ...``) and the line ``├── .planning/                     # PhaseFlow state (gitignored)``; delete the section "### PhaseFlow State Machine (internal/phaseflow/)" through the end of its code block; delete the table row ``| Add PhaseFlow step | ...``; delete the bullet beginning ``- **PhaseFlow state**:``.

`docs/GOPHERMIND-SETUP.md`: in the bullet that describes `PROJECT.md` drop the trailing `, PhaseFlow`; delete the section "### `gophermind-phaseflow.md`" (heading, "Reference for:" and its six bullets); delete the bullet that begins `- **Work on PhaseFlow projects**`.

`docs/CONTEXT-HANDOFF.md`: on the checked item whose text names `internal/phaseflow/validate.go`, append ` (both files removed in 0.10.0)`. It is a dated checklist, so the line stays as history.

`CHANGELOG.md`, under `## [Unreleased]` (a blank line after the heading):

```markdown
### Removed

- **`gophermind phase` and `/phase` are removed**, with PhaseFlow's roadmap workflow, its embedded prompts and its `.planning/` files (`ROADMAP.md`, `STATE.md`, `assignments.json`). There is no replacement for the roadmap loop. `gophermind phase ...` and `/phase` now print a notice and the CLI exits 2; plan and build a brief with `gophermind brief plan` and `gophermind brief run`, or with `/project`. Existing `.planning/` folders are not read. The notice stub is removed in the release after this one.
- **The v1 planner and runner are removed**: the `plantree` and `orchestrate` packages, `/plan-v1`, `/plan-v1-execute`, `/project-execute` and `/questions`.

### Changed

- **Breaking: the pipeline contract.** The server's `/pipeline/state`, `/pipeline/events` and `/pipeline/report` and the desktop app's pipeline panel now read the run folder (nodes, runtime files, event log, report) instead of `.planning/assignments.json`. Every body carries `schema_version` 1 and every response the header `X-Gophermind-Pipeline-Schema: 1`. New routes: `/pipeline/runs`, `/pipeline/node`, `/pipeline/artifact`, `/pipeline/decisions`. Event streams carry ids, resume with `since` or `Last-Event-ID`, and are capped at 16 (`GOPHERMIND_PIPELINE_MAX_STREAMS`). The server and the desktop app must be updated together; a pair that disagrees shows a message in the panel and chat keeps working.
- The `briefv2` package folder is now `gophermind-lib/build` and `docs/briefv2` is `docs/build` (import path change only, no behavior change).
```

`CREDITS.md`: replace the body of the "## PhaseFlow" section (keep the heading) with the text below. Do not touch the file's other sections, which keep their own punctuation.

```markdown
GopherMind once had a native spec-driven workflow (`gophermind phase ...`, `/phase`) that was a
port of **PhaseFlow** by **Lex Christopherson** (<https://github.com/jbrahy/metaphaseflow>), used
under the **MIT License** (Copyright 2025 Lex Christopherson). That workflow and the vendored
prompts were removed in 0.10.0, and no PhaseFlow code is part of GopherMind any more. Thank you.
```

`desktop/frontend/src/App.tsx`: replace the two lines

```text
      'Finally run `gophermind phase validate` (or the equivalent) and fix',
      'anything it reports before telling me the plan is ready.',
```

with

```text
      'Finally re-read assignments.json and check that every depends_on id',
      'names a task that exists and that no task depends on itself; fix',
      'anything that fails before telling me the plan is ready.',
```

`examples/phase-flow/briefs/README.md`: change the command line `/project <name> examples/phase-flow/briefs/01-cli-tool.md` to `/project examples/phase-flow/briefs/01-cli-tool.md` (the v2 form takes the brief path only).

Comments: `gophermind-lib/llm/client.go` (the comment that says `phaseflow.FallbackRunner is that caller`) and `gophermind-lib/llm/clone_test.go` (`in phaseflow.FallbackRunner`) now say `the executor's router`.

Skill pack: `.gophermind/skills/README.md` delete the table row ``| Operate an explicitly requested PhaseFlow project | `gophermind-phaseflow` |``; `.gophermind/skills/gophermind-architecture.md` change ``For workflow changes, use `gophermind-phaseflow` and inspect transition tests.`` to ``For workflow changes, inspect the transition tests of the blackboard and the executor (`gophermind-lib/briefv2/blackboard`, `executor`).``; `.gophermind/skills-packaging/SKILL.md` delete the bullet ``- [PhaseFlow](packs/gophermind-phaseflow.md)``, remove `, and explicitly requested PhaseFlow operations` from the description, and change "The seven files" to "The six files"; `.gophermind/skills-packaging/references/INSTALL.md` delete the line ``- `gophermind-phaseflow.md` ``; `.gophermind/skills-packaging/scripts/validate_pack.py`: remove `'gophermind-phaseflow.md',` from `FILES`, change the check title `'Exactly seven flat Markdown replacements'` to `'Exactly six flat Markdown replacements'`, and delete the `check('No root-level PhaseFlow state path', ...)` call (two lines). The audit records under `.gophermind/skills-packaging/references/` (REVIEW.md, PROVENANCE.txt, metrics, validation results, changes.diff) describe the pack as it was reviewed and stay as history.

- [ ] **Step 5: Regenerate the code index and verify**

Run: `go run ./cmd/gophermind -root "$PWD" index` (rewrites `INDEX.md`; review `git diff --stat INDEX.md` for removed packages only plus the new ones).
Run: `python3 -I -m py_compile .gophermind/skills-packaging/scripts/validate_pack.py && echo compiled` and delete the `__pycache__` it creates inside that folder: `target="$PWD/.gophermind/skills-packaging/scripts/__pycache__"; case "$target" in /*/skills-packaging/scripts/__pycache__) rm -rf -- "${target:?}";; esac`.
Run the gates and builds: `gofmt -l gophermind-lib cmd gophermind-server gophermind-osx` (prints nothing); `gt ./gophermind-lib/gates/ ./cmd/gophermind/ ./gophermind-lib/tui/ ./gophermind-lib/serve/... -count=1`; `go build ./... && go vet ./...`; `(cd gophermind-lib && go build ./... && go vet ./...)`; `(cd gophermind-osx && go build ./... && go vet ./...)`.
Run the text gate (live files only; the allowlist is the dated records and the notice itself):

```bash
git grep -n -I -i 'phaseflow\|gophermind phase\|/phase\b' -- . \
  ':(exclude)docs/superpowers' ':(exclude)docs/reviews' ':(exclude)docs/goals' ':(exclude)docs/GOAL.md' ':(exclude)docs/GOAL-UNIFICATION.md' \
  ':(exclude)docs/unification-overview.md' ':(exclude)docs/CONTEXT-HANDOFF.md' ':(exclude)docs/plans' \
  ':(exclude)CHANGELOG.md' ':(exclude)CREDITS.md' ':(exclude)PLAN.md' ':(exclude).planning' \
  ':(exclude).gophermind/skills-packaging/references' ':(exclude)gophermind-lib/briefv2/planner/testdata' \
  ':(exclude)gophermind-lib/gates' ':(exclude)gophermind-lib/tui/phase_removed*' ':(exclude)cmd/gophermind/phase.go' ':(exclude)cmd/gophermind/dispatch_test.go'
```

Expected: no output. Any hit is live text: fix it, or add the file to the exclusion list with a reason in the commit message (for dated records only). `projectctx` still reading `.planning/SPEC.md` is reported in the rulings and is not changed here (it does not match this pattern).

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: retire /phase and PhaseFlow (notice stub, phaseflow deleted, docs, skill pack and credits updated)" -m "Gate: no package imports phaseflow and its folder is gone (Go test); gophermind phase prints the notice and exits 2 (dispatch test). CREDITS keeps a thank-you line. Rulings: the stub names release 0.10.0 (tui.PhaseRemovedIn); examples/phase-flow/ai-interview removed with the workflow it demonstrated; the skill pack validator edited and checked with py_compile only because its packs folder is absent." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

### Task 15: Drop the version names (G7): `briefv2` becomes `build`, in one mechanical commit

This task is last and is one commit. It changes names only. Run it on a clean tree: `git status --short` prints nothing.

**Files:**
- Move: `gophermind-lib/briefv2/` to `gophermind-lib/build/`; `docs/briefv2/` to `docs/build/`
- Modify: `.gitignore` (two negations); every non-historical tracked file that contains `briefv2`; `gophermind-lib/gates/removal_test.go`
- The root package of the moved folder is `build_test` (its only file, `nosql_test.go`, is a test); the brief parser package keeps the name `brief`, every sub-package keeps its name.

**Interfaces:**
- Produces: import paths `gophermind/gophermind-lib/build/<pkg>`; the gate `TestNoBriefv2Anywhere`.

- [ ] **Step 1: The ignore rule**

`.gitignore` line 26 is `build/`, which would silently ignore every new file under `gophermind-lib/build/` and `docs/build/` (files moved with `git mv` stay tracked, but anything added later would be dropped without a word). Add, right after the `/desktop/build/bin/` line (the same pattern `desktop/build` already uses):

```gitignore
# gophermind-lib/build is the planner and executor (renamed from briefv2) and
# docs/build is their documentation; the generic build/ rule above must not
# swallow either.
!/gophermind-lib/build/
!/docs/build/
```

Check: `git check-ignore -v gophermind-lib/build/x.go docs/build/README.md; echo "rc=$?"` must print `rc=1` and no rule lines (not ignored). If it prints a rule, the negation is misplaced: it must come after line 26.

- [ ] **Step 2: The gate, red first**

Append to `gophermind-lib/gates/removal_test.go`:

```go
// historicalPrefixes are dated records that keep the old names. Keep this list
// equal to the exclusions of the rename command in the plan.
var historicalPrefixes = []string{
	"docs/superpowers/", "docs/reviews/", "docs/goals/", "docs/GOAL.md", "docs/GOAL-UNIFICATION.md",
	"docs/unification-overview.md", "CHANGELOG.md", ".planning/", ".gophermind/skills-packaging/references/",
	"PLAN.md", "todo.md", "todo-2.md", "todo-3.md", "todo-4.md", "BUILD_AUDIT_LOG.md", "BUILD_AUDIT_REPORT.md",
	"DEPLOY_QUEUE.md", "CONTEXT.md", "gophermind-lib/gates/",
}

// TestNoBriefv2Anywhere: no folder, import path or live text names briefv2.
func TestNoBriefv2Anywhere(t *testing.T) {
	root := repoRoot(t)
	hist := func(rel string) bool {
		for _, p := range historicalPrefixes {
			if rel == p || strings.HasPrefix(rel, p) {
				return true
			}
		}
		return false
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[d.Name()] && d.Name() != "testdata" {
				return filepath.SkipDir
			}
			if d.Name() == "briefv2" {
				t.Errorf("folder %s still exists", rel)
			}
			return nil
		}
		if hist(rel) {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil || fi.Size() > 1<<20 {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil || strings.IndexByte(string(b), 0) >= 0 {
			return nil
		}
		if strings.Contains(strings.ToLower(string(b)), "briefv2") {
			t.Errorf("%s still mentions briefv2", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
```

Run: `gt ./gophermind-lib/gates/ -run TestNoBriefv2Anywhere -count=1`
Expected: FAIL, listing the folders and the files that mention `briefv2`. Save the list: it is the worklist for step 4 and must match what the rename touches.

- [ ] **Step 3: The move**

```bash
git mv gophermind-lib/briefv2 gophermind-lib/build
git mv docs/briefv2 docs/build
```

- [ ] **Step 4: The rewrite**

List what will be rewritten, and read the list before running the rewrite:

```bash
excl=(':(exclude)docs/superpowers' ':(exclude)docs/reviews' ':(exclude)docs/goals' ':(exclude)docs/GOAL.md' ':(exclude)docs/GOAL-UNIFICATION.md' \
  ':(exclude)docs/unification-overview.md' ':(exclude)CHANGELOG.md' ':(exclude).planning' ':(exclude).gophermind/skills-packaging/references' \
  ':(exclude)PLAN.md' ':(exclude)todo.md' ':(exclude)todo-2.md' ':(exclude)todo-3.md' ':(exclude)todo-4.md' ':(exclude)BUILD_AUDIT_LOG.md' \
  ':(exclude)BUILD_AUDIT_REPORT.md' ':(exclude)DEPLOY_QUEUE.md' ':(exclude)gophermind-lib/gates')
git grep -I -l -i briefv2 -- . "${excl[@]}"
```

Expected: Go files under `gophermind-lib/build/`, `cmd/gophermind/`, `gophermind-lib/build/.../testdata/aivs/README.md`, `docs/build/README.md`, `gophermind-osx/examples/briefs/README.md`, and the Go files that name `docs/briefv2/handoff/...` in comments. A file that is a dated record belongs in the exclusion list (here and in `historicalPrefixes`, with the reason in the commit message); anything else is live and is rewritten.

Rewrite (path forms first, then the bare word; the replacement is the same for both, so one pass does it):

```bash
git grep -I -l -z -i briefv2 -- . "${excl[@]}" | xargs -0 perl -pi -e 's/briefv2/build/g; s/Briefv2/Build/g'
gofmt -w $(git diff --name-only -- '*.go')
```

`gofmt -w` is needed because import blocks are sorted by path and `build` sorts differently from `briefv2`; it changes order only. The package clause of `gophermind-lib/build/nosql_test.go` becomes `package build_test`, the test name `TestNoBriefv2PackageImportsSQL` becomes `TestNoBuildPackageImportsSQL`, and its search string `briefv2/db"` becomes `build/db"`: read that file's diff once, it is the only place the word is an identifier.

- [ ] **Step 5: Verify**

Run: `gofmt -l gophermind-lib cmd gophermind-server gophermind-osx` (prints nothing); `find . -path ./.git -prune -o -iname '*briefv2*' -print | grep -v node_modules` (prints nothing); `git status --short | grep -v '^R\|^ M\|^M\|^RM\|^A'` (prints nothing: only renames and modifications); `gt ./gophermind-lib/gates/ -count=1`; `go build ./... && go vet ./...`; `(cd gophermind-lib && go build ./... && go vet ./...)`; `(cd gophermind-osx && go build ./... && go vet ./...)`; then the three suites to files: `gt ./gophermind-lib/... ./cmd/... ./gophermind-server/... -count=1 -timeout 45m > "$out/root.txt"` with `out="$(mktemp -d)"` (the executor package takes about 11 minutes; delete `$out` afterwards with the guard) and `(cd gophermind-osx && gt ./client/ ./ui/ -count=1)`. Read the failing lines with `grep -n '^--- FAIL\|^FAIL\|^panic' "$out"/*.txt`.
Expected: all green. Also `git diff --stat -M | tail -3` shows renames plus small modifications, and `git diff -M --numstat | awk '$1 > 40'` lists only the import-heavy files you can eyeball as pure path changes.
Then: `git check-ignore -v gophermind-lib/build/nosql_test.go docs/build/README.md; echo "rc=$?"` prints `rc=1`.

- [ ] **Step 6: Commit (the only commit of the task)**

```bash
git add -A
git commit -m "refactor: rename gophermind-lib/briefv2 to gophermind-lib/build and docs/briefv2 to docs/build" -m "Mechanical: git mv of two folders, a text rewrite of briefv2 to build in live files, gofmt for import order, two .gitignore negations (the generic build/ rule would have ignored new files). Historical records keep the old name: docs/superpowers, docs/reviews, docs/goals, the goal documents, CHANGELOG.md, .planning, the skill pack's audit records and the dated root notes. The command names (gophermind brief ..., /project) do not change." -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN"
```

---

## Self-review

**Spec coverage** (`2026-10-08-retire-v1-and-upgrade-consumers-design.md`):

| Spec item | Task |
|---|---|
| 4 contract: `schema_version`, header, the eight endpoints | 1 (types), 6 (routes, header), 7 (events), 8 (page) |
| 4.1 `RunSummary` (all fields, status vocabulary) | 1, 3 |
| 4.2 `NodeSummary` (all fields) | 1, 3 |
| 4.3 events: ids, `Last-Event-ID` / `since` replay, six types, keepalive, cap of 16 with 503 | 5 (derivation, replay), 7 (ids, keepalive, cap) |
| 5 `Runs`, `Open`, `State`, `Node`, `Decisions`, `Report`, `ArtifactNames`, `Artifact`, `Follow` (250 ms) | 3, 4, 5 |
| 5 path safety (run, node, file, attempt, symlinked run folder) | 1, 4, 6 |
| 6 `PipelineDeps{Root, MaxStreams}`, hub, `StartPipelineWatcher`, `pipeline_watch.go` deleted | 6, 7 |
| 6 `pipeline.go` rewritten, dashboard page rewritten for nodes (tree by component, report, decisions, detail) | 6, 8 |
| 6 wiring in `gophermind-server/server.go` and `cmd/gophermind/main.go`; `events.go` comment | 7, 9 |
| 6 deployment (`git pull`, `launchctl kickstart`, verify with a token) | not done here by rule; Task 12 proves the endpoints and the Final report of the goal lists John's steps |
| 7 client calls, `sse.go` ids and resume, typed schema error | 9 |
| 7 `ui/pipeline.go`, `pipelinepump.go`, panel formatting, `main.go:398` | 10, 11 |
| 7 no `phaseflow` import left in the module; `e2e_pipeline_test.go` over a real run folder | 11 |
| 7 signed, notarised release with the server | John's step; Task 12 builds unsigned only |
| 8 removed list: `plantree`, `phaseflow`, `orchestrate`, `phase.go`, the TUI files, skill pack, docs, `App.tsx`, `CHANGELOG`, `CREDITS` | 13, 14 |
| 8 the notice, exit 2, one release | 13 (TUI), 14 (CLI) |
| 9 rename in one mechanical commit, last; `docs/build`; command names unchanged | 15 |
| 10 order G1 to G7 | tasks 1 to 15 in order |
| 11 tests: `runview` states, `Follow` replay, corrupt line, path safety, symlink; server with and without token, header, `since`, cap, artifact names, partial report; app goldens, mismatch message, panel formatting, reconnect | 1 to 5, 6, 7, 9, 10 |
| 11 removal gates: no importer, `go build`/`go vet` in three modules, no `briefv2` path, stub exits 2 | 13, 14, 15 (and each task's verify step) |
| 12 risks: schema check, loud removals behind gates | 9 to 11, 13 to 15 |
| 13 open items 1 to 4: `build`, CREDITS line, 16 and 250 ms, no pruning | decided in `docs/GOAL-UNIFICATION.md`; applied in 7, 14, 15 |

**Placeholder scan:** every code step shows code. Three places hand the engineer a rule instead of text, each with the exact command that finds the cases: (a) Task 2 step 3, a node field name that the current schema rejects (the command prints the schema's property list); (b) Task 4 step 4, goldens written by `-update` and then reviewed by eye for paths and times; (c) Tasks 13 and 14 (the TUI edits and the doc edits are anchored on quoted text because sub-project C shifts the lines; the compiler and a final grep gate close the loop). No step says "add error handling" or "write tests for the above".

**Type consistency:** `runview.StateBody`, `NodeBody`, `DecisionsBody`, `RunsBody`, `ReportBody`, `Attempt`, `NodeResult`, `Event` and the event data structs are defined once in Task 1 and used with the same fields in Tasks 3 to 5, the server (6, 7), the client (9) and the app (10, 11). `View.Warnings`, `EventSeq`, `Follow(ctx, since int64)` and the `WithPoll`/`WithClock` options are defined in Tasks 3 and 5 and used in Task 7. `PipelineDeps{Root, Hub, MaxStreams}` in Task 6 becomes `{Root, MaxStreams}` in Task 7; `newPipelineAPI` and `pipelineAPI.slots` are defined in Task 6 and used in 7. `OpenEventsFunc(ctx, run, since)` is defined in Task 10 and used in Task 11. `tui.PhaseRemovedNotice` is defined in Task 13 and used in Task 14. `removedImports` and `removedDirs` grow in Tasks 13 and 14 and `historicalPrefixes` is added in Task 15.

**Order and buildability:** after Task 6 the server still builds (hub kept); after Task 7 the desktop app does not build until Task 9 and 10 land (it imports `serve.TaskStatusEvent`, kept until Task 9, and `phaseflow`, replaced in Tasks 10 and 11), which is why Task 9 forbids `go build ./...` in `gophermind-osx` and Task 11 is the first green build of that module. Tasks 13 to 15 each leave the tree green. The rename is last.

**Known risks the engineer should watch:**
- Task 4's goldens are the contract between two modules; regenerate them only with `-update` and read the diff.
- Task 11's panel is cgo and is exercised only by the `runOnUIThread` harness and by looking at it once; everything it computes is tested in `ui`.
- Task 13 assumes C's shape of `tui` (`project_v2.go`, `/plan-v1` names). If C changed which messages or fields `/project` uses, the compiler will say which deleted symbol a surviving file still needs; the rule in step 4 decides.
- Task 15's text rewrite is global: read the list printed before running it, and the `--numstat` check after.
