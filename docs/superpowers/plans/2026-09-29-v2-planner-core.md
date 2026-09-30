# v2 Planner Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the planning half of the v2 brief-to-build engine: a shared SQLite store (blackboard and a model-call ledger that records `task_type` and `node_class` on every row), an OpenAI-compatible provider layer, a router that enforces fallback chains and a need-to-know privacy rule, human gates, a requirements parser and coverage checker, the planner stages (Clarify, Contract as an outline pass plus one pass per component, Decompose, Coverage, Approve, Test-writer), and `gophermind brief plan|resume|status|calls|coverage`.

**Architecture:** New packages under `gophermind-lib/briefv2/`, dependencies pointing downward: `db` <- `blackboard`, `ledger`; `ledger` <- `events`; `provider`; `settings` (uses `provider`, builds providers); `router` (uses `settings`, `provider`, `ledger`, `events`); `human`; `planner` (uses all of them plus the existing `brief`, `tree`, `contract`, `schema`, `rundir`, `vault`). The planner is a list of stages run in order, each skipped when its output already exists in the run folder, which is all that `resume` is. Requirements are parsed from the brief by code and a plan cannot reach approval while one of them has nothing behind it. The commands live in `cmd/gophermind/brief_plan.go`. The whole planner runs offline against canned replies served per stage by a fixture provider.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` and `gopkg.in/yaml.v3` (both already dependencies), `net/http/httptest` for provider tests, `go/parser` for signature and test-file checks, the standard library otherwise.

**Spec:** `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (read it first; it holds the decisions and the reasons). Handoff reference: `docs/briefv2/handoff/` (interfaces, prompt templates, BUILD_PLAN.md items 5 to 8).

## Global Constraints

- Pure Go, no cgo, no new dependency. `modernc.org/sqlite` and `gopkg.in/yaml.v3` are already in `gophermind-lib/go.mod`.
- New code lives under `gophermind-lib/briefv2/<package>/` (module `gophermind/gophermind-lib`) and `cmd/gophermind/brief_plan.go`. Do not modify `plantree`, `plan`, `orchestrate`, `phaseflow`, `freellm`, or the existing `briefv2` packages, except for exactly these three named changes: `schema.Raw` in `gophermind-lib/briefv2/schema/schema.go` (Task 8, Steps 1 and 2); skipping `_state/`, `requirements.json` and `coverage.json` in `gophermind-lib/briefv2/tree/store.go` (Task 8, Steps 3 and 4); and the subcommand routing and usage text in `cmd/gophermind/brief.go` (Task 12, Step 3). `docs/briefv2/README.md` is updated in Task 12, Step 5.
- No code limits the number of components, functions, or tests. A large brief is handled by more calls (Contract continuation, Decompose batches), never by a coarser plan. The per-call sizes (`maxTokens*`, the Decompose batch of 8) bound one reply, not the plan.
- Prompt text and reply text are never stored in the ledger, the blackboard, an event, or a log line. Only byte sizes and SHA-256 hashes. No secret value is ever written anywhere or put in an error message.
- Public providers never receive `brief` or `component` scope calls unless the run was started with `--allow-public`; with `privacy.mode: private_only` they never receive any call.
- Tests never use the real network. `httptest` servers on loopback are allowed. Nothing in a test reads or writes the user's real `~/.gophermind`; tests use `t.TempDir()` and `t.Setenv("GOPHERMIND_CONFIG_DIR", ...)`.
- `go build ./...` at the repo root works in the main checkout. In a git worktree the ignored `desktop/frontend/dist` folder is missing, so use `go build ./cmd/... ./gophermind-lib/...` there.
- Code is gofmt-clean and `go vet` clean; tests pass with `-race`. New documents contain no em dashes and no emojis.
- Commit messages end with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`. Stage explicit paths only. Never push. Never use `--gw-force`, `git stash`, or `git checkout` (the git wrapper blocks them).

## Decisions specific to this plan

| # | Choice | Why |
|---|---|---|
| L1 | Blackboard and provider interfaces are copied byte for byte from `docs/briefv2/handoff/interfaces/` into their packages | The handoff says implement them exactly |
| L2 | `Router.Call` returns a `Result` with the ledger row id; `Ledger.Record` takes a pointer; `Ledger.Amend` exists | A reply that arrives fine but fails the stage's parser must be corrected to `malformed` in the ledger |
| L3 | Waves are computed a second time inside the planner (a small helper) so the Approve summary can show them before function nodes are valid | Function nodes are schema-invalid until Test-writer adds tests; the finished tree is checked with `tree.CheckWaves`, which must agree |
| L4 | The end-to-end tests use a tiny invented brief (`greeter`, three components, three functions) with hand-written canned replies, not the handoff's Acme example. Variant folders (`greeter-gap`, `greeter-stuck`, `greeter-badtest`) override single replies | The Acme `contracts.json` is missing types its functions rely on (decision D3 of the first plan) |
| L5 | `resume` finds a run through `<config dir>/runs/<run-id>.json`, written by `plan`. The run id is the brief id | A run folder lives inside the target repo; the id alone does not say where |
| L6 | One manual smoke test against the real model (Task 12, Step 7) is not automated | Model quality on the mini is the main open risk and a live model is slow and non-deterministic |
| L7 | Coverage is a stage of its own: requirements are parsed by code at Load, the model proposes a requirement-to-node mapping, code checks it, gaps go to `coverage_fill` for at most `max_coverage_rounds` (2), and Approve refuses to run with an uncovered requirement | The v1 planner approved a 99-step plan for a 730-line brief with 96 empty test commands and no node for most of the brief's acceptance round trips (spec section 9, Coverage) |
| L8 | A generated test file may import the standard library and the contract module's own packages, nothing else | A same-package test of a handler needs the module's other packages (a store type, a client); a rule of standard library only would make such tests impossible |
| L9 | Component nodes carry no `depends_on` | Deriving it from their functions makes cycles that are legal at function level (the handoff's own example has `registration` and `health` calling into each other), and a component's readiness looks at its children only (deviation D2) |
| L10 | `logs` and `outline` are refused as component ids | A component id becomes a folder in the run folder and a stage name (`contract:<component>`); `logs/` is the run's log folder and `contract:outline` is the outline pass |
| L11 | For every function node the harness derives the test function name (from the node id), the test file path (beside the function's file, named after the node), and the `command` of every test; what a model writes for these is ignored | A model-written command would be run by the executor later. The only model-written commands in a plan are the root acceptance tests, and those are printed in full in the approval summary |
| L12 | `gophermind-lib/briefv2/planner/testdata/aivs/` (`nodes.json`, `mapping.json`, `README.md`) is committed with this plan. Do not regenerate or edit it | It was built from the v1 planner's output in `.planning/plan/`, which is not in the repository |
| L13 | `node_class` is asked for beside each Decompose draft, removed from the node document, and kept in `_state/classes.json` | The node schema is the handoff's and forbids unknown fields |
| L14 | Contract replies are capped at 8000 tokens per pass and Decompose asks for at most 8 functions per call | A pass that needs more says `"more": true` and is called again; a bigger component makes more batches. Neither number limits the plan |
| L15 | A signature that does not parse as Go is refused in the Contract pass that wrote it, not in Decompose | Decompose overwrites a draft's signature with the contract's, so a Decompose retry could never repair a bad one |
| L16 | `router.CallInfo.TaskType`, when empty, is recorded as the stage up to its first colon | No ledger row can lack a task type, whoever the caller is |
| L17 | `planner.Options.StopAfter` ends a run after a named stage; `planner.Deps.LedgerErrors` reports the router's count | Each task's tests run before the later stages exist, and the resume test stops between stages; the planner, not the command, writes `_state/status.json` |

## Review Focus

1. A public provider receives zero requests for `brief` and `component` scope calls under `need_to_know`, and zero of any kind under `private_only`. (Task 5, `TestPublicProvidersAreOnlyEverAskedWhatThePrivacyRuleAllows`; Task 11, `TestAPublicProviderOnlyEverSeesNodeScopeCalls`)
2. Every model attempt writes exactly one ledger row, including failures and cancellations, and the database file contains no prompt or reply text. (Task 2, `TestNoPromptOrReplyTextIsEverStored`; Task 5, `TestFailsOverInChainOrderAndWritesOneRowPerAttempt`, `TestCancellationLeavesExactlyOneCancelledRow`, `TestNoPromptOrReplyTextReachesTheDatabase`)
3. Under 20 concurrent claimers exactly one wins; illegal status transitions are rejected; a stale claim is released. (Task 1, `TestClaimRaceHasExactlyOneWinner`, `TestSetStatusTransitions`, `TestReleaseStaleReleasesOnlyOldClaims`)
4. Nothing is written into the target repo before an `approval.json` whose hash matches the plan as it stands; stopping a run between stages and resuming repeats only the unfinished stage and makes no model call for a finished one. (Task 11, `TestNothingTouchesTheRepoWithoutApproval`, `TestAChangedPlanInvalidatesTheApproval`, `TestResumeRepeatsOnlyWhatIsUnfinished`)
5. No plan reaches Approve while a feature, constraint, or acceptance bullet of the brief is uncovered; the golden failing plan (the v1 planner's AI Venture Studio output) is reported as uncovered for the known gaps. (Task 7, `TestCheckCoverage`, `TestGoldenFailingPlan`; Task 10, `TestCoverageStopsWhenAGapIsLeft`, `TestCoverageFillClosesGapsAndDecomposesNewFunctions`)
6. A model reply that parses but has the wrong shape never reaches the tree: a leaf that does not describe every input, output and error condition is refused, a test file can never be written outside the repo root, and a test file may import only the standard library or the module's own packages. (Task 9, `TestLeafChecks`; Task 11, `TestCheckTestSource`, `TestSafeTestPath`, `TestABadTestFileIsRefusedAndRetried`, `TestTheTestWriterOnlyWritesItsOwnFilesInsideTheRepo`, `TestTestwriterRepliesNeedATestPerErrorCondition`)
7. Plan size has no ceiling: a component pass that says `"more": true` is called again and both replies are merged, and a component with 9 functions makes two Decompose calls. (Task 8, `TestContractContinuesAComponentWhileMoreIsTrue`; Task 9, `TestDecomposeBatchesALargeComponent`)
8. Every ledger row carries `task_type`, leaf calls carry `node_class`, and the summary groups by both. (Task 2, `TestSummaryGroupsByTaskTypeAndNodeClass`; Task 5, `TestLedgerRowsCarryTaskTypeAndNodeClass`; Task 11, `TestLedgerRowsCarryTheTaskTypeAndTheNodeClass`)

## File Structure

```text
gophermind-lib/briefv2/
  db/          db.go, db_test.go                       open the shared SQLite file, migrations, timestamp helpers (Task 1)
  blackboard/  blackboard.go (handoff, exact), sqlite.go, sqlite_test.go   (Task 1)
  ledger/      ledger.go, ledger_test.go               calls table: Record, Amend, List, Summary by task type and node class (Task 2)
  events/      events.go, events_test.go               Event, Sink, Nop, Collector (Task 3)
  provider/    provider.go (handoff, exact), errors.go, openai.go, fake.go, openai_test.go   (Task 3)
  settings/    settings.go, providers.go, settings_test.go   gophermind.yaml: types, defaults, load, validate, build providers (Task 4)
  router/      router.go, attempt.go, parsed.go, privacy.go, errors.go, router_test.go   (Task 5)
  human/       human.go, terminal.go, file.go, programmatic.go, human_test.go   (Task 6)
  planner/     requirements.go, coverage_check.go                       pure code (Task 7; coverage_check.go gains StrayCommandWarnings in Task 10)
               requirements_test.go, coverage_check_test.go
               testdata/aivs/nodes.json, mapping.json, README.md        committed with this plan (L12)
               state.go, answers.go, parse.go, prompts.go, fixture.go,  foundation, Load, Clarify, Contract (Task 8)
               planner.go, status.go, load.go, clarify.go, contract_stage.go
               prompts/clarify.md, contract_outline.md, contract_component.md
               harness_test.go, parse_test.go, fixture_test.go, load_test.go,
               clarify_test.go, contract_stage_test.go, contract_internal_test.go
               decompose.go, prompts/decompose.md,                      (Task 9)
               decompose_test.go, decompose_internal_test.go
               coverage.go, prompts/coverage.md, coverage_fill.md,      (Task 10)
               coverage_test.go
               approve.go, testwriter.go, prompts/testwriter.md,        (Task 11)
               testwriter_internal_test.go, e2e_test.go
               testdata/greeter/, greeter-gap/, greeter-stuck/,         canned replies, created by Tasks 8 to 11
               greeter-badtest/
  schema/      schema.go, schema_test.go               add Raw(kind) (Task 8)
  tree/        store.go, store_test.go                 skip _state/, requirements.json, coverage.json (Task 8)
cmd/gophermind/brief_plan.go, brief_plan_test.go       plan, resume, status, coverage, calls (Task 12)
cmd/gophermind/brief.go                                routing and usage (Task 12)
docs/briefv2/README.md                                 commands, run folder, settings, ledger (Task 12)
```

---

### Task 1: Shared database and the blackboard

**Files:**
- Create: `gophermind-lib/briefv2/db/db.go`, `gophermind-lib/briefv2/db/db_test.go`
- Create: `gophermind-lib/briefv2/blackboard/blackboard.go` (copy), `sqlite.go`, `sqlite_test.go`

**Interfaces:**
- Produces: `db.Open(path string) (*sql.DB, error)`; `db.DefaultPath() (string, error)`; `db.TS(time.Time) string`; `db.ParseTS(string) (time.Time, error)`; `db.TimeFormat`.
- Produces: `blackboard.NewSQLite(*sql.DB) *SQLite` implementing the handoff `Blackboard` interface exactly. `Close()` does nothing (the caller owns the `*sql.DB`, which the ledger shares).

- [ ] **Step 1: Copy the handoff interface and write the failing db test**

```bash
cd gophermind-lib
mkdir -p briefv2/db briefv2/blackboard
cp ../docs/briefv2/handoff/interfaces/blackboard.go briefv2/blackboard/blackboard.go
gofmt -l briefv2/blackboard   # must print nothing
```

`briefv2/db/db_test.go`:

```go
package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
)

func TestOpenCreatesSchemaAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "bb.db")
	for i := 0; i < 2; i++ {
		d, err := db.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"rows", "events", "calls"} {
			var n int
			if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
				t.Fatalf("open %d: table %s missing (n=%d err=%v)", i, table, n, err)
			}
		}
		var v int
		if err := d.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 1 {
			t.Fatalf("user_version = %d, %v", v, err)
		}
		d.Close()
	}
}

func TestPragmas(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var mode string
	if err := d.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v", mode, err)
	}
	var timeout int
	if err := d.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 5000 {
		t.Errorf("busy_timeout = %d, %v", timeout, err)
	}
}

func TestTimestampsSortAsText(t *testing.T) {
	a := time.Date(2026, 9, 29, 12, 0, 5, 500_000_000, time.UTC)
	b := time.Date(2026, 9, 29, 12, 0, 5, 123_000_000, time.UTC) // earlier, but more digits than a
	c := time.Date(2026, 9, 29, 12, 0, 6, 0, time.UTC)
	if !(db.TS(b) < db.TS(a) && db.TS(a) < db.TS(c)) {
		t.Errorf("timestamps do not sort as text: %s %s %s", db.TS(b), db.TS(a), db.TS(c))
	}
	back, err := db.ParseTS(db.TS(a))
	if err != nil || !back.Equal(a) {
		t.Errorf("round trip: %v %v", back, err)
	}
	if z, err := db.ParseTS(""); err != nil || !z.IsZero() {
		t.Errorf("empty string should parse to the zero time, got %v %v", z, err)
	}
}

func TestDefaultPathUsesConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	p, err := db.DefaultPath()
	if err != nil || p != filepath.Join(dir, "blackboard.db") {
		t.Errorf("DefaultPath = %q, %v", p, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/db/ 2>&1 | head`
Expected: FAIL, `undefined: db.Open` (the package has no non-test files).

- [ ] **Step 3: Implement `db.go`**

```go
// Package db opens the SQLite file shared by the blackboard (rows and their
// change events) and the model-call ledger (calls). One file per harness,
// write-ahead logging, safe for several processes.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"gophermind/gophermind-lib/config"
)

// TimeFormat is fixed width and always UTC, so timestamps compare correctly as
// text (RFC3339Nano trims trailing zeros and would not).
const TimeFormat = "2006-01-02T15:04:05.000000000Z"

// TS formats t for storage.
func TS(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseTS is the inverse of TS; the empty string is the zero time.
func ParseTS(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(TimeFormat, s)
}

// DefaultPath is ~/.gophermind/blackboard.db (or under GOPHERMIND_CONFIG_DIR).
func DefaultPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "blackboard.db"), nil
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	d.SetMaxOpenConns(8)
	if err := migrate(d); err != nil {
		d.Close()
		return nil, fmt.Errorf("db: %w", err)
	}
	return d, nil
}

var schemaV1 = []string{
	`CREATE TABLE IF NOT EXISTS rows (
		run_id TEXT NOT NULL, node_id TEXT NOT NULL,
		status TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0, wave INTEGER NOT NULL DEFAULT 0,
		claim_worker TEXT NOT NULL DEFAULT '', claim_at TEXT NOT NULL DEFAULT '', heartbeat_at TEXT NOT NULL DEFAULT '',
		attempts TEXT NOT NULL DEFAULT '[]', result TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL,
		PRIMARY KEY (run_id, node_id))`,
	`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, node_id TEXT NOT NULL,
		kind TEXT NOT NULL, at TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS events_run ON events (run_id, id)`,
	`CREATE TABLE IF NOT EXISTS calls (
		id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, at TEXT NOT NULL,
		stage TEXT NOT NULL, task_type TEXT NOT NULL DEFAULT '', node_class TEXT NOT NULL DEFAULT '',
		node_id TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0,
		scope TEXT NOT NULL, tier TEXT NOT NULL, chain_pos INTEGER NOT NULL DEFAULT 0,
		provider TEXT NOT NULL, model_requested TEXT NOT NULL, model_served TEXT NOT NULL DEFAULT '',
		prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
		prompt_bytes INTEGER NOT NULL DEFAULT 0, prompt_sha256 TEXT NOT NULL DEFAULT '',
		response_bytes INTEGER NOT NULL DEFAULT 0, response_sha256 TEXT NOT NULL DEFAULT '',
		duration_ms INTEGER NOT NULL DEFAULT 0,
		outcome TEXT NOT NULL, error_kind TEXT NOT NULL DEFAULT '', retry_after_s INTEGER NOT NULL DEFAULT 0)`,
	`CREATE INDEX IF NOT EXISTS calls_run ON calls (run_id, at)`,
	`CREATE INDEX IF NOT EXISTS calls_node ON calls (run_id, node_id)`,
}

func migrate(d *sql.DB) error {
	var v int
	if err := d.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v >= 1 {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range schemaV1 {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = d.Exec(`PRAGMA user_version = 1`)
	return err
}
```

- [ ] **Step 4: Run the db tests**

Run: `cd gophermind-lib && go test ./briefv2/db/ -race -v 2>&1 | tail -12`
Expected: PASS (4 tests).

- [ ] **Step 5: Write the failing blackboard tests** (`briefv2/blackboard/sqlite_test.go`, white-box so tests can set the clock)

```go
package blackboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
)

func newBB(t *testing.T) (*SQLite, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewSQLite(d), d
}

// initReady creates the nodes and moves them to ready.
func initReady(t *testing.T, b *SQLite, run string, ids ...string) {
	t.Helper()
	ctx := context.Background()
	waves := map[string]int{}
	for _, id := range ids {
		waves[id] = 0
	}
	if err := b.InitRun(ctx, run, ids, waves); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := b.SetStatus(ctx, run, id, StatusPending, StatusReady); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitRunIsIdempotent(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	if err := b.InitRun(ctx, "r", []string{"a", "b"}, map[string]int{"a": 0, "b": 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetStatus(ctx, "r", "a", StatusPending, StatusReady); err != nil {
		t.Fatal(err)
	}
	if err := b.InitRun(ctx, "r", []string{"a", "b", "c"}, map[string]int{"a": 0, "b": 1, "c": 2}); err != nil {
		t.Fatal(err)
	}
	rows, err := b.List(ctx, "r", Filter{})
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	a, _ := b.Get(ctx, "r", "a")
	if a.Status != StatusReady {
		t.Errorf("a was reset to %s; InitRun must leave existing rows alone", a.Status)
	}
}

func TestClaimRaceHasExactlyOneWinner(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "leaf")
	var wins atomic.Int32
	winner := make(chan string, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := fmt.Sprintf("worker-%d", i)
			ok, err := b.Claim(ctx, "r", "leaf", w)
			if err != nil {
				t.Errorf("Claim returned an error for a lost race: %v", err)
				return
			}
			if ok {
				wins.Add(1)
				winner <- w
			}
		}(i)
	}
	wg.Wait()
	close(winner)
	if wins.Load() != 1 {
		t.Fatalf("%d workers won the claim, want exactly 1", wins.Load())
	}
	row, _ := b.Get(ctx, "r", "leaf")
	if row.Status != StatusClaimed || row.Claim == nil || row.Claim.Worker != <-winner {
		t.Errorf("row after claim = %+v", row)
	}
}

func TestClaimOnlyWorksOnReadyNodes(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	if err := b.InitRun(ctx, "r", []string{"a"}, map[string]int{"a": 0}); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.Claim(ctx, "r", "a", "w"); ok || err != nil {
		t.Errorf("claiming a pending node = %v, %v; want false, nil", ok, err)
	}
	if ok, err := b.Claim(ctx, "r", "missing", "w"); ok || err != nil {
		t.Errorf("claiming an unknown node = %v, %v; want false, nil", ok, err)
	}
}

func TestSetStatusTransitions(t *testing.T) {
	// path lists the statuses to walk through starting from pending.
	cases := []struct {
		name string
		path []Status
		ok   bool
	}{
		{"pending to ready", []Status{StatusReady}, true},
		{"ready to claimed to in_progress to verified", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusVerified}, true},
		{"in_progress released back to ready", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusReady}, true},
		{"in_progress to needs_revision to escalated", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusNeedsRevision, StatusEscalated}, true},
		{"escalated to failed", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusNeedsRevision, StatusEscalated, StatusFailed}, true},
		{"verified back to needs_revision", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusVerified, StatusNeedsRevision}, true},
		{"pending straight to verified", []Status{StatusVerified}, false},
		{"ready straight to verified", []Status{StatusReady, StatusVerified}, false},
		{"failed is terminal", []Status{StatusReady, StatusClaimed, StatusInProgress, StatusFailed, StatusReady}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, _ := newBB(t)
			ctx := context.Background()
			if err := b.InitRun(ctx, "r", []string{"n"}, map[string]int{"n": 0}); err != nil {
				t.Fatal(err)
			}
			from := StatusPending
			for i, to := range c.path {
				err := b.SetStatus(ctx, "r", "n", from, to)
				last := i == len(c.path)-1
				if last && !c.ok {
					if !errors.Is(err, ErrBadTransition) {
						t.Fatalf("%s -> %s: err = %v, want ErrBadTransition", from, to, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("%s -> %s: %v", from, to, err)
				}
				from = to
			}
			row, _ := b.Get(ctx, "r", "n")
			if row.Status != from {
				t.Errorf("final status = %s, want %s", row.Status, from)
			}
		})
	}
}

func TestSetStatusIsCompareAndSet(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	// The row is ready; claiming to move from pending must fail even though pending->ready is a legal edge.
	if err := b.SetStatus(ctx, "r", "n", StatusPending, StatusReady); !errors.Is(err, ErrBadTransition) {
		t.Errorf("stale from-status: err = %v, want ErrBadTransition", err)
	}
	if err := b.SetStatus(ctx, "r", "nope", StatusPending, StatusReady); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown node: err = %v, want ErrNotFound", err)
	}
}

func TestHeartbeatAndReleaseRequireTheOwner(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	if ok, _ := b.Claim(ctx, "r", "n", "alice"); !ok {
		t.Fatal("alice should win an uncontested claim")
	}
	if err := b.Heartbeat(ctx, "r", "n", "bob"); !errors.Is(err, ErrNotOwner) {
		t.Errorf("bob heartbeat: %v, want ErrNotOwner", err)
	}
	if err := b.Release(ctx, "r", "n", "bob"); !errors.Is(err, ErrNotOwner) {
		t.Errorf("bob release: %v, want ErrNotOwner", err)
	}
	if err := b.Heartbeat(ctx, "r", "n", "alice"); err != nil {
		t.Errorf("alice heartbeat: %v", err)
	}
	if err := b.Release(ctx, "r", "n", "alice"); err != nil {
		t.Fatal(err)
	}
	row, _ := b.Get(ctx, "r", "n")
	if row.Status != StatusReady || row.Claim != nil {
		t.Errorf("after release: %+v", row)
	}
	if err := b.Heartbeat(ctx, "r", "missing", "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("heartbeat on unknown node: %v, want ErrNotFound", err)
	}
}

func TestReleaseStaleReleasesOnlyOldClaims(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "old", "fresh", "idle")
	base := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return base }
	if ok, _ := b.Claim(ctx, "r", "old", "w1"); !ok {
		t.Fatal("claim old")
	}
	b.now = func() time.Time { return base.Add(19 * time.Minute) }
	if ok, _ := b.Claim(ctx, "r", "fresh", "w2"); !ok {
		t.Fatal("claim fresh")
	}
	b.now = func() time.Time { return base.Add(20 * time.Minute) }
	released, err := b.ReleaseStale(ctx, "r", 15*time.Minute)
	if err != nil || len(released) != 1 || released[0] != "old" {
		t.Fatalf("released = %v, %v; want [old]", released, err)
	}
	if row, _ := b.Get(ctx, "r", "old"); row.Status != StatusReady || row.Claim != nil {
		t.Errorf("old after release: %+v", row)
	}
	if row, _ := b.Get(ctx, "r", "fresh"); row.Status != StatusClaimed {
		t.Errorf("fresh should stay claimed, is %s", row.Status)
	}
	if row, _ := b.Get(ctx, "r", "idle"); row.Status != StatusReady {
		t.Errorf("idle should stay ready, is %s", row.Status)
	}
}

func TestAttemptsAndResultRoundTrip(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	started := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	first := Attempt{Model: "m1", Provider: "p", Revision: 0, Order: 1, StartedAt: started, DurationMS: 1200,
		Verdict: VerdictFail, TestsPassed: 2, TestsTotal: 4, FailedTests: []string{"a", "b"}, FailureReason: "off_by_one: boundary"}
	second := Attempt{Model: "m2", Provider: "q", Order: 2, StartedAt: started.Add(time.Minute), Verdict: VerdictPass, TestsPassed: 4, TestsTotal: 4}
	if err := b.AppendAttempt(ctx, "r", "n", first); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendAttempt(ctx, "r", "n", second); err != nil {
		t.Fatal(err)
	}
	if err := b.SetResult(ctx, "r", "n", Result{FilesChanged: []string{"x.go"}, Commit: "abc1234"}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetRevision(ctx, "r", "n", 2); err != nil {
		t.Fatal(err)
	}
	row, err := b.Get(ctx, "r", "n")
	if err != nil {
		t.Fatal(err)
	}
	if len(row.Attempts) != 2 || row.Attempts[0].Model != "m1" || row.Attempts[1].Model != "m2" {
		t.Fatalf("attempts = %+v", row.Attempts)
	}
	if got := row.Attempts[0]; got.FailureReason != "off_by_one: boundary" || len(got.FailedTests) != 2 || !got.StartedAt.Equal(started) || got.Verdict != VerdictFail {
		t.Errorf("first attempt lost data: %+v", got)
	}
	if row.Result == nil || row.Result.Commit != "abc1234" || row.Revision != 2 {
		t.Errorf("result/revision = %+v / %d", row.Result, row.Revision)
	}
}

func TestConcurrentAppendAttemptLosesNothing(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := b.AppendAttempt(ctx, "r", "n", Attempt{Model: fmt.Sprintf("m%d", i), Provider: "p", Verdict: VerdictError}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	row, _ := b.Get(ctx, "r", "n")
	if len(row.Attempts) != 12 {
		t.Errorf("stored %d attempts, want 12", len(row.Attempts))
	}
}

func TestListFilters(t *testing.T) {
	b, _ := newBB(t)
	ctx := context.Background()
	if err := b.InitRun(ctx, "r", []string{"a", "b", "c"}, map[string]int{"a": 0, "b": 1, "c": 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetStatus(ctx, "r", "b", StatusPending, StatusReady); err != nil {
		t.Fatal(err)
	}
	wave1 := 1
	rows, _ := b.List(ctx, "r", Filter{Wave: &wave1})
	if len(rows) != 2 {
		t.Errorf("wave 1 rows = %d, want 2", len(rows))
	}
	rows, _ = b.List(ctx, "r", Filter{Statuses: []Status{StatusReady}})
	if len(rows) != 1 || rows[0].NodeID != "b" {
		t.Errorf("ready rows = %+v", rows)
	}
	rows, _ = b.List(ctx, "r", Filter{Statuses: []Status{StatusReady, StatusPending}, Wave: &wave1})
	if len(rows) != 2 {
		t.Errorf("combined filter rows = %d, want 2", len(rows))
	}
	if rows, _ := b.List(ctx, "other", Filter{}); len(rows) != 0 {
		t.Errorf("another run's rows leaked: %d", len(rows))
	}
}

func TestWatchDeliversChanges(t *testing.T) {
	b, _ := newBB(t)
	b.poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.InitRun(ctx, "r", []string{"n"}, map[string]int{"n": 0}); err != nil {
		t.Fatal(err)
	}
	ch, err := b.Watch(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetStatus(ctx, "r", "n", StatusPending, StatusReady); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Kind != EventStatus || ev.Row.NodeID != "n" || ev.Row.Status != StatusReady {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3 seconds")
	}
	cancel()
	select {
	case _, open := <-ch:
		for open {
			_, open = <-ch
		}
	case <-time.After(3 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/blackboard/ 2>&1 | head`
Expected: FAIL, `undefined: NewSQLite`.

- [ ] **Step 7: Implement `sqlite.go`**

```go
package blackboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
)

// SQLite is the blackboard over the shared database. Close does nothing: the
// *sql.DB belongs to the caller because the ledger uses it too.
type SQLite struct {
	db   *sql.DB
	now  func() time.Time
	poll time.Duration
}

var _ Blackboard = (*SQLite)(nil)

func NewSQLite(d *sql.DB) *SQLite {
	return &SQLite{db: d, now: func() time.Time { return time.Now().UTC() }, poll: 250 * time.Millisecond}
}

var allowed = map[Status][]Status{
	StatusPending:       {StatusReady, StatusNeedsRevision},
	StatusReady:         {StatusClaimed, StatusNeedsRevision},
	StatusClaimed:       {StatusInProgress, StatusReady},
	StatusInProgress:    {StatusVerified, StatusFailed, StatusNeedsRevision, StatusReady},
	StatusNeedsRevision: {StatusReady, StatusEscalated},
	StatusEscalated:     {StatusReady, StatusFailed},
	StatusVerified:      {StatusNeedsRevision},
}

func transitionAllowed(from, to Status) bool {
	for _, s := range allowed[from] {
		if s == to {
			return true
		}
	}
	return false
}

const rowCols = `run_id, node_id, status, revision, claim_worker, claim_at, heartbeat_at, attempts, result, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanRow(sc scanner) (Row, error) {
	var (
		r                           Row
		status, worker, claimAt, hb string
		attempts, result, updated   string
	)
	if err := sc.Scan(&r.RunID, &r.NodeID, &status, &r.Revision, &worker, &claimAt, &hb, &attempts, &result, &updated); err != nil {
		return Row{}, err
	}
	r.Status = Status(status)
	r.HeartbeatAt, _ = db.ParseTS(hb)
	r.UpdatedAt, _ = db.ParseTS(updated)
	if worker != "" {
		at, _ := db.ParseTS(claimAt)
		r.Claim = &Claim{Worker: worker, ClaimedAt: at}
	}
	if attempts != "" && attempts != "[]" {
		if err := json.Unmarshal([]byte(attempts), &r.Attempts); err != nil {
			return Row{}, err
		}
	}
	if result != "" {
		r.Result = &Result{}
		if err := json.Unmarshal([]byte(result), r.Result); err != nil {
			return Row{}, err
		}
	}
	return r, nil
}

func (s *SQLite) ts() string { return db.TS(s.now()) }

func (s *SQLite) emit(ctx context.Context, kind EventKind, runID, nodeID string) {
	// A lost change notification must never fail the write it describes.
	_, _ = s.db.ExecContext(ctx, `INSERT INTO events (run_id, node_id, kind, at) VALUES (?, ?, ?, ?)`, runID, nodeID, string(kind), s.ts())
}

func (s *SQLite) exists(ctx context.Context, runID, nodeID string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rows WHERE run_id = ? AND node_id = ?`, runID, nodeID).Scan(&n)
	return n > 0
}

func (s *SQLite) InitRun(ctx context.Context, runID string, nodeIDs []string, waves map[string]int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range nodeIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO rows (run_id, node_id, status, wave, updated_at) VALUES (?, ?, 'pending', ?, ?)`,
			runID, id, waves[id], s.ts()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Get(ctx context.Context, runID, nodeID string) (Row, error) {
	r, err := scanRow(s.db.QueryRowContext(ctx, `SELECT `+rowCols+` FROM rows WHERE run_id = ? AND node_id = ?`, runID, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Row{}, ErrNotFound
	}
	return r, err
}

func (s *SQLite) List(ctx context.Context, runID string, f Filter) ([]Row, error) {
	q := `SELECT ` + rowCols + ` FROM rows WHERE run_id = ?`
	args := []any{runID}
	if len(f.Statuses) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",") + `)`
		for _, st := range f.Statuses {
			args = append(args, string(st))
		}
	}
	if f.Wave != nil {
		q += ` AND wave = ?`
		args = append(args, *f.Wave)
	}
	q += ` ORDER BY node_id`
	rs, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []Row
	for rs.Next() {
		r, err := scanRow(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func (s *SQLite) Claim(ctx context.Context, runID, nodeID, worker string) (bool, error) {
	now := s.ts()
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET status = 'claimed', claim_worker = ?, claim_at = ?, heartbeat_at = ?, updated_at = ?
		 WHERE run_id = ? AND node_id = ? AND status = 'ready'`,
		worker, now, now, now, runID, nodeID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		s.emit(ctx, EventStatus, runID, nodeID)
	}
	return n == 1, nil
}

func (s *SQLite) Heartbeat(ctx context.Context, runID, nodeID, worker string) error {
	now := s.ts()
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET heartbeat_at = ?, updated_at = ?
		 WHERE run_id = ? AND node_id = ? AND claim_worker = ? AND status IN ('claimed', 'in_progress')`,
		now, now, runID, nodeID, worker)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if !s.exists(ctx, runID, nodeID) {
			return ErrNotFound
		}
		return ErrNotOwner
	}
	return nil
}

func (s *SQLite) Release(ctx context.Context, runID, nodeID, worker string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET status = 'ready', claim_worker = '', claim_at = '', heartbeat_at = '', updated_at = ?
		 WHERE run_id = ? AND node_id = ? AND claim_worker = ? AND status IN ('claimed', 'in_progress')`,
		s.ts(), runID, nodeID, worker)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if !s.exists(ctx, runID, nodeID) {
			return ErrNotFound
		}
		return ErrNotOwner
	}
	s.emit(ctx, EventStatus, runID, nodeID)
	return nil
}

func (s *SQLite) SetStatus(ctx context.Context, runID, nodeID string, from, to Status) error {
	if !transitionAllowed(from, to) {
		return ErrBadTransition
	}
	holds := to == StatusClaimed || to == StatusInProgress
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET status = ?, updated_at = ?,
		   claim_worker = CASE WHEN ? THEN claim_worker ELSE '' END,
		   claim_at = CASE WHEN ? THEN claim_at ELSE '' END,
		   heartbeat_at = CASE WHEN ? THEN heartbeat_at ELSE '' END
		 WHERE run_id = ? AND node_id = ? AND status = ?`,
		string(to), s.ts(), holds, holds, holds, runID, nodeID, string(from))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if !s.exists(ctx, runID, nodeID) {
			return ErrNotFound
		}
		return ErrBadTransition
	}
	s.emit(ctx, EventStatus, runID, nodeID)
	return nil
}

func (s *SQLite) SetRevision(ctx context.Context, runID, nodeID string, revision int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE rows SET revision = ?, updated_at = ? WHERE run_id = ? AND node_id = ?`, revision, s.ts(), runID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) AppendAttempt(ctx context.Context, runID, nodeID string, a Attempt) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	// One statement, so concurrent appends cannot lose each other's writes.
	res, err := s.db.ExecContext(ctx,
		`UPDATE rows SET attempts = json_insert(attempts, '$[#]', json(?)), updated_at = ? WHERE run_id = ? AND node_id = ?`,
		string(b), s.ts(), runID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.emit(ctx, EventAttempt, runID, nodeID)
	return nil
}

func (s *SQLite) SetResult(ctx context.Context, runID, nodeID string, r Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE rows SET result = ?, updated_at = ? WHERE run_id = ? AND node_id = ?`, string(b), s.ts(), runID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.emit(ctx, EventResult, runID, nodeID)
	return nil
}

func (s *SQLite) ReleaseStale(ctx context.Context, runID string, olderThan time.Duration) ([]string, error) {
	cutoff := db.TS(s.now().Add(-olderThan))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rs, err := tx.QueryContext(ctx,
		`SELECT node_id FROM rows WHERE run_id = ? AND status IN ('claimed', 'in_progress') AND heartbeat_at < ? ORDER BY node_id`,
		runID, cutoff)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rs.Next() {
		var id string
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rs.Close()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE rows SET status = 'ready', claim_worker = '', claim_at = '', heartbeat_at = '', updated_at = ?
			 WHERE run_id = ? AND node_id = ?`, s.ts(), runID, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		s.emit(ctx, EventStatus, runID, id)
	}
	return ids, nil
}

func (s *SQLite) Watch(ctx context.Context, runID string) (<-chan Event, error) {
	var last int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM events WHERE run_id = ?`, runID).Scan(&last); err != nil {
		return nil, err
	}
	ch := make(chan Event)
	go func() {
		defer close(ch)
		tick := time.NewTicker(s.poll)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			rs, err := s.db.QueryContext(ctx, `SELECT id, node_id, kind, at FROM events WHERE run_id = ? AND id > ? ORDER BY id`, runID, last)
			if err != nil {
				continue // transient; try again on the next tick
			}
			type change struct {
				id               int64
				node, kind, when string
			}
			var changes []change
			for rs.Next() {
				var c change
				if rs.Scan(&c.id, &c.node, &c.kind, &c.when) == nil {
					changes = append(changes, c)
				}
			}
			rs.Close()
			for _, c := range changes {
				last = c.id
				row, err := s.Get(ctx, runID, c.node)
				if err != nil {
					continue
				}
				at, _ := db.ParseTS(c.when)
				select {
				case ch <- Event{Kind: EventKind(c.kind), Row: row, At: at}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func (s *SQLite) Close() error { return nil }
```

- [ ] **Step 8: Run everything for this task**

Run: `cd gophermind-lib && gofmt -l briefv2 && go vet ./briefv2/db/ ./briefv2/blackboard/ && go test ./briefv2/db/ ./briefv2/blackboard/ -race -v 2>&1 | tail -30`
Expected: `gofmt` prints nothing; all tests PASS. If `json_insert` with `$[#]` is unsupported by the bundled SQLite, `TestAttemptsAndResultRoundTrip` fails with a SQL error: report it and use a read-modify-write inside `BEGIN IMMEDIATE` instead (a `db.Conn` with an explicit `BEGIN IMMEDIATE`), keeping `TestConcurrentAppendAttemptLosesNothing` green.

- [ ] **Step 9: Commit**

```bash
git add gophermind-lib/briefv2/db gophermind-lib/briefv2/blackboard
git commit -m "feat(briefv2): shared SQLite database and the blackboard" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The call ledger

**Files:**
- Create: `gophermind-lib/briefv2/ledger/ledger.go`, `gophermind-lib/briefv2/ledger/ledger_test.go`

**Interfaces:**
- Consumes: `db.Open`, `db.TS`, `db.ParseTS`.
- Produces: `ledger.Outcome` constants; `ledger.Call` (with `TaskType` and `NodeClass`); `ledger.Filter{Stage, TaskType, NodeID, Provider string; Outcome Outcome}`; `ledger.ModelSummary{TaskType, NodeClass, Provider, Model string; Calls int; PromptTokens, CompletionTokens, TotalMS int64; Outcomes map[Outcome]int}`; `ledger.Ledger` interface (`Record(ctx, *Call) error` sets `c.ID`; `Amend(ctx, id, Outcome, errorKind) error`; `List(ctx, runID, Filter) ([]Call, error)`; `Summary(ctx, runID) ([]ModelSummary, error)`, one entry per task type, node class, provider and model served); `ledger.NewSQLite(*sql.DB) *SQLite`; `ledger.Digest([]byte) (int, string)`.

- [ ] **Step 1: Write the failing test** (`ledger_test.go`)

```go
package ledger_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func newLedger(t *testing.T) (*ledger.SQLite, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bb.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return ledger.NewSQLite(d), d, path
}

func call(run, stage, prov, model string, o ledger.Outcome) *ledger.Call {
	return &ledger.Call{
		RunID: run, At: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Stage: stage, Scope: "brief", Tier: "strong",
		ChainPos: 1, Provider: prov, ModelRequested: model, ModelServed: model,
		PromptTokens: 100, CompletionTokens: 20, DurationMS: 1500, Outcome: o,
	}
}

func TestRecordAssignsIDAndListReturnsFields(t *testing.T) {
	l, _, _ := newLedger(t)
	ctx := context.Background()
	c := call("r", "clarify", "mini", "qwen", ledger.OutcomeOK)
	c.NodeID, c.Revision, c.PromptBytes, c.PromptSHA256 = "fn-a", 2, 400, "abc"
	c.TaskType, c.NodeClass = "testwrite", "validation"
	if err := l.Record(ctx, c); err != nil || c.ID == 0 {
		t.Fatalf("Record: id=%d err=%v", c.ID, err)
	}
	got, err := l.List(ctx, "r", ledger.Filter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("List = %d rows, %v", len(got), err)
	}
	g := got[0]
	if g.ID != c.ID || g.Stage != "clarify" || g.NodeID != "fn-a" || g.Revision != 2 || g.Provider != "mini" ||
		g.PromptTokens != 100 || g.CompletionTokens != 20 || g.DurationMS != 1500 || g.Outcome != ledger.OutcomeOK ||
		g.PromptBytes != 400 || g.PromptSHA256 != "abc" || !g.At.Equal(c.At) ||
		g.TaskType != "testwrite" || g.NodeClass != "validation" {
		t.Errorf("row lost data: %+v", g)
	}
}

func TestRecordRejectsIncompleteRows(t *testing.T) {
	l, _, _ := newLedger(t)
	for _, c := range []*ledger.Call{
		{Stage: "s", Provider: "p", Outcome: ledger.OutcomeOK},
		{RunID: "r", Provider: "p", Outcome: ledger.OutcomeOK},
		{RunID: "r", Stage: "s", Outcome: ledger.OutcomeOK},
		{RunID: "r", Stage: "s", Provider: "p"},
	} {
		if err := l.Record(context.Background(), c); err == nil {
			t.Errorf("Record accepted an incomplete row: %+v", c)
		}
	}
}

func TestAmendChangesTheOutcome(t *testing.T) {
	l, _, _ := newLedger(t)
	ctx := context.Background()
	c := call("r", "contract", "mini", "qwen", ledger.OutcomeOK)
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := l.Amend(ctx, c.ID, ledger.OutcomeMalformed, "component x is not a feature"); err != nil {
		t.Fatal(err)
	}
	got, _ := l.List(ctx, "r", ledger.Filter{})
	if got[0].Outcome != ledger.OutcomeMalformed || got[0].ErrorKind != "component x is not a feature" {
		t.Errorf("after amend: %+v", got[0])
	}
	if err := l.Amend(ctx, 9999, ledger.OutcomeError, ""); err == nil {
		t.Error("amending an unknown row should fail")
	}
}

func TestListFilters(t *testing.T) {
	l, _, _ := newLedger(t)
	ctx := context.Background()
	for _, c := range []*ledger.Call{
		call("r", "clarify", "mini", "qwen", ledger.OutcomeOK),
		call("r", "testwrite:fn-a", "kilo", "auto", ledger.OutcomeRateLimited),
		call("r", "testwrite:fn-a", "mini", "qwen", ledger.OutcomeOK),
		call("other", "clarify", "mini", "qwen", ledger.OutcomeOK),
	} {
		if strings.HasPrefix(c.Stage, "testwrite") {
			c.NodeID = "fn-a"
		}
		if err := l.Record(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	count := func(f ledger.Filter) int { rows, _ := l.List(ctx, "r", f); return len(rows) }
	if n := count(ledger.Filter{}); n != 3 {
		t.Errorf("all rows of run r = %d, want 3", n)
	}
	if n := count(ledger.Filter{NodeID: "fn-a"}); n != 2 {
		t.Errorf("node filter = %d, want 2", n)
	}
	if n := count(ledger.Filter{Provider: "kilo"}); n != 1 {
		t.Errorf("provider filter = %d, want 1", n)
	}
	if n := count(ledger.Filter{Outcome: ledger.OutcomeOK}); n != 2 {
		t.Errorf("outcome filter = %d, want 2", n)
	}
	if n := count(ledger.Filter{Stage: "clarify"}); n != 1 {
		t.Errorf("stage filter = %d, want 1", n)
	}
}

func TestSummaryMatchesAHandCount(t *testing.T) {
	l, _, _ := newLedger(t)
	ctx := context.Background()
	rows := []struct {
		prov, model string
		o           ledger.Outcome
		ms          int64
	}{
		{"mini", "qwen", ledger.OutcomeOK, 1000},
		{"mini", "qwen", ledger.OutcomeOK, 3000},
		{"mini", "qwen", ledger.OutcomeMalformed, 500},
		{"kilo", "kilo-auto/free", ledger.OutcomeRateLimited, 100},
	}
	for _, r := range rows {
		c := call("r", "s", r.prov, r.model, r.o)
		c.DurationMS = r.ms
		if err := l.Record(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	// A reply served by a different model than requested is summarized under the served name.
	c := call("r", "s", "kilo", "kilo-auto/free", ledger.OutcomeOK)
	c.ModelServed = "stealth/space-bunny-alpha"
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	sum, err := l.Summary(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ledger.ModelSummary{}
	for _, s := range sum {
		by[s.Provider+"/"+s.Model] = s
	}
	mini := by["mini/qwen"]
	if mini.Calls != 3 || mini.PromptTokens != 300 || mini.CompletionTokens != 60 || mini.TotalMS != 4500 ||
		mini.Outcomes[ledger.OutcomeOK] != 2 || mini.Outcomes[ledger.OutcomeMalformed] != 1 {
		t.Errorf("mini/qwen = %+v", mini)
	}
	if k := by["kilo/kilo-auto/free"]; k.Calls != 1 || k.Outcomes[ledger.OutcomeRateLimited] != 1 {
		t.Errorf("kilo/kilo-auto/free = %+v", k)
	}
	if k := by["kilo/stealth/space-bunny-alpha"]; k.Calls != 1 {
		t.Errorf("served model not summarized separately: %+v", by)
	}
	if len(sum) != 3 {
		t.Errorf("summary has %d groups, want 3", len(sum))
	}
}

// The same model doing different kinds of work is summarized once per kind,
// so its results can be compared by task type and by class of function.
func TestSummaryGroupsByTaskTypeAndNodeClass(t *testing.T) {
	l, _, _ := newLedger(t)
	ctx := context.Background()
	rows := []struct {
		task, class string
		o           ledger.Outcome
	}{
		{"contract", "", ledger.OutcomeOK},
		{"testwrite", "pure", ledger.OutcomeOK},
		{"testwrite", "pure", ledger.OutcomeMalformed},
		{"testwrite", "handler", ledger.OutcomeOK},
	}
	for _, r := range rows {
		c := call("r", r.task, "mini", "qwen", r.o)
		c.TaskType, c.NodeClass = r.task, r.class
		if err := l.Record(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := l.Summary(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sum {
		got = append(got, fmt.Sprintf("%s|%s|%s/%s|%d", s.TaskType, s.NodeClass, s.Provider, s.Model, s.Calls))
	}
	want := []string{"contract||mini/qwen|1", "testwrite|handler|mini/qwen|1", "testwrite|pure|mini/qwen|2"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("summary = %v\nwant     %v", got, want)
	}
	if sum[2].Outcomes[ledger.OutcomeOK] != 1 || sum[2].Outcomes[ledger.OutcomeMalformed] != 1 {
		t.Errorf("testwrite/pure outcomes = %v", sum[2].Outcomes)
	}
	only, err := l.List(ctx, "r", ledger.Filter{TaskType: "testwrite"})
	if err != nil || len(only) != 3 {
		t.Errorf("task type filter = %d rows, %v; want 3", len(only), err)
	}
}

func TestNoPromptOrReplyTextIsEverStored(t *testing.T) {
	l, d, path := newLedger(t)
	ctx := context.Background()
	const canary = "CANARY-prompt-text-7f3a91"
	n, sum := ledger.Digest([]byte(canary))
	if n != len(canary) || len(sum) != 64 {
		t.Fatalf("Digest = %d %q", n, sum)
	}
	c := call("r", "clarify", "mini", "qwen", ledger.OutcomeOK)
	c.PromptBytes, c.PromptSHA256, c.ResponseBytes, c.ResponseSHA256 = n, sum, n, sum
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	rs, err := d.Query(`SELECT * FROM calls`)
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := rs.Columns()
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			if strings.Contains(strings.ToLower(toString(v)), strings.ToLower(canary)) {
				t.Errorf("column %s contains the prompt text", cols[i])
			}
		}
	}
	rs.Close()
	d.Close() // checkpoints the write-ahead log into the main file
	for _, f := range []string{path, path + "-wal"} {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), canary) {
			t.Errorf("%s contains the prompt text", f)
		}
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return ""
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/ledger/ 2>&1 | head`
Expected: FAIL, `undefined: ledger.NewSQLite` (no non-test files).

- [ ] **Step 3: Implement `ledger.go`**

```go
// Package ledger records every model call: who was asked, who answered, how
// long it took, and how it went. It stores sizes and SHA-256 hashes of the
// prompt and reply, never their text.
package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
)

type Outcome string

const (
	OutcomeOK           Outcome = "ok"
	OutcomeRateLimited  Outcome = "rate_limited"
	OutcomeTimeout      Outcome = "timeout"
	OutcomeMalformed    Outcome = "malformed"
	OutcomeAuth         Outcome = "auth"
	OutcomeTooLong      Outcome = "too_long"
	OutcomeModelMissing Outcome = "model_missing"
	OutcomeError        Outcome = "error"
)

// Call is one model attempt.
type Call struct {
	ID       int64
	RunID    string
	At       time.Time
	Stage    string // clarify, contract:<pass>, decompose:<component>, coverage, coverage_fill, testwrite:<node>, revise:<node>, implement:<node>
	NodeID   string
	Revision int
	Scope    string // brief | component | node
	Tier     string
	ChainPos int

	// TaskType is the kind of work (clarify, contract, decompose, coverage,
	// testwrite, revise, implement) and NodeClass, for a call about one leaf,
	// the class of function (pure, validation, handler, client, storage,
	// concurrency, wiring, other). Together they let models be compared by the
	// kind of work they were given.
	TaskType  string
	NodeClass string

	Provider       string
	ModelRequested string
	ModelServed    string

	PromptTokens     int
	CompletionTokens int
	PromptBytes      int
	PromptSHA256     string
	ResponseBytes    int
	ResponseSHA256   string
	DurationMS       int64

	Outcome     Outcome
	ErrorKind   string
	RetryAfterS int
}

type Filter struct {
	Stage    string
	TaskType string
	NodeID   string
	Provider string
	Outcome  Outcome
}

// ModelSummary aggregates a run's calls per task type, node class, provider
// and model served.
type ModelSummary struct {
	TaskType         string
	NodeClass        string
	Provider         string
	Model            string
	Calls            int
	PromptTokens     int64
	CompletionTokens int64
	TotalMS          int64
	Outcomes         map[Outcome]int
}

type Ledger interface {
	// Record stores c and sets c.ID.
	Record(ctx context.Context, c *Call) error
	// Amend changes a stored row's outcome, for a reply that arrived fine but
	// failed the stage's parser.
	Amend(ctx context.Context, id int64, o Outcome, errorKind string) error
	List(ctx context.Context, runID string, f Filter) ([]Call, error)
	Summary(ctx context.Context, runID string) ([]ModelSummary, error)
}

// Digest returns the byte length and hex SHA-256 of b.
func Digest(b []byte) (int, string) {
	sum := sha256.Sum256(b)
	return len(b), hex.EncodeToString(sum[:])
}

type SQLite struct{ db *sql.DB }

var _ Ledger = (*SQLite)(nil)

func NewSQLite(d *sql.DB) *SQLite { return &SQLite{db: d} }

func (s *SQLite) Record(ctx context.Context, c *Call) error {
	if c.RunID == "" || c.Stage == "" || c.Provider == "" || c.Outcome == "" {
		return errors.New("ledger: run, stage, provider and outcome are required")
	}
	if c.At.IsZero() {
		c.At = time.Now()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO calls (run_id, at, stage, task_type, node_class, node_id, revision, scope, tier, chain_pos, provider, model_requested, model_served,
		   prompt_tokens, completion_tokens, prompt_bytes, prompt_sha256, response_bytes, response_sha256, duration_ms,
		   outcome, error_kind, retry_after_s)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.RunID, db.TS(c.At), c.Stage, c.TaskType, c.NodeClass, c.NodeID, c.Revision, c.Scope, c.Tier, c.ChainPos, c.Provider, c.ModelRequested, c.ModelServed,
		c.PromptTokens, c.CompletionTokens, c.PromptBytes, c.PromptSHA256, c.ResponseBytes, c.ResponseSHA256, c.DurationMS,
		string(c.Outcome), c.ErrorKind, c.RetryAfterS)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLite) Amend(ctx context.Context, id int64, o Outcome, errorKind string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE calls SET outcome = ?, error_kind = ? WHERE id = ?`, string(o), errorKind, id)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("ledger: no call with id %d", id)
	}
	return nil
}

func (s *SQLite) List(ctx context.Context, runID string, f Filter) ([]Call, error) {
	q := `SELECT id, run_id, at, stage, task_type, node_class, node_id, revision, scope, tier, chain_pos, provider, model_requested, model_served,
	        prompt_tokens, completion_tokens, prompt_bytes, prompt_sha256, response_bytes, response_sha256, duration_ms,
	        outcome, error_kind, retry_after_s
	      FROM calls WHERE run_id = ?`
	args := []any{runID}
	add := func(col, val string) {
		if val != "" {
			q += ` AND ` + col + ` = ?`
			args = append(args, val)
		}
	}
	add("stage", f.Stage)
	add("task_type", f.TaskType)
	add("node_id", f.NodeID)
	add("provider", f.Provider)
	add("outcome", string(f.Outcome))
	q += ` ORDER BY at, id`
	rs, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	defer rs.Close()
	var out []Call
	for rs.Next() {
		var c Call
		var at, outcome string
		if err := rs.Scan(&c.ID, &c.RunID, &at, &c.Stage, &c.TaskType, &c.NodeClass, &c.NodeID, &c.Revision, &c.Scope, &c.Tier, &c.ChainPos, &c.Provider,
			&c.ModelRequested, &c.ModelServed, &c.PromptTokens, &c.CompletionTokens, &c.PromptBytes, &c.PromptSHA256,
			&c.ResponseBytes, &c.ResponseSHA256, &c.DurationMS, &outcome, &c.ErrorKind, &c.RetryAfterS); err != nil {
			return nil, fmt.Errorf("ledger: %w", err)
		}
		c.At, _ = db.ParseTS(at)
		c.Outcome = Outcome(outcome)
		out = append(out, c)
	}
	return out, rs.Err()
}

func (s *SQLite) Summary(ctx context.Context, runID string) ([]ModelSummary, error) {
	rs, err := s.db.QueryContext(ctx,
		`SELECT task_type, node_class, provider, CASE WHEN model_served = '' THEN model_requested ELSE model_served END AS m, outcome,
		        COUNT(*), SUM(prompt_tokens), SUM(completion_tokens), SUM(duration_ms)
		 FROM calls WHERE run_id = ? GROUP BY task_type, node_class, provider, m, outcome`, runID)
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	defer rs.Close()
	byKey := map[string]*ModelSummary{}
	for rs.Next() {
		var task, class, prov, model, outcome string
		var n int
		var pt, ct, ms int64
		if err := rs.Scan(&task, &class, &prov, &model, &outcome, &n, &pt, &ct, &ms); err != nil {
			return nil, fmt.Errorf("ledger: %w", err)
		}
		k := task + "\x00" + class + "\x00" + prov + "\x00" + model
		m := byKey[k]
		if m == nil {
			m = &ModelSummary{TaskType: task, NodeClass: class, Provider: prov, Model: model, Outcomes: map[Outcome]int{}}
			byKey[k] = m
		}
		m.Calls += n
		m.PromptTokens += pt
		m.CompletionTokens += ct
		m.TotalMS += ms
		m.Outcomes[Outcome(outcome)] += n
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	out := make([]ModelSummary, 0, len(byKey))
	for _, m := range byKey {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.TaskType != b.TaskType {
			return a.TaskType < b.TaskType
		}
		if a.NodeClass != b.NodeClass {
			return a.NodeClass < b.NodeClass
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Model < b.Model
	})
	return out, nil
}
```

- [ ] **Step 4: Run and commit**

Run: `cd gophermind-lib && gofmt -l briefv2 && go vet ./briefv2/ledger/ && go test ./briefv2/ledger/ -race -v 2>&1 | tail -20`
Expected: `gofmt` prints nothing; PASS (7 test functions).

```bash
git add gophermind-lib/briefv2/ledger
git commit -m "feat(briefv2): model-call ledger with hashes and sizes only" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Progress events and the provider layer

**Files:**
- Create: `gophermind-lib/briefv2/events/events.go`, `events_test.go`
- Create: `gophermind-lib/briefv2/provider/provider.go` (copy), `errors.go`, `openai.go`, `fake.go`, `openai_test.go`

**Interfaces:**
- Consumes: `ledger.Call` (Task 2).
- Produces: `events.Event{Kind, Stage, NodeID, Message string; At time.Time; Call *ledger.Call}`; `events.Sink` (`Emit(Event)`); `events.Nop` (a `Sink`); `events.NewCollector() *Collector` with `Emit(Event)`, `Events() []Event` (a copy), `OfKind(kind string) []Event`; kind constants `events.KindStageStarted`, `KindStageFinished`, `KindWaitingOnHuman`, `KindCall`, `KindLedgerError`, `KindWarning`, `KindCoverageGap` (all untyped string constants).
- Produces: the handoff types exactly (`provider.Provider`, `Request`, `Response`, `Message`, `Role`, `Usage`, `ModelInfo`, `Config`, `ErrRateLimited{RetryAfter}`, `ErrContextTooLong{PromptTokens, Limit}`, `ErrAuth{Provider}`, `ErrTransient{Cause}`), all error types as values, not pointers.
- Produces: `provider.ErrModelNotFound{Model string}`; `provider.NewOpenAI(cfg provider.Config) provider.Provider`; `provider.NewFake(name string, models []ModelInfo, fn func(call int, req Request) (Response, error)) *Fake` with `Calls() int` and `Requests() []Request`.

- [ ] **Step 1: Write the failing events test** (`briefv2/events/events_test.go`)

```go
package events_test

import (
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func TestCollectorKeepsOrderAndStampsTime(t *testing.T) {
	c := events.NewCollector()
	c.Emit(events.Event{Kind: events.KindStageStarted, Stage: "clarify"})
	fixed := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	c.Emit(events.Event{Kind: events.KindWarning, Message: "w", At: fixed})
	c.Emit(events.Event{Kind: events.KindCall, Call: &ledger.Call{ID: 7}})

	got := c.Events()
	if len(got) != 3 || got[0].Stage != "clarify" || got[1].Message != "w" || got[2].Call.ID != 7 {
		t.Fatalf("events = %+v", got)
	}
	if got[0].At.IsZero() {
		t.Error("an event without a time should be stamped")
	}
	if !got[1].At.Equal(fixed) {
		t.Errorf("an event with a time must keep it, got %v", got[1].At)
	}
	if n := len(c.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("OfKind(warning) = %d, want 1", n)
	}
}

func TestCollectorReturnsACopy(t *testing.T) {
	c := events.NewCollector()
	c.Emit(events.Event{Kind: events.KindWarning, Message: "a"})
	first := c.Events()
	first[0].Message = "changed"
	if c.Events()[0].Message != "a" {
		t.Error("mutating the returned slice changed the collector")
	}
}

func TestCollectorIsSafeForConcurrentUse(t *testing.T) {
	c := events.NewCollector()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Emit(events.Event{Kind: events.KindWarning})
			_ = c.Events()
		}()
	}
	wg.Wait()
	if n := len(c.Events()); n != 50 {
		t.Errorf("collected %d events, want 50", n)
	}
}

func TestNopDiscards(t *testing.T) {
	events.Nop.Emit(events.Event{Kind: events.KindWarning}) // must not panic
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/events/ 2>&1 | head -5`
Expected: FAIL, `no non-test Go files` (the package has only the test).

- [ ] **Step 3: Implement `events.go`**

```go
// Package events is how the engine reports progress. The engine writes events
// to a Sink and never reads from one; the terminal, a file, or (later) the
// desktop app implement the Sink.
package events

import (
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/ledger"
)

// Event kinds.
const (
	KindStageStarted   = "stage_started"
	KindStageFinished  = "stage_finished"
	KindWaitingOnHuman = "waiting_on_human"
	KindCall           = "call"
	KindLedgerError    = "ledger_error"
	KindWarning        = "warning"
	KindCoverageGap    = "coverage_gap"
)

// Event is one progress report. Call is set only for KindCall and carries the
// ledger row of the attempt.
type Event struct {
	Kind    string
	Stage   string
	NodeID  string
	Message string
	At      time.Time
	Call    *ledger.Call
}

// Sink receives events. Implementations must be safe for concurrent use.
type Sink interface{ Emit(Event) }

type nop struct{}

func (nop) Emit(Event) {}

// Nop discards every event.
var Nop Sink = nop{}

// Collector keeps every event in memory, for tests and for the status command.
type Collector struct {
	mu     sync.Mutex
	events []Event
}

func NewCollector() *Collector { return &Collector{} }

// Emit stamps the event with the current time when At is zero and stores it.
func (c *Collector) Emit(e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

// Events returns a copy of everything collected so far, oldest first.
func (c *Collector) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Event(nil), c.events...)
}

// OfKind returns the collected events of one kind, oldest first.
func (c *Collector) OfKind(kind string) []Event {
	var out []Event
	for _, e := range c.Events() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
```

- [ ] **Step 4: Run the events tests**

Run: `cd gophermind-lib && gofmt -l briefv2/events && go vet ./briefv2/events/ && go test ./briefv2/events/ -race -v 2>&1 | tail -10`
Expected: `gofmt` prints nothing; PASS (4 tests).

- [ ] **Step 5: Copy the handoff provider interface and add the model-not-found error**

```bash
cd gophermind-lib
mkdir -p briefv2/provider
cp ../docs/briefv2/handoff/interfaces/provider.go briefv2/provider/provider.go
gofmt -l briefv2/provider   # must print nothing
```

`briefv2/provider/errors.go` (spec deviation P8; the handoff has no such error):

```go
package provider

// ErrModelNotFound: the provider answered 404 "model not found" (the mini's
// Ollama did this on 2026-09-29 when a model was removed mid-run). The router
// skips that provider/model entry for the rest of the run and warns once.
type ErrModelNotFound struct{ Model string }

func (e ErrModelNotFound) Error() string { return "provider: model not found: " + e.Model }
```

- [ ] **Step 6: Write the failing provider tests** (`briefv2/provider/openai_test.go`)

The tests use `httptest` servers on loopback only. Two of them prove that neither the API key nor the prompt text can appear in an error string, even when the server echoes the whole request back in its error body.

```go
package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/provider"
)

const (
	secretKey = "sk-SECRET-key-91b7"
	canary    = "CANARY-prompt-text-5e21"
)

func newProvider(t *testing.T, h http.Handler, key string) provider.Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return provider.NewOpenAI(provider.Config{
		Name: "mini", BaseURL: srv.URL + "/v1", APIKey: key, MaxConcurrent: 1, HTTPClient: srv.Client(),
		Models: []provider.ModelInfo{{ID: "qwen", ContextTokens: 32768}},
	})
}

func request() provider.Request {
	return provider.Request{
		Model:       "qwen",
		Messages:    []provider.Message{{Role: provider.RoleSystem, Content: "be brief"}, {Role: provider.RoleUser, Content: canary}},
		MaxTokens:   256,
		Temperature: 0.2,
	}
}

func TestCompleteSuccessSendsAnOpenAIRequestAndReadsTheReply(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		io.WriteString(w, `{"model":"stealth/served","choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`)
	}), secretKey)

	got, err := p.Complete(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer "+secretKey {
		t.Errorf("path %q auth %q", gotPath, gotAuth)
	}
	if gotBody["model"] != "qwen" || gotBody["max_tokens"] != float64(256) || gotBody["temperature"] != 0.2 {
		t.Errorf("body = %v", gotBody)
	}
	if msgs, _ := gotBody["messages"].([]any); len(msgs) != 2 {
		t.Errorf("messages = %v", gotBody["messages"])
	}
	if got.Text != "hello" || got.Model != "stealth/served" || got.Usage.PromptTokens != 11 || got.Usage.CompletionTokens != 3 || got.Duration <= 0 {
		t.Errorf("response = %+v", got)
	}
}

func TestNoAuthorizationHeaderWithoutAKey(t *testing.T) {
	var sawAuth bool
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}), "")
	got, err := p.Complete(context.Background(), request())
	if err != nil || sawAuth {
		t.Fatalf("err=%v sawAuth=%v", err, sawAuth)
	}
	if got.Model != "qwen" {
		t.Errorf("a reply with no model field should report the requested model, got %q", got.Model)
	}
}

func TestStatusCodesMapToTypedErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		header  map[string]string
		body    string
		check   func(error) bool
		message string
	}{
		{"429 with Retry-After", 429, map[string]string{"Retry-After": "7"}, `{"error":"slow down"}`,
			func(e error) bool {
				var x provider.ErrRateLimited
				return errors.As(e, &x) && x.RetryAfter == 7*time.Second
			}, "rate limited with 7s"},
		{"429 without Retry-After", 429, nil, ``,
			func(e error) bool { var x provider.ErrRateLimited; return errors.As(e, &x) && x.RetryAfter == 0 }, "rate limited with 0"},
		{"401", 401, nil, `{"error":"bad key"}`,
			func(e error) bool { var x provider.ErrAuth; return errors.As(e, &x) && x.Provider == "mini" }, "auth"},
		{"403", 403, nil, `{"error":"forbidden"}`,
			func(e error) bool { var x provider.ErrAuth; return errors.As(e, &x) }, "auth"},
		{"400 context length", 400, nil, `{"error":{"message":"This model's maximum context length is 32768 tokens"}}`,
			func(e error) bool { var x provider.ErrContextTooLong; return errors.As(e, &x) && x.Limit == 32768 }, "context too long with the model's limit"},
		{"404 model not found", 404, nil, `{"error":{"message":"model \"qwen\" not found, try pulling it first"}}`,
			func(e error) bool { var x provider.ErrModelNotFound; return errors.As(e, &x) && x.Model == "qwen" }, "model not found"},
		{"500", 500, nil, `boom`,
			func(e error) bool { var x provider.ErrTransient; return errors.As(e, &x) }, "transient"},
		{"503", 503, nil, ``,
			func(e error) bool { var x provider.ErrTransient; return errors.As(e, &x) }, "transient"},
		{"400 other", 400, nil, `{"error":"unknown field"}`,
			func(e error) bool {
				var a provider.ErrContextTooLong
				var b provider.ErrTransient
				return e != nil && !errors.As(e, &a) && !errors.As(e, &b)
			}, "a plain error, neither context nor transient"},
		{"404 not a model problem", 404, nil, `page missing`,
			func(e error) bool { var x provider.ErrModelNotFound; return e != nil && !errors.As(e, &x) }, "a plain error, not model_not_found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range c.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.status)
				io.WriteString(w, c.body)
			}), secretKey)
			_, err := p.Complete(context.Background(), request())
			if !c.check(err) {
				t.Errorf("err = %#v, want %s", err, c.message)
			}
		})
	}
}

func TestErrorsNeverCarryTheKeyOrThePrompt(t *testing.T) {
	// The server echoes the whole request back in its error body, the way some gateways do.
	for _, status := range []int{400, 401, 404, 429, 500} {
		p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			w.WriteHeader(status)
			io.WriteString(w, "maximum context length model not found "+string(b)+r.Header.Get("Authorization"))
		}), secretKey)
		_, err := p.Complete(context.Background(), request())
		if err == nil {
			t.Fatalf("status %d: no error", status)
		}
		if s := err.Error(); strings.Contains(s, secretKey) || strings.Contains(s, canary) {
			t.Errorf("status %d: error text leaks the key or the prompt: %q", status, s)
		}
	}
}

func TestTransportFailureIsTransientAndDoesNotNameTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening any more
	p := provider.NewOpenAI(provider.Config{Name: "mini", BaseURL: url + "/v1", APIKey: secretKey, HTTPClient: http.DefaultClient})
	_, err := p.Complete(context.Background(), request())
	var tr provider.ErrTransient
	if !errors.As(err, &tr) {
		t.Fatalf("err = %v, want ErrTransient", err)
	}
	if strings.Contains(err.Error(), secretKey) || strings.Contains(err.Error(), canary) {
		t.Errorf("error leaks: %v", err)
	}
}

func TestCancellationReturnsTheContextError(t *testing.T) {
	release := make(chan struct{})
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}), "")
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := p.Complete(ctx, request())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestReplyProblemsAreTransient(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     `<html>`,
		"no choices":   `{"choices":[]}`,
		"error object": `{"error":{"message":"overloaded"},"choices":[{"message":{"content":"x"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }), "")
			_, err := p.Complete(context.Background(), request())
			var tr provider.ErrTransient
			if !errors.As(err, &tr) {
				t.Errorf("err = %v, want ErrTransient", err)
			}
		})
	}
}

func TestFakeCountsCallsAndRecordsRequests(t *testing.T) {
	f := provider.NewFake("fake", []provider.ModelInfo{{ID: "m", ContextTokens: 100}}, func(call int, req provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: time.Second}
		}
		return provider.Response{Text: "ok", Model: req.Model}, nil
	})
	if f.Name() != "fake" || len(f.Models()) != 1 {
		t.Fatalf("name/models: %q %v", f.Name(), f.Models())
	}
	if _, err := f.Complete(context.Background(), provider.Request{Model: "m"}); err == nil {
		t.Fatal("first call should be rate limited")
	}
	if r, err := f.Complete(context.Background(), provider.Request{Model: "m"}); err != nil || r.Text != "ok" {
		t.Fatalf("second call = %+v, %v", r, err)
	}
	if f.Calls() != 2 || len(f.Requests()) != 2 {
		t.Errorf("calls=%d requests=%d", f.Calls(), len(f.Requests()))
	}
}

func TestFakeIsSafeForConcurrentUseAndHonorsCancellation(t *testing.T) {
	f := provider.NewFake("fake", nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.Complete(context.Background(), provider.Request{Model: "m"}) }()
	}
	wg.Wait()
	if f.Calls() != 40 {
		t.Errorf("calls = %d, want 40", f.Calls())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Complete(ctx, provider.Request{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: err = %v", err)
	}
	if f.Calls() != 40 {
		t.Error("a cancelled call must not reach the script")
	}
}
```

- [ ] **Step 7: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/provider/ 2>&1 | head -6`
Expected: FAIL, `undefined: provider.NewOpenAI` and `undefined: provider.NewFake`.

- [ ] **Step 8: Implement the scripted fake and the OpenAI-compatible client**

`briefv2/provider/fake.go`:

```go
package provider

import (
	"context"
	"sync"
)

// Fake is a scripted provider for tests. fn receives the 1-based call number
// and the request and returns the reply or a typed error.
type Fake struct {
	name   string
	models []ModelInfo
	fn     func(call int, req Request) (Response, error)

	mu   sync.Mutex
	n    int
	reqs []Request
}

var _ Provider = (*Fake)(nil)

// NewFake builds a Fake. A nil fn answers every call with an empty reply that
// names the requested model.
func NewFake(name string, models []ModelInfo, fn func(call int, req Request) (Response, error)) *Fake {
	return &Fake{name: name, models: models, fn: fn}
}

func (f *Fake) Name() string        { return f.name }
func (f *Fake) Models() []ModelInfo { return f.models }

func (f *Fake) Complete(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	f.mu.Lock()
	f.n++
	n := f.n
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.fn == nil {
		return Response{Model: req.Model}, nil
	}
	return f.fn(n, req)
}

// Calls is how many times Complete reached the script.
func (f *Fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

// Requests returns a copy of every request received, oldest first.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.reqs...)
}
```

`briefv2/provider/openai.go`:

```go
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxReply bounds how much of a reply body is read.
const maxReply = 8 << 20

// OpenAI talks to any server that speaks the OpenAI chat completions protocol:
// Ollama, Kilo Code's gateway, OVHcloud, and most cloud APIs.
type OpenAI struct{ cfg Config }

var _ Provider = (*OpenAI)(nil)

// NewOpenAI builds the client. cfg.HTTPClient is used as given; when it is nil
// http.DefaultClient is used (the harness proxy plan replaces this).
func NewOpenAI(cfg Config) Provider { return &OpenAI{cfg: cfg} }

func (o *OpenAI) Name() string        { return o.cfg.Name }
func (o *OpenAI) Models() []ModelInfo { return o.cfg.Models }

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireRequest struct {
	Model       string        `json:"model"`
	Messages    []wireMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
	Stop        []string      `json:"stop,omitempty"`
}

type wireReply struct {
	Model   string `json:"model"`
	Choices []struct {
		Message wireMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	wr := wireRequest{Model: req.Model, MaxTokens: req.MaxTokens, Temperature: req.Temperature, Stop: req.StopSequences}
	for _, m := range req.Messages {
		wr.Messages = append(wr.Messages, wireMessage{Role: string(m.Role), Content: m.Content})
	}
	body, err := json.Marshal(wr)
	if err != nil {
		return Response{}, fmt.Errorf("provider %s: %w", o.cfg.Name, err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("provider %s: bad base_url", o.cfg.Name)
	}
	hreq.Header.Set("Content-Type", "application/json")
	if o.cfg.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	}
	client := o.cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	start := time.Now()
	resp, err := client.Do(hreq)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return Response{}, cerr
		}
		// A *url.Error carries the URL; report only the underlying cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: %w", o.cfg.Name, err)}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return Response{}, cerr
		}
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reading reply: %w", o.cfg.Name, err)}
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, o.statusError(resp, raw, req)
	}
	var wp wireReply
	if err := json.Unmarshal(raw, &wp); err != nil {
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reply is not JSON", o.cfg.Name)}
	}
	if len(wp.Error) > 0 && string(wp.Error) != "null" {
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reply carried an error object", o.cfg.Name)}
	}
	if len(wp.Choices) == 0 {
		return Response{}, ErrTransient{Cause: fmt.Errorf("provider %s: reply had no choices", o.cfg.Name)}
	}
	served := wp.Model
	if served == "" {
		served = req.Model
	}
	return Response{
		Text:     wp.Choices[0].Message.Content,
		Usage:    Usage{PromptTokens: wp.Usage.PromptTokens, CompletionTokens: wp.Usage.CompletionTokens},
		Model:    served,
		Duration: time.Since(start),
	}, nil
}

// statusError maps a non-200 reply to a typed error. Messages name the
// provider and the status only: a reply body can echo the prompt, and a
// prompt, a key, or a secret must never end up in an error string.
func (o *OpenAI) statusError(resp *http.Response, raw []byte, req Request) error {
	text := strings.ToLower(string(raw))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		var after time.Duration
		if secs, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && secs > 0 {
			after = time.Duration(secs) * time.Second
		}
		return ErrRateLimited{RetryAfter: after}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrAuth{Provider: o.cfg.Name}
	case resp.StatusCode == http.StatusBadRequest && looksLikeContextError(text):
		limit := 0
		for _, m := range o.cfg.Models {
			if m.ID == req.Model {
				limit = m.ContextTokens
			}
		}
		return ErrContextTooLong{Limit: limit}
	case resp.StatusCode == http.StatusNotFound && strings.Contains(text, "model") &&
		(strings.Contains(text, "not found") || strings.Contains(text, "does not exist")):
		return ErrModelNotFound{Model: req.Model}
	case resp.StatusCode >= 500:
		return ErrTransient{Cause: fmt.Errorf("provider %s: HTTP %d", o.cfg.Name, resp.StatusCode)}
	}
	return fmt.Errorf("provider %s: HTTP %d", o.cfg.Name, resp.StatusCode)
}

func looksLikeContextError(lower string) bool {
	for _, marker := range []string{"context length", "context_length", "maximum context", "context window", "too many tokens", "prompt is too long"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 9: Run and commit**

Run: `cd gophermind-lib && gofmt -l briefv2 && go vet ./briefv2/events/ ./briefv2/provider/ && go test ./briefv2/events/ ./briefv2/provider/ -race -v 2>&1 | tail -30`
Expected: `gofmt` prints nothing; PASS (events: 4 tests; provider: 9 test functions).

```bash
git add gophermind-lib/briefv2/events gophermind-lib/briefv2/provider
git commit -m "feat(briefv2): progress events and the provider layer (OpenAI-compatible client, fake)" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Settings (`gophermind.yaml`)

**Files:**
- Create: `gophermind-lib/briefv2/settings/settings.go`, `providers.go`, `settings_test.go`

**Interfaces:**
- Consumes: `provider.ModelInfo`, `provider.Config`, `provider.NewOpenAI`, `provider.Provider` (Task 3); `config.Dir()` (existing package `gophermind-lib/config`, honors `GOPHERMIND_CONFIG_DIR`).
- Produces: `settings.Config` with fields `Providers []ProviderConfig`, `Models map[string][]string`, `Privacy Privacy{Mode}`, `Defaults Defaults{MaxContextTokens, MaxRevisions, MaxCoverageRounds int; CallTimeout time.Duration; MaxWaitMinutes int}`, `RateLimits RateLimits{CooldownAfter429Seconds, BackoffInitialSeconds, BackoffMaxSeconds, BackoffMultiplier int}`, `Human Human{Mode}`, `Vault Vault{Path}`.
- Produces: `settings.ProviderConfig{Name, BaseURL string; Visibility Visibility; MaxConcurrent int; Models []ModelEntry; APIKeySecret string}`; `settings.ModelEntry{ID string; ContextTokens int}`; `settings.Visibility` with `settings.Private` and `settings.Public`; `settings.Tiers []string{"strong","standard","any"}`.
- Produces: `settings.Default() *Config`; `settings.Path() (string, error)`; `settings.Load(path string) (*Config, error)`; `settings.SplitEntry(entry string) (providerName, model string, ok bool)`; `(*Config).Validate() error`; `(*Config).Visibility(provider string) (Visibility, bool)`; `(*Config).ModelInfo(entry string) (provider.ModelInfo, bool)`; `(*Config).BuildProviders(client *http.Client, secret func(name string) (string, error)) (map[string]provider.Provider, error)`.

- [ ] **Step 1: Write the failing tests** (`briefv2/settings/settings_test.go`)

```go
package settings_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
)

func TestDefaultIsValid(t *testing.T) {
	if err := settings.Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFirstLoadWritesDefaultsWithPrivateModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "conf")
	path := filepath.Join(dir, "gophermind.yaml")
	c, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, settings.Default()) {
		t.Errorf("first load did not return the defaults:\n%+v", c)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %v, want 0700", di.Mode().Perm())
	}
	// The written file loads back to the same values, durations included.
	again, err := settings.Load(path)
	if err != nil || !reflect.DeepEqual(again, settings.Default()) {
		t.Errorf("round trip differs: %v\n%+v", err, again)
	}
	if again.Defaults.CallTimeout != 10*time.Minute {
		t.Errorf("call_timeout = %v", again.Defaults.CallTimeout)
	}
}

func TestLoadReadsAnExistingFileWithoutRewritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gophermind.yaml")
	if _, err := settings.Load(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	edited := strings.Replace(string(b), "max_revisions: 2", "max_revisions: 5", 1)
	if edited == string(b) {
		t.Fatal("the default file no longer contains max_revisions: 2; update this test")
	}
	os.WriteFile(path, []byte(edited), 0o600)
	c, err := settings.Load(path)
	if err != nil || c.Defaults.MaxRevisions != 5 {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != edited {
		t.Error("Load rewrote an existing file")
	}
}

func TestPathHonorsTheConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	p, err := settings.Path()
	if err != nil || p != filepath.Join(dir, "gophermind.yaml") {
		t.Errorf("Path = %q, %v", p, err)
	}
}

func TestLoadRefusesAKeyValueInTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gophermind.yaml")
	if _, err := settings.Load(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	withKey := strings.Replace(string(b), "name: kilo\n", "name: kilo\n    api_key: sk-LEAK-4417\n", 1)
	os.WriteFile(path, []byte(withKey), 0o600)
	_, err := settings.Load(path)
	if err == nil {
		t.Fatal("a file with api_key was accepted")
	}
	if strings.Contains(err.Error(), "sk-LEAK-4417") {
		t.Errorf("the error repeats the key: %v", err)
	}
}

func TestLoadRejectsBadDurationsAndBadYAML(t *testing.T) {
	for name, edit := range map[string]func(string) string{
		"unparsable duration": func(s string) string {
			return strings.Replace(s, "call_timeout: 10m0s", "call_timeout: ten minutes", 1)
		},
		"not yaml": func(string) string { return "providers: [unclosed" },
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gophermind.yaml")
			settings.Load(path)
			b, _ := os.ReadFile(path)
			os.WriteFile(path, []byte(edit(string(b))), 0o600)
			if _, err := settings.Load(path); err == nil {
				t.Error("Load accepted a broken file")
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*settings.Config)
		want   string
	}{
		{"no providers", func(c *settings.Config) { c.Providers = nil }, "at least one provider"},
		{"duplicate provider", func(c *settings.Config) { c.Providers[1].Name = "mini" }, "duplicate provider"},
		{"slash in name", func(c *settings.Config) { c.Providers[0].Name = "a/b" }, "slash"},
		{"bad visibility", func(c *settings.Config) { c.Providers[0].Visibility = "shared" }, "visibility"},
		{"no base url", func(c *settings.Config) { c.Providers[0].BaseURL = "" }, "base_url"},
		{"zero concurrency", func(c *settings.Config) { c.Providers[0].MaxConcurrent = 0 }, "max_concurrent"},
		{"no models", func(c *settings.Config) { c.Providers[0].Models = nil }, "at least one model"},
		{"zero context", func(c *settings.Config) { c.Providers[0].Models[0].ContextTokens = 0 }, "context_tokens"},
		{"duplicate model", func(c *settings.Config) {
			c.Providers[0].Models = append(c.Providers[0].Models, c.Providers[0].Models[0])
		}, "duplicate model"},
		{"unknown provider in a tier", func(c *settings.Config) { c.Models["strong"] = []string{"nobody/qwen"} }, "not a configured provider/model"},
		{"unknown model in a tier", func(c *settings.Config) { c.Models["any"] = []string{"mini/nope"} }, "not a configured provider/model"},
		{"entry without a slash", func(c *settings.Config) { c.Models["any"] = []string{"mini"} }, "not a configured provider/model"},
		{"missing tier", func(c *settings.Config) { delete(c.Models, "standard") }, "tier standard has no entries"},
		{"unknown tier", func(c *settings.Config) { c.Models["huge"] = []string{"mini/qwen3.6:35b-a3b"} }, "unknown tier"},
		{"bad privacy mode", func(c *settings.Config) { c.Privacy.Mode = "open" }, "privacy.mode"},
		{"zero call timeout", func(c *settings.Config) { c.Defaults.CallTimeout = 0 }, "call_timeout"},
		{"negative coverage rounds", func(c *settings.Config) { c.Defaults.MaxCoverageRounds = -1 }, "max_coverage_rounds"},
		{"backoff max below initial", func(c *settings.Config) { c.RateLimits.BackoffMaxSeconds = 1 }, "backoff_max_seconds"},
		{"zero cooldown", func(c *settings.Config) { c.RateLimits.CooldownAfter429Seconds = 0 }, "cooldown_after_429_seconds"},
		{"bad human mode", func(c *settings.Config) { c.Human.Mode = "carrier pigeon" }, "human.mode"},
		{"no vault path", func(c *settings.Config) { c.Vault.Path = "" }, "vault.path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := settings.Default()
			c.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestLookups(t *testing.T) {
	c := settings.Default()
	if v, ok := c.Visibility("mini"); !ok || v != settings.Private {
		t.Errorf("mini = %v %v", v, ok)
	}
	if v, ok := c.Visibility("kilo"); !ok || v != settings.Public {
		t.Errorf("kilo = %v %v", v, ok)
	}
	if _, ok := c.Visibility("nobody"); ok {
		t.Error("unknown provider reported as known")
	}
	if mi, ok := c.ModelInfo("kilo/kilo-auto/free"); !ok || mi.ID != "kilo-auto/free" || mi.ContextTokens != 131072 {
		t.Errorf("ModelInfo = %+v %v", mi, ok)
	}
	if _, ok := c.ModelInfo("kilo/other"); ok {
		t.Error("unknown model reported as known")
	}
	for entry, want := range map[string][3]string{
		"kilo/kilo-auto/free":  {"kilo", "kilo-auto/free", "true"},
		"mini/qwen3.6:35b-a3b": {"mini", "qwen3.6:35b-a3b", "true"},
		"noslash":              {"", "", "false"},
		"/model":               {"", "", "false"},
		"prov/":                {"", "", "false"},
	} {
		p, m, ok := settings.SplitEntry(entry)
		if p != want[0] || m != want[1] || (want[2] == "true") != ok {
			t.Errorf("SplitEntry(%q) = %q %q %v", entry, p, m, ok)
		}
	}
}

func TestBuildProvidersResolvesKeysFromTheSecretSource(t *testing.T) {
	var auths = map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths[r.URL.Path] = r.Header.Get("Authorization")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	cfg := settings.Default()
	cfg.Providers = []settings.ProviderConfig{
		{Name: "keyed", BaseURL: srv.URL + "/keyed", Visibility: settings.Public, MaxConcurrent: 1, APIKeySecret: "keyed-key",
			Models: []settings.ModelEntry{{ID: "m", ContextTokens: 1000}}},
		{Name: "open", BaseURL: srv.URL + "/open", Visibility: settings.Private, MaxConcurrent: 1,
			Models: []settings.ModelEntry{{ID: "m", ContextTokens: 1000}}},
	}
	cfg.Models = map[string][]string{"strong": {"open/m"}, "standard": {"open/m"}, "any": {"keyed/m"}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	var asked []string
	ps, err := cfg.BuildProviders(srv.Client(), func(name string) (string, error) {
		asked = append(asked, name)
		return "sk-VALUE", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps["keyed"].Name() != "keyed" || ps["open"].Models()[0].ID != "m" {
		t.Fatalf("providers = %v", ps)
	}
	if len(asked) != 1 || asked[0] != "keyed-key" {
		t.Errorf("secret source asked for %v, want only keyed-key", asked)
	}
	for _, name := range []string{"keyed", "open"} {
		if _, err := ps[name].Complete(context.Background(), provider.Request{Model: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	if auths["/keyed/chat/completions"] != "Bearer sk-VALUE" || auths["/open/chat/completions"] != "" {
		t.Errorf("auth headers = %v", auths)
	}
}

func TestBuildProvidersSecretFailuresNameTheSecretNotItsValue(t *testing.T) {
	cfg := settings.Default()
	cfg.Providers[1].APIKeySecret = "kilo-key"
	if _, err := cfg.BuildProviders(nil, nil); err == nil {
		t.Error("a provider that needs a secret was built with no secret source")
	}
	_, err := cfg.BuildProviders(nil, func(string) (string, error) { return "sk-LEAK", errors.New("vault locked") })
	if err == nil || !strings.Contains(err.Error(), "kilo-key") || strings.Contains(err.Error(), "sk-LEAK") {
		t.Errorf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/settings/ 2>&1 | head -5`
Expected: FAIL, `no non-test Go files`.

- [ ] **Step 3: Implement `settings.go`**

The decoder runs with `KnownFields(true)`, so a pasted `api_key: ...` line is refused instead of silently kept, and the error names the field, never the value.

```go
// Package settings reads ~/.gophermind/gophermind.yaml: which providers exist,
// which of them may see what (visibility), and which models each tier tries in
// which order. A provider's key is never stored here, only the name of the
// vault entry that holds it.
package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/config"
)

// Visibility says whether a provider runs on John's hardware or somewhere else.
type Visibility string

const (
	Private Visibility = "private"
	Public  Visibility = "public"
)

// The three model tiers, in the order a config lists them.
var Tiers = []string{"strong", "standard", "any"}

type ModelEntry struct {
	ID            string `yaml:"id"`
	ContextTokens int    `yaml:"context_tokens"`
}

type ProviderConfig struct {
	Name          string       `yaml:"name"`
	BaseURL       string       `yaml:"base_url"`
	Visibility    Visibility   `yaml:"visibility"`
	MaxConcurrent int          `yaml:"max_concurrent"`
	Models        []ModelEntry `yaml:"models"`
	APIKeySecret  string       `yaml:"api_key_secret,omitempty"`
}

type Privacy struct {
	Mode string `yaml:"mode"` // need_to_know | private_only
}

type Defaults struct {
	MaxContextTokens  int           `yaml:"max_context_tokens"`
	MaxRevisions      int           `yaml:"max_revisions"`
	MaxCoverageRounds int           `yaml:"max_coverage_rounds"`
	CallTimeout       time.Duration `yaml:"call_timeout"`
	MaxWaitMinutes    int           `yaml:"max_wait_minutes"`
}

type RateLimits struct {
	CooldownAfter429Seconds int `yaml:"cooldown_after_429_seconds"`
	BackoffInitialSeconds   int `yaml:"backoff_initial_seconds"`
	BackoffMaxSeconds       int `yaml:"backoff_max_seconds"`
	BackoffMultiplier       int `yaml:"backoff_multiplier"`
}

type Human struct {
	Mode string `yaml:"mode"` // terminal | file
}

type Vault struct {
	Path string `yaml:"path"`
}

// Config is the whole gophermind.yaml.
type Config struct {
	Providers  []ProviderConfig    `yaml:"providers"`
	Models     map[string][]string `yaml:"models"`
	Privacy    Privacy             `yaml:"privacy"`
	Defaults   Defaults            `yaml:"defaults"`
	RateLimits RateLimits          `yaml:"rate_limits"`
	Human      Human               `yaml:"human"`
	Vault      Vault               `yaml:"vault"`
}

// Default is what a first run writes: the Mac mini plus two providers that
// need no key.
func Default() *Config {
	return &Config{
		Providers: []ProviderConfig{
			{Name: "mini", BaseURL: "http://192.168.1.35:11434/v1", Visibility: Private, MaxConcurrent: 1,
				Models: []ModelEntry{{ID: "qwen3.6:35b-a3b", ContextTokens: 32768}}},
			{Name: "kilo", BaseURL: "https://api.kilo.ai/api/gateway", Visibility: Public, MaxConcurrent: 2,
				Models: []ModelEntry{{ID: "kilo-auto/free", ContextTokens: 131072}}},
			{Name: "ovh", BaseURL: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1", Visibility: Public, MaxConcurrent: 1,
				Models: []ModelEntry{{ID: "Qwen3.6-27B", ContextTokens: 131072}}},
		},
		Models: map[string][]string{
			"strong":   {"mini/qwen3.6:35b-a3b"},
			"standard": {"mini/qwen3.6:35b-a3b", "kilo/kilo-auto/free"},
			"any":      {"kilo/kilo-auto/free", "ovh/Qwen3.6-27B", "mini/qwen3.6:35b-a3b"},
		},
		Privacy: Privacy{Mode: "need_to_know"},
		Defaults: Defaults{MaxContextTokens: 8000, MaxRevisions: 2, MaxCoverageRounds: 2,
			CallTimeout: 10 * time.Minute, MaxWaitMinutes: 30},
		RateLimits: RateLimits{CooldownAfter429Seconds: 60, BackoffInitialSeconds: 5, BackoffMaxSeconds: 300, BackoffMultiplier: 2},
		Human:      Human{Mode: "terminal"},
		Vault:      Vault{Path: "~/.gophermind/vault.age"},
	}
}

// Path is ~/.gophermind/gophermind.yaml, or under GOPHERMIND_CONFIG_DIR.
func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind.yaml"), nil
}

const header = "# GopherMind v2 settings. A provider's key is never written here: api_key_secret names\n" +
	"# an entry in the vault (gophermind brief vault set <name>).\n"

// Load reads path. When the file does not exist it writes the defaults there
// (file mode 0600, folder 0700) and returns them. Unknown fields are an error,
// so a pasted "api_key: ..." line is refused rather than silently kept.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeDefaults(path)
	}
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	return &c, nil
}

func writeDefaults(path string) (*Config, error) {
	c := Default()
	body, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return Load(path) // another process created it first
	}
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	_, werr := f.WriteString(header + string(body))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, fmt.Errorf("settings: %w", werr)
	}
	return c, nil
}

// SplitEntry splits a chain entry "provider/model" at the first slash, so
// "kilo/kilo-auto/free" is provider kilo, model kilo-auto/free.
func SplitEntry(entry string) (providerName, model string, ok bool) {
	i := strings.Index(entry, "/")
	if i <= 0 || i == len(entry)-1 {
		return "", "", false
	}
	return entry[:i], entry[i+1:], true
}

// Validate checks everything Load and the router rely on.
func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return errors.New("providers: at least one provider is required")
	}
	byName := map[string]ProviderConfig{}
	for i, p := range c.Providers {
		where := fmt.Sprintf("providers[%d]", i)
		if p.Name == "" {
			return fmt.Errorf("%s: name is required", where)
		}
		if strings.Contains(p.Name, "/") {
			return fmt.Errorf("%s: name %q must not contain a slash", where, p.Name)
		}
		if _, dup := byName[p.Name]; dup {
			return fmt.Errorf("providers: duplicate provider name %q", p.Name)
		}
		byName[p.Name] = p
		where = "provider " + p.Name
		if p.BaseURL == "" {
			return fmt.Errorf("%s: base_url is required", where)
		}
		if p.Visibility != Private && p.Visibility != Public {
			return fmt.Errorf("%s: visibility must be private or public, got %q", where, p.Visibility)
		}
		if p.MaxConcurrent < 1 {
			return fmt.Errorf("%s: max_concurrent must be at least 1, got %d", where, p.MaxConcurrent)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("%s: at least one model is required", where)
		}
		seen := map[string]bool{}
		for _, m := range p.Models {
			if m.ID == "" {
				return fmt.Errorf("%s: a model has no id", where)
			}
			if seen[m.ID] {
				return fmt.Errorf("%s: duplicate model %q", where, m.ID)
			}
			seen[m.ID] = true
			if m.ContextTokens < 1 {
				return fmt.Errorf("%s: model %s: context_tokens must be at least 1, got %d", where, m.ID, m.ContextTokens)
			}
		}
	}
	for tier := range c.Models {
		if tier != "strong" && tier != "standard" && tier != "any" {
			return fmt.Errorf("models: unknown tier %q (want strong, standard, or any)", tier)
		}
	}
	for _, tier := range Tiers {
		chain := c.Models[tier]
		if len(chain) == 0 {
			return fmt.Errorf("models: tier %s has no entries", tier)
		}
		for _, entry := range chain {
			if _, ok := c.ModelInfo(entry); !ok {
				return fmt.Errorf("models: tier %s: %q is not a configured provider/model", tier, entry)
			}
		}
	}
	if c.Privacy.Mode != "need_to_know" && c.Privacy.Mode != "private_only" {
		return fmt.Errorf("privacy.mode must be need_to_know or private_only, got %q", c.Privacy.Mode)
	}
	d := c.Defaults
	switch {
	case d.MaxContextTokens < 1:
		return fmt.Errorf("defaults.max_context_tokens must be at least 1, got %d", d.MaxContextTokens)
	case d.MaxRevisions < 0:
		return fmt.Errorf("defaults.max_revisions must not be negative, got %d", d.MaxRevisions)
	case d.MaxCoverageRounds < 0:
		return fmt.Errorf("defaults.max_coverage_rounds must not be negative, got %d", d.MaxCoverageRounds)
	case d.CallTimeout <= 0:
		return fmt.Errorf("defaults.call_timeout must be positive, got %v", d.CallTimeout)
	case d.MaxWaitMinutes < 0:
		return fmt.Errorf("defaults.max_wait_minutes must not be negative, got %d", d.MaxWaitMinutes)
	}
	r := c.RateLimits
	switch {
	case r.CooldownAfter429Seconds < 1:
		return fmt.Errorf("rate_limits.cooldown_after_429_seconds must be at least 1, got %d", r.CooldownAfter429Seconds)
	case r.BackoffInitialSeconds < 1:
		return fmt.Errorf("rate_limits.backoff_initial_seconds must be at least 1, got %d", r.BackoffInitialSeconds)
	case r.BackoffMaxSeconds < r.BackoffInitialSeconds:
		return fmt.Errorf("rate_limits.backoff_max_seconds (%d) must be at least backoff_initial_seconds (%d)", r.BackoffMaxSeconds, r.BackoffInitialSeconds)
	case r.BackoffMultiplier < 1:
		return fmt.Errorf("rate_limits.backoff_multiplier must be at least 1, got %d", r.BackoffMultiplier)
	}
	if c.Human.Mode != "terminal" && c.Human.Mode != "file" {
		return fmt.Errorf("human.mode must be terminal or file, got %q", c.Human.Mode)
	}
	if c.Vault.Path == "" {
		return errors.New("vault.path is required")
	}
	return nil
}

// Visibility reports a provider's visibility; ok is false for an unknown name.
func (c *Config) Visibility(providerName string) (Visibility, bool) {
	for _, p := range c.Providers {
		if p.Name == providerName {
			return p.Visibility, true
		}
	}
	return "", false
}

// ModelInfo resolves a chain entry "provider/model" to the model's info.
func (c *Config) ModelInfo(entry string) (provider.ModelInfo, bool) {
	name, model, ok := SplitEntry(entry)
	if !ok {
		return provider.ModelInfo{}, false
	}
	for _, p := range c.Providers {
		if p.Name != name {
			continue
		}
		for _, m := range p.Models {
			if m.ID == model {
				return provider.ModelInfo{ID: m.ID, ContextTokens: m.ContextTokens}, true
			}
		}
	}
	return provider.ModelInfo{}, false
}
```

- [ ] **Step 4: Implement `providers.go`**

```go
package settings

import (
	"fmt"
	"net/http"

	"gophermind/gophermind-lib/briefv2/provider"
)

// BuildProviders constructs one OpenAI-compatible provider per configured
// entry. A provider with api_key_secret gets its key from secret(name); the
// value goes to the provider and nowhere else, and it never appears in an
// error. client is the HTTP client every provider uses; nil means
// http.DefaultClient (the harness proxy plan replaces this).
func (c *Config) BuildProviders(client *http.Client, secret func(name string) (string, error)) (map[string]provider.Provider, error) {
	if client == nil {
		client = http.DefaultClient
	}
	out := make(map[string]provider.Provider, len(c.Providers))
	for _, p := range c.Providers {
		key := ""
		if p.APIKeySecret != "" {
			if secret == nil {
				return nil, fmt.Errorf("settings: provider %s needs the secret %q but no secret source was given", p.Name, p.APIKeySecret)
			}
			v, err := secret(p.APIKeySecret)
			if err != nil {
				return nil, fmt.Errorf("settings: provider %s: secret %q: %w", p.Name, p.APIKeySecret, err)
			}
			key = v
		}
		models := make([]provider.ModelInfo, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, provider.ModelInfo{ID: m.ID, ContextTokens: m.ContextTokens})
		}
		out[p.Name] = provider.NewOpenAI(provider.Config{
			Name: p.Name, BaseURL: p.BaseURL, APIKey: key, MaxConcurrent: p.MaxConcurrent,
			HTTPClient: client, Models: models,
		})
	}
	return out, nil
}
```

- [ ] **Step 5: Run and commit**

Run: `cd gophermind-lib && gofmt -l briefv2/settings && go vet ./briefv2/settings/ && go test ./briefv2/settings/ -race -v 2>&1 | tail -20`
Expected: `gofmt` prints nothing; PASS (10 test functions).

```bash
git add gophermind-lib/briefv2/settings
git commit -m "feat(briefv2): gophermind.yaml settings with validation and provider construction" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The router (fallback chains, cooldowns, privacy)

**Files:**
- Create: `gophermind-lib/briefv2/router/errors.go`, `privacy.go`, `router.go`, `attempt.go`, `parsed.go`, `router_test.go`

**Interfaces:**
- Consumes: `settings.Config`, `settings.SplitEntry`, `settings.Private` (Task 4); `provider.Provider`, `provider.Request`, `provider.Response`, the `provider.Err*` value types (Task 3); `ledger.Ledger`, `ledger.Call`, `ledger.Outcome*`, `ledger.Digest` (Task 2); `events.Sink`, `events.Event`, `events.Kind*`, `events.Nop` (Task 3).
- Produces: `router.Tier` (`TierStrong`, `TierStandard`, `TierAny`); `router.Scope` (`ScopeBrief`, `ScopeComponent`, `ScopeNode`).
- Produces: `router.CallInfo{RunID, Stage, NodeID string; Tier Tier; Scope Scope; Revision int; TaskType, NodeClass string; Exclude []string; Only string}` (an empty `TaskType` is recorded as the stage up to its first colon); `router.Result{provider.Response; CallID int64; Entry string; ChainPos int}`.
- Produces: `router.New(cfg *settings.Config, providers map[string]provider.Provider, led ledger.Ledger, sink events.Sink, opts ...Option) *Router`; options `WithAllowPublic(bool)`, `WithSleep(func(context.Context, time.Duration) error)`, `WithNow(func() time.Time)`.
- Produces: `(*Router).Call(ctx, CallInfo, provider.Request) (Result, error)`; `(*Router).CallParsed(ctx, CallInfo, provider.Request, parse func(text string) error) (Result, error)`; `(*Router).LedgerErrors() int`.
- Produces: `*router.ChainExhausted{Tier Tier; Reasons []EntryReason; ParseErr error}` with `OnlyPrivacy() bool` and `Unwrap()` returning `ParseErr`; `router.EntryReason{Entry, Kind, Detail string}`; reason kinds `ReasonExcluded`, `ReasonNoProvider`, `ReasonModelMissing`, `ReasonAuth`, `ReasonPrivacy`, `ReasonCooldown`, `ReasonTooLong`, `ReasonFailed`.

How a call behaves (spec sections 5 to 7). Every entry of `models[tier]` is tried in order. An entry is skipped, without a ledger row, when it is excluded by the caller, its provider is missing, disabled for the run by an auth failure, in cooldown, or not allowed to see the call's scope. An entry whose model window cannot hold the prompt (bytes/4 plus 10 percent, plus `MaxTokens`) is skipped with one `too_long` row. Every attempt that reaches a provider writes exactly one ledger row. A 429 puts the provider in cooldown for `Retry-After` or `cooldown_after_429_seconds`. A transient failure retries the same model up to 3 times with exponential backoff, then the provider rests like a rate-limited one. When the only thing blocking the chain is cooldowns, the router waits for the shortest one, up to `defaults.max_wait_minutes` in total. Cancellation of the caller's context records a row with `error_kind` `cancelled`. A failed ledger write is reported as an event and counted; the model's answer is still returned.

- [ ] **Step 1: Write the failing tests** (`briefv2/router/router_test.go`)

The tests use a real ledger in a temporary SQLite file, three scripted fake providers (`mini` private, `kilo` and `ovh` public), and a fake clock whose `sleep` advances time, so cooldowns and backoff run instantly.

```go
package router_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

// clock is a fake clock: sleeping advances it and records the request.
type clock struct {
	mu    sync.Mutex
	t     time.Time
	slept []time.Duration
}

func newClock() *clock { return &clock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}
func (c *clock) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
	c.mu.Unlock()
	return nil
}
func (c *clock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// testConfig has one private provider (mini) and two public ones (kilo, ovh).
func testConfig() *settings.Config {
	c := settings.Default()
	c.Providers = []settings.ProviderConfig{
		{Name: "mini", BaseURL: "http://mini", Visibility: settings.Private, MaxConcurrent: 1, Models: []settings.ModelEntry{{ID: "qwen", ContextTokens: 1000}}},
		{Name: "kilo", BaseURL: "http://kilo", Visibility: settings.Public, MaxConcurrent: 2, Models: []settings.ModelEntry{{ID: "auto", ContextTokens: 100000}}},
		{Name: "ovh", BaseURL: "http://ovh", Visibility: settings.Public, MaxConcurrent: 1, Models: []settings.ModelEntry{{ID: "q27", ContextTokens: 100000}}},
	}
	c.Models = map[string][]string{
		"strong":   {"mini/qwen"},
		"standard": {"mini/qwen", "kilo/auto"},
		"any":      {"kilo/auto", "ovh/q27", "mini/qwen"},
	}
	c.Defaults.CallTimeout = 2 * time.Second
	c.Defaults.MaxWaitMinutes = 5
	return c
}

type script = func(call int, req provider.Request) (provider.Response, error)

func okText(text string) script {
	return func(_ int, req provider.Request) (provider.Response, error) {
		return provider.Response{Text: text, Model: req.Model, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5}}, nil
	}
}

type rig struct {
	r               *router.Router
	led             *ledger.SQLite
	db              *sql.DB
	path            string
	sink            *events.Collector
	clk             *clock
	mini, kilo, ovh *provider.Fake
}

// newRig wires a router over three fakes and a real ledger in a temp database.
// A nil script answers "ok".
func newRig(t *testing.T, mini, kilo, ovh script, mutate func(*settings.Config), opts ...router.Option) *rig {
	t.Helper()
	cfg := testConfig()
	if mutate != nil {
		mutate(cfg)
	}
	for _, s := range []*script{&mini, &kilo, &ovh} {
		if *s == nil {
			*s = okText("ok")
		}
	}
	g := &rig{
		mini: provider.NewFake("mini", []provider.ModelInfo{{ID: "qwen", ContextTokens: 1000}}, mini),
		kilo: provider.NewFake("kilo", []provider.ModelInfo{{ID: "auto", ContextTokens: 100000}}, kilo),
		ovh:  provider.NewFake("ovh", []provider.ModelInfo{{ID: "q27", ContextTokens: 100000}}, ovh),
	}
	g.build(t, cfg, map[string]provider.Provider{"mini": g.mini, "kilo": g.kilo, "ovh": g.ovh}, nil, opts...)
	return g
}

// build finishes a rig: real ledger unless ledOverride is given.
func (g *rig) build(t *testing.T, cfg *settings.Config, providers map[string]provider.Provider, ledOverride ledger.Ledger, opts ...router.Option) {
	t.Helper()
	g.path = filepath.Join(t.TempDir(), "bb.db")
	d, err := db.Open(g.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	g.db, g.led = d, ledger.NewSQLite(d)
	g.sink, g.clk = events.NewCollector(), newClock()
	var led ledger.Ledger = g.led
	if ledOverride != nil {
		led = ledOverride
	}
	all := append([]router.Option{router.WithNow(g.clk.now), router.WithSleep(g.clk.sleep)}, opts...)
	g.r = router.New(cfg, providers, led, g.sink, all...)
}

func info(tier router.Tier, scope router.Scope) router.CallInfo {
	return router.CallInfo{RunID: "r", Stage: "testwrite:fn-a", NodeID: "fn-a", Tier: tier, Scope: scope}
}

func req(text string) provider.Request {
	return provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: text}}}
}

func (g *rig) rows(t *testing.T) []ledger.Call {
	t.Helper()
	rows, err := g.led.List(context.Background(), "r", ledger.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestFailsOverInChainOrderAndWritesOneRowPerAttempt(t *testing.T) {
	g := newRig(t, nil,
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: 30 * time.Second}
		},
		okText("from ovh"), nil)
	res, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "from ovh" || res.Entry != "ovh/q27" || res.ChainPos != 2 {
		t.Errorf("result = %+v", res)
	}
	if g.kilo.Calls() != 1 || g.ovh.Calls() != 1 || g.mini.Calls() != 0 {
		t.Errorf("calls: kilo %d ovh %d mini %d", g.kilo.Calls(), g.ovh.Calls(), g.mini.Calls())
	}
	rows := g.rows(t)
	if len(rows) != 2 {
		t.Fatalf("%d ledger rows, want 2", len(rows))
	}
	if rows[0].Provider != "kilo" || rows[0].Outcome != ledger.OutcomeRateLimited || rows[0].RetryAfterS != 30 || rows[0].ChainPos != 1 {
		t.Errorf("first row = %+v", rows[0])
	}
	if rows[1].Provider != "ovh" || rows[1].Outcome != ledger.OutcomeOK || rows[1].ModelServed != "q27" || rows[1].PromptTokens != 10 ||
		rows[1].Scope != "node" || rows[1].Tier != "any" || rows[1].Stage != "testwrite:fn-a" || rows[1].NodeID != "fn-a" {
		t.Errorf("second row = %+v", rows[1])
	}
	if res.CallID != rows[1].ID {
		t.Errorf("CallID %d is not the answering row %d", res.CallID, rows[1].ID)
	}
	if n := len(g.sink.OfKind(events.KindCall)); n != 2 {
		t.Errorf("%d call events, want 2", n)
	}
}

func TestCooldownHoldsAndThenExpires(t *testing.T) {
	g := newRig(t, nil, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: 30 * time.Second}
		}
		return provider.Response{Text: "kilo", Model: r.Model}, nil
	}, nil, nil)
	ctx := context.Background()
	call := func() router.Result {
		res, err := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call(); res.Entry != "ovh/q27" {
		t.Fatalf("first call answered by %s", res.Entry)
	}
	g.clk.advance(10 * time.Second)
	if res := call(); res.Entry != "ovh/q27" || g.kilo.Calls() != 1 {
		t.Errorf("during the cooldown: %s, kilo calls %d", res.Entry, g.kilo.Calls())
	}
	g.clk.advance(21 * time.Second)
	if res := call(); res.Entry != "kilo/auto" || g.kilo.Calls() != 2 {
		t.Errorf("after the cooldown: %s, kilo calls %d", res.Entry, g.kilo.Calls())
	}
}

func TestRateLimitWithoutRetryAfterUsesTheConfiguredCooldown(t *testing.T) {
	g := newRig(t, nil, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{}
		}
		return provider.Response{Text: "kilo", Model: r.Model}, nil
	}, nil, nil) // cooldown_after_429_seconds is 60
	ctx := context.Background()
	g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
	g.clk.advance(59 * time.Second)
	if res, _ := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi")); res.Entry != "ovh/q27" {
		t.Errorf("at 59s the provider should still be cooling, got %s", res.Entry)
	}
	g.clk.advance(2 * time.Second)
	if res, _ := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi")); res.Entry != "kilo/auto" {
		t.Errorf("at 61s the provider should be back, got %s", res.Entry)
	}
}

func TestAuthFailureDisablesTheProviderForTheRunAndWarnsOnce(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrAuth{Provider: "kilo"}
	}, nil, nil)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		res, err := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
		if err != nil || res.Entry != "ovh/q27" {
			t.Fatalf("call %d: %+v %v", i, res, err)
		}
	}
	if g.kilo.Calls() != 1 {
		t.Errorf("kilo was called %d times after a 401, want 1", g.kilo.Calls())
	}
	if n := len(g.sink.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("%d warnings, want exactly 1", n)
	}
	var auth int
	for _, row := range g.rows(t) {
		if row.Outcome == ledger.OutcomeAuth {
			auth++
		}
	}
	if auth != 1 {
		t.Errorf("%d auth rows, want 1", auth)
	}
}

func TestModelNotFoundSkipsThatEntryForTheRestOfTheRun(t *testing.T) {
	g := newRig(t, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrModelNotFound{Model: "qwen"}
	}, nil, nil, nil)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		res, err := g.r.Call(ctx, info(router.TierStandard, router.ScopeNode), req("hi"))
		if err != nil || res.Entry != "kilo/auto" {
			t.Fatalf("call %d: %+v %v", i, res, err)
		}
	}
	if g.mini.Calls() != 1 {
		t.Errorf("mini was asked %d times after a 404, want 1", g.mini.Calls())
	}
	rows := g.rows(t)
	if rows[0].Outcome != ledger.OutcomeModelMissing {
		t.Errorf("first row = %+v", rows[0])
	}
	if n := len(g.sink.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("%d warnings, want 1", n)
	}
}

func TestTransientFailuresRetryWithBackoffThenMoveOn(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrTransient{Cause: errors.New("boom")}
	}, nil, nil)
	ctx := context.Background()
	res, err := g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
	if err != nil || res.Entry != "ovh/q27" {
		t.Fatalf("%+v %v", res, err)
	}
	if g.kilo.Calls() != 4 {
		t.Errorf("kilo calls = %d, want 1 try plus 3 retries", g.kilo.Calls())
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	if got := g.clk.sleeps(); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("backoff sleeps = %v, want %v", got, want)
	}
	rows := g.rows(t)
	if len(rows) != 5 {
		t.Fatalf("%d rows, want 4 failed attempts and 1 answer", len(rows))
	}
	for _, row := range rows[:4] {
		if row.Provider != "kilo" || row.Outcome != ledger.OutcomeError || row.ErrorKind != "transient" {
			t.Errorf("row = %+v", row)
		}
	}
	// After giving up it is treated like a rate limit: the provider rests.
	g.r.Call(ctx, info(router.TierAny, router.ScopeNode), req("hi"))
	if g.kilo.Calls() != 4 {
		t.Errorf("kilo was tried again during its cooldown (%d calls)", g.kilo.Calls())
	}
}

func TestTransientFailureThatRecoversAnswersFromTheSameModel(t *testing.T) {
	g := newRig(t, nil, func(call int, r provider.Request) (provider.Response, error) {
		if call < 3 {
			return provider.Response{}, provider.ErrTransient{Cause: errors.New("reset")}
		}
		return provider.Response{Text: "third time", Model: r.Model}, nil
	}, nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi"))
	if err != nil || res.Entry != "kilo/auto" || res.Text != "third time" || g.kilo.Calls() != 3 {
		t.Fatalf("%+v %v calls=%d", res, err, g.kilo.Calls())
	}
	if len(g.rows(t)) != 3 {
		t.Errorf("%d rows, want 3", len(g.rows(t)))
	}
}

func TestBackoffIsCappedAtTheConfiguredMaximum(t *testing.T) {
	g := newRig(t, nil, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrTransient{Cause: errors.New("boom")}
	}, nil, func(c *settings.Config) { c.RateLimits.BackoffMaxSeconds = 8 })
	g.r.Call(context.Background(), info(router.TierAny, router.ScopeNode), req("hi"))
	got := g.clk.sleeps()
	if len(got) != 3 || got[0] != 5*time.Second || got[1] != 8*time.Second || got[2] != 8*time.Second {
		t.Errorf("sleeps = %v, want [5s 8s 8s]", got)
	}
}

func TestPromptTooLongIsSkippedAndRecordedWithoutCallingTheModel(t *testing.T) {
	cases := []struct {
		name      string
		promptLen int
		maxTokens int
		fits      bool
	}{
		{"small prompt", 100, 0, true},
		{"reserve pushes it over", 100, 990, false},
		{"reserve fits", 100, 900, true},
		{"huge prompt", 8000, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, nil, nil, nil, nil) // mini holds 1000 tokens
			r := req(strings.Repeat("x", c.promptLen))
			r.MaxTokens = c.maxTokens
			res, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), r)
			if c.fits {
				if err != nil || res.Entry != "mini/qwen" {
					t.Fatalf("%+v %v", res, err)
				}
				return
			}
			var ce *router.ChainExhausted
			if !errors.As(err, &ce) || ce.Reasons[0].Kind != router.ReasonTooLong {
				t.Fatalf("err = %v, want too_long", err)
			}
			if g.mini.Calls() != 0 {
				t.Error("the model was called with a prompt that cannot fit")
			}
			rows := g.rows(t)
			if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeTooLong || rows[0].PromptBytes != c.promptLen+1 {
				t.Errorf("rows = %+v", rows)
			}
		})
	}
}

func TestBigPromptFallsThroughToALargerModel(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req(strings.Repeat("x", 8000)))
	if err != nil || res.Entry != "kilo/auto" || res.ChainPos != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if g.mini.Calls() != 0 {
		t.Error("mini was called with a prompt larger than its window")
	}
}

func TestExhaustionExplainsEveryEntry(t *testing.T) {
	g := newRig(t,
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrAuth{Provider: "mini"}
		},
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: time.Hour}
		}, nil, nil)
	_, err := g.r.Call(context.Background(), info(router.TierStandard, router.ScopeNode), req("hi"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if ce.Tier != router.TierStandard || len(ce.Reasons) != 2 ||
		ce.Reasons[0].Entry != "mini/qwen" || ce.Reasons[0].Kind != router.ReasonAuth ||
		ce.Reasons[1].Entry != "kilo/auto" || ce.Reasons[1].Kind != router.ReasonCooldown {
		t.Errorf("reasons = %+v", ce.Reasons)
	}
	if msg := err.Error(); !strings.Contains(msg, "mini/qwen") || !strings.Contains(msg, "kilo/auto") || !strings.Contains(msg, "standard") {
		t.Errorf("message = %q", msg)
	}
	if len(g.clk.sleeps()) != 0 {
		t.Errorf("waited %v for a cooldown longer than max_wait_minutes", g.clk.sleeps())
	}
}

func TestWaitsWhenOnlyACooldownBlocks(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{}, provider.ErrRateLimited{RetryAfter: 30 * time.Second}
		}
		return provider.Response{Text: "after the wait", Model: r.Model}, nil
	}, nil, nil, nil)
	res, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	if err != nil || res.Text != "after the wait" {
		t.Fatalf("%+v %v", res, err)
	}
	if got := g.clk.sleeps(); len(got) != 1 || got[0] != 30*time.Second {
		t.Errorf("sleeps = %v, want [30s]", got)
	}
	if g.mini.Calls() != 2 || len(g.rows(t)) != 2 {
		t.Errorf("mini calls %d, rows %d; want 2 and 2", g.mini.Calls(), len(g.rows(t)))
	}
	if n := len(g.sink.OfKind(events.KindWarning)); n != 1 {
		t.Errorf("%d warnings about waiting, want 1", n)
	}
}

func TestStopsWaitingAtMaxWaitMinutes(t *testing.T) {
	g := newRig(t, func(int, provider.Request) (provider.Response, error) {
		return provider.Response{}, provider.ErrRateLimited{RetryAfter: 4 * time.Minute}
	}, nil, nil, nil)
	_, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	// One 4 minute wait fits in max_wait_minutes (5); a second one would not.
	if got := g.clk.sleeps(); len(got) != 1 || got[0] != 4*time.Minute {
		t.Errorf("sleeps = %v, want one 4m wait", got)
	}
	if g.mini.Calls() != 2 {
		t.Errorf("mini calls = %d, want 2", g.mini.Calls())
	}
}

func TestPublicProvidersAreOnlyEverAskedWhatThePrivacyRuleAllows(t *testing.T) {
	cases := []struct {
		name        string
		mode        string
		allowPublic bool
		scope       router.Scope
		publicOK    bool
	}{
		{"need_to_know, brief", "need_to_know", false, router.ScopeBrief, false},
		{"need_to_know, component", "need_to_know", false, router.ScopeComponent, false},
		{"need_to_know, node", "need_to_know", false, router.ScopeNode, true},
		{"allow-public, brief", "need_to_know", true, router.ScopeBrief, true},
		{"allow-public, component", "need_to_know", true, router.ScopeComponent, true},
		{"private_only, node", "private_only", false, router.ScopeNode, false},
		{"private_only, node, allow-public", "private_only", true, router.ScopeNode, false},
		{"private_only, brief, allow-public", "private_only", true, router.ScopeBrief, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, nil, nil, nil, func(cfg *settings.Config) { cfg.Privacy.Mode = c.mode },
				router.WithAllowPublic(c.allowPublic))
			res, err := g.r.Call(context.Background(), info(router.TierAny, c.scope), req("the whole brief"))
			if err != nil {
				t.Fatal(err)
			}
			public := g.kilo.Calls() + g.ovh.Calls()
			if c.publicOK {
				if public == 0 || res.Entry != "kilo/auto" {
					t.Errorf("public providers were allowed but were not used: entry %s, hits %d", res.Entry, public)
				}
				return
			}
			if public != 0 {
				t.Errorf("a public provider received %d request(s) it must never see", public)
			}
			if res.Entry != "mini/qwen" {
				t.Errorf("answered by %s, want the private mini", res.Entry)
			}
		})
	}
}

func TestEveryEntryFilteredByPrivacyIsReportedAsSuch(t *testing.T) {
	g := newRig(t, nil, nil, nil, func(c *settings.Config) { c.Models["any"] = []string{"kilo/auto", "ovh/q27"} })
	_, err := g.r.Call(context.Background(), info(router.TierAny, router.ScopeBrief), req("the whole brief"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || !ce.OnlyPrivacy() {
		t.Fatalf("err = %v, want a ChainExhausted where only privacy blocked", err)
	}
	if g.kilo.Calls()+g.ovh.Calls() != 0 {
		t.Error("a public provider was called")
	}
	if len(g.rows(t)) != 0 {
		t.Error("skipping for privacy is not an attempt and must not write a ledger row")
	}
}

func TestCallsThatDoNotDeclareThemselvesAreRefused(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	ctx := context.Background()
	bad := []router.CallInfo{
		{RunID: "r", Stage: "s", Tier: router.TierAny},                      // no scope
		{RunID: "r", Stage: "s", Tier: router.TierAny, Scope: "everything"}, // unknown scope
		{RunID: "r", Stage: "s", Tier: "huge", Scope: router.ScopeNode},     // unknown tier
		{Stage: "s", Tier: router.TierAny, Scope: router.ScopeNode},         // no run
		{RunID: "r", Tier: router.TierAny, Scope: router.ScopeNode},         // no stage
	}
	for _, i := range bad {
		if _, err := g.r.Call(ctx, i, req("hi")); err == nil {
			t.Errorf("Call accepted %+v", i)
		}
	}
	if g.mini.Calls()+g.kilo.Calls()+g.ovh.Calls() != 0 {
		t.Error("a refused call reached a provider")
	}
}

// Every row says what kind of work the call was. A call that does not say
// gets the stage up to its first colon; a leaf call also carries its class.
func TestLedgerRowsCarryTaskTypeAndNodeClass(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	ctx := context.Background()
	leaf := info(router.TierStrong, router.ScopeNode)
	leaf.NodeClass = "validation"
	if _, err := g.r.Call(ctx, leaf, req("a")); err != nil {
		t.Fatal(err)
	}
	fill := router.CallInfo{RunID: "r", Stage: "coverage_fill", TaskType: "coverage", Tier: router.TierStrong, Scope: router.ScopeBrief}
	if _, err := g.r.Call(ctx, fill, req("b")); err != nil {
		t.Fatal(err)
	}
	rows := g.rows(t)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].TaskType != "testwrite" || rows[0].NodeClass != "validation" {
		t.Errorf("leaf row = task %q class %q, want testwrite and validation", rows[0].TaskType, rows[0].NodeClass)
	}
	if rows[1].TaskType != "coverage" || rows[1].NodeClass != "" {
		t.Errorf("fill row = task %q class %q, want coverage and no class", rows[1].TaskType, rows[1].NodeClass)
	}
	sum, err := g.led.Summary(ctx, "r")
	if err != nil || len(sum) != 2 {
		t.Fatalf("summary = %d groups, %v; want 2 (one per task type)", len(sum), err)
	}
}

func TestOnlyAndExcludeSteerTheWalk(t *testing.T) {
	g := newRig(t, nil, nil, nil, nil)
	ctx := context.Background()
	i := info(router.TierAny, router.ScopeNode)
	i.Only = "ovh/q27"
	if res, err := g.r.Call(ctx, i, req("hi")); err != nil || res.Entry != "ovh/q27" {
		t.Errorf("Only: %+v %v", res, err)
	}
	i = info(router.TierAny, router.ScopeNode)
	i.Exclude = []string{"kilo/auto", "ovh/q27"}
	if res, err := g.r.Call(ctx, i, req("hi")); err != nil || res.Entry != "mini/qwen" {
		t.Errorf("Exclude: %+v %v", res, err)
	}
	i.Exclude = []string{"kilo/auto", "ovh/q27", "mini/qwen"}
	var ce *router.ChainExhausted
	if _, err := g.r.Call(ctx, i, req("hi")); !errors.As(err, &ce) || ce.Reasons[0].Kind != router.ReasonExcluded {
		t.Errorf("excluding everything: %v", err)
	}
}

func TestNoPromptOrReplyTextReachesTheDatabase(t *testing.T) {
	const canaryPrompt, canaryReply = "CANARY-PROMPT-8c41d2", "CANARY-REPLY-19ab73"
	g := newRig(t,
		func(int, provider.Request) (provider.Response, error) {
			return provider.Response{}, provider.ErrTransient{Cause: errors.New("boom")}
		},
		okText(canaryReply), nil, nil)
	ctx := context.Background()
	if _, err := g.r.Call(ctx, info(router.TierStandard, router.ScopeNode), req(canaryPrompt)); err != nil {
		t.Fatal(err)
	}
	if _, err := g.r.CallParsed(ctx, info(router.TierStandard, router.ScopeNode), req(canaryPrompt), func(string) error {
		return errors.New("the reply " + canaryReply + " is not JSON")
	}); err == nil {
		t.Fatal("a parser that rejects everything should exhaust the chain")
	}
	rs, err := g.db.Query(`SELECT * FROM calls`)
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := rs.Columns()
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rs.Scan(ptrs...)
		for i, v := range vals {
			var s string
			switch x := v.(type) {
			case string:
				s = x
			case []byte:
				s = string(x)
			}
			if strings.Contains(s, canaryPrompt) || strings.Contains(s, canaryReply) {
				t.Errorf("column %s holds prompt or reply text: %q", cols[i], s)
			}
		}
	}
	rs.Close()
	g.db.Close() // checkpoints the write-ahead log
	for _, f := range []string{g.path, g.path + "-wal"} {
		if b, err := os.ReadFile(f); err == nil && (strings.Contains(string(b), canaryPrompt) || strings.Contains(string(b), canaryReply)) {
			t.Errorf("%s contains prompt or reply text", f)
		}
	}
}

var errBad = errors.New("not JSON")

func rejectBad(text string) error {
	if text == "bad" {
		return errBad
	}
	return nil
}

func TestCallParsedRetriesTheSameModelOnceWithTheParseError(t *testing.T) {
	g := newRig(t, func(call int, r provider.Request) (provider.Response, error) {
		if call == 1 {
			return provider.Response{Text: "bad", Model: r.Model}, nil
		}
		return provider.Response{Text: "good", Model: r.Model}, nil
	}, nil, nil, nil)
	res, err := g.r.CallParsed(context.Background(), info(router.TierStandard, router.ScopeNode), req("write JSON"), rejectBad)
	if err != nil || res.Text != "good" || res.Entry != "mini/qwen" {
		t.Fatalf("%+v %v", res, err)
	}
	if g.mini.Calls() != 2 || g.kilo.Calls() != 0 {
		t.Errorf("mini %d kilo %d, want 2 and 0", g.mini.Calls(), g.kilo.Calls())
	}
	second := g.mini.Requests()[1].Messages
	if len(second) != 3 || second[1].Role != provider.RoleAssistant || second[1].Content != "bad" ||
		second[2].Role != provider.RoleUser || !strings.Contains(second[2].Content, "not JSON") {
		t.Errorf("retry request = %+v", second)
	}
	rows := g.rows(t)
	if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeMalformed || rows[0].ErrorKind != "malformed" || rows[1].Outcome != ledger.OutcomeOK {
		t.Errorf("rows = %+v", rows)
	}
}

func TestCallParsedExcludesAModelThatKeepsReturningGarbage(t *testing.T) {
	g := newRig(t, okText("bad"), okText("good"), nil, nil)
	res, err := g.r.CallParsed(context.Background(), info(router.TierStandard, router.ScopeNode), req("write JSON"), rejectBad)
	if err != nil || res.Entry != "kilo/auto" {
		t.Fatalf("%+v %v", res, err)
	}
	if g.mini.Calls() != 2 || g.kilo.Calls() != 1 {
		t.Errorf("mini %d kilo %d, want 2 and 1", g.mini.Calls(), g.kilo.Calls())
	}
	var outcomes []ledger.Outcome
	for _, row := range g.rows(t) {
		outcomes = append(outcomes, row.Outcome)
	}
	want := []ledger.Outcome{ledger.OutcomeMalformed, ledger.OutcomeMalformed, ledger.OutcomeOK}
	if len(outcomes) != 3 || outcomes[0] != want[0] || outcomes[1] != want[1] || outcomes[2] != want[2] {
		t.Errorf("outcomes = %v, want %v", outcomes, want)
	}
}

func TestCallParsedReportsTheLastParseErrorWhenEveryModelFails(t *testing.T) {
	g := newRig(t, okText("bad"), okText("bad"), nil, nil)
	_, err := g.r.CallParsed(context.Background(), info(router.TierStandard, router.ScopeNode), req("write JSON"), rejectBad)
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || !errors.Is(err, errBad) {
		t.Fatalf("err = %v, want a ChainExhausted wrapping the parse error", err)
	}
	if g.mini.Calls() != 2 || g.kilo.Calls() != 2 {
		t.Errorf("mini %d kilo %d, want 2 each", g.mini.Calls(), g.kilo.Calls())
	}
	for _, row := range g.rows(t) {
		if row.Outcome != ledger.OutcomeMalformed {
			t.Errorf("row %+v should be malformed", row)
		}
	}
}

func TestCallParsedMakesOneCallWhenTheFirstReplyParses(t *testing.T) {
	g := newRig(t, okText("good"), nil, nil, nil)
	if _, err := g.r.CallParsed(context.Background(), info(router.TierStrong, router.ScopeNode), req("x"), rejectBad); err != nil {
		t.Fatal(err)
	}
	if g.mini.Calls() != 1 || len(g.rows(t)) != 1 {
		t.Errorf("mini %d, rows %d", g.mini.Calls(), len(g.rows(t)))
	}
}

// blocker waits for its context to end, like a slow model.
type blocker struct {
	started chan struct{}
	once    sync.Once
}

func (b *blocker) Name() string { return "mini" }
func (b *blocker) Models() []provider.ModelInfo {
	return []provider.ModelInfo{{ID: "qwen", ContextTokens: 1000}}
}
func (b *blocker) Complete(ctx context.Context, _ provider.Request) (provider.Response, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
}

func TestCancellationLeavesExactlyOneCancelledRow(t *testing.T) {
	b := &blocker{started: make(chan struct{})}
	g := &rig{}
	g.build(t, testConfig(), map[string]provider.Provider{"mini": b}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := g.r.Call(ctx, info(router.TierStrong, router.ScopeNode), req("hi"))
		done <- err
	}()
	<-b.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	rows := g.rows(t)
	if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeError || rows[0].ErrorKind != "cancelled" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestCallTimeoutIsRecordedAsATimeout(t *testing.T) {
	cfg := testConfig()
	cfg.Defaults.CallTimeout = 20 * time.Millisecond
	b := &blocker{started: make(chan struct{})}
	g := &rig{}
	g.build(t, cfg, map[string]provider.Provider{"mini": b}, nil)
	_, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	var ce *router.ChainExhausted
	if !errors.As(err, &ce) || ce.Reasons[0].Kind != router.ReasonFailed {
		t.Fatalf("err = %v", err)
	}
	rows := g.rows(t)
	if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeTimeout {
		t.Errorf("rows = %+v", rows)
	}
}

// brokenLedger fails every write, like a full disk.
type brokenLedger struct{ ledger.Ledger }

func (brokenLedger) Record(context.Context, *ledger.Call) error { return errors.New("disk full") }

func TestALedgerFailureDoesNotThrowAwayTheModelsAnswer(t *testing.T) {
	g := &rig{}
	cfg := testConfig()
	g.mini = provider.NewFake("mini", nil, okText("precious answer"))
	g.build(t, cfg, map[string]provider.Provider{"mini": g.mini}, brokenLedger{})
	res, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi"))
	if err != nil || res.Text != "precious answer" || res.CallID != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if g.r.LedgerErrors() != 1 {
		t.Errorf("LedgerErrors = %d, want 1", g.r.LedgerErrors())
	}
	if n := len(g.sink.OfKind(events.KindLedgerError)); n != 1 {
		t.Errorf("%d ledger_error events, want 1", n)
	}
}

func TestAProviderNeverRunsMoreCallsAtOnceThanItsMaxConcurrent(t *testing.T) {
	var inflight, peak atomic.Int32
	g := newRig(t, func(_ int, r provider.Request) (provider.Response, error) {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inflight.Add(-1)
		return provider.Response{Text: "ok", Model: r.Model}, nil
	}, nil, nil, nil) // mini has max_concurrent 1
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.r.Call(context.Background(), info(router.TierStrong, router.ScopeNode), req("hi")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Errorf("peak concurrency = %d, want 1", peak.Load())
	}
	if g.mini.Calls() != 8 || len(g.rows(t)) != 8 {
		t.Errorf("calls %d rows %d", g.mini.Calls(), len(g.rows(t)))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/router/ 2>&1 | head -5`
Expected: FAIL, `no non-test Go files`.

- [ ] **Step 3: Implement `errors.go` and `privacy.go`**

`briefv2/router/errors.go`:

```go
package router

import (
	"fmt"
	"strings"
)

// Why one chain entry did not answer.
const (
	ReasonExcluded     = "excluded"      // the caller asked to skip it (after a malformed reply)
	ReasonNoProvider   = "no_provider"   // the entry names a provider that was not built
	ReasonModelMissing = "model_missing" // the provider said the model does not exist
	ReasonAuth         = "auth"          // bad or missing key; disabled for the run
	ReasonPrivacy      = "privacy"       // a public provider may not see this scope
	ReasonCooldown     = "cooldown"      // rate limited or failing; try again later
	ReasonTooLong      = "too_long"      // the prompt does not fit the model
	ReasonFailed       = "failed"        // the call was made and failed (for example a timeout)
)

// EntryReason is one row of a ChainExhausted report.
type EntryReason struct {
	Entry  string // provider/model
	Kind   string // one of the Reason constants
	Detail string
}

// ChainExhausted is returned when no entry of the tier's chain answered. It
// says why each one did not, so the caller can choose between waiting, asking
// a human, or failing with a message that names the setting to change.
type ChainExhausted struct {
	Tier     Tier
	Reasons  []EntryReason
	ParseErr error // the last parse error, when CallParsed excluded entries for malformed replies
}

func (e *ChainExhausted) Error() string {
	parts := make([]string, 0, len(e.Reasons))
	for _, r := range e.Reasons {
		s := r.Entry + ": " + r.Kind
		if r.Detail != "" {
			s += " (" + r.Detail + ")"
		}
		parts = append(parts, s)
	}
	msg := fmt.Sprintf("router: no model answered for tier %s: %s", e.Tier, strings.Join(parts, "; "))
	if e.ParseErr != nil {
		msg += "; last parse error: " + e.ParseErr.Error()
	}
	return msg
}

func (e *ChainExhausted) Unwrap() error { return e.ParseErr }

// OnlyPrivacy reports whether every entry was skipped because a public
// provider may not see this scope. The fix is then --allow-public, a private
// model, or a config edit, not waiting.
func (e *ChainExhausted) OnlyPrivacy() bool {
	if len(e.Reasons) == 0 {
		return false
	}
	for _, r := range e.Reasons {
		if r.Kind != ReasonPrivacy {
			return false
		}
	}
	return true
}
```

`briefv2/router/privacy.go`:

```go
package router

import "gophermind/gophermind-lib/briefv2/settings"

// eligible applies the need-to-know rule (spec section 6).
//
//	private provider                     always
//	privacy.mode private_only            public providers never
//	privacy.mode need_to_know            public providers for node scope only,
//	                                     or for any scope when the run was
//	                                     started with --allow-public
func (r *Router) eligible(providerName string, scope Scope) bool {
	vis, ok := r.cfg.Visibility(providerName)
	if !ok {
		return false
	}
	if vis == settings.Private {
		return true
	}
	if r.cfg.Privacy.Mode == "private_only" {
		return false
	}
	return scope == ScopeNode || r.allowPublic
}
```

- [ ] **Step 4: Implement `router.go`** (types, options, the chain walk, the skip rules)

```go
// Package router sends every model call down a fallback chain: it picks the
// tier's models in order, skips the ones that are cooling down, disabled, too
// small, or not allowed to see the call (the privacy rule), retries transient
// failures, and writes exactly one ledger row per attempt.
package router

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
)

type Tier string

const (
	TierStrong   Tier = "strong"
	TierStandard Tier = "standard"
	TierAny      Tier = "any"
)

// Scope is the widest thing a call carries. The stage fixes it in code; a
// model never chooses it.
type Scope string

const (
	ScopeBrief     Scope = "brief"     // the whole brief
	ScopeComponent Scope = "component" // one component's slice of the brief
	ScopeNode      Scope = "node"      // one function's contract, signatures and tests
)

// CallInfo says who is asking and what the call carries.
type CallInfo struct {
	RunID    string
	Stage    string // clarify, contract, decompose:<component>, coverage, coverage_fill, testwrite:<node>, revise:<node>, implement:<node>
	NodeID   string // empty for run-level stages
	Tier     Tier
	Scope    Scope
	Revision int
	// TaskType is the kind of work: clarify, contract, decompose, coverage,
	// testwrite, revise, implement. Left empty it is the stage up to its first
	// colon, so every ledger row has one.
	TaskType string
	// NodeClass is, for a call about one leaf, the class of function: pure,
	// validation, handler, client, storage, concurrency, wiring, other.
	NodeClass string
	Exclude   []string // provider/model entries to skip, used after a malformed reply
	Only      string   // if set, try only this provider/model entry
}

// Result is a successful reply plus where it came from.
type Result struct {
	provider.Response
	CallID   int64  // the ledger row this attempt wrote (0 if the ledger write failed)
	Entry    string // provider/model that answered
	ChainPos int    // 1-based position in the tier's chain
}

type Option func(*Router)

// WithAllowPublic lets public providers see brief and component scope calls
// under privacy.mode need_to_know. It never overrides private_only.
func WithAllowPublic(allow bool) Option { return func(r *Router) { r.allowPublic = allow } }

// WithSleep replaces the waiting function (backoff and cooldown waits), so
// tests run instantly.
func WithSleep(fn func(context.Context, time.Duration) error) Option {
	return func(r *Router) { r.sleep = fn }
}

// WithNow replaces the clock used for cooldowns and timestamps.
func WithNow(fn func() time.Time) Option { return func(r *Router) { r.now = fn } }

const transientRetries = 3

type Router struct {
	cfg         *settings.Config
	providers   map[string]provider.Provider
	led         ledger.Ledger
	sink        events.Sink
	allowPublic bool
	sleep       func(context.Context, time.Duration) error
	now         func() time.Time
	slots       map[string]chan struct{}

	mu         sync.Mutex
	cooldown   map[string]time.Time // provider -> earliest next use
	disabled   map[string]bool      // provider -> auth failed this run
	missing    map[string]bool      // provider/model -> not found this run
	ledgerErrs int
}

// New builds a router over already-constructed providers.
func New(cfg *settings.Config, providers map[string]provider.Provider, led ledger.Ledger, sink events.Sink, opts ...Option) *Router {
	if sink == nil {
		sink = events.Nop
	}
	r := &Router{
		cfg: cfg, providers: providers, led: led, sink: sink,
		sleep:    sleepCtx,
		now:      time.Now,
		slots:    map[string]chan struct{}{},
		cooldown: map[string]time.Time{},
		disabled: map[string]bool{},
		missing:  map[string]bool{},
	}
	for _, p := range cfg.Providers {
		r.slots[p.Name] = make(chan struct{}, p.MaxConcurrent)
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LedgerErrors counts ledger writes that failed. A run with a non-zero count
// is marked incomplete in its status; the model results were still used.
func (r *Router) LedgerErrors() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ledgerErrs
}

// promptDigest is the size and hash of the prompt text, the only things the
// ledger ever stores about it.
type promptDigest struct {
	bytes  int
	sha256 string
	tokens int // estimated: bytes/4 plus 10 percent
}

func digestPrompt(req provider.Request) promptDigest {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	n, sum := ledger.Digest([]byte(b.String()))
	return promptDigest{bytes: n, sha256: sum, tokens: n/4 + n/40}
}

// Call walks the tier's chain until one entry answers.
func (r *Router) Call(ctx context.Context, info CallInfo, req provider.Request) (Result, error) {
	chain, err := r.chainFor(info)
	if err != nil {
		return Result{}, err
	}
	pd := digestPrompt(req)
	maxWait := time.Duration(r.cfg.Defaults.MaxWaitMinutes) * time.Minute
	var waited time.Duration
	recordedTooLong := map[string]bool{}

	for {
		var reasons []EntryReason
		for i, entry := range chain {
			if info.Only != "" && entry != info.Only {
				continue
			}
			provName, model, _ := settings.SplitEntry(entry)
			if reason, skip := r.gate(info, entry, provName); skip {
				reasons = append(reasons, reason)
				continue
			}
			mi, _ := r.cfg.ModelInfo(entry)
			if pd.tokens+req.MaxTokens > mi.ContextTokens {
				if !recordedTooLong[entry] {
					recordedTooLong[entry] = true
					r.record(ctx, r.newRow(info, i+1, provName, model, pd, ledger.OutcomeTooLong, "too_long"))
				}
				reasons = append(reasons, EntryReason{Entry: entry, Kind: ReasonTooLong,
					Detail: fmt.Sprintf("about %d tokens plus %d reserved, model holds %d", pd.tokens, req.MaxTokens, mi.ContextTokens)})
				continue
			}
			res, reason, answered, err := r.attempt(ctx, info, req, entry, i+1, provName, model, pd)
			if err != nil {
				return Result{}, err
			}
			if answered {
				return res, nil
			}
			reasons = append(reasons, reason)
		}

		wait, cooling := r.shortestCooldown(reasons)
		if !cooling || waited+wait > maxWait {
			return Result{}, &ChainExhausted{Tier: info.Tier, Reasons: reasons}
		}
		if wait > 0 {
			r.emit(events.Event{Kind: events.KindWarning, Stage: info.Stage, NodeID: info.NodeID,
				Message: fmt.Sprintf("every model is cooling down; waiting %s", wait.Round(time.Second))})
			if err := r.sleep(ctx, wait); err != nil {
				return Result{}, err
			}
			waited += wait
		}
	}
}

// chainFor validates the call's tier, scope and identity and returns the chain.
func (r *Router) chainFor(info CallInfo) ([]string, error) {
	if info.RunID == "" || info.Stage == "" {
		return nil, errors.New("router: RunID and Stage are required")
	}
	switch info.Scope {
	case ScopeBrief, ScopeComponent, ScopeNode:
	default:
		return nil, fmt.Errorf("router: unknown scope %q (a call must declare what it carries)", info.Scope)
	}
	chain, ok := r.cfg.Models[string(info.Tier)]
	if !ok || len(chain) == 0 {
		return nil, fmt.Errorf("router: unknown tier %q", info.Tier)
	}
	return chain, nil
}

// gate reports why an entry must not be tried right now, if it must not.
func (r *Router) gate(info CallInfo, entry, provName string) (EntryReason, bool) {
	for _, x := range info.Exclude {
		if x == entry {
			return EntryReason{Entry: entry, Kind: ReasonExcluded, Detail: "its last reply was unusable"}, true
		}
	}
	if r.providers[provName] == nil {
		return EntryReason{Entry: entry, Kind: ReasonNoProvider}, true
	}
	r.mu.Lock()
	missing, disabled, until := r.missing[entry], r.disabled[provName], r.cooldown[provName]
	r.mu.Unlock()
	switch {
	case missing:
		return EntryReason{Entry: entry, Kind: ReasonModelMissing, Detail: "not found earlier in this run"}, true
	case disabled:
		return EntryReason{Entry: entry, Kind: ReasonAuth, Detail: "authentication failed earlier in this run"}, true
	case !r.eligible(provName, info.Scope):
		return EntryReason{Entry: entry, Kind: ReasonPrivacy,
			Detail: fmt.Sprintf("a public provider may not see %s scope under privacy.mode %s", info.Scope, r.cfg.Privacy.Mode)}, true
	case r.now().Before(until):
		return EntryReason{Entry: entry, Kind: ReasonCooldown, Detail: "until " + until.UTC().Format(time.RFC3339)}, true
	}
	return EntryReason{}, false
}

// shortestCooldown returns how long until the first cooling provider in
// reasons is usable again. cooling is false when nothing is merely waiting.
func (r *Router) shortestCooldown(reasons []EntryReason) (wait time.Duration, cooling bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range reasons {
		if x.Kind != ReasonCooldown {
			continue
		}
		provName, _, _ := settings.SplitEntry(x.Entry)
		d := r.cooldown[provName].Sub(r.now())
		if d < 0 {
			d = 0
		}
		if !cooling || d < wait {
			wait, cooling = d, true
		}
	}
	return wait, cooling
}

func (r *Router) setCooldown(provName string, d time.Duration) {
	r.mu.Lock()
	r.cooldown[provName] = r.now().Add(d)
	r.mu.Unlock()
}

func (r *Router) defaultCooldown() time.Duration {
	return time.Duration(r.cfg.RateLimits.CooldownAfter429Seconds) * time.Second
}

func (r *Router) emit(e events.Event) {
	if e.At.IsZero() {
		e.At = r.now().UTC()
	}
	r.sink.Emit(e)
}

// newRow starts a ledger row for an attempt on one entry.
func (r *Router) newRow(info CallInfo, pos int, provName, model string, pd promptDigest, o ledger.Outcome, errorKind string) *ledger.Call {
	taskType := info.TaskType
	if taskType == "" {
		taskType, _, _ = strings.Cut(info.Stage, ":")
	}
	return &ledger.Call{
		RunID: info.RunID, At: r.now().UTC(), Stage: info.Stage, NodeID: info.NodeID, Revision: info.Revision,
		TaskType: taskType, NodeClass: info.NodeClass,
		Scope: string(info.Scope), Tier: string(info.Tier), ChainPos: pos,
		Provider: provName, ModelRequested: model,
		PromptBytes: pd.bytes, PromptSHA256: pd.sha256,
		Outcome: o, ErrorKind: errorKind,
	}
}

// record writes a row. A failed write never fails the call: it is reported as
// a ledger_error event and counted, and the run is marked incomplete.
func (r *Router) record(ctx context.Context, row *ledger.Call) {
	if r.led == nil {
		return
	}
	// A cancelled call still gets its row, so write with a context that survives the cancel.
	if err := r.led.Record(context.WithoutCancel(ctx), row); err != nil {
		r.noteLedgerError(row.Stage, row.NodeID, err)
		return
	}
	cp := *row
	r.emit(events.Event{Kind: events.KindCall, Stage: row.Stage, NodeID: row.NodeID, Call: &cp})
}

func (r *Router) noteLedgerError(stage, nodeID string, err error) {
	r.mu.Lock()
	r.ledgerErrs++
	r.mu.Unlock()
	r.emit(events.Event{Kind: events.KindLedgerError, Stage: stage, NodeID: nodeID, Message: err.Error()})
}
```

- [ ] **Step 5: Implement `attempt.go`** (one entry: slots, timeout, retry with backoff, outcome mapping)

```go
package router

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
)

func (r *Router) acquire(ctx context.Context, provName string) error {
	select {
	case r.slots[provName] <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Router) release(provName string) { <-r.slots[provName] }

// attempt asks one chain entry, retrying transient failures with backoff. It
// returns answered=true with the result, or answered=false with the reason the
// entry did not answer. A non-nil error means the caller's context ended.
func (r *Router) attempt(ctx context.Context, info CallInfo, req provider.Request, entry string, pos int,
	provName, model string, pd promptDigest) (res Result, reason EntryReason, answered bool, err error) {

	p := r.providers[provName]
	backoff := time.Duration(r.cfg.RateLimits.BackoffInitialSeconds) * time.Second
	maxBackoff := time.Duration(r.cfg.RateLimits.BackoffMaxSeconds) * time.Second
	mult := time.Duration(r.cfg.RateLimits.BackoffMultiplier)

	for try := 0; ; try++ {
		if err := r.acquire(ctx, provName); err != nil {
			return Result{}, EntryReason{}, false, err
		}
		callCtx, cancel := context.WithTimeout(ctx, r.cfg.Defaults.CallTimeout)
		r2 := req
		r2.Model = model
		start := r.now()
		resp, cerr := p.Complete(callCtx, r2)
		cancel()
		r.release(provName)

		row := r.newRow(info, pos, provName, model, pd, ledger.OutcomeOK, "")
		row.DurationMS = r.now().Sub(start).Milliseconds()

		if cerr == nil {
			row.ModelServed = resp.Model
			row.PromptTokens, row.CompletionTokens = resp.Usage.PromptTokens, resp.Usage.CompletionTokens
			row.ResponseBytes, row.ResponseSHA256 = ledger.Digest([]byte(resp.Text))
			if resp.Duration > 0 {
				row.DurationMS = resp.Duration.Milliseconds()
			}
			r.record(ctx, row)
			return Result{Response: resp, CallID: row.ID, Entry: entry, ChainPos: pos}, EntryReason{}, true, nil
		}

		// The caller gave up: record it and stop the whole walk.
		if ctx.Err() != nil {
			row.Outcome, row.ErrorKind = ledger.OutcomeError, "cancelled"
			r.record(ctx, row)
			return Result{}, EntryReason{}, false, ctx.Err()
		}

		var rl provider.ErrRateLimited
		var tl provider.ErrContextTooLong
		var au provider.ErrAuth
		var nf provider.ErrModelNotFound
		switch {
		case errors.As(cerr, &rl):
			d := rl.RetryAfter
			if d <= 0 {
				d = r.defaultCooldown()
			}
			row.Outcome, row.ErrorKind, row.RetryAfterS = ledger.OutcomeRateLimited, "rate_limited", int(d/time.Second)
			r.record(ctx, row)
			r.setCooldown(provName, d)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonCooldown, Detail: "rate limited for " + d.String()}, false, nil

		case errors.As(cerr, &tl):
			row.Outcome, row.ErrorKind = ledger.OutcomeTooLong, "too_long"
			r.record(ctx, row)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonTooLong, Detail: "the provider refused the prompt as too long"}, false, nil

		case errors.As(cerr, &au):
			row.Outcome, row.ErrorKind = ledger.OutcomeAuth, "auth"
			r.record(ctx, row)
			r.mu.Lock()
			first := !r.disabled[provName]
			r.disabled[provName] = true
			r.mu.Unlock()
			if first {
				r.emit(events.Event{Kind: events.KindWarning, Stage: info.Stage, NodeID: info.NodeID,
					Message: fmt.Sprintf("provider %s rejected its credentials; it is disabled for this run", provName)})
			}
			return Result{}, EntryReason{Entry: entry, Kind: ReasonAuth, Detail: "authentication failed"}, false, nil

		case errors.As(cerr, &nf):
			row.Outcome, row.ErrorKind = ledger.OutcomeModelMissing, "model_missing"
			r.record(ctx, row)
			r.mu.Lock()
			r.missing[entry] = true
			r.mu.Unlock()
			r.emit(events.Event{Kind: events.KindWarning, Stage: info.Stage, NodeID: info.NodeID,
				Message: fmt.Sprintf("model %s was not found on its provider; skipping it for this run", entry)})
			return Result{}, EntryReason{Entry: entry, Kind: ReasonModelMissing}, false, nil

		case errors.Is(cerr, context.DeadlineExceeded):
			// Our own call_timeout expired (the caller's context is still live).
			row.Outcome, row.ErrorKind = ledger.OutcomeTimeout, "timeout"
			r.record(ctx, row)
			return Result{}, EntryReason{Entry: entry, Kind: ReasonFailed, Detail: "timed out after " + r.cfg.Defaults.CallTimeout.String()}, false, nil
		}

		// ErrTransient, and anything else the provider returned: retry with backoff.
		row.Outcome, row.ErrorKind = ledger.OutcomeError, "transient"
		r.record(ctx, row)
		if try >= transientRetries {
			r.setCooldown(provName, r.defaultCooldown())
			return Result{}, EntryReason{Entry: entry, Kind: ReasonCooldown, Detail: fmt.Sprintf("failed %d times in a row", try+1)}, false, nil
		}
		if err := r.sleep(ctx, backoff); err != nil {
			return Result{}, EntryReason{}, false, err
		}
		backoff *= mult
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
```

- [ ] **Step 6: Implement `parsed.go`** (malformed replies: retry once with the parse error, then exclude)

```go
package router

import (
	"context"
	"errors"

	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
)

// CallParsed is Call plus the stage's parser. A reply that arrives fine but
// does not parse is a failed attempt: its ledger row is corrected to
// malformed, the same model gets one more try with the parse error appended,
// and if that also fails the model is excluded and the chain continues.
func (r *Router) CallParsed(ctx context.Context, info CallInfo, req provider.Request, parse func(text string) error) (Result, error) {
	exclude := append([]string(nil), info.Exclude...)
	var lastParse error

	for {
		next := info
		next.Exclude = exclude
		res, err := r.Call(ctx, next, req)
		if err != nil {
			return Result{}, withParseErr(err, lastParse)
		}
		perr := parse(res.Text)
		if perr == nil {
			return res, nil
		}
		lastParse = perr
		r.markMalformed(ctx, res)

		// One more try on the same model, told what was wrong.
		again := next
		again.Only = res.Entry
		res2, err := r.Call(ctx, again, withParseError(req, res.Text, perr))
		if err == nil {
			perr2 := parse(res2.Text)
			if perr2 == nil {
				return res2, nil
			}
			lastParse = perr2
			r.markMalformed(ctx, res2)
		} else {
			var ce *ChainExhausted
			if !errors.As(err, &ce) {
				return Result{}, err // the caller's context ended
			}
		}
		exclude = append(exclude, res.Entry)
	}
}

func (r *Router) markMalformed(ctx context.Context, res Result) {
	if r.led == nil || res.CallID == 0 {
		return
	}
	// The error kind is a fixed word: a parser error can quote the reply, and reply text is never stored.
	if err := r.led.Amend(context.WithoutCancel(ctx), res.CallID, ledger.OutcomeMalformed, "malformed"); err != nil {
		r.noteLedgerError("", "", err)
	}
}

func withParseErr(err error, parseErr error) error {
	var ce *ChainExhausted
	if parseErr != nil && errors.As(err, &ce) {
		ce.ParseErr = parseErr
	}
	return err
}

func withParseError(req provider.Request, reply string, perr error) provider.Request {
	msgs := append([]provider.Message(nil), req.Messages...)
	msgs = append(msgs,
		provider.Message{Role: provider.RoleAssistant, Content: reply},
		provider.Message{Role: provider.RoleUser, Content: "That reply could not be used: " + perr.Error() + "\nReply again with the corrected output only."})
	req.Messages = msgs
	return req
}
```

- [ ] **Step 7: Run the router tests**

Run: `cd gophermind-lib && gofmt -l briefv2/router && go vet ./briefv2/router/ && go test ./briefv2/router/ -race -count=3 -v 2>&1 | tail -50`
Expected: `gofmt` prints nothing; PASS (all 27 test functions, three runs).

- [ ] **Step 8: Prove the privacy test can fail**

Temporarily make the rule allow everything, confirm the tests catch it, then put it back.

```bash
cd gophermind-lib
sed -i.bak 's/return scope == ScopeNode || r.allowPublic/return true/' briefv2/router/privacy.go
go test ./briefv2/router/ 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'
mv briefv2/router/privacy.go.bak briefv2/router/privacy.go
go test ./briefv2/router/ 2>&1 | tail -1
```

Expected: the first run shows `--- FAIL: TestPublicProvidersAreOnlyEverAskedWhatThePrivacyRuleAllows` and `--- FAIL: TestEveryEntryFilteredByPrivacyIsReportedAsSuch`; the last line is `ok`. `git diff --stat briefv2/router/privacy.go` prints nothing (the file is restored).

- [ ] **Step 9: Commit**

```bash
git add gophermind-lib/briefv2/router
git commit -m "feat(briefv2): model router with fallback chains, cooldowns, and the need-to-know privacy rule" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Human gates

**Files:**
- Create: `gophermind-lib/briefv2/human/human.go`, `terminal.go`, `file.go`, `programmatic.go`, `human_test.go`

**Interfaces:**
- Consumes: the standard library only.
- Produces: `human.Question{ID, Text, Default string; Options []string}`; `human.Answer{ID, Text string; Assumed bool}`; `human.PlanSummary{Markdown, Hash string}`; `human.Decision{Approved bool; By, Note string}`; `human.Escalation{NodeID, Reason string; History []string}`; `human.Action` (`ActionRetry`, `ActionSkip`, `ActionStop`); `human.Resolution{Action Action; Note string}`; `human.ErrWaiting`.
- Produces: `human.Gate` with `Ask(ctx, []Question) ([]Answer, error)`, `Approve(ctx, PlanSummary) (Decision, error)`, `Escalate(ctx, Escalation) (Resolution, error)`.
- Produces: `human.NewTerminal(in io.Reader, out io.Writer) *Terminal`; `human.NewFile(dir string) *File`; `human.NewProgrammatic() *Programmatic` with `Requests() <-chan *Request`; `human.Request{Kind Kind; Questions []Question; Plan PlanSummary; Escalation Escalation}` with `Answer([]Answer)`, `Decide(Decision)`, `Resolve(Resolution)`, `Fail(error)`; kinds `human.KindAsk`, `KindApprove`, `KindEscalate`.

Behavior worth knowing before reading the tests:

- **Terminal:** prompts on `out`, reads `in`. Enter takes the question's default (`Assumed: true`); a question with no default re-prompts; a number picks an option; end of input takes the default or is an error. Approval defaults to no.
- **File:** the first call writes the file and returns `ErrWaiting`. A default is written into the answer block, so leaving it alone accepts it; an empty block keeps waiting; a partial answer is never overwritten. An answered `QUESTIONS.md` is moved to `QUESTIONS.answered.md` so a later `Ask` starts fresh. `APPROVAL.md` carries `Plan hash: <hash>`; an approval for a different hash is never honored (the file is rewritten for the new plan and the call waits). Escalation file names are sanitized so a node id cannot leave the run folder.
- **Programmatic:** a request is published on a channel and completed by calling a method on it; the first reply wins.
- An answer is valid only if it answers the questions one for one, in order, with non-blank text.

- [ ] **Step 1: Write the failing tests** (`briefv2/human/human_test.go`)

One table of scenarios is run against all three adapters (a person answering in words each adapter understands), followed by tests for each adapter's own rules.

```go
package human_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/human"
)

// adapter runs one gate call the way a person would answer it. reply is the
// person's answer in words every adapter understands: "approve", "reject: why",
// "retry use the stdlib". typed holds one entry per question; "" means the
// person left the question alone (accepting its default).
type adapter struct {
	name     string
	ask      func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error)
	approve  func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error)
	escalate func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error)
}

func terminalAdapter() adapter {
	run := func(input string) (*human.Terminal, *bytes.Buffer) {
		out := &bytes.Buffer{}
		return human.NewTerminal(strings.NewReader(input), out), out
	}
	return adapter{
		name: "terminal",
		ask: func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error) {
			g, _ := run(strings.Join(typed, "\n") + "\n")
			return g.Ask(context.Background(), qs)
		},
		approve: func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error) {
			g, _ := run(reply + "\n")
			return g.Approve(context.Background(), plan)
		},
		escalate: func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error) {
			g, _ := run(reply + "\n")
			return g.Escalate(context.Background(), e)
		},
	}
}

// fill replaces the text of the block of the given kind (and id, when non-empty) in path.
func fill(t *testing.T, path, kind, id, text string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := "```" + kind
	if id != "" {
		head += " " + id
	}
	re := regexp.MustCompile("(?ms)^" + regexp.QuoteMeta(head) + "[ \\t]*\\n.*?^```[ \\t]*$")
	if !re.Match(data) {
		t.Fatalf("no %q block in %s:\n%s", head, path, data)
	}
	out := re.ReplaceAllLiteral(data, []byte(head+"\n"+text+"\n```"))
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileAdapter() adapter {
	return adapter{
		name: "file",
		ask: func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error) {
			dir := t.TempDir()
			g := human.NewFile(dir)
			if _, err := g.Ask(context.Background(), qs); !errors.Is(err, human.ErrWaiting) {
				t.Fatalf("first call = %v, want ErrWaiting", err)
			}
			for i, q := range qs {
				if typed[i] != "" {
					fill(t, filepath.Join(dir, "QUESTIONS.md"), "answer", q.ID, typed[i])
				}
			}
			return g.Ask(context.Background(), qs)
		},
		approve: func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error) {
			dir := t.TempDir()
			g := human.NewFile(dir)
			if _, err := g.Approve(context.Background(), plan); !errors.Is(err, human.ErrWaiting) {
				t.Fatalf("first call = %v, want ErrWaiting", err)
			}
			fill(t, filepath.Join(dir, "APPROVAL.md"), "decision", "", reply)
			return g.Approve(context.Background(), plan)
		},
		escalate: func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error) {
			dir := t.TempDir()
			g := human.NewFile(dir)
			if _, err := g.Escalate(context.Background(), e); !errors.Is(err, human.ErrWaiting) {
				t.Fatalf("first call = %v, want ErrWaiting", err)
			}
			fill(t, filepath.Join(dir, "ESCALATION-"+e.NodeID+".md"), "resolution", "", reply)
			return g.Escalate(context.Background(), e)
		},
	}
}

func programmaticAdapter() adapter {
	return adapter{
		name: "programmatic",
		ask: func(t *testing.T, qs []human.Question, typed []string) ([]human.Answer, error) {
			g := human.NewProgrammatic()
			type out struct {
				a   []human.Answer
				err error
			}
			done := make(chan out, 1)
			go func() { a, err := g.Ask(context.Background(), qs); done <- out{a, err} }()
			req := <-g.Requests()
			if req.Kind != human.KindAsk || len(req.Questions) != len(qs) {
				t.Errorf("request = %+v", req)
			}
			as := make([]human.Answer, len(qs))
			for i, q := range qs {
				as[i] = human.Answer{ID: q.ID, Text: typed[i]}
				if typed[i] == "" {
					as[i] = human.Answer{ID: q.ID, Text: q.Default, Assumed: true}
				}
			}
			req.Answer(as)
			r := <-done
			return r.a, r.err
		},
		approve: func(t *testing.T, plan human.PlanSummary, reply string) (human.Decision, error) {
			g := human.NewProgrammatic()
			type out struct {
				d   human.Decision
				err error
			}
			done := make(chan out, 1)
			go func() { d, err := g.Approve(context.Background(), plan); done <- out{d, err} }()
			req := <-g.Requests()
			if req.Kind != human.KindApprove || req.Plan != plan {
				t.Errorf("request = %+v", req)
			}
			word, note, _ := strings.Cut(reply, ":")
			req.Decide(human.Decision{Approved: word == "approve", Note: strings.TrimSpace(note)})
			r := <-done
			return r.d, r.err
		},
		escalate: func(t *testing.T, e human.Escalation, reply string) (human.Resolution, error) {
			g := human.NewProgrammatic()
			type out struct {
				r   human.Resolution
				err error
			}
			done := make(chan out, 1)
			go func() { r, err := g.Escalate(context.Background(), e); done <- out{r, err} }()
			req := <-g.Requests()
			word, note, _ := strings.Cut(reply, " ")
			req.Resolve(human.Resolution{Action: human.Action(word), Note: note})
			r := <-done
			return r.r, r.err
		},
	}
}

func adapters() []adapter { return []adapter{terminalAdapter(), fileAdapter(), programmaticAdapter()} }

var twoQuestions = []human.Question{
	{ID: "db", Text: "Which database?"},
	{ID: "lang", Text: "Which language?", Default: "Go", Options: []string{"Go", "Python"}},
}

func TestEveryAdapterAsksQuestionsTheSameWay(t *testing.T) {
	cases := []struct {
		name  string
		typed []string
		want  []human.Answer
	}{
		{"typed answer and an accepted default", []string{"postgres", ""},
			[]human.Answer{{ID: "db", Text: "postgres"}, {ID: "lang", Text: "Go", Assumed: true}}},
		{"both typed", []string{"postgres", "Python"},
			[]human.Answer{{ID: "db", Text: "postgres"}, {ID: "lang", Text: "Python"}}},
	}
	for _, a := range adapters() {
		for _, c := range cases {
			t.Run(a.name+"/"+c.name, func(t *testing.T) {
				got, err := a.ask(t, twoQuestions, c.typed)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, c.want) {
					t.Errorf("answers = %+v, want %+v", got, c.want)
				}
			})
		}
	}
}

func TestEveryAdapterHandlesApproval(t *testing.T) {
	plan := human.PlanSummary{Markdown: "# Plan\n3 components, 12 functions", Hash: "h123"}
	cases := []struct {
		name  string
		reply string
		want  human.Decision
	}{
		{"approve", "approve", human.Decision{Approved: true}},
		{"reject with a reason", "reject: too many components", human.Decision{Approved: false, Note: "too many components"}},
	}
	for _, a := range adapters() {
		for _, c := range cases {
			t.Run(a.name+"/"+c.name, func(t *testing.T) {
				got, err := a.approve(t, plan, c.reply)
				if err != nil {
					t.Fatal(err)
				}
				if got.By != a.name {
					t.Errorf("By = %q, want %q", got.By, a.name)
				}
				got.By = ""
				if got != c.want {
					t.Errorf("decision = %+v, want %+v", got, c.want)
				}
			})
		}
	}
}

func TestEveryAdapterHandlesEscalation(t *testing.T) {
	e := human.Escalation{NodeID: "fn-a", Reason: "all models failed", History: []string{"mini: tests failed", "kilo: timeout"}}
	cases := []struct {
		name  string
		reply string
		want  human.Resolution
	}{
		{"retry with a note", "retry use the standard library", human.Resolution{Action: human.ActionRetry, Note: "use the standard library"}},
		{"stop", "stop", human.Resolution{Action: human.ActionStop}},
	}
	for _, a := range adapters() {
		for _, c := range cases {
			t.Run(a.name+"/"+c.name, func(t *testing.T) {
				got, err := a.escalate(t, e, c.reply)
				if err != nil || got != c.want {
					t.Errorf("resolution = %+v, %v; want %+v", got, err, c.want)
				}
			})
		}
	}
}

func TestTerminalPromptsOnTheOutputWriter(t *testing.T) {
	out := &bytes.Buffer{}
	g := human.NewTerminal(strings.NewReader("postgres\n\n"), out)
	if _, err := g.Ask(context.Background(), twoQuestions); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Question 1 of 2", "Which database?", "Question 2 of 2", "1) Go", "2) Python", "press Enter for: Go"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("prompt output lacks %q:\n%s", want, out)
		}
	}
}

func TestTerminalNumberPicksAnOption(t *testing.T) {
	g := human.NewTerminal(strings.NewReader("mysql\n2\n"), &bytes.Buffer{})
	got, err := g.Ask(context.Background(), twoQuestions)
	if err != nil || got[1].Text != "Python" || got[1].Assumed {
		t.Errorf("%+v %v", got, err)
	}
}

func TestTerminalRequiresAnAnswerWhenThereIsNoDefault(t *testing.T) {
	out := &bytes.Buffer{}
	g := human.NewTerminal(strings.NewReader("\n\npostgres\n\n"), out)
	got, err := g.Ask(context.Background(), twoQuestions)
	if err != nil || got[0].Text != "postgres" {
		t.Fatalf("%+v %v", got, err)
	}
	if strings.Count(out.String(), "An answer is required.") != 2 {
		t.Errorf("expected two reminders:\n%s", out)
	}
}

func TestTerminalEndOfInput(t *testing.T) {
	// Input ends: a question with a default takes it; one without is an error.
	g := human.NewTerminal(strings.NewReader("postgres\n"), &bytes.Buffer{})
	got, err := g.Ask(context.Background(), twoQuestions)
	if err != nil || got[1].Text != "Go" || !got[1].Assumed {
		t.Errorf("default at end of input: %+v %v", got, err)
	}
	g = human.NewTerminal(strings.NewReader(""), &bytes.Buffer{})
	if _, err := g.Ask(context.Background(), twoQuestions); err == nil {
		t.Error("no input and no default should be an error")
	}
	g = human.NewTerminal(strings.NewReader("postgres"), &bytes.Buffer{}) // no trailing newline
	if got, err := g.Ask(context.Background(), twoQuestions[:1]); err != nil || got[0].Text != "postgres" {
		t.Errorf("last line without newline: %+v %v", got, err)
	}
}

func TestTerminalApprovalAndEscalationReprompt(t *testing.T) {
	plan := human.PlanSummary{Markdown: "plan", Hash: "h"}
	g := human.NewTerminal(strings.NewReader("maybe\ny\n"), &bytes.Buffer{})
	if d, err := g.Approve(context.Background(), plan); err != nil || !d.Approved {
		t.Errorf("garbage then y: %+v %v", d, err)
	}
	g = human.NewTerminal(strings.NewReader("\n"), &bytes.Buffer{})
	if d, err := g.Approve(context.Background(), plan); err != nil || d.Approved {
		t.Errorf("an empty line must not approve: %+v %v", d, err)
	}
	g = human.NewTerminal(strings.NewReader(""), &bytes.Buffer{})
	if _, err := g.Approve(context.Background(), plan); err == nil {
		t.Error("end of input must not approve")
	}
	g = human.NewTerminal(strings.NewReader("later\nskip\n"), &bytes.Buffer{})
	if r, err := g.Escalate(context.Background(), human.Escalation{NodeID: "fn-a"}); err != nil || r.Action != human.ActionSkip {
		t.Errorf("garbage then skip: %+v %v", r, err)
	}
}

func TestTerminalStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := human.NewTerminal(strings.NewReader("x\n"), &bytes.Buffer{})
	if _, err := g.Ask(ctx, twoQuestions); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestFileAskWritesTheQuestionsFileAndWaits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	g := human.NewFile(dir)
	_, err := g.Ask(context.Background(), twoQuestions)
	if !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("err = %v", err)
	}
	path := filepath.Join(dir, "QUESTIONS.md")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{"Which database?", "Which language?", "Options: Go | Python", "```answer db", "```answer lang\nGo\n```"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("QUESTIONS.md lacks %q:\n%s", want, b)
		}
	}
}

func TestFileAskKeepsWaitingAndNeverOverwritesAPartialAnswer(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	g.Ask(context.Background(), twoQuestions)
	path := filepath.Join(dir, "QUESTIONS.md")
	fill(t, path, "answer", "lang", "Python") // db still empty and it has no default
	before, _ := os.ReadFile(path)
	if _, err := g.Ask(context.Background(), twoQuestions); !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("err = %v, want ErrWaiting", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("a waiting call rewrote the file and lost the person's edits")
	}
}

func TestFileAnswersAreArchivedSoTheNextAskStartsFresh(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	g.Ask(context.Background(), twoQuestions)
	fill(t, filepath.Join(dir, "QUESTIONS.md"), "answer", "db", "postgres")
	if _, err := g.Ask(context.Background(), twoQuestions); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "QUESTIONS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Error("QUESTIONS.md should have been moved aside once answered")
	}
	if _, err := os.Stat(filepath.Join(dir, "QUESTIONS.answered.md")); err != nil {
		t.Error("the answered file should be kept as a record")
	}
	next := []human.Question{{ID: "other", Text: "A later question?"}}
	if _, err := g.Ask(context.Background(), next); !errors.Is(err, human.ErrWaiting) {
		t.Errorf("a new question set should wait, got %v", err)
	}
}

func TestFileAskRefusesAFileForDifferentQuestions(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	g.Ask(context.Background(), twoQuestions)
	_, err := g.Ask(context.Background(), []human.Question{{ID: "other", Text: "?"}})
	if err == nil || errors.Is(err, human.ErrWaiting) {
		t.Errorf("err = %v, want a mismatch error", err)
	}
	_, err = g.Ask(context.Background(), []human.Question{{ID: "lang", Text: "?"}, {ID: "db", Text: "?"}})
	if err == nil || errors.Is(err, human.ErrWaiting) {
		t.Errorf("questions in a different order: err = %v", err)
	}
}

func TestFileApprovalIsBoundToTheExactPlan(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	ctx := context.Background()
	planA := human.PlanSummary{Markdown: "plan A", Hash: "hashA"}
	planB := human.PlanSummary{Markdown: "plan B", Hash: "hashB"}
	if _, err := g.Approve(ctx, planA); !errors.Is(err, human.ErrWaiting) {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "APPROVAL.md")
	fill(t, path, "decision", "", "approve")
	for i := 0; i < 2; i++ { // asking again for the same plan returns the same decision
		if d, err := g.Approve(ctx, planA); err != nil || !d.Approved {
			t.Fatalf("call %d: %+v %v", i, d, err)
		}
	}
	// The plan changed: the old approval must not carry over.
	if _, err := g.Approve(ctx, planB); !errors.Is(err, human.ErrWaiting) {
		t.Fatalf("a changed plan must wait for a new decision, got %v", err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "Plan hash: hashB") || !strings.Contains(string(b), "plan B") || strings.Contains(string(b), "approve\n```") {
		t.Errorf("APPROVAL.md was not rewritten for the new plan:\n%s", b)
	}
}

func TestFileApprovalRejectsAnUnreadableDecision(t *testing.T) {
	dir := t.TempDir()
	g := human.NewFile(dir)
	plan := human.PlanSummary{Markdown: "plan", Hash: "h"}
	g.Approve(context.Background(), plan)
	fill(t, filepath.Join(dir, "APPROVAL.md"), "decision", "", "looks fine I guess")
	_, err := g.Approve(context.Background(), plan)
	if err == nil || errors.Is(err, human.ErrWaiting) {
		t.Errorf("err = %v, want a format error", err)
	}
}

func TestFileEscalationNamesStayInsideTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	g := human.NewFile(dir)
	if _, err := g.Escalate(context.Background(), human.Escalation{NodeID: "../../evil/fn"}); !errors.Is(err, human.ErrWaiting) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "ESCALATION-") || strings.Contains(entries[0].Name(), "/") {
		t.Errorf("entries = %v", entries)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "evil")); err == nil {
		t.Error("a node id escaped the run folder")
	}
}

func TestProgrammaticRejectsAnswersForTheWrongQuestions(t *testing.T) {
	g := human.NewProgrammatic()
	done := make(chan error, 1)
	go func() { _, err := g.Ask(context.Background(), twoQuestions); done <- err }()
	req := <-g.Requests()
	req.Answer([]human.Answer{{ID: "db", Text: "x"}}) // one answer for two questions
	if err := <-done; err == nil {
		t.Error("a short answer list was accepted")
	}
	go func() { _, err := g.Ask(context.Background(), twoQuestions); done <- err }()
	req = <-g.Requests()
	req.Answer([]human.Answer{{ID: "db", Text: "x"}, {ID: "wrong", Text: "y"}})
	if err := <-done; err == nil {
		t.Error("an answer for the wrong question id was accepted")
	}
	go func() { _, err := g.Ask(context.Background(), twoQuestions[:1]); done <- err }()
	req = <-g.Requests()
	req.Answer([]human.Answer{{ID: "db", Text: "  "}})
	if err := <-done; err == nil {
		t.Error("a blank answer was accepted")
	}
}

func TestProgrammaticCancellationFailureAndDoubleReply(t *testing.T) {
	g := human.NewProgrammatic()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := g.Approve(ctx, human.PlanSummary{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("unanswered request: err = %v", err)
	}
	<-g.Requests() // drain the abandoned request

	boom := errors.New("the app closed")
	done := make(chan error, 1)
	go func() { _, err := g.Escalate(context.Background(), human.Escalation{NodeID: "n"}); done <- err }()
	req := <-g.Requests()
	req.Fail(boom)
	req.Fail(errors.New("second reply is ignored")) // must not block or panic
	if err := <-done; !errors.Is(err, boom) {
		t.Errorf("Fail: err = %v", err)
	}

	go func() { _, err := g.Escalate(context.Background(), human.Escalation{NodeID: "n"}); done <- err }()
	req = <-g.Requests()
	req.Resolve(human.Resolution{}) // no action
	if err := <-done; err == nil {
		t.Error("a resolution without an action was accepted")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/human/ 2>&1 | head -5`
Expected: FAIL, `no non-test Go files`.

- [ ] **Step 3: Implement `human.go`** (types, `ErrWaiting`, answer validation, parsing of `approve` and `retry`)

```go
// Package human is where the engine stops and asks a person: clarifying
// questions, plan approval, and (for the executor plan) escalated leaves. The
// Gate interface has three adapters so the terminal, a pair of files, and
// later the desktop app can all answer the same calls.
package human

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrWaiting means a question or approval was written out and nobody has
// answered it yet. The command exits with code 3; `resume` asks again.
var ErrWaiting = errors.New("human: waiting for an answer")

// Question is one thing the planner needs to know.
type Question struct {
	ID      string
	Text    string
	Default string   // taken when the person leaves it alone; empty means an answer is required
	Options []string // suggestions; the terminal lets a number pick one
}

// Answer answers the question with the same ID. Assumed is true when the
// default was taken rather than typed.
type Answer struct {
	ID      string
	Text    string
	Assumed bool
}

// PlanSummary is the plan as shown for approval. Hash binds an approval to
// this exact plan.
type PlanSummary struct {
	Markdown string
	Hash     string
}

// Decision is the answer to Approve.
type Decision struct {
	Approved bool
	By       string // terminal, file, programmatic
	Note     string
}

// Escalation is a leaf that every model failed, handed to a person.
type Escalation struct {
	NodeID  string
	Reason  string
	History []string // one line per failed attempt
}

// Action is what to do about an escalated leaf.
type Action string

const (
	ActionRetry Action = "retry" // try again, with the note added to the leaf's context
	ActionSkip  Action = "skip"  // leave the leaf failed and continue
	ActionStop  Action = "stop"  // stop the run
)

// Resolution is the answer to Escalate.
type Resolution struct {
	Action Action
	Note   string
}

// Gate is the whole conversation with a person.
type Gate interface {
	// Ask blocks until every question is answered, in order.
	Ask(ctx context.Context, qs []Question) ([]Answer, error)
	Approve(ctx context.Context, plan PlanSummary) (Decision, error)
	// Escalate is used by the executor plan.
	Escalate(ctx context.Context, e Escalation) (Resolution, error)
}

// validateAnswers checks that as answers qs one for one, in order, with text.
func validateAnswers(qs []Question, as []Answer) error {
	if len(as) != len(qs) {
		return fmt.Errorf("human: %d answers for %d questions", len(as), len(qs))
	}
	for i, q := range qs {
		if as[i].ID != q.ID {
			return fmt.Errorf("human: answer %d is for %q, want %q", i+1, as[i].ID, q.ID)
		}
		if strings.TrimSpace(as[i].Text) == "" {
			return fmt.Errorf("human: question %q has an empty answer", q.ID)
		}
	}
	return nil
}

// parseDecision reads "approve", "yes", "reject: why", "no" and so on. ok is
// false for anything else, including empty text.
func parseDecision(text string) (approved bool, note string, ok bool) {
	word, rest := splitWord(text)
	switch word {
	case "approve", "approved", "yes", "y":
		return true, rest, true
	case "reject", "rejected", "no", "n":
		return false, rest, true
	}
	return false, "", false
}

// parseAction reads "retry check the test", "skip", "stop".
func parseAction(text string) (Action, string, bool) {
	word, rest := splitWord(text)
	switch Action(word) {
	case ActionRetry, ActionSkip, ActionStop:
		return Action(word), rest, true
	}
	return "", "", false
}

// splitWord returns the lower-cased first word (trailing colon removed) and
// the trimmed remainder.
func splitWord(text string) (word, rest string) {
	text = strings.TrimSpace(text)
	i := strings.IndexAny(text, " \t\r\n")
	if i < 0 {
		return strings.ToLower(strings.TrimRight(text, ":")), ""
	}
	return strings.ToLower(strings.TrimRight(text[:i], ":")), strings.TrimSpace(text[i+1:])
}
```

- [ ] **Step 4: Implement `terminal.go`**

```go
package human

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Terminal asks on out and reads answers from in (stderr and stdin in the
// real command).
type Terminal struct {
	in  *bufio.Reader
	out io.Writer
}

var _ Gate = (*Terminal)(nil)

func NewTerminal(in io.Reader, out io.Writer) *Terminal {
	return &Terminal{in: bufio.NewReader(in), out: out}
}

// readLine returns the next line without its newline. A last line with no
// newline still counts; io.EOF is returned only when nothing was read.
func (t *Terminal) readLine(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s, err := t.in.ReadString('\n')
	if err == io.EOF && s != "" {
		err = nil
	}
	return strings.TrimRight(s, "\r\n"), err
}

func (t *Terminal) Ask(ctx context.Context, qs []Question) ([]Answer, error) {
	out := make([]Answer, 0, len(qs))
	for i, q := range qs {
		fmt.Fprintf(t.out, "\nQuestion %d of %d: %s\n", i+1, len(qs), q.Text)
		for j, o := range q.Options {
			fmt.Fprintf(t.out, "  %d) %s\n", j+1, o)
		}
		if q.Default != "" {
			fmt.Fprintf(t.out, "  (press Enter for: %s)\n", q.Default)
		}
		for {
			fmt.Fprint(t.out, "> ")
			line, err := t.readLine(ctx)
			line = strings.TrimSpace(line)
			if err != nil && line == "" {
				if errors.Is(err, io.EOF) && q.Default != "" {
					out = append(out, Answer{ID: q.ID, Text: q.Default, Assumed: true})
					break
				}
				if errors.Is(err, io.EOF) {
					return nil, fmt.Errorf("human: input ended before question %q was answered", q.ID)
				}
				return nil, err
			}
			if line == "" {
				if q.Default != "" {
					out = append(out, Answer{ID: q.ID, Text: q.Default, Assumed: true})
					break
				}
				fmt.Fprintln(t.out, "An answer is required.")
				continue
			}
			if n, convErr := strconv.Atoi(line); convErr == nil && n >= 1 && n <= len(q.Options) {
				line = q.Options[n-1]
			}
			out = append(out, Answer{ID: q.ID, Text: line})
			break
		}
	}
	return out, validateAnswers(qs, out)
}

func (t *Terminal) Approve(ctx context.Context, plan PlanSummary) (Decision, error) {
	fmt.Fprintf(t.out, "\n%s\n\nPlan hash: %s\n", plan.Markdown, plan.Hash)
	for {
		fmt.Fprint(t.out, "Approve this plan? [y/N] ")
		line, err := t.readLine(ctx)
		if err != nil && strings.TrimSpace(line) == "" {
			if errors.Is(err, io.EOF) {
				return Decision{}, errors.New("human: input ended before the plan was approved or rejected")
			}
			return Decision{}, err
		}
		if strings.TrimSpace(line) == "" {
			return Decision{Approved: false, By: "terminal"}, nil
		}
		if approved, note, ok := parseDecision(line); ok {
			return Decision{Approved: approved, By: "terminal", Note: note}, nil
		}
		fmt.Fprintln(t.out, "Answer y or n, optionally followed by a note.")
	}
}

func (t *Terminal) Escalate(ctx context.Context, e Escalation) (Resolution, error) {
	fmt.Fprintf(t.out, "\nEvery model failed %s: %s\n", e.NodeID, e.Reason)
	for _, h := range e.History {
		fmt.Fprintf(t.out, "  - %s\n", h)
	}
	for {
		fmt.Fprint(t.out, "retry, skip, or stop (add a note after it if you like): ")
		line, err := t.readLine(ctx)
		if err != nil && strings.TrimSpace(line) == "" {
			if errors.Is(err, io.EOF) {
				return Resolution{}, errors.New("human: input ended before the escalation was resolved")
			}
			return Resolution{}, err
		}
		if action, note, ok := parseAction(line); ok {
			return Resolution{Action: action, Note: note}, nil
		}
		fmt.Fprintln(t.out, "Type retry, skip, or stop.")
	}
}
```

- [ ] **Step 5: Implement `file.go`**

```go
package human

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// File is the gate for runs nobody is watching. It writes QUESTIONS.md,
// APPROVAL.md, or ESCALATION-<node>.md into a folder, each holding a fenced
// block for the answer, and returns ErrWaiting until the block is filled in.
// The next call (after `resume`) reads and validates the answer.
//
// The answer must not itself contain a line of three backticks.
type File struct{ dir string }

var _ Gate = (*File)(nil)

func NewFile(dir string) *File { return &File{dir: dir} }

var (
	blockRE = regexp.MustCompile("(?ms)^```([a-z]+) ?([^\\n]*)\\n(.*?)^```[ \\t]*$")
	hashRE  = regexp.MustCompile(`(?m)^Plan hash: (\S+)`)
	unsafeR = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

type block struct{ kind, id, text string }

// parseBlocks returns the fenced blocks of the given kind ("answer",
// "decision", "resolution") in file order.
func parseBlocks(data []byte, kind string) []block {
	var out []block
	for _, m := range blockRE.FindAllStringSubmatch(string(data), -1) {
		if m[1] == kind {
			out = append(out, block{kind: kind, id: strings.TrimSpace(m[2]), text: strings.TrimSpace(m[3])})
		}
	}
	return out
}

func (f *File) write(name, content string) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return fmt.Errorf("human: %w", err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		return fmt.Errorf("human: %w", err)
	}
	return nil
}

func (f *File) read(name string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(f.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("human: %w", err)
	}
	return data, true, nil
}

// archive moves an answered file aside so the next call with the same file
// name starts a fresh conversation.
func (f *File) archive(name string) {
	answered := strings.TrimSuffix(name, ".md") + ".answered.md"
	os.Rename(filepath.Join(f.dir, name), filepath.Join(f.dir, answered))
}

func (f *File) Ask(ctx context.Context, qs []Question) ([]Answer, error) {
	const name = "QUESTIONS.md"
	data, found, err := f.read(name)
	if err != nil {
		return nil, err
	}
	if !found {
		var b strings.Builder
		b.WriteString("# Questions\n\nFill in each answer block, save the file, then run `gophermind brief resume`.\n")
		b.WriteString("A block that already holds text is the default answer; leave it to accept it.\n")
		for i, q := range qs {
			fmt.Fprintf(&b, "\n## Question %d (id: %s)\n\n%s\n", i+1, q.ID, q.Text)
			if len(q.Options) > 0 {
				fmt.Fprintf(&b, "\nOptions: %s\n", strings.Join(q.Options, " | "))
			}
			fmt.Fprintf(&b, "\n```answer %s\n%s\n```\n", q.ID, q.Default)
		}
		if err := f.write(name, b.String()); err != nil {
			return nil, err
		}
		return nil, ErrWaiting
	}
	blocks := parseBlocks(data, "answer")
	if len(blocks) != len(qs) {
		return nil, fmt.Errorf("human: %s has %d answer blocks but %d questions are being asked; delete it and run again", name, len(blocks), len(qs))
	}
	answers := make([]Answer, len(qs))
	for i, q := range qs {
		if blocks[i].id != q.ID {
			return nil, fmt.Errorf("human: %s block %d is for %q but question %q is being asked; delete it and run again", name, i+1, blocks[i].id, q.ID)
		}
		if blocks[i].text == "" {
			return nil, ErrWaiting
		}
		answers[i] = Answer{ID: q.ID, Text: blocks[i].text, Assumed: q.Default != "" && blocks[i].text == q.Default}
	}
	if err := validateAnswers(qs, answers); err != nil {
		return nil, err
	}
	f.archive(name)
	return answers, nil
}

func (f *File) Approve(ctx context.Context, plan PlanSummary) (Decision, error) {
	const name = "APPROVAL.md"
	data, found, err := f.read(name)
	if err != nil {
		return Decision{}, err
	}
	if found {
		if m := hashRE.FindSubmatch(data); m != nil && string(m[1]) == plan.Hash {
			blocks := parseBlocks(data, "decision")
			if len(blocks) != 1 {
				return Decision{}, fmt.Errorf("human: %s must contain exactly one decision block", name)
			}
			if blocks[0].text == "" {
				return Decision{}, ErrWaiting
			}
			approved, note, ok := parseDecision(blocks[0].text)
			if !ok {
				return Decision{}, fmt.Errorf("human: %s: write approve, or reject followed by a reason", name)
			}
			return Decision{Approved: approved, By: "file", Note: note}, nil
		}
		// The plan changed since the file was written: an old answer must not approve a new plan.
	}
	body := fmt.Sprintf("# Approval\n\nRead the plan, then write `approve` or `reject: <reason>` in the decision block and run `gophermind brief resume`.\n\n"+
		"Plan hash: %s\n\n---\n\n%s\n\n---\n\n```decision\n\n```\n", plan.Hash, plan.Markdown)
	if err := f.write(name, body); err != nil {
		return Decision{}, err
	}
	return Decision{}, ErrWaiting
}

func (f *File) Escalate(ctx context.Context, e Escalation) (Resolution, error) {
	name := "ESCALATION-" + unsafeR.ReplaceAllString(e.NodeID, "_") + ".md"
	data, found, err := f.read(name)
	if err != nil {
		return Resolution{}, err
	}
	if !found {
		var b strings.Builder
		fmt.Fprintf(&b, "# Escalation: %s\n\nEvery model failed this leaf: %s\n\n", e.NodeID, e.Reason)
		for _, h := range e.History {
			fmt.Fprintf(&b, "- %s\n", h)
		}
		b.WriteString("\nWrite `retry`, `skip`, or `stop` in the block, optionally followed by a note, then run `gophermind brief resume`.\n")
		b.WriteString("\n```resolution\n\n```\n")
		if err := f.write(name, b.String()); err != nil {
			return Resolution{}, err
		}
		return Resolution{}, ErrWaiting
	}
	blocks := parseBlocks(data, "resolution")
	if len(blocks) != 1 {
		return Resolution{}, fmt.Errorf("human: %s must contain exactly one resolution block", name)
	}
	if blocks[0].text == "" {
		return Resolution{}, ErrWaiting
	}
	action, note, ok := parseAction(blocks[0].text)
	if !ok {
		return Resolution{}, fmt.Errorf("human: %s: write retry, skip, or stop", name)
	}
	f.archive(name)
	return Resolution{Action: action, Note: note}, nil
}
```

- [ ] **Step 6: Implement `programmatic.go`**

```go
package human

import (
	"context"
	"errors"
)

// Kind says which call a Request is.
type Kind string

const (
	KindAsk      Kind = "ask"
	KindApprove  Kind = "approve"
	KindEscalate Kind = "escalate"
)

type reply struct {
	answers    []Answer
	decision   Decision
	resolution Resolution
	err        error
}

// Request is one pending question for a person, published on
// Programmatic.Requests. Exactly one of Answer, Decide, Resolve, or Fail
// completes it (the matching one for its Kind); a second call does nothing.
type Request struct {
	Kind       Kind
	Questions  []Question
	Plan       PlanSummary
	Escalation Escalation
	reply      chan reply
}

func (r *Request) send(x reply) {
	select {
	case r.reply <- x:
	default:
	}
}

func (r *Request) Answer(as []Answer)   { r.send(reply{answers: as}) }
func (r *Request) Decide(d Decision)    { r.send(reply{decision: d}) }
func (r *Request) Resolve(x Resolution) { r.send(reply{resolution: x}) }
func (r *Request) Fail(err error)       { r.send(reply{err: err}) }

// Programmatic is the gate the run service and the desktop app use: requests
// are published as values and answered by calling methods on them.
type Programmatic struct{ reqs chan *Request }

var _ Gate = (*Programmatic)(nil)

func NewProgrammatic() *Programmatic { return &Programmatic{reqs: make(chan *Request, 16)} }

// Requests delivers each pending request, oldest first.
func (p *Programmatic) Requests() <-chan *Request { return p.reqs }

func (p *Programmatic) roundTrip(ctx context.Context, r *Request) (reply, error) {
	r.reply = make(chan reply, 1)
	select {
	case p.reqs <- r:
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
	select {
	case x := <-r.reply:
		return x, x.err
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
}

func (p *Programmatic) Ask(ctx context.Context, qs []Question) ([]Answer, error) {
	x, err := p.roundTrip(ctx, &Request{Kind: KindAsk, Questions: qs})
	if err != nil {
		return nil, err
	}
	if err := validateAnswers(qs, x.answers); err != nil {
		return nil, err
	}
	return x.answers, nil
}

func (p *Programmatic) Approve(ctx context.Context, plan PlanSummary) (Decision, error) {
	x, err := p.roundTrip(ctx, &Request{Kind: KindApprove, Plan: plan})
	if err != nil {
		return Decision{}, err
	}
	if x.decision.By == "" {
		x.decision.By = "programmatic"
	}
	return x.decision, nil
}

func (p *Programmatic) Escalate(ctx context.Context, e Escalation) (Resolution, error) {
	x, err := p.roundTrip(ctx, &Request{Kind: KindEscalate, Escalation: e})
	if err != nil {
		return Resolution{}, err
	}
	switch x.resolution.Action {
	case ActionRetry, ActionSkip, ActionStop:
		return x.resolution, nil
	}
	return Resolution{}, errors.New("human: resolution needs an action of retry, skip, or stop")
}
```

- [ ] **Step 7: Run and commit**

Run: `cd gophermind-lib && gofmt -l briefv2/human && go vet ./briefv2/human/ && go test ./briefv2/human/ -race -count=3 -v 2>&1 | tail -40`
Expected: `gofmt` prints nothing; PASS (all 18 test functions, three runs).

```bash
git add gophermind-lib/briefv2/human
git commit -m "feat(briefv2): human gates (terminal, file, programmatic) with plan-bound approval" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Requirements and the coverage checker

This task is pure code: no model call, no router, no database. It gives the planner the list of things a brief demands and the check that a plan covers every one of them (spec section 9, "Coverage", rules 1 to 5, and two of the three warnings; the third, the stray `cmd/<name>` warning, is added in Task 10). The stage that calls a model with these is Task 10.

**Files:**
- Create: `gophermind-lib/briefv2/planner/requirements.go`, `gophermind-lib/briefv2/planner/requirements_test.go`
- Create: `gophermind-lib/briefv2/planner/coverage_check.go`, `gophermind-lib/briefv2/planner/coverage_check_test.go`
- Existing (committed with this plan, do not recreate or edit): `gophermind-lib/briefv2/planner/testdata/aivs/nodes.json`, `mapping.json`, `README.md`
- Existing (read only): `gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md`

**Interfaces:**
- Consumes: `tree.Kind`, `tree.KindRoot`, `tree.KindComponent`, `tree.KindFunction` from `gophermind/gophermind-lib/briefv2/tree`. Nothing from Tasks 1 to 6.
- Produces: `planner.ReqKind` with `ReqConstraint`, `ReqAcceptance`, `ReqFeature`; `planner.Requirement{ID, Kind, Name, Text, Line}`; `planner.ParseRequirements(src []byte) []Requirement` (never nil, file order, ids `C1..`, `A1..`, `F1..` numbered per kind).
- Produces: `planner.PlanNode{ID string; Kind tree.Kind; Parent, Title, File string}`; `planner.MapEntry{Requirement string; Nodes []string}`; `planner.RootTest{Requirement, Name, Given, Expect, Command string}`; `planner.CoverageReply{Map []MapEntry; RootTests []RootTest}` (JSON keys `map`, `root_tests`); `planner.Gap{Requirement string; Line int; Text, Reason string}`; `planner.Covered{Requirement string; Nodes, RootTests []string}`.
- Produces: `planner.ParseCoverageReply(text string, reqs []Requirement) (CoverageReply, error)` (an error means the reply is malformed; unknown JSON keys such as a fill reply's `types` and `functions` are ignored here); `(CoverageReply).Merge(other CoverageReply) CoverageReply`; `planner.CheckCoverage(reqs []Requirement, nodes []PlanNode, reply CoverageReply) (covered []Covered, gaps []Gap)` (both never nil, both in `reqs` order); `planner.PathWarnings(briefSrc []byte, nodes []PlanNode) []string`; `planner.CommandWarnings(reqs []Requirement, reply CoverageReply) []string`.

Behaviour the later tasks rely on:
- A bullet's `Text` is the bullet line with its `- ` or `* ` marker removed, then each continuation line verbatim (indentation kept), joined with `\n`. A feature's `Text` is its block, trimmed. `Line` counts from the first line of the file, frontmatter included.
- Gap reasons are plain words joined with `; `: `not mapped`, `no node and no root test`, `no covering node`, `acceptance bullet has no root test`, preceded by one entry per dropped id (`names unknown node <id>`, `the root node alone covers nothing`, `component <id> has no functions`).
- `CommandWarnings` skips an acceptance requirement that has no root test at all: that is a gap, and reporting it twice would only add noise.
- `PathWarnings` looks at function nodes only. Against the AI Venture Studio fixture it reports 18 paths (for example `internal/tenancy` and `internal/httpapi`) and does not report `cmd/venture-server`, because that plan did place its main package there.

- [ ] **Step 1: Check the fixtures are present**

```bash
cd gophermind-lib
python3 - <<'PY'
import json
nodes = json.load(open("briefv2/planner/testdata/aivs/nodes.json"))
reply = json.load(open("briefv2/planner/testdata/aivs/mapping.json"))
kinds = [n["kind"] for n in nodes]
print(len(nodes), kinds.count("root"), kinds.count("component"), kinds.count("function"))
print(len(reply["map"]), len(reply["root_tests"]))
PY
wc -l briefv2/testdata/ai-venture-studio-server-brief.md
```

Expected: `121 1 21 99`, then `29 0`, then `750 briefv2/testdata/ai-venture-studio-server-brief.md`. If a file is missing or a number differs, stop and report it: the fixtures are hand-reviewed (see `testdata/aivs/README.md`) and must not be regenerated.

- [ ] **Step 2: Write the failing requirements test** (`briefv2/planner/requirements_test.go`)

```go
package planner_test

import (
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

const aivsBrief = "../testdata/ai-venture-studio-server-brief.md"

// sampleBrief is numbered by line in the comments of the test below.
const sampleBrief = `---
spec_version: "2.0"
id: gm-2026-09-29-009
---

## Overview

- An overview bullet is not a requirement.

## Features

### Greeting

Says hello.

- Returns "hello, NAME".

### Farewell

Says goodbye.

## Constraints

These hold everywhere.

- Standard library only.
- Errors are values:
  - never panic
  - wrap with %w

` + "```text" + `
- a bullet inside a fence
` + "```" + `
* Go 1.22 or later.

## Out of scope

- Persistence.

## Acceptance

- ` + "`go build ./...`" + ` succeeds.
- ` + "`go test ./...`" + ` passes
  with no network.
`

func TestParseRequirements(t *testing.T) {
	got := planner.ParseRequirements([]byte(sampleBrief))
	want := []planner.Requirement{
		{ID: "F1", Kind: planner.ReqFeature, Name: "Greeting", Text: "Says hello.\n\n- Returns \"hello, NAME\".", Line: 12},
		{ID: "F2", Kind: planner.ReqFeature, Name: "Farewell", Text: "Says goodbye.", Line: 18},
		{ID: "C1", Kind: planner.ReqConstraint, Text: "Standard library only.", Line: 26},
		{ID: "C2", Kind: planner.ReqConstraint, Text: "Errors are values:\n  - never panic\n  - wrap with %w", Line: 27},
		{ID: "C3", Kind: planner.ReqConstraint, Text: "Go 1.22 or later.", Line: 34},
		{ID: "A1", Kind: planner.ReqAcceptance, Text: "`go build ./...` succeeds.", Line: 42},
		{ID: "A2", Kind: planner.ReqAcceptance, Text: "`go test ./...` passes\n  with no network.", Line: 43},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requirements, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("requirement %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestParseRequirementsEdgeCases(t *testing.T) {
	cases := []struct {
		name, src string
		wantIDs   string
	}{
		{"empty file", "", ""},
		{"no frontmatter", "## Constraints\n\n- one\n", "C1"},
		{"crlf and bom", "\xef\xbb\xbf---\r\nid: x\r\n---\r\n\r\n## Acceptance\r\n\r\n- one\r\n- two\r\n", "A1 A2"},
		{"no acceptance section", "---\n---\n## Constraints\n- one\n## Features\n### A\nbody\n", "C1 F1"},
		{"heading inside a fence is not a section", "## Constraints\n- one\n```\n## Acceptance\n- hidden\n```\n- two\n", "C1 C2"},
		{"text before the first bullet is ignored", "## Acceptance\nIntro line.\n\n- one\n", "A1"},
		{"a paragraph ends a bullet and is ignored", "## Constraints\n- one\nloose text\n  indented after loose text\n- two\n", "C1 C2"},
		{"h3 outside features is nothing", "## Architecture\n### Packages\n- `cmd/x`\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ids []string
			for _, r := range planner.ParseRequirements([]byte(c.src)) {
				ids = append(ids, r.ID)
			}
			if got := strings.Join(ids, " "); got != c.wantIDs {
				t.Fatalf("ids = %q, want %q", got, c.wantIDs)
			}
		})
	}
	// The ignored paragraph must not leak into the bullet before it.
	got := planner.ParseRequirements([]byte("## Constraints\n- one\nloose text\n  indented after loose text\n- two\n"))
	if got[0].Text != "one" || got[1].Text != "two" {
		t.Fatalf("texts = %q, %q", got[0].Text, got[1].Text)
	}
}

// The AI Venture Studio brief is the one the v1 planner dropped requirements
// from. The counts are pinned so a parser change that loses a bullet fails.
func TestParseRequirementsAIVentureStudio(t *testing.T) {
	src, err := os.ReadFile(aivsBrief)
	if err != nil {
		t.Fatal(err)
	}
	reqs := planner.ParseRequirements(src)
	counts := map[planner.ReqKind]int{}
	for _, r := range reqs {
		counts[r.Kind]++
		if r.Text == "" || r.Line == 0 {
			t.Errorf("%s has empty text or no line: %+v", r.ID, r)
		}
	}
	if counts[planner.ReqConstraint] != 10 || counts[planner.ReqAcceptance] != 12 || counts[planner.ReqFeature] != 19 {
		t.Fatalf("counts = %v, want 10 constraints, 12 acceptance, 19 features", counts)
	}
	lines := strings.Split(string(src), "\n")
	for _, r := range reqs {
		at := lines[r.Line-1]
		switch r.Kind {
		case planner.ReqFeature:
			if at != "### "+r.Name {
				t.Errorf("%s: line %d is %q, want the heading %q", r.ID, r.Line, at, r.Name)
			}
		default:
			first := strings.SplitN(r.Text, "\n", 2)[0]
			if at != "- "+first {
				t.Errorf("%s: line %d is %q, want the bullet %q", r.ID, r.Line, at, first)
			}
		}
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | tail -3`
Expected: `no non-test Go files` and `FAIL ... [build failed]` (the package does not exist yet).

- [ ] **Step 4: Implement `requirements.go`**

```go
// Package planner turns a validated v2 brief into a task tree: it parses the
// brief's requirements, runs the planning stages against the model router, and
// refuses to let a plan reach approval while a requirement is uncovered.
package planner

import (
	"fmt"
	"strings"
)

// ReqKind says which part of the brief a requirement came from.
type ReqKind string

const (
	ReqConstraint ReqKind = "constraint"
	ReqAcceptance ReqKind = "acceptance"
	ReqFeature    ReqKind = "feature"
)

// Requirement is one obligation the brief states. IDs are C1.., A1.., F1..,
// numbered per kind in file order, so they are stable for a given brief text.
type Requirement struct {
	ID   string  `json:"id"`
	Kind ReqKind `json:"kind"`
	Name string  `json:"name,omitempty"` // feature heading; features only
	Text string  `json:"text"`           // verbatim; bullet marker removed
	Line int     `json:"line"`           // 1-based line in the brief file
}

// ParseRequirements scans a brief file: every top-level bullet under
// "## Constraints" and "## Acceptance", and every "### " heading under
// "## Features", is one requirement. It is code, not a model, so the list a
// plan is checked against cannot drift from the brief. Returned in file order.
func ParseRequirements(src []byte) []Requirement {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	text = strings.TrimPrefix(text, string([]byte{0xef, 0xbb, 0xbf}))
	lines := strings.Split(text, "\n")
	start := 0
	if lines[0] == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				start = i + 1
				break
			}
		}
	}

	out := []Requirement{}
	counts := map[ReqKind]int{}
	prefix := map[ReqKind]string{ReqConstraint: "C", ReqAcceptance: "A", ReqFeature: "F"}
	var cur *Requirement
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(strings.Join(body, "\n"))
		out = append(out, *cur)
		cur, body = nil, nil
	}
	open := func(kind ReqKind, line int) {
		flush()
		counts[kind]++
		cur = &Requirement{ID: fmt.Sprintf("%s%d", prefix[kind], counts[kind]), Kind: kind, Line: line}
	}

	section := ""
	inFence := false
	for i := start; i < len(lines); i++ {
		line := lines[i]
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			section = strings.TrimSpace(line[3:])
			continue
		}
		switch section {
		case "Features":
			if !inFence && strings.HasPrefix(line, "### ") {
				open(ReqFeature, i+1)
				cur.Name = strings.TrimSpace(line[4:])
				continue
			}
			if cur != nil {
				body = append(body, line)
			}
		case "Constraints", "Acceptance":
			kind := ReqConstraint
			if section == "Acceptance" {
				kind = ReqAcceptance
			}
			switch {
			case !inFence && (strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")):
				open(kind, i+1)
				body = append(body, strings.TrimSpace(line[2:]))
			case t == "":
				// blank lines are dropped from a bullet's text
			case line[0] == ' ' || line[0] == '\t':
				if cur != nil {
					body = append(body, strings.TrimRight(line, " \t"))
				}
			default:
				// A non-blank line at column 0 that is not a bullet ends the
				// current bullet and is not a requirement itself.
				flush()
			}
		}
	}
	flush()
	return out
}
```

- [ ] **Step 5: Run the requirements tests**

Run: `cd gophermind-lib && go test ./briefv2/planner/ -run 'TestParseRequirements' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `--- PASS` for `TestParseRequirements`, `TestParseRequirementsEdgeCases`, and `TestParseRequirementsAIVentureStudio`, then `ok`.

- [ ] **Step 6: Write the failing coverage tests** (`briefv2/planner/coverage_check_test.go`)

`TestGoldenFailingPlan` is the regression test for the failure that motivated this work. Its gap list is exact on purpose: if a change makes the checker accept that plan, the test must fail.

```go
package planner_test

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/tree"
)

var testReqs = []planner.Requirement{
	{ID: "F1", Kind: planner.ReqFeature, Name: "Greeting", Text: "Says hello.", Line: 10},
	{ID: "C1", Kind: planner.ReqConstraint, Text: "Standard library only.", Line: 20},
	{ID: "A1", Kind: planner.ReqAcceptance, Text: "`go build ./...` succeeds.", Line: 30},
}

var testNodes = []planner.PlanNode{
	{ID: "gm-1", Kind: tree.KindRoot, Title: "Root"},
	{ID: "greeting", Kind: tree.KindComponent, Parent: "gm-1", Title: "Greeting"},
	{ID: "types", Kind: tree.KindComponent, Parent: "gm-1", Title: "Types"},
	{ID: "fn-greet", Kind: tree.KindFunction, Parent: "greeting", Title: "Greet", File: "internal/greet/greet.go"},
}

func build(name string) planner.RootTest {
	return planner.RootTest{Requirement: "A1", Name: name, Given: "the tree is verified", Expect: "exit 0", Command: "go build ./..."}
}

func TestCheckCoverage(t *testing.T) {
	full := planner.CoverageReply{
		Map: []planner.MapEntry{
			{Requirement: "F1", Nodes: []string{"greeting"}},
			{Requirement: "C1", Nodes: []string{"fn-greet"}},
			{Requirement: "A1", Nodes: nil},
		},
		RootTests: []planner.RootTest{build("builds")},
	}
	cases := []struct {
		name    string
		reply   planner.CoverageReply
		wantGap map[string]string // requirement id -> reason
	}{
		{"everything covered", full, map[string]string{}},
		{"a requirement missing from the reply",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "C1", Nodes: []string{"fn-greet"}}}, RootTests: full.RootTests},
			map[string]string{"F1": "not mapped"}},
		{"only node does not exist",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{"fn-nope"}}}, RootTests: full.RootTests},
			map[string]string{"C1": "names unknown node fn-nope; no node and no root test"}},
		{"constraint with neither node nor root test",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{}}}, RootTests: full.RootTests},
			map[string]string{"C1": "no node and no root test"}},
		{"constraint covered by a root test alone",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}},
				RootTests: []planner.RootTest{build("builds"), {Requirement: "C1", Name: "no third-party modules", Command: "test -z \"$(go list -m all | tail -n +2)\""}}},
			map[string]string{}},
		{"acceptance bullet with nodes but no root test",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{"fn-greet"}}, {Requirement: "A1", Nodes: []string{"fn-greet"}}}},
			map[string]string{"A1": "acceptance bullet has no root test"}},
		{"feature mapped only to the root node",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"gm-1"}}, {Requirement: "C1", Nodes: []string{"fn-greet"}}}, RootTests: full.RootTests},
			map[string]string{"F1": "the root node alone covers nothing; no covering node"}},
		{"feature mapped to a component with no functions",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "F1", Nodes: []string{"types"}}, {Requirement: "C1", Nodes: []string{"fn-greet"}}}, RootTests: full.RootTests},
			map[string]string{"F1": "component types has no functions; no covering node"}},
		{"a feature is not covered by a root test",
			planner.CoverageReply{Map: []planner.MapEntry{{Requirement: "C1", Nodes: []string{"fn-greet"}}},
				RootTests: []planner.RootTest{build("builds"), {Requirement: "F1", Name: "greets", Command: "go run ./cmd/greet"}}},
			map[string]string{"F1": "no covering node"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			covered, gaps := planner.CheckCoverage(testReqs, testNodes, c.reply)
			got := map[string]string{}
			for _, g := range gaps {
				got[g.Requirement] = g.Reason
				if g.Text == "" || g.Line == 0 {
					t.Errorf("gap %s lost its text or line: %+v", g.Requirement, g)
				}
			}
			if !reflect.DeepEqual(got, c.wantGap) {
				t.Fatalf("gaps = %v, want %v", got, c.wantGap)
			}
			if len(covered)+len(gaps) != len(testReqs) {
				t.Fatalf("%d covered + %d gaps, want %d requirements in total", len(covered), len(gaps), len(testReqs))
			}
		})
	}

	covered, _ := planner.CheckCoverage(testReqs, testNodes, full)
	want := []planner.Covered{
		{Requirement: "F1", Nodes: []string{"greeting"}, RootTests: []string{}},
		{Requirement: "C1", Nodes: []string{"fn-greet"}, RootTests: []string{}},
		{Requirement: "A1", Nodes: []string{}, RootTests: []string{"builds"}},
	}
	if !reflect.DeepEqual(covered, want) {
		t.Fatalf("covered = %+v, want %+v", covered, want)
	}
}

func TestParseCoverageReply(t *testing.T) {
	ok := `{"map":[{"requirement":"C1","nodes":["fn-greet"]}],"root_tests":[{"requirement":"A1","name":"builds","given":"g","expect":"e","command":"go build ./..."}],"functions":[]}`
	r, err := planner.ParseCoverageReply(ok, testReqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Map) != 1 || len(r.RootTests) != 1 || r.RootTests[0].Command != "go build ./..." {
		t.Fatalf("reply = %+v", r)
	}
	bad := []struct{ name, text, wantErr string }{
		{"not json", `here is the mapping`, "not valid JSON"},
		{"unknown requirement in map", `{"map":[{"requirement":"C9","nodes":[]}]}`, `unknown requirement "C9"`},
		{"unknown requirement in root test", `{"root_tests":[{"requirement":"A9","name":"x","command":"true"}]}`, `unknown requirement "A9"`},
		{"root test without a name", `{"root_tests":[{"requirement":"A1","name":" ","command":"true"}]}`, "has no name"},
		{"root test without a command", `{"root_tests":[{"requirement":"A1","name":"builds","command":""}]}`, "has no command"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := planner.ParseCoverageReply(c.text, testReqs)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	a := planner.CoverageReply{
		Map:       []planner.MapEntry{{Requirement: "C1", Nodes: []string{"fn-a"}}, {Requirement: "F1", Nodes: nil}},
		RootTests: []planner.RootTest{build("builds")},
	}
	b := planner.CoverageReply{
		Map:       []planner.MapEntry{{Requirement: "F1", Nodes: []string{"greeting"}}, {Requirement: "C1", Nodes: []string{"fn-a", "fn-b"}}, {Requirement: "A1", Nodes: []string{}}},
		RootTests: []planner.RootTest{build("builds"), build("builds twice")},
	}
	got := a.Merge(b)
	want := planner.CoverageReply{
		Map: []planner.MapEntry{
			{Requirement: "C1", Nodes: []string{"fn-a", "fn-b"}},
			{Requirement: "F1", Nodes: []string{"greeting"}},
			{Requirement: "A1", Nodes: []string{}},
		},
		RootTests: []planner.RootTest{build("builds"), build("builds twice")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge = %+v\nwant   %+v", got, want)
	}
	if len(a.Map[0].Nodes) != 1 {
		t.Fatal("Merge changed its receiver")
	}
}

func TestPathWarnings(t *testing.T) {
	src := []byte("Packages: `cmd/greet`, `internal/greet`, `internal/store/`, `internal/...`, `cmd/greet` again, `go build ./cmd/greet`.\n")
	nodes := []planner.PlanNode{
		{ID: "fn-greet", Kind: tree.KindFunction, Parent: "greeting", File: "internal/greet/greet.go"},
		{ID: "fn-main", Kind: tree.KindFunction, Parent: "greeting", File: "cmd/greeter/main.go"},
		{ID: "store", Kind: tree.KindComponent, File: "internal/store/store.go"},
	}
	got := planner.PathWarnings(src, nodes)
	want := []string{
		"`cmd/greet` is named in the brief but no function file is under it",
		"`internal/store/` is named in the brief but no function file is under it",
		"`internal/...` is named in the brief but no function file is under it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q\nwant       %q", got, want)
	}
}

func TestCommandWarnings(t *testing.T) {
	reqs := []planner.Requirement{
		{ID: "A1", Kind: planner.ReqAcceptance, Text: "`go build ./...` succeeds."},
		{ID: "A2", Kind: planner.ReqAcceptance, Text: "`go vet ./...` reports nothing."},
		{ID: "A3", Kind: planner.ReqAcceptance, Text: "Two users cannot read each other's data: `GET` returns 404."},
		{ID: "A4", Kind: planner.ReqAcceptance, Text: "`go test ./...` passes."},
		{ID: "C1", Kind: planner.ReqConstraint, Text: "`gofmt` clean."},
	}
	reply := planner.CoverageReply{RootTests: []planner.RootTest{
		{Requirement: "A1", Name: "builds", Command: "sh -c 'go build ./... && echo ok'"},
		{Requirement: "A2", Name: "vet", Command: "go vet ./cmd/..."},
		{Requirement: "A3", Name: "isolation", Command: "go test ./internal/tenancy"},
		{Requirement: "C1", Name: "fmt", Command: "true"},
	}}
	got := planner.CommandWarnings(reqs, reply)
	want := []string{"A2: no root test runs the command the bullet begins with (`go vet ./...`)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// The plan the v1 planner produced for AI Venture Studio on 2026-09-29 was
// approved with most of the brief's constraints and all of its acceptance
// bullets dropped. The checker must say so. See testdata/aivs/README.md.
func TestGoldenFailingPlan(t *testing.T) {
	src, err := os.ReadFile(aivsBrief)
	if err != nil {
		t.Fatal(err)
	}
	reqs := planner.ParseRequirements(src)
	var nodes []planner.PlanNode
	readJSON(t, "testdata/aivs/nodes.json", &nodes)
	raw, err := os.ReadFile("testdata/aivs/mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := planner.ParseCoverageReply(string(raw), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 121 {
		t.Fatalf("nodes.json has %d nodes, want 121 (1 root, 21 phases, 99 steps)", len(nodes))
	}

	covered, gaps := planner.CheckCoverage(reqs, nodes, reply)
	var ids []string
	text := map[string]string{}
	for _, g := range gaps {
		ids = append(ids, g.Requirement)
		text[g.Requirement] = g.Text
	}
	sort.Strings(ids)
	want := []string{"A1", "A10", "A11", "A12", "A2", "A3", "A4", "A5", "A6", "A7", "A8", "A9", "C1", "C10", "C2", "C4", "C5", "C7", "C9"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("gaps = %v\nwant   %v", ids, want)
	}
	if len(covered) != 22 {
		t.Fatalf("covered = %d, want 22 (19 features and C3, C6, C8)", len(covered))
	}
	for id, part := range map[string]string{
		"C1":  "`gofmt` and `go vet` clean",
		"C4":  "A test scans all SQL under `internal/`",
		"C9":  "Handlers never exceed 60 lines",
		"A8":  "`stage.advanced` in that order",
		"A9":  "Financial round trip",
		"A10": "`?as_of=` before the transfer",
		"A12": "returns 404",
	} {
		if !strings.Contains(text[id], part) {
			t.Errorf("gap %s should be the requirement containing %q, got %q", id, part, text[id])
		}
	}

	warnings := planner.PathWarnings(src, nodes)
	joined := strings.Join(warnings, "\n")
	for _, tok := range []string{"internal/tenancy", "internal/httpapi", "internal/openapi", "internal/pnl"} {
		if !strings.Contains(joined, "`"+tok+"`") {
			t.Errorf("no path warning for %s; warnings:\n%s", tok, joined)
		}
	}
	// The v1 plan did put its main package under cmd/venture-server (beside a
	// stray cmd/fake-llm), so that path is not among the warnings.
	if strings.Contains(joined, "`cmd/venture-server`") {
		t.Errorf("cmd/venture-server has a function file under it and must not be warned about")
	}
	if len(warnings) != 18 {
		t.Errorf("%d path warnings, want 18:\n%s", len(warnings), joined)
	}
}
```

- [ ] **Step 7: Run to verify it fails**

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | tail -4`
Expected: `undefined: planner.CoverageReply` (and similar), `FAIL ... [build failed]`.

- [ ] **Step 8: Implement `coverage_check.go`**

```go
package planner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/tree"
)

// PlanNode is what the coverage check needs to know about one node of a plan.
type PlanNode struct {
	ID     string    `json:"id"`
	Kind   tree.Kind `json:"kind"`
	Parent string    `json:"parent,omitempty"`
	Title  string    `json:"title"`
	File   string    `json:"file,omitempty"` // contract.file, function nodes only
}

// MapEntry names the nodes whose code satisfies one requirement.
type MapEntry struct {
	Requirement string   `json:"requirement"`
	Nodes       []string `json:"nodes"`
}

// RootTest is an acceptance test for the root node, proving one requirement.
type RootTest struct {
	Requirement string `json:"requirement"`
	Name        string `json:"name"`
	Given       string `json:"given"`
	Expect      string `json:"expect"`
	Command     string `json:"command"`
}

// CoverageReply is the model's proposed mapping from requirements to the plan.
type CoverageReply struct {
	Map       []MapEntry `json:"map"`
	RootTests []RootTest `json:"root_tests"`
}

// Gap is a requirement nothing in the plan covers, and why.
type Gap struct {
	Requirement string `json:"requirement"`
	Line        int    `json:"line"`
	Text        string `json:"text"`
	Reason      string `json:"reason"`
}

// Covered is a requirement with what covers it.
type Covered struct {
	Requirement string   `json:"requirement"`
	Nodes       []string `json:"nodes"`
	RootTests   []string `json:"root_tests"` // test names
}

// ParseCoverageReply decodes a reply that has already had fences and prose
// stripped. An error means the reply is malformed: invalid JSON, a map entry
// or root test naming a requirement the brief does not have, or a root test
// with no name or no command.
func ParseCoverageReply(text string, reqs []Requirement) (CoverageReply, error) {
	var r CoverageReply
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		return CoverageReply{}, fmt.Errorf("coverage reply is not valid JSON: %w", err)
	}
	known := map[string]bool{}
	for _, q := range reqs {
		known[q.ID] = true
	}
	for _, m := range r.Map {
		if !known[m.Requirement] {
			return CoverageReply{}, fmt.Errorf("coverage reply: map names unknown requirement %q", m.Requirement)
		}
	}
	for _, t := range r.RootTests {
		if !known[t.Requirement] {
			return CoverageReply{}, fmt.Errorf("coverage reply: root test names unknown requirement %q", t.Requirement)
		}
		if strings.TrimSpace(t.Name) == "" {
			return CoverageReply{}, fmt.Errorf("coverage reply: a root test for %s has no name", t.Requirement)
		}
		if strings.TrimSpace(t.Command) == "" {
			return CoverageReply{}, fmt.Errorf("coverage reply: root test %q for %s has no command", t.Name, t.Requirement)
		}
	}
	return r, nil
}

// Merge returns r with other's entries added. Node lists for the same
// requirement are unioned, keeping order; a root test is appended unless one
// with the same requirement and name is already there.
func (r CoverageReply) Merge(other CoverageReply) CoverageReply {
	out := CoverageReply{Map: []MapEntry{}, RootTests: []RootTest{}}
	at := map[string]int{}
	add := func(m MapEntry) {
		i, ok := at[m.Requirement]
		if !ok {
			at[m.Requirement] = len(out.Map)
			out.Map = append(out.Map, MapEntry{Requirement: m.Requirement, Nodes: []string{}})
			i = len(out.Map) - 1
		}
		for _, n := range m.Nodes {
			if !contains(out.Map[i].Nodes, n) {
				out.Map[i].Nodes = append(out.Map[i].Nodes, n)
			}
		}
	}
	for _, m := range r.Map {
		add(m)
	}
	for _, m := range other.Map {
		add(m)
	}
	seen := map[string]bool{}
	for _, t := range append(append([]RootTest{}, r.RootTests...), other.RootTests...) {
		k := t.Requirement + "\x00" + t.Name
		if !seen[k] {
			seen[k] = true
			out.RootTests = append(out.RootTests, t)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// CheckCoverage decides, without a model, which requirements the plan covers.
// A covering node is a function node, or a component node with at least one
// function node under it; the root node and unknown ids cover nothing. A
// constraint needs a covering node or a root test, a feature needs a covering
// node, and an acceptance bullet needs a root test. covered and gaps are both
// in reqs order, and every requirement is in exactly one of them.
func CheckCoverage(reqs []Requirement, nodes []PlanNode, reply CoverageReply) (covered []Covered, gaps []Gap) {
	byID := map[string]PlanNode{}
	hasFunction := map[string]bool{}
	for _, n := range nodes {
		byID[n.ID] = n
		if n.Kind == tree.KindFunction {
			hasFunction[n.Parent] = true
		}
	}
	mapped := map[string]bool{}
	named := map[string][]string{}
	for _, m := range reply.Map {
		mapped[m.Requirement] = true
		for _, id := range m.Nodes {
			if !contains(named[m.Requirement], id) {
				named[m.Requirement] = append(named[m.Requirement], id)
			}
		}
	}
	tests := map[string][]string{}
	for _, t := range reply.RootTests {
		tests[t.Requirement] = append(tests[t.Requirement], t.Name)
	}

	covered, gaps = []Covered{}, []Gap{}
	for _, q := range reqs {
		valid := []string{}
		var why []string
		for _, id := range named[q.ID] {
			n, ok := byID[id]
			switch {
			case !ok:
				why = append(why, "names unknown node "+id)
			case n.Kind == tree.KindRoot:
				why = append(why, "the root node alone covers nothing")
			case n.Kind == tree.KindComponent && !hasFunction[id]:
				why = append(why, "component "+id+" has no functions")
			default:
				valid = append(valid, id)
			}
		}
		names := tests[q.ID]
		if names == nil {
			names = []string{}
		}
		missing := ""
		switch q.Kind {
		case ReqAcceptance:
			if len(names) == 0 {
				missing = "acceptance bullet has no root test"
			}
		case ReqFeature:
			if len(valid) == 0 {
				missing = "no covering node"
			}
		default:
			if len(valid) == 0 && len(names) == 0 {
				missing = "no node and no root test"
			}
		}
		if missing == "" {
			covered = append(covered, Covered{Requirement: q.ID, Nodes: valid, RootTests: names})
			continue
		}
		if q.Kind != ReqAcceptance && !mapped[q.ID] && len(names) == 0 {
			missing = "not mapped"
		}
		gaps = append(gaps, Gap{Requirement: q.ID, Line: q.Line, Text: q.Text, Reason: strings.Join(append(why, missing), "; ")})
	}
	return covered, gaps
}

var (
	backtickRE = regexp.MustCompile("`([^`\n]+)`")
	pathRE     = regexp.MustCompile(`^(cmd|internal)/[A-Za-z0-9_./-]+$`)
)

// PathWarnings lists each backticked cmd/ or internal/ path in the brief that
// no function node's file sits under. A plan that builds cmd/server when the
// brief names cmd/venture-server shows up here.
func PathWarnings(briefSrc []byte, nodes []PlanNode) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range backtickRE.FindAllSubmatch(briefSrc, -1) {
		tok := string(m[1])
		if !pathRE.MatchString(tok) || seen[tok] {
			continue
		}
		seen[tok] = true
		dir := strings.TrimRight(tok, "/")
		found := false
		for _, n := range nodes {
			if n.Kind == tree.KindFunction && (n.File == dir || strings.HasPrefix(n.File, dir+"/")) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, "`"+tok+"` is named in the brief but no function file is under it")
		}
	}
	return out
}

// CommandWarnings lists each acceptance requirement that begins with a
// backticked command none of its root tests runs. A requirement with no root
// test at all is a gap, not a warning, and is skipped here.
func CommandWarnings(reqs []Requirement, reply CoverageReply) []string {
	out := []string{}
	for _, q := range reqs {
		if q.Kind != ReqAcceptance || !strings.HasPrefix(q.Text, "`") {
			continue
		}
		end := strings.Index(q.Text[1:], "`")
		if end < 0 {
			continue
		}
		span := q.Text[1 : 1+end]
		has, runs := false, false
		for _, t := range reply.RootTests {
			if t.Requirement == q.ID {
				has = true
				if strings.Contains(t.Command, span) {
					runs = true
				}
			}
		}
		if has && !runs {
			out = append(out, fmt.Sprintf("%s: no root test runs the command the bullet begins with (`%s`)", q.ID, span))
		}
	}
	return out
}
```

- [ ] **Step 9: Run everything for this task and commit**

Run: `cd gophermind-lib && gofmt -l briefv2/planner && go vet ./briefv2/planner/ && go test ./briefv2/planner/ -race -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `gofmt` prints nothing; `--- PASS` for 9 tests (`TestParseRequirements`, `TestParseRequirementsEdgeCases`, `TestParseRequirementsAIVentureStudio`, `TestCheckCoverage`, `TestParseCoverageReply`, `TestMerge`, `TestPathWarnings`, `TestCommandWarnings`, `TestGoldenFailingPlan`), then `ok`.

```bash
git add gophermind-lib/briefv2/planner/requirements.go gophermind-lib/briefv2/planner/requirements_test.go \
        gophermind-lib/briefv2/planner/coverage_check.go gophermind-lib/briefv2/planner/coverage_check_test.go
git commit -m "feat(briefv2): parse brief requirements and check a plan covers them

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Planner foundation, Load, Clarify, and Contract in passes

**Files:**
- Modify: `gophermind-lib/briefv2/schema/schema.go` (add `Raw`), `gophermind-lib/briefv2/schema/schema_test.go`
- Modify: `gophermind-lib/briefv2/tree/store.go` (skip `_state/`, two more artifact names), `gophermind-lib/briefv2/tree/store_test.go`
- Create: `gophermind-lib/briefv2/planner/state.go`, `answers.go`, `parse.go`, `prompts.go`, `fixture.go`, `planner.go`, `status.go`, `load.go`, `clarify.go`, `contract_stage.go`
- Create: `gophermind-lib/briefv2/planner/prompts/clarify.md`, `contract_outline.md`, `contract_component.md`
- Create: `gophermind-lib/briefv2/planner/testdata/greeter/brief.md`, `clarify.txt`, `contract.outline.txt`, `contract.types.txt`, `contract.greeting.txt`, `contract.farewell.txt`
- Test: `gophermind-lib/briefv2/planner/harness_test.go`, `parse_test.go`, `fixture_test.go`, `load_test.go`, `clarify_test.go`, `contract_stage_test.go`, `contract_internal_test.go`

**Interfaces:**
- Consumes: `router.CallInfo{RunID, Stage, NodeID, Tier, Scope, TaskType, NodeClass, ...}`, `router.Result`, `router.TierStrong`, `router.ScopeBrief`, `router.ScopeComponent`, `*router.ChainExhausted` with `OnlyPrivacy()`, `(*router.Router).CallParsed`, `(*router.Router).LedgerErrors` (Task 5); `provider.Request`, `provider.Message`, `provider.RoleSystem`, `provider.RoleUser`, `provider.NewFake`, `provider.ModelInfo` (Task 3); `settings.Config`, `settings.Default`, `settings.Tiers`, `settings.Private` (Task 4); `human.Gate`, `human.Question`, `human.Answer`, `human.ErrWaiting` (Task 6); `events.Sink`, `events.Nop`, `events.Event`, `events.Kind*` (Task 3); `blackboard.Blackboard` (Task 1); `ledger.Filter{TaskType}` and `ledger.Call.TaskType` (Task 2); `ParseRequirements`, `Requirement`, `ReqConstraint` (Task 7); the existing `brief.Parse`, `*brief.InvalidError`, `rundir.Create`, `rundir.ErrExists`, `vault.RunScope`, `vault.HarnessScope`, `contract.Load`, `config.Dir`.
- Produces: `schema.Raw(kind schema.Kind) ([]byte, error)`.
- Produces: `planner.Caller` (`CallParsed(ctx, router.CallInfo, provider.Request, parse func(text string) error) (router.Result, error)`); `planner.Secrets` (`Get(scope, name string) (string, bool)`, `Set(scope, name, value string) error`); `planner.Deps{Caller, Gate, Sink, Board, Settings, OpenSecrets func() (Secrets, error), PromptSecret func(name, purpose string) (string, error), LedgerErrors func() int, Now func() time.Time}`; `planner.Options{BriefPath, RunID string; Yes, AllowPublic bool; StopAfter string}`; `planner.Outcome` with `planner.Done` and `planner.Waiting`; `planner.New(Deps) *Planner`; `(*Planner).Run(ctx, Options) (Outcome, error)`.
- Produces: `planner.RunRecord{RunID, Repo, RunDir, BriefPath, StartedAt string}`; `planner.LookupRun(runID string) (RunRecord, error)`; `planner.StageState{Name string; Done bool}`; `planner.Status{RunID, RunDir, Repo string; Stages []StageState; Waiting string; Requirements, Covered int; AllowPublic bool; LedgerErrors int}`; `planner.ReadStatus(runID string) (Status, error)`.
- Produces: `planner.StripReply(text string) string`; `planner.Question(text string) (q string, ok bool)`; `planner.StageOf(provider.Request) string`; `planner.FixtureProvider(dirs ...string) (*provider.Fake, error)`; `planner.FixtureSettings() *settings.Config`.
- Produces, package-internal, for Tasks 9 to 11: `type run struct{ id, dir, repo string; src []byte; brief *brief.Brief; reqs []Requirement; opts Options; status runStatus }` with `path(name string) string` and `saveStatus() error`; `type stage struct{ name; run func(*Planner, context.Context, *run) error; done func(*run) bool }` and the ordered `var stages []stage`; `type callSpec struct{ stage, taskType, nodeID, nodeClass string; scope router.Scope; maxTokens int }`; `(*Planner).call(ctx, r, cs, prompt, parse) error`; `(*Planner).callAsking(ctx, r, cs, prompt, parse) error` (the `QUESTION:` protocol); `(*Planner).emit(kind, stage, nodeID, msg string)`; `render(name string, data map[string]string) (string, error)`; `writeJSON(path, v) error`; `readJSON(path, v) (found bool, err error)`; `writeFileAtomic`; `exists(path) bool`; `loadAnswers(r) (answersFile, error)`; `answersText(answersFile) string`; `parseSignature(sig string) (*ast.FuncDecl, error)`; `validateContractDoc(doc map[string]any, briefID string) (*contract.Contracts, error)`; `contractItemSchemas(names ...string) (string, error)`; `copyDoc`, `mustJSON`, `objects`, `strList`, `slug`, `briefSection(r, component) string`; the file name constants in `state.go`; the `maxTokens*` constants.

What this task builds. The planner is a list of stages run in order; a stage is skipped when its output already exists, which is all that `resume` is. Every model call goes through one helper that names the stage in a system message (`GopherMind planner. Stage: <stage>`), so the offline fixture provider can serve a canned reply per stage and tests can see which stages were called. `Options.StopAfter` ends a run after a named stage; the tests of this and the next three tasks use it so that a stage can be tested before the later ones exist.

The Contract stage runs in passes (spec section 9, "Size: no ceiling, more calls"): one `contract:outline` call returns the module, the conventions, the components and the shared types; then one `contract:<component>` call per component returns that component's functions, and is repeated while the reply says `"more": true`. Progress is kept in `_state/contract.json`; `contracts.json` is written only when every pass is done. Because a component id becomes a stage name and a folder, `logs` and `outline` are refused as component ids.

Two things differ from a naive reading of the spec, both deliberate: a function's `component` is set by the harness from the pass it arrived in (the model never writes it), and a signature that does not parse as Go is refused here, in the pass that wrote it, because a later stage could not fix it.

- [ ] **Step 1: Write the failing test for `schema.Raw`**

Append to `gophermind-lib/briefv2/schema/schema_test.go`:

```go

func TestRawReturnsTheEmbeddedSchema(t *testing.T) {
	raw, err := schema.Raw(schema.KindContract)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Raw(KindContract) is not JSON: %v", err)
	}
	if doc["$id"] != "https://gophermind.local/schema/contract/2.0" {
		t.Errorf("$id = %v", doc["$id"])
	}
	if _, err := schema.Raw("nope"); err == nil {
		t.Error("unknown kind must error")
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/schema/ 2>&1 | head -4`
Expected: FAIL, `undefined: schema.Raw`.

- [ ] **Step 2: Implement `schema.Raw`**

Append to `gophermind-lib/briefv2/schema/schema.go`:

```go

// Raw returns the embedded schema file for kind, byte for byte. The Contract
// prompt shows it to the model so the reply can be validated against the same
// text.
func Raw(kind Kind) ([]byte, error) {
	s, ok := sources[kind]
	if !ok {
		return nil, fmt.Errorf("schema: unknown kind %q", kind)
	}
	return files.ReadFile(s.file)
}
```

Run: `cd gophermind-lib && gofmt -l briefv2/schema && go test ./briefv2/schema/ 2>&1 | tail -2`
Expected: `gofmt` prints nothing; `ok`.

- [ ] **Step 3: Write the failing test for the tree store**

The planner keeps `requirements.json`, `coverage.json` and a `_state/` folder in the run folder. `tree.Store.Load` reads every `.json` file as a node, so it must learn to skip them.

Append to `gophermind-lib/briefv2/tree/store_test.go`:

```go

func TestLoadSkipsPlannerArtifacts(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	if err := s.Write(fn(t, "fn-a", "comp", 0)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"requirements.json", "coverage.json", "_state/decomposed.json", "_state/status.json"} {
		p := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(`{"not":"a node"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := s.Load()
	if err != nil {
		t.Fatalf("planner artifacts must not be read as nodes: %v", err)
	}
	if len(tr.Nodes) != 1 {
		t.Errorf("loaded %d nodes, want 1", len(tr.Nodes))
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/tree/ -run TestLoadSkipsPlannerArtifacts 2>&1 | head -4`
Expected: FAIL, `planner artifacts must not be read as nodes: tree: _state/decomposed.json: jsonschema validation failed`.

- [ ] **Step 4: Teach the tree store to skip them**

In `gophermind-lib/briefv2/tree/store.go` make three replacements.

Replace

```go
var notNodes = map[string]bool{"contracts.json": true, "answers.json": true, "approval.json": true, "report.json": true}
```

with

```go
var notNodes = map[string]bool{"contracts.json": true, "answers.json": true, "approval.json": true, "report.json": true,
	"requirements.json": true, "coverage.json": true}
```

Replace

```go
			if rel == "logs" {
```

with

```go
			if rel == "logs" || rel == "_state" {
```

Replace the comment above `Load`

```go
// Load reads every node file, skipping run artifacts, and rejects a node that
// is stored somewhere other than its own Path().
```

with

```go
// Load reads every node file, skipping run artifacts, logs/ and the planner's
// _state/ working folder, and rejects a node that is stored somewhere other
// than its own Path().
```

Run: `cd gophermind-lib && gofmt -l briefv2/tree && go test ./briefv2/tree/ 2>&1 | tail -2`
Expected: `gofmt` prints nothing; `ok`.

- [ ] **Step 5: Write the failing tests for reply parsing** (`briefv2/planner/parse_test.go`)

```go
package planner_test

import (
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

func TestStripReply(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bare object", `{"a": 1}`, `{"a": 1}`},
		{"bare array with spaces", "  [1, 2]\n", `[1, 2]`},
		{"json fence", "```json\n{\"a\": 1}\n```", `{"a": 1}`},
		{"plain fence with prose", "Here it is:\n```\n[1]\n```\nHope that helps.", `[1]`},
		{"second fence is the json", "```text\nnot json\n```\n```json\n{\"a\": 2}\n```", `{"a": 2}`},
		{"no fence parses, first wins", "```\nnot json\n```\n```\nalso not\n```", `not json`},
		{"prose before and after", "Sure. {\"a\": [1, 2]} Done.", `{"a": [1, 2]}`},
		{"brackets inside strings", `Result: {"a": "x } y ] z"} ok`, `{"a": "x } y ] z"}`},
		{"array with prose", "The list: [\"a\", \"b\"] as asked", `["a", "b"]`},
		{"valid json holding a fence is left alone", "{\"test_file\": \"x := `a`\\n```\\n\"}", "{\"test_file\": \"x := `a`\\n```\\n\"}"},
		{"unclosed fence", "```json\n{\"a\": 1}", `{"a": 1}`},
		{"nothing to find", "no json here", "no json here"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := planner.StripReply(c.in); got != c.want {
				t.Errorf("StripReply(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestQuestion(t *testing.T) {
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"marker alone on a line", "QUESTION:\nShould names be trimmed?", "Should names be trimmed?", true},
		{"prose before the marker", "I need one thing.\n  QUESTION:  \nTrim or not?\nIt changes the tests.", "Trim or not?\nIt changes the tests.", true},
		{"marker with text on the same line is not the protocol", "QUESTION: trim?", "", false},
		{"marker with nothing after it", "QUESTION:\n\n", "", false},
		{"ordinary reply", `[{"id": "fn-a"}]`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := planner.Question(c.in)
			if got != c.want || ok != c.ok {
				t.Errorf("Question(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | head -4`
Expected:

```text
# gophermind/gophermind-lib/briefv2/planner_test [gophermind/gophermind-lib/briefv2/planner.test]
briefv2/planner/parse_test.go:26:22: undefined: planner.StripReply
briefv2/planner/parse_test.go:46:23: undefined: planner.Question
FAIL	gophermind/gophermind-lib/briefv2/planner [build failed]
```

- [ ] **Step 6: Implement `parse.go`**

```go
package planner

import (
	"encoding/json"
	"strings"
)

// StripReply removes what models wrap around the JSON they were asked for: a
// markdown fence, a sentence before it, a sentence after it. A reply that is
// already valid JSON is returned as it is, so a fence inside a JSON string
// (Go source in a test file) is never mistaken for a wrapper.
func StripReply(text string) string {
	t := strings.TrimSpace(text)
	if json.Valid([]byte(t)) {
		return t
	}
	if blocks := fencedBlocks(t); len(blocks) > 0 {
		for _, b := range blocks {
			if json.Valid([]byte(b)) {
				return b
			}
		}
		return blocks[0]
	}
	start := strings.IndexAny(t, "{[")
	if start < 0 {
		return t
	}
	closer := "}"
	if t[start] == '[' {
		closer = "]"
	}
	end := strings.LastIndex(t, closer)
	if end < start {
		return t[start:]
	}
	return t[start : end+1]
}

// fencedBlocks returns the trimmed content of every ``` block, in order. An
// unclosed fence runs to the end of the text.
func fencedBlocks(t string) []string {
	var out []string
	var cur []string
	in := false
	for _, line := range strings.Split(t, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if in {
				out = append(out, strings.TrimSpace(strings.Join(cur, "\n")))
				cur = nil
			}
			in = !in
			continue
		}
		if in {
			cur = append(cur, line)
		}
	}
	if in {
		out = append(out, strings.TrimSpace(strings.Join(cur, "\n")))
	}
	return out
}

// Question finds the pause-and-ask marker: a line that is exactly QUESTION:
// with the question on the lines after it.
func Question(text string) (q string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "QUESTION:" {
			q = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			return q, q != ""
		}
	}
	return "", false
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ -run 'StripReply|Question' 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 7: Write the fixture brief, the test harness, and the failing fixture and Load tests**

The end-to-end fixture (decision L4) is a tiny invented brief, `greeter`. Its `repo` is the placeholder `REPO_DIR`; the harness replaces it with a temp directory.

`briefv2/planner/testdata/greeter/brief.md`:

```text
---
spec_version: "2.0"
id: gm-2026-09-29-900
title: Greeter
language: go
repo: REPO_DIR
base_branch: main
landing: diff_only
on_ambiguity: halt
---

## Overview

A tiny library that builds greeting and farewell messages for a name. It is the
fixture brief for the planner's offline tests: two features, two constraints,
three acceptance bullets.

## Features

### Greeting

`Greet(name)` returns `Hello, <name>!`.

Acceptance criteria:

- An empty name returns a `*NameError`.

### Farewell

`Farewell(name)` returns `Goodbye, <name>!`.

Acceptance criteria:

- An empty name returns a `*NameError`.

## Architecture

- `internal/greet`: pure functions that build messages. No I/O.

## Data

Name: a string that is not empty after trimming spaces.

## Constraints

- Standard library only.
- `gofmt` clean and `go vet` clean.

## Out of scope

- Localisation.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
```

`briefv2/planner/harness_test.go` (shared by every planner test from here on: a temp repo, a temp config dir, a real router over the fixture provider, a real ledger and blackboard, and a scripted human gate):

```go
package planner_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

const (
	greeter   = "testdata/greeter"
	greeterID = "gm-2026-09-29-900"
)

// scriptGate is a human gate that answers at once from a script and records
// what it was asked. A nil answer function answers every question "yes".
type scriptGate struct {
	mu       sync.Mutex
	answer   func(q human.Question) string
	decision human.Decision
	err      error // returned by every call when set (human.ErrWaiting for a gate nobody answers)
	asked    []human.Question
	plans    []human.PlanSummary
}

var _ human.Gate = (*scriptGate)(nil)

func (g *scriptGate) Ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, qs...)
	if g.err != nil {
		return nil, g.err
	}
	out := make([]human.Answer, len(qs))
	for i, q := range qs {
		text := "yes"
		if g.answer != nil {
			text = g.answer(q)
		}
		out[i] = human.Answer{ID: q.ID, Text: text}
	}
	return out, nil
}

func (g *scriptGate) Approve(ctx context.Context, plan human.PlanSummary) (human.Decision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.plans = append(g.plans, plan)
	if g.err != nil {
		return human.Decision{}, g.err
	}
	return g.decision, nil
}

func (g *scriptGate) Escalate(ctx context.Context, e human.Escalation) (human.Resolution, error) {
	return human.Resolution{Action: human.ActionStop}, nil
}

// approving is a gate that answers "yes" and approves the plan.
func approving() *scriptGate {
	return &scriptGate{decision: human.Decision{Approved: true, By: "test"}}
}

// rig is one target repo, one config dir and one database, with a planner
// wired over a real router and the fixture provider.
type rig struct {
	t         *testing.T
	repo      string // the target repository (a temp dir)
	runDir    string // <repo>/.gophermind/<id>
	briefPath string
	dbPath    string
	db        *sql.DB
	led       *ledger.SQLite
	board     *blackboard.SQLite
	sink      *events.Collector
	gate      human.Gate
	fake      *provider.Fake
	router    *router.Router
	cfg       *settings.Config
	deps      planner.Deps
}

// newRig copies the greeter brief into a temp dir with its repo pointed at a
// fresh temp repository, and wires a planner whose model is the fixture
// provider over dirs (testdata/greeter is always searched last).
func newRig(t *testing.T, gate human.Gate, dirs ...string) *rig {
	t.Helper()
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	g := &rig{t: t, repo: t.TempDir(), gate: gate}
	if resolved, err := filepath.EvalSymlinks(g.repo); err == nil {
		g.repo = resolved
	}
	g.runDir = filepath.Join(g.repo, ".gophermind", greeterID)
	g.briefPath = writeBrief(t, g.repo, nil)
	g.dbPath = filepath.Join(t.TempDir(), "bb.db")
	d, err := db.Open(g.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	g.db, g.led, g.board = d, ledger.NewSQLite(d), blackboard.NewSQLite(d)
	g.wire(dirs...)
	return g
}

// writeBrief writes the greeter brief with repo filled in and returns its
// path. edit, when given, changes the text first.
func writeBrief(t *testing.T, repo string, edit func(string) string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(greeter, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "REPO_DIR", repo, 1)
	if edit != nil {
		text = edit(text)
	}
	path := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// wire builds a fresh provider, router and dependency set, the way a new
// process would on `resume`. The repo, config dir and database stay.
func (g *rig) wire(dirs ...string) {
	g.t.Helper()
	fake, err := planner.FixtureProvider(append(dirs, greeter)...)
	if err != nil {
		g.t.Fatal(err)
	}
	g.fake = fake
	g.cfg = planner.FixtureSettings()
	g.sink = events.NewCollector()
	g.router = router.New(g.cfg, map[string]provider.Provider{"fake": fake}, g.led, g.sink)
	g.deps = planner.Deps{Caller: g.router, Gate: g.gate, Sink: g.sink, Board: g.board, Settings: g.cfg,
		LedgerErrors: g.router.LedgerErrors}
}

func (g *rig) plan(o planner.Options) (planner.Outcome, error) {
	if o.BriefPath == "" && o.RunID == "" {
		o.BriefPath = g.briefPath
	}
	return planner.New(g.deps).Run(context.Background(), o)
}

func (g *rig) mustPlan(o planner.Options) {
	g.t.Helper()
	out, err := g.plan(o)
	if err != nil || out != planner.Done {
		g.t.Fatalf("Run = %q, %v; want done", out, err)
	}
}

// stagesCalled lists the stage of every request the fixture provider got, in order.
func (g *rig) stagesCalled() []string {
	var out []string
	for _, r := range g.fake.Requests() {
		out = append(out, planner.StageOf(r))
	}
	return out
}

func (g *rig) read(name string) []byte {
	g.t.Helper()
	raw, err := os.ReadFile(filepath.Join(g.runDir, filepath.FromSlash(name)))
	if err != nil {
		g.t.Fatal(err)
	}
	return raw
}

func (g *rig) has(name string) bool {
	_, err := os.Stat(filepath.Join(g.runDir, filepath.FromSlash(name)))
	return err == nil
}

// variant writes canned replies into a temp dir to put in front of the
// greeter fixture: files maps a file name to its content.
func variant(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func count(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}
```

`briefv2/planner/fixture_test.go`:

```go
package planner_test

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
)

func stageReq(stage string) provider.Request {
	return provider.Request{Model: "fixture", Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "GopherMind planner. Stage: " + stage},
		{Role: provider.RoleUser, Content: "prompt"},
	}}
}

func TestFixtureProviderServesRepliesByStage(t *testing.T) {
	base := variant(t, map[string]string{
		"clarify.txt":              "base clarify",
		"decompose.greeting.txt":   "base first",
		"decompose.greeting.2.txt": "base second",
	})
	over := variant(t, map[string]string{"clarify.txt": "override clarify"})
	p, err := planner.FixtureProvider(over, base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, c := range []struct{ stage, want string }{
		{"clarify", "override clarify"},       // the first directory holding the file wins
		{"decompose:greeting", "base first"},  // a colon in the stage is a dot in the file name
		{"decompose:greeting", "base second"}, // the second request for a stage gets .2.txt
		{"decompose:greeting", "base first"},  // no .3.txt, so the base file again
		{"clarify", "override clarify"},       // no clarify.2.txt either
	} {
		resp, err := p.Complete(ctx, stageReq(c.stage))
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if resp.Text != c.want || resp.Model != "fixture" {
			t.Errorf("call %d (%s) = %q served by %q, want %q served by fixture", i+1, c.stage, resp.Text, resp.Model, c.want)
		}
	}
	if _, err := p.Complete(ctx, stageReq("coverage")); err == nil || !strings.Contains(err.Error(), "coverage.txt") {
		t.Errorf("a stage with no file must fail naming the file, got %v", err)
	}
	if _, err := p.Complete(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}}); err == nil {
		t.Error("a request with no stage line must fail")
	}
	if got := planner.StageOf(stageReq("testwrite:fn-greet")); got != "testwrite:fn-greet" {
		t.Errorf("StageOf = %q", got)
	}
}

func TestFixtureProviderNeedsRealDirectories(t *testing.T) {
	if _, err := planner.FixtureProvider(); err == nil {
		t.Error("no directory must be an error")
	}
	if _, err := planner.FixtureProvider(t.TempDir() + "/missing"); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestFixtureSettingsAreValidAndPrivate(t *testing.T) {
	c := planner.FixtureSettings()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tier := range []string{"strong", "standard", "any"} {
		if got := c.Models[tier]; len(got) != 1 || got[0] != "fake/fixture" {
			t.Errorf("tier %s = %v, want [fake/fixture]", tier, got)
		}
	}
	if v, ok := c.Visibility("fake"); !ok || v != "private" {
		t.Errorf("fake provider visibility = %q, %v", v, ok)
	}
}
```

`briefv2/planner/load_test.go`:

```go
package planner_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/vault"
)

func TestPlanCreatesTheRunFolderTheRegistryAndTheRequirements(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load", AllowPublic: true})

	if got := string(g.read("brief.md")); !strings.Contains(got, "id: "+greeterID) {
		t.Error("brief.md was not copied into the run folder")
	}
	var reqs []planner.Requirement
	if err := json.Unmarshal(g.read("requirements.json"), &reqs); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, q := range reqs {
		ids = append(ids, q.ID)
	}
	if got := strings.Join(ids, " "); got != "F1 F2 C1 C2 A1 A2 A3" {
		t.Errorf("requirements = %s, want F1 F2 C1 C2 A1 A2 A3", got)
	}
	rec, err := planner.LookupRun(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Repo != g.repo || rec.RunDir != g.runDir || rec.BriefPath != g.briefPath || rec.StartedAt == "" {
		t.Errorf("run record = %+v", rec)
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.AllowPublic || st.Requirements != 7 || st.Covered != 0 || len(st.Stages) == 0 || st.Stages[0] != (planner.StageState{Name: "load", Done: true}) {
		t.Errorf("status = %+v", st)
	}
	for _, s := range st.Stages[1:] {
		if s.Done {
			t.Errorf("stage %s is done after load only", s.Name)
		}
	}
	if len(g.fake.Requests()) != 0 {
		t.Error("Load made a model call")
	}
}

func TestPlanRefusesARepoItCannotUse(t *testing.T) {
	cases := []struct{ name, repo, want string }{
		{"https url", "https://github.com/acme/greeter.git", "is a URL"},
		{"ssh url", "git@github.com:acme/greeter.git", "is a URL"},
		{"missing directory", filepath.Join(t.TempDir(), "nope"), "is not an existing directory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving())
			g.briefPath = writeBrief(t, g.repo, func(s string) string { return strings.Replace(s, "repo: "+g.repo, "repo: "+c.repo, 1) })
			_, err := g.plan(planner.Options{})
			var inv *brief.InvalidError
			if !errors.As(err, &inv) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want an invalid-brief error containing %q", err, c.want)
			}
			if g.has("brief.md") {
				t.Error("a run folder was created for a refused brief")
			}
		})
	}
}

func TestPlanOnAnExistingRunPointsAtResume(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "load"})
	_, err := g.plan(planner.Options{StopAfter: "load"})
	if err == nil || !strings.Contains(err.Error(), "gophermind brief resume "+greeterID) {
		t.Fatalf("second plan = %v, want an error pointing at resume", err)
	}
	if _, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "load"}); err != nil {
		t.Fatalf("resume of an existing run: %v", err)
	}
}

func TestRunNeedsABriefOrARunID(t *testing.T) {
	g := newRig(t, approving())
	if _, err := planner.New(g.deps).Run(t.Context(), planner.Options{}); err == nil {
		t.Error("Run with neither a brief nor a run id must fail")
	}
	if _, err := planner.LookupRun("gm-2026-09-29-901"); err == nil || !strings.Contains(err.Error(), "no run") {
		t.Errorf("LookupRun of an unknown run = %v", err)
	}
	if _, err := planner.LookupRun("../../etc/passwd"); err == nil || !strings.Contains(err.Error(), "not a run id") {
		t.Errorf("LookupRun of a path = %v", err)
	}
}

// memSecrets is a vault stand-in. It records every Set.
type memSecrets struct {
	vals map[string]string
	sets []string
}

func (m *memSecrets) Get(scope, name string) (string, bool) {
	v, ok := m.vals[scope+"/"+name]
	return v, ok
}

func (m *memSecrets) Set(scope, name, value string) error {
	if m.vals == nil {
		m.vals = map[string]string{}
	}
	m.vals[scope+"/"+name] = value
	m.sets = append(m.sets, scope+"/"+name)
	return nil
}

const canary = "canary-9f8e7d"

func withSecret(s string) string {
	return strings.Replace(s, "on_ambiguity: halt\n", "on_ambiguity: halt\nsecrets:\n  - name: GREETER_API_KEY\n    purpose: Signs greetings\n", 1)
}

func TestLoadPutsDeclaredSecretsInTheRunScope(t *testing.T) {
	runScope := vault.RunScope(greeterID) + "/GREETER_API_KEY"
	cases := []struct {
		name     string
		have     map[string]string
		prompt   bool
		wantSets int
		wantErr  string
	}{
		{"already stored for the run", map[string]string{runScope: canary}, false, 0, ""},
		{"copied from the harness scope", map[string]string{vault.HarnessScope + "/GREETER_API_KEY": canary}, false, 1, ""},
		{"asked for when someone can be asked", nil, true, 1, ""},
		{"missing with nobody to ask", nil, false, 0, "gophermind brief vault set GREETER_API_KEY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving())
			g.briefPath = writeBrief(t, g.repo, withSecret)
			store := &memSecrets{vals: c.have}
			g.deps.OpenSecrets = func() (planner.Secrets, error) { return store, nil }
			if c.prompt {
				g.deps.PromptSecret = func(name, purpose string) (string, error) {
					if name != "GREETER_API_KEY" || purpose != "Signs greetings" {
						t.Errorf("prompted for %q (%q)", name, purpose)
					}
					return canary, nil
				}
			}
			_, err := g.plan(planner.Options{StopAfter: "load"})
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
				}
				if g.has("brief.md") {
					t.Error("a run folder was created although a secret is missing")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := store.Get(vault.RunScope(greeterID), "GREETER_API_KEY"); got != canary {
				t.Errorf("run scope holds %q", got)
			}
			if len(store.sets) != c.wantSets {
				t.Errorf("Set was called %d times, want %d", len(store.sets), c.wantSets)
			}
			// The value is in the vault and nowhere in the run folder.
			filepath.WalkDir(g.repo, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					if raw, _ := os.ReadFile(p); strings.Contains(string(raw), canary) {
						t.Errorf("%s contains the secret value", p)
					}
				}
				return nil
			})
		})
	}
}

func TestABriefWithoutSecretsNeverOpensTheVault(t *testing.T) {
	g := newRig(t, approving())
	g.deps.OpenSecrets = func() (planner.Secrets, error) {
		t.Error("the vault was opened for a brief that declares no secret")
		return nil, errors.New("unexpected")
	}
	g.mustPlan(planner.Options{StopAfter: "load"})
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | head -6`
Expected: the build fails, starting with

```text
# gophermind/gophermind-lib/briefv2/planner_test [gophermind/gophermind-lib/briefv2/planner.test]
briefv2/planner/harness_test.go:94:20: undefined: planner.Deps
briefv2/planner/harness_test.go:143:23: undefined: planner.FixtureProvider
briefv2/planner/harness_test.go:155:30: undefined: planner.Options
briefv2/planner/harness_test.go:155:48: undefined: planner.Outcome
briefv2/planner/harness_test.go:162:34: undefined: planner.Options
```

- [ ] **Step 8: Implement the foundation: state, answers, prompts, the fixture provider, the planner core, status, and Load**

`briefv2/planner/state.go`:

```go
package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/config"
)

// Files of a run folder (.gophermind/<brief-id>/ inside the target repo).
const (
	fileBrief        = "brief.md"
	fileRequirements = "requirements.json"
	fileAnswers      = "answers.json"
	fileContracts    = "contracts.json"
	fileCoverage     = "coverage.json"
	fileApproval     = "approval.json"

	stateDir          = "_state"
	stateStatus       = "_state/status.json"
	stateClarify      = "_state/clarify.json"
	stateQuestion     = "_state/question.json"
	stateContract     = "_state/contract.json"
	stateDecomposed   = "_state/decomposed.json"
	stateClasses      = "_state/classes.json"
	stateTestwriter   = "_state/testwriter.json"
	stateTestFiles    = "_state/test_files.json"
	stagePrefixSystem = "GopherMind planner. Stage: "
)

// RunRecord is how `resume`, `status` and `calls` find a run from its id
// alone: the run folder lives inside the target repo, and the id does not say
// where that is.
type RunRecord struct {
	RunID     string `json:"run_id"`
	Repo      string `json:"repo"`
	RunDir    string `json:"run_dir"`
	BriefPath string `json:"brief_path"`
	StartedAt string `json:"started_at"`
}

func runRecordPath(runID string) (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runs", runID+".json"), nil
}

// LookupRun reads <config dir>/runs/<run-id>.json.
func LookupRun(runID string) (RunRecord, error) {
	var rec RunRecord
	if !runIDRE.MatchString(runID) {
		return rec, fmt.Errorf("planner: %q is not a run id (want gm-YYYY-MM-DD-NNN)", runID)
	}
	path, err := runRecordPath(runID)
	if err != nil {
		return rec, err
	}
	found, err := readJSON(path, &rec)
	if err != nil {
		return rec, err
	}
	if !found {
		return rec, fmt.Errorf("planner: no run %s on this machine (looked for %s)", runID, path)
	}
	return rec, nil
}

// runStatus is _state/status.json.
type runStatus struct {
	AllowPublic  bool   `json:"allow_public"`
	LedgerErrors int    `json:"ledger_errors"`
	Waiting      string `json:"waiting"`
	PlannedAt    string `json:"planned_at"`
}

// writeFileAtomic writes data beside path and renames it into place, so a
// reader never sees half a file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeJSON stores v as indented JSON, mode 0600.
func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(raw, '\n'), 0o600)
}

// readJSON decodes path into v. found is false, with no error, when the file
// does not exist.
func readJSON(path string, v any) (found bool, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

`briefv2/planner/answers.go`:

```go
package planner

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// answer is one entry of answers.json: a question a model asked and what the
// owner said, or what was assumed when nobody was asked.
type answer struct {
	ID       string `json:"id"`
	Stage    string `json:"stage"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Assumed  bool   `json:"assumed"`
}

type answersFile struct {
	Answers []answer `json:"answers"`
}

func loadAnswers(r *run) (answersFile, error) {
	as := answersFile{Answers: []answer{}}
	_, err := readJSON(r.path(fileAnswers), &as)
	return as, err
}

// answersText is how earlier answers are shown to a model.
func answersText(as answersFile) string {
	if len(as.Answers) == 0 {
		return "(no questions were asked)"
	}
	var b strings.Builder
	for _, a := range as.Answers {
		fmt.Fprintf(&b, "Q: %s\nA: %s", a.Question, a.Answer)
		if a.Assumed {
			b.WriteString(" (assumed, nobody was asked)")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
```

The three prompt templates this task uses. `clarify.md` is the handoff's `01-clarify.md` unchanged (`cp ../docs/briefv2/handoff/prompts/01-clarify.md briefv2/planner/prompts/clarify.md` from `gophermind-lib`); its content is:

```markdown
You are the planner for GopherMind, an autonomous Go build system. You are about to decompose a product brief into function-level tasks that small models will implement without talking to a human. Before that happens, ask every question whose answer would change the code.

Ask only questions that:
- cannot be answered from the brief,
- would change a type, a signature, a file layout, an error code, or a test expectation,
- cannot be safely resolved by picking the most conservative option.

Do not ask about things the brief already states, style preferences the Constraints section covers, or anything in Out of scope.

Brief:
<brief>
{{.Brief}}
</brief>

Respond with a JSON array and nothing else. Each item:
{"id": "q1", "question": "...", "why_it_matters": "...", "default_if_unanswered": "..."}

If you have no questions, respond with [].
```

`contract_outline.md` and `contract_component.md` are the handoff's `02-contract.md` split in two. Every requirement of the original is kept in one of them; nothing in either limits a count.

`briefv2/planner/prompts/contract_outline.md`:

```markdown
You are the architect for GopherMind. Produce the outline of the contract for the product below: the Go module, the conventions every implementer follows, the list of components, and the types more than one component shares. A later pass writes each component's functions, so do not write any function here.

Requirements:
- Go only. Standard library unless the brief's Constraints allow modules.
- Components map to the brief's ### Feature headings plus a `types` component for shared declarations. A component id is lower case letters, digits and dashes. Never use `logs` or `outline` as an id.
- List the components in dependency order: a component comes after every component whose functions it calls.
- Every type decl is complete Go source with a doc comment, exactly as it will appear in the file. `uses` lists the ids of other types the decl references.
- Every `file` is a path relative to the repository root.
- Leave each component's `exports` empty; the harness fills it.
- `integration_tests` for a component describe behavior that needs more than one of its functions. Give `name`, `given`, `expect`, and a `command`.
- Error handling follows one convention stated in `conventions.errors`.
- No secrets. Refer to secrets by environment variable name only.
- There is no limit on the number of components or types. Do not merge features to keep the list short.

Brief:
<brief>
{{.Brief}}
</brief>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

Respond with one JSON object and nothing else:
{"module": "...", "conventions": {"layout": ["..."], "naming": ["..."], "errors": "...", "logging": "...", "testing": "..."}, "components": [{"id": "...", "package": "...", "exports": [], "integration_tests": []}], "types": [<type>]}

Each <type> matches this schema:
<schema>
{{.TypeSchema}}
</schema>
```

`briefv2/planner/prompts/contract_component.md`:

```markdown
You are the architect for GopherMind. Write the contract for ONE component of the product: every function it needs, and any type only this component uses. Implementers will receive only the slices of the contract they need and will never see each other's code, so anything not written here does not exist.

Requirements:
- Every function has an exact Go signature line and a one to three sentence doc that states behavior, not implementation.
- Functions are small. One responsibility each. If a function needs more than roughly 40 lines, split it.
- Every function id starts with `fn-` and is unique across the whole contract.
- `uses` lists every type and function id a function's signature or expected body depends on. Use only ids listed under "Already declared" or declared in this reply. Be complete; the harness derives dependency order from it.
- Every type decl is complete Go source with a doc comment. Declare a type here only when no other component needs it.
- Every `file` is a path relative to the repository root, inside this component's package.
- Follow the conventions in the outline. No secrets. Refer to secrets by environment variable name only.
- Do not repeat a function listed under "Already written for this component".
- There is no limit on the number of functions. Write every function the component needs. If the reply is getting long, stop at a function boundary and set "more" to true; you will be asked to continue. Set "more" to false only when the component is complete.

Component:
<component>
{{.Component}}
</component>

Outline (module, conventions, all components):
<outline>
{{.Outline}}
</outline>

Already declared (id, then declaration):
<declared>
{{.Declared}}
</declared>

Already written for this component:
<written>
{{.Written}}
</written>

Brief section for this component:
<brief_section>
{{.BriefSection}}
</brief_section>

Answers to clarifying questions:
<answers>
{{.Answers}}
</answers>

Respond with one JSON object and nothing else:
{"types": [<type>], "functions": [<function>], "more": false}

<type> and <function> match these schemas (leave `component` out; the harness sets it):
<schema>
{{.ItemSchemas}}
</schema>
```

`briefv2/planner/prompts.go`:

```go
package planner

import (
	"embed"
	"strings"
	"text/template"

	"gophermind/gophermind-lib/briefv2/provider"
)

//go:embed prompts/*.md
var promptFS embed.FS

// render fills prompts/<name>.md. A field the template names and data lacks
// is an error, never an empty string in a prompt.
func render(name string, data map[string]string) (string, error) {
	raw, err := promptFS.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// request is the shape of every planner call: a system line naming the stage
// (the offline fixture provider keys its canned replies on it) and the prompt.
func request(stage, prompt string, maxTokens int) provider.Request {
	return provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: stagePrefixSystem + stage},
			{Role: provider.RoleUser, Content: prompt},
		},
		MaxTokens:   maxTokens,
		Temperature: 0.2,
	}
}

// Output budgets per stage, in tokens. They bound one reply, not the plan:
// a large brief makes more calls, never longer ones.
const (
	maxTokensClarify   = 2048
	maxTokensContract  = 8000
	maxTokensDecompose = 8000
	maxTokensCoverage  = 6000
	maxTokensFill      = 8000
	maxTokensTestwrite = 6000
)
```

`briefv2/planner/fixture.go`:

```go
package planner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
)

// StageOf returns the stage a planner request was made for, read from its
// system message, or "" for a request the planner did not build.
func StageOf(req provider.Request) string {
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem && strings.HasPrefix(m.Content, stagePrefixSystem) {
			return strings.TrimPrefix(m.Content, stagePrefixSystem)
		}
	}
	return ""
}

// FixtureProvider is the offline model: it answers every planner call with a
// canned reply from a directory. The stage "decompose:greeting" is served from
// decompose.greeting.txt; the nth request for the same stage (n >= 2) is
// served from decompose.greeting.<n>.txt when that file exists, otherwise from
// the base file again. With several directories the first one holding the
// file wins, so a variant folder overrides single replies of a base fixture.
func FixtureProvider(dirs ...string) (*provider.Fake, error) {
	if len(dirs) == 0 {
		return nil, errors.New("planner: fixture provider needs at least one directory")
	}
	for _, d := range dirs {
		fi, err := os.Stat(d)
		if err != nil {
			return nil, fmt.Errorf("planner: fixture directory: %w", err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("planner: fixture %s is not a directory", d)
		}
	}
	var mu sync.Mutex
	seen := map[string]int{}
	find := func(name string) ([]byte, bool) {
		for _, d := range dirs {
			if raw, err := os.ReadFile(filepath.Join(d, name)); err == nil {
				return raw, true
			}
		}
		return nil, false
	}
	fn := func(_ int, req provider.Request) (provider.Response, error) {
		stage := StageOf(req)
		if stage == "" {
			return provider.Response{}, errors.New("planner: fixture provider got a request with no stage line")
		}
		base := strings.ReplaceAll(stage, ":", ".")
		mu.Lock()
		seen[stage]++
		n := seen[stage]
		mu.Unlock()
		var raw []byte
		var ok bool
		if n >= 2 {
			raw, ok = find(fmt.Sprintf("%s.%d.txt", base, n))
		}
		if !ok {
			raw, ok = find(base + ".txt")
		}
		if !ok {
			return provider.Response{}, fmt.Errorf("planner: no fixture reply for stage %q (looked for %s.txt in %s)", stage, base, strings.Join(dirs, ", "))
		}
		return provider.Response{Text: string(raw), Model: "fixture",
			Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
	}
	models := []provider.ModelInfo{{ID: "fixture", ContextTokens: 1 << 20}}
	return provider.NewFake("fake", models, fn), nil
}

// FixtureSettings is the configuration that goes with FixtureProvider: one
// private provider named fake, in every tier.
func FixtureSettings() *settings.Config {
	c := settings.Default()
	c.Providers = []settings.ProviderConfig{{
		Name: "fake", BaseURL: "http://fixture.invalid/v1", Visibility: settings.Private, MaxConcurrent: 1,
		Models: []settings.ModelEntry{{ID: "fixture", ContextTokens: 1 << 20}},
	}}
	c.Models = map[string][]string{}
	for _, tier := range settings.Tiers {
		c.Models[tier] = []string{"fake/fixture"}
	}
	return c
}
```

`briefv2/planner/planner.go` (the stage list is empty for now; Steps 10 and 12, and Tasks 9 to 11, each add their stage to it):

```go
package planner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

// Caller is the one router method the planner uses. *router.Router satisfies it.
type Caller interface {
	CallParsed(ctx context.Context, info router.CallInfo, req provider.Request, parse func(text string) error) (router.Result, error)
}

// Secrets is the part of the vault the Load stage needs. *vault.Vault satisfies it.
type Secrets interface {
	Get(scope, name string) (string, bool)
	Set(scope, name, value string) error
}

// Deps is everything the planner talks to. Nothing here is the terminal: the
// run service and the desktop app pass their own gate and sink.
type Deps struct {
	Caller   Caller
	Gate     human.Gate
	Sink     events.Sink           // nil means events.Nop
	Board    blackboard.Blackboard // nil skips the blackboard rows (tests of early stages)
	Settings *settings.Config      // nil means settings.Default()

	// OpenSecrets is called only when the brief declares a secret.
	OpenSecrets func() (Secrets, error)
	// PromptSecret asks a person for one secret value; nil when nothing can
	// prompt (the file gate, tests).
	PromptSecret func(name, purpose string) (string, error)
	// LedgerErrors reports how many ledger writes failed so far
	// ((*router.Router).LedgerErrors); nil means none are counted.
	LedgerErrors func() int
	Now          func() time.Time // nil means time.Now
}

// Options says what one Run call does.
type Options struct {
	BriefPath   string // plan: the brief file
	RunID       string // resume: the run to continue (BriefPath empty)
	Yes         bool   // --yes: approval is recorded as approved_by "flag"
	AllowPublic bool   // recorded in the run's status; the router option is set by the caller
	// StopAfter ends the run once the named stage has finished (load, clarify,
	// contract, decompose, coverage, approve). The run stays resumable.
	StopAfter string
}

type Outcome string

const (
	Done    Outcome = "done"    // every requested stage finished
	Waiting Outcome = "waiting" // a person has to answer before the run can go on
)

type Planner struct{ d Deps }

func New(d Deps) *Planner {
	if d.Sink == nil {
		d.Sink = events.Nop
	}
	if d.Settings == nil {
		d.Settings = settings.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Planner{d: d}
}

// run is one brief being planned.
type run struct {
	id     string
	dir    string // the run folder
	repo   string // the target repository root, absolute
	src    []byte // brief.md as loaded
	brief  *brief.Brief
	reqs   []Requirement
	opts   Options
	status runStatus
}

func (r *run) path(name string) string { return filepath.Join(r.dir, filepath.FromSlash(name)) }

func (r *run) saveStatus() error { return writeJSON(r.path(stateStatus), r.status) }

// stage is one step of the pipeline. done reports whether its output already
// exists, which is what makes a resume skip it.
type stage struct {
	name string
	run  func(p *Planner, ctx context.Context, r *run) error
	done func(r *run) bool
}

// stages after Load, in order.
var stages = []stage{}

// Run plans a brief, or continues a run, until every stage is done, a person
// has to answer (Waiting), or something fails.
func (p *Planner) Run(ctx context.Context, o Options) (Outcome, error) {
	r, err := p.load(ctx, o)
	if err != nil {
		return "", err
	}
	if o.StopAfter == "load" {
		return Done, nil
	}
	defer p.noteLedgerErrors(r)
	for _, st := range stages {
		if st.done(r) {
			if o.StopAfter == st.name {
				return Done, nil
			}
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		p.emit(events.KindStageStarted, st.name, "", "")
		err := st.run(p, ctx, r)
		if errors.Is(err, human.ErrWaiting) {
			r.status.Waiting = st.name
			if serr := r.saveStatus(); serr != nil {
				return "", serr
			}
			p.emit(events.KindWaitingOnHuman, st.name, "", "waiting for an answer; run `gophermind brief resume "+r.id+"` once it is given")
			return Waiting, nil
		}
		if err != nil {
			return "", fmt.Errorf("planner: %s: %w", st.name, err)
		}
		if r.status.Waiting != "" {
			r.status.Waiting = ""
			if err := r.saveStatus(); err != nil {
				return "", err
			}
		}
		p.emit(events.KindStageFinished, st.name, "", "")
		if o.StopAfter == st.name {
			return Done, nil
		}
	}
	return Done, nil
}

func (p *Planner) emit(kind, stageName, nodeID, msg string) {
	p.d.Sink.Emit(events.Event{Kind: kind, Stage: stageName, NodeID: nodeID, Message: msg, At: p.d.Now().UTC()})
}

func (p *Planner) noteLedgerErrors(r *run) {
	if p.d.LedgerErrors == nil {
		return
	}
	if n := p.d.LedgerErrors(); n != r.status.LedgerErrors {
		r.status.LedgerErrors = n
		_ = r.saveStatus()
	}
}

// callSpec says what one model call carries, for the router and the ledger.
type callSpec struct {
	stage     string // full stage name, for example decompose:greeting
	taskType  string // clarify, contract, decompose, coverage, testwrite
	nodeID    string
	nodeClass string
	scope     router.Scope
	maxTokens int
}

// call makes one model call through the router. parse receives the raw reply
// text; an error from it marks the reply malformed and takes the retry path.
func (p *Planner) call(ctx context.Context, r *run, cs callSpec, prompt string, parse func(text string) error) error {
	info := router.CallInfo{RunID: r.id, Stage: cs.stage, NodeID: cs.nodeID, Tier: router.TierStrong, Scope: cs.scope,
		TaskType: cs.taskType, NodeClass: cs.nodeClass}
	_, err := p.d.Caller.CallParsed(ctx, info, request(cs.stage, prompt, cs.maxTokens), parse)
	var ce *router.ChainExhausted
	if errors.As(err, &ce) && ce.OnlyPrivacy() {
		return fmt.Errorf("%w; this call carries %s scope and every model in the %s tier is a public provider: "+
			"add a private provider to that tier in gophermind.yaml, or start the run with --allow-public", err, cs.scope, ce.Tier)
	}
	return err
}

const maxQuestionsPerCall = 3

// pendingQuestion is _state/question.json: a question a model asked that no
// person has answered yet. A resume asks the person again without asking the
// model again.
type pendingQuestion struct {
	Stage    string `json:"stage"`
	Question string `json:"question"`
}

// callAsking is call plus the pause-and-ask protocol: a reply that is a
// QUESTION: goes to the human gate (or, when the brief says assume and
// document, back to the model with that instruction) and the call is made
// again with the answer added.
func (p *Planner) callAsking(ctx context.Context, r *run, cs callSpec, prompt string, parse func(text string) error) error {
	as, err := loadAnswers(r)
	if err != nil {
		return err
	}
	for _, a := range as.Answers {
		if a.Stage == cs.stage {
			prompt += answerNote(a.Question, a.Answer)
		}
	}
	for asked := 0; ; asked++ {
		var pend pendingQuestion
		found, err := readJSON(r.path(stateQuestion), &pend)
		if err != nil {
			return err
		}
		question := ""
		if found && pend.Stage == cs.stage {
			question = pend.Question
		} else {
			err := p.call(ctx, r, cs, prompt, func(text string) error {
				if q, ok := Question(text); ok {
					question = q
					return nil
				}
				question = ""
				return parse(text)
			})
			if err != nil {
				return err
			}
			if question == "" {
				return nil
			}
		}
		if asked >= maxQuestionsPerCall {
			return fmt.Errorf("the model asked more than %d questions in stage %s; the last was: %s", maxQuestionsPerCall, cs.stage, question)
		}
		if r.brief.Front.OnAmbiguity == "assume_and_document" {
			prompt += "\n\nNo human is available. Choose the most conservative option and record it in the node's \"assumptions\" array."
			continue
		}
		if err := writeJSON(r.path(stateQuestion), pendingQuestion{Stage: cs.stage, Question: question}); err != nil {
			return err
		}
		id := fmt.Sprintf("%s-q%d", strings.ReplaceAll(cs.stage, ":", "-"), len(as.Answers)+1)
		got, err := p.ask(ctx, []human.Question{{ID: id, Text: question}})
		if err != nil {
			return err
		}
		as.Answers = append(as.Answers, answer{ID: id, Stage: cs.stage, Question: question, Answer: got[0].Text, Assumed: got[0].Assumed})
		if err := writeJSON(r.path(fileAnswers), as); err != nil {
			return err
		}
		if err := removeFile(r.path(stateQuestion)); err != nil {
			return err
		}
		prompt += answerNote(question, got[0].Text)
	}
}

// ask puts questions to the human gate.
func (p *Planner) ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	if p.d.Gate == nil {
		return nil, errors.New("a question needs an answer and no human gate is configured")
	}
	return p.d.Gate.Ask(ctx, qs)
}

func answerNote(question, text string) string {
	return "\n\nAnswer from the owner to your question:\n" + question + "\n" + text
}
```

`briefv2/planner/status.go`:

```go
package planner

import "encoding/json"

// StageState is one line of `gophermind brief status`.
type StageState struct {
	Name string
	Done bool
}

// Status is what `gophermind brief status` prints. It is computed from the
// run folder alone.
type Status struct {
	RunID, RunDir, Repo   string
	Stages                []StageState // load, then every later stage in order
	Waiting               string       // the stage a person has to answer for, or ""
	Requirements, Covered int
	AllowPublic           bool
	LedgerErrors          int
}

// ReadStatus reports how far a run has come.
func ReadStatus(runID string) (Status, error) {
	rec, err := LookupRun(runID)
	if err != nil {
		return Status{}, err
	}
	r := &run{id: rec.RunID, dir: rec.RunDir, repo: rec.Repo}
	if _, err := readJSON(r.path(stateStatus), &r.status); err != nil {
		return Status{}, err
	}
	st := Status{RunID: rec.RunID, RunDir: rec.RunDir, Repo: rec.Repo, Waiting: r.status.Waiting,
		AllowPublic: r.status.AllowPublic, LedgerErrors: r.status.LedgerErrors}
	st.Stages = append(st.Stages, StageState{Name: "load", Done: exists(r.path(fileRequirements))})
	for _, s := range stages {
		st.Stages = append(st.Stages, StageState{Name: s.name, Done: s.done(r)})
	}
	var reqs []Requirement
	if _, err := readJSON(r.path(fileRequirements), &reqs); err != nil {
		return Status{}, err
	}
	st.Requirements = len(reqs)
	var cov struct {
		Covered []json.RawMessage `json:"covered"`
	}
	if _, err := readJSON(r.path(fileCoverage), &cov); err != nil {
		return Status{}, err
	}
	st.Covered = len(cov.Covered)
	return st, nil
}
```

`briefv2/planner/load.go`:

```go
package planner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/rundir"
	"gophermind/gophermind-lib/briefv2/vault"
)

var runIDRE = regexp.MustCompile(`^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$`)

// load is the Load stage. For `plan` it validates the brief, makes sure its
// secrets are in the vault, creates the run folder and writes the
// requirements. For `resume` it finds the run and reads the same things back.
func (p *Planner) load(ctx context.Context, o Options) (*run, error) {
	if o.RunID != "" {
		return p.loadExisting(o)
	}
	if o.BriefPath == "" {
		return nil, errors.New("planner: give a brief file to plan or a run id to resume")
	}
	p.emit(events.KindStageStarted, "load", "", "")
	src, err := os.ReadFile(o.BriefPath)
	if err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	b, err := brief.Parse(src)
	if err != nil {
		return nil, err
	}
	repo, err := resolveRepo(b.Front.Repo)
	if err != nil {
		return nil, err
	}
	// Secrets come before the run folder: a missing secret must not leave a
	// half-made run behind.
	if err := p.storeSecrets(b); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	dir, err := rundir.Create(repo, b.Front.ID, src)
	if errors.Is(err, rundir.ErrExists) {
		return nil, fmt.Errorf("planner: run %s already exists in %s; continue it with `gophermind brief resume %s`", b.Front.ID, repo, b.Front.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	briefPath, err := filepath.Abs(o.BriefPath)
	if err != nil {
		briefPath = o.BriefPath
	}
	recPath, err := runRecordPath(b.Front.ID)
	if err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	rec := RunRecord{RunID: b.Front.ID, Repo: repo, RunDir: dir, BriefPath: briefPath, StartedAt: p.d.Now().UTC().Format("2006-01-02T15:04:05Z")}
	if err := writeJSON(recPath, rec); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	r := &run{id: b.Front.ID, dir: dir, repo: repo, src: src, brief: b, reqs: ParseRequirements(src), opts: o}
	if err := writeJSON(r.path(fileRequirements), r.reqs); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	if err := p.notePublic(r, o); err != nil {
		return nil, err
	}
	p.emit(events.KindStageFinished, "load", "", "")
	return r, nil
}

func (p *Planner) loadExisting(o Options) (*run, error) {
	rec, err := LookupRun(o.RunID)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(filepath.Join(rec.RunDir, fileBrief))
	if err != nil {
		return nil, fmt.Errorf("planner: run %s: %w", o.RunID, err)
	}
	b, err := brief.Parse(src)
	if err != nil {
		return nil, err
	}
	if err := p.storeSecrets(b); err != nil {
		return nil, fmt.Errorf("planner: load: %w", err)
	}
	r := &run{id: rec.RunID, dir: rec.RunDir, repo: rec.Repo, src: src, brief: b, reqs: ParseRequirements(src), opts: o}
	if _, err := readJSON(r.path(stateStatus), &r.status); err != nil {
		return nil, err
	}
	if !exists(r.path(fileRequirements)) {
		if err := writeJSON(r.path(fileRequirements), r.reqs); err != nil {
			return nil, err
		}
	}
	if err := p.notePublic(r, o); err != nil {
		return nil, err
	}
	return r, nil
}

// notePublic records that a run was allowed to show the brief to public
// providers. Once true it stays true: the status is a statement about what
// may already have left the machine.
func (p *Planner) notePublic(r *run, o Options) error {
	if o.AllowPublic {
		r.status.AllowPublic = true
		p.emit(events.KindWarning, "load", "", "--allow-public: public providers may be sent the whole brief in this run")
	}
	return r.saveStatus()
}

// resolveRepo turns the brief's repo into an absolute path to an existing
// directory. A URL is refused: cloning belongs to the git landing plan.
func resolveRepo(repo string) (string, error) {
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return "", &brief.InvalidError{Reason: fmt.Sprintf("repo: %q is a URL; the planner needs an existing local path (cloning belongs to git landing)", repo)}
	}
	if repo == "~" || strings.HasPrefix(repo, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		repo = filepath.Join(home, strings.TrimPrefix(repo, "~"))
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", &brief.InvalidError{Reason: fmt.Sprintf("repo: %s is not an existing directory", abs)}
	}
	return abs, nil
}

// storeSecrets makes sure every secret the brief declares is in the vault
// under this run's scope. A value already stored for the run is left alone; a
// value stored for the harness (`gophermind brief vault set`) is copied; a
// person is asked when someone can be; otherwise the run does not start.
// Values never leave the vault and never appear in an error.
func (p *Planner) storeSecrets(b *brief.Brief) error {
	if len(b.Front.Secrets) == 0 {
		return nil
	}
	if p.d.OpenSecrets == nil {
		return errors.New("the brief declares secrets but no vault is available")
	}
	store, err := p.d.OpenSecrets()
	if err != nil {
		return err
	}
	scope := vault.RunScope(b.Front.ID)
	for _, s := range b.Front.Secrets {
		if _, ok := store.Get(scope, s.Name); ok {
			continue
		}
		v, ok := store.Get(vault.HarnessScope, s.Name)
		if !ok {
			if p.d.PromptSecret == nil {
				return fmt.Errorf("secret %s is not in the vault: run `gophermind brief vault set %s`, then start again", s.Name, s.Name)
			}
			v, err = p.d.PromptSecret(s.Name, s.Purpose)
			if err != nil {
				return fmt.Errorf("secret %s: %w", s.Name, err)
			}
			if v == "" {
				return fmt.Errorf("secret %s: an empty value was given", s.Name)
			}
		}
		if err := store.Set(scope, s.Name, v); err != nil {
			return fmt.Errorf("secret %s: %w", s.Name, err)
		}
	}
	return nil
}
```

- [ ] **Step 9: Run the foundation tests**

Run: `cd gophermind-lib && gofmt -l briefv2/planner && go vet ./briefv2/planner/ && go test ./briefv2/planner/ -race 2>&1 | tail -3`
Expected: `gofmt` prints nothing; `ok  gophermind/gophermind-lib/briefv2/planner`.

- [ ] **Step 10: Write the failing Clarify tests**

`briefv2/planner/testdata/greeter/clarify.txt`:

```text
[{"id": "q1", "question": "Should a name be trimmed of surrounding spaces before it is used?", "why_it_matters": "It changes what Greet returns and what the tests expect.", "default_if_unanswered": "Yes, trim it."}]
```

`briefv2/planner/clarify_test.go`:

```go
package planner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

type savedAnswer struct {
	ID, Stage, Question, Answer string
	Assumed                     bool
}

func (g *rig) answers() []savedAnswer {
	g.t.Helper()
	var f struct {
		Answers []savedAnswer `json:"answers"`
	}
	if err := json.Unmarshal(g.read("answers.json"), &f); err != nil {
		g.t.Fatal(err)
	}
	return f.Answers
}

func TestClarifyAsksThePersonAndStoresTheAnswers(t *testing.T) {
	gate := approving()
	gate.answer = func(q human.Question) string { return "Trim it, and collapse inner runs of spaces." }
	g := newRig(t, gate)
	g.mustPlan(planner.Options{StopAfter: "clarify"})

	if len(gate.asked) != 1 || gate.asked[0].ID != "q1" || gate.asked[0].Default != "Yes, trim it." ||
		!strings.Contains(gate.asked[0].Text, "It changes what Greet returns") {
		t.Fatalf("gate was asked %+v", gate.asked)
	}
	as := g.answers()
	if len(as) != 1 || as[0].Stage != "clarify" || as[0].Answer != "Trim it, and collapse inner runs of spaces." || as[0].Assumed {
		t.Errorf("answers.json = %+v", as)
	}
	rows, err := g.led.List(context.Background(), greeterID, ledger.Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("ledger rows = %d, %v; want 1", len(rows), err)
	}
	if rows[0].Stage != "clarify" || rows[0].TaskType != "clarify" || rows[0].Scope != "brief" || rows[0].Tier != "strong" {
		t.Errorf("ledger row = %+v", rows[0])
	}
}

func TestClarifyTakesTheDefaultsWhenTheBriefSaysToAssume(t *testing.T) {
	gate := approving()
	g := newRig(t, gate)
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		return strings.Replace(s, "on_ambiguity: halt", "on_ambiguity: assume_and_document", 1)
	})
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	if len(gate.asked) != 0 {
		t.Errorf("the gate was asked %d questions in assume mode", len(gate.asked))
	}
	as := g.answers()
	if len(as) != 1 || as[0].Answer != "Yes, trim it." || !as[0].Assumed {
		t.Errorf("answers.json = %+v", as)
	}
}

func TestClarifyWithNoQuestionsSkipsTheGate(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{"clarify.txt": "No questions.\n```json\n[]\n```"}))
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	if len(gate.asked) != 0 || len(g.answers()) != 0 {
		t.Errorf("asked %d, stored %d; want none", len(gate.asked), len(g.answers()))
	}
}

// A gate that nobody has answered yet stops the run as Waiting. The resume
// asks the person again and must not ask the model again.
func TestClarifyWaitingThenResumeDoesNotCallTheModelAgain(t *testing.T) {
	gate := approving()
	gate.err = human.ErrWaiting
	g := newRig(t, gate)
	out, err := g.plan(planner.Options{StopAfter: "clarify"})
	if err != nil || out != planner.Waiting {
		t.Fatalf("Run = %q, %v; want waiting", out, err)
	}
	if g.has("answers.json") {
		t.Fatal("answers.json exists before anyone answered")
	}
	if st, _ := planner.ReadStatus(greeterID); st.Waiting != "clarify" {
		t.Errorf("status.Waiting = %q, want clarify", st.Waiting)
	}

	gate.err = nil
	g.wire() // a new process: fresh provider, same run
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "clarify"})
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("resume made %d model calls, want 0", n)
	}
	if len(g.answers()) != 1 {
		t.Errorf("answers.json = %+v", g.answers())
	}
	if st, _ := planner.ReadStatus(greeterID); st.Waiting != "" {
		t.Errorf("status.Waiting = %q after the answer, want empty", st.Waiting)
	}
}

func TestAMalformedClarifyReplyIsRetriedAndRecorded(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"clarify.txt":   "I have some thoughts but no JSON.",
		"clarify.2.txt": "[]",
	}))
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{})
	if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeMalformed || rows[1].Outcome != ledger.OutcomeOK {
		t.Fatalf("ledger rows = %+v", rows)
	}
}

// When every model that could answer is a public provider, the error must
// name the way out instead of a bare "no model answered".
func TestAPrivacyDeadEndNamesTheSettingToChange(t *testing.T) {
	g := newRig(t, approving())
	cfg := planner.FixtureSettings()
	cfg.Providers[0].Visibility = settings.Public
	g.router = router.New(cfg, map[string]provider.Provider{"fake": g.fake}, g.led, g.sink)
	g.deps.Caller = g.router
	_, err := g.plan(planner.Options{StopAfter: "clarify"})
	if err == nil || !strings.Contains(err.Error(), "--allow-public") || !strings.Contains(err.Error(), "brief scope") {
		t.Fatalf("err = %v, want it to name --allow-public and the scope", err)
	}
	if len(g.fake.Requests()) != 0 {
		t.Error("a public provider was sent the brief")
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ -run 'Clarify|Privacy' 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'`
Expected (there is no clarify stage yet, so nothing is asked and nothing is written):

```text
--- FAIL: TestClarifyAsksThePersonAndStoresTheAnswers
--- FAIL: TestClarifyTakesTheDefaultsWhenTheBriefSaysToAssume
--- FAIL: TestClarifyWithNoQuestionsSkipsTheGate
--- FAIL: TestClarifyWaitingThenResumeDoesNotCallTheModelAgain
--- FAIL: TestAMalformedClarifyReplyIsRetriedAndRecorded
--- FAIL: TestAPrivacyDeadEndNamesTheSettingToChange
FAIL
FAIL	gophermind/gophermind-lib/briefv2/planner
```

- [ ] **Step 11: Implement Clarify**

`briefv2/planner/clarify.go`:

```go
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/router"
)

type clarifyQuestion struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Why      string `json:"why_it_matters"`
	Default  string `json:"default_if_unanswered"`
}

// parseClarify decodes the Clarify reply. Ids are renumbered q1, q2, ... so
// they are unique whatever the model wrote.
func parseClarify(text string) ([]clarifyQuestion, error) {
	qs := []clarifyQuestion{}
	if err := json.Unmarshal([]byte(text), &qs); err != nil {
		return nil, fmt.Errorf("clarify reply is not a JSON array of questions: %w", err)
	}
	for i := range qs {
		if strings.TrimSpace(qs[i].Question) == "" {
			return nil, fmt.Errorf("clarify reply: question %d has no text", i+1)
		}
		qs[i].ID = fmt.Sprintf("q%d", i+1)
	}
	return qs, nil
}

func clarifyDone(r *run) bool { return exists(r.path(fileAnswers)) }

// clarify asks the model what it needs to know before planning, then gets the
// answers from a person, or takes the defaults when the brief says to assume.
// The questions are kept in _state/clarify.json so a resume after the file
// gate stopped the run never asks the model again.
func (p *Planner) clarify(ctx context.Context, r *run) error {
	var qs []clarifyQuestion
	found, err := readJSON(r.path(stateClarify), &qs)
	if err != nil {
		return err
	}
	if !found {
		prompt, err := render("clarify", map[string]string{"Brief": string(r.src)})
		if err != nil {
			return err
		}
		cs := callSpec{stage: "clarify", taskType: "clarify", scope: router.ScopeBrief, maxTokens: maxTokensClarify}
		if err := p.call(ctx, r, cs, prompt, func(text string) error {
			parsed, err := parseClarify(StripReply(text))
			if err != nil {
				return err
			}
			qs = parsed
			return nil
		}); err != nil {
			return err
		}
		if err := writeJSON(r.path(stateClarify), qs); err != nil {
			return err
		}
	}

	as := answersFile{Answers: []answer{}}
	switch {
	case len(qs) == 0:
	case r.brief.Front.OnAmbiguity == "assume_and_document":
		for _, q := range qs {
			text := q.Default
			if strings.TrimSpace(text) == "" {
				text = "No answer was given; take the most conservative option."
			}
			as.Answers = append(as.Answers, answer{ID: q.ID, Stage: "clarify", Question: q.Question, Answer: text, Assumed: true})
		}
	default:
		ask := make([]human.Question, len(qs))
		for i, q := range qs {
			text := q.Question
			if q.Why != "" {
				text += " (" + q.Why + ")"
			}
			ask[i] = human.Question{ID: q.ID, Text: text, Default: q.Default}
		}
		got, err := p.ask(ctx, ask)
		if err != nil {
			return err
		}
		for i, q := range qs {
			as.Answers = append(as.Answers, answer{ID: q.ID, Stage: "clarify", Question: q.Question, Answer: got[i].Text, Assumed: got[i].Assumed})
		}
	}
	return writeJSON(r.path(fileAnswers), as)
}
```

In `briefv2/planner/planner.go` replace

```go
var stages = []stage{}
```

with

```go
var stages = []stage{
	{"clarify", (*Planner).clarify, clarifyDone},
}
```

Run: `cd gophermind-lib && gofmt -l briefv2/planner && go test ./briefv2/planner/ -run 'Clarify|Privacy' -race 2>&1 | tail -2`
Expected: `gofmt` prints nothing; `ok`.

- [ ] **Step 12: Write the failing Contract tests**

Canned replies for the outline pass and one pass per component. One is wrapped in a fence with prose around it and one has a sentence in front of the JSON, to exercise `StripReply` on the way.

`briefv2/planner/testdata/greeter/contract.outline.txt`:

````text
Here is the outline.

```json
{
  "module": "example.com/greeter",
  "conventions": {
    "layout": ["internal/greet: pure functions that build messages. No I/O."],
    "naming": ["Exported functions are verbs."],
    "errors": "Functions return *NameError as error and never panic.",
    "testing": "Table-driven tests next to the code."
  },
  "components": [
    {"id": "types", "package": "greet", "exports": []},
    {"id": "greeting", "package": "greet", "exports": [],
     "integration_tests": [{"name": "greets a trimmed name", "given": "Greet(\"  Ada  \")", "expect": "Hello, Ada!", "command": "go test ./internal/greet -run TestGreetIntegration"}]},
    {"id": "farewell", "package": "greet", "exports": []}
  ],
  "types": [
    {"id": "name-error", "package": "greet", "file": "internal/greet/errors.go",
     "decl": "// NameError says why a name was refused.\ntype NameError struct {\n\tReason string\n}"}
  ]
}
```
````

`briefv2/planner/testdata/greeter/contract.types.txt`:

```text
{"types": [], "functions": [
  {"id": "fn-name-error-error", "package": "greet", "file": "internal/greet/errors.go",
   "signature": "func (e *NameError) Error() string",
   "doc": "Error returns the reason, so *NameError satisfies error.", "uses": ["name-error"]}
], "more": false}
```

`briefv2/planner/testdata/greeter/contract.greeting.txt`:

```text
{"types": [], "functions": [
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go",
   "signature": "func Greet(name string) (string, error)",
   "doc": "Greet trims the name and returns Hello, <name>!. An empty name is an error.",
   "uses": ["name-error", "fn-name-error-error"]}
], "more": false}
```

`briefv2/planner/testdata/greeter/contract.farewell.txt`:

```text
The farewell component needs one function. {"types": [], "functions": [
  {"id": "fn-farewell", "package": "greet", "file": "internal/greet/farewell.go",
   "signature": "func Farewell(name string) (string, error)",
   "doc": "Farewell trims the name and returns Goodbye, <name>!. An empty name is an error.",
   "uses": ["name-error"]}
], "more": false}
```

`briefv2/planner/contract_stage_test.go`:

```go
package planner_test

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

func (g *rig) contracts() *contract.Contracts {
	g.t.Helper()
	c, err := contract.Load(g.read("contracts.json"))
	if err != nil {
		g.t.Fatalf("contracts.json: %v", err)
	}
	return c
}

func TestContractIsBuiltInPassesAndValidated(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})

	if got := strings.Join(g.stagesCalled(), " "); got != "clarify contract:outline contract:types contract:greeting contract:farewell" {
		t.Errorf("calls = %s", got)
	}
	c := g.contracts()
	if c.BriefID != greeterID || c.Module != "example.com/greeter" || len(c.Types) != 1 || len(c.Functions) != 3 || len(c.Components) != 3 {
		t.Fatalf("contract = %d types, %d functions, %d components", len(c.Types), len(c.Functions), len(c.Components))
	}
	owner := map[string]string{}
	for _, f := range c.Functions {
		owner[f.ID] = f.Component
	}
	if owner["fn-name-error-error"] != "types" || owner["fn-greet"] != "greeting" || owner["fn-farewell"] != "farewell" {
		t.Errorf("function owners = %v (the harness sets component from the pass)", owner)
	}
	for _, comp := range c.Components {
		if len(comp.Exports) != 1 {
			t.Errorf("component %s exports %v, want its one function (filled by the harness)", comp.ID, comp.Exports)
		}
	}

	// The component pass sees what earlier passes declared, and the answers.
	var greeting string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:greeting" {
			greeting = r.Messages[1].Content
		}
	}
	for _, want := range []string{"fn-name-error-error: func (e *NameError) Error() string", "type NameError struct", "### Greeting", "A: yes", "(none yet)"} {
		if !strings.Contains(greeting, want) {
			t.Errorf("contract:greeting prompt lacks %q", want)
		}
	}

	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "contract"})
	if len(rows) != 4 {
		t.Fatalf("contract rows = %d, want 4", len(rows))
	}
	if rows[0].Stage != "contract:outline" || rows[0].Scope != "brief" || rows[1].Stage != "contract:types" || rows[1].Scope != "component" {
		t.Errorf("rows = %s/%s then %s/%s", rows[0].Stage, rows[0].Scope, rows[1].Stage, rows[1].Scope)
	}
}

// A component reply that ends with "more": true is continued: the same stage
// is called again, told what is already written, and both replies are merged.
func TestContractContinuesAComponentWhileMoreIsTrue(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.greeting.txt": `{"types": [], "functions": [
  {"id": "fn-greet", "package": "greet", "file": "internal/greet/greet.go",
   "signature": "func Greet(name string) (string, error)", "doc": "Greet returns Hello, <name>!.", "uses": ["name-error"]}
], "more": true}`,
		"contract.greeting.2.txt": `{"types": [
  {"id": "shout-mode", "package": "greet", "file": "internal/greet/shout.go", "decl": "// ShoutMode says how loud a greeting is.\ntype ShoutMode int"}
], "functions": [
  {"id": "fn-greet-loud", "package": "greet", "file": "internal/greet/shout.go",
   "signature": "func GreetLoud(name string, mode ShoutMode) (string, error)", "doc": "GreetLoud is Greet in capitals.", "uses": ["fn-greet", "shout-mode"]}
], "more": false}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})

	calls := g.stagesCalled()
	if count(calls, "contract:greeting") != 2 || count(calls, "contract:types") != 1 || count(calls, "contract:farewell") != 1 {
		t.Fatalf("calls = %v, want two for greeting and one for each other component", calls)
	}
	n := 0
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) != "contract:greeting" {
			continue
		}
		n++
		written := strings.Contains(r.Messages[1].Content, "<written>\nfn-greet\n</written>")
		if n == 1 && written || n == 2 && !written {
			t.Errorf("call %d for greeting: already-written list is wrong", n)
		}
	}
	c := g.contracts()
	if len(c.Functions) != 4 || len(c.Types) != 2 {
		t.Fatalf("contract = %d functions and %d types, want 4 and 2 (both passes merged)", len(c.Functions), len(c.Types))
	}
	for _, comp := range c.Components {
		if comp.ID == "greeting" && strings.Join(comp.Exports, " ") != "fn-greet fn-greet-loud" {
			t.Errorf("greeting exports = %v", comp.Exports)
		}
	}
}

// Finished components are kept in _state/contract.json, so a run that died
// in the middle of the contract continues from the next component.
func TestContractResumeSkipsFinishedComponents(t *testing.T) {
	broken := variant(t, map[string]string{"contract.farewell.txt": "the model fell over", "contract.farewell.2.txt": "and again"})
	g := newRig(t, approving(), broken)
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil || !strings.Contains(err.Error(), "contract") {
		t.Fatalf("err = %v, want the contract stage to fail on farewell", err)
	}
	if g.has("contracts.json") {
		t.Fatal("contracts.json was written before every pass finished")
	}

	g.wire()
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := strings.Join(g.stagesCalled(), " "); got != "contract:farewell" {
		t.Errorf("resume called %q, want only contract:farewell", got)
	}
	if len(g.contracts().Functions) != 3 {
		t.Error("the resumed contract is incomplete")
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Stages {
		if want := s.Name == "load" || s.Name == "clarify" || s.Name == "contract"; s.Done != want {
			t.Errorf("stage %s done = %v, want %v", s.Name, s.Done, want)
		}
	}
}
```

`briefv2/planner/contract_internal_test.go` (white-box: the outline parser and the pass merger are where the contract rules live):

```go
package planner

import (
	"strings"
	"testing"
)

const testRunID = "gm-2026-09-29-900"

const okOutline = `{"module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "components": [{"id": "types", "package": "x"}, {"id": "greeting", "package": "x"}],
 "types": [{"id": "name-error", "package": "x", "file": "internal/x/errors.go", "decl": "type NameError struct{}"}]}`

func TestParseOutline(t *testing.T) {
	doc, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if doc["brief_id"] != testRunID || doc["spec_version"] != "2.0" || len(objects(doc["components"])) != 2 {
		t.Errorf("doc = %v", doc)
	}
	for _, c := range objects(doc["components"]) {
		if _, ok := c["exports"].([]any); !ok {
			t.Errorf("component %v has no exports array", c["id"])
		}
	}

	bad := []struct{ name, edit, with, want string }{
		{"no components", `"components": [{"id": "types", "package": "x"}, {"id": "greeting", "package": "x"}]`, `"components": []`, "lists no component"},
		{"reserved id logs", `"id": "greeting"`, `"id": "logs"`, "reserved"},
		{"reserved id outline", `"id": "greeting"`, `"id": "outline"`, "reserved"},
		{"duplicate component", `"id": "greeting"`, `"id": "types"`, "duplicate component"},
		{"component named like the run", `"id": "greeting"`, `"id": "gm-2026-09-29-900"`, "is the run id"},
		{"type file leaves the repo", `internal/x/errors.go`, `../x/errors.go`, "inside the repository"},
		{"type file is absolute", `internal/x/errors.go`, `/etc/errors.go`, "relative path"},
		{"no conventions", `"errors": "return error"`, `"mistakes": "x"`, "conventions"},
		{"not json", okOutline, "an outline in prose", "not a JSON object"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseOutline(strings.Replace(okOutline, c.edit, c.with, 1), testRunID)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestMergePass(t *testing.T) {
	base, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	fn := func(id, sig, file, uses string) string {
		return `{"id": "` + id + `", "package": "x", "file": "` + file + `", "signature": "` + sig + `", "doc": "d", "uses": [` + uses + `]}`
	}
	good := `{"types": [], "functions": [` + fn("fn-greet", "func Greet(name string) (string, error)", "internal/x/greet.go", `"name-error"`) + `], "more": true}`
	doc, more, err := mergePass(base, "greeting", good, testRunID)
	if err != nil || !more {
		t.Fatalf("mergePass = more %v, %v", more, err)
	}
	if fns := objects(doc["functions"]); len(fns) != 1 || fns[0]["component"] != "greeting" {
		t.Errorf("functions = %v (component must be set by the harness)", fns)
	}
	if len(objects(base["functions"])) != 0 {
		t.Error("mergePass changed the document it was given")
	}

	bad := []struct{ name, reply, want string }{
		{"more with nothing new", `{"types": [], "functions": [], "more": true}`, "holds no function"},
		{"uses an id nobody declared", `{"functions": [` + fn("fn-a", "func A()", "internal/x/a.go", `"fn-later"`) + `]}`, "unknown id"},
		{"signature is not Go", `{"functions": [` + fn("fn-a", "A(name) string", "internal/x/a.go", ``) + `]}`, "not valid Go"},
		{"signature is two declarations", `{"functions": [` + fn("fn-a", "func A() {}\\nfunc B()", "internal/x/a.go", ``) + `]}`, "exactly one function"},
		{"file leaves the repo", `{"functions": [` + fn("fn-a", "func A()", "../a.go", ``) + `]}`, "inside the repository"},
		{"file is not Go", `{"functions": [` + fn("fn-a", "func A()", "internal/x/a.txt", ``) + `]}`, "not a Go file"},
		{"id repeats", `{"functions": [` + fn("fn-greet", "func Greet()", "internal/x/g.go", ``) + `]}`, "duplicate id"},
		{"not json", "here are the functions", "not a JSON object"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := mergePass(doc, "greeting", c.reply, testRunID)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestParseSignature(t *testing.T) {
	for _, sig := range []string{
		"func Greet(name string) (string, error)",
		"func (s *Server) Routes() http.Handler",
		"func Map[T any](in []T, f func(T) T) []T",
	} {
		if _, err := parseSignature(sig); err != nil {
			t.Errorf("parseSignature(%q): %v", sig, err)
		}
	}
	for _, sig := range []string{"", "Greet(name string)", "type T int", "func"} {
		if _, err := parseSignature(sig); err == nil {
			t.Errorf("parseSignature(%q) accepted a non-function", sig)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Greeting": "greeting", "CRM sync": "crm-sync", "  KPI Tracking & Evaluation ": "kpi-tracking-evaluation"} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | head -6`
Expected: the build fails, starting with

```text
# gophermind/gophermind-lib/briefv2/planner [gophermind/gophermind-lib/briefv2/planner.test]
briefv2/planner/contract_internal_test.go:16:14: undefined: parseOutline
briefv2/planner/contract_internal_test.go:20:73: undefined: objects
briefv2/planner/contract_internal_test.go:23:20: undefined: objects
briefv2/planner/contract_internal_test.go:42:14: undefined: parseOutline
briefv2/planner/contract_internal_test.go:51:15: undefined: parseOutline
```

- [ ] **Step 13: Implement Contract in passes**

`briefv2/planner/contract_stage.go`:

```go
package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/schema"
)

// contractState is _state/contract.json: the contract as far as it has been
// written, and which components are finished. contracts.json itself is only
// written once every pass is done.
type contractState struct {
	Doc  map[string]any `json:"doc"`
	Done []string       `json:"done"`
}

func contractDone(r *run) bool { return exists(r.path(fileContracts)) }

// contract is the Contract stage (Wave 0), built in passes so that a large
// brief makes more calls rather than a coarser contract: one outline call,
// then one call per component, repeated while the model says more remains.
func (p *Planner) contract(ctx context.Context, r *run) error {
	as, err := loadAnswers(r)
	if err != nil {
		return err
	}
	var st contractState
	found, err := readJSON(r.path(stateContract), &st)
	if err != nil {
		return err
	}
	if !found {
		typeSchema, err := contractItemSchemas("types")
		if err != nil {
			return err
		}
		prompt, err := render("contract_outline", map[string]string{
			"Brief": string(r.src), "Answers": answersText(as), "TypeSchema": typeSchema})
		if err != nil {
			return err
		}
		cs := callSpec{stage: "contract:outline", taskType: "contract", scope: router.ScopeBrief, maxTokens: maxTokensContract}
		if err := p.call(ctx, r, cs, prompt, func(text string) error {
			doc, err := parseOutline(StripReply(text), r.id)
			if err != nil {
				return err
			}
			st.Doc = doc
			return nil
		}); err != nil {
			return err
		}
		if err := writeJSON(r.path(stateContract), st); err != nil {
			return err
		}
	}

	itemSchemas, err := contractItemSchemas("types", "functions")
	if err != nil {
		return err
	}
	for _, comp := range objects(st.Doc["components"]) {
		id, _ := comp["id"].(string)
		if contains(st.Done, id) {
			continue
		}
		for {
			prompt, err := render("contract_component", map[string]string{
				"Component":    mustJSON(comp),
				"Outline":      mustJSON(map[string]any{"module": st.Doc["module"], "conventions": st.Doc["conventions"], "components": st.Doc["components"]}),
				"Declared":     declaredText(st.Doc),
				"Written":      writtenText(st.Doc, id),
				"BriefSection": briefSection(r, id),
				"Answers":      answersText(as),
				"ItemSchemas":  itemSchemas,
			})
			if err != nil {
				return err
			}
			more := false
			cs := callSpec{stage: "contract:" + id, taskType: "contract", scope: router.ScopeComponent, maxTokens: maxTokensContract}
			if err := p.call(ctx, r, cs, prompt, func(text string) error {
				doc, m, err := mergePass(st.Doc, id, StripReply(text), r.id)
				if err != nil {
					return err
				}
				st.Doc, more = doc, m
				return nil
			}); err != nil {
				return err
			}
			if !more {
				st.Done = append(st.Done, id)
			}
			if err := writeJSON(r.path(stateContract), st); err != nil {
				return err
			}
			if !more {
				break
			}
		}
	}

	// A component whose exports the outline left empty exports all its functions.
	fns := objects(st.Doc["functions"])
	if len(fns) == 0 {
		return errors.New("the contract declares no function")
	}
	for _, comp := range objects(st.Doc["components"]) {
		if len(strList(comp["exports"])) > 0 {
			continue
		}
		exports := []any{}
		for _, f := range fns {
			if f["component"] == comp["id"] {
				exports = append(exports, f["id"])
			}
		}
		comp["exports"] = exports
	}
	if _, err := validateContractDoc(st.Doc, r.id); err != nil {
		return err
	}
	return writeJSON(r.path(fileContracts), st.Doc)
}

// parseOutline turns the outline reply into the start of contracts.json: the
// harness supplies the version, the brief id and an empty function list.
func parseOutline(text, briefID string) (map[string]any, error) {
	var o struct {
		Module      string           `json:"module"`
		Conventions map[string]any   `json:"conventions"`
		Components  []map[string]any `json:"components"`
		Types       []map[string]any `json:"types"`
	}
	if err := json.Unmarshal([]byte(text), &o); err != nil {
		return nil, fmt.Errorf("contract outline is not a JSON object: %w", err)
	}
	if len(o.Components) == 0 {
		return nil, errors.New("contract outline lists no component")
	}
	comps := make([]any, 0, len(o.Components))
	for _, c := range o.Components {
		if _, ok := c["exports"].([]any); !ok {
			c["exports"] = []any{}
		}
		comps = append(comps, c)
	}
	types := make([]any, 0, len(o.Types))
	for _, t := range o.Types {
		types = append(types, t)
	}
	doc := map[string]any{
		"spec_version": "2.0", "brief_id": briefID, "revision": 0,
		"module": o.Module, "conventions": o.Conventions,
		"types": types, "functions": []any{}, "components": comps,
	}
	if _, err := validateContractDoc(doc, briefID); err != nil {
		return nil, err
	}
	return doc, nil
}

// mergePass adds one component pass to a copy of doc and validates the whole
// contract again. It never changes doc, so a rejected reply leaves no trace.
func mergePass(doc map[string]any, component, text, briefID string) (map[string]any, bool, error) {
	var pass struct {
		Types     []map[string]any `json:"types"`
		Functions []map[string]any `json:"functions"`
		More      bool             `json:"more"`
	}
	if err := json.Unmarshal([]byte(text), &pass); err != nil {
		return nil, false, fmt.Errorf("contract reply for %s is not a JSON object: %w", component, err)
	}
	if pass.More && len(pass.Functions) == 0 {
		return nil, false, fmt.Errorf("contract reply for %s says more remains but holds no function", component)
	}
	next, err := copyDoc(doc)
	if err != nil {
		return nil, false, err
	}
	types, _ := next["types"].([]any)
	for _, t := range pass.Types {
		types = append(types, t)
	}
	fns, _ := next["functions"].([]any)
	for _, f := range pass.Functions {
		f["component"] = component
		fns = append(fns, f)
	}
	next["types"], next["functions"] = types, fns
	if _, err := validateContractDoc(next, briefID); err != nil {
		return nil, false, err
	}
	return next, pass.More, nil
}

var componentIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validateContractDoc runs the schema and reference checks of contract.Load
// and then the ones only the planner knows: ids that would collide in the
// tree or with a stage name, files that leave the repository, signatures that
// are not Go.
func validateContractDoc(doc map[string]any, briefID string) (*contract.Contracts, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	c, err := contract.Load(raw)
	if err != nil {
		return nil, err
	}
	if c.BriefID != briefID {
		return nil, fmt.Errorf("contract: brief_id is %q, want %q", c.BriefID, briefID)
	}
	comps := map[string]bool{}
	for _, comp := range c.Components {
		switch {
		case !componentIDRE.MatchString(comp.ID):
			return nil, fmt.Errorf("contract: component id %q must be lower case letters, digits and dashes", comp.ID)
		case comp.ID == "logs" || comp.ID == "outline":
			return nil, fmt.Errorf("contract: component id %q is reserved", comp.ID)
		case comp.ID == briefID:
			return nil, fmt.Errorf("contract: component id %q is the run id", comp.ID)
		case comps[comp.ID]:
			return nil, fmt.Errorf("contract: duplicate component id %q", comp.ID)
		}
		comps[comp.ID] = true
	}
	for _, t := range c.Types {
		if err := cleanGoFile(t.File); err != nil {
			return nil, fmt.Errorf("contract: type %s: %w", t.ID, err)
		}
	}
	for _, f := range c.Functions {
		if comps[f.ID] || f.ID == briefID {
			return nil, fmt.Errorf("contract: function id %q is also a component or the run id", f.ID)
		}
		if !comps[f.Component] {
			return nil, fmt.Errorf("contract: function %s belongs to unknown component %q", f.ID, f.Component)
		}
		if err := cleanGoFile(f.File); err != nil {
			return nil, fmt.Errorf("contract: function %s: %w", f.ID, err)
		}
		if _, err := parseSignature(f.Signature); err != nil {
			return nil, fmt.Errorf("contract: function %s: %w", f.ID, err)
		}
	}
	return c, nil
}

// cleanGoFile accepts a Go file path that stays inside the repository:
// relative, already clean, no parent steps.
func cleanGoFile(file string) error {
	switch {
	case file == "":
		return errors.New("file is empty")
	case strings.Contains(file, `\`) || path.IsAbs(file):
		return fmt.Errorf("file %q must be a relative path with forward slashes", file)
	case path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../"):
		return fmt.Errorf("file %q must be a clean path inside the repository", file)
	case !strings.HasSuffix(file, ".go"):
		return fmt.Errorf("file %q is not a Go file", file)
	}
	return nil
}

// parseSignature parses one Go function signature line.
func parseSignature(sig string) (*ast.FuncDecl, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+sig+" {}\n", parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("signature %q is not valid Go: %v", sig, err)
	}
	if len(f.Decls) != 1 {
		return nil, fmt.Errorf("signature %q must declare exactly one function", sig)
	}
	fd, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("signature %q is not a function", sig)
	}
	return fd, nil
}

// contractItemSchemas returns the item definitions of the named contract
// arrays ("types", "functions") as JSON, for the prompts.
func contractItemSchemas(names ...string) (string, error) {
	raw, err := schema.Raw(schema.KindContract)
	if err != nil {
		return "", err
	}
	var doc struct {
		Properties map[string]struct {
			Items json.RawMessage `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	out := map[string]json.RawMessage{}
	for _, n := range names {
		out[strings.TrimSuffix(n, "s")] = doc.Properties[n].Items
	}
	b, err := json.MarshalIndent(out, "", "  ")
	return string(b), err
}

func copyDoc(doc map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(raw, &out)
	return out, err
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// objects returns the JSON objects of a decoded array.
func objects(v any) []map[string]any {
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// strList returns the strings of a decoded array.
func strList(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// declaredText lists every declaration written so far, id first, the way a
// component pass needs to see them.
func declaredText(doc map[string]any) string {
	var b strings.Builder
	for _, t := range objects(doc["types"]) {
		fmt.Fprintf(&b, "%v:\n%v\n\n", t["id"], t["decl"])
	}
	for _, f := range objects(doc["functions"]) {
		fmt.Fprintf(&b, "%v: %v\n", f["id"], f["signature"])
	}
	if b.Len() == 0 {
		return "(nothing yet)"
	}
	return strings.TrimRight(b.String(), "\n")
}

func writtenText(doc map[string]any, component string) string {
	var ids []string
	for _, f := range objects(doc["functions"]) {
		if f["component"] == component {
			ids = append(ids, fmt.Sprint(f["id"]))
		}
	}
	if len(ids) == 0 {
		return "(none yet)"
	}
	return strings.Join(ids, ", ")
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a feature heading into the id its component is expected to have.
func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// briefSection is the part of the brief a component came from: the feature
// whose slug is the component id, or the Architecture section.
func briefSection(r *run, component string) string {
	for _, f := range r.brief.Features {
		if slug(f.Name) == component {
			return "### " + f.Name + "\n\n" + f.Body
		}
	}
	return r.brief.Sections["Architecture"]
}
```

In `briefv2/planner/planner.go` add the stage after the clarify line:

```go
	{"contract", (*Planner).contract, contractDone},
```

- [ ] **Step 14: Run everything for this task and commit**

Run: `cd gophermind-lib && gofmt -l briefv2 && go vet ./briefv2/schema/ ./briefv2/tree/ ./briefv2/planner/ && go test ./briefv2/schema/ ./briefv2/tree/ ./briefv2/planner/ -race 2>&1 | tail -4`
Expected: `gofmt` prints nothing; three `ok` lines.

```bash
git add gophermind-lib/briefv2/schema/schema.go \
  gophermind-lib/briefv2/schema/schema_test.go \
  gophermind-lib/briefv2/tree/store.go \
  gophermind-lib/briefv2/tree/store_test.go \
  gophermind-lib/briefv2/planner
git commit -m "feat(briefv2): planner foundation, Load, Clarify, and the contract in passes" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Decompose (batches, leaf checks, the tree skeleton)

**Files:**
- Create: `gophermind-lib/briefv2/planner/decompose.go`, `gophermind-lib/briefv2/planner/prompts/decompose.md`
- Create: `gophermind-lib/briefv2/planner/testdata/greeter/decompose.types.txt`, `decompose.greeting.txt`, `decompose.farewell.txt`
- Modify: `gophermind-lib/briefv2/planner/planner.go` (one line: the stage)
- Test: `gophermind-lib/briefv2/planner/decompose_test.go`, `decompose_internal_test.go`

**Interfaces:**
- Consumes: everything Task 8 produces, in particular `callAsking`, `callSpec`, `render`, `parseSignature`, `briefSection`, `slug`, `objects`, `strList`, `copyDoc`, `mustJSON`, `readJSON`, `writeJSON`, the `run` type; `contract.Contracts` with `Slice(dependsOn []string, self string) ([]string, error)`, `contract.Function`, `contract.Component`; `schema.Validate(schema.KindNode, raw)`; `tree.ParseNode`, `tree.NewStore(dir).Write`, `tree.KindRoot`, `tree.KindComponent`, `tree.KindFunction`; `PlanNode` and `RootTest` (Task 7).
- Produces: `planner.PlanNodes(runDir string) ([]PlanNode, error)`.
- Produces, package-internal, for Tasks 10 and 11: `type decomposed struct{ Components map[string][]map[string]any; Done bool }`; `loadDecomposed(r) (decomposed, error)`; `loadContracts(r) (*contract.Contracts, error)`; `loadClasses(r) (map[string]string, error)`; `(*Planner).decomposeMissing(ctx, r, c, dec *decomposed) error`; `planWaves(rootID string, c, dec) (map[string]int, error)`; `waves(deps map[string][]string) (map[string]int, error)`; `buildSkeleton(r, maxContext, maxRevisions int, c, dec, rootTests []RootTest) (root map[string]any, comps []map[string]any, err error)`; `writeSkeleton(r, maxContext, maxRevisions, c, dec, rootTests) error`; `cut(s string, n int) string`; `firstParagraph(text string) string`; `featureFor(b, component)`.

What this task builds. One model call per batch of at most 8 functions of one component (stage `decompose:<component>`), saved after every call so a resume, or a coverage round that added functions, only asks for what is missing. The model describes each function; everything the harness can derive, it derives and overwrites: `kind`, `parent`, `status`, `brief_ref`, the contract's `package`, `file` and `signature`, `depends_on` (function ids only, from the draft plus the contract's `uses`), and `dependency_signatures`. The leaf checks of spec section 9 run on every draft and make a failing reply malformed.

`node_class` is asked for beside each draft, checked against the closed list, removed from the node document (the node schema has no such field) and kept in `_state/classes.json`.

Component nodes get no `depends_on`. Deriving it from their functions would make cycles that are legal at function level (the handoff's own example has `registration` calling into `health` and `health` into `registration`), and a component's readiness looks at its children only (deviation D2), so nothing needs it.

The leaf check for errors is the mechanical half of the spec's rule: a function whose results include `error` must list at least one condition. "Its doc names a failure" cannot be decided by code and is left to the reviewer of the plan.

- [ ] **Step 1: Write the failing tests**

`briefv2/planner/testdata/greeter/decompose.types.txt`:

```text
[
  {
    "id": "fn-name-error-error",
    "title": "NameError.Error",
    "description": "Return the reason so that *NameError satisfies error.",
    "model_tier": "any",
    "node_class": "pure",
    "depends_on": ["name-error"],
    "contract": {
      "package": "greet",
      "file": "internal/greet/errors.go",
      "signature": "func (e *NameError) Error() string",
      "inputs": [],
      "outputs": [{"name": "reason", "type": "string", "description": "the Reason field, unchanged"}],
      "errors": [],
      "side_effects": []
    }
  }
]
```

`briefv2/planner/testdata/greeter/decompose.greeting.txt`:

````text
```json
[
  {
    "id": "fn-greet",
    "title": "Greet a name",
    "description": "Build the greeting for a name, refusing an empty one.",
    "model_tier": "any",
    "node_class": "validation",
    "depends_on": ["name-error", "fn-name-error-error"],
    "contract": {
      "package": "greet",
      "file": "internal/greet/greet.go",
      "signature": "func Greet(name string) (string, error)",
      "inputs": [{"name": "name", "type": "string", "constraints": ["May be empty", "May have surrounding spaces"]}],
      "outputs": [
        {"name": "message", "type": "string", "description": "Hello, <name>! with the name trimmed"},
        {"name": "err", "type": "error", "description": "nil when the name is usable"}
      ],
      "errors": [{"when": "name is empty after trimming", "returns": "*NameError with Reason \"name is empty\""}],
      "side_effects": []
    }
  }
]
```
````

`briefv2/planner/testdata/greeter/decompose.farewell.txt`:

```text
[
  {
    "id": "fn-farewell",
    "title": "Say goodbye to a name",
    "description": "Build the farewell for a name, refusing an empty one.",
    "model_tier": "any",
    "node_class": "validation",
    "depends_on": [],
    "contract": {
      "package": "greet",
      "file": "internal/greet/farewell.go",
      "signature": "func Farewell(n string) string",
      "inputs": [{"name": "name", "type": "string", "constraints": ["May be empty"]}],
      "outputs": [
        {"name": "message", "type": "string", "description": "Goodbye, <name>! with the name trimmed"},
        {"name": "err", "type": "error", "description": "nil when the name is usable"}
      ],
      "errors": [{"when": "name is empty after trimming", "returns": "*NameError with Reason \"name is empty\""}],
      "side_effects": []
    }
  }
]
```

The farewell draft carries a wrong signature on purpose: the test proves the contract's signature wins.

`briefv2/planner/decompose_test.go`:

```go
package planner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/tree"
)

type draftFile struct {
	Components map[string][]map[string]any `json:"components"`
	Done       bool                        `json:"done"`
}

func (g *rig) drafts() draftFile {
	g.t.Helper()
	var d draftFile
	if err := json.Unmarshal(g.read("_state/decomposed.json"), &d); err != nil {
		g.t.Fatal(err)
	}
	return d
}

func strs(v any) []string {
	var out []string
	arr, _ := v.([]any)
	for _, x := range arr {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func TestDecomposeWritesDraftsClassesAndTheSkeleton(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "decompose"})

	d := g.drafts()
	if !d.Done || len(d.Components["types"]) != 1 || len(d.Components["greeting"]) != 1 || len(d.Components["farewell"]) != 1 {
		t.Fatalf("decomposed.json = done %v, %d components", d.Done, len(d.Components))
	}
	greet := d.Components["greeting"][0]
	if greet["kind"] != "function" || greet["parent"] != "greeting" || greet["status"] != "pending" ||
		greet["brief_ref"] != "#features/greeting" || greet["spec_version"] != "2.0" {
		t.Errorf("harness-owned fields on fn-greet = %v", greet)
	}
	for _, k := range []string{"node_class", "tests", "wave"} {
		if _, ok := greet[k]; ok {
			t.Errorf("draft still carries %q", k)
		}
	}
	// depends_on keeps node ids only; the type reaches the node as a signature.
	if got := strs(greet["depends_on"]); len(got) != 1 || got[0] != "fn-name-error-error" {
		t.Errorf("fn-greet depends_on = %v, want [fn-name-error-error]", got)
	}
	ctx := greet["context"].(map[string]any)
	sigs := strings.Join(strs(ctx["dependency_signatures"]), "\n")
	if !strings.Contains(sigs, "type NameError struct") || !strings.Contains(sigs, "func (e *NameError) Error() string") {
		t.Errorf("fn-greet dependency_signatures = %q", sigs)
	}
	if got := strs(ctx["constraints"]); len(got) != 2 || got[0] != "Standard library only." {
		t.Errorf("fn-greet constraints = %v, want the brief's two", got)
	}
	// The fixture's farewell draft has the wrong signature; the contract's wins.
	fw := d.Components["farewell"][0]["contract"].(map[string]any)
	if fw["signature"] != "func Farewell(name string) (string, error)" {
		t.Errorf("fn-farewell signature = %v, want the contract's", fw["signature"])
	}

	var classes map[string]string
	if err := json.Unmarshal(g.read("_state/classes.json"), &classes); err != nil {
		t.Fatal(err)
	}
	if classes["fn-greet"] != "validation" || classes["fn-name-error-error"] != "pure" || len(classes) != 3 {
		t.Errorf("classes.json = %v", classes)
	}

	// Root and components are real tree nodes already; function nodes are not written yet.
	tr, err := tree.NewStore(g.runDir).Load()
	if err != nil {
		t.Fatalf("the run folder must load as a tree with its working files in place: %v", err)
	}
	if len(tr.Nodes) != 4 {
		t.Errorf("tree has %d nodes, want the root and three components", len(tr.Nodes))
	}
	if err := tr.CheckStructure(); err != nil {
		t.Error(err)
	}
	if kids := tr.Nodes["greeting"].Children; len(kids) != 1 || kids[0] != "fn-greet" {
		t.Errorf("greeting children = %v", kids)
	}
	var comp map[string]any
	if err := json.Unmarshal(g.read("greeting/component.json"), &comp); err != nil {
		t.Fatal(err)
	}
	if tests := comp["tests"].([]any); len(tests) != 1 || tests[0].(map[string]any)["level"] != "integration" {
		t.Errorf("greeting tests = %v, want the contract's integration test", comp["tests"])
	}
	if comp["title"] != "Greeting" || comp["brief_ref"] != "#features/greeting" {
		t.Errorf("greeting title and ref = %v, %v", comp["title"], comp["brief_ref"])
	}

	nodes, err := planner.PlanNodes(g.runDir)
	if err != nil || len(nodes) != 7 {
		t.Fatalf("PlanNodes = %d, %v; want 7", len(nodes), err)
	}
	if nodes[0].Kind != tree.KindRoot || nodes[0].Title != "Greeter" || nodes[6].File != "internal/greet/farewell.go" {
		t.Errorf("PlanNodes = %+v", nodes)
	}

	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "decompose"})
	if len(rows) != 3 || rows[0].Scope != "component" || rows[0].Stage != "decompose:types" {
		t.Errorf("decompose rows = %d (%+v)", len(rows), rows)
	}
}

// nineFunctions builds a fixture whose greeting component has nine functions,
// so Decompose has to make two calls for it (eight, then one).
func nineFunctions(t *testing.T) string {
	var fns, first, second []string
	for i := 1; i <= 9; i++ {
		id := fmt.Sprintf("fn-greet-%d", i)
		sig := fmt.Sprintf("func Greet%d(name string) string", i)
		fns = append(fns, fmt.Sprintf(`{"id": %q, "package": "greet", "file": "internal/greet/greet%d.go", "signature": %q, "doc": "Greeting number %d.", "uses": []}`, id, i, sig, i))
		draft := fmt.Sprintf(`{"id": %q, "title": "Greeting %d", "description": "Greeting number %d.", "node_class": "pure", "depends_on": [],
 "contract": {"inputs": [{"name": "name", "type": "string"}], "outputs": [{"name": "message", "type": "string"}], "errors": [], "side_effects": []}}`, id, i, i)
		if i <= 8 {
			first = append(first, draft)
		} else {
			second = append(second, draft)
		}
	}
	return variant(t, map[string]string{
		"contract.greeting.txt":    `{"types": [], "functions": [` + strings.Join(fns, ",\n") + `], "more": false}`,
		"decompose.greeting.txt":   "[" + strings.Join(first, ",\n") + "]",
		"decompose.greeting.2.txt": "[" + strings.Join(second, ",\n") + "]",
	})
}

func TestDecomposeBatchesALargeComponent(t *testing.T) {
	g := newRig(t, approving(), nineFunctions(t))
	g.mustPlan(planner.Options{StopAfter: "decompose"})
	if n := count(g.stagesCalled(), "decompose:greeting"); n != 2 {
		t.Fatalf("decompose:greeting was called %d times, want 2 (eight functions, then one)", n)
	}
	var asked []int
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:greeting" {
			asked = append(asked, strings.Count(r.Messages[1].Content, `"id": "fn-greet-`))
		}
	}
	if len(asked) != 2 || asked[0] != 8 || asked[1] != 1 {
		t.Errorf("functions per call = %v, want [8 1]", asked)
	}
	if got := len(g.drafts().Components["greeting"]); got != 9 {
		t.Errorf("greeting has %d drafts, want 9", got)
	}
}

const question = "QUESTION:\nShould Greet capitalise the name?\n"

// A QUESTION: reply pauses that one call: the person is asked, the answer is
// stored, and only that call is made again, with the answer in its prompt.
func TestAQuestionPausesOneCallAndRerunsOnlyThatCall(t *testing.T) {
	greeting, err := os.ReadFile(filepath.Join(greeter, "decompose.greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	gate := approving()
	gate.answer = func(q human.Question) string {
		if strings.Contains(q.Text, "capitalise") {
			return "No, keep it as typed."
		}
		return "yes"
	}
	g := newRig(t, gate, variant(t, map[string]string{
		"decompose.greeting.txt":   question,
		"decompose.greeting.2.txt": string(greeting),
	}))
	g.mustPlan(planner.Options{StopAfter: "decompose"})

	calls := g.stagesCalled()
	if count(calls, "decompose:greeting") != 2 || count(calls, "decompose:types") != 1 || count(calls, "decompose:farewell") != 1 {
		t.Fatalf("calls = %v, want the greeting call twice and the others once", calls)
	}
	as := g.answers()
	last := as[len(as)-1]
	if last.Stage != "decompose:greeting" || last.Question != "Should Greet capitalise the name?" || last.Answer != "No, keep it as typed." {
		t.Errorf("stored answer = %+v", last)
	}
	var second string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:greeting" {
			second = r.Messages[1].Content
		}
	}
	if !strings.Contains(second, "Answer from the owner to your question:\nShould Greet capitalise the name?\nNo, keep it as typed.") {
		t.Error("the rerun prompt does not carry the answer")
	}
	if g.has("_state/question.json") {
		t.Error("the pending question was not cleared once answered")
	}
}

func TestAQuestionInAssumeModeGoesBackToTheModel(t *testing.T) {
	greeting, _ := os.ReadFile(filepath.Join(greeter, "decompose.greeting.txt"))
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{
		"decompose.greeting.txt":   question,
		"decompose.greeting.2.txt": string(greeting),
	}))
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		return strings.Replace(s, "on_ambiguity: halt", "on_ambiguity: assume_and_document", 1)
	})
	g.mustPlan(planner.Options{StopAfter: "decompose"})
	if len(gate.asked) != 0 {
		t.Errorf("the gate was asked %d questions in assume mode", len(gate.asked))
	}
	var second string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "decompose:greeting" {
			second = r.Messages[1].Content
		}
	}
	if !strings.Contains(second, "No human is available. Choose the most conservative option") {
		t.Error("the rerun prompt does not tell the model to assume")
	}
}

// With a gate nobody has answered, the question is kept, the run waits, and
// the resume asks the person, not the model, for it.
func TestAPendingQuestionSurvivesAResume(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, variant(t, map[string]string{"decompose.greeting.txt": question}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	gate.err = human.ErrWaiting
	out, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
	if err != nil || out != planner.Waiting {
		t.Fatalf("Run = %q, %v; want waiting", out, err)
	}
	if !g.has("_state/question.json") {
		t.Fatal("the question was not kept for the resume")
	}

	gate.err = nil
	gate.answer = func(q human.Question) string { return "No." }
	g.wire() // the real reply this time
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
	calls := g.stagesCalled()
	if count(calls, "decompose:greeting") != 1 || count(calls, "decompose:types") != 0 {
		t.Errorf("resume calls = %v, want one decompose:greeting and nothing already done", calls)
	}
	if !strings.Contains(g.fake.Requests()[0].Messages[1].Content, "Should Greet capitalise the name?\nNo.") {
		t.Error("the resumed call does not carry the answer")
	}
}

func TestDecomposeResumeSkipsFinishedComponents(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"decompose.farewell.txt": "nope", "decompose.farewell.2.txt": "nope"}))
	if _, err := g.plan(planner.Options{StopAfter: "decompose"}); err == nil {
		t.Fatal("decompose must fail on the broken farewell reply")
	}
	g.wire()
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "decompose"})
	if got := strings.Join(g.stagesCalled(), " "); got != "decompose:farewell" {
		t.Errorf("resume called %q, want only decompose:farewell", got)
	}
}
```

`briefv2/planner/decompose_internal_test.go` (the leaf-check table of spec section 14):

```go
package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/contract"
)

const leafContract = `{"spec_version": "2.0", "brief_id": "gm-2026-09-29-900", "revision": 0, "module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "types": [{"id": "name-error", "package": "x", "file": "internal/x/errors.go", "decl": "type NameError struct{}"}],
 "functions": [
  {"id": "fn-greet", "package": "x", "file": "internal/x/greet.go", "signature": "func Greet(name string, times int) (string, error)", "doc": "d", "uses": ["name-error"], "component": "greeting"},
  {"id": "fn-join", "package": "x", "file": "internal/x/join.go", "signature": "func Join(string, string) string", "doc": "d", "uses": [], "component": "greeting"}],
 "components": [{"id": "greeting", "package": "x", "exports": []}]}`

const okDraft = `{"id": "fn-greet", "title": "Greet", "description": "Greets.", "node_class": "validation", "depends_on": [],
 "contract": {"inputs": [{"name": "name", "type": "string"}, {"name": "times", "type": "int"}],
  "outputs": [{"name": "message", "type": "string"}, {"name": "err", "type": "error"}],
  "errors": [{"when": "name is empty", "returns": "*NameError"}], "side_effects": []}}`

// What every leaf must carry, checked by code. Each bad draft is the good one
// with one thing removed or broken.
func TestLeafChecks(t *testing.T) {
	c, err := contract.Load([]byte(leafContract))
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: "gm-2026-09-29-900", brief: &brief.Brief{}, reqs: []Requirement{{ID: "C1", Kind: ReqConstraint, Text: "Standard library only."}}}
	greet := c.Functions[:1]

	drafts, classes, err := normalizeDrafts("["+okDraft+"]", "greeting", greet, c, r)
	if err != nil {
		t.Fatalf("the good draft was refused: %v", err)
	}
	if classes["fn-greet"] != "validation" || drafts[0]["brief_ref"] != "#architecture" {
		t.Errorf("classes = %v, brief_ref = %v", classes, drafts[0]["brief_ref"])
	}

	bad := []struct{ name, edit, with, want string }{
		{"no input for a parameter", `{"name": "times", "type": "int"}`, `{"name": "count", "type": "int"}`, `no entry for parameter "times"`},
		{"an input without a type", `{"name": "name", "type": "string"}`, `{"name": "name", "type": " "}`, `input "name" has no type`},
		{"an output missing", `, {"name": "err", "type": "error"}`, ``, "outputs has 1 entries for 2 results"},
		{"an output without a type", `{"name": "err", "type": "error"}`, `{"name": "err", "type": ""}`, `output "err" has no type`},
		{"returns error with no errors entry", `{"when": "name is empty", "returns": "*NameError"}`, ``, "returns error but errors lists no condition"},
		{"an errors entry without returns", `"returns": "*NameError"`, `"returns": ""`, "needs both when and returns"},
		{"unknown node_class", `"node_class": "validation"`, `"node_class": "magic"`, `node_class "magic" is not one of pure, validation`},
		{"no node_class", `"node_class": "validation", `, ``, `node_class "" is not one of`},
		{"depends on an id nobody declared", `"depends_on": []`, `"depends_on": ["fn-ghost"]`, "unknown id"},
		{"a field the schema does not have", `"title": "Greet"`, `"title": "Greet", "notes": "x"`, "additional properties"},
		{"no title", `"title": "Greet", `, ``, "title"},
		{"no contract", `"contract": {`, `"agreement": {`, "contract is missing"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			draft := strings.Replace(okDraft, b.edit, b.with, 1)
			if draft == okDraft {
				t.Fatalf("the edit %q did not change the draft", b.edit)
			}
			_, _, err := normalizeDrafts("["+draft+"]", "greeting", greet, c, r)
			if err == nil || !strings.Contains(err.Error(), b.want) {
				t.Errorf("err = %v, want it to contain %q", err, b.want)
			}
		})
	}

	t.Run("a node nobody asked for", func(t *testing.T) {
		extra := strings.Replace(okDraft, `"id": "fn-greet"`, `"id": "fn-extra"`, 1)
		if _, _, err := normalizeDrafts("["+okDraft+","+extra+"]", "greeting", greet, c, r); err == nil || !strings.Contains(err.Error(), "not one of the functions asked for") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a function left out", func(t *testing.T) {
		if _, _, err := normalizeDrafts("[]", "greeting", greet, c, r); err == nil || !strings.Contains(err.Error(), "no node for fn-greet") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("the same node twice", func(t *testing.T) {
		if _, _, err := normalizeDrafts("["+okDraft+","+okDraft+"]", "greeting", greet, c, r); err == nil || !strings.Contains(err.Error(), "two nodes for fn-greet") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("unnamed parameters need one input each", func(t *testing.T) {
		join := c.Functions[1:]
		one := `{"id": "fn-join", "title": "Join", "description": "Joins.", "node_class": "pure",
 "contract": {"inputs": [{"name": "a", "type": "string"}], "outputs": [{"name": "s", "type": "string"}]}}`
		if _, _, err := normalizeDrafts("["+one+"]", "greeting", join, c, r); err == nil || !strings.Contains(err.Error(), "inputs has 1 entries for 2 parameters") {
			t.Errorf("err = %v", err)
		}
		two := strings.Replace(one, `[{"name": "a", "type": "string"}]`, `[{"name": "a", "type": "string"}, {"name": "b", "type": "string"}]`, 1)
		if _, _, err := normalizeDrafts("["+two+"]", "greeting", join, c, r); err != nil {
			t.Errorf("two inputs for two unnamed parameters were refused: %v", err)
		}
	})
	t.Run("an input may name a part of a parameter", func(t *testing.T) {
		part := strings.Replace(okDraft, `{"name": "name", "type": "string"}`, `{"name": "name.first", "type": "string"}`, 1)
		if _, _, err := normalizeDrafts("["+part+"]", "greeting", greet, c, r); err != nil {
			t.Errorf("refused: %v", err)
		}
	})
}

func TestWaves(t *testing.T) {
	got, err := waves(map[string][]string{"root": nil, "a": nil, "b": {"a"}, "c": {"a", "b"}, "d": {"c"}})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"root": 0, "a": 0, "b": 1, "c": 2, "d": 3} {
		if got[id] != want {
			t.Errorf("wave(%s) = %d, want %d", id, got[id], want)
		}
	}
	if _, err := waves(map[string][]string{"a": {"b"}, "b": {"a"}}); err == nil || !strings.Contains(err.Error(), "dependency cycle: a -> b -> a") {
		t.Errorf("cycle err = %v", err)
	}
	if _, err := waves(map[string][]string{"a": {"ghost"}}); err == nil || !strings.Contains(err.Error(), "unknown node") {
		t.Errorf("unknown dependency err = %v", err)
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | head -6`
Expected: the build fails, starting with

```text
# gophermind/gophermind-lib/briefv2/planner [gophermind/gophermind-lib/briefv2/planner.test]
briefv2/planner/decompose_internal_test.go:34:26: undefined: normalizeDrafts
briefv2/planner/decompose_internal_test.go:62:17: undefined: normalizeDrafts
briefv2/planner/decompose_internal_test.go:71:19: undefined: normalizeDrafts
briefv2/planner/decompose_internal_test.go:76:19: undefined: normalizeDrafts
briefv2/planner/decompose_internal_test.go:81:19: undefined: normalizeDrafts
```

- [ ] **Step 2: Write the Decompose prompt** (`briefv2/planner/prompts/decompose.md`)

This is the handoff's `03-decompose.md` with three changes: the batch wording ("the functions below"), the `node_class` list, and the `QUESTION:` sentence. The closing format line lists the fields the parser checks.

```markdown
You are the decomposer for GopherMind. Turn the functions below, all from one component of the contract, into function task nodes. You are given the component, the functions to write nodes for, the type decls and signatures they reference, and the brief section the component came from.

For each function listed produce one node. Rules:
- `id` is the contract function id. Copy `signature`, `package`, and `file` from the contract exactly. Do not rename, reorder parameters, or change types.
- `inputs` has one entry for every parameter of the signature, with the parameter's `name` and `type`, and describes nil-ability, empty-input behavior, and units. `outputs` has one entry for every result.
- `errors` lists every condition that produces a non-nil error or non-zero error code, with `when` and the exact code or sentinel in `returns`. A function that returns `error` has at least one entry.
- `side_effects` lists every call to another contract function, every write, and every network call. Pure functions have an empty array.
- `depends_on` lists the IDs of contract functions and types whose declarations the implementer must see. Use `uses` from the contract as the starting point and add anything the errors or side_effects need.
- `title` is at most 120 characters. `description` is one or two sentences of intent, not implementation.
- Do not write tests. A separate pass does that.
- Do not write `dependency_signatures`. The harness fills them from the contract.
- `model_tier`: `strong` for anything with concurrency, I/O orchestration, or more than three side effects; `standard` for handlers and clients; `any` for pure functions.
- `node_class` is exactly one of:
  - `pure`: no I/O and no state; the result depends only on the arguments.
  - `validation`: checks input and reports what is wrong with it.
  - `handler`: receives a request or command and produces the response.
  - `client`: calls another service over the network.
  - `storage`: reads or writes a database, a file, or another store.
  - `concurrency`: starts goroutines, or coordinates them with channels or locks.
  - `wiring`: constructors, routing, `main`, and other code that connects parts.
  - `other`: none of the above.

If something you need is not stated and cannot be settled by choosing the most conservative option, reply with a line containing only `QUESTION:` followed by your question on the next lines, and nothing else.

Component and the functions to write nodes for:
<component>
{{.Component}}
</component>

Referenced contract entries:
<contract>
{{.ContractSlice}}
</contract>

Brief section:
<brief_section>
{{.BriefSection}}
</brief_section>

Respond with a JSON array of node objects, one per function above, each with `id`, `title`, `description`, `model_tier`, `node_class`, `depends_on`, and `contract` (`package`, `file`, `signature`, `inputs`, `outputs`, `errors`, `side_effects`), and nothing else.
```

- [ ] **Step 3: Implement `decompose.go`**

```go
package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"os"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/schema"
	"gophermind/gophermind-lib/briefv2/tree"
)

// decomposeBatch is how many functions one Decompose call writes nodes for.
// It bounds the size of one reply, not the size of the plan.
const decomposeBatch = 8

// decomposed is _state/decomposed.json: every function node as far as it can
// be written before its tests exist, by component, in the order written.
type decomposed struct {
	Components map[string][]map[string]any `json:"components"`
	Done       bool                        `json:"done"`
}

func loadDecomposed(r *run) (decomposed, error) {
	d := decomposed{}
	_, err := readJSON(r.path(stateDecomposed), &d)
	if d.Components == nil {
		d.Components = map[string][]map[string]any{}
	}
	return d, err
}

func loadClasses(r *run) (map[string]string, error) {
	classes := map[string]string{}
	_, err := readJSON(r.path(stateClasses), &classes)
	return classes, err
}

func loadContracts(r *run) (*contract.Contracts, error) {
	raw, err := os.ReadFile(r.path(fileContracts))
	if err != nil {
		return nil, err
	}
	return contract.Load(raw)
}

func decomposeDone(r *run) bool {
	var d decomposed
	found, err := readJSON(r.path(stateDecomposed), &d)
	return err == nil && found && d.Done
}

// decompose is the Decompose stage: one function node draft per contract
// function, then the root and component nodes.
func (p *Planner) decompose(ctx context.Context, r *run) error {
	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	if err := p.decomposeMissing(ctx, r, c, &dec); err != nil {
		return err
	}
	if _, err := planWaves(r.id, c, dec); err != nil {
		return err
	}
	if err := writeSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, nil); err != nil {
		return err
	}
	dec.Done = true
	return writeJSON(r.path(stateDecomposed), dec)
}

// decomposeMissing writes a draft for every contract function that has none
// yet, component by component, at most decomposeBatch functions per call.
// Progress is saved after every call, so a resume (and a coverage round that
// added functions to the contract) only asks for what is missing.
func (p *Planner) decomposeMissing(ctx context.Context, r *run, c *contract.Contracts, dec *decomposed) error {
	classes, err := loadClasses(r)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, drafts := range dec.Components {
		for _, d := range drafts {
			id, _ := d["id"].(string)
			have[id] = true
		}
	}
	for _, comp := range c.Components {
		var missing []contract.Function
		for _, f := range c.Functions {
			if f.Component == comp.ID && !have[f.ID] {
				missing = append(missing, f)
			}
		}
		for len(missing) > 0 {
			n := min(decomposeBatch, len(missing))
			batch := missing[:n]
			missing = missing[n:]
			drafts, got, err := p.decomposeCall(ctx, r, c, comp, batch)
			if err != nil {
				return err
			}
			dec.Components[comp.ID] = append(dec.Components[comp.ID], drafts...)
			for id, class := range got {
				classes[id] = class
			}
			if err := writeJSON(r.path(stateClasses), classes); err != nil {
				return err
			}
			if err := writeJSON(r.path(stateDecomposed), dec); err != nil {
				return err
			}
		}
	}
	return nil
}

// decomposeCall asks for the nodes of one batch of one component's functions.
func (p *Planner) decomposeCall(ctx context.Context, r *run, c *contract.Contracts, comp contract.Component, batch []contract.Function) ([]map[string]any, map[string]string, error) {
	var uses []string
	for _, f := range batch {
		uses = append(uses, f.Uses...)
	}
	slice, err := c.Slice(uses, "")
	if err != nil {
		return nil, nil, err
	}
	sliceText := strings.Join(slice, "\n\n")
	if sliceText == "" {
		sliceText = "(these functions reference no other contract entry)"
	}
	prompt, err := render("decompose", map[string]string{
		"Component":     mustJSON(map[string]any{"component": comp, "functions": batch}),
		"ContractSlice": sliceText,
		"BriefSection":  briefSection(r, comp.ID),
	})
	if err != nil {
		return nil, nil, err
	}
	var drafts []map[string]any
	var classes map[string]string
	cs := callSpec{stage: "decompose:" + comp.ID, taskType: "decompose", scope: router.ScopeComponent, maxTokens: maxTokensDecompose}
	err = p.callAsking(ctx, r, cs, prompt, func(text string) error {
		d, cl, err := normalizeDrafts(StripReply(text), comp.ID, batch, c, r)
		if err != nil {
			return err
		}
		drafts, classes = d, cl
		return nil
	})
	return drafts, classes, err
}

// nodeClasses is the closed list a draft's node_class must come from.
var nodeClasses = []string{"pure", "validation", "handler", "client", "storage", "concurrency", "wiring", "other"}

// normalizeDrafts turns a Decompose reply into function node drafts: exactly
// one per function asked for, with every field the harness owns set or
// overwritten by code, and every leaf check applied. Any failure is an error,
// which makes the reply malformed. The second result maps node id to the
// node_class the model gave; the class is not part of the node document.
func normalizeDrafts(text, component string, fns []contract.Function, c *contract.Contracts, r *run) ([]map[string]any, map[string]string, error) {
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		return nil, nil, fmt.Errorf("decompose reply is not a JSON array of nodes: %w", err)
	}
	want := map[string]bool{}
	for _, f := range fns {
		want[f.ID] = true
	}
	byID := map[string]map[string]any{}
	for _, d := range got {
		id, _ := d["id"].(string)
		switch {
		case !want[id]:
			return nil, nil, fmt.Errorf("decompose reply has a node %q, which is not one of the functions asked for", id)
		case byID[id] != nil:
			return nil, nil, fmt.Errorf("decompose reply has two nodes for %s", id)
		}
		byID[id] = d
	}
	isFunction := map[string]bool{}
	for _, f := range c.Functions {
		isFunction[f.ID] = true
	}
	var constraints []any
	for _, q := range r.reqs {
		if q.Kind == ReqConstraint {
			constraints = append(constraints, q.Text)
		}
	}
	ref := "#architecture"
	for _, f := range r.brief.Features {
		if slug(f.Name) == component {
			ref = "#features/" + component
		}
	}

	out := make([]map[string]any, 0, len(fns))
	classes := map[string]string{}
	for _, f := range fns {
		d := byID[f.ID]
		if d == nil {
			return nil, nil, fmt.Errorf("decompose reply has no node for %s", f.ID)
		}
		class, _ := d["node_class"].(string)
		if !contains(nodeClasses, class) {
			return nil, nil, fmt.Errorf("node %s: node_class %q is not one of %s", f.ID, class, strings.Join(nodeClasses, ", "))
		}
		classes[f.ID] = class
		for _, k := range []string{"node_class", "tests", "wave", "claim", "attempts", "result"} {
			delete(d, k)
		}
		d["spec_version"], d["kind"], d["parent"] = "2.0", "function", component
		d["status"], d["revision"], d["brief_ref"] = "pending", 0, ref
		if t, _ := d["title"].(string); len([]rune(t)) > 120 {
			d["title"] = string([]rune(t)[:120])
		}
		if _, ok := d["model_tier"]; !ok {
			d["model_tier"] = "standard"
		}
		ct, ok := d["contract"].(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("node %s: contract is missing", f.ID)
		}
		ct["package"], ct["file"], ct["signature"] = f.Package, f.File, f.Signature
		fd, err := parseSignature(f.Signature)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", f.ID, err)
		}
		if err := checkLeaf(ct, fd); err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", f.ID, err)
		}

		// depends_on holds node ids only; types reach the node through its
		// dependency signatures.
		deps := append(strList(d["depends_on"]), f.Uses...)
		sigs, err := c.Slice(deps, f.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: depends_on: %w", f.ID, err)
		}
		fnDeps := []string{}
		for _, id := range deps {
			if isFunction[id] && id != f.ID && !contains(fnDeps, id) {
				fnDeps = append(fnDeps, id)
			}
		}
		sort.Strings(fnDeps)
		d["depends_on"] = fnDeps
		nctx, _ := d["context"].(map[string]any)
		if nctx == nil {
			nctx = map[string]any{}
		}
		nctx["dependency_signatures"] = sigs
		if _, ok := nctx["constraints"]; !ok && len(constraints) > 0 {
			nctx["constraints"] = constraints
		}
		d["context"] = nctx

		if err := validateDraft(d); err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", f.ID, err)
		}
		out = append(out, d)
	}
	return out, classes, nil
}

// checkLeaf is what every leaf must carry before it may enter the plan: an
// input for every parameter, an output for every result, and an error entry
// when the function can fail.
func checkLeaf(ct map[string]any, fd *ast.FuncDecl) error {
	inputs := objects(ct["inputs"])
	for _, in := range inputs {
		if s, _ := in["type"].(string); strings.TrimSpace(s) == "" {
			return fmt.Errorf("input %q has no type", in["name"])
		}
	}
	params, unnamed := 0, false
	if fd.Type.Params != nil {
		for _, field := range fd.Type.Params.List {
			if len(field.Names) == 0 {
				params++
				unnamed = true
				continue
			}
			for _, name := range field.Names {
				params++
				if name.Name == "_" {
					unnamed = true
					continue
				}
				if !hasInput(inputs, name.Name) {
					return fmt.Errorf("inputs has no entry for parameter %q", name.Name)
				}
			}
		}
	}
	if unnamed && len(inputs) < params {
		return fmt.Errorf("inputs has %d entries for %d parameters", len(inputs), params)
	}

	results, returnsError := 0, false
	if fd.Type.Results != nil {
		for _, field := range fd.Type.Results.List {
			results += max(1, len(field.Names))
			if id, ok := field.Type.(*ast.Ident); ok && id.Name == "error" {
				returnsError = true
			}
		}
	}
	outputs := objects(ct["outputs"])
	if len(outputs) < results {
		return fmt.Errorf("outputs has %d entries for %d results", len(outputs), results)
	}
	for _, o := range outputs {
		if s, _ := o["type"].(string); strings.TrimSpace(s) == "" {
			return fmt.Errorf("output %q has no type", o["name"])
		}
	}

	errs := objects(ct["errors"])
	for i, e := range errs {
		when, _ := e["when"].(string)
		returns, _ := e["returns"].(string)
		if strings.TrimSpace(when) == "" || strings.TrimSpace(returns) == "" {
			return fmt.Errorf("errors entry %d needs both when and returns", i+1)
		}
	}
	if returnsError && len(errs) == 0 {
		return errors.New("the function returns error but errors lists no condition")
	}
	return nil
}

// hasInput reports whether inputs describes the parameter: an entry named
// exactly like it, or one naming a part of it (r.Body for r).
func hasInput(inputs []map[string]any, param string) bool {
	for _, in := range inputs {
		name, _ := in["name"].(string)
		if name == param || strings.HasPrefix(name, param+".") {
			return true
		}
	}
	return false
}

// validateDraft checks a draft against the node schema. A function node is
// only schema-valid once it has tests and a wave, which come later, so the
// check runs on a copy that has placeholders for both.
func validateDraft(d map[string]any) error {
	cp, err := copyDoc(d)
	if err != nil {
		return err
	}
	cp["wave"] = 0
	cp["tests"] = []any{map[string]any{"name": "placeholder", "level": "unit", "given": "", "expect": "", "command": "true"}}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	return schema.Validate(schema.KindNode, raw)
}

// waves computes wave(n) = 0 with no dependencies, else 1 + the highest wave
// among them, the same rule as tree.ComputeWaves. The planner needs it before
// function nodes are valid tree nodes (decision L3).
func waves(deps map[string][]string) (map[string]int, error) {
	const visiting = -1
	out := map[string]int{}
	var visit func(id string, trail []string) (int, error)
	visit = func(id string, trail []string) (int, error) {
		if w, ok := out[id]; ok {
			if w == visiting {
				return 0, fmt.Errorf("dependency cycle: %s", strings.Join(append(trail, id), " -> "))
			}
			return w, nil
		}
		out[id] = visiting
		w := 0
		for _, d := range deps[id] {
			if _, ok := deps[d]; !ok {
				return 0, fmt.Errorf("node %q depends on unknown node %q", id, d)
			}
			dw, err := visit(d, append(trail, id))
			if err != nil {
				return 0, err
			}
			w = max(w, dw+1)
		}
		out[id] = w
		return w, nil
	}
	ids := make([]string, 0, len(deps))
	for id := range deps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := visit(id, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// planWaves is waves over the whole plan: the root and the components have
// no dependencies of their own, a function depends on other functions.
func planWaves(rootID string, c *contract.Contracts, dec decomposed) (map[string]int, error) {
	deps := map[string][]string{rootID: nil}
	for _, comp := range c.Components {
		deps[comp.ID] = nil
		for _, d := range dec.Components[comp.ID] {
			id, _ := d["id"].(string)
			deps[id] = strList(d["depends_on"])
		}
	}
	return waves(deps)
}

// writeSkeleton writes root.json and every <component>/component.json. It is
// called again whenever they change: when coverage adds root tests or
// functions.
func writeSkeleton(r *run, maxContext, maxRevisions int, c *contract.Contracts, dec decomposed, rootTests []RootTest) error {
	root, comps, err := buildSkeleton(r, maxContext, maxRevisions, c, dec, rootTests)
	if err != nil {
		return err
	}
	store := tree.NewStore(r.dir)
	for _, doc := range append([]map[string]any{root}, comps...) {
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		n, err := tree.ParseNode(raw)
		if err != nil {
			return fmt.Errorf("node %v: %w", doc["id"], err)
		}
		if err := store.Write(n); err != nil {
			return err
		}
	}
	return nil
}

// buildSkeleton makes the root and component node documents. Everything in
// them is derived by code from the brief, the contract and the drafts.
func buildSkeleton(r *run, maxContext, maxRevisions int, c *contract.Contracts, dec decomposed, rootTests []RootTest) (map[string]any, []map[string]any, error) {
	var integ struct {
		Components []struct {
			ID    string `json:"id"`
			Tests []struct {
				Name    string `json:"name"`
				Given   string `json:"given"`
				Expect  string `json:"expect"`
				Command string `json:"command"`
			} `json:"integration_tests"`
		} `json:"components"`
	}
	if _, err := readJSON(r.path(fileContracts), &integ); err != nil {
		return nil, nil, err
	}
	integration := map[string][]any{}
	for _, comp := range integ.Components {
		for _, t := range comp.Tests {
			test := map[string]any{"name": t.Name, "level": "integration", "given": t.Given, "expect": t.Expect}
			if t.Command != "" {
				test["command"] = t.Command
			}
			integration[comp.ID] = append(integration[comp.ID], test)
		}
	}

	front := r.brief.Front
	budget := map[string]any{"max_context_tokens": maxContext, "max_revisions": maxRevisions}
	if front.Budget != nil {
		if front.Budget.MaxContextTokens > 0 {
			budget["max_context_tokens"] = front.Budget.MaxContextTokens
		}
		if front.Budget.MaxRevisions != nil {
			budget["max_revisions"] = *front.Budget.MaxRevisions
		}
	}
	children := []any{}
	for _, comp := range c.Components {
		children = append(children, comp.ID)
	}
	root := map[string]any{
		"spec_version": "2.0", "id": r.id, "kind": "root", "children": children,
		"title": cut(front.Title, 120), "description": firstParagraph(r.brief.Sections["Overview"]),
		"brief_ref": "#overview", "status": "pending", "wave": 0, "model_tier": "strong", "revision": 0,
		"budget": budget,
	}
	var constraints []any
	for _, q := range r.reqs {
		if q.Kind == ReqConstraint {
			constraints = append(constraints, q.Text)
		}
	}
	if len(constraints) > 0 {
		root["context"] = map[string]any{"constraints": constraints}
	}
	if len(front.Secrets) > 0 {
		names := []any{}
		for _, s := range front.Secrets {
			names = append(names, s.Name)
		}
		root["secrets"] = names
	}
	if len(front.Network) > 0 {
		hosts := []any{}
		for _, n := range front.Network {
			hosts = append(hosts, map[string]any{"host": n.Host, "critical": n.Critical})
		}
		root["network"] = hosts
	}
	if len(rootTests) > 0 {
		tests := []any{}
		for _, t := range rootTests {
			tests = append(tests, map[string]any{"name": t.Requirement + ": " + t.Name, "level": "acceptance",
				"given": t.Given, "expect": t.Expect, "command": t.Command})
		}
		root["tests"] = tests
	}

	var comps []map[string]any
	for _, comp := range c.Components {
		title, desc, ref := comp.ID, "Shared declarations.", "#architecture"
		if f, ok := featureFor(r.brief, comp.ID); ok {
			title, ref = cut(f.Name, 120), "#features/"+comp.ID
			if p := firstParagraph(f.Body); p != "" {
				desc = p
			}
		}
		kids := []any{}
		for _, d := range dec.Components[comp.ID] {
			kids = append(kids, d["id"])
		}
		doc := map[string]any{
			"spec_version": "2.0", "id": comp.ID, "kind": "component", "parent": r.id, "children": kids,
			"title": title, "description": desc, "brief_ref": ref,
			"status": "pending", "wave": 0, "model_tier": "strong", "revision": 0,
		}
		if tests := integration[comp.ID]; len(tests) > 0 {
			doc["tests"] = tests
		}
		comps = append(comps, doc)
	}
	return root, comps, nil
}

func featureFor(b *brief.Brief, component string) (brief.Feature, bool) {
	for _, f := range b.Features {
		if slug(f.Name) == component {
			return f, true
		}
	}
	return brief.Feature{}, false
}

func cut(s string, n int) string {
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n])
	}
	return s
}

// firstParagraph is the text up to the first blank line, on one line.
func firstParagraph(text string) string {
	para, _, _ := strings.Cut(strings.TrimSpace(text), "\n\n")
	return strings.Join(strings.Fields(para), " ")
}

// PlanNodes lists the plan's nodes the way the coverage check and the
// approval summary need them: the root, every component, and every function
// draft, read from the run folder.
func PlanNodes(runDir string) ([]PlanNode, error) {
	r := &run{dir: runDir}
	c, err := loadContracts(r)
	if err != nil {
		return nil, err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(r.path(fileBrief))
	if err != nil {
		return nil, err
	}
	b, err := brief.Parse(src)
	if err != nil {
		return nil, err
	}
	nodes := []PlanNode{{ID: c.BriefID, Kind: tree.KindRoot, Title: b.Front.Title}}
	for _, comp := range c.Components {
		title := comp.ID
		if f, ok := featureFor(b, comp.ID); ok {
			title = f.Name
		}
		nodes = append(nodes, PlanNode{ID: comp.ID, Kind: tree.KindComponent, Parent: c.BriefID, Title: title})
	}
	for _, comp := range c.Components {
		for _, d := range dec.Components[comp.ID] {
			id, _ := d["id"].(string)
			title, _ := d["title"].(string)
			ct, _ := d["contract"].(map[string]any)
			file, _ := ct["file"].(string)
			nodes = append(nodes, PlanNode{ID: id, Kind: tree.KindFunction, Parent: comp.ID, Title: title, File: file})
		}
	}
	return nodes, nil
}
```

In `briefv2/planner/planner.go` add the stage after the contract line:

```go
	{"decompose", (*Planner).decompose, decomposeDone},
```

- [ ] **Step 4: Run and commit**

Run: `cd gophermind-lib && gofmt -l briefv2/planner && go vet ./briefv2/planner/ && go test ./briefv2/planner/ -race 2>&1 | tail -3`
Expected: `gofmt` prints nothing; `ok  gophermind/gophermind-lib/briefv2/planner`.

```bash
git add gophermind-lib/briefv2/planner
git commit -m "feat(briefv2): Decompose in batches with leaf checks and the tree skeleton" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 10: The Coverage stage

**Files:**
- Modify: `gophermind-lib/briefv2/planner/coverage_check.go` (add `StrayCommandWarnings`), `gophermind-lib/briefv2/planner/coverage_check_test.go`
- Create: `gophermind-lib/briefv2/planner/coverage.go`, `gophermind-lib/briefv2/planner/prompts/coverage.md`, `coverage_fill.md`
- Create: `gophermind-lib/briefv2/planner/testdata/greeter/coverage.txt`, `testdata/greeter-gap/coverage.txt`, `coverage_fill.txt`, `decompose.greeting.2.txt`, `testdata/greeter-stuck/coverage.txt`, `coverage_fill.txt`
- Modify: `gophermind-lib/briefv2/planner/planner.go` (one line: the stage)
- Test: `gophermind-lib/briefv2/planner/coverage_test.go`

**Interfaces:**
- Consumes: `ParseCoverageReply`, `CheckCoverage`, `CoverageReply` with `Merge`, `Covered`, `Gap`, `RootTest`, `PlanNode`, `PathWarnings`, `CommandWarnings`, `backtickRE` (Task 7); `PlanNodes`, `decomposeMissing`, `planWaves`, `writeSkeleton`, `loadContracts`, `loadDecomposed` (Task 9); `call`, `callSpec`, `render`, `validateContractDoc`, `contractItemSchemas`, `copyDoc`, `objects`, `cut` (Tasks 8 and 9); `events.KindCoverageGap`, `events.KindWarning`; `settings.Defaults.MaxCoverageRounds`.
- Produces: `planner.StrayCommandWarnings(briefSrc []byte, nodes []PlanNode) []string`; `planner.CoverageFile{Rounds int; Covered []Covered; RootTests []RootTest; Warnings []string}`; `planner.ReadCoverage(runDir string) (CoverageFile, error)`; `*planner.CoverageError{Gaps []Gap}`.
- Produces, package-internal, for Task 11: `oneLine(text string, n int) string`; `(*Planner).rewriteSkeleton(r, rootTests) error`.

What this task builds. The stage that compares the plan with the brief (spec section 9, "Coverage"). One `coverage` call proposes which nodes and root tests satisfy each requirement; `CheckCoverage` decides. Gaps go back in a `coverage_fill` call, up to `defaults.max_coverage_rounds` times. A fill reply may declare functions and types that are missing from the contract: they are validated with the rest of the contract (revision plus one), written to `contracts.json`, and decomposed by the same code as Task 9 before coverage is checked again. If a gap is left the stage returns `*CoverageError`, `coverage.json` is not written, and nothing later can run. When everything is covered the root node gets its acceptance tests and `coverage.json` is written last, because its existence is what marks the stage done.

Both calls are scope `brief` (they carry every constraint and acceptance bullet), so under `need_to_know` they stay on private providers. A fill call is recorded with task type `coverage`.

- [ ] **Step 1: Write the failing test for the stray-command warning**

Append to `briefv2/planner/coverage_check_test.go`:

```go

func TestStrayCommandWarnings(t *testing.T) {
	src := []byte("The binary is `cmd/greeter`. Run `cmd/tool/main.go` by hand. Not a path: `cmd`.")
	nodes := []planner.PlanNode{
		{ID: "fn-main", Kind: tree.KindFunction, File: "cmd/greeter/main.go"},
		{ID: "fn-tool", Kind: tree.KindFunction, File: "cmd/tool/main.go"},
		{ID: "fn-a", Kind: tree.KindFunction, File: "cmd/server/main.go"},
		{ID: "fn-b", Kind: tree.KindFunction, File: "cmd/server/router.go"},
		{ID: "fn-c", Kind: tree.KindFunction, File: "internal/cmd/x.go"},
		{ID: "comp", Kind: tree.KindComponent, File: "cmd/ghost/x.go"},
	}
	got := planner.StrayCommandWarnings(src, nodes)
	want := []string{"`cmd/server` holds plan files but the brief never names it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

// The plan that motivated the coverage stage put a tool in cmd/fake-llm
// although the brief names one binary, cmd/venture-server. (That plan also
// touched cmd/server, but never as a step's first file, which is the only one
// nodes.json keeps.)
func TestGoldenPlanHasAStrayCommand(t *testing.T) {
	src, err := os.ReadFile(aivsBrief)
	if err != nil {
		t.Fatal(err)
	}
	var nodes []planner.PlanNode
	readJSON(t, "testdata/aivs/nodes.json", &nodes)
	got := planner.StrayCommandWarnings(src, nodes)
	want := []string{"`cmd/fake-llm` holds plan files but the brief never names it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | head -4`
Expected:

```text
# gophermind/gophermind-lib/briefv2/planner_test [gophermind/gophermind-lib/briefv2/planner.test]
briefv2/planner/coverage_check_test.go:285:17: undefined: planner.StrayCommandWarnings
briefv2/planner/coverage_check_test.go:303:17: undefined: planner.StrayCommandWarnings
FAIL	gophermind/gophermind-lib/briefv2/planner [build failed]
```

- [ ] **Step 2: Implement `StrayCommandWarnings`**

Append to `briefv2/planner/coverage_check.go`:

```go

// StrayCommandWarnings lists each cmd/<name> directory that holds plan files
// although the brief never names it. A plan that spreads its entry point over
// cmd/server and cmd/fake-llm when the brief names one binary shows up here.
func StrayCommandWarnings(briefSrc []byte, nodes []PlanNode) []string {
	named := map[string]bool{}
	for _, m := range backtickRE.FindAllSubmatch(briefSrc, -1) {
		parts := strings.Split(string(m[1]), "/")
		if len(parts) >= 2 && parts[0] == "cmd" && parts[1] != "" {
			named[parts[1]] = true
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, n := range nodes {
		parts := strings.Split(n.File, "/")
		if n.Kind != tree.KindFunction || len(parts) < 3 || parts[0] != "cmd" {
			continue
		}
		if name := parts[1]; !named[name] && !seen[name] {
			seen[name] = true
			out = append(out, "`cmd/"+name+"` holds plan files but the brief never names it")
		}
	}
	return out
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ -run 'Stray|Golden' 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 3: Write the failing Coverage tests**

The complete reply, used by every test that is not about gaps:

`briefv2/planner/testdata/greeter/coverage.txt`:

```text
{
  "map": [
    {"requirement": "F1", "nodes": ["greeting"]},
    {"requirement": "F2", "nodes": ["farewell"]},
    {"requirement": "C1", "nodes": ["fn-greet", "fn-farewell", "fn-name-error-error"]},
    {"requirement": "C2", "nodes": []},
    {"requirement": "A1", "nodes": []},
    {"requirement": "A2", "nodes": []},
    {"requirement": "A3", "nodes": ["fn-greet", "fn-farewell"]}
  ],
  "root_tests": [
    {"requirement": "C2", "name": "formatted and vetted", "given": "the full tree is verified", "expect": "gofmt lists nothing and vet exits 0", "command": "test -z \"$(gofmt -l .)\" && go vet ./..."},
    {"requirement": "A1", "name": "builds", "given": "the full tree is verified", "expect": "exit 0", "command": "go build ./..."},
    {"requirement": "A2", "name": "vet clean", "given": "the full tree is verified", "expect": "exit 0", "command": "go vet ./..."},
    {"requirement": "A3", "name": "all tests pass", "given": "the full tree is verified", "expect": "exit 0", "command": "go test ./..."}
  ]
}
```

`greeter-gap` overrides single replies: its coverage reply leaves `C1` without a node and `A3` without a root test, and its fill reply closes both, one of them by declaring a new function, which then needs a second Decompose call for `greeting` and (in Task 11) a test-writer reply.

`briefv2/planner/testdata/greeter-gap/coverage.txt`:

```text
{
  "map": [
    {"requirement": "F1", "nodes": ["greeting"]},
    {"requirement": "F2", "nodes": ["farewell"]},
    {"requirement": "C1", "nodes": []},
    {"requirement": "C2", "nodes": []},
    {"requirement": "A1", "nodes": []},
    {"requirement": "A2", "nodes": []},
    {"requirement": "A3", "nodes": ["fn-greet"]}
  ],
  "root_tests": [
    {"requirement": "C2", "name": "formatted and vetted", "given": "the full tree is verified", "expect": "gofmt lists nothing and vet exits 0", "command": "test -z \"$(gofmt -l .)\" && go vet ./..."},
    {"requirement": "A1", "name": "builds", "given": "the full tree is verified", "expect": "exit 0", "command": "go build ./..."},
    {"requirement": "A2", "name": "vet clean", "given": "the full tree is verified", "expect": "exit 0", "command": "go vet ./..."}
  ]
}
```

`briefv2/planner/testdata/greeter-gap/coverage_fill.txt`:

```text
{
  "map": [{"requirement": "C1", "nodes": ["fn-imports-are-standard"]}],
  "root_tests": [
    {"requirement": "A3", "name": "all tests pass", "given": "the full tree is verified", "expect": "exit 0", "command": "go test ./..."}
  ],
  "types": [],
  "functions": [
    {"id": "fn-imports-are-standard", "package": "greet", "file": "internal/greet/imports.go",
     "signature": "func ImportsAreStandard(paths []string) bool",
     "doc": "ImportsAreStandard reports whether every import path belongs to the standard library.",
     "uses": [], "component": "greeting"}
  ]
}
```

`briefv2/planner/testdata/greeter-gap/decompose.greeting.2.txt`:

```text
[
  {
    "id": "fn-imports-are-standard",
    "title": "Check that imports are standard library only",
    "description": "Report whether every import path given belongs to the standard library.",
    "model_tier": "any",
    "node_class": "pure",
    "depends_on": [],
    "contract": {
      "package": "greet",
      "file": "internal/greet/imports.go",
      "signature": "func ImportsAreStandard(paths []string) bool",
      "inputs": [{"name": "paths", "type": "[]string", "constraints": ["May be empty"]}],
      "outputs": [{"name": "ok", "type": "bool", "description": "true when no path has a dot in its first element"}],
      "errors": [],
      "side_effects": []
    }
  }
]
```

`greeter-stuck` has the same coverage reply and a fill reply that still leaves `A3` uncovered:

`briefv2/planner/testdata/greeter-stuck/coverage.txt`:

```text
{
  "map": [
    {"requirement": "F1", "nodes": ["greeting"]},
    {"requirement": "F2", "nodes": ["farewell"]},
    {"requirement": "C1", "nodes": []},
    {"requirement": "C2", "nodes": []},
    {"requirement": "A1", "nodes": []},
    {"requirement": "A2", "nodes": []},
    {"requirement": "A3", "nodes": ["fn-greet"]}
  ],
  "root_tests": [
    {"requirement": "C2", "name": "formatted and vetted", "given": "the full tree is verified", "expect": "gofmt lists nothing and vet exits 0", "command": "test -z \"$(gofmt -l .)\" && go vet ./..."},
    {"requirement": "A1", "name": "builds", "given": "the full tree is verified", "expect": "exit 0", "command": "go build ./..."},
    {"requirement": "A2", "name": "vet clean", "given": "the full tree is verified", "expect": "exit 0", "command": "go vet ./..."}
  ]
}
```

`briefv2/planner/testdata/greeter-stuck/coverage_fill.txt`:

```text
{"map": [{"requirement": "C1", "nodes": ["fn-greet"]}], "root_tests": []}
```

`briefv2/planner/coverage_test.go`:

```go
package planner_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

const (
	greeterGap   = "testdata/greeter-gap"
	greeterStuck = "testdata/greeter-stuck"
)

func (g *rig) rootTests() []map[string]any {
	g.t.Helper()
	var root struct {
		Tests []map[string]any `json:"tests"`
	}
	if err := json.Unmarshal(g.read("root.json"), &root); err != nil {
		g.t.Fatal(err)
	}
	return root.Tests
}

func TestCoverageMapsEveryRequirement(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "coverage"})

	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Rounds != 0 || len(cov.Covered) != 7 || len(cov.RootTests) != 4 || len(cov.Warnings) != 0 {
		t.Fatalf("coverage.json = %d rounds, %d covered, %d root tests, warnings %q", cov.Rounds, len(cov.Covered), len(cov.RootTests), cov.Warnings)
	}
	tests := g.rootTests()
	if len(tests) != 4 || tests[1]["name"] != "A1: builds" || tests[1]["level"] != "acceptance" || tests[1]["command"] != "go build ./..." {
		t.Errorf("root tests = %v", tests)
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil || st.Requirements != 7 || st.Covered != 7 {
		t.Errorf("status = %d of %d covered, %v", st.Covered, st.Requirements, err)
	}
	if n := count(g.stagesCalled(), "coverage"); n != 1 || count(g.stagesCalled(), "coverage_fill") != 0 {
		t.Errorf("coverage calls = %d, fill calls = %d", n, count(g.stagesCalled(), "coverage_fill"))
	}
	var prompt string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "coverage" {
			prompt = r.Messages[1].Content
		}
	}
	for _, want := range []string{"F1 (feature, line", "Greeting: `Greet(name)` returns", "C2 (constraint, line", "A3 (acceptance, line", "fn-greet | function | greeting | Greet a name | internal/greet/greet.go"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("coverage prompt lacks %q", want)
		}
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "coverage"})
	if len(rows) != 1 || rows[0].Scope != "brief" {
		t.Errorf("coverage rows = %+v", rows)
	}
}

// The canned coverage reply leaves an acceptance bullet without a root test
// and a constraint without a node. The fill reply adds a root test and
// declares a new function, which has to be decomposed before the plan counts
// as covered.
func TestCoverageFillClosesGapsAndDecomposesNewFunctions(t *testing.T) {
	g := newRig(t, approving(), greeterGap)
	g.mustPlan(planner.Options{StopAfter: "coverage"})

	calls := g.stagesCalled()
	if count(calls, "coverage") != 1 || count(calls, "coverage_fill") != 1 || count(calls, "decompose:greeting") != 2 {
		t.Fatalf("calls = %v, want one coverage, one fill and a second decompose for greeting", calls)
	}
	var fill, redo string
	for _, r := range g.fake.Requests() {
		switch planner.StageOf(r) {
		case "coverage_fill":
			fill = r.Messages[1].Content
		case "decompose:greeting":
			redo = r.Messages[1].Content
		}
	}
	for _, want := range []string{"C1 (line", "reason: no node and no root test", "A3 (line", "reason: acceptance bullet has no root test", "greeting (package greet)"} {
		if !strings.Contains(fill, want) {
			t.Errorf("fill prompt lacks %q", want)
		}
	}
	if !strings.Contains(redo, "fn-imports-are-standard") || strings.Contains(redo, `"id": "fn-greet"`) {
		t.Error("the second decompose call must ask for the new function only")
	}

	c := g.contracts()
	if c.Revision != 1 || len(c.Functions) != 4 {
		t.Errorf("contract revision %d with %d functions, want 1 and 4", c.Revision, len(c.Functions))
	}
	if got := len(g.drafts().Components["greeting"]); got != 2 {
		t.Errorf("greeting has %d drafts, want 2", got)
	}
	var comp struct {
		Children []string `json:"children"`
	}
	if err := json.Unmarshal(g.read("greeting/component.json"), &comp); err != nil || strings.Join(comp.Children, " ") != "fn-greet fn-imports-are-standard" {
		t.Errorf("greeting children = %v, %v", comp.Children, err)
	}
	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Rounds != 1 || len(cov.Covered) != 7 || len(cov.RootTests) != 4 {
		t.Errorf("coverage.json = %d rounds, %d covered, %d root tests", cov.Rounds, len(cov.Covered), len(cov.RootTests))
	}
	for _, cv := range cov.Covered {
		if cv.Requirement == "C1" && strings.Join(cv.Nodes, " ") != "fn-imports-are-standard" {
			t.Errorf("C1 is covered by %v", cv.Nodes)
		}
	}
	if gaps := g.sink.OfKind(events.KindCoverageGap); len(gaps) != 2 || !strings.Contains(gaps[0].Message, "round 1: C1") {
		t.Errorf("coverage_gap events = %+v", gaps)
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "coverage_fill"})
	if len(rows) != 1 || rows[0].TaskType != "coverage" {
		t.Errorf("fill rows = %+v (a fill call is coverage work)", rows)
	}
}

// A fill that still leaves a gap stops the run in Coverage. Nothing is
// approved, and the resume repeats only that stage.
func TestCoverageStopsWhenAGapIsLeft(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, greeterStuck)
	_, err := g.plan(planner.Options{})
	var ce *planner.CoverageError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a *CoverageError", err)
	}
	if len(ce.Gaps) != 1 || ce.Gaps[0].Requirement != "A3" || ce.Gaps[0].Reason != "acceptance bullet has no root test" {
		t.Fatalf("gaps = %+v", ce.Gaps)
	}
	if !strings.Contains(err.Error(), "A3 (line") || !strings.Contains(err.Error(), "`go test ./...` passes.") {
		t.Errorf("the error must name the requirement and its text: %v", err)
	}
	if n := count(g.stagesCalled(), "coverage_fill"); n != 2 {
		t.Errorf("fill rounds = %d, want 2 (max_coverage_rounds)", n)
	}
	if g.has("coverage.json") || g.has("approval.json") || len(gate.plans) != 0 {
		t.Error("an uncovered plan reached approval")
	}
	if len(g.rootTests()) != 0 {
		t.Error("root tests were written for an uncovered plan")
	}

	g.wire(greeterStuck)
	if _, err := g.plan(planner.Options{RunID: greeterID}); !errors.As(err, &ce) {
		t.Fatalf("resume err = %v, want a *CoverageError again", err)
	}
	for _, s := range g.stagesCalled() {
		if s != "coverage" && s != "coverage_fill" {
			t.Errorf("resume called %s; only the coverage stage may run", s)
		}
	}
}

func TestCoverageRepliesThatCannotBeUsedAreRetried(t *testing.T) {
	good, err := os.ReadFile(filepath.Join(greeterGap, "coverage_fill.txt"))
	if err != nil {
		t.Fatal(err)
	}
	base, _ := os.ReadFile(filepath.Join(greeter, "coverage.txt"))
	cases := []struct {
		name  string
		dirs  []string
		stage string
	}{
		{"a requirement the brief does not have", []string{variant(t, map[string]string{
			"coverage.txt":   `{"map": [{"requirement": "C9", "nodes": []}], "root_tests": []}`,
			"coverage.2.txt": string(base),
		})}, "coverage"},
		{"a new function in a component that does not exist", []string{variant(t, map[string]string{
			"coverage_fill.txt":   strings.Replace(string(good), `"component": "greeting"`, `"component": "ghost"`, 1),
			"coverage_fill.2.txt": string(good),
		}), greeterGap}, "coverage_fill"},
		{"a new function whose file leaves the repository", []string{variant(t, map[string]string{
			"coverage_fill.txt":   strings.Replace(string(good), `internal/greet/imports.go`, `../imports.go`, 1),
			"coverage_fill.2.txt": string(good),
		}), greeterGap}, "coverage_fill"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving(), c.dirs...)
			g.mustPlan(planner.Options{StopAfter: "coverage"})
			rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: c.stage})
			if len(rows) != 2 || rows[0].Outcome != ledger.OutcomeMalformed || rows[1].Outcome != ledger.OutcomeOK {
				t.Fatalf("%s rows = %+v, want a malformed attempt then a good one", c.stage, rows)
			}
			if cov, err := planner.ReadCoverage(g.runDir); err != nil || len(cov.Covered) != 7 {
				t.Errorf("coverage = %+v, %v", cov, err)
			}
		})
	}
}

func TestCoverageWarningsAreRecordedAndReported(t *testing.T) {
	g := newRig(t, approving())
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		s = strings.Replace(s, "- `internal/greet`: pure functions", "- `cmd/greeter`: the binary.\n- `internal/greet`: pure functions", 1)
		return strings.Replace(s, "- `go test ./...` passes.", "- `go test -race ./...` passes.", 1)
	})
	g.mustPlan(planner.Options{StopAfter: "coverage"})
	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"`cmd/greeter` is named in the brief but no function file is under it",
		"A3: no root test runs the command the bullet begins with (`go test -race ./...`)",
	}
	if strings.Join(cov.Warnings, "\n") != strings.Join(want, "\n") {
		t.Fatalf("warnings = %q\nwant       %q", cov.Warnings, want)
	}
	n := 0
	for _, e := range g.sink.OfKind(events.KindWarning) {
		if e.Stage == "coverage" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d coverage warning events, want 2", n)
	}
}

// A run that died after a fill round extended the contract, but before the
// new function was decomposed, picks the function up when Coverage runs again.
func TestCoverageDecomposesFunctionsLeftWithoutANode(t *testing.T) {
	g := newRig(t, approving(), greeterGap)
	g.mustPlan(planner.Options{StopAfter: "decompose"})

	var doc map[string]any
	if err := json.Unmarshal(g.read("contracts.json"), &doc); err != nil {
		t.Fatal(err)
	}
	doc["functions"] = append(doc["functions"].([]any), map[string]any{
		"id": "fn-imports-are-standard", "package": "greet", "file": "internal/greet/imports.go",
		"signature": "func ImportsAreStandard(paths []string) bool", "doc": "d", "uses": []any{}, "component": "greeting"})
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(g.runDir, "contracts.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	g.wire(variant(t, map[string]string{"decompose.greeting.txt": string(mustRead(t, filepath.Join(greeterGap, "decompose.greeting.2.txt")))}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "coverage"})
	if got := strings.Join(g.stagesCalled(), " "); got != "decompose:greeting coverage" {
		t.Errorf("calls = %q, want the missing node decomposed and then coverage", got)
	}
	if got := len(g.drafts().Components["greeting"]); got != 2 {
		t.Errorf("greeting has %d drafts, want 2", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | head -6`
Expected: the build fails, starting with

```text
# gophermind/gophermind-lib/briefv2/planner_test [gophermind/gophermind-lib/briefv2/planner.test]
briefv2/planner/coverage_test.go:37:22: undefined: planner.ReadCoverage
briefv2/planner/coverage_test.go:115:22: undefined: planner.ReadCoverage
briefv2/planner/coverage_test.go:142:18: undefined: planner.CoverageError
briefv2/planner/coverage_test.go:205:27: undefined: planner.ReadCoverage
briefv2/planner/coverage_test.go:219:22: undefined: planner.ReadCoverage
```

- [ ] **Step 4: Write the two Coverage prompts**

`briefv2/planner/prompts/coverage.md`:

```markdown
You are the coverage auditor for GopherMind. A plan was decomposed from a product brief. Show, requirement by requirement, which part of the plan satisfies it, so that nothing the brief asks for is silently dropped.

Requirements (id, kind, line in the brief, text):
<requirements>
{{.Requirements}}
</requirements>

Plan nodes (id | kind | parent | title | file):
<nodes>
{{.Nodes}}
</nodes>

Rules:
- Every requirement id appears exactly once in "map".
- "nodes" lists the ids of the function or component nodes whose code satisfies the requirement. Use only ids from the node list. The root node does not count. If no node satisfies the requirement, give an empty array. Never guess.
- Every acceptance requirement (ids starting with A) needs an entry in "root_tests": a shell command that exits 0 only when the requirement holds. When the requirement begins with a command in backticks, use that command.
- A constraint (ids starting with C) that is checked by a command rather than built by one function, for example formatting or vet, gets a root test instead of nodes.
- Commands run from the repository root with the brief's declared environment and secrets, without interactive input.

Respond with one JSON object and nothing else:
{"map": [{"requirement": "C1", "nodes": ["fn-..."]}], "root_tests": [{"requirement": "A1", "name": "...", "given": "...", "expect": "...", "command": "..."}]}
```

`briefv2/planner/prompts/coverage_fill.md`:

```markdown
You are the coverage auditor for GopherMind. The plan below leaves some requirements of the brief uncovered. Close each gap.

Uncovered requirements, each with the reason it counts as uncovered:
<gaps>
{{.Gaps}}
</gaps>

Plan nodes (id | kind | parent | title | file):
<nodes>
{{.Nodes}}
</nodes>

Components (id, package):
<components>
{{.Components}}
</components>

For each gap do one of these:
- Map it to function or component nodes from the list that already satisfy it and were overlooked.
- Add a root test: a shell command, run from the repository root, that exits 0 only when the requirement holds. An acceptance requirement (ids starting with A) always needs one.
- Declare the functions, and any types, that are missing from the plan. Give each function the `component` it belongs to (an id from the list), and map the requirement to the new function ids.

Use only requirement ids from the gap list. Do not repeat a function that is already in the node list. There is no limit on how many functions you may declare.

Respond with one JSON object and nothing else:
{"map": [{"requirement": "C1", "nodes": ["fn-..."]}], "root_tests": [{"requirement": "A1", "name": "...", "given": "...", "expect": "...", "command": "..."}], "types": [<type>], "functions": [<function>]}

<type> and <function> match these schemas:
<schema>
{{.ItemSchemas}}
</schema>
```

- [ ] **Step 5: Implement `coverage.go`**

```go
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
)

// CoverageFile is coverage.json: what covers every requirement of the brief.
// It exists only when nothing is uncovered.
type CoverageFile struct {
	Rounds    int        `json:"rounds"` // fill rounds it took
	Covered   []Covered  `json:"covered"`
	RootTests []RootTest `json:"root_tests"`
	Warnings  []string   `json:"warnings"`
}

// ReadCoverage reads a run folder's coverage.json.
func ReadCoverage(runDir string) (CoverageFile, error) {
	var f CoverageFile
	r := &run{dir: runDir}
	found, err := readJSON(r.path(fileCoverage), &f)
	if err != nil {
		return f, err
	}
	if !found {
		return f, fmt.Errorf("planner: %s has no coverage.json; the coverage stage has not finished", runDir)
	}
	return f, nil
}

// CoverageError stops a run whose plan leaves requirements of the brief
// uncovered after every fill round. Nothing is approved in that state.
type CoverageError struct{ Gaps []Gap }

func (e *CoverageError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d requirement(s) of the brief are not covered by the plan:", len(e.Gaps))
	for _, g := range e.Gaps {
		fmt.Fprintf(&b, "\n  %s", gapLine(g))
	}
	return b.String()
}

func gapLine(g Gap) string {
	return fmt.Sprintf("%s (line %d): %s [%s]", g.Requirement, g.Line, oneLine(g.Text, 160), g.Reason)
}

// oneLine puts text on one line and cuts it to n characters.
func oneLine(text string, n int) string {
	return cut(strings.Join(strings.Fields(text), " "), n)
}

func coverageDone(r *run) bool { return exists(r.path(fileCoverage)) }

// coverage is the Coverage stage: the model proposes what covers each
// requirement, code checks it, gaps go back for up to max_coverage_rounds
// fill rounds, and a plan with a gap left never reaches approval.
func (p *Planner) coverage(ctx context.Context, r *run) error {
	// A fill round that died after it extended the contract left functions
	// without nodes; write those first.
	if err := p.refreshPlan(ctx, r); err != nil {
		return err
	}
	nodes, err := PlanNodes(r.dir)
	if err != nil {
		return err
	}
	prompt, err := render("coverage", map[string]string{"Requirements": requirementsText(r.reqs), "Nodes": nodesText(nodes)})
	if err != nil {
		return err
	}
	var reply CoverageReply
	cs := callSpec{stage: "coverage", taskType: "coverage", scope: router.ScopeBrief, maxTokens: maxTokensCoverage}
	if err := p.call(ctx, r, cs, prompt, func(text string) error {
		cr, err := ParseCoverageReply(StripReply(text), r.reqs)
		if err != nil {
			return err
		}
		reply = cr
		return nil
	}); err != nil {
		return err
	}

	covered, gaps := CheckCoverage(r.reqs, nodes, reply)
	rounds := 0
	for len(gaps) > 0 && rounds < p.d.Settings.Defaults.MaxCoverageRounds {
		rounds++
		for _, g := range gaps {
			p.emit(events.KindCoverageGap, "coverage", "", fmt.Sprintf("round %d: %s", rounds, gapLine(g)))
		}
		fill, err := p.coverageFill(ctx, r, gaps, nodes)
		if err != nil {
			return err
		}
		reply = reply.Merge(fill)
		if nodes, err = PlanNodes(r.dir); err != nil {
			return err
		}
		covered, gaps = CheckCoverage(r.reqs, nodes, reply)
	}
	if len(gaps) > 0 {
		return &CoverageError{Gaps: gaps}
	}

	warnings := PathWarnings(r.src, nodes)
	warnings = append(warnings, StrayCommandWarnings(r.src, nodes)...)
	warnings = append(warnings, CommandWarnings(r.reqs, reply)...)
	for _, w := range warnings {
		p.emit(events.KindWarning, "coverage", "", w)
	}
	if reply.RootTests == nil {
		reply.RootTests = []RootTest{}
	}
	// The root node gets its acceptance tests before coverage.json, the file
	// that marks this stage done, is written.
	if err := p.rewriteSkeleton(r, reply.RootTests); err != nil {
		return err
	}
	return writeJSON(r.path(fileCoverage), CoverageFile{Rounds: rounds, Covered: covered, RootTests: reply.RootTests, Warnings: warnings})
}

// coverageFill asks the model to close the gaps. Its reply may map gaps to
// nodes, add root tests, and declare contract functions and types that are
// missing; declarations are appended to contracts.json and decomposed before
// it returns.
func (p *Planner) coverageFill(ctx context.Context, r *run, gaps []Gap, nodes []PlanNode) (CoverageReply, error) {
	var doc map[string]any
	if _, err := readJSON(r.path(fileContracts), &doc); err != nil {
		return CoverageReply{}, err
	}
	var gapText, compText strings.Builder
	for _, g := range gaps {
		fmt.Fprintf(&gapText, "%s (line %d): %s\n  reason: %s\n", g.Requirement, g.Line, oneLine(g.Text, 600), g.Reason)
	}
	for _, c := range objects(doc["components"]) {
		fmt.Fprintf(&compText, "%v (package %v)\n", c["id"], c["package"])
	}
	itemSchemas, err := contractItemSchemas("types", "functions")
	if err != nil {
		return CoverageReply{}, err
	}
	prompt, err := render("coverage_fill", map[string]string{
		"Gaps": strings.TrimRight(gapText.String(), "\n"), "Nodes": nodesText(nodes),
		"Components": strings.TrimRight(compText.String(), "\n"), "ItemSchemas": itemSchemas})
	if err != nil {
		return CoverageReply{}, err
	}

	var fill CoverageReply
	var next map[string]any // the contract with the reply's declarations added, or nil
	cs := callSpec{stage: "coverage_fill", taskType: "coverage", scope: router.ScopeBrief, maxTokens: maxTokensFill}
	if err := p.call(ctx, r, cs, prompt, func(text string) error {
		raw := StripReply(text)
		cr, err := ParseCoverageReply(raw, r.reqs)
		if err != nil {
			return err
		}
		var add struct {
			Types     []map[string]any `json:"types"`
			Functions []map[string]any `json:"functions"`
		}
		if err := json.Unmarshal([]byte(raw), &add); err != nil {
			return fmt.Errorf("coverage fill reply: %w", err)
		}
		next = nil
		if len(add.Types)+len(add.Functions) > 0 {
			cp, err := copyDoc(doc)
			if err != nil {
				return err
			}
			types, _ := cp["types"].([]any)
			for _, t := range add.Types {
				types = append(types, t)
			}
			fns, _ := cp["functions"].([]any)
			for _, f := range add.Functions {
				fns = append(fns, f)
			}
			rev, _ := cp["revision"].(float64)
			cp["types"], cp["functions"], cp["revision"] = types, fns, int(rev)+1
			if _, err := validateContractDoc(cp, r.id); err != nil {
				return fmt.Errorf("coverage fill reply: %w", err)
			}
			next = cp
		}
		fill = cr
		return nil
	}); err != nil {
		return CoverageReply{}, err
	}
	if next != nil {
		if err := writeJSON(r.path(fileContracts), next); err != nil {
			return CoverageReply{}, err
		}
		if err := p.refreshPlan(ctx, r); err != nil {
			return CoverageReply{}, err
		}
	}
	return fill, nil
}

// refreshPlan brings the drafts and the skeleton in line with contracts.json:
// it decomposes every function that has no draft and rewrites the root and
// component nodes. With nothing missing it makes no model call.
func (p *Planner) refreshPlan(ctx context.Context, r *run) error {
	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	before := 0
	for _, d := range dec.Components {
		before += len(d)
	}
	if before == len(c.Functions) {
		return nil
	}
	if err := p.decomposeMissing(ctx, r, c, &dec); err != nil {
		return err
	}
	if _, err := planWaves(r.id, c, dec); err != nil {
		return err
	}
	return writeSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, nil)
}

// rewriteSkeleton writes the root and component nodes again, the root with
// the given acceptance tests.
func (p *Planner) rewriteSkeleton(r *run, rootTests []RootTest) error {
	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	return writeSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, rootTests)
}

// requirementsText lists the requirements for the coverage prompt, one per
// line. A feature is shown by its heading and the start of its text.
func requirementsText(reqs []Requirement) string {
	var b strings.Builder
	for _, q := range reqs {
		text := oneLine(q.Text, 2000)
		if q.Kind == ReqFeature {
			text = q.Name + ": " + oneLine(q.Text, 300)
		}
		fmt.Fprintf(&b, "%s (%s, line %d): %s\n", q.ID, q.Kind, q.Line, text)
	}
	return strings.TrimRight(b.String(), "\n")
}

func nodesText(nodes []PlanNode) string {
	var b strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s\n", n.ID, n.Kind, n.Parent, n.Title, n.File)
	}
	return strings.TrimRight(b.String(), "\n")
}
```

In `briefv2/planner/planner.go` add the stage after the decompose line:

```go
	{"coverage", (*Planner).coverage, coverageDone},
```

- [ ] **Step 6: Run and commit**

Run: `cd gophermind-lib && gofmt -l briefv2/planner && go vet ./briefv2/planner/ && go test ./briefv2/planner/ -race 2>&1 | tail -3`
Expected: `gofmt` prints nothing; `ok  gophermind/gophermind-lib/briefv2/planner`.

```bash
git add gophermind-lib/briefv2/planner
git commit -m "feat(briefv2): Coverage stage, a plan cannot drop parts of the brief" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Approve, Test-writer, the finished tree, and the end-to-end tests

**Files:**
- Create: `gophermind-lib/briefv2/planner/approve.go`, `testwriter.go`, `gophermind-lib/briefv2/planner/prompts/testwriter.md`
- Create: `gophermind-lib/briefv2/planner/testdata/greeter/testwrite.fn-greet.txt`, `testwrite.fn-farewell.txt`, `testwrite.fn-name-error-error.txt`, `testdata/greeter-gap/testwrite.fn-imports-are-standard.txt`, `testdata/greeter-badtest/testwrite.fn-greet.txt`, `testwrite.fn-greet.2.txt`
- Modify: `gophermind-lib/briefv2/planner/planner.go` (two lines: the stages)
- Test: `gophermind-lib/briefv2/planner/testwriter_internal_test.go`, `e2e_test.go`

**Interfaces:**
- Consumes: `ReadCoverage`, `CoverageFile`, `oneLine` (Task 10); `buildSkeleton`, `planWaves`, `loadContracts`, `loadDecomposed`, `loadClasses`, `decomposed`, `cut` (Task 9); `callAsking`, `callSpec`, `render`, `loadAnswers`, `readJSON`, `writeJSON`, `exists`, `copyDoc`, `mustJSON`, `objects`, `strList`, `stateTestwriter`, `stateTestFiles`, `fileApproval` (Task 8); `human.PlanSummary`, `human.Decision`, `human.NewProgrammatic`, `human.KindAsk`, `human.KindApprove` (Task 6); `router.ScopeNode`; `tree.ParseNode`, `tree.NewTree`, `tree.NewStore(dir)` with `WriteAll` and `Load`, `(*tree.Tree).CheckStructure`, `CheckWaves`, `ComputeWaves`; `blackboard.Blackboard.InitRun(ctx, runID string, nodeIDs []string, waves map[string]int) error`, `blackboard.Filter`, `blackboard.StatusPending` (Task 1); `ledger.ModelSummary{TaskType, NodeClass}` (Task 2).
- Produces: `planner.RenderPlan(runDir string) (markdown, hash string, err error)`.
- Produces: `approval.json` as `{"approved_at", "approved_by", "plan_hash"}`; `_state/testwriter.json`; `_state/test_files.json` (the repo-relative test files the executor must never let an implementer edit); `planned_at` in `_state/status.json`.

What this task builds. Approve renders the plan from the run folder alone, hashes it, and records the decision. The summary holds everything in `coverage.json`, so editing the mapping after approval changes the hash. Test-writer refuses to start unless `approval.json` holds the hash of the plan as it stands now; it is the first stage that writes into the target repository.

For each function node, by wave and then id, one `testwrite:<node>` call (scope `node`, carrying the node's class for the ledger). Three things are derived by code and never taken from a model: the test function name (`fn-validate-email` gives `TestValidateEmail`), the test file path (beside the function's file, named after the node), and the command of every test (`go test ./<dir> -run ^<TestFunc>$`). The reply is refused as malformed unless it has at least `len(contract.errors) + 1` tests and a Go file that parses, holds the expected test function, and imports only the standard library or the module's own packages. A path that would leave the repository, even through a symbolic link, is never written, and a file that is already there is never overwritten.

When the last node has its tests, the whole tree is written through `tree.Store.WriteAll`, loaded back and checked, its waves are compared with the ones the approval summary showed, and the blackboard gets one pending row per node.

- [ ] **Step 1: Write the failing tests**

The canned test-writer replies. Each is one JSON object whose `test_file` is Go source. The first test of `testwrite.fn-greet.txt` carries `"command": "rm -rf /"` and `"level": "acceptance"` on purpose: the end-to-end test proves both are replaced by the harness.

`briefv2/planner/testdata/greeter/testwrite.fn-greet.txt`:

```text
{
  "tests": [
    {
      "name": "greets a name",
      "given": "\"Ada\"",
      "expect": "Hello, Ada! and a nil error",
      "level": "acceptance",
      "command": "rm -rf /"
    },
    {
      "name": "trims the name",
      "given": "\"  Ada  \"",
      "expect": "Hello, Ada! and a nil error"
    },
    {
      "name": "refuses an empty name",
      "given": "\"   \"",
      "expect": "an empty string and a *NameError"
    }
  ],
  "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tcases := []struct {\n\t\tname    string\n\t\tin      string\n\t\twant    string\n\t\twantErr bool\n\t}{\n\t\t{\"greets a name\", \"Ada\", \"Hello, Ada!\", false},\n\t\t{\"trims the name\", \"  Ada  \", \"Hello, Ada!\", false},\n\t\t{\"refuses an empty name\", \"   \", \"\", true},\n\t}\n\tfor _, c := range cases {\n\t\tt.Run(c.name, func(t *testing.T) {\n\t\t\tgot, err := Greet(c.in)\n\t\t\tif (err != nil) != c.wantErr {\n\t\t\t\tt.Fatalf(\"Greet(%q) error = %v, wantErr %v\", c.in, err, c.wantErr)\n\t\t\t}\n\t\t\tif got != c.want {\n\t\t\t\tt.Errorf(\"Greet(%q) = %q, want %q\", c.in, got, c.want)\n\t\t\t}\n\t\t})\n\t}\n}\n"
}
```

`briefv2/planner/testdata/greeter/testwrite.fn-farewell.txt`:

````text
```json
{
  "tests": [
    {
      "name": "says goodbye to a name",
      "given": "\"Ada\"",
      "expect": "Goodbye, Ada! and a nil error"
    },
    {
      "name": "trims the name",
      "given": "\"  Ada  \"",
      "expect": "Goodbye, Ada! and a nil error"
    },
    {
      "name": "refuses an empty name",
      "given": "\"   \"",
      "expect": "an empty string and a *NameError"
    }
  ],
  "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestFarewell(t *testing.T) {\n\tcases := []struct {\n\t\tname    string\n\t\tin      string\n\t\twant    string\n\t\twantErr bool\n\t}{\n\t\t{\"says goodbye to a name\", \"Ada\", \"Goodbye, Ada!\", false},\n\t\t{\"trims the name\", \"  Ada  \", \"Goodbye, Ada!\", false},\n\t\t{\"refuses an empty name\", \"   \", \"\", true},\n\t}\n\tfor _, c := range cases {\n\t\tt.Run(c.name, func(t *testing.T) {\n\t\t\tgot, err := Farewell(c.in)\n\t\t\tif (err != nil) != c.wantErr {\n\t\t\t\tt.Fatalf(\"Farewell(%q) error = %v, wantErr %v\", c.in, err, c.wantErr)\n\t\t\t}\n\t\t\tif got != c.want {\n\t\t\t\tt.Errorf(\"Farewell(%q) = %q, want %q\", c.in, got, c.want)\n\t\t\t}\n\t\t})\n\t}\n}\n"
}
```
````

`briefv2/planner/testdata/greeter/testwrite.fn-name-error-error.txt`:

```text
{
  "tests": [
    {
      "name": "returns the reason",
      "given": "Reason \"name is empty\"",
      "expect": "name is empty"
    },
    {
      "name": "returns an empty reason unchanged",
      "given": "Reason \"\"",
      "expect": "an empty string"
    }
  ],
  "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestNameErrorError(t *testing.T) {\n\tcases := []struct {\n\t\tname   string\n\t\treason string\n\t}{\n\t\t{\"returns the reason\", \"name is empty\"},\n\t\t{\"returns an empty reason unchanged\", \"\"},\n\t}\n\tfor _, c := range cases {\n\t\tt.Run(c.name, func(t *testing.T) {\n\t\t\te := &NameError{Reason: c.reason}\n\t\t\tif got := e.Error(); got != c.reason {\n\t\t\t\tt.Errorf(\"Error() = %q, want %q\", got, c.reason)\n\t\t\t}\n\t\t})\n\t}\n}\n"
}
```

`briefv2/planner/testdata/greeter-gap/testwrite.fn-imports-are-standard.txt`:

```text
{
  "tests": [
    {
      "name": "accepts standard packages",
      "given": "fmt and net/http",
      "expect": "true"
    },
    {
      "name": "accepts no imports",
      "given": "nil",
      "expect": "true"
    },
    {
      "name": "refuses a module path",
      "given": "fmt and github.com/x/y",
      "expect": "false"
    }
  ],
  "test_file": "package greet\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\nfunc TestImportsAreStandard(t *testing.T) {\n\tcases := []struct {\n\t\tname  string\n\t\tpaths []string\n\t\twant  bool\n\t}{\n\t\t{\"accepts standard packages\", []string{\"fmt\", \"net/http\"}, true},\n\t\t{\"accepts no imports\", nil, true},\n\t\t{\"refuses a module path\", []string{\"fmt\", \"github.com/x/y\"}, false},\n\t}\n\tfor _, c := range cases {\n\t\tt.Run(c.name, func(t *testing.T) {\n\t\t\tif got := ImportsAreStandard(c.paths); got != c.want {\n\t\t\t\tt.Errorf(\"ImportsAreStandard(%s) = %v, want %v\", strings.Join(c.paths, \",\"), got, c.want)\n\t\t\t}\n\t\t})\n\t}\n}\n"
}
```

`greeter-badtest` has a first reply whose test file imports a third-party module, and a clean second one:

`briefv2/planner/testdata/greeter-badtest/testwrite.fn-greet.txt`:

```text
{
  "tests": [
    {
      "name": "greets a name",
      "given": "\"Ada\"",
      "expect": "Hello, Ada! and a nil error",
      "level": "acceptance",
      "command": "rm -rf /"
    },
    {
      "name": "trims the name",
      "given": "\"  Ada  \"",
      "expect": "Hello, Ada! and a nil error"
    },
    {
      "name": "refuses an empty name",
      "given": "\"   \"",
      "expect": "an empty string and a *NameError"
    }
  ],
  "test_file": "package greet\n\nimport (\n\t\"testing\"\n\n\t\"github.com/stretchr/testify/assert\"\n)\n\nfunc TestGreet(t *testing.T) {\n\tcases := []struct {\n\t\tname    string\n\t\tin      string\n\t\twant    string\n\t\twantErr bool\n\t}{\n\t\t{\"greets a name\", \"Ada\", \"Hello, Ada!\", false},\n\t\t{\"trims the name\", \"  Ada  \", \"Hello, Ada!\", false},\n\t\t{\"refuses an empty name\", \"   \", \"\", true},\n\t}\n\tfor _, c := range cases {\n\t\tt.Run(c.name, func(t *testing.T) {\n\t\t\tgot, err := Greet(c.in)\n\t\t\tif (err != nil) != c.wantErr {\n\t\t\t\tt.Fatalf(\"Greet(%q) error = %v, wantErr %v\", c.in, err, c.wantErr)\n\t\t\t}\n\t\t\tassert.Equal(t, c.want, got)\n\t\t})\n\t}\n}\n"
}
```

`briefv2/planner/testdata/greeter-badtest/testwrite.fn-greet.2.txt`:

```text
{
  "tests": [
    {
      "name": "greets a name",
      "given": "\"Ada\"",
      "expect": "Hello, Ada! and a nil error",
      "level": "acceptance",
      "command": "rm -rf /"
    },
    {
      "name": "trims the name",
      "given": "\"  Ada  \"",
      "expect": "Hello, Ada! and a nil error"
    },
    {
      "name": "refuses an empty name",
      "given": "\"   \"",
      "expect": "an empty string and a *NameError"
    }
  ],
  "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tcases := []struct {\n\t\tname    string\n\t\tin      string\n\t\twant    string\n\t\twantErr bool\n\t}{\n\t\t{\"greets a name\", \"Ada\", \"Hello, Ada!\", false},\n\t\t{\"trims the name\", \"  Ada  \", \"Hello, Ada!\", false},\n\t\t{\"refuses an empty name\", \"   \", \"\", true},\n\t}\n\tfor _, c := range cases {\n\t\tt.Run(c.name, func(t *testing.T) {\n\t\t\tgot, err := Greet(c.in)\n\t\t\tif (err != nil) != c.wantErr {\n\t\t\t\tt.Fatalf(\"Greet(%q) error = %v, wantErr %v\", c.in, err, c.wantErr)\n\t\t\t}\n\t\t\tif got != c.want {\n\t\t\t\tt.Errorf(\"Greet(%q) = %q, want %q\", c.in, got, c.want)\n\t\t\t}\n\t\t})\n\t}\n}\n"
}
```

`briefv2/planner/testwriter_internal_test.go`:

```go
package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDerivedTestNames(t *testing.T) {
	cases := []struct{ id, file, wantFunc, wantPath, wantCmd string }{
		{"fn-validate-email", "internal/validation/email.go", "TestValidateEmail", "internal/validation/fn_validate_email_test.go", "go test ./internal/validation -run ^TestValidateEmail$"},
		{"fn-name-error-error", "internal/greet/errors.go", "TestNameErrorError", "internal/greet/fn_name_error_error_test.go", "go test ./internal/greet -run ^TestNameErrorError$"},
		{"fn-main", "main.go", "TestMain", "fn_main_test.go", "go test . -run ^TestMain$"},
		{"fn-2fa-check", "internal/auth/twofa.go", "Test2faCheck", "internal/auth/fn_2fa_check_test.go", "go test ./internal/auth -run ^Test2faCheck$"},
	}
	for _, c := range cases {
		if got := testFuncName(c.id); got != c.wantFunc {
			t.Errorf("testFuncName(%s) = %s, want %s", c.id, got, c.wantFunc)
		}
		if got := testFilePath(c.id, c.file); got != c.wantPath {
			t.Errorf("testFilePath(%s) = %s, want %s", c.id, got, c.wantPath)
		}
		if got := testCommand(c.file, c.wantFunc); got != c.wantCmd {
			t.Errorf("testCommand(%s) = %q, want %q", c.id, got, c.wantCmd)
		}
	}
}

func TestSafeTestPath(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "internal", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "internal", "escape")); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}

	good := []string{"a_test.go", "internal/real/x_test.go", "internal/not/yet/made/x_test.go"}
	for _, rel := range good {
		abs, err := safeTestPath(repo, rel)
		if err != nil || abs != filepath.Join(repo, filepath.FromSlash(rel)) {
			t.Errorf("safeTestPath(%q) = %q, %v", rel, abs, err)
		}
	}
	if exists(filepath.Join(repo, "internal", "not")) {
		t.Error("safeTestPath created a directory; it must only answer")
	}
	bad := map[string]string{
		"../x_test.go":                     "not a clean path",
		"internal/../../x_test.go":         "not a clean path",
		"/etc/x_test.go":                   "not a clean path",
		`internal\x_test.go`:               "not a clean path",
		"":                                 "not a clean path",
		"internal/real/x.go":               "does not end in _test.go",
		"internal/escape/x_test.go":        "resolves outside the repository",
		"internal/escape/deeper/x_test.go": "resolves outside the repository",
	}
	for rel, want := range bad {
		if _, err := safeTestPath(repo, rel); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("safeTestPath(%q) = %v, want an error containing %q", rel, err, want)
		}
	}
}

const okTestSource = `package greet

import (
	"strings"
	"testing"

	"example.com/greeter/internal/names"
)

func TestGreet(t *testing.T) {
	_ = strings.TrimSpace(names.Default)
}
`

func TestCheckTestSource(t *testing.T) {
	if err := checkTestSource(okTestSource, "greet", "TestGreet", "example.com/greeter"); err != nil {
		t.Fatalf("the good file was refused: %v", err)
	}
	external := strings.Replace(okTestSource, "package greet\n", "package greet_test\n", 1)
	if err := checkTestSource(external, "greet", "TestGreet", "example.com/greeter"); err != nil {
		t.Errorf("an external test package was refused: %v", err)
	}
	bad := []struct{ name, edit, with, want string }{
		{"not Go", "package greet", "pakage greet", "not valid Go"},
		{"another package", "package greet\n", "package other\n", "is in package other"},
		{"a third-party import", `"strings"`, `"github.com/stretchr/testify/assert"`, `imports "github.com/stretchr/testify/assert"`},
		{"another module that shares the prefix", `example.com/greeter/internal/names`, `example.com/greeter2/internal/names`, "only the standard library and this module"},
		{"an extended library import", `"strings"`, `"golang.org/x/text/cases"`, `imports "golang.org/x/text/cases"`},
		{"the test function is missing", "func TestGreet(", "func TestOther(", "has no func TestGreet(t *testing.T)"},
		{"the test function has the wrong shape", "func TestGreet(t *testing.T)", "func TestGreet(t *testing.B)", "not as func TestGreet(t *testing.T)"},
		{"the name is only a method", "func TestGreet(t *testing.T)", "func (x X) TestGreet(t *testing.T)", "has no func TestGreet"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			src := strings.Replace(okTestSource, b.edit, b.with, 1)
			if err := checkTestSource(src, "greet", "TestGreet", "example.com/greeter"); err == nil || !strings.Contains(err.Error(), b.want) {
				t.Errorf("err = %v, want it to contain %q", err, b.want)
			}
		})
	}
}

// At least the happy path and one test per error condition: fewer tests than
// errors + 1 is refused.
func TestTestwriterRepliesNeedATestPerErrorCondition(t *testing.T) {
	ct := map[string]any{"file": "internal/greet/greet.go", "errors": []any{
		map[string]any{"when": "name is empty", "returns": "*NameError"},
		map[string]any{"when": "name is too long", "returns": "*NameError"},
	}}
	src := strings.ReplaceAll(okTestSource, "\n\t\"example.com/greeter/internal/names\"\n", "")
	src = strings.Replace(src, "names.Default", `"x"`, 1)
	test := func(name string) string {
		return `{"name": "` + name + `", "given": "g", "expect": "e", "level": "acceptance", "command": "rm -rf /"}`
	}
	reply := func(tests ...string) string {
		quoted, _ := jsonString(src)
		return `{"tests": [` + strings.Join(tests, ",") + `], "test_file": ` + quoted + `}`
	}

	_, _, err := parseTestwrite(reply(test("a"), test("b")), ct, "greet", "TestGreet", "example.com/greeter")
	if err == nil || !strings.Contains(err.Error(), "has 2 tests; the contract lists 2 error conditions, so at least 3 are needed") {
		t.Fatalf("two tests for two error conditions: err = %v", err)
	}
	tests, got, err := parseTestwrite(reply(test("a"), test("b"), test("c")), ct, "greet", "TestGreet", "example.com/greeter")
	if err != nil {
		t.Fatalf("three tests were refused: %v", err)
	}
	if got != src || len(tests) != 3 {
		t.Errorf("parseTestwrite returned %d tests", len(tests))
	}
	// Level and command are the harness's, whatever the model wrote.
	for _, tc := range tests {
		if tc["level"] != "unit" || tc["command"] != "go test ./internal/greet -run ^TestGreet$" {
			t.Errorf("test %v = level %v, command %v", tc["name"], tc["level"], tc["command"])
		}
	}
	for name, bad := range map[string]string{
		"no name":   `{"name": " ", "given": "g", "expect": "e"}`,
		"no given":  `{"name": "a", "expect": "e"}`,
		"no expect": `{"name": "a", "given": "g", "expect": ""}`,
	} {
		if _, _, err := parseTestwrite(reply(test("a"), test("b"), bad), ct, "greet", "TestGreet", "example.com/greeter"); err == nil || !strings.Contains(err.Error(), "needs a name, a given and an expect") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, _, err := parseTestwrite("not json", ct, "greet", "TestGreet", "example.com/greeter"); err == nil {
		t.Error("a reply that is not JSON was accepted")
	}
}

func jsonString(s string) (string, error) {
	raw, err := json.Marshal(s)
	return string(raw), err
}
```

`briefv2/planner/e2e_test.go` (the whole pipeline, against a real router, ledger, blackboard and temp repository):

```go
package planner_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
)

const greeterBadTest = "testdata/greeter-badtest"

// repoFiles lists every file in the target repository outside .gophermind/.
func (g *rig) repoFiles() []string {
	g.t.Helper()
	var out []string
	err := filepath.WalkDir(g.repo, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(g.repo, p)
		if d.IsDir() {
			if rel == ".gophermind" {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		g.t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func (g *rig) node(path string) map[string]any {
	g.t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(g.read(path), &doc); err != nil {
		g.t.Fatal(err)
	}
	return doc
}

var greeterTestFiles = []string{
	"internal/greet/fn_farewell_test.go",
	"internal/greet/fn_greet_test.go",
	"internal/greet/fn_name_error_error_test.go",
}

// The whole pipeline against canned replies, answered through the gate the
// run service and the desktop app will use.
func TestPlanEndToEnd(t *testing.T) {
	gate := human.NewProgrammatic()
	g := newRig(t, gate)
	stop := make(chan struct{})
	defer close(stop)
	shown := make(chan human.PlanSummary, 1)
	go func() {
		for {
			select {
			case <-stop:
				return
			case req := <-gate.Requests():
				switch req.Kind {
				case human.KindAsk:
					as := make([]human.Answer, len(req.Questions))
					for i, q := range req.Questions {
						as[i] = human.Answer{ID: q.ID, Text: "Yes, trim it."}
					}
					req.Answer(as)
				case human.KindApprove:
					shown <- req.Plan
					req.Decide(human.Decision{Approved: true, By: "programmatic"})
				}
			}
		}
	}()
	g.mustPlan(planner.Options{})

	// The tree is complete and consistent.
	tr, err := tree.NewStore(g.runDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckStructure(); err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckWaves(); err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 7 {
		t.Fatalf("tree has %d nodes, want 7 (root, three components, three functions)", len(tr.Nodes))
	}
	for id, want := range map[string]int{"fn-name-error-error": 0, "fn-farewell": 0, "fn-greet": 1} {
		if n := tr.Nodes[id]; n.Wave == nil || *n.Wave != want {
			t.Errorf("wave of %s = %v, want %d", id, n.Wave, want)
		}
	}
	for _, path := range []string{"types/fn-name-error-error.json", "greeting/fn-greet.json", "farewell/fn-farewell.json"} {
		doc := g.node(path)
		if _, ok := doc["contract"].(map[string]any); !ok {
			t.Errorf("%s has no contract", path)
		}
		tests, _ := doc["tests"].([]any)
		if len(tests) == 0 {
			t.Fatalf("%s has no tests", path)
		}
		for _, x := range tests {
			tc := x.(map[string]any)
			cmd, _ := tc["command"].(string)
			if !strings.HasPrefix(cmd, "go test ./internal/greet -run ^Test") || tc["level"] != "unit" {
				t.Errorf("%s test %v = level %v, command %q; both are set by the harness", path, tc["name"], tc["level"], cmd)
			}
		}
		if deps := strs(doc["depends_on"]); len(deps) > 0 {
			if sigs := strs(doc["context"].(map[string]any)["dependency_signatures"]); len(sigs) == 0 {
				t.Errorf("%s depends on %v but has no dependency_signatures", path, deps)
			}
		}
	}
	if tests := g.rootTests(); len(tests) != 4 {
		t.Errorf("root has %d acceptance tests, want 4", len(tests))
	}

	// The repository holds the test files and nothing else; the blackboard holds one pending row per node.
	if got := g.repoFiles(); strings.Join(got, " ") != strings.Join(greeterTestFiles, " ") {
		t.Errorf("repo files = %v", got)
	}
	var forbidden []string
	if err := json.Unmarshal(g.read("_state/test_files.json"), &forbidden); err != nil || strings.Join(forbidden, " ") != strings.Join(greeterTestFiles, " ") {
		t.Errorf("test_files.json = %v, %v", forbidden, err)
	}
	rows, err := g.board.List(context.Background(), greeterID, blackboard.Filter{})
	if err != nil || len(rows) != 7 {
		t.Fatalf("blackboard rows = %d, %v; want 7", len(rows), err)
	}
	for _, row := range rows {
		if row.Status != blackboard.StatusPending {
			t.Errorf("row %s is %s, want pending", row.NodeID, row.Status)
		}
	}

	// What was approved is what stands, and it showed the coverage.
	plan := <-shown
	for _, want := range []string{"# Plan: Greeter", "| greeting | 1 | 1 |", "Functions: 3 in 2 wave(s).", "Requirements covered: 7 of 7",
		"| C2 | `gofmt` clean and `go vet` clean. | none | formatted and vetted |", "      go test ./...", "- Secrets (names only): none"} {
		if !strings.Contains(plan.Markdown, want) {
			t.Errorf("the approval summary lacks %q", want)
		}
	}
	var ap struct {
		ApprovedBy string `json:"approved_by"`
		PlanHash   string `json:"plan_hash"`
	}
	if err := json.Unmarshal(g.read("approval.json"), &ap); err != nil || ap.ApprovedBy != "programmatic" || ap.PlanHash != plan.Hash {
		t.Errorf("approval.json = %+v, %v", ap, err)
	}
	if _, hash, err := planner.RenderPlan(g.runDir); err != nil || hash != plan.Hash {
		t.Errorf("the plan no longer hashes to what was approved: %v", err)
	}
	st, err := planner.ReadStatus(greeterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Stages {
		if !s.Done {
			t.Errorf("stage %s is not done", s.Name)
		}
	}

	// A finished run has nothing left to do.
	g.wire()
	g.mustPlan(planner.Options{RunID: greeterID})
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("resuming a finished run made %d model calls", n)
	}
}

// Every ledger row says what kind of work the call was, and a call about one
// leaf says what class of function it is, so models can be compared by both.
func TestLedgerRowsCarryTheTaskTypeAndTheNodeClass(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})
	ctx := context.Background()
	rows, err := g.led.List(ctx, greeterID, ledger.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	byTask := map[string]int{}
	for _, r := range rows {
		byTask[r.TaskType]++
		leaf := r.TaskType == "testwrite"
		if leaf != (r.NodeClass != "") || leaf != (r.NodeID != "") {
			t.Errorf("row %s: task %q, node %q, class %q", r.Stage, r.TaskType, r.NodeID, r.NodeClass)
		}
	}
	want := map[string]int{"clarify": 1, "contract": 4, "decompose": 3, "coverage": 1, "testwrite": 3}
	for task, n := range want {
		if byTask[task] != n {
			t.Errorf("%d rows of task type %s, want %d (all: %v)", byTask[task], task, n, byTask)
		}
	}
	if len(byTask) != len(want) {
		t.Errorf("task types = %v", byTask)
	}
	sum, err := g.led.Summary(ctx, greeterID)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	for _, s := range sum {
		groups[s.TaskType+"/"+s.NodeClass] = s.Calls
	}
	if groups["testwrite/validation"] != 2 || groups["testwrite/pure"] != 1 || groups["contract/"] != 4 {
		t.Errorf("summary groups = %v", groups)
	}
}

// Nothing is written into the target repository before the plan is approved.
func TestNothingTouchesTheRepoWithoutApproval(t *testing.T) {
	gate := &scriptGate{decision: human.Decision{Approved: false, Note: "too big"}}
	g := newRig(t, gate)
	_, err := g.plan(planner.Options{})
	if err == nil || !strings.Contains(err.Error(), "plan not approved: too big") {
		t.Fatalf("err = %v, want the refusal", err)
	}
	if files := g.repoFiles(); len(files) != 0 {
		t.Errorf("files in the repo without approval: %v", files)
	}
	if g.has("approval.json") {
		t.Error("approval.json exists for a refused plan")
	}
	for _, s := range g.stagesCalled() {
		if strings.HasPrefix(s, "testwrite:") {
			t.Errorf("%s was called without approval", s)
		}
	}
	rows, _ := g.board.List(context.Background(), greeterID, blackboard.Filter{})
	if len(rows) != 0 {
		t.Errorf("%d blackboard rows exist for an unapproved plan", len(rows))
	}
}

// Stop after each stage in turn and resume: no stage that had finished makes
// a model call again.
func TestResumeRepeatsOnlyWhatIsUnfinished(t *testing.T) {
	order := []string{"load", "clarify", "contract", "decompose", "coverage", "approve"}
	owner := func(stage string) string {
		switch name, _, _ := strings.Cut(stage, ":"); name {
		case "coverage_fill":
			return "coverage"
		case "testwrite":
			return "testwriter"
		default:
			return name
		}
	}
	for i, stop := range order {
		t.Run("after "+stop, func(t *testing.T) {
			g := newRig(t, approving())
			g.mustPlan(planner.Options{StopAfter: stop})
			finished := map[string]bool{}
			for _, s := range order[:i+1] {
				finished[s] = true
			}
			g.wire()
			g.mustPlan(planner.Options{RunID: greeterID})
			calls := g.stagesCalled()
			for _, s := range calls {
				if finished[owner(s)] {
					t.Errorf("resume after %s called %s again", stop, s)
				}
			}
			if count(calls, "testwrite:fn-greet") != 1 {
				t.Errorf("resume after %s did not finish the run: %v", stop, calls)
			}
			if got := g.repoFiles(); len(got) != 3 {
				t.Errorf("repo files = %v", got)
			}
		})
	}

	t.Run("waiting for approval", func(t *testing.T) {
		gate := approving()
		gate.err = human.ErrWaiting
		g := newRig(t, gate, variant(t, map[string]string{"clarify.txt": "[]"}))
		out, err := g.plan(planner.Options{})
		if err != nil || out != planner.Waiting {
			t.Fatalf("Run = %q, %v; want waiting", out, err)
		}
		if st, _ := planner.ReadStatus(greeterID); st.Waiting != "approve" {
			t.Errorf("status.Waiting = %q, want approve", st.Waiting)
		}
		gate.err = nil
		g.wire()
		g.mustPlan(planner.Options{RunID: greeterID})
		for _, s := range g.stagesCalled() {
			if !strings.HasPrefix(s, "testwrite:") {
				t.Errorf("resume after approval called %s", s)
			}
		}
	})

	t.Run("in the middle of the test writer", func(t *testing.T) {
		g := newRig(t, approving(), variant(t, map[string]string{"testwrite.fn-greet.txt": "nope", "testwrite.fn-greet.2.txt": "nope"}))
		if _, err := g.plan(planner.Options{}); err == nil || !strings.Contains(err.Error(), "node fn-greet") {
			t.Fatalf("err = %v, want the test writer to fail on fn-greet", err)
		}
		g.wire()
		g.mustPlan(planner.Options{RunID: greeterID})
		if got := strings.Join(g.stagesCalled(), " "); got != "testwrite:fn-greet" {
			t.Errorf("resume called %q, want only testwrite:fn-greet", got)
		}
	})
}

// An approval is for one exact plan. Change what it covered and the test
// writer refuses to run.
func TestAChangedPlanInvalidatesTheApproval(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "approve"})
	edited := strings.Replace(string(g.read("coverage.json")), `"greeting"`, `"fn-greet"`, 1)
	if err := os.WriteFile(filepath.Join(g.runDir, "coverage.json"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	g.wire()
	_, err := g.plan(planner.Options{RunID: greeterID})
	if err == nil || !strings.Contains(err.Error(), "approval.json does not match the plan as it stands") {
		t.Fatalf("err = %v, want the stale approval refused", err)
	}
	if files := g.repoFiles(); len(files) != 0 || len(g.fake.Requests()) != 0 {
		t.Errorf("files %v, %d model calls after a stale approval", files, len(g.fake.Requests()))
	}
}

func TestCoverageFillRunsThroughToAFinishedTree(t *testing.T) {
	g := newRig(t, approving(), greeterGap)
	g.mustPlan(planner.Options{})
	tr, err := tree.NewStore(g.runDir).Load()
	if err != nil || len(tr.Nodes) != 8 {
		t.Fatalf("tree = %d nodes, %v; want 8 (the fill added a function)", len(tr.Nodes), err)
	}
	if !contains(g.repoFiles(), "internal/greet/fn_imports_are_standard_test.go") {
		t.Errorf("repo files = %v", g.repoFiles())
	}
	md, _, err := planner.RenderPlan(g.runDir)
	if err != nil || !strings.Contains(md, "Coverage fill rounds: 1") || !strings.Contains(md, "- Contract revision: 1") {
		t.Errorf("plan summary does not show the fill round: %v", err)
	}
}

func contains(list []string, s string) bool { return count(list, s) > 0 }

// A test file that breaks a rule is a malformed reply: the same model gets
// one more try, and only a clean file is ever written.
func TestABadTestFileIsRefusedAndRetried(t *testing.T) {
	good := string(mustRead(t, filepath.Join(greeter, "testwrite.fn-greet.txt")))
	one := `{"tests": [{"name": "greets a name", "given": "Ada", "expect": "Hello, Ada!"}], "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {}\n"}`
	cases := []struct {
		name string
		dir  string
	}{
		{"an import outside the standard library and the module", greeterBadTest},
		{"fewer tests than error conditions plus one", variant(t, map[string]string{"testwrite.fn-greet.txt": one, "testwrite.fn-greet.2.txt": good})},
		{"a question first", variant(t, map[string]string{"testwrite.fn-greet.txt": "QUESTION:\nShould the greeting end with an exclamation mark?", "testwrite.fn-greet.2.txt": good})},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, approving(), c.dir)
			g.mustPlan(planner.Options{})
			rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{Stage: "testwrite:fn-greet"})
			wantFirst := ledger.OutcomeMalformed
			if i == 2 {
				wantFirst = ledger.OutcomeOK // a question is a usable reply, not a broken one
			}
			if len(rows) != 2 || rows[0].Outcome != wantFirst || rows[1].Outcome != ledger.OutcomeOK {
				t.Fatalf("rows for testwrite:fn-greet = %+v", rows)
			}
			src, err := os.ReadFile(filepath.Join(g.repo, "internal/greet/fn_greet_test.go"))
			if err != nil || strings.Contains(string(src), "testify") || !strings.Contains(string(src), "refuses an empty name") {
				t.Errorf("the written test file is not the clean one: %v", err)
			}
		})
	}
}

// A contract file that points out of the repository never produces a write
// outside it, and a file the run did not write is never overwritten.
func TestTheTestWriterOnlyWritesItsOwnFilesInsideTheRepo(t *testing.T) {
	t.Run("a path that leaves the repository", func(t *testing.T) {
		g := newRig(t, approving())
		g.mustPlan(planner.Options{StopAfter: "approve"})
		edited := strings.Replace(string(g.read("_state/decomposed.json")), "internal/greet/greet.go", "../outside/greet.go", 1)
		if err := os.WriteFile(filepath.Join(g.runDir, "_state", "decomposed.json"), []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		g.wire()
		_, err := g.plan(planner.Options{RunID: greeterID})
		if err == nil || !strings.Contains(err.Error(), "node fn-greet") || !strings.Contains(err.Error(), "not a clean path inside the repository") {
			t.Fatalf("err = %v, want fn-greet refused for its path", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(g.repo), "outside")); err == nil {
			t.Error("a directory was created outside the repository")
		}
		if count(g.stagesCalled(), "testwrite:fn-greet") != 0 {
			t.Error("the model was asked for a test file that could never be written")
		}
	})
	t.Run("a file that is already there", func(t *testing.T) {
		g := newRig(t, approving())
		mine := filepath.Join(g.repo, "internal", "greet", "fn_greet_test.go")
		if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mine, []byte("package greet\n// written by a person\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := g.plan(planner.Options{})
		if err == nil || !strings.Contains(err.Error(), "internal/greet/fn_greet_test.go already exists") {
			t.Fatalf("err = %v, want the existing file named", err)
		}
		if got, _ := os.ReadFile(mine); !strings.Contains(string(got), "written by a person") {
			t.Error("the existing file was overwritten")
		}
	})
}

// The privacy rule, end to end: a public provider sits first in the strong
// chain. It may write tests for one function (node scope) and must never see
// a call that carries the brief or a component.
func TestAPublicProviderOnlyEverSeesNodeScopeCalls(t *testing.T) {
	g := newRig(t, approving())
	inner, err := planner.FixtureProvider(greeter)
	if err != nil {
		t.Fatal(err)
	}
	pub := provider.NewFake("pub", []provider.ModelInfo{{ID: "fixture", ContextTokens: 1 << 20}},
		func(_ int, req provider.Request) (provider.Response, error) {
			return inner.Complete(context.Background(), req)
		})
	cfg := planner.FixtureSettings()
	cfg.Providers = append(cfg.Providers, settings.ProviderConfig{Name: "pub", BaseURL: "http://public.invalid/v1",
		Visibility: settings.Public, MaxConcurrent: 1, Models: []settings.ModelEntry{{ID: "fixture", ContextTokens: 1 << 20}}})
	cfg.Models["strong"] = []string{"pub/fixture", "fake/fixture"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	g.router = router.New(cfg, map[string]provider.Provider{"fake": g.fake, "pub": pub}, g.led, g.sink)
	g.deps.Caller, g.deps.Settings = g.router, cfg
	g.mustPlan(planner.Options{})

	if n := len(pub.Requests()); n != 3 {
		t.Errorf("the public provider got %d requests, want the 3 test-writer calls", n)
	}
	for _, r := range pub.Requests() {
		if s := planner.StageOf(r); !strings.HasPrefix(s, "testwrite:") {
			t.Errorf("the public provider was sent a %s call", s)
		}
		if strings.Contains(r.Messages[1].Content, "## Overview") {
			t.Error("the public provider was sent the brief")
		}
	}
	for _, s := range g.stagesCalled() {
		if strings.HasPrefix(s, "testwrite:") {
			t.Errorf("the private provider answered %s although the public one comes first for node scope", s)
		}
	}
}

// A secret's value is in the vault and nowhere else: not in the run folder,
// the repository, the run registry, or the database.
func TestNoSecretValueLeavesTheVault(t *testing.T) {
	g := newRig(t, approving())
	g.briefPath = writeBrief(t, g.repo, withSecret)
	store := &memSecrets{vals: map[string]string{vault.HarnessScope + "/GREETER_API_KEY": canary}}
	g.deps.OpenSecrets = func() (planner.Secrets, error) { return store, nil }
	g.mustPlan(planner.Options{})

	root := g.node("root.json")
	if got := strs(root["secrets"]); len(got) != 1 || got[0] != "GREETER_API_KEY" {
		t.Errorf("root secrets = %v, want the name only", got)
	}
	g.db.Close()
	dirs := []string{g.repo, os.Getenv("GOPHERMIND_CONFIG_DIR"), filepath.Dir(g.dbPath)}
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if raw, _ := os.ReadFile(p); strings.Contains(string(raw), canary) {
					t.Errorf("%s contains the secret value", p)
				}
			}
			return nil
		})
	}
	for _, r := range g.fake.Requests() {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, canary) {
				t.Errorf("a %s prompt contains the secret value", planner.StageOf(r))
			}
		}
	}
}
```

Run: `cd gophermind-lib && go test ./briefv2/planner/ 2>&1 | head -6`
Expected: the build fails, starting with

```text
# gophermind/gophermind-lib/briefv2/planner [gophermind/gophermind-lib/briefv2/planner.test]
briefv2/planner/testwriter_internal_test.go:19:13: undefined: testFuncName
briefv2/planner/testwriter_internal_test.go:22:13: undefined: testFilePath
briefv2/planner/testwriter_internal_test.go:25:13: undefined: testCommand
briefv2/planner/testwriter_internal_test.go:46:15: undefined: safeTestPath
briefv2/planner/testwriter_internal_test.go:65:16: undefined: safeTestPath
```

- [ ] **Step 2: Write the Test-writer prompt** (`briefv2/planner/prompts/testwriter.md`)

This is the handoff's `04-testwriter.md` with three changes: the model no longer writes `level` or `command` (the harness does), the test-count rule is stated, and the `QUESTION:` sentence is added.

```markdown
You are the test writer for GopherMind. You write the tests for one function from its contract alone. You will never see the implementation, and the implementer will never be allowed to edit your test file.

Write:
1. A `tests` array for the node: one entry per case with `name`, `given`, and `expect`. The harness sets `level` and `command` itself (`go test ./{{.PackageDir}} -run ^{{.TestFuncName}}$`).
2. The Go test file `{{.TestFile}}` containing `func {{.TestFuncName}}(t *testing.T)` as a table-driven test whose case names match the `name` fields exactly (spaces become underscores in subtest names).

Cover:
- the happy path,
- every entry in `contract.errors` (so the array has at least one more entry than `contract.errors`),
- every constraint on every input (empty, whitespace, boundary lengths, nil),
- boundaries on both sides (the last valid value and the first invalid value).

Do not:
- test private helpers or implementation details,
- import any package outside the standard library and this module,
- reference any function or type not present in `dependency_signatures` or the contract,
- depend on ordering, timing, randomness, or the network. Fake `*http.Client` transports and `Store` implementations inline in the test file when needed.

If something you need is not stated and cannot be settled by choosing the most conservative option, reply with a line containing only `QUESTION:` followed by your question on the next lines, and nothing else.

Node:
<node>
{{.Node}}
</node>

Respond with one JSON object and nothing else:
{"tests": [...], "test_file": "<full Go source of the test file>"}
```

- [ ] **Step 3: Implement `approve.go`**

```go
package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/human"
)

// approval is approval.json. PlanHash binds it to one exact plan.
type approval struct {
	ApprovedAt string `json:"approved_at"`
	ApprovedBy string `json:"approved_by"`
	PlanHash   string `json:"plan_hash"`
}

func approveDone(r *run) bool { return exists(r.path(fileApproval)) }

// approve shows the plan to a person and records the decision. Nothing past
// this stage runs without an approval.json whose hash matches the plan.
func (p *Planner) approve(ctx context.Context, r *run) error {
	md, hash, err := RenderPlan(r.dir)
	if err != nil {
		return err
	}
	by := "flag"
	if !r.opts.Yes {
		if p.d.Gate == nil {
			return errors.New("the plan needs approval and no human gate is configured; pass --yes to approve it unseen")
		}
		d, err := p.d.Gate.Approve(ctx, human.PlanSummary{Markdown: md, Hash: hash})
		if err != nil {
			return err
		}
		if !d.Approved {
			if d.Note != "" {
				return fmt.Errorf("plan not approved: %s", d.Note)
			}
			return errors.New("plan not approved")
		}
		if by = d.By; by == "" {
			by = "gate"
		}
	}
	return writeJSON(r.path(fileApproval), approval{ApprovedAt: p.d.Now().UTC().Format("2006-01-02T15:04:05Z"), ApprovedBy: by, PlanHash: hash})
}

// RenderPlan is the plan as a person approves it, and its SHA-256. It is
// built only from files in the run folder, and it contains everything in
// coverage.json, so changing the contract, a draft, an answer or the coverage
// mapping after approval changes the hash.
func RenderPlan(runDir string) (markdown, hash string, err error) {
	r := &run{dir: runDir}
	src, err := os.ReadFile(r.path(fileBrief))
	if err != nil {
		return "", "", err
	}
	b, err := brief.Parse(src)
	if err != nil {
		return "", "", err
	}
	reqs := ParseRequirements(src)
	c, err := loadContracts(r)
	if err != nil {
		return "", "", err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return "", "", err
	}
	cov, err := ReadCoverage(runDir)
	if err != nil {
		return "", "", err
	}
	as, err := loadAnswers(r)
	if err != nil {
		return "", "", err
	}
	w, err := planWaves(c.BriefID, c, dec)
	if err != nil {
		return "", "", err
	}

	var s strings.Builder
	f := b.Front
	fmt.Fprintf(&s, "# Plan: %s\n\n", f.Title)
	fmt.Fprintf(&s, "- Run: %s\n- Repository: %s\n- Landing: %s onto %s\n- On ambiguity: %s\n- Contract revision: %d\n\n", f.ID, f.Repo, f.Landing, f.BaseBranch, f.OnAmbiguity, c.Revision)

	s.WriteString("## Components\n\n| Component | Functions | Waves |\n|---|---|---|\n")
	total, top := 0, -1
	for _, comp := range c.Components {
		drafts := dec.Components[comp.ID]
		lo, hi := -1, -1
		for _, d := range drafts {
			id, _ := d["id"].(string)
			wave := w[id]
			if lo < 0 || wave < lo {
				lo = wave
			}
			hi = max(hi, wave)
		}
		span := "none"
		switch {
		case lo >= 0 && lo == hi:
			span = fmt.Sprint(lo)
		case lo >= 0:
			span = fmt.Sprintf("%d to %d", lo, hi)
		}
		fmt.Fprintf(&s, "| %s | %d | %s |\n", comp.ID, len(drafts), span)
		total += len(drafts)
		top = max(top, hi)
	}
	fmt.Fprintf(&s, "\nFunctions: %d in %d wave(s).\n\n", total, top+1)

	s.WriteString("## Assumptions\n\n")
	n := 0
	for _, a := range as.Answers {
		if a.Assumed {
			fmt.Fprintf(&s, "- %s: %s Assumed: %s\n", a.Stage, oneLine(a.Question, 300), oneLine(a.Answer, 300))
			n++
		}
	}
	for _, comp := range c.Components {
		for _, d := range dec.Components[comp.ID] {
			for _, text := range strList(d["assumptions"]) {
				fmt.Fprintf(&s, "- %v: %s\n", d["id"], oneLine(text, 300))
				n++
			}
		}
	}
	if n == 0 {
		s.WriteString("None.\n")
	}

	s.WriteString("\n## Secrets, environment and network\n\n")
	var secrets, env, hosts []string
	for _, x := range f.Secrets {
		secrets = append(secrets, x.Name)
	}
	for _, x := range f.Env {
		env = append(env, x.Name)
	}
	for _, x := range f.Network {
		h := x.Host
		if x.Critical {
			h += " (critical)"
		}
		hosts = append(hosts, h)
	}
	fmt.Fprintf(&s, "- Secrets (names only): %s\n- Environment: %s\n- Hosts: %s\n", orNone(secrets), orNone(env), orNone(hosts))

	s.WriteString("\n## Coverage\n\n| Requirement | Text | Covered by | Root tests |\n|---|---|---|---|\n")
	text := map[string]string{}
	for _, q := range reqs {
		text[q.ID] = q.Text
		if q.Kind == ReqFeature {
			text[q.ID] = q.Name
		}
	}
	for _, cv := range cov.Covered {
		fmt.Fprintf(&s, "| %s | %s | %s | %s |\n", cv.Requirement, cell(oneLine(text[cv.Requirement], 80)), cell(orNone(cv.Nodes)), cell(orNone(cv.RootTests)))
	}
	fmt.Fprintf(&s, "\nRequirements covered: %d of %d\n", len(cov.Covered), len(reqs))
	if cov.Rounds > 0 {
		fmt.Fprintf(&s, "Coverage fill rounds: %d\n", cov.Rounds)
	}

	s.WriteString("\n## Root test commands\n\nThese commands were written by a model. Approving this plan approves running them when the build is verified.\n\n")
	if len(cov.RootTests) == 0 {
		s.WriteString("None.\n")
	}
	for _, t := range cov.RootTests {
		fmt.Fprintf(&s, "- %s: %s\n\n      %s\n", t.Requirement, t.Name, strings.ReplaceAll(t.Command, "\n", "\n      "))
	}

	s.WriteString("\n## Warnings\n\n")
	if len(cov.Warnings) == 0 {
		s.WriteString("None.\n")
	}
	warnings := append([]string(nil), cov.Warnings...)
	sort.Strings(warnings)
	for _, wn := range warnings {
		fmt.Fprintf(&s, "- %s\n", wn)
	}

	markdown = s.String()
	sum := sha256.Sum256([]byte(markdown))
	return markdown, hex.EncodeToString(sum[:]), nil
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

// cell makes text safe inside a markdown table cell.
func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
```

- [ ] **Step 4: Implement `testwriter.go`**

```go
package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/tree"
)

// testwriterState is _state/testwriter.json: the tests of every node that is
// finished, and the hash of the test file written for it.
type testwriterState struct {
	Nodes map[string]writtenTests `json:"nodes"`
}

type writtenTests struct {
	Tests    []map[string]any `json:"tests"`
	TestFile string           `json:"test_file"` // repo-relative, forward slashes
	SHA256   string           `json:"sha256"`
}

func testwriterDone(r *run) bool { return r.status.PlannedAt != "" }

// testFuncName is the test function a node's tests live in: fn-validate-email
// gives TestValidateEmail. Node ids are unique, so the names are too.
func testFuncName(nodeID string) string {
	name := "Test"
	for _, part := range strings.Split(strings.TrimPrefix(nodeID, "fn-"), "-") {
		if part != "" {
			name += strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return name
}

// testFilePath is where a node's test file goes: beside the function's file,
// named after the node so two functions in one file never share a test file.
func testFilePath(nodeID, contractFile string) string {
	return path.Join(path.Dir(contractFile), strings.ReplaceAll(nodeID, "-", "_")+"_test.go")
}

// testCommand is the command of every test of a node. The harness writes it;
// a model never does.
func testCommand(contractFile, funcName string) string {
	dir := path.Dir(contractFile)
	if dir != "." {
		dir = "./" + dir
	}
	return "go test " + dir + " -run ^" + funcName + "$"
}

// testwriter is the Test-writer stage: for every function node, tests written
// from its contract alone and a test file placed in the target repository,
// then the finished tree and the blackboard rows. It is the first stage that
// touches the repository, and it refuses to run without a matching approval.
func (p *Planner) testwriter(ctx context.Context, r *run) error {
	var ap approval
	found, err := readJSON(r.path(fileApproval), &ap)
	if err != nil {
		return err
	}
	_, hash, err := RenderPlan(r.dir)
	if err != nil {
		return err
	}
	if !found || ap.PlanHash != hash {
		return errors.New("approval.json does not match the plan as it stands; remove it and resume to approve the plan again")
	}

	c, err := loadContracts(r)
	if err != nil {
		return err
	}
	dec, err := loadDecomposed(r)
	if err != nil {
		return err
	}
	classes, err := loadClasses(r)
	if err != nil {
		return err
	}
	w, err := planWaves(r.id, c, dec)
	if err != nil {
		return err
	}
	st := testwriterState{}
	if _, err := readJSON(r.path(stateTestwriter), &st); err != nil {
		return err
	}
	if st.Nodes == nil {
		st.Nodes = map[string]writtenTests{}
	}

	var drafts []map[string]any
	for _, comp := range c.Components {
		drafts = append(drafts, dec.Components[comp.ID]...)
	}
	sort.SliceStable(drafts, func(i, j int) bool {
		a, _ := drafts[i]["id"].(string)
		b, _ := drafts[j]["id"].(string)
		if w[a] != w[b] {
			return w[a] < w[b]
		}
		return a < b
	})
	for _, d := range drafts {
		id, _ := d["id"].(string)
		if _, done := st.Nodes[id]; done {
			continue
		}
		wt, err := p.writeTests(ctx, r, c, d, classes[id])
		if err != nil {
			return fmt.Errorf("node %s: %w", id, err)
		}
		st.Nodes[id] = wt
		if err := writeJSON(r.path(stateTestwriter), st); err != nil {
			return err
		}
	}
	return p.finishTree(ctx, r, c, dec, st, w)
}

// writeTests makes the model call for one node and writes its test file.
func (p *Planner) writeTests(ctx context.Context, r *run, c *contract.Contracts, d map[string]any, class string) (writtenTests, error) {
	id, _ := d["id"].(string)
	ct, _ := d["contract"].(map[string]any)
	file, _ := ct["file"].(string)
	pkg, _ := ct["package"].(string)
	funcName := testFuncName(id)
	rel := testFilePath(id, file)
	abs, err := safeTestPath(r.repo, rel)
	if err != nil {
		return writtenTests{}, err
	}
	if exists(abs) {
		return writtenTests{}, fmt.Errorf("test file %s already exists and this run did not write it; move it away, then resume", rel)
	}
	prompt, err := render("testwriter", map[string]string{
		"Node": mustJSON(d), "PackageDir": path.Dir(file), "TestFuncName": funcName, "TestFile": rel})
	if err != nil {
		return writtenTests{}, err
	}
	var tests []map[string]any
	var source string
	cs := callSpec{stage: "testwrite:" + id, taskType: "testwrite", nodeID: id, nodeClass: class, scope: router.ScopeNode, maxTokens: maxTokensTestwrite}
	err = p.callAsking(ctx, r, cs, prompt, func(text string) error {
		ts, src, err := parseTestwrite(StripReply(text), ct, pkg, funcName, c.Module)
		if err != nil {
			return err
		}
		tests, source = ts, src
		return nil
	})
	if err != nil {
		return writtenTests{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return writtenTests{}, err
	}
	if err := os.WriteFile(abs, []byte(source), 0o644); err != nil {
		return writtenTests{}, err
	}
	sum := sha256.Sum256([]byte(source))
	return writtenTests{Tests: tests, TestFile: rel, SHA256: hex.EncodeToString(sum[:])}, nil
}

// parseTestwrite checks a Test-writer reply: enough tests, each described,
// and a Go test file that parses, holds the expected test function, and
// imports nothing outside the standard library and the module. Level and
// command are set here, whatever the model wrote.
func parseTestwrite(text string, ct map[string]any, pkg, funcName, module string) ([]map[string]any, string, error) {
	var reply struct {
		Tests    []map[string]any `json:"tests"`
		TestFile string           `json:"test_file"`
	}
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return nil, "", fmt.Errorf("test-writer reply is not a JSON object: %w", err)
	}
	need := len(objects(ct["errors"])) + 1
	if len(reply.Tests) < need {
		return nil, "", fmt.Errorf("test-writer reply has %d tests; the contract lists %d error conditions, so at least %d are needed (the happy path and one per condition)",
			len(reply.Tests), need-1, need)
	}
	file, _ := ct["file"].(string)
	command := testCommand(file, funcName)
	tests := make([]map[string]any, 0, len(reply.Tests))
	for i, t := range reply.Tests {
		name, _ := t["name"].(string)
		given, okGiven := t["given"].(string)
		expect, okExpect := t["expect"].(string)
		if strings.TrimSpace(name) == "" || !okGiven || !okExpect || strings.TrimSpace(expect) == "" {
			return nil, "", fmt.Errorf("test %d needs a name, a given and an expect", i+1)
		}
		tests = append(tests, map[string]any{"name": name, "level": "unit", "given": given, "expect": expect, "command": command})
	}
	if err := checkTestSource(reply.TestFile, pkg, funcName, module); err != nil {
		return nil, "", err
	}
	return tests, reply.TestFile, nil
}

// checkTestSource refuses a test file that is not the one asked for.
func checkTestSource(src, pkg, funcName, module string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("test file is not valid Go: %v", err)
	}
	if f.Name.Name != pkg && f.Name.Name != pkg+"_test" {
		return fmt.Errorf("test file is in package %s, want %s or %s_test", f.Name.Name, pkg, pkg)
	}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return fmt.Errorf("test file has an unreadable import %s", imp.Path.Value)
		}
		first, _, _ := strings.Cut(p, "/")
		standard := !strings.Contains(first, ".")
		own := module != "" && (p == module || strings.HasPrefix(p, module+"/"))
		if !standard && !own {
			return fmt.Errorf("test file imports %q; only the standard library and this module (%s) are allowed", p, module)
		}
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != funcName {
			continue
		}
		if ps := fd.Type.Params; ps != nil && len(ps.List) == 1 {
			if star, ok := ps.List[0].Type.(*ast.StarExpr); ok {
				if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "T" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "testing" {
						return nil
					}
				}
			}
		}
		return fmt.Errorf("test file declares %s but not as func %s(t *testing.T)", funcName, funcName)
	}
	return fmt.Errorf("test file has no func %s(t *testing.T)", funcName)
}

// safeTestPath returns where rel lands inside repo, refusing anything that is
// not a test file strictly inside the repository, including a path that would
// pass through a symbolic link pointing out of it.
func safeTestPath(repo, rel string) (string, error) {
	if rel == "" || path.IsAbs(rel) || strings.Contains(rel, `\`) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("test file path %q is not a clean path inside the repository", rel)
	}
	if !strings.HasSuffix(rel, "_test.go") {
		return "", fmt.Errorf("test file path %q does not end in _test.go", rel)
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	// The deepest directory on the way that already exists decides where the
	// file would really be written.
	dir := filepath.Dir(abs)
	for !exists(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", fmt.Errorf("test file path %q resolves outside the repository", rel)
	}
	return abs, nil
}

// finishTree writes the complete tree, checks it, records the test files the
// executor must never let an implementer edit, and creates the blackboard rows.
func (p *Planner) finishTree(ctx context.Context, r *run, c *contract.Contracts, dec decomposed, st testwriterState, w map[string]int) error {
	cov, err := ReadCoverage(r.dir)
	if err != nil {
		return err
	}
	root, comps, err := buildSkeleton(r, p.d.Settings.Defaults.MaxContextTokens, p.d.Settings.Defaults.MaxRevisions, c, dec, cov.RootTests)
	if err != nil {
		return err
	}
	docs := append([]map[string]any{root}, comps...)
	files := []string{}
	for _, comp := range c.Components {
		for _, d := range dec.Components[comp.ID] {
			id, _ := d["id"].(string)
			wt, ok := st.Nodes[id]
			if !ok {
				return fmt.Errorf("node %s has no tests", id)
			}
			doc, err := copyDoc(d)
			if err != nil {
				return err
			}
			doc["tests"], doc["wave"] = wt.Tests, w[id]
			docs = append(docs, doc)
			files = append(files, wt.TestFile)
		}
	}
	nodes := make([]tree.Node, 0, len(docs))
	for _, doc := range docs {
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		n, err := tree.ParseNode(raw)
		if err != nil {
			return fmt.Errorf("node %v: %w", doc["id"], err)
		}
		nodes = append(nodes, n)
	}
	t, err := tree.NewTree(nodes)
	if err != nil {
		return err
	}
	store := tree.NewStore(r.dir)
	if err := store.WriteAll(t); err != nil {
		return err
	}
	loaded, err := store.Load()
	if err != nil {
		return err
	}
	if err := loaded.CheckStructure(); err != nil {
		return err
	}
	if err := loaded.CheckWaves(); err != nil {
		return err
	}
	computed, err := loaded.ComputeWaves()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(computed))
	for id, wave := range computed {
		if w[id] != wave {
			return fmt.Errorf("node %s: the plan showed wave %d but the tree computes %d", id, w[id], wave)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sort.Strings(files)
	if err := writeJSON(r.path(stateTestFiles), files); err != nil {
		return err
	}
	if p.d.Board != nil {
		if err := p.d.Board.InitRun(ctx, r.id, ids, computed); err != nil {
			return err
		}
	}
	r.status.PlannedAt = p.d.Now().UTC().Format("2006-01-02T15:04:05Z")
	return r.saveStatus()
}
```

In `briefv2/planner/planner.go` add the two stages after the coverage line, which completes the list:

```go
	{"approve", (*Planner).approve, approveDone},
	{"testwriter", (*Planner).testwriter, testwriterDone},
```

- [ ] **Step 5: Run the whole planner package**

Run: `cd gophermind-lib && gofmt -l briefv2 && go vet ./briefv2/... && go test ./briefv2/planner/ -race -count=3 2>&1 | tail -3`
Expected: `gofmt` prints nothing; `ok  gophermind/gophermind-lib/briefv2/planner` (63 test functions, three runs).

- [ ] **Step 6: Prove the approval check and the privacy test can fail**

In `testwriter.go` change `if !found || ap.PlanHash != hash {` to `if !found {` and run `cd gophermind-lib && go test ./briefv2/planner/ -run TestAChangedPlanInvalidatesTheApproval 2>&1 | tail -3`.
Expected: FAIL. Restore the line.

In `clarify.go` change `scope: router.ScopeBrief` to `scope: router.ScopeNode` and run `cd gophermind-lib && go test ./briefv2/planner/ -run TestAPublicProviderOnlyEverSeesNodeScopeCalls 2>&1 | tail -3`.
Expected: FAIL (`the public provider was sent a clarify call`). Restore the line, then run `git diff --stat` and confirm neither file shows a change from this step.

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/briefv2/planner
git commit -m "feat(briefv2): approval gate, test writer, finished tree and blackboard rows" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Commands, documentation, and the smoke test

**Files:**
- Create: `cmd/gophermind/brief_plan.go`
- Modify: `cmd/gophermind/brief.go` (usage text, the dispatch switch, one comment)
- Modify: `docs/briefv2/README.md`
- Test: `cmd/gophermind/brief_plan_test.go`

**Interfaces:**
- Consumes: `planner.New`, `planner.Deps`, `planner.Options`, `planner.Done`, `planner.Waiting`, `planner.Secrets`, `planner.LookupRun`, `planner.ReadStatus`, `planner.ReadCoverage`, `planner.ParseRequirements`, `planner.FixtureProvider`, `planner.FixtureSettings` (Tasks 7 to 11); `settings.Path`, `settings.Load`, `(*settings.Config).BuildProviders`, `settings.Config.Human.Mode` (Task 4); `router.New`, `router.WithAllowPublic`, `(*router.Router).LedgerErrors` (Task 5); `db.DefaultPath`, `db.Open` (Task 1); `ledger.NewSQLite` with `List` and `Summary`, `ledger.Filter`, `ledger.OutcomeOK`, `ledger.OutcomeMalformed` (Task 2); `blackboard.NewSQLite` (Task 1); `human.NewTerminal`, `human.NewFile`, `human.Gate` (Task 6); `events.Event`, `events.Kind*` (Task 3); the existing `runBrief`, `briefUsage`, `vaultPath`, `vaultOptions`, `vault.Passphrase`, `vault.ReadSecret`, `vault.Open`, `brief.Parse`, `*brief.InvalidError`.
- Produces: `gophermind brief plan <brief.md> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]`, `gophermind brief resume <run-id>` with the same flags, `gophermind brief status <run-id>`, `gophermind brief coverage <run-id>`, `gophermind brief calls <run-id>`. Exit codes: 0 done, 1 error, 2 invalid brief, 3 waiting on a human.

What this task builds. The commands are thin: they wire settings, the database, the providers, the router, a gate and a printing sink, and call `planner.Run`. With `--fake` nothing outside the fixture directory and the config directory is touched: no settings file is read or written, no network, no vault. `--fake` takes one directory or several separated by commas, the first holding a reply winning, which is how a variant fixture overrides single replies of a base one.

The file gate needs the run folder, which only exists once Load has run, so the command wraps it in a small gate that looks the run up by id each time it is used. The brief is parsed once in the command to learn that id, which also gives exit code 2 for an invalid brief before anything else happens.

- [ ] **Step 1: Write the failing command tests** (`cmd/gophermind/brief_plan_test.go`)

They drive `runBrief` the way `brief_test.go` already does (`runBriefCmd` is defined there), with the greeter fixture from Task 8.

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	greeterFixture = "../../gophermind-lib/briefv2/planner/testdata/greeter"
	greeterRunID   = "gm-2026-09-29-900"
)

// planEnv gives a test its own config dir (settings, run registry, database)
// and a target repo, and returns the repo and a greeter brief pointing at it.
func planEnv(t *testing.T) (repo, briefPath string) {
	t.Helper()
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	repo = t.TempDir()
	raw, err := os.ReadFile(filepath.Join(greeterFixture, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	briefPath = filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(briefPath, []byte(strings.Replace(string(raw), "REPO_DIR", repo, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo, briefPath
}

// fill writes text into the first empty or default-holding fenced block of
// the given kind in a gate file.
func fill(t *testing.T, path, kind, text string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "```"+kind) {
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(lines[end], "```") {
				end++
			}
			out := append(append(append([]string{}, lines[:i+1]...), text), lines[end:]...)
			if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("%s has no %s block", path, kind)
}

// The unattended flow: plan stops for the questions (exit 3), resume stops
// for the approval (exit 3), resume finishes (exit 0).
func TestBriefPlanWithTheFileGate(t *testing.T) {
	repo, briefPath := planEnv(t)
	runDir := filepath.Join(repo, ".gophermind", greeterRunID)

	code, out, errs := runBriefCmd(t, "", "plan", briefPath, "--fake", greeterFixture, "--gate", "file")
	if code != 3 {
		t.Fatalf("plan: code=%d out=%q err=%q, want 3 (waiting)", code, out, errs)
	}
	if !strings.Contains(out, "gophermind brief resume "+greeterRunID) || !strings.Contains(errs, "clarify: started") {
		t.Errorf("plan output: out=%q err=%q", out, errs)
	}
	fill(t, filepath.Join(runDir, "QUESTIONS.md"), "answer", "Yes, and collapse inner spaces too.")

	code, out, errs = runBriefCmd(t, "", "resume", greeterRunID, "--fake", greeterFixture, "--gate", "file")
	if code != 3 {
		t.Fatalf("first resume: code=%d out=%q err=%q, want 3 (waiting for approval)", code, out, errs)
	}
	approval, err := os.ReadFile(filepath.Join(runDir, "APPROVAL.md"))
	if err != nil || !strings.Contains(string(approval), "Requirements covered: 7 of 7") {
		t.Fatalf("APPROVAL.md does not show the coverage: %v", err)
	}
	if entries, _ := os.ReadDir(repo); len(entries) != 1 {
		t.Errorf("the repo holds %d entries before approval, want only .gophermind", len(entries))
	}
	code, out, _ = runBriefCmd(t, "", "status", greeterRunID)
	if code != 0 || !strings.Contains(out, "waiting on a human: approve") || !strings.Contains(out, "approve     not done") {
		t.Errorf("status while waiting: code=%d out=%q", code, out)
	}
	fill(t, filepath.Join(runDir, "APPROVAL.md"), "decision", "approve")

	code, out, errs = runBriefCmd(t, "", "resume", greeterRunID, "--fake", greeterFixture, "--gate", "file")
	if code != 0 || !strings.Contains(out, "planned: "+greeterRunID) || !strings.Contains(out, "Requirements covered: 7 of 7") {
		t.Fatalf("second resume: code=%d out=%q err=%q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(repo, "internal", "greet", "fn_greet_test.go")); err != nil {
		t.Errorf("the test file was not written: %v", err)
	}

	code, out, _ = runBriefCmd(t, "", "tree", "check", runDir)
	if code != 0 || !strings.Contains(out, "ok: 7 nodes, waves 0-1") {
		t.Errorf("tree check on the planned run: code=%d out=%q", code, out)
	}
	code, out, _ = runBriefCmd(t, "", "status", greeterRunID)
	if code != 0 || strings.Contains(out, "not done") || strings.Contains(out, "waiting on a human") {
		t.Errorf("status of a finished run: code=%d out=%q", code, out)
	}
	for _, want := range []string{"testwriter  done", "Requirements covered: 7 of 7", "TASK", "CLASS", "testwrite  validation  fake/fixture  2", "contract   -           fake/fixture  4"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = runBriefCmd(t, "", "calls", greeterRunID)
	if code != 0 || !strings.Contains(out, "12 call(s)") || !strings.Contains(out, "testwrite:fn-greet") || !strings.Contains(out, "contract:outline") {
		t.Errorf("calls: code=%d out=%q", code, out)
	}
	for _, want := range []string{"TASK", "CLASS", "validation", "OUTCOME"} {
		if !strings.Contains(out, want) {
			t.Errorf("calls lacks the %q column:\n%s", want, out)
		}
	}
	code, out, _ = runBriefCmd(t, "", "coverage", greeterRunID)
	if code != 0 || !strings.Contains(out, "Requirements covered: 7 of 7 (fill rounds: 0)") || !strings.Contains(out, "formatted and vetted") {
		t.Errorf("coverage: code=%d out=%q", code, out)
	}
}

// At a terminal with nothing typed, the default answer is taken and --yes
// stands in for the approval.
func TestBriefPlanWithYesAtATerminal(t *testing.T) {
	repo, briefPath := planEnv(t)
	code, out, errs := runBriefCmd(t, "", "plan", "--yes", "--fake", greeterFixture, "--gate", "terminal", "--allow-public", briefPath)
	if code != 0 || !strings.Contains(out, "planned: "+greeterRunID) {
		t.Fatalf("plan: code=%d out=%q err=%q", code, out, errs)
	}
	for _, want := range []string{"Should a name be trimmed", "coverage: done", "testwriter: done", "warning: --allow-public"} {
		if !strings.Contains(errs, want) {
			t.Errorf("progress output lacks %q:\n%s", want, errs)
		}
	}
	approval, err := os.ReadFile(filepath.Join(repo, ".gophermind", greeterRunID, "approval.json"))
	if err != nil || !strings.Contains(string(approval), `"approved_by": "flag"`) {
		t.Errorf("approval.json = %s, %v", approval, err)
	}
	if _, out, _ := runBriefCmd(t, "", "status", greeterRunID); !strings.Contains(out, "public providers were allowed") {
		t.Errorf("status does not say the run allowed public providers:\n%s", out)
	}
	code, _, errs = runBriefCmd(t, "", "plan", briefPath, "--yes", "--fake", greeterFixture)
	if code != 1 || !strings.Contains(errs, "gophermind brief resume "+greeterRunID) {
		t.Errorf("planning the same brief twice: code=%d err=%q, want 1 and a pointer at resume", code, errs)
	}
}

func TestBriefPlanExitCodes(t *testing.T) {
	_, briefPath := planEnv(t)
	raw, _ := os.ReadFile(briefPath)
	write := func(text string) string {
		p := filepath.Join(t.TempDir(), "brief.md")
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	noVersion := write(strings.Replace(string(raw), "spec_version: \"2.0\"\n", "", 1))
	urlRepo := write(strings.Replace(string(raw), "repo: ", "repo: https://example.com/x.git #", 1))
	stuck := "../../gophermind-lib/briefv2/planner/testdata/greeter-stuck"

	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"an invalid brief is 2", []string{"plan", noVersion, "--fake", greeterFixture}, 2, "spec_version"},
		{"a repo that is a URL is 2", []string{"plan", urlRepo, "--fake", greeterFixture}, 2, "is a URL"},
		{"an unreadable brief is 1", []string{"plan", filepath.Join(t.TempDir(), "nope.md")}, 1, "no such file"},
		{"no brief is a usage error", []string{"plan"}, 1, "usage:"},
		{"two briefs is a usage error", []string{"plan", briefPath, briefPath}, 1, "usage:"},
		{"an unknown flag is a usage error", []string{"plan", briefPath, "--fast"}, 1, "usage:"},
		{"an unknown gate is 1", []string{"plan", briefPath, "--fake", greeterFixture, "--gate", "pigeon"}, 1, `unknown gate "pigeon"`},
		{"a missing fixture directory is 1", []string{"plan", briefPath, "--fake", filepath.Join(t.TempDir(), "nope")}, 1, "fixture directory"},
		{"an unknown run is 1", []string{"resume", "gm-2026-01-01-001", "--fake", greeterFixture}, 1, "no run gm-2026-01-01-001"},
		{"status of an unknown run is 1", []string{"status", "gm-2026-01-01-001"}, 1, "no run gm-2026-01-01-001"},
		{"calls of an unknown run is 1", []string{"calls", "gm-2026-01-01-001"}, 1, "no run gm-2026-01-01-001"},
		{"coverage of an unknown run is 1", []string{"coverage", "gm-2026-01-01-001"}, 1, "no run gm-2026-01-01-001"},
		{"status needs a run id", []string{"status"}, 1, "usage:"},
		{"uncovered requirements are 1", []string{"plan", briefPath, "--yes", "--fake", stuck + "," + greeterFixture}, 1, "1 requirement(s) of the brief are not covered by the plan"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errs := runBriefCmd(t, "", c.args...)
			if code != c.code || !strings.Contains(errs, c.want) {
				t.Errorf("code=%d out=%q err=%q, want %d and %q", code, out, errs, c.code, c.want)
			}
		})
	}
}
```

Run: `go test ./cmd/gophermind/ -run BriefPlan 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'`
Expected: three `--- FAIL` lines (`TestBriefPlanWithTheFileGate`, `TestBriefPlanWithYesAtATerminal`, `TestBriefPlanExitCodes`): `plan` is not a subcommand yet, so every call prints the usage and returns 1.

- [ ] **Step 2: Implement `cmd/gophermind/brief_plan.go`**

```go
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

// Exit codes of `gophermind brief plan` and `resume`.
const (
	exitDone    = 0
	exitError   = 1
	exitInvalid = 2
	exitWaiting = 3
)

// printSink writes the planner's progress to the terminal. Model calls are
// not printed one by one; `gophermind brief calls` shows them.
type printSink struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *printSink) Emit(e events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e.Kind {
	case events.KindStageStarted:
		fmt.Fprintf(s.w, "%s: started\n", e.Stage)
	case events.KindStageFinished:
		fmt.Fprintf(s.w, "%s: done\n", e.Stage)
	case events.KindWaitingOnHuman:
		fmt.Fprintf(s.w, "%s: %s\n", e.Stage, e.Message)
	case events.KindWarning:
		fmt.Fprintf(s.w, "warning: %s\n", e.Message)
	case events.KindCoverageGap:
		fmt.Fprintf(s.w, "coverage gap, %s\n", e.Message)
	case events.KindLedgerError:
		fmt.Fprintf(s.w, "warning: a model call could not be recorded in the ledger: %s\n", e.Message)
	}
}

// lazyFileGate is the file gate for a run whose folder is only known once
// Load has created it: it looks the run up each time it is used.
type lazyFileGate struct{ runID string }

func (g lazyFileGate) file() (*human.File, error) {
	rec, err := planner.LookupRun(g.runID)
	if err != nil {
		return nil, err
	}
	return human.NewFile(rec.RunDir), nil
}

func (g lazyFileGate) Ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	f, err := g.file()
	if err != nil {
		return nil, err
	}
	return f.Ask(ctx, qs)
}

func (g lazyFileGate) Approve(ctx context.Context, plan human.PlanSummary) (human.Decision, error) {
	f, err := g.file()
	if err != nil {
		return human.Decision{}, err
	}
	return f.Approve(ctx, plan)
}

func (g lazyFileGate) Escalate(ctx context.Context, e human.Escalation) (human.Resolution, error) {
	f, err := g.file()
	if err != nil {
		return human.Resolution{}, err
	}
	return f.Escalate(ctx, e)
}

// briefPlan implements `gophermind brief plan <brief.md>` and
// `gophermind brief resume <run-id>`.
func briefPlan(verb string, args []string, in *os.File, out, errw io.Writer) int {
	fs := flag.NewFlagSet("brief "+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "approve the plan without showing it")
	gateMode := fs.String("gate", "", "terminal or file")
	fake := fs.String("fake", "", "answer every model call from this fixture directory (several, comma separated: the first holding a reply wins)")
	allowPublic := fs.Bool("allow-public", false, "let public providers see the brief")
	// The target comes first, then the flags; flags first also works.
	target := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(errw, briefUsage)
		return exitError
	}
	if target == "" && fs.NArg() == 1 {
		target = fs.Arg(0)
	} else if fs.NArg() != 0 {
		target = ""
	}
	if target == "" {
		fmt.Fprintln(errw, briefUsage)
		return exitError
	}

	opts := planner.Options{Yes: *yes, AllowPublic: *allowPublic}
	runID := target
	if verb == "plan" {
		src, err := os.ReadFile(target)
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		b, err := brief.Parse(src)
		if err != nil {
			return briefFail(errw, err)
		}
		opts.BriefPath, runID = target, b.Front.ID
	} else {
		opts.RunID = target
	}

	var cfg *settings.Config
	var providers map[string]provider.Provider
	var v *vault.Vault
	openVault := func() (*vault.Vault, error) {
		if v != nil {
			return v, nil
		}
		path, err := vaultPath()
		if err != nil {
			return nil, err
		}
		pass, err := vault.Passphrase("Vault passphrase: ", in, errw)
		if err != nil {
			return nil, err
		}
		v, err = vault.Open(path, pass, vaultOptions)
		return v, err
	}
	if *fake != "" {
		fp, err := planner.FixtureProvider(strings.Split(*fake, ",")...)
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		cfg, providers = planner.FixtureSettings(), map[string]provider.Provider{"fake": fp}
	} else {
		path, err := settings.Path()
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		if cfg, err = settings.Load(path); err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
		// The harness proxy does not exist yet (spec deviation P4), so the
		// providers get a plain client. The router bounds every call with
		// defaults.call_timeout.
		providers, err = cfg.BuildProviders(&http.Client{}, func(name string) (string, error) {
			vlt, err := openVault()
			if err != nil {
				return "", err
			}
			val, ok := vlt.Get(vault.HarnessScope, name)
			if !ok {
				return "", fmt.Errorf("not in the vault; set it with `gophermind brief vault set %s`", name)
			}
			return val, nil
		})
		if err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
	}

	mode := *gateMode
	if mode == "" {
		mode = cfg.Human.Mode
	}
	deps := planner.Deps{Settings: cfg, OpenSecrets: func() (planner.Secrets, error) { return openVault() }}
	switch mode {
	case "terminal":
		deps.Gate = human.NewTerminal(in, errw)
		deps.PromptSecret = func(name, purpose string) (string, error) {
			return vault.ReadSecret(fmt.Sprintf("Value for %s (%s): ", name, purpose), in, errw)
		}
	case "file":
		deps.Gate = lazyFileGate{runID: runID}
	default:
		fmt.Fprintf(errw, "error: unknown gate %q (want terminal or file)\n", mode)
		return exitError
	}

	d, code := openBriefDB(errw)
	if d == nil {
		return code
	}
	defer d.Close()
	sink := &printSink{w: errw}
	rt := router.New(cfg, providers, ledger.NewSQLite(d), sink, router.WithAllowPublic(*allowPublic))
	deps.Caller, deps.Sink, deps.Board, deps.LedgerErrors = rt, sink, blackboard.NewSQLite(d), rt.LedgerErrors

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	outcome, err := planner.New(deps).Run(ctx, opts)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintf(errw, "interrupted; continue with `gophermind brief resume %s`\n", runID)
			return exitError
		}
		return briefFail(errw, err)
	}
	if outcome == planner.Waiting {
		if rec, lerr := planner.LookupRun(runID); lerr == nil {
			fmt.Fprintf(out, "waiting: answer in %s, then run `gophermind brief resume %s`\n", rec.RunDir, runID)
		}
		return exitWaiting
	}
	st, err := planner.ReadStatus(runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	fmt.Fprintf(out, "planned: %s in %s\nRequirements covered: %d of %d\n", st.RunID, st.RunDir, st.Covered, st.Requirements)
	return exitDone
}

// briefFail prints err and returns the exit code it deserves: 2 when the
// brief itself is wrong, 1 for everything else.
func briefFail(errw io.Writer, err error) int {
	var inv *brief.InvalidError
	if errors.As(err, &inv) {
		fmt.Fprintf(errw, "invalid brief: %v\n", err)
		return exitInvalid
	}
	fmt.Fprintf(errw, "error: %v\n", err)
	return exitError
}

func openBriefDB(errw io.Writer) (*sql.DB, int) {
	path, err := db.DefaultPath()
	if err == nil {
		var d *sql.DB
		if d, err = db.Open(path); err == nil {
			return d, exitDone
		}
	}
	fmt.Fprintf(errw, "error: %v\n", err)
	return nil, exitError
}

// briefStatus implements `gophermind brief status <run-id>`.
func briefStatus(runID string, out, errw io.Writer) int {
	st, err := planner.ReadStatus(runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	fmt.Fprintf(out, "run %s\nfolder: %s\nrepository: %s\n\nstages:\n", st.RunID, st.RunDir, st.Repo)
	for _, s := range st.Stages {
		state := "not done"
		if s.Done {
			state = "done"
		}
		fmt.Fprintf(out, "  %-11s %s\n", s.Name, state)
	}
	fmt.Fprintln(out)
	if st.Waiting != "" {
		fmt.Fprintf(out, "waiting on a human: %s\n", st.Waiting)
	}
	fmt.Fprintf(out, "Requirements covered: %d of %d\n", st.Covered, st.Requirements)
	if st.AllowPublic {
		fmt.Fprintln(out, "public providers were allowed to see the brief in this run (--allow-public)")
	}
	if st.LedgerErrors > 0 {
		fmt.Fprintf(out, "incomplete ledger: %d model call(s) could not be recorded\n", st.LedgerErrors)
	}

	d, code := openBriefDB(errw)
	if d == nil {
		return code
	}
	defer d.Close()
	sum, err := ledger.NewSQLite(d).Summary(context.Background(), runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	fmt.Fprintln(out, "\nmodel calls by task type and node class:")
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  TASK\tCLASS\tMODEL\tCALLS\tOK\tMALFORMED\tOTHER\tPROMPT TOK\tREPLY TOK\tSECONDS")
	for _, m := range sum {
		ok, bad := m.Outcomes[ledger.OutcomeOK], m.Outcomes[ledger.OutcomeMalformed]
		fmt.Fprintf(tw, "  %s\t%s\t%s/%s\t%d\t%d\t%d\t%d\t%d\t%d\t%.1f\n", m.TaskType, dash(m.NodeClass), m.Provider, m.Model,
			m.Calls, ok, bad, m.Calls-ok-bad, m.PromptTokens, m.CompletionTokens, float64(m.TotalMS)/1000)
	}
	tw.Flush()
	return exitDone
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// briefCalls implements `gophermind brief calls <run-id>`: the ledger as a table.
func briefCalls(runID string, out, errw io.Writer) int {
	if _, err := planner.LookupRun(runID); err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	d, code := openBriefDB(errw)
	if d == nil {
		return code
	}
	defer d.Close()
	rows, err := ledger.NewSQLite(d).List(context.Background(), runID, ledger.Filter{})
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tSTAGE\tTASK\tCLASS\tMODEL SERVED\tOUTCOME\tPROMPT TOK\tREPLY TOK\tMS")
	for _, c := range rows {
		model := c.ModelServed
		if model == "" {
			model = c.ModelRequested
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s/%s\t%s\t%d\t%d\t%d\n", c.At.UTC().Format("15:04:05"), c.Stage, c.TaskType, dash(c.NodeClass),
			c.Provider, model, c.Outcome, c.PromptTokens, c.CompletionTokens, c.DurationMS)
	}
	tw.Flush()
	fmt.Fprintf(out, "%d call(s)\n", len(rows))
	return exitDone
}

// briefCoverage implements `gophermind brief coverage <run-id>`: what covers
// each requirement of the brief, and the warnings.
func briefCoverage(runID string, out, errw io.Writer) int {
	rec, err := planner.LookupRun(runID)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	cov, err := planner.ReadCoverage(rec.RunDir)
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	src, err := os.ReadFile(filepath.Join(rec.RunDir, "brief.md"))
	if err != nil {
		fmt.Fprintf(errw, "error: %v\n", err)
		return exitError
	}
	reqs := planner.ParseRequirements(src)
	text := map[string]string{}
	for _, q := range reqs {
		text[q.ID] = q.Text
		if q.Name != "" {
			text[q.ID] = q.Name
		}
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REQUIREMENT\tTEXT\tCOVERED BY\tROOT TESTS")
	for _, c := range cov.Covered {
		short := strings.Join(strings.Fields(text[c.Requirement]), " ")
		if rs := []rune(short); len(rs) > 60 {
			short = string(rs[:60])
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Requirement, short, dash(strings.Join(c.Nodes, ", ")), dash(strings.Join(c.RootTests, ", ")))
	}
	tw.Flush()
	fmt.Fprintf(out, "\nRequirements covered: %d of %d (fill rounds: %d)\n", len(cov.Covered), len(reqs), cov.Rounds)
	for _, w := range cov.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	return exitDone
}
```

- [ ] **Step 3: Route the new subcommands in `cmd/gophermind/brief.go`**

Replace the usage constant

```go
const briefUsage = `usage:
  gophermind brief validate <brief.md>
  gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
  gophermind brief vault list
  gophermind brief tree check <run-dir>`
```

with

```go
const briefUsage = `usage:
  gophermind brief validate <brief.md>
  gophermind brief plan <brief.md> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
  gophermind brief resume <run-id> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
  gophermind brief status <run-id>
  gophermind brief coverage <run-id>
  gophermind brief calls <run-id>
  gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
  gophermind brief vault list
  gophermind brief tree check <run-dir>`
```

Replace the comment above `runBrief`

```go
// runBrief implements `gophermind brief ...` and returns the process exit
// code: 0 ok, 1 error, 2 invalid brief.
```

with

```go
// runBrief implements `gophermind brief ...` and returns the process exit
// code: 0 ok, 1 error, 2 invalid brief, 3 waiting on a human (plan and resume
// with the file gate).
```

In the `switch args[0]` of `runBrief`, put these cases in front of `case "vault":`

```go
	case "plan", "resume":
		return briefPlan(args[0], args[1:], in, out, errw)
	case "status", "coverage", "calls":
		if len(args) != 2 {
			fmt.Fprintln(errw, briefUsage)
			return 1
		}
		switch args[0] {
		case "status":
			return briefStatus(args[1], out, errw)
		case "coverage":
			return briefCoverage(args[1], out, errw)
		}
		return briefCalls(args[1], out, errw)
```

- [ ] **Step 4: Run the command tests**

Run: `gofmt -l cmd/gophermind && go vet ./cmd/gophermind/ && go test ./cmd/gophermind/ -race 2>&1 | tail -3`
Expected: `gofmt` prints nothing; `ok  gophermind/cmd/gophermind`. (In a git worktree use these package paths, not `./...`: the ignored `desktop/frontend/dist` folder is missing there.)

- [ ] **Step 5: Update `docs/briefv2/README.md`**

Four replacements. The file uses no em dashes and no emojis; keep it that way.

Replace the title and first paragraph:

```markdown
# GopherMind v2: brief to build (offline foundations)

v2 turns a written brief (a markdown file with YAML front matter) into a tree of
task nodes that agents build, wave by wave, against a shared contract. This
directory documents the offline foundations only: brief loading, the secret
vault, the node tree with waves, contract slicing, and the run directory. No
model calls happen in any of it.
```

with:

```markdown
# GopherMind v2: brief to build (foundations and planner)

v2 turns a written brief (a markdown file with YAML front matter) into a tree of
task nodes that agents build, wave by wave, against a shared contract. This
directory documents the foundations (brief loading, the secret vault, the node
tree with waves, contract slicing, the run directory) and the planner, which
takes a brief as far as an approved plan with its tests written. Nothing is
built or executed yet; that is the executor plan.
```

Replace the commands block and the two paragraphs after it:

````markdown
```text
gophermind brief validate <brief.md>
gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
gophermind brief vault list
gophermind brief tree check <run-dir>
```

Anything else under `gophermind brief` prints this usage and exits 1. The run,
resume, status and report subcommands arrive in later plans (deviation D1).

Exit codes: 0 ok, 1 error (usage, unreadable file, failed check), 2 invalid
brief (the message names the offending field). Codes 3 and 4 are reserved for
human gates in a later plan.
````

with:

````markdown
```text
gophermind brief validate <brief.md>
gophermind brief plan <brief.md> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
gophermind brief resume <run-id> [--yes] [--gate terminal|file] [--fake <fixture-dir>] [--allow-public]
gophermind brief status <run-id>
gophermind brief coverage <run-id>
gophermind brief calls <run-id>
gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
gophermind brief vault list
gophermind brief tree check <run-dir>
```

Anything else under `gophermind brief` prints this usage and exits 1. The run
and report subcommands arrive with the executor plan (deviation D1).

Exit codes: 0 ok, 1 error (usage, unreadable file, failed check, uncovered
requirements), 2 invalid brief (the message names the offending field), 3
waiting on a human (`plan` and `resume` with the file gate). Code 4 is reserved
for an escalated leaf in the executor plan.

`plan` takes a brief through Load, Clarify, Contract, Decompose, Coverage,
Approve and Test-writer (see "Planning a brief"). `resume` continues a run from
its first unfinished stage; the run id is the brief's `id`. `--yes` records the
approval as given by the flag without showing the plan. `--gate file` writes
`QUESTIONS.md` and `APPROVAL.md` into the run folder and exits 3 until they are
filled in; the default comes from `human.mode` in the settings. `--fake` answers
every model call from a directory of canned replies and needs no settings file,
no network and no model (several directories may be given, comma separated; the
first one holding a reply wins). `--allow-public` lets public providers see the
whole brief, and the run's status says so afterwards.

`status` prints each stage, what the run is waiting for, how many requirements
are covered, and the model calls summed by task type and node class. `coverage`
prints what covers each requirement of the brief and the warnings. `calls`
prints one line per model call.
````

Replace the heading `## Environment block` (three new sections go in front of it):

```markdown
## Environment block
```

with:

```markdown
## Planning a brief

`gophermind brief plan` runs these stages. Each one is skipped on `resume` when
its output is already in the run folder.

| Stage | What it does | Output |
|---|---|---|
| Load | Validates the brief, makes sure its secrets are in the vault, creates the run folder | `brief.md`, `requirements.json` |
| Clarify | Asks the model what it needs to know, then asks you (or takes the defaults when the brief says `assume_and_document`) | `answers.json` |
| Contract | One outline call, then one call per component, repeated while the model says more remains | `contracts.json` |
| Decompose | One node per function, at most 8 functions per call | the root and component nodes, drafts in `_state/` |
| Coverage | Maps every requirement of the brief to the nodes and tests that satisfy it | `coverage.json`, acceptance tests on the root node |
| Approve | Shows the plan and waits for a decision | `approval.json` |
| Test-writer | Writes the tests of every function from its contract alone | test files in the target repository, the finished tree, blackboard rows |

Size. Nothing caps the number of components, functions or tests. A large brief
makes more calls, never a coarser plan.

Requirements and coverage. Every top-level bullet under `## Constraints` (C1,
C2, ...) and `## Acceptance` (A1, A2, ...) and every `###` heading under
`## Features` (F1, F2, ...) is one requirement, parsed by code. A constraint
needs a covering node or a root test, a feature needs a covering node, and an
acceptance bullet needs a root test with a command. Gaps go back to the model
for up to `defaults.max_coverage_rounds` rounds; if any remain the run stops
with exit 1 and lists them, and nothing is approved. Write briefs with one
obligation per bullet: a bullet that bundles three counts as one requirement.

Leaf checks. A function node is refused unless its signature parses as Go, it
describes every parameter and every result, it lists at least one error
condition when the function returns `error`, and it carries a node class
(`pure`, `validation`, `handler`, `client`, `storage`, `concurrency`, `wiring`,
`other`). A test file is refused unless it parses, holds the expected test
function, imports only the standard library and the module's own packages, and
comes with at least one test per error condition plus one for the happy path.

What a model may not decide. Dependency signatures, waves, the path of a test
file and the command of every function test (`go test ./<dir> -run ^<Test>$`)
are derived by code. The only model-written commands in a plan are the root
acceptance tests, and the approval summary prints each one in full.

Nothing is written into the target repository before `approval.json` exists and
matches the plan as it stands. Test files are left uncommitted.

Run folder files added by the planner: `requirements.json`, `answers.json`,
`contracts.json`, `coverage.json`, `approval.json`, and working files under
`_state/` (`tree check` skips all of these). `<config dir>/runs/<run-id>.json`
records where a run's folder is, so `resume`, `status`, `coverage` and `calls`
need only the id.

## Settings

`<config dir>/gophermind.yaml` is written with defaults the first time `plan`
runs without `--fake`: the Mac mini as the one private provider, and two
providers that need no key. `models` lists, per tier (`strong`, `standard`,
`any`), the `provider/model` entries to try in order. `privacy.mode` is
`need_to_know` (public providers see single functions only) or `private_only`.
A provider's key is never in this file: `api_key_secret` names an entry set with
`gophermind brief vault set`.

## The call ledger

Every model call, including every failed attempt, is one row in the `calls`
table of `<config dir>/blackboard.db`: stage, task type (`clarify`, `contract`,
`decompose`, `coverage`, `testwrite`), node class for a call about one function,
provider, model requested and served, token counts, duration, and outcome. The
prompt and the reply are never stored, only their sizes and SHA-256 hashes.
`gophermind brief calls <run-id>` prints the rows; `status` prints them summed by
task type and node class, which is how models are compared by kind of work.

## Environment block
```

Replace two lines of the package map:

```markdown
  rundir/     .gophermind/<id>/ layout, kept out of git via .git/info/exclude
cmd/gophermind/brief.go   the `gophermind brief ...` command group
```

with:

```markdown
  rundir/     .gophermind/<id>/ layout, kept out of git via .git/info/exclude
  db/         the shared SQLite file and its migrations
  blackboard/ runtime state of every node (rows, claims, attempts)
  ledger/     one row per model call
  events/     progress events and sinks
  provider/   the provider interface, the OpenAI-compatible client, a scripted fake
  settings/   gophermind.yaml
  router/     fallback chains, cooldowns, the privacy rule
  human/      the human gate: terminal, file, programmatic
  planner/    requirements, the stages, coverage, prompts, the offline fixture provider
cmd/gophermind/brief.go        the `gophermind brief ...` command group
cmd/gophermind/brief_plan.go   plan, resume, status, coverage, calls
```

Check: `grep -c '## Planning a brief' docs/briefv2/README.md` prints `1`, and `grep -n $'\u2014' docs/briefv2/README.md` prints nothing.

- [ ] **Step 6: Run everything and commit**

Run: `gofmt -l cmd gophermind-lib/briefv2 && go build ./cmd/... ./gophermind-lib/... && go vet ./cmd/gophermind/ ./gophermind-lib/briefv2/... && go test ./cmd/gophermind/ ./gophermind-lib/briefv2/... -race 2>&1 | tail -20`
Expected: `gofmt` prints nothing; every package `ok`.

```bash
git add cmd/gophermind/brief.go \
  cmd/gophermind/brief_plan.go \
  cmd/gophermind/brief_plan_test.go \
  docs/briefv2/README.md
git commit -m "feat(brief): plan, resume, status, coverage and calls commands" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 7: Manual smoke test against the real model (decision L6, not automated)**

This is the one step that needs the Mac mini and takes real time (minutes per call). It answers the open question of spec section 16: can `qwen3.6:35b-a3b` write a usable contract. Do not run it in CI, and do not count its result as a test failure of this plan: report what happened.

```bash
go build -o /tmp/gophermind-smoke ./cmd/gophermind
mkdir -p /tmp/csvstat-smoke && sed "s|^repo: .*|repo: /tmp/csvstat-smoke|" gophermind-osx/examples/briefs/04-csvstat.md > /tmp/csvstat-brief.md
GOPHERMIND_CONFIG_DIR=/tmp/gm-smoke-config /tmp/gophermind-smoke brief plan /tmp/csvstat-brief.md --gate file
```

The brief says `on_ambiguity: assume_and_document`, so no question is asked and the first stop is the approval (exit 3). Then:

```bash
GOPHERMIND_CONFIG_DIR=/tmp/gm-smoke-config /tmp/gophermind-smoke brief status gm-2026-09-29-003
GOPHERMIND_CONFIG_DIR=/tmp/gm-smoke-config /tmp/gophermind-smoke brief coverage gm-2026-09-29-003
GOPHERMIND_CONFIG_DIR=/tmp/gm-smoke-config /tmp/gophermind-smoke brief calls gm-2026-09-29-003
```

Read `/tmp/csvstat-smoke/.gophermind/gm-2026-09-29-003/APPROVAL.md` against the brief, in particular the coverage table (spec section 16: a mapping that names a real node for the wrong requirement passes every code check, so a person has to look). Report: whether the run reached approval or where it stopped and with which error; how many calls were malformed, by task type (`status` shows it); how many coverage fill rounds it took; and whether the coverage table is true. Do not approve the plan and do not resume past approval: the executor does not exist yet. Remove `/tmp/gm-smoke-config`, `/tmp/csvstat-smoke`, `/tmp/csvstat-brief.md` and `/tmp/gophermind-smoke` afterwards.
