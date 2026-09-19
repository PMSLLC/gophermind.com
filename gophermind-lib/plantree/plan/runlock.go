package plan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

// runLockFile is the run lock's own file. It is deliberately NOT the tree's
// write.lock: that one is taken around every single node write, and a run
// holding it would deadlock the first Create or Update the run itself makes.
const runLockFile = "run.lock"

// ErrRunBusy is returned when another run already holds this tree's run lock.
// Planning passes cost model calls and write the same nodes, so a second one
// is refused rather than queued behind the first.
var ErrRunBusy = errors.New("plan: another planning run is already working on this plan")

// held tracks the run locks this process holds, so a caller that already
// holds one can take it again instead of deadlocking against itself. flock is
// per open file description and the Windows lock is a file's existence, so
// without this count a nested AcquireRun (the TUI taking the lock and then
// calling RunPass1, which takes it too) would report ErrRunBusy against its
// own run. The count is per path, so two different trees are independent.
var held = struct {
	mu   sync.Mutex
	n    map[string]*runHold
	next uint64
}{n: map[string]*runHold{}}

// runHold is one acquisition of the file lock, from the first AcquireRun to
// the release that drops the count to zero. gen tells it apart from the next
// acquisition of the same path, so a release leaked from an earlier hold
// cannot decrement a later one.
type runHold struct {
	gen     uint64
	count   int
	release func()
}

// AcquireRun takes this plan's run lock and returns the function that frees
// it. It never waits: when another process is already running passes over the
// same tree it returns ErrRunBusy, so a second /project or /questions is told
// so instead of hanging with no explanation.
//
// It is re-entrant within one process: a caller that already holds the lock
// gets it again, and the lock is freed when the outermost release runs. That
// is what lets a command take the lock for a whole run of several passes and
// still call RunPass1 and RunPass2, which take it for themselves.
//
// The limit of that: within one process the count cannot tell a nested call
// from a second goroutine, so two concurrent runs in the same process are both
// admitted. The lock protects against other processes; serializing runs inside
// one process is the caller's job (the TUI does it in startPass).
//
// Callers must defer the returned release. It is safe to call more than once,
// and a release from an earlier hold that arrives after the lock was freed and
// taken again does nothing to the newer hold.
func AcquireRun(repo *plantree.Repo) (func(), error) {
	dir := filepath.Join(repo.Dir(), "_state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, runLockFile)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}

	held.mu.Lock()
	defer held.mu.Unlock()
	if h := held.n[path]; h != nil {
		h.count++
		return releaseOnce(path, h.gen), nil
	}
	free, err := lockfile.TryAcquire(path)
	if errors.Is(err, lockfile.ErrBusy) {
		return nil, fmt.Errorf("%w (its lock is %s; %s)", ErrRunBusy, path, busyAdvice(runtime.GOOS))
	}
	if err != nil {
		return nil, err
	}
	held.next++
	held.n[path] = &runHold{gen: held.next, count: 1, release: free}
	return releaseOnce(path, held.next), nil
}

// busyAdvice is what to tell the user when the lock is held. On unix the lock
// dies with its process, so deleting the file is never the answer and would
// let a second run in beside a live one. Only Windows, where a crashed run
// leaves the file behind, gets delete advice.
func busyAdvice(goos string) string {
	if goos == "windows" {
		return "wait for it to finish; if no run is left, a crashed run left the file, so delete it"
	}
	return "wait for that run to finish or cancel it"
}

// releaseOnce returns a release that drops one hold of generation gen on
// path, at most once however often it is called, and not at all if that
// generation is already gone.
func releaseOnce(path string, gen uint64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			held.mu.Lock()
			defer held.mu.Unlock()
			h := held.n[path]
			if h == nil || h.gen != gen {
				return
			}
			h.count--
			if h.count > 0 {
				return
			}
			delete(held.n, path)
			h.release()
		})
	}
}
