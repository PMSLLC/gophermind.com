package executor

import "context"

// runContext returns the run's context, a function naming why it ended (empty
// while it has not), and its release function. Task 12b: a cancel context whose
// cause is "cancelled". Task 14 replaces the body with signal handling and
// max_run_minutes; the signature does not change.
func (rc *runCtx) runContext(parent context.Context) (ctx context.Context, cause func() string, stop func()) {
	ctx, cancel := context.WithCancel(parent)
	cause = func() string {
		if ctx.Err() != nil {
			return "cancelled"
		}
		return ""
	}
	return ctx, cause, cancel
}
