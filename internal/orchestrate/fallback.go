package orchestrate

import (
	"context"
	"sync"

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
// The verdict is remembered per task, not in a single field. A wave runs
// its tasks concurrently and one StatusVerifyingRunner serves all of them,
// so a shared field is overwritten by a sibling's Run between a task's own
// Run and Verify. That does not merely race: it makes a failing task verify
// against a passing sibling's status and be recorded as done, which nothing
// downstream can detect.
type StatusVerifyingRunner struct {
	inner phaseflow.TaskRunner

	mu     sync.Mutex
	status map[string]string // task id -> status from its most recent Run
}

// NewStatusVerifyingRunner wraps inner, an ordinary TaskRunner, so it can
// serve as both FallbackRunner.Inner and FallbackRunner.Verify.
func NewStatusVerifyingRunner(inner phaseflow.TaskRunner) *StatusVerifyingRunner {
	return &StatusVerifyingRunner{inner: inner, status: map[string]string{}}
}

// Run implements phaseflow.TaskRunner: it delegates to inner and records
// the status returned, for the Verify call FallbackRunner makes next.
func (s *StatusVerifyingRunner) Run(ctx context.Context, t phaseflow.Task) (status string, detail string, err error) {
	status, detail, err = s.inner.Run(ctx, t)
	s.mu.Lock()
	s.status[t.ID] = status
	s.mu.Unlock()
	return status, detail, err
}

// Verify implements phaseflow.Verifier: it trusts the status this task's own
// most recent Run returned. StatusDone and StatusCorrected pass; anything
// else fails, with detail (the wrapped runner's own failure explanation) as
// the reason so the circuit breaker in piece 4 still gets something specific
// rather than a bare "failed".
//
// The entry is consumed as it is read, so the map holds only tasks currently
// between their Run and their Verify rather than every task of the run. A
// Verify with no recorded Run fails closed: absent evidence of a pass is not
// a pass.
func (s *StatusVerifyingRunner) Verify(_ context.Context, t phaseflow.Task, detail string) (ok bool, reason string) {
	s.mu.Lock()
	last, seen := s.status[t.ID]
	delete(s.status, t.ID)
	s.mu.Unlock()

	if !seen {
		return false, "no run recorded for task " + t.ID
	}
	if last == phaseflow.StatusDone || last == phaseflow.StatusCorrected {
		return true, ""
	}
	if detail != "" {
		return false, detail
	}
	return false, "runner returned status " + last
}
