package executor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// errLandingFault is what wait returns to a job behind a landing that failed
// with a harness fault (ruling C5, spec section 7.3): the job does not land,
// its candidate stays, and it ends interrupted.
var errLandingFault = errors.New("executor: an earlier landing of the level failed; this leaf does not land")

// turn is one job's place in the landing order of a parallel level (spec
// section 7.1). Jobs start in id order; each waits for the job before it to
// end before it touches the main tree, so commits are made one at a time and
// in id order. A job releases its turn when it ends, however it ends, so a
// failing leaf never blocks the ones after it. The turn holder also holds the
// scheduler's body mutex (ruling 15). Only the job's own goroutine calls wait
// and release; ids only wait on lower ids, which started earlier and hold their
// own slot, so the chain cannot deadlock.
type turn struct {
	prev  <-chan struct{} // done of the previous job that ran in the level; nil for the first
	done  chan struct{}   // closed once, by release
	body  *sync.Mutex     // rc.sched.body
	fault *atomic.Bool    // set by the level when a landing failed (ruling C5); nil: never
	held  bool
	once  sync.Once
}

func newTurn(prev <-chan struct{}, body *sync.Mutex) *turn {
	return &turn{prev: prev, done: make(chan struct{}), body: body}
}

// wait blocks until the previous job ended, then takes the main tree. When the
// context has ended, or ends first, it returns the context's error and takes
// nothing. When a landing of the level failed, it returns errLandingFault and
// takes nothing. A turn already held is not taken again.
func (t *turn) wait(ctx context.Context) error {
	if t.held {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.prev != nil {
		select {
		case <-t.prev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.fault != nil && t.fault.Load() {
		return errLandingFault
	}
	t.body.Lock()
	t.held = true
	return nil
}

// release gives the main tree back and lets the next job go. Safe to call more
// than once.
func (t *turn) release() {
	t.once.Do(func() {
		if t.held {
			t.held = false
			t.body.Unlock()
		}
		close(t.done)
	})
}
