# Filesystem Run State Implementation Plan (sub-project A)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the SQLite blackboard, ledger and event store of the v2 planner and executor with browsable files inside the run folder, and save scrubbed attempt artifacts there.

**Architecture:** Two new filesystem implementations sit behind the existing `blackboard.Blackboard` and `ledger.Ledger` interfaces, built on one small package of file primitives (`runfs`: atomic write, exclusive-create lock, append-only lines). Each node's runtime state is one `*.runtime.json` file beside its plan file (the name `tree.Store.Load` already skips); events and calls are append-only JSON-lines files in `_state/`. A new `artifacts` package writes the scrubbed reply and check output of each attempt under `attempts/`. When everything is wired, the SQLite code and the `db` package are deleted.

**Tech Stack:** Go (module `gophermind/gophermind-lib`), standard library only for the new code; existing packages `tree`, `packer`, `settings`, `pathsafe`.

**Spec:** `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md`, section 4 and decisions 3, 7 and 11. Evidence: `docs/reviews/2026-10-07-planner-executor-v1-v2-review.md` (sections 4 to 6).

## Global Constraints

- **Do not start this plan until the GOAL.md graded run has finished** (unification spec, decision 11). Nothing here may change the planner, the executor, the node schema or the run-state format while that run is in progress.
- All code work happens in the linked worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core` on branch `feat/briefv2-planner-core`, one writer at a time. The main checkout holds docs only. Never run `git worktree add`, `git checkout`, `git stash` or `--gw-force` (the user's git wrapper blocks them).
- Paths below are relative to the worktree root unless they start with `docs/` in the main checkout (say so explicitly).
- Run tests with a throwaway home so no test can touch `~/.gophermind`. Define once per shell: `gt() { HOME="$(mktemp -d)" GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)" go test "$@"; }`. The executor package takes about 11 minutes: use `-timeout 30m`.
- Any test that shells out to `git` must strip every `GIT_*` variable first (a past incident wrote into the repo's `.git/config`).
- State files are mode 0600 and directories 0700.
- Replies, prompts and secret values: prompt text is never stored; a declared secret value never appears in any file the new code writes (canary tests prove it).
- No em dashes and no emojis in new text, code comments or messages.
- Never run a recursive delete on a variable path without the guard: assign the target, check it is absolute, non-empty and inside the intended folder, print it, then `rm -rf -- "${target:?}"`. Prefer `os.Remove` of single files.
- Each commit message ends with these two lines, after a blank line:
  `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01RnQ7xtbotonNCnc7FGkznN`
- The interface contracts in `blackboard/blackboard.go` (transition table, error values, semantics of `Claim`, `Release`, `SetStatus`, `ReleaseStale`, `Watch`) do not change. The one interface change is `Ledger.Amend` gaining a `runID` argument (Task 3).

## Deviations from the spec (decided while reading the code)

1. **No `.claim` file.** The spec's section 4.2 lists a `<fn-id>.claim` file. The claim lives inside the node's `.runtime.json`, and a transient `<node>.runtime.json.lock` file (exclusive create) makes the read-modify-write atomic. One file per node is easier to browse and there is nothing to keep in sync. The unification spec is amended in Task 9.
2. **Sidecar name is `.runtime.json`, not `.state.json`.** `tree.Store.Load` already skips `*.runtime.json` and the handoff (`BUILD_PLAN.md`, `examples/runtime/`) names it.
3. **`ResetRun` is not needed.** State now lives in the run folder, and a reused run id gets a fresh folder (`rundir.Create` refuses an existing one; an untouched one is removed first). Task 7 deletes the `ResetRun` hook.
4. **Run lookup.** The file backends find a run's folder through a resolver function. The commands pass `planner.LookupRun` (the run record the planner writes at Load). Tests pass `runfs.Fixed(dir)`.

## Review Focus

1. A crash between writing a temporary file and the rename, or while a lock is held: the old state is intact and a stale lock is reclaimed (Tasks 1 to 3).
2. Two processes on one run, for example `brief status` while `brief run` is running: reads never see a torn file and never write (Task 2, `TestFSReadsDoNotWrite`, `TestFSNeverReadsATornFile`).
3. A declared secret value in a model reply or compiler output never reaches `attempts/`, including one that straddles the truncation point (Tasks 4 and 5).
4. A node id or run id from outside that tries to leave the run folder (`../x`, an absolute path) is refused (Tasks 2 and 4).
5. A reused run id after its folder was deleted starts clean, and a leftover `~/.gophermind/blackboard.db` is ignored, not read (Task 7).
6. Heartbeat writes inside `.gophermind/<run>/` during a leaf's check are not mistaken for stray files (Task 5).

## File Structure

```text
gophermind-lib/briefv2/
  runfs/        runfs.go, runfs_test.go                  NEW  atomic write, lock, append line, read lines, timestamps, Fixed (Task 1)
  blackboard/   fs.go, fs_test.go                        NEW  FS implementation; fs_test.go replaces sqlite_test.go (Task 2)
                sqlite.go, sqlite_test.go                DELETED in Task 8 (sqlite_test.go is moved in Task 2)
  ledger/       fs.go, fs_test.go                        NEW  FS implementation; fs_test.go replaces ledger_test.go (Task 3)
                ledger.go                                MODIFIED  Amend gets runID; SQLite removed in Task 8
  artifacts/    artifacts.go, artifacts_test.go          NEW  attempt artifact writer (Task 4)
  settings/     executor.go, executor_test.go            MODIFIED  artifacts, artifact_max_bytes (Task 4)
  tree/         store.go, store_test.go                  MODIFIED  Load skips attempts/ (Task 4)
  executor/     start.go, leaf.go, artifacts_test.go     MODIFIED / NEW  save artifacts from record() (Task 5)
  planner/      planner.go, replies.go, replies_test.go  MODIFIED / NEW  save stage replies (Task 6)
  router/       parsed.go                                MODIFIED  markMalformed passes the run id (Task 3)
  db/                                                    DELETED in Task 8
cmd/gophermind/ brief_state.go                           NEW  resolver and backend constructors (Task 7)
                brief_plan.go, brief_run.go              MODIFIED  stop opening the database (Task 7)
docs/briefv2/README.md                                   MODIFIED  state layout and settings (Task 9)
```

Test harnesses that open the database today and are converted in Task 7: `planner/harness_test.go`, `executor/rig_test.go`, `executor/options_test.go`, `packer/packer_test.go`, `router/router_test.go`.

---

### Task 1: The `runfs` primitives

**Files:**
- Create: `gophermind-lib/briefv2/runfs/runfs.go`
- Test: `gophermind-lib/briefv2/runfs/runfs_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (later tasks rely on these exact names):
  - `const TimeFormat = "2006-01-02T15:04:05.000000000Z"`
  - `func TS(t time.Time) string`, `func ParseTS(s string) (time.Time, error)`
  - `func Fixed(dir string) func(runID string) (string, error)`
  - `func WriteFileAtomic(path string, data []byte) error`
  - `var ErrLockTimeout error`, `func Lock(path string, wait, stale time.Duration) (unlock func(), err error)`
  - `func AppendLine(path string, line []byte) error`
  - `func ReadLines(path string, off int64) (lines [][]byte, next int64, err error)`
  - `func Size(path string) int64`

- [ ] **Step 1: Write the failing tests**

Create `gophermind-lib/briefv2/runfs/runfs_test.go`:

```go
package runfs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
)

func TestWriteFileAtomicReplacesTheWholeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "a.json")
	if err := runfs.WriteFileAtomic(p, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := runfs.WriteFileAtomic(p, []byte("second")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "second" {
		t.Fatalf("read = %q, %v", b, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(p))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700", di.Mode().Perm())
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Errorf("left %d entries beside the file, want only the file", len(ents))
	}
}

func TestWriteFileAtomicLeavesNoTempFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "adir")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runfs.WriteFileAtomic(target, []byte("x")); err == nil {
		t.Fatal("writing over a directory must fail")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "adir" {
		t.Errorf("entries after the failed write: %v", ents)
	}
}

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "n.lock")
	counter := filepath.Join(dir, "count")
	if err := os.WriteFile(counter, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := runfs.Lock(lock, 10*time.Second, time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			defer unlock()
			b, _ := os.ReadFile(counter)
			n, _ := strconv.Atoi(string(b))
			_ = os.WriteFile(counter, []byte(strconv.Itoa(n+1)), 0o600)
		}()
	}
	wg.Wait()
	b, _ := os.ReadFile(counter)
	if string(b) != "20" {
		t.Errorf("counter = %s, want 20: the lock let two holders in", b)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after every unlock: %v", err)
	}
}

func TestLockTimesOutWhileHeld(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "n.lock")
	unlock, err := runfs.Lock(lock, time.Second, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = runfs.Lock(lock, 50*time.Millisecond, time.Hour)
	if err != runfs.ErrLockTimeout {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

func TestLockReclaimsAStaleLock(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "n.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := runfs.Lock(lock, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("a lock older than stale must be reclaimed: %v", err)
	}
	unlock()
}

func TestAppendLineKeepsConcurrentLinesIntact(t *testing.T) {
	p := filepath.Join(t.TempDir(), "_state", "events.jsonl")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, _ := json.Marshal(map[string]any{"n": i, "pad": strings.Repeat("x", 200)})
			if err := runfs.AppendLine(p, b); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	lines, _, err := runfs.ReadLines(p, 0)
	if err != nil || len(lines) != 100 {
		t.Fatalf("lines = %d, %v; want 100", len(lines), err)
	}
	seen := map[int]bool{}
	for _, ln := range lines {
		var v struct{ N int }
		if err := json.Unmarshal(ln, &v); err != nil {
			t.Fatalf("a line is not intact JSON: %q", ln)
		}
		seen[v.N] = true
	}
	if len(seen) != 100 {
		t.Errorf("distinct lines = %d, want 100", len(seen))
	}
}

func TestAppendLineRefusesANewline(t *testing.T) {
	if err := runfs.AppendLine(filepath.Join(t.TempDir(), "x.jsonl"), []byte("a\nb")); err == nil {
		t.Fatal("a line containing a newline must be refused")
	}
}

func TestReadLinesStopsAtAPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jsonl")
	if err := os.WriteFile(p, []byte("a\nb"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, next, err := runfs.ReadLines(p, 0)
	if err != nil || len(lines) != 1 || string(lines[0]) != "a" || next != 2 {
		t.Fatalf("lines = %q next = %d err = %v; want [a] 2", lines, next, err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("\n")
	f.Close()
	lines, next, _ = runfs.ReadLines(p, next)
	if len(lines) != 1 || string(lines[0]) != "b" || next != 4 {
		t.Fatalf("second read: lines = %q next = %d", lines, next)
	}
}

func TestReadLinesOfAMissingFile(t *testing.T) {
	lines, next, err := runfs.ReadLines(filepath.Join(t.TempDir(), "none"), 7)
	if err != nil || len(lines) != 0 || next != 7 {
		t.Fatalf("lines = %v next = %d err = %v", lines, next, err)
	}
	if got := runfs.Size(filepath.Join(t.TempDir(), "none")); got != 0 {
		t.Errorf("Size of a missing file = %d, want 0", got)
	}
}

func TestTimestampsRoundTripAndSortAsText(t *testing.T) {
	a := time.Date(2026, 10, 7, 12, 0, 0, 5, time.UTC)
	b := a.Add(time.Nanosecond)
	if runfs.TS(a) >= runfs.TS(b) {
		t.Errorf("TS must sort as text: %s %s", runfs.TS(a), runfs.TS(b))
	}
	back, err := runfs.ParseTS(runfs.TS(a))
	if err != nil || !back.Equal(a) {
		t.Errorf("round trip = %v, %v", back, err)
	}
	if z, err := runfs.ParseTS(""); err != nil || !z.IsZero() {
		t.Errorf("empty string must parse to the zero time: %v %v", z, err)
	}
}

func TestFixedResolvesEveryRunToTheSameFolder(t *testing.T) {
	r := runfs.Fixed("/some/dir")
	for _, id := range []string{"a", "gm-2026-10-07-001"} {
		got, err := r(id)
		if err != nil || got != "/some/dir" {
			t.Errorf("Fixed(%q) = %q, %v", id, got, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `gt ./gophermind-lib/briefv2/runfs/ -count=1`
Expected: FAIL with a build error ("no non-test Go files" or "undefined: runfs.WriteFileAtomic").

- [ ] **Step 3: Write the implementation**

Create `gophermind-lib/briefv2/runfs/runfs.go`:

```go
// Package runfs holds the small file primitives the filesystem run state is
// built on: an atomic write, an exclusive-create lock, an append-only line
// file and its reader. The blackboard, the call ledger and the attempt
// artifacts all use it, so there is one place to get these details right.
package runfs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
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

// Fixed returns a resolver that maps every run id to dir. Tests use it; the
// commands resolve a run id through the run record instead.
func Fixed(dir string) func(runID string) (string, error) {
	return func(string) (string, error) { return dir, nil }
}

// WriteFileAtomic writes data to path through a temporary file in the same
// folder and a rename, so a reader sees the old file or the new one and never
// half of either. The file is mode 0600 and missing folders are made 0700.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	done = true
	return nil
}

// ErrLockTimeout is returned by Lock when the lock stayed held for the whole wait.
var ErrLockTimeout = errors.New("runfs: lock wait timed out")

// Lock takes an exclusive lock by creating path with O_EXCL and returns the
// function that releases it. It retries until wait has passed. A lock file
// older than stale belongs to a holder that died and is removed. The window
// between judging a lock stale and removing it is tiny next to a stale age of
// tens of seconds, and a lock is held for milliseconds.
func Lock(path string, wait, stale time.Duration) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	delay := time.Millisecond
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, serr := os.Stat(path); serr == nil && time.Since(fi.ModTime()) > stale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, ErrLockTimeout
		}
		time.Sleep(delay)
		if delay < 20*time.Millisecond {
			delay *= 2
		}
	}
}

// AppendLine appends line and a newline to path with one write on a file opened
// O_APPEND, so concurrent appenders do not interleave. The line must not hold
// a newline itself (compact JSON never does).
func AppendLine(path string, line []byte) error {
	if bytes.IndexByte(line, '\n') >= 0 {
		return errors.New("runfs: a line must not contain a newline")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	buf := make([]byte, 0, len(line)+1)
	buf = append(append(buf, line...), '\n')
	_, werr := f.Write(buf)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// ReadLines returns the complete lines of path from byte offset off and the
// offset just after the last complete line. A partial last line (an append in
// progress) is left for the next call. A missing file has no lines.
func ReadLines(path string, off int64) (lines [][]byte, next int64, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, off, nil
	}
	if err != nil {
		return nil, off, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, off, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, off, err
	}
	pos := 0
	for {
		i := bytes.IndexByte(data[pos:], '\n')
		if i < 0 {
			break
		}
		lines = append(lines, data[pos:pos+i])
		pos += i + 1
	}
	return lines, off + int64(pos), nil
}

// Size is the byte length of path, 0 when it does not exist.
func Size(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gt ./gophermind-lib/briefv2/runfs/ -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/briefv2/runfs/runfs.go gophermind-lib/briefv2/runfs/runfs_test.go
git commit -m "feat(briefv2): runfs file primitives for filesystem run state"
```

(append the two trailer lines from Global Constraints to every commit message in this plan)

---

### Task 2: The filesystem blackboard

**Files:**
- Create: `gophermind-lib/briefv2/blackboard/fs.go`
- Move + modify: `gophermind-lib/briefv2/blackboard/sqlite_test.go` to `gophermind-lib/briefv2/blackboard/fs_test.go`

**Interfaces:**
- Consumes: `runfs` (Task 1); `tree.NewStore(runDir).Load()` and `tree.Node.Path()`; the `Blackboard` interface and types in `blackboard.go`; `transitionAllowed` and the `allowed` table (currently in `sqlite.go`, moved in this task).
- Produces:
  - `type RunDirFunc func(runID string) (string, error)`
  - `type FS struct{ ... }` with unexported fields `now func() time.Time` and `poll time.Duration` (the moved tests set them)
  - `func NewFS(resolve RunDirFunc, opts ...FSOption) *FS`
  - `type FSOption func(*FS)`, `func WithFlatLayout() FSOption`
  - `var _ Blackboard = (*FS)(nil)`
  - runtime file layout `<node dir>/<node file name without .json>.runtime.json` (flat layout: `<run>/nodes/<id>.runtime.json`); events at `<run>/_state/events.jsonl`.

- [ ] **Step 1: Move the existing tests and point them at the new type**

The existing twelve tests become the contract suite for the new backend. In the worktree:

```bash
git mv gophermind-lib/briefv2/blackboard/sqlite_test.go gophermind-lib/briefv2/blackboard/fs_test.go
```

Edit `fs_test.go`:

1. Replace the imports `"database/sql"`, `"path/filepath"` and `"gophermind/gophermind-lib/briefv2/db"` with `"gophermind/gophermind-lib/briefv2/runfs"` (keep `context`, `errors`, `fmt`, `sync`, `sync/atomic`, `testing`, `time`; delete any import the compiler then reports unused).
2. Replace `newBB` with:

```go
func newBB(t *testing.T) *FS {
	t.Helper()
	return NewFS(runfs.Fixed(t.TempDir()), WithFlatLayout())
}
```

3. Change `initReady(t *testing.T, b *SQLite, ...)` to take `b *FS`.
4. Replace every `b, _ := newBB(t)` with `b := newBB(t)`:

```bash
sed -i '' 's/b, _ := newBB(t)/b := newBB(t)/' gophermind-lib/briefv2/blackboard/fs_test.go
```

The tests that set `b.now` and `b.poll` keep working because `FS` has fields with those names. Rename the test file's SQLite-specific wording if any remains (`grep -n -i sqlite fs_test.go` should return nothing).

- [ ] **Step 2: Add the tests that only make sense for files**

Append to `fs_test.go` (add `os`, `path/filepath`, `strings`, `encoding/json` and `gophermind/gophermind-lib/briefv2/tree` to the imports):

```go
func TestFSReadsDoNotWrite(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	if _, err := b.List(ctx, "r", Filter{}); err != nil {
		t.Fatalf("List of an empty run: %v", err)
	}
	if _, err := b.Get(ctx, "r", "nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of a missing node: %v", err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Errorf("reads created %d entries in the run folder", len(ents))
	}
}

func TestFSRefusesAnUnsafeNodeID(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	for _, id := range []string{"../escape", "/abs", "a/b", "", "UPPER", ".hidden"} {
		if err := b.InitRun(ctx, "r", []string{id}, map[string]int{id: 0}); err == nil {
			t.Errorf("InitRun accepted node id %q", id)
		}
	}
	ents, _ := os.ReadDir(filepath.Dir(dir))
	for _, e := range ents {
		if e.Name() == "escape" {
			t.Error("a node id escaped the run folder")
		}
	}
}

func TestFSNeverReadsATornFile(t *testing.T) {
	b := newBB(t)
	ctx := context.Background()
	initReady(t, b, "r", "n")
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = b.AppendAttempt(ctx, "r", "n", Attempt{Model: "m", Verdict: VerdictFail, FailureReason: strings.Repeat("x", 500)})
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			wg.Wait()
			return
		default:
			if _, err := b.Get(ctx, "r", "n"); err != nil {
				t.Fatalf("a read during a write failed: %v", err)
			}
		}
	}
}

func TestFSRuntimeFileSitsBesideTheNodeFile(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	for _, raw := range []string{
		`{"spec_version":"2.0","id":"rt","kind":"root","title":"t","description":"d","brief_ref":"#x","status":"pending","children":["comp"]}`,
		`{"spec_version":"2.0","id":"comp","kind":"component","parent":"rt","title":"t","description":"d","brief_ref":"#x","status":"pending","children":["fn-a"]}`,
		`{"spec_version":"2.0","id":"fn-a","kind":"function","parent":"comp","title":"t","description":"d","brief_ref":"#x","status":"pending","wave":0,"depends_on":[],
"contract":{"package":"p","file":"p/fn-a.go","signature":"func F()","inputs":[],"outputs":[]},
"tests":[{"name":"n","level":"unit","given":"g","expect":"e","command":"go test ./p"}]}`,
	} {
		n, err := tree.ParseNode([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Write(n); err != nil {
			t.Fatal(err)
		}
	}
	b := NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	if err := b.InitRun(ctx, "run1", []string{"rt", "comp", "fn-a"}, map[string]int{"rt": 2, "comp": 1, "fn-a": 0}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"root.runtime.json", "comp/component.runtime.json", "comp/fn-a.runtime.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("expected %s beside the node file: %v", want, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "comp", "fn-a.runtime.json"))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil || doc["node_id"] != "fn-a" || doc["status"] != "pending" {
		t.Errorf("runtime file = %s (%v)", raw, err)
	}
	if _, err := b.Get(ctx, "run1", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a node that is not in the tree: err = %v, want ErrNotFound", err)
	}
	if loaded, err := s.Load(); err != nil || len(loaded.Nodes) != 3 {
		t.Errorf("Load must still see only the 3 plan nodes: %d, %v", len(loaded.Nodes), err)
	}
}

func TestFSEventsAreOneJSONLinePerChange(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	initReady(t, b, "r", "n")
	if ok, _ := b.Claim(ctx, "r", "n", "w"); !ok {
		t.Fatal("claim")
	}
	lines, _, err := runfs.ReadLines(filepath.Join(dir, "_state", "events.jsonl"), 0)
	if err != nil || len(lines) != 2 {
		t.Fatalf("event lines = %d, %v; want 2 (ready, claimed)", len(lines), err)
	}
	var e struct{ RunID, NodeID, Kind, At string }
	if err := json.Unmarshal(lines[1], &e); err != nil || e.Kind != "status" || e.NodeID != "n" {
		t.Errorf("last event = %s (%v)", lines[1], err)
	}
}

func TestFSLeavesAStaleLockBehindNothing(t *testing.T) {
	dir := t.TempDir()
	b := NewFS(runfs.Fixed(dir), WithFlatLayout())
	ctx := context.Background()
	initReady(t, b, "r", "n")
	lock := filepath.Join(dir, "nodes", "n.runtime.json.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(lock, old, old)
	if ok, err := b.Claim(ctx, "r", "n", "w"); err != nil || !ok {
		t.Fatalf("claim past a dead holder's lock: %v %v", ok, err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("lock still present after the claim: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `gt ./gophermind-lib/briefv2/blackboard/ -count=1`
Expected: FAIL with a build error (`undefined: NewFS`, `undefined: WithFlatLayout`).

- [ ] **Step 4: Write the implementation**

Create `gophermind-lib/briefv2/blackboard/fs.go`:

```go
package blackboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
	"gophermind/gophermind-lib/briefv2/tree"
)

// RunDirFunc maps a run id to the run's folder.
type RunDirFunc func(runID string) (string, error)

// FS is the blackboard over files in the run folder. Each node has one
// runtime file beside its plan file (comp/fn-a.json has comp/fn-a.runtime.json);
// the file holds the node's status, claim, attempts and result. A change reads
// the file, edits it and writes it back under an exclusive lock file, so a
// claim has exactly one winner even across processes. Every change also
// appends one line to <run>/_state/events.jsonl, which Watch follows.
type FS struct {
	resolve RunDirFunc
	flat    bool
	now     func() time.Time
	poll    time.Duration
	wait    time.Duration
	stale   time.Duration

	mu    sync.Mutex
	paths map[string]map[string]string // run folder -> node id -> runtime file
}

var _ Blackboard = (*FS)(nil)

// FSOption configures NewFS.
type FSOption func(*FS)

// WithFlatLayout keeps every runtime file under <run>/nodes/<id>.runtime.json
// instead of beside the plan file. Tests use it for node ids that have no
// plan file; production never does.
func WithFlatLayout() FSOption { return func(f *FS) { f.flat = true } }

// NewFS returns the filesystem blackboard. resolve finds a run's folder.
func NewFS(resolve RunDirFunc, opts ...FSOption) *FS {
	f := &FS{
		resolve: resolve,
		now:     func() time.Time { return time.Now().UTC() },
		poll:    250 * time.Millisecond,
		wait:    10 * time.Second,
		stale:   30 * time.Second,
		paths:   map[string]map[string]string{},
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

var nodeIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// The wire types use snake_case names, matching docs/briefv2/handoff/examples/runtime.
type fsClaim struct {
	Worker    string `json:"worker"`
	ClaimedAt string `json:"claimed_at"`
}

type fsAttempt struct {
	Model         string   `json:"model"`
	Provider      string   `json:"provider"`
	Revision      int      `json:"revision"`
	Order         int      `json:"order"`
	StartedAt     string   `json:"started_at"`
	DurationMS    int64    `json:"duration_ms"`
	Verdict       Verdict  `json:"verdict"`
	TestsPassed   int      `json:"tests_passed"`
	TestsTotal    int      `json:"tests_total"`
	FailedTests   []string `json:"failed_tests,omitempty"`
	FailureReason string   `json:"failure_reason,omitempty"`
	ReplySHA256   string   `json:"reply_sha256,omitempty"`
}

type fsResult struct {
	FilesChanged []string `json:"files_changed"`
	Commit       string   `json:"commit"`
}

type fsDoc struct {
	NodeID      string      `json:"node_id"`
	RunID       string      `json:"run_id"`
	Status      Status      `json:"status"`
	Revision    int         `json:"revision"`
	Wave        int         `json:"wave"`
	Claim       *fsClaim    `json:"claim,omitempty"`
	HeartbeatAt string      `json:"heartbeat_at,omitempty"`
	Attempts    []fsAttempt `json:"attempts"`
	Result      *fsResult   `json:"result,omitempty"`
	UpdatedAt   string      `json:"updated_at"`
}

func (d fsDoc) row() (Row, error) {
	r := Row{RunID: d.RunID, NodeID: d.NodeID, Status: d.Status, Revision: d.Revision}
	r.HeartbeatAt, _ = runfs.ParseTS(d.HeartbeatAt)
	r.UpdatedAt, _ = runfs.ParseTS(d.UpdatedAt)
	if d.Claim != nil {
		at, _ := runfs.ParseTS(d.Claim.ClaimedAt)
		r.Claim = &Claim{Worker: d.Claim.Worker, ClaimedAt: at}
	}
	for _, a := range d.Attempts {
		at, _ := runfs.ParseTS(a.StartedAt)
		r.Attempts = append(r.Attempts, Attempt{
			Model: a.Model, Provider: a.Provider, Revision: a.Revision, Order: a.Order, StartedAt: at,
			DurationMS: a.DurationMS, Verdict: a.Verdict, TestsPassed: a.TestsPassed, TestsTotal: a.TestsTotal,
			FailedTests: a.FailedTests, FailureReason: a.FailureReason, ReplySHA256: a.ReplySHA256,
		})
	}
	if d.Result != nil {
		r.Result = &Result{FilesChanged: d.Result.FilesChanged, Commit: d.Result.Commit}
	}
	return r, nil
}

func wireAttempt(a Attempt) fsAttempt {
	return fsAttempt{
		Model: a.Model, Provider: a.Provider, Revision: a.Revision, Order: a.Order, StartedAt: runfs.TS(a.StartedAt),
		DurationMS: a.DurationMS, Verdict: a.Verdict, TestsPassed: a.TestsPassed, TestsTotal: a.TestsTotal,
		FailedTests: a.FailedTests, FailureReason: a.FailureReason, ReplySHA256: a.ReplySHA256,
	}
}

func readDoc(path string) (fsDoc, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsDoc{}, ErrNotFound
	}
	if err != nil {
		return fsDoc{}, err
	}
	var d fsDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return fsDoc{}, fmt.Errorf("blackboard: damaged runtime file %s", filepath.Base(path))
	}
	return d, nil
}

func writeDoc(path string, d fsDoc) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return runfs.WriteFileAtomic(path, append(b, '\n'))
}

// runtimePath finds the run folder and the node's runtime file. In the tree
// layout the location comes from the plan files: a node stored at Path() has
// its runtime file at Path() with .json replaced by .runtime.json.
func (f *FS) runtimePath(runID, nodeID string) (runDir, path string, err error) {
	if !nodeIDRE.MatchString(nodeID) {
		return "", "", ErrNotFound
	}
	runDir, err = f.resolve(runID)
	if err != nil {
		return "", "", err
	}
	if f.flat {
		return runDir, filepath.Join(runDir, "nodes", nodeID+".runtime.json"), nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.paths[runDir][nodeID]; ok {
		return runDir, p, nil
	}
	t, err := tree.NewStore(runDir).Load()
	if err != nil {
		return "", "", fmt.Errorf("blackboard: reading the plan of run %s: %w", runID, err)
	}
	m := make(map[string]string, len(t.Nodes))
	for id, n := range t.Nodes {
		m[id] = filepath.Join(runDir, filepath.FromSlash(strings.TrimSuffix(n.Path(), ".json")+".runtime.json"))
	}
	f.paths[runDir] = m
	if p, ok := m[nodeID]; ok {
		return runDir, p, nil
	}
	return "", "", ErrNotFound
}

// mutate runs fn on the node's document under the node's lock. fn returns
// whether to write the document back and which event to record, if any.
func (f *FS) mutate(ctx context.Context, runID, nodeID string, fn func(d *fsDoc) (write bool, ev EventKind, err error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runDir, path, err := f.runtimePath(runID, nodeID)
	if err != nil {
		return err
	}
	unlock, err := runfs.Lock(path+".lock", f.wait, f.stale)
	if err != nil {
		return err
	}
	defer unlock()
	d, err := readDoc(path)
	if err != nil {
		return err
	}
	write, ev, err := fn(&d)
	if err != nil || !write {
		return err
	}
	d.UpdatedAt = runfs.TS(f.now())
	if err := writeDoc(path, d); err != nil {
		return err
	}
	if ev != "" {
		f.emit(runDir, ev, runID, nodeID)
	}
	return nil
}

type eventLine struct {
	RunID  string `json:"run_id"`
	NodeID string `json:"node_id"`
	Kind   string `json:"kind"`
	At     string `json:"at"`
}

// emit records a change. A lost notification must never fail the write it describes.
func (f *FS) emit(runDir string, kind EventKind, runID, nodeID string) {
	b, err := json.Marshal(eventLine{RunID: runID, NodeID: nodeID, Kind: string(kind), At: runfs.TS(f.now())})
	if err != nil {
		return
	}
	_ = runfs.AppendLine(filepath.Join(runDir, "_state", "events.jsonl"), b)
}

func (f *FS) InitRun(ctx context.Context, runID string, nodeIDs []string, waves map[string]int) error {
	for _, id := range nodeIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, path, err := f.runtimePath(runID, id)
		if err != nil {
			return err
		}
		unlock, err := runfs.Lock(path+".lock", f.wait, f.stale)
		if err != nil {
			return err
		}
		if _, serr := os.Stat(path); serr == nil {
			unlock()
			continue
		}
		werr := writeDoc(path, fsDoc{
			NodeID: id, RunID: runID, Status: StatusPending, Wave: waves[id],
			Attempts: []fsAttempt{}, UpdatedAt: runfs.TS(f.now()),
		})
		unlock()
		if werr != nil {
			return werr
		}
	}
	return nil
}

func (f *FS) Get(ctx context.Context, runID, nodeID string) (Row, error) {
	if err := ctx.Err(); err != nil {
		return Row{}, err
	}
	_, path, err := f.runtimePath(runID, nodeID)
	if err != nil {
		return Row{}, err
	}
	d, err := readDoc(path)
	if err != nil {
		return Row{}, err
	}
	return d.row()
}

func (f *FS) List(ctx context.Context, runID string, flt Filter) ([]Row, error) {
	runDir, err := f.resolve(runID)
	if err != nil {
		return nil, err
	}
	var rows []Row
	err = filepath.WalkDir(runDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			switch d.Name() {
			case "_state", "attempts", "logs":
				if p != runDir {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".runtime.json") {
			return nil
		}
		doc, err := readDoc(p)
		if err != nil {
			return err
		}
		if doc.RunID != runID {
			return nil
		}
		if len(flt.Statuses) > 0 {
			match := false
			for _, s := range flt.Statuses {
				if s == doc.Status {
					match = true
				}
			}
			if !match {
				return nil
			}
		}
		if flt.Wave != nil && doc.Wave != *flt.Wave {
			return nil
		}
		r, err := doc.row()
		if err != nil {
			return err
		}
		rows = append(rows, r)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].NodeID < rows[j].NodeID })
	return rows, nil
}

func (f *FS) Claim(ctx context.Context, runID, nodeID, worker string) (bool, error) {
	won := false
	err := f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if d.Status != StatusReady {
			return false, "", nil
		}
		now := runfs.TS(f.now())
		d.Status = StatusClaimed
		d.Claim = &fsClaim{Worker: worker, ClaimedAt: now}
		d.HeartbeatAt = now
		won = true
		return true, EventStatus, nil
	})
	if errors.Is(err, ErrNotFound) {
		return false, nil // a lost race and a missing row both read as "not yours"
	}
	return won, err
}

func owns(d *fsDoc, worker string) bool {
	return d.Claim != nil && d.Claim.Worker == worker && (d.Status == StatusClaimed || d.Status == StatusInProgress)
}

func (f *FS) Heartbeat(ctx context.Context, runID, nodeID, worker string) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if !owns(d, worker) {
			return false, "", ErrNotOwner
		}
		d.HeartbeatAt = runfs.TS(f.now())
		return true, "", nil
	})
}

func (f *FS) Release(ctx context.Context, runID, nodeID, worker string) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if !owns(d, worker) {
			return false, "", ErrNotOwner
		}
		d.Status, d.Claim, d.HeartbeatAt = StatusReady, nil, ""
		return true, EventStatus, nil
	})
}

func (f *FS) SetStatus(ctx context.Context, runID, nodeID string, from, to Status) error {
	if !transitionAllowed(from, to) {
		return ErrBadTransition
	}
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		if d.Status != from {
			return false, "", ErrBadTransition
		}
		d.Status = to
		if to != StatusClaimed && to != StatusInProgress {
			d.Claim, d.HeartbeatAt = nil, ""
		}
		return true, EventStatus, nil
	})
}

func (f *FS) SetRevision(ctx context.Context, runID, nodeID string, revision int) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		d.Revision = revision
		return true, "", nil
	})
}

func (f *FS) AppendAttempt(ctx context.Context, runID, nodeID string, a Attempt) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		d.Attempts = append(d.Attempts, wireAttempt(a))
		return true, EventAttempt, nil
	})
}

func (f *FS) SetResult(ctx context.Context, runID, nodeID string, r Result) error {
	return f.mutate(ctx, runID, nodeID, func(d *fsDoc) (bool, EventKind, error) {
		d.Result = &fsResult{FilesChanged: r.FilesChanged, Commit: r.Commit}
		return true, EventResult, nil
	})
}

func (f *FS) ReleaseStale(ctx context.Context, runID string, olderThan time.Duration) ([]string, error) {
	cutoff := f.now().Add(-olderThan)
	held, err := f.List(ctx, runID, Filter{Statuses: []Status{StatusClaimed, StatusInProgress}})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range held {
		if !r.HeartbeatAt.Before(cutoff) {
			continue
		}
		released := false
		err := f.mutate(ctx, runID, r.NodeID, func(d *fsDoc) (bool, EventKind, error) {
			hb, _ := runfs.ParseTS(d.HeartbeatAt)
			if (d.Status != StatusClaimed && d.Status != StatusInProgress) || !hb.Before(cutoff) {
				return false, "", nil // changed since we listed it
			}
			d.Status, d.Claim, d.HeartbeatAt = StatusReady, nil, ""
			released = true
			return true, EventStatus, nil
		})
		if err != nil {
			return nil, err
		}
		if released {
			ids = append(ids, r.NodeID)
		}
	}
	return ids, nil
}

func (f *FS) Watch(ctx context.Context, runID string) (<-chan Event, error) {
	runDir, err := f.resolve(runID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(runDir, "_state", "events.jsonl")
	off := runfs.Size(path)
	ch := make(chan Event)
	go func() {
		defer close(ch)
		tick := time.NewTicker(f.poll)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			lines, next, err := runfs.ReadLines(path, off)
			if err != nil {
				continue // transient; try again on the next tick
			}
			off = next
			for _, ln := range lines {
				var e eventLine
				if json.Unmarshal(ln, &e) != nil || e.RunID != runID {
					continue
				}
				row, err := f.Get(ctx, runID, e.NodeID)
				if err != nil {
					continue
				}
				at, _ := runfs.ParseTS(e.At)
				select {
				case ch <- Event{Kind: EventKind(e.Kind), Row: row, At: at}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func (f *FS) Close() error { return nil }
```

`transitionAllowed` and the `allowed` table are still defined in `sqlite.go` and are used by both backends until Task 8 deletes that file; Task 8 moves them into a new `transitions.go`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gt ./gophermind-lib/briefv2/blackboard/ -count=1 -race`
Expected: PASS (the twelve moved tests plus the six new ones). If `TestClaimRaceHasExactlyOneWinner` is slow, that is the lock retry delay; it must still pass.

- [ ] **Step 6: Commit**

```bash
git add gophermind-lib/briefv2/blackboard/fs.go gophermind-lib/briefv2/blackboard/fs_test.go
git commit -m "feat(briefv2): filesystem blackboard (runtime files beside the nodes, events.jsonl)"
```

Note: `sqlite_test.go` no longer exists after the move, so the SQLite blackboard has no tests from here until Task 8 deletes it. That is intended.

---

### Task 3: The filesystem ledger and the `Amend` signature

**Files:**
- Create: `gophermind-lib/briefv2/ledger/fs.go`
- Move + modify: `gophermind-lib/briefv2/ledger/ledger_test.go` to `gophermind-lib/briefv2/ledger/fs_test.go`
- Modify: `gophermind-lib/briefv2/ledger/ledger.go` (Amend gets a `runID`), `gophermind-lib/briefv2/router/parsed.go`

**Interfaces:**
- Consumes: `runfs` (Task 1); `Call`, `Outcome`, `Filter`, `ModelSummary`, `Digest`, the `Ledger` interface.
- Produces:
  - `Ledger.Amend(ctx context.Context, runID string, id int64, o Outcome, errorKind string) error` (changed signature)
  - `type RunDirFunc func(runID string) (string, error)`
  - `type FS struct{ ... }`, `func NewFS(resolve RunDirFunc) *FS`, `var _ Ledger = (*FS)(nil)`
  - files `<run>/_state/calls.jsonl` (append-only; `kind` is `call` or `amend`), `<run>/_state/calls.seq` (last id), `<run>/_state/calls.lock`.

- [ ] **Step 1: Change `Amend` to take the run id**

In `ledger/ledger.go` change the interface line and the SQLite method (the SQLite method is deleted in Task 8, but must compile until then):

```go
	Amend(ctx context.Context, runID string, id int64, o Outcome, errorKind string) error
```

```go
func (s *SQLite) Amend(ctx context.Context, runID string, id int64, o Outcome, errorKind string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE calls SET outcome = ?, error_kind = ? WHERE id = ? AND run_id = ?`, string(o), errorKind, id, runID)
```

(only the signature and the `WHERE` clause change; the rest of the body is untouched).

In `router/parsed.go` change `markMalformed` to receive the run id and update both callers:

```go
func (r *Router) markMalformed(ctx context.Context, runID string, res Result) {
	if r.led == nil || res.CallID == 0 {
		return
	}
	// The error kind is a fixed word: a parser error can quote the reply, and reply text is never stored.
	if err := r.led.Amend(context.WithoutCancel(ctx), runID, res.CallID, ledger.OutcomeMalformed, "malformed"); err != nil {
		r.noteLedgerError("", "", err)
	}
}
```

Run `grep -n 'markMalformed(' gophermind-lib/briefv2/router/*.go` and change each call `r.markMalformed(ctx, resX)` to `r.markMalformed(ctx, info.RunID, resX)` (the `CallInfo` parameter in `CallParsed` is named `info`; read the function to confirm).

- [ ] **Step 2: Move the existing tests to the new backend**

```bash
git mv gophermind-lib/briefv2/ledger/ledger_test.go gophermind-lib/briefv2/ledger/fs_test.go
```

Edit `fs_test.go` (`package ledger_test`): replace the `db` import with `"gophermind/gophermind-lib/briefv2/runfs"`, and replace the helper that opens the database (it returns `ledger.NewSQLite(d), d, path`) with:

```go
func newLedger(t *testing.T) (*ledger.FS, string) {
	t.Helper()
	dir := t.TempDir()
	return ledger.NewFS(runfs.Fixed(dir)), dir
}
```

Update each test's call sites for the new return values (read the old helper's name at the top of the file and apply the same rename everywhere; the tests that used the raw `*sql.DB` or the database path to inspect storage are adapted in the next step). Change the two `Amend` calls in `TestAmendChangesTheOutcome`:

```go
	if err := l.Amend(ctx, c.RunID, c.ID, ledger.OutcomeMalformed, "component x is not a feature"); err != nil {
```
```go
	if err := l.Amend(ctx, "run-x", 9999, ledger.OutcomeError, ""); err == nil {
```

(use the run id the test's rows were recorded under; `run-x` is a placeholder only for a run with no such call and must produce an error).

Rewrite `TestNoPromptOrReplyTextIsEverStored` (it reads the stored database for the canary strings) to read `<dir>/_state/calls.jsonl` and assert the canary prompt and reply text are absent while the SHA-256 and sizes are present.

- [ ] **Step 3: Add the tests that only make sense for files**

Append to `fs_test.go`:

```go
func TestFSIDsAreSequentialAndSurviveAReopen(t *testing.T) {
	dir := t.TempDir()
	l := ledger.NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	mk := func() *ledger.Call {
		return &ledger.Call{RunID: "r", Stage: "clarify", Provider: "mini", Outcome: ledger.OutcomeOK, TaskType: "clarify"}
	}
	a, b := mk(), mk()
	if err := l.Record(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := l.Record(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("ids = %d, %d; want 1, 2", a.ID, b.ID)
	}
	c := mk()
	if err := ledger.NewFS(runfs.Fixed(dir)).Record(ctx, c); err != nil || c.ID != 3 {
		t.Fatalf("after a reopen: id = %d, %v; want 3", c.ID, err)
	}
}

func TestFSConcurrentRecordsGetDistinctIDs(t *testing.T) {
	l := ledger.NewFS(runfs.Fixed(t.TempDir()))
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make(chan int64, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &ledger.Call{RunID: "r", Stage: "s", Provider: "p", Outcome: ledger.OutcomeOK}
			if err := l.Record(ctx, c); err != nil {
				t.Error(err)
				return
			}
			ids <- c.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[int64]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("id %d handed out twice", id)
		}
		seen[id] = true
	}
	rows, err := l.List(ctx, "r", ledger.Filter{})
	if err != nil || len(rows) != 50 {
		t.Fatalf("List = %d rows, %v; want 50", len(rows), err)
	}
}

func TestFSAmendIsAFoldedLineNotARewrite(t *testing.T) {
	dir := t.TempDir()
	l := ledger.NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	c := &ledger.Call{RunID: "r", Stage: "s", Provider: "p", Outcome: ledger.OutcomeOK}
	if err := l.Record(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := l.Amend(ctx, "r", c.ID, ledger.OutcomeMalformed, "malformed"); err != nil {
		t.Fatal(err)
	}
	rows, _ := l.List(ctx, "r", ledger.Filter{})
	if len(rows) != 1 || rows[0].Outcome != ledger.OutcomeMalformed || rows[0].ErrorKind != "malformed" {
		t.Fatalf("rows = %+v", rows)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "_state", "calls.jsonl"))
	if n := strings.Count(string(raw), "\n"); n != 2 {
		t.Errorf("calls.jsonl has %d lines, want 2 (the call and its amend): the file is append-only", n)
	}
	if err := l.Amend(ctx, "r", 999, ledger.OutcomeError, ""); err == nil {
		t.Error("amending a call that does not exist must fail")
	}
}

func TestFSReadsDoNotWrite(t *testing.T) {
	dir := t.TempDir()
	l := ledger.NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	if rows, err := l.List(ctx, "r", ledger.Filter{}); err != nil || len(rows) != 0 {
		t.Fatalf("List = %v, %v", rows, err)
	}
	if sum, err := l.Summary(ctx, "r"); err != nil || len(sum) != 0 {
		t.Fatalf("Summary = %v, %v", sum, err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("reads created %d entries", len(ents))
	}
}
```

(imports: `context`, `os`, `path/filepath`, `strings`, `sync`, `testing`, the `ledger` and `runfs` packages).

- [ ] **Step 4: Run the tests to verify they fail**

Run: `gt ./gophermind-lib/briefv2/ledger/ -count=1`
Expected: FAIL with a build error (`undefined: ledger.NewFS`).

- [ ] **Step 5: Write the implementation**

Create `gophermind-lib/briefv2/ledger/fs.go`:

```go
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// RunDirFunc maps a run id to the run's folder.
type RunDirFunc func(runID string) (string, error)

// FS is the ledger over append-only files in the run folder:
// _state/calls.jsonl holds one JSON line per call and one per amend (an amend
// changes the outcome of an earlier call by id and is folded in when the file
// is read, so no line is ever rewritten); _state/calls.seq holds the last id
// handed out; _state/calls.lock serialises id assignment across processes.
type FS struct {
	resolve RunDirFunc
	wait    time.Duration
	stale   time.Duration
}

var _ Ledger = (*FS)(nil)

// NewFS returns the filesystem ledger. resolve finds a run's folder.
func NewFS(resolve RunDirFunc) *FS {
	return &FS{resolve: resolve, wait: 10 * time.Second, stale: 30 * time.Second}
}

// wire is one line of calls.jsonl. Kind "amend" lines carry only ID, Outcome
// and ErrorKind.
type wire struct {
	Kind             string `json:"kind"`
	ID               int64  `json:"id"`
	RunID            string `json:"run_id,omitempty"`
	At               string `json:"at,omitempty"`
	Stage            string `json:"stage,omitempty"`
	TaskType         string `json:"task_type,omitempty"`
	NodeClass        string `json:"node_class,omitempty"`
	NodeID           string `json:"node_id,omitempty"`
	Revision         int    `json:"revision,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Tier             string `json:"tier,omitempty"`
	ChainPos         int    `json:"chain_pos,omitempty"`
	Provider         string `json:"provider,omitempty"`
	ModelRequested   string `json:"model_requested,omitempty"`
	ModelServed      string `json:"model_served,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	PromptBytes      int    `json:"prompt_bytes,omitempty"`
	PromptSHA256     string `json:"prompt_sha256,omitempty"`
	ResponseBytes    int    `json:"response_bytes,omitempty"`
	ResponseSHA256   string `json:"response_sha256,omitempty"`
	DurationMS       int64  `json:"duration_ms,omitempty"`
	Outcome          string `json:"outcome"`
	ErrorKind        string `json:"error_kind,omitempty"`
	RetryAfterS      int    `json:"retry_after_s,omitempty"`
}

func toWire(c Call) wire {
	return wire{
		Kind: "call", ID: c.ID, RunID: c.RunID, At: runfs.TS(c.At), Stage: c.Stage, TaskType: c.TaskType, NodeClass: c.NodeClass,
		NodeID: c.NodeID, Revision: c.Revision, Scope: c.Scope, Tier: c.Tier, ChainPos: c.ChainPos, Provider: c.Provider,
		ModelRequested: c.ModelRequested, ModelServed: c.ModelServed, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens,
		PromptBytes: c.PromptBytes, PromptSHA256: c.PromptSHA256, ResponseBytes: c.ResponseBytes, ResponseSHA256: c.ResponseSHA256,
		DurationMS: c.DurationMS, Outcome: string(c.Outcome), ErrorKind: c.ErrorKind, RetryAfterS: c.RetryAfterS,
	}
}

func (w wire) call() Call {
	at, _ := runfs.ParseTS(w.At)
	return Call{
		ID: w.ID, RunID: w.RunID, At: at, Stage: w.Stage, TaskType: w.TaskType, NodeClass: w.NodeClass, NodeID: w.NodeID,
		Revision: w.Revision, Scope: w.Scope, Tier: w.Tier, ChainPos: w.ChainPos, Provider: w.Provider,
		ModelRequested: w.ModelRequested, ModelServed: w.ModelServed, PromptTokens: w.PromptTokens, CompletionTokens: w.CompletionTokens,
		PromptBytes: w.PromptBytes, PromptSHA256: w.PromptSHA256, ResponseBytes: w.ResponseBytes, ResponseSHA256: w.ResponseSHA256,
		DurationMS: w.DurationMS, Outcome: Outcome(w.Outcome), ErrorKind: w.ErrorKind, RetryAfterS: w.RetryAfterS,
	}
}

func (f *FS) stateDir(runID string) (string, error) {
	dir, err := f.resolve(runID)
	if err != nil {
		return "", fmt.Errorf("ledger: %w", err)
	}
	return filepath.Join(dir, "_state"), nil
}

// nextID reads, bumps and stores the sequence. The sequence is written before
// the call line is appended: a crash between the two leaves a gap in the ids,
// never a duplicate.
func nextID(stateDir string) (int64, error) {
	p := filepath.Join(stateDir, "calls.seq")
	var last int64
	if b, err := os.ReadFile(p); err == nil {
		last, _ = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	last++
	if err := runfs.WriteFileAtomic(p, []byte(strconv.FormatInt(last, 10)+"\n")); err != nil {
		return 0, err
	}
	return last, nil
}

func (f *FS) Record(ctx context.Context, c *Call) error {
	if c.RunID == "" || c.Stage == "" || c.Provider == "" || c.Outcome == "" {
		return errors.New("ledger: run, stage, provider and outcome are required")
	}
	if c.At.IsZero() {
		c.At = time.Now()
	}
	dir, err := f.stateDir(c.RunID)
	if err != nil {
		return err
	}
	unlock, err := runfs.Lock(filepath.Join(dir, "calls.lock"), f.wait, f.stale)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	defer unlock()
	id, err := nextID(dir)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	c.ID = id
	line, err := json.Marshal(toWire(*c))
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if err := runfs.AppendLine(filepath.Join(dir, "calls.jsonl"), line); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	return nil
}

func (f *FS) Amend(ctx context.Context, runID string, id int64, o Outcome, errorKind string) error {
	dir, err := f.stateDir(runID)
	if err != nil {
		return err
	}
	unlock, err := runfs.Lock(filepath.Join(dir, "calls.lock"), f.wait, f.stale)
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	defer unlock()
	calls, err := f.readAll(dir)
	if err != nil {
		return err
	}
	found := false
	for _, c := range calls {
		if c.ID == id {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("ledger: no call with id %d", id)
	}
	line, err := json.Marshal(wire{Kind: "amend", ID: id, Outcome: string(o), ErrorKind: errorKind})
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if err := runfs.AppendLine(filepath.Join(dir, "calls.jsonl"), line); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	return nil
}

// readAll returns every call of the run with its amends folded in, in file order.
func (f *FS) readAll(stateDir string) ([]Call, error) {
	lines, _, err := runfs.ReadLines(filepath.Join(stateDir, "calls.jsonl"), 0)
	if err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	var calls []Call
	index := map[int64]int{}
	for _, ln := range lines {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var w wire
		if err := json.Unmarshal(ln, &w); err != nil {
			return nil, errors.New("ledger: calls.jsonl holds a line that is not JSON")
		}
		switch w.Kind {
		case "call":
			index[w.ID] = len(calls)
			calls = append(calls, w.call())
		case "amend":
			if i, ok := index[w.ID]; ok {
				calls[i].Outcome, calls[i].ErrorKind = Outcome(w.Outcome), w.ErrorKind
			}
		}
	}
	return calls, nil
}

func (f *FS) List(ctx context.Context, runID string, flt Filter) ([]Call, error) {
	dir, err := f.stateDir(runID)
	if err != nil {
		return nil, err
	}
	all, err := f.readAll(dir)
	if err != nil {
		return nil, err
	}
	var out []Call
	for _, c := range all {
		if c.RunID != runID ||
			(flt.Stage != "" && c.Stage != flt.Stage) ||
			(flt.TaskType != "" && c.TaskType != flt.TaskType) ||
			(flt.NodeID != "" && c.NodeID != flt.NodeID) ||
			(flt.Provider != "" && c.Provider != flt.Provider) ||
			(flt.Outcome != "" && c.Outcome != flt.Outcome) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *FS) Summary(ctx context.Context, runID string) ([]ModelSummary, error) {
	calls, err := f.List(ctx, runID, Filter{})
	if err != nil {
		return nil, err
	}
	byKey := map[string]*ModelSummary{}
	for _, c := range calls {
		model := c.ModelServed
		if model == "" {
			model = c.ModelRequested
		}
		k := c.TaskType + "\x00" + c.NodeClass + "\x00" + c.Provider + "\x00" + model
		m := byKey[k]
		if m == nil {
			m = &ModelSummary{TaskType: c.TaskType, NodeClass: c.NodeClass, Provider: c.Provider, Model: model, Outcomes: map[Outcome]int{}}
			byKey[k] = m
		}
		m.Calls++
		m.PromptTokens += int64(c.PromptTokens)
		m.CompletionTokens += int64(c.CompletionTokens)
		m.TotalMS += c.DurationMS
		m.Outcomes[c.Outcome]++
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

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gt ./gophermind-lib/briefv2/ledger/ ./gophermind-lib/briefv2/router/ -count=1 -race`
Expected: PASS. The router tests still use `ledger.NewSQLite` until Task 7; they must still compile with the changed `Amend`.

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/briefv2/ledger gophermind-lib/briefv2/router/parsed.go
git commit -m "feat(briefv2): filesystem ledger (calls.jsonl), Ledger.Amend takes the run id"
```

---

### Task 4: Settings and the artifacts writer

**Files:**
- Create: `gophermind-lib/briefv2/artifacts/artifacts.go`, `gophermind-lib/briefv2/artifacts/artifacts_test.go`
- Modify: `gophermind-lib/briefv2/settings/executor.go`, `gophermind-lib/briefv2/settings/executor_test.go`, `gophermind-lib/briefv2/tree/store.go`, `gophermind-lib/briefv2/tree/store_test.go`

**Interfaces:**
- Consumes: `runfs.WriteFileAtomic`.
- Produces:
  - settings: `ExecutorConfig.Artifacts string` (yaml `artifacts`, `"on"` or `"off"`, default `"on"`) and `ExecutorConfig.ArtifactMaxBytes int` (yaml `artifact_max_bytes`, default 65536, range 1 to 16777216)
  - `type Meta struct{ Provider, Model string; Revision, Order int; Verdict, Class, Reason, ReplySHA256 string; StartedAt time.Time; DurationMS int64 }`
  - `type Files struct{ Source []byte; Raw string; CheckOutput string }`
  - `type Writer struct{ ... }`, `func New(runDir string, on bool, maxBytes int, scrub func(string) string) *Writer`
  - `func (w *Writer) Save(nodeID string, m Meta, f Files) (dir string, err error)` (a nil `*Writer`, or one made with `on == false`, saves nothing and returns `"", nil`)
  - layout `<run>/attempts/<node-id>/<n>/{reply.go, reply.txt, check-output.txt, attempt.json}`, `n` starting at 1.

- [ ] **Step 1: Write the failing tests for the writer**

Create `gophermind-lib/briefv2/artifacts/artifacts_test.go`:

```go
package artifacts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/artifacts"
)

const canary = "s3cr3t-canary-value-9f2"

func scrubCanary(s string) string {
	if strings.Contains(s, canary) {
		return "(redacted)"
	}
	return s
}

func meta() artifacts.Meta {
	return artifacts.Meta{Provider: "mini", Model: "qwen", Revision: 0, Order: 1, Verdict: "fail", Class: "build", StartedAt: time.Unix(1, 0).UTC()}
}

func TestSaveWritesTheFourFilesAndNumbersAttempts(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 1<<20, scrubCanary)
	dir1, err := w.Save("fn-a", meta(), artifacts.Files{Source: []byte("package p\n"), Raw: "```go\npackage p\n```", CheckOutput: "build failed"})
	if err != nil {
		t.Fatal(err)
	}
	dir2, err := w.Save("fn-a", meta(), artifacts.Files{Raw: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if dir1 != filepath.Join(run, "attempts", "fn-a", "1") || dir2 != filepath.Join(run, "attempts", "fn-a", "2") {
		t.Fatalf("dirs = %s, %s", dir1, dir2)
	}
	for name, want := range map[string]string{"reply.go": "package p\n", "reply.txt": "```go\npackage p\n```", "check-output.txt": "build failed"} {
		b, err := os.ReadFile(filepath.Join(dir1, name))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", name, b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir1, "attempt.json")); err != nil {
		t.Errorf("attempt.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir2, "reply.go")); !os.IsNotExist(err) {
		t.Errorf("an empty Source must not create reply.go: %v", err)
	}
	if fi, _ := os.Stat(filepath.Join(dir1, "reply.go")); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSaveNeverWritesASecret(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 1<<20, scrubCanary)
	dir, err := w.Save("fn-a", meta(), artifacts.Files{
		Source: []byte("var k = \"" + canary + "\""), Raw: "token " + canary, CheckOutput: "got " + canary,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(run, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), canary) {
			t.Errorf("%s holds the canary secret", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "reply.txt"))
	if string(b) != "(redacted)" {
		t.Errorf("a file that held a secret must be replaced whole by the redaction mark, got %q", b)
	}
}

func TestSaveCapsEachFileAndMarksTheCut(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 100, nil)
	dir, err := w.Save("fn-a", meta(), artifacts.Files{CheckOutput: strings.Repeat("é", 200)})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "check-output.txt"))
	if !strings.Contains(string(b), "[truncated:") || len(b) > 100+64 {
		t.Errorf("capped file = %d bytes: %q", len(b), b)
	}
	if !strings.HasPrefix(string(b), "éé") {
		t.Errorf("the cut must fall on a rune boundary: %q", b[:8])
	}
}

func TestSaveCapDoesNotHideASecretAcrossTheCut(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 12, scrubCanary)
	dir, err := w.Save("fn-a", meta(), artifacts.Files{CheckOutput: "0123456789" + canary})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "check-output.txt"))
	if strings.Contains(string(b), canary[:6]) {
		t.Errorf("a secret straddling the cut leaked its start: %q", b)
	}
}

func TestSaveOffAndNilWriteNothing(t *testing.T) {
	run := t.TempDir()
	for _, w := range []*artifacts.Writer{nil, artifacts.New(run, false, 100, nil)} {
		dir, err := w.Save("fn-a", meta(), artifacts.Files{Raw: "x"})
		if err != nil || dir != "" {
			t.Errorf("Save = %q, %v; want nothing", dir, err)
		}
	}
	if ents, _ := os.ReadDir(run); len(ents) != 0 {
		t.Errorf("an off writer created %d entries", len(ents))
	}
}

func TestSaveRefusesAnUnsafeNodeID(t *testing.T) {
	run := t.TempDir()
	w := artifacts.New(run, true, 100, nil)
	for _, id := range []string{"../x", "/abs", "a/b", "", "UP"} {
		if _, err := w.Save(id, meta(), artifacts.Files{Raw: "x"}); err == nil {
			t.Errorf("Save accepted node id %q", id)
		}
	}
	if ents, _ := os.ReadDir(filepath.Dir(run)); len(ents) > 1 {
		for _, e := range ents {
			if e.Name() == "x" {
				t.Error("a node id escaped the run folder")
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `gt ./gophermind-lib/briefv2/artifacts/ -count=1`
Expected: FAIL (build error, package has no Go files).

- [ ] **Step 3: Write the writer**

Create `gophermind-lib/briefv2/artifacts/artifacts.go`:

```go
// Package artifacts saves what a model attempt produced, so a bad run can be
// inspected afterwards: the reply it gave and the compiler or test output that
// judged it. Files go under <run>/attempts/<node-id>/<n>/ and are scrubbed of
// every declared secret value before they touch disk. A prompt is never saved:
// it carries the brief slice and the dependency signatures and adds nothing a
// reply does not show. Saving is switched off by settings (executor.artifacts).
package artifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// Meta describes one attempt. It holds no reply text.
type Meta struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Revision    int       `json:"revision"`
	Order       int       `json:"order"`
	Verdict     string    `json:"verdict"`
	Class       string    `json:"class"`
	Reason      string    `json:"reason,omitempty"`
	ReplySHA256 string    `json:"reply_sha256,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	DurationMS  int64     `json:"duration_ms"`
}

// Files is what to save. An empty field creates no file.
type Files struct {
	Source      []byte // the formatted file the reply carried: reply.go
	Raw         string // the reply text as the model wrote it: reply.txt
	CheckOutput string // compiler, vet or test output: check-output.txt
}

// Writer saves attempts for one run. The zero value and nil save nothing.
type Writer struct {
	root     string
	on       bool
	maxBytes int
	scrub    func(string) string
}

// New returns a writer for the run folder runDir. scrub maps a text to itself
// or, when it holds a declared secret value, to a redaction mark; nil means no
// secrets are declared. maxBytes caps each saved file.
func New(runDir string, on bool, maxBytes int, scrub func(string) string) *Writer {
	return &Writer{root: filepath.Join(runDir, "attempts"), on: on, maxBytes: maxBytes, scrub: scrub}
}

var nodeIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Save writes one attempt of nodeID and returns its folder.
func (w *Writer) Save(nodeID string, m Meta, f Files) (string, error) {
	if w == nil || !w.on {
		return "", nil
	}
	if !nodeIDRE.MatchString(nodeID) {
		return "", errors.New("artifacts: not a node id")
	}
	nodeDir := filepath.Join(w.root, nodeID)
	n, err := nextNumber(nodeDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(nodeDir, strconv.Itoa(n))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	for name, text := range map[string]string{"reply.go": string(f.Source), "reply.txt": f.Raw, "check-output.txt": f.CheckOutput} {
		if text == "" {
			continue
		}
		if err := runfs.WriteFileAtomic(filepath.Join(dir, name), []byte(w.prepare(text))); err != nil {
			return "", err
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := runfs.WriteFileAtomic(filepath.Join(dir, "attempt.json"), append(b, '\n')); err != nil {
		return "", err
	}
	return dir, nil
}

// prepare scrubs the whole text first (a secret may straddle the cut), then
// caps it.
func (w *Writer) prepare(s string) string {
	if w.scrub != nil {
		s = w.scrub(s)
	}
	if w.maxBytes <= 0 || len(s) <= w.maxBytes {
		return s
	}
	cut := w.maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n[truncated: %d bytes cut]\n", len(s)-cut)
}

// nextNumber is one more than the highest numbered attempt folder of the node.
func nextNumber(nodeDir string) (int, error) {
	ents, err := os.ReadDir(nodeDir)
	if errors.Is(err, os.ErrNotExist) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	max := 0
	for _, e := range ents {
		if n, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() && n > max {
			max = n
		}
	}
	return max + 1, nil
}
```

Note on `TestSaveCapDoesNotHideASecretAcrossTheCut`: the scrub runs on the whole text first, so the secret is replaced whole by `(redacted)` before the cap is applied. The test proves no partial secret survives.

- [ ] **Step 4: Run to verify the writer passes**

Run: `gt ./gophermind-lib/briefv2/artifacts/ -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Add the settings keys (test first)**

In `settings/executor_test.go`, extend `TestExecutorSettingsValidate`: add to the `ints` map

```go
		"artifact_max_bytes":         func(e *settings.ExecutorConfig, v int) { e.ArtifactMaxBytes = v },
```

and add these cases to the file (read how the neighbouring `sandbox` case is written and follow it exactly):

```go
func TestExecutorArtifactsSetting(t *testing.T) {
	d := settings.DefaultExecutor()
	if d.Artifacts != "on" || d.ArtifactMaxBytes != 65536 {
		t.Fatalf("defaults = %q, %d; want on, 65536", d.Artifacts, d.ArtifactMaxBytes)
	}
	var e settings.ExecutorConfig
	if err := yaml.Unmarshal([]byte("artifacts: off\nartifact_max_bytes: 1000\n"), &e); err != nil || e.Artifacts != "off" || e.ArtifactMaxBytes != 1000 {
		t.Errorf("decoded %q %d, %v", e.Artifacts, e.ArtifactMaxBytes, err)
	}
	c := settings.Default()
	c.Executor.Artifacts = "maybe"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "executor.artifacts") {
		t.Errorf("artifacts: maybe must be refused, got %v", err)
	}
}
```

(use whatever the file already calls the config constructor and validator; the names above follow `settings.Default()` and `Validate()` which the executor spec names; `grep -n 'func Default()\|func (c Config) Validate\|func (c \*Config) Validate' gophermind-lib/briefv2/settings/*.go` to confirm and adjust the two calls).

Run: `gt ./gophermind-lib/briefv2/settings/ -count=1` and expect FAIL (`e.ArtifactMaxBytes undefined`).

In `settings/executor.go`: add the two fields to `ExecutorConfig` after `Sandbox`:

```go
	Artifacts        string `yaml:"artifacts"`          // "on" | "off": save the reply and check output of each attempt under attempts/
	ArtifactMaxBytes int    `yaml:"artifact_max_bytes"` // cap on each saved file
```

in `DefaultExecutor()` add `Artifacts: "on", ArtifactMaxBytes: 65536,`; in `applyExecutorDefaults` add `{&e.ArtifactMaxBytes, d.ArtifactMaxBytes},` to the int table and

```go
	if e.Artifacts == "" {
		e.Artifacts = d.Artifacts
	}
```

in `validate()` add `{"artifact_max_bytes", e.ArtifactMaxBytes, 16 << 20},` to the table and

```go
	if e.Artifacts != "on" && e.Artifacts != "off" {
		return fmt.Errorf("executor.artifacts must be on or off")
	}
```

Run: `gt ./gophermind-lib/briefv2/settings/ -count=1` and expect PASS.

- [ ] **Step 6: Make `tree.Store.Load` skip `attempts/` (test first)**

Append to `tree/store_test.go`:

```go
func TestLoadSkipsTheAttemptsFolder(t *testing.T) {
	dir := t.TempDir()
	s := tree.NewStore(dir)
	if err := s.Write(fn(t, "fn-a", "comp", 0)); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(dir, "attempts", "fn-a", "1")
	if err := os.MkdirAll(a, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "attempt.json"), []byte(`{"provider":"mini"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := s.Load()
	if err != nil || len(back.Nodes) != 1 {
		t.Fatalf("Load = %d nodes, %v; attempt.json must not be read as a node", len(back.Nodes), err)
	}
}
```

Run: `gt ./gophermind-lib/briefv2/tree/ -run TestLoadSkipsTheAttemptsFolder -count=1` and expect FAIL (a schema error for `attempt.json`).

In `tree/store.go` change the directory skip to:

```go
			if rel == "logs" || rel == "_state" || rel == "attempts" {
```

Run: `gt ./gophermind-lib/briefv2/tree/ -count=1` and expect PASS.

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/briefv2/artifacts gophermind-lib/briefv2/settings gophermind-lib/briefv2/tree
git commit -m "feat(briefv2): attempt artifacts writer, artifacts settings, Load skips attempts/"
```

---

### Task 5: Save artifacts from the executor

**Files:**
- Modify: `gophermind-lib/briefv2/executor/start.go` (the `runCtx` struct at line 47 and its construction at line 143), `gophermind-lib/briefv2/executor/leaf.go` (the `leafRun` struct, `attempt`, `check`, `record`)
- Create: `gophermind-lib/briefv2/executor/artifacts_test.go`

**Interfaces:**
- Consumes: `artifacts.New`, `artifacts.Writer.Save`, `artifacts.Meta`, `artifacts.Files` (Task 4); `rc.scrubText` (`wave.go:440`); `packer.Reply.Source`; `runner.Verdict.Out.Text()`.
- Produces: `runCtx.art *artifacts.Writer`; `leafRun.art attemptArt`; one saved attempt folder per `record` call.

- [ ] **Step 1: Write the failing test**

Read `executor/rig_test.go` and `executor/e2e_test.go` first to learn how a rig is built and how a scripted provider answers (`scriptedProvider` in `harness_test.go:225-262`). Create `executor/artifacts_test.go` with a test that runs the greeter plan (`TestExecutorE2E`'s setup) with one scripted reply that fails to build and a second that passes, and asserts:

```go
func TestExecutorSavesScrubbedAttemptArtifacts(t *testing.T) {
	g := newGreeterRig(t) // the helper TestExecutorE2E uses; rename to match rig_test.go
	g.script("implement:fn-greet", badBuildReply, goodReply)
	rep := g.run(t)
	if rep.Status != "verified" {
		t.Fatalf("status = %s", rep.Status)
	}
	dir := filepath.Join(g.runDir, "attempts", "fn-greet")
	for _, n := range []string{"1", "2"} {
		for _, f := range []string{"reply.go", "reply.txt", "attempt.json"} {
			if _, err := os.Stat(filepath.Join(dir, n, f)); err != nil {
				t.Errorf("attempt %s: %s missing: %v", n, f, err)
			}
		}
	}
	out, err := os.ReadFile(filepath.Join(dir, "1", "check-output.txt"))
	if err != nil || !strings.Contains(string(out), "greet.go") {
		t.Errorf("the failed attempt must keep its compiler output, got %q, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2", "check-output.txt")); err == nil {
		// a passing attempt has output too (go test -json); either is fine, but if present it must be scrubbed
		b, _ := os.ReadFile(filepath.Join(dir, "2", "check-output.txt"))
		if strings.Contains(string(b), g.canary) {
			t.Error("the canary secret reached check-output.txt")
		}
	}
}

func TestExecutorArtifactsOffWritesNothing(t *testing.T) {
	g := newGreeterRig(t)
	g.cfg.Executor.Artifacts = "off"
	g.script("implement:fn-greet", goodReply)
	g.run(t)
	if _, err := os.Stat(filepath.Join(g.runDir, "attempts")); !os.IsNotExist(err) {
		t.Errorf("artifacts: off must not create attempts/: %v", err)
	}
}

func TestExecutorArtifactsDoNotLeakACanarySecret(t *testing.T) {
	g := newGreeterRig(t)
	g.declareSecret("API_TOKEN", g.canary)
	g.script("implement:fn-greet", replyThatEchoesTheSecret, goodReply)
	g.run(t)
	filepath.WalkDir(g.runDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, _ := os.ReadFile(p); strings.Contains(string(b), g.canary) {
			t.Errorf("%s holds the canary secret", p)
		}
		return nil
	})
}

func TestRuntimeWritesInsideTheRunFolderAreNotStrayFiles(t *testing.T) {
	g := newGreeterRig(t) // a real temp git repo with .gophermind excluded
	before, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(g.runDir, "attempts", "fn-greet", "1"), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(g.runDir, "attempts", "fn-greet", "1", "reply.txt"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(g.runDir, "greet", "fn-greet.runtime.json"), []byte("{}"), 0o600)
	after, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	if len(stray(before, after)) != 0 { // use the function cleanStray uses; see stray.go line 85
		t.Errorf("run folder writes were reported as stray: %v", stray(before, after))
	}
}
```

The helper names (`newGreeterRig`, `script`, `run`, `declareSecret`, `canary`, `badBuildReply`, `goodReply`, `replyThatEchoesTheSecret`, `stray`) are placeholders for the equivalents in `rig_test.go` and `e2e_test.go`; open those files, use the real names, and if a needed helper does not exist add it to `rig_test.go` in the same style. The assertions are what the task requires; do not weaken them.

- [ ] **Step 2: Run to verify they fail**

Run: `gt ./gophermind-lib/briefv2/executor/ -run 'TestExecutorSavesScrubbed|TestExecutorArtifacts|TestRuntimeWrites' -count=1 -timeout 30m`
Expected: FAIL (no `attempts/` folder is created yet; the stray test may already pass).

- [ ] **Step 3: Add the writer to `runCtx`**

In `executor/start.go` add `"gophermind/gophermind-lib/briefv2/artifacts"` to the imports and a field to `runCtx` (after `heartbeat`):

```go
	art *artifacts.Writer // saves the scrubbed reply and check output of each attempt (settings executor.artifacts)
```

Where `rc := &runCtx{` is built (line 143), after the struct is created and `rc.cfg` and `rc.o` are set, add:

```go
	rc.art = artifacts.New(o.RunDir, rc.cfg.Executor.Artifacts == "on", rc.cfg.Executor.ArtifactMaxBytes, rc.scrubText)
```

(use the variable names that function really uses for the options and config; read lines 120 to 175 first).

- [ ] **Step 4: Collect the per-attempt material in `leafRun`**

In `executor/leaf.go` add a type and a field:

```go
// attemptArt is what the current attempt produced and record will save. It is
// memory only and is cleared when the attempt is recorded.
type attemptArt struct {
	raw    string // the reply text as the model wrote it (the last text the parser saw)
	source []byte // the formatted file the reply carried
	out    string // the check output
}
```

and in `leafRun` (after `permClass`): `art attemptArt`.

In `attempt` (around line 960), directly after `started := rc.o.Now()` add `lr.art = attemptArt{}`.

In the `parse` closure (around line 995) record the text:

```go
	parse := func(text string) error {
		lr.art.raw = text
		r, perr := packer.ParseReply(text, lr.expect)
		reply, lastSHA = r, r.SHA256
		return perr
	}
```

Right after the `CallParsed` error check (before the `identical` test) add `lr.art.source = reply.Source`.

In `check`, directly after `v := settle(rc.checkLeaf(ctx, l))` add `lr.art.out = v.Out.Text()`.

- [ ] **Step 5: Save from `record`**

In `record`, after the `AppendAttempt` error check and before `lr.history = append(...)`, add:

```go
	rc.saveArtifacts(lr, a, class)
```

and add the method (same file, below `record`):

```go
// saveArtifacts writes the reply and the check output of the attempt just
// recorded. A failure to write is told by a warning event and never fails the
// attempt: the verdict is already on the blackboard, and a full disk must not
// throw away an hour of model work.
func (rc *runCtx) saveArtifacts(lr *leafRun, a blackboard.Attempt, class string) {
	art := lr.art
	lr.art = attemptArt{}
	_, err := rc.art.Save(lr.l.ID, artifacts.Meta{
		Provider: a.Provider, Model: a.Model, Revision: a.Revision, Order: a.Order, Verdict: string(a.Verdict),
		Class: class, Reason: a.FailureReason, ReplySHA256: a.ReplySHA256, StartedAt: a.StartedAt, DurationMS: a.DurationMS,
	}, artifacts.Files{Source: art.source, Raw: art.raw, CheckOutput: art.out})
	if err != nil {
		rc.emit("warning", lr.l.ID, "artifacts: an attempt could not be saved")
	}
}
```

(add the `artifacts` import to `leaf.go`).

- [ ] **Step 6: Run to verify they pass**

Run: `gt ./gophermind-lib/briefv2/executor/ -run 'TestExecutorSavesScrubbed|TestExecutorArtifacts|TestRuntimeWrites' -count=1 -timeout 30m`
Expected: PASS.

Then the whole executor package, to prove nothing else changed: `gt ./gophermind-lib/briefv2/executor/ -count=1 -timeout 30m` (about 11 minutes). Expected: PASS (it still uses the SQLite backends until Task 7).

- [ ] **Step 7: Commit**

```bash
git add gophermind-lib/briefv2/executor
git commit -m "feat(briefv2): executor saves scrubbed attempt artifacts under attempts/"
```

---

### Task 6: Save planner stage replies

**Files:**
- Create: `gophermind-lib/briefv2/planner/replies.go`, `gophermind-lib/briefv2/planner/replies_test.go`
- Modify: `gophermind-lib/briefv2/planner/planner.go` (line 210, next to `p.dumpReply`)

**Interfaces:**
- Consumes: `runfs.WriteFileAtomic`; `settings.Config.Executor.Artifacts` and `.ArtifactMaxBytes` (Task 4); the planner's `run` (fields `dir`) and `p.d.Settings`.
- Produces: `func (p *Planner) saveReply(r *run, stage, text string)` writing `<run>/_state/replies/<stage>-<n>.txt`, where `<stage>` has every character outside `[A-Za-z0-9._-]` replaced by `_` (the `dumpNameRE` already in `debugdump.go`).

Planner prompts never contain secret values (project rule), so a reply cannot echo one unless a model invents it; the canary test below proves the files hold none of the canary the existing planner end-to-end test already uses.

- [ ] **Step 1: Write the failing test**

Read `planner/e2e_test.go` around line 492 (the vault canary test) and `planner/harness_test.go`. Create `planner/replies_test.go` with a test named `TestPlannerSavesStageRepliesFreeOfSecrets` (and a second, `TestPlannerSavesNoRepliesWhenArtifactsAreOff`). The first runs the offline greeter plan to the approval gate with the harness, then asserts:

1. `<run>/_state/replies/` exists and holds at least one file per model-calling stage that ran (`clarify`, `contract_outline`, `decompose_...`, `coverage`), each non-empty and mode 0600;
2. no file under the run folder holds the vault canary the neighbouring test uses;
3. with `Settings.Executor.Artifacts = "off"` the folder does not exist;
4. `tree.Store.Load` of the run folder still returns the plan nodes (the `_state` folder is skipped).

- [ ] **Step 2: Run to verify it fails**

Run: `gt ./gophermind-lib/briefv2/planner/ -run TestPlannerSavesStageReplies -count=1`
Expected: FAIL (no `_state/replies`).

- [ ] **Step 3: Write the implementation**

Create `planner/replies.go`:

```go
package planner

import (
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// saveReply keeps the text of each stage reply under _state/replies/ so a bad
// plan can be inspected afterwards. It follows the same setting as the
// executor's attempt artifacts (executor.artifacts, executor.artifact_max_bytes)
// and never fails the call: a write error is told on the notice stream only.
// Prompts are not saved, and planner prompts carry no secret value, so a reply
// has none to echo.
func (p *Planner) saveReply(r *run, stage, text string) {
	cfg := p.d.Settings
	if cfg == nil || cfg.Executor.Artifacts != "on" || text == "" {
		return
	}
	dir := filepath.Join(r.dir, "_state", "replies")
	name := dumpNameRE.ReplaceAllString(stage, "_")
	n := 1
	for {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%s-%d.txt", name, n))); os.IsNotExist(err) {
			break
		}
		n++
	}
	if max := cfg.Executor.ArtifactMaxBytes; max > 0 && len(text) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + fmt.Sprintf("\n[truncated: %d bytes cut]\n", len(text)-cut)
	}
	if err := runfs.WriteFileAtomic(filepath.Join(dir, fmt.Sprintf("%s-%d.txt", name, n)), []byte(text)); err != nil {
		fmt.Fprintf(debugOut, "stage reply not saved: %v\n", err)
	}
}
```

In `planner.go`, directly after the existing `p.dumpReply(r, cs.stage, req, text)` at line 210, add:

```go
			p.saveReply(r, cs.stage, text)
```

- [ ] **Step 4: Run to verify it passes**

Run: `gt ./gophermind-lib/briefv2/planner/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/briefv2/planner
git commit -m "feat(briefv2): planner saves stage replies under _state/replies"
```

---

### Task 7: Wire the commands and the test harnesses to the file backends

**Files:**
- Create: `cmd/gophermind/brief_state.go`
- Modify: `cmd/gophermind/brief_plan.go` (lines 218 to 226, 265 to 275, 304 to 360), `cmd/gophermind/brief_run.go` (lines 240 to 250, 656 to 670), `gophermind-lib/briefv2/planner/planner.go` and `load.go` (remove `ResetRun`)
- Modify (test harnesses): `planner/harness_test.go:110-115`, `executor/rig_test.go:189-195`, `executor/options_test.go:33-40`, `packer/packer_test.go:487-491`, `router/router_test.go:116-121`

**Interfaces:**
- Consumes: `blackboard.NewFS`, `ledger.NewFS`, `planner.LookupRun`, `runfs.Fixed`.
- Produces: `func runDirOf(runID string) (string, error)` and `func briefBackends() (blackboard.Blackboard, ledger.Ledger)` in `cmd/gophermind/brief_state.go`.

- [ ] **Step 1: Write the failing test for the commands**

Read `cmd/gophermind/brief_test.go` and `brief_run_test.go` to see how a command test sets up `GOPHERMIND_CONFIG_DIR` and a run record. Add to `cmd/gophermind/brief_state_test.go`:

```go
func TestBriefStatusReadsTheRunFolderAndCreatesNoDatabase(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", cfgDir)
	runDir := makeRunWithPlan(t, cfgDir) // writes a run record and a small valid tree; use the helper the existing brief tests use
	board, led := briefBackends()
	ctx := context.Background()
	if err := board.InitRun(ctx, runIDOf(runDir), nodeIDsOf(t, runDir), map[string]int{}); err != nil {
		t.Fatal(err)
	}
	rows, err := board.List(ctx, runIDOf(runDir), blackboard.Filter{})
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	if _, err := led.Summary(ctx, runIDOf(runDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "blackboard.db")); !os.IsNotExist(err) {
		t.Errorf("a database file was created: %v", err)
	}
}
```

(helper names are placeholders for what the existing command tests use; keep the three assertions.) Run: `gt ./cmd/gophermind/ -run TestBriefStatusReadsTheRunFolder -count=1` and expect FAIL (`undefined: briefBackends`).

- [ ] **Step 2: Add the constructors**

Create `cmd/gophermind/brief_state.go`:

```go
package main

import (
	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
)

// runDirOf finds a run's folder through the run record the planner writes at
// Load (<config dir>/runs/<run-id>.json).
func runDirOf(runID string) (string, error) {
	rec, err := planner.LookupRun(runID)
	if err != nil {
		return "", err
	}
	return rec.RunDir, nil
}

// briefBackends returns the blackboard and the ledger over the run folders.
// They hold no open files, so there is nothing to close.
func briefBackends() (blackboard.Blackboard, ledger.Ledger) {
	return blackboard.NewFS(runDirOf), ledger.NewFS(runDirOf)
}
```

- [ ] **Step 3: Replace the database opening in the commands**

Read `cmd/gophermind/brief_plan.go` lines 205 to 275 and 295 to 365, and `brief_run.go` lines 230 to 255 and 650 to 675.

In `brief_plan.go` (plan/resume command) replace the block that opens the database and builds the router and deps. The shape of the change is:

```go
	board, led := briefBackends()
	rt := router.New(cfg, providers, led, sink, router.WithAllowPublic(*allowPublic))
	deps.Caller, deps.Sink, deps.Board, deps.LedgerErrors = rt, sink, board, rt.LedgerErrors
```

and delete the `d, code := openBriefDB(errw)` lines with their `if code != 0` handling, the `defer d.Close()`, and the `deps.ResetRun = ...` line. Delete the function `openBriefDB` (line 265). In the status and calls commands (lines 304 and 351) replace `openBriefDBReadOnly(errw)` and the `ledger.NewSQLite(d)` / `blackboard.NewSQLite(d)` uses with `board, led := briefBackends()` and use `led.Summary`, `led.List`, `board` in `executorLines(...)`. In `brief_run.go` replace lines 240 to 250 the same way (`Board: board, Ledger: led`) and delete `openBriefDBReadOnly` (line 656 to its end) once nothing calls it. Remove every import that becomes unused (`database/sql`, `briefv2/db`).

- [ ] **Step 4: Remove the `ResetRun` hook**

State now lives in the run folder, which is created fresh, so the hook has no work to do. In `planner/planner.go` delete the `ResetRun` field and its comment (lines 48 to 51 of `Deps`). In `planner/load.go` delete the block (lines 76 to 80):

```go
	if p.d.ResetRun != nil {
		if err := p.d.ResetRun(ctx, b.Front.ID); err != nil {
			return nil, fmt.Errorf("planner: load: %w", err)
		}
	}
```

`grep -rn ResetRun gophermind-lib cmd` must then return nothing except test files; delete the tests of the hook or rewrite them to assert that a reused run id starts with no runtime files (a run folder that is removed and recreated has none).

- [ ] **Step 5: Convert the five test harnesses**

Each one opens the database with `db.Open(path)` and builds `ledger.NewSQLite(d)` and `blackboard.NewSQLite(d)`. Convert them one at a time and run the package tests after each.

1. `router/router_test.go:116-121`: the router tests need only a ledger. Replace the `db.Open(g.path)` and `ledger.NewSQLite(d)` with `ledger.NewFS(runfs.Fixed(g.runDir))`, where `g.runDir = t.TempDir()`; drop the `db` field and the `g.path` field if nothing else uses them. Run `gt ./gophermind-lib/briefv2/router/ -count=1`.
2. `packer/packer_test.go:487-491`: same replacement for the ledger. Run `gt ./gophermind-lib/briefv2/packer/ -count=1`.
3. `executor/options_test.go:33-40`: `Board: blackboard.NewFS(runfs.Fixed(dir), blackboard.WithFlatLayout()), Ledger: ledger.NewFS(runfs.Fixed(dir))` with `dir := t.TempDir()`.
4. `executor/rig_test.go:189-195`: the rig already has the run folder with the written tree; use `blackboard.NewFS(runfs.Fixed(g.runDir))` (tree layout, so the runtime files land beside the node files) and `ledger.NewFS(runfs.Fixed(g.runDir))`. Remove `g.dbPath` and every use of it.
5. `planner/harness_test.go:110-115`: the planner creates its run folder itself and writes the run record, so use the same resolver as production: `blackboard.NewFS(func(id string) (string, error) { rec, err := planner.LookupRun(id); return rec.RunDir, err })` and the same for `ledger.NewFS` (the harness already sets `GOPHERMIND_CONFIG_DIR` so `LookupRun` reads its own records; confirm by reading the file). Remove `g.dbPath` and `g.db`.

After the five conversions run: `gt ./gophermind-lib/briefv2/... ./cmd/... -count=1 -timeout 30m`. Expected: PASS except anything still importing `briefv2/db` for its own tests (the `db` package tests, deleted in Task 8).

- [ ] **Step 6: Run the new command test and the whole affected set**

Run: `gt ./cmd/gophermind/ -count=1` and `gt ./gophermind-lib/briefv2/planner/ ./gophermind-lib/briefv2/executor/ -count=1 -timeout 30m`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/gophermind gophermind-lib/briefv2
git commit -m "feat(briefv2): commands and test harnesses use the file backends; drop ResetRun"
```

---

### Task 8: Remove SQLite from briefv2

**Files:**
- Delete: `gophermind-lib/briefv2/db/` (the whole package), `gophermind-lib/briefv2/blackboard/sqlite.go`
- Create: `gophermind-lib/briefv2/blackboard/transitions.go`, `gophermind-lib/briefv2/nosql_test.go`
- Modify: `gophermind-lib/briefv2/ledger/ledger.go`

**Interfaces:**
- Consumes: everything above.
- Produces: no briefv2 package imports `database/sql` or `modernc.org/sqlite`; `go.mod` keeps `modernc.org/sqlite` because `gophermind-lib/tools` still uses it.

- [ ] **Step 1: Write the grep-gate test (fails while SQLite is still there)**

Create `gophermind-lib/briefv2/nosql_test.go`:

```go
package briefv2_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBriefv2PackageImportsSQL pins decision 3 of the unification spec: run
// state is files, so nothing under briefv2 may import database/sql or the
// SQLite driver.
func TestNoBriefv2PackageImportsSQL(t *testing.T) {
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || p == "nosql_test.go" {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		for _, bad := range []string{`"database/sql"`, `modernc.org/sqlite`, `briefv2/db"`} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s imports %s", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
```

Run: `gt ./gophermind-lib/briefv2/ -run TestNoBriefv2PackageImportsSQL -count=1`
Expected: FAIL, listing `db/db.go`, `blackboard/sqlite.go`, `ledger/ledger.go` (and `executor/typedecl.go` if it contains the string; it holds `"sql": "database/sql"` as data in a map, which is a false positive: if it is listed, change the test to skip lines of that file by matching `typedecl.go` in the skip condition and say so in a comment).

- [ ] **Step 2: Move the transition table, delete SQLite**

Create `blackboard/transitions.go` holding the `allowed` map and `transitionAllowed` exactly as they are in `sqlite.go` (lines 28 to 46 of that file), with `package blackboard` and no imports. Then:

```bash
git rm -r gophermind-lib/briefv2/db gophermind-lib/briefv2/blackboard/sqlite.go
```

In `ledger/ledger.go` delete the `SQLite` type, `NewSQLite`, and its four methods (`Record`, `Amend`, `List`, `Summary`), plus the imports `database/sql` and `briefv2/db`; keep `Outcome`, `Call`, `Filter`, `ModelSummary`, the `Ledger` interface and `Digest`.

- [ ] **Step 3: Build everything**

Run: `go build ./... && go vet ./gophermind-lib/briefv2/... ./cmd/...`
Expected: no output. Fix any leftover reference (`grep -rn 'NewSQLite\|briefv2/db' gophermind-lib cmd` must be empty).

- [ ] **Step 4: Run the gate and the full briefv2 suite**

Run: `gt ./gophermind-lib/briefv2/ -run TestNoBriefv2PackageImportsSQL -count=1` (expect PASS), then `gt ./gophermind-lib/briefv2/... ./cmd/... -count=1 -timeout 30m` (expect PASS).

- [ ] **Step 5: Commit**

```bash
git add -A gophermind-lib/briefv2 cmd
git commit -m "refactor(briefv2): remove the SQLite blackboard, ledger and db package; add the no-SQL gate"
```

---

### Task 9: End-to-end proof and documentation

**Files:**
- Modify: `docs/briefv2/README.md` (worktree), `docs/superpowers/specs/2026-09-30-v2-executor-design.md` (worktree; rule R15 and the settings table in section 11)
- Modify in the main checkout: `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md`, `CONTEXT.md` (local file)

- [ ] **Step 1: End-to-end run on the offline greeter plan**

Run: `gt ./gophermind-lib/briefv2/executor/ -run 'TestExecutorE2E|TestExecutorE2EResumeAfterKill|TestExecutorE2EEscalationEndsRun' -count=1 -timeout 30m`
Expected: PASS. Then inspect one finished run folder (the e2e rig's temp folder: print its path with `t.Log` temporarily, or re-run one test with `-v` and `t.TempDir` retained) and confirm by eye: `*.runtime.json` beside every node file with the right status, `_state/events.jsonl` and `_state/calls.jsonl` present, `attempts/<node>/<n>/` present for each attempt, no `.lock` files left, and no `blackboard.db` anywhere under the temp HOME. Record what you saw in the task report.

- [ ] **Step 2: Check the smoke path by hand**

With a throwaway home and the csvstat brief used by the executor smoke test (`scratchpad/smoke` per CONTEXT.md; or the brief at `gophermind-lib/briefv2/testdata/` if the scratchpad is gone), run `gophermind brief plan <brief> --yes`, then `gophermind brief status <run-id>` and `gophermind brief calls <run-id>`. Expected: status and calls print from the run folder; `ls <repo>/.gophermind/<run-id>` shows the layout of the spec, section 4.2 (with `.runtime.json`, no `.claim`). If the mini is unreachable, run the same commands against the offline fixture used by `cmd/gophermind/brief_test.go` instead and say so.

- [ ] **Step 3: Update `docs/briefv2/README.md`**

Add a section "Where a run keeps its state" with the layout block from the spec (using the `.runtime.json` names), the rule that run state is never written into plan node files, the two settings (`executor.artifacts`, `executor.artifact_max_bytes`) with their defaults, a sentence that `artifacts: off` stores hashes and sizes only, and the sentence "A run folder is self-contained: copy it to move the run." Remove every mention of `~/.gophermind/blackboard.db`.

- [ ] **Step 4: Update the executor spec**

In `docs/superpowers/specs/2026-09-30-v2-executor-design.md` change rule R15 (replies are never stored) to: replies and check output are saved, scrubbed, under `attempts/` unless `executor.artifacts` is `off`; prompts are never stored. Add the two settings to the table in section 11.

- [ ] **Step 5: Amend the unification spec (main checkout)**

In `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md` section 4.2 replace the `.state.json` and `.claim` lines with `<component>/<fn-id>.runtime.json` and a note that the claim lives inside it with a transient `.lock` file, and amend rule 2 of section 4.3 to say the claim is the exclusive-create `.lock` plus an edit of the runtime file. Keep decision 3 and decision 7 as they are. Add a line to section 4.4: "`Ledger.Amend` takes the run id; the ledger appends an amend line instead of rewriting a call." Add to `CONTEXT.md` a dated update saying sub-project A is built, with the commit range.

- [ ] **Step 6: Final checks and commits**

Run: `gt ./gophermind-lib/... ./cmd/... -count=1 -timeout 45m` once, from the worktree. Expected: PASS (the pre-push gate flakes at its iOS step: retry unchanged, never bypass; `TestAcceptanceInterruptedIsNotAPass` is a known flaky test under load).

Commit the worktree docs: `git add docs/briefv2/README.md docs/superpowers/specs/2026-09-30-v2-executor-design.md && git commit -m "docs(briefv2): run state is files; artifacts settings; R15 updated"`. Commit the main-checkout spec: `git add docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md && git commit -m "docs(spec): sub-project A as built (runtime.json, no claim file)"` (in the main checkout, explicit path, never `git add -A`).

---

## Self-review

**Spec coverage** (unification spec, section 4):

| Spec item | Task |
|---|---|
| 4.1 filesystem implementations behind the interfaces; SQLite and `db` removed | 2, 3, 8 |
| 4.2 layout: runtime files, `_state/events.jsonl`, `_state/calls.jsonl`, `attempts/` | 2, 3, 4, 5 |
| 4.3 rule 1 atomic writes | 1 |
| 4.3 rule 2 atomic claim | 2 (deviation 1: lock plus runtime file, no `.claim`) |
| 4.3 rule 3 transitions and lock with stale reclaim | 1, 2 |
| 4.3 rule 4 heartbeat | 2 |
| 4.3 rule 5 append-only logs; ledger failure rule | 1, 3 (the ledger-failure handling is in the router and is unchanged) |
| 4.3 rule 6 Watch follows events.jsonl | 2 |
| 4.3 rule 7 path safety | 2, 4 |
| 4.3 rules 8 and 9 attempt and planner artifacts | 4, 5, 6 |
| 4.4 Load skips new files; commands construct the backends | 4, 7 |
| 4.5 old database not migrated | 7, 8 |
| 4.6 tests 1 to 9 | 1 to 8 (test 7, the executor e2e with no SQLite, is Task 9 step 1 and the gate in Task 8) |

No gaps. The wording of the old spec's section 4.2 and rule 2 is amended in Task 9.

**Placeholder scan:** the only deliberate non-literal parts are test helper names in Tasks 5, 6 and 7 that the engineer must map to the real helpers in `rig_test.go`, `e2e_test.go` and the command tests (the plan names the file to read and states what each assertion must prove); and the `settings` constructor and validator names in Task 4, which the step tells the engineer to confirm with one grep. No step says "add error handling" or "write tests for the above".

**Type consistency:** `runfs.Fixed`, `runfs.Lock(path, wait, stale)`, `runfs.WriteFileAtomic`, `runfs.AppendLine`, `runfs.ReadLines`, `runfs.Size`, `runfs.TS` and `runfs.ParseTS` are defined in Task 1 and used with those exact signatures in Tasks 2, 3, 4 and 6. `blackboard.NewFS(resolve, opts...)`, `WithFlatLayout()` and `ledger.NewFS(resolve)` are used the same way in Tasks 7. `Ledger.Amend(ctx, runID, id, outcome, errorKind)` is changed in Task 3 and its only production caller is `markMalformed`, changed in the same task. `artifacts.New(runDir, on, maxBytes, scrub)`, `Save(nodeID, Meta, Files)`, `Meta` and `Files` match between Tasks 4 and 5.

**Known risks the engineer should watch:**
- `blackboard.FS.runtimePath` reads the whole plan once per run folder (`tree.NewStore(runDir).Load()`); if a plan node is invalid at that moment, every blackboard call fails with a named error rather than falling back. This is intended.
- The lock's stale rule uses file modification time against the wall clock, while the blackboard's own `now` (used for heartbeats) can be faked in tests; they are independent on purpose.
- The executor test package takes about 11 minutes; run the narrow tests while iterating and the full package once per task.
