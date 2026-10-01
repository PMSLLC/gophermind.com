package executor

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// The two causes runContext records on the context it returns; any other end
// (the caller's context) is "cancelled".
var (
	errRunLimit  = errors.New("max_run_minutes")
	errRunSignal = errors.New("signal")
)

// runLimit is how long this invocation may work: the test override rc.limit,
// else executor.max_run_minutes.
func (rc *runCtx) runLimit() time.Duration {
	if rc.limit > 0 {
		return rc.limit
	}
	return time.Duration(rc.cfg.Executor.MaxRunMinutes) * time.Minute
}

// runContext returns the run's context, a function naming why it ended (empty
// while it has not: "max_run_minutes", "signal", or "cancelled" when the
// parent ended), and its release function. The context ends when SIGINT or
// SIGTERM arrives, or when the run limit has elapsed since this call: the
// limit starts from zero on every invocation, so a resumed run gets all of
// max_run_minutes again, and it is created after startRun, so it measures work.
// The clock is rc.afterFunc (time.AfterFunc when nil).
func (rc *runCtx) runContext(parent context.Context) (ctx context.Context, cause func() string, stop func()) {
	ctx, cancel := context.WithCancelCause(parent)
	after := rc.afterFunc
	if after == nil {
		after = func(d time.Duration, f func()) func() {
			t := time.AfterFunc(d, f)
			return func() { t.Stop() }
		}
	}
	stopTimer := after(rc.runLimit(), func() { cancel(errRunLimit) })

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-sig:
			cancel(errRunSignal)
		case <-done:
		}
	}()

	cause = func() string {
		if ctx.Err() == nil {
			return ""
		}
		switch context.Cause(ctx) {
		case errRunLimit:
			return "max_run_minutes"
		case errRunSignal:
			return "signal"
		}
		return "cancelled"
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			stopTimer()
			signal.Stop(sig)
			close(done)
			cancel(nil)
		})
	}
	return ctx, cause, stop
}
