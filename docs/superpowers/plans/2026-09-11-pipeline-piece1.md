# Pipeline Piece 1: Task Model, Attempts History, and a Safe Store - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `phaseflow.Task` the fields the pipeline needs (wave, dependencies, candidate models, attempt history, revision rounds) and make the assignments store safe for the concurrent writes that waves will bring.

**Architecture:** Extract the odometer's proven file-claim pattern into a small shared package, use it for `Assignments.Save`, then extend `Task` additively so an existing `assignments.json` keeps working.

**Tech Stack:** Go 1.26.5, module `gophermind`. Standard library only.

**Spec:** `docs/superpowers/specs/2026-09-11-harness-pipeline-design.md` (piece 1), which adapts `docs/superpowers/specs/2026-09-11-harness-pipeline-source-spec.md`.

## Global Constraints

- Module is `gophermind`. Go 1.26.5. Standard library only. `go.mod`/`go.sum` byte-unchanged.
- NO em dashes and NO emoji on any line you add. Plain hyphens.
- Every exported symbol gets a doc comment.
- No test may make a network call.
- `go build ./... && go test ./... -race` green before each commit.
- Commit messages end with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## Why the store comes first

`Assignments.Save` (`internal/phaseflow/assignments.go:69`) is `os.WriteFile` with
no lock and no atomic rename. Execution is strictly sequential today, so nothing
has ever exercised it concurrently. Piece 3 introduces parallel tasks within a
wave, at which point two tasks finishing together means one result is silently
lost. Fixing that is not a refactor for tidiness; it is the precondition for the
feature that makes this pipeline worth building.

---

### Task 1: Extract the file-claim pattern into `internal/lockfile`

**Files:**
- Create: `internal/lockfile/lockfile.go`, `internal/lockfile/lock_unix.go`, `internal/lockfile/lock_windows.go`
- Create: `internal/lockfile/lockfile_test.go`
- Modify: `internal/freellm/odometer.go`, and delete `internal/freellm/lock_unix.go` / `lock_windows.go`

**Interfaces produced:**
```go
// Acquire takes an exclusive advisory lock on path, blocking until it is
// available. The returned function releases it.
func Acquire(path string) (release func(), err error)

// WriteAtomic writes data to path via a temp file in the same directory,
// fsynced and renamed, so a crash cannot leave a half-written file.
func WriteAtomic(path string, data []byte, perm os.FileMode) error
```

The existing implementations are in `internal/freellm/lock_unix.go` (flock),
`lock_windows.go` (O_EXCL with a 60s mtime takeover) and `odometer.go`'s `save`
(temp, Chmod, Sync, rename). Move them, do not rewrite them: they carry
hard-won behaviour, including the stale-lock takeover added after a real
incident.

- [ ] **Step 1: Move the files with `git mv` to preserve history**

```bash
mkdir -p internal/lockfile
git mv --gw-force internal/freellm/lock_unix.go internal/lockfile/lock_unix.go
git mv --gw-force internal/freellm/lock_windows.go internal/lockfile/lock_windows.go
```
Change their package clause to `lockfile` and export `lockFile` as `Acquire`,
keeping every comment: they explain WHY the takeover exists.

- [ ] **Step 2: Write the tests**

Create `internal/lockfile/lockfile_test.go`:

```go
package lockfile

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// The property the whole pipeline rests on: N concurrent writers, none lost.
func TestAcquireSerializesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "state.lock")
	data := filepath.Join(dir, "state")

	if err := os.WriteFile(data, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}

	const n = 25
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := Acquire(lock)
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			// Read-modify-write under the lock. Without it, increments are lost.
			b, err := os.ReadFile(data)
			if err != nil {
				t.Error(err)
				return
			}
			var cur int
			for _, c := range b {
				cur = cur*10 + int(c-'0')
			}
			if err := WriteAtomic(data, []byte(itoa(cur+1)), 0o600); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	b, _ := os.ReadFile(data)
	var got int
	for _, c := range b {
		got = got*10 + int(c-'0')
	}
	if got != n {
		t.Fatalf("counter = %d after %d locked increments, want %d: writes were lost", got, n, n)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	return string(out)
}

func TestWriteAtomicReplacesContentWholesale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := WriteAtomic(path, []byte("a longer first value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "short" {
		t.Errorf("got %q: a rename must replace, never overlay", b)
	}
}

func TestWriteAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAtomic(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want just the target file", names)
	}
}

func TestWriteAtomicHonorsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := WriteAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}
```

- [ ] **Step 3: Implement and rewire `freellm`**

`Odometer.save` becomes a `lockfile.WriteAtomic` call; `Add` uses
`lockfile.Acquire`. **The odometer's existing tests must pass unchanged** -
especially `TestOdometerConcurrentAdd` and the monotonicity tests. They are the
proof the move preserved behaviour.

- [ ] **Step 4: Run everything**

Run: `go build ./... && go test ./... -race -count=1`
Expected: green, including every `internal/freellm` test with no edits.
Also run `GOOS=windows go build ./...` so the Windows lock still compiles.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(lockfile): extract the file-claim pattern for reuse

The odometer's flock, Windows mtime takeover and atomic temp-sync-rename
move to internal/lockfile so the assignments store can use the same
proven code rather than a second implementation. Moved with git mv and
not rewritten: the takeover exists because a killed process wedged the
odometer permanently, and that reasoning travels with the comments.

freellm's own tests pass unchanged, which is the proof the move was
behaviour-preserving.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 2: Make the assignments store concurrency-safe

**Files:**
- Modify: `internal/phaseflow/assignments.go`
- Test: `internal/phaseflow/assignments_concurrent_test.go` (new)

**Interfaces produced:**
```go
// Update applies mutate to the assignments under an exclusive lock and writes
// the result atomically. It is the only safe way to change a task's state when
// more than one task may be running.
func Update(root string, mutate func(*Assignments) error) error
```

`Save` keeps its signature for existing callers, but routes through
`lockfile.WriteAtomic`. `Update` is the read-modify-write primitive piece 3
needs: load, mutate, save, all inside one lock.

- [ ] **Step 1: Write the failing test**

Create `internal/phaseflow/assignments_concurrent_test.go`. The central test:
N goroutines each flipping a DIFFERENT task's status through `Update`, then
assert every one of them stuck. Under the current `Save` this fails, because
each goroutine writes a whole file built from a stale read.

```go
func TestUpdateDoesNotLoseConcurrentTaskUpdates(t *testing.T) {
	root := t.TempDir()
	const n = 20

	var a Assignments
	for i := 0; i < n; i++ {
		a.Tasks = append(a.Tasks, Task{ID: fmt.Sprintf("t%02d", i), Status: StatusPending})
	}
	if err := a.Save(root); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("t%02d", i)
			if err := Update(root, func(a *Assignments) error {
				for j := range a.Tasks {
					if a.Tasks[j].ID == id {
						a.Tasks[j].Status = StatusDone
						return nil
					}
				}
				return fmt.Errorf("task %s missing", id)
			}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	final, found, err := LoadAssignments(root)
	if err != nil || !found {
		t.Fatalf("load after concurrent updates: %v found=%v", err, found)
	}
	for _, tk := range final.Tasks {
		if tk.Status != StatusDone {
			t.Errorf("task %s is %q: a concurrent update was lost", tk.ID, tk.Status)
		}
	}
}
```

Also test: `Update` propagates a mutate error without writing; a missing
assignments file is a clear error rather than a silent empty write.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/phaseflow/ -run TestUpdateDoesNotLose -race -count=1`
Expected: FAIL to build (`undefined: Update`). After adding a naive `Update`
built on the current `Save`, it should fail on lost updates; confirm that
intermediate failure and say so in your report, because it is the evidence the
test is real.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run everything**

Run: `go build ./... && go test ./... -race -count=1`

- [ ] **Step 5: Commit**

---

### Task 3: Extend the task model

**Files:**
- Modify: `internal/phaseflow/assignments.go`
- Test: `internal/phaseflow/assignments_model_test.go` (new)

Add, exactly as the design doc specifies: `Wave`, `DependsOn`,
`CandidateModels`, `Attempts`, `RevisionRounds`, the `Attempt` and `Revision`
types, and the `needs_revision` and `escalated` status constants. Every new
field is `omitempty`.

**Add `func (t *Task) RecordAttempt(a Attempt)`** which appends and caps the
history. **Retention rule:** keep at most 50 attempts per task, dropping the
oldest, because a long unattended run with revisions could otherwise grow
`assignments.json` without bound. The odometer's ring is the precedent. State
the cap as a named constant with a comment explaining why it exists.

- [ ] **Step 1: Write the failing tests**

Cover: a legacy `assignments.json` with none of the new fields loads and
round-trips without gaining noise (marshal it and assert the absent fields are
still absent, which is what `omitempty` buys); `RecordAttempt` preserves order;
the cap drops the oldest and keeps the newest; the two new statuses exist.

The legacy test must use a **JSON literal**, not a marshalled struct, so it keeps
testing the old on-disk shape after the struct grows. That is the same technique
that protected the odometer.

- [ ] **Step 2-4:** fail, implement, pass.

- [ ] **Step 5: Commit**

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green, with every pre-existing `internal/freellm` and `internal/phaseflow` test passing unedited.
- [ ] `GOOS=windows go build ./...` succeeds.
- [ ] A legacy `assignments.json` loads, round-trips, and gains no new keys.
- [ ] N concurrent `Update` calls all stick.
- [ ] `go.mod`/`go.sum` unchanged.
- [ ] No em dashes on added lines.
