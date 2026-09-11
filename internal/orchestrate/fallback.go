package orchestrate

import (
	"context"

	"gophermind/internal/phaseflow"
)

// StatusVerifyingRunner adapts a TaskRunner that already does its own
// verify-and-correct internally, such as Runner, to
// phaseflow.FallbackRunner's split design: FallbackRunner always calls
// Inner and then, separately, Verify. Runner already ran the task's
// acceptance criteria before returning, so re-verifying independently would
// mean building a second fresh agent and paying for a second verification
// pass for no more insight than Runner's own verdict already carries.
// StatusVerifyingRunner instead remembers the status its wrapped Run call
// returned and trusts it as the verdict when FallbackRunner calls Verify
// immediately afterward.
//
// It must be used the way FallbackRunner itself is documented to be used:
// strictly sequentially. Piece 2's execution is sequential, which is
// exactly what makes that safe.
type StatusVerifyingRunner struct {
	inner      phaseflow.TaskRunner
	lastStatus string
}

// NewStatusVerifyingRunner wraps inner, an ordinary TaskRunner, so it can
// serve as both FallbackRunner.Inner and FallbackRunner.Verify.
func NewStatusVerifyingRunner(inner phaseflow.TaskRunner) *StatusVerifyingRunner {
	return &StatusVerifyingRunner{inner: inner}
}

// Run implements phaseflow.TaskRunner: it delegates to inner and records
// the status returned, for the Verify call FallbackRunner makes next.
func (s *StatusVerifyingRunner) Run(ctx context.Context, t phaseflow.Task) (status string, detail string, err error) {
	status, detail, err = s.inner.Run(ctx, t)
	s.lastStatus = status
	return status, detail, err
}

// Verify implements phaseflow.Verifier: it trusts the status from the most
// recent Run call. StatusDone and StatusCorrected pass; anything else
// fails, with detail (the wrapped runner's own failure explanation) as the
// reason so the circuit breaker in piece 4 still gets something specific
// rather than a bare "failed".
func (s *StatusVerifyingRunner) Verify(_ context.Context, _ phaseflow.Task, detail string) (ok bool, reason string) {
	if s.lastStatus == phaseflow.StatusDone || s.lastStatus == phaseflow.StatusCorrected {
		return true, ""
	}
	if detail != "" {
		return false, detail
	}
	return false, "runner returned status " + s.lastStatus
}
