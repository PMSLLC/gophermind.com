package phaseflow

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// This file implements sequential per-task model fallback (pipeline piece 2,
// see docs/superpowers/specs/2026-09-11-harness-pipeline-design.md). It sits
// entirely on top of the existing TaskRunner seam: the execution engine in
// execute.go is unaware of candidate models, verification or attempts - it
// only ever sees a TaskRunner and a terminal status. FallbackRunner is that
// TaskRunner, wrapping an inner one.

// Verifier decides whether a runner's output satisfies a task. It returns a
// specific reason on failure, never a bare boolean: the revision circuit
// breaker (piece 4) exists to notice that several candidate models failed
// the SAME way, and it cannot do that from a bare pass/fail.
type Verifier interface {
	Verify(ctx context.Context, t Task, detail string) (ok bool, reason string)
}

// FallbackRunner wraps an inner TaskRunner and tries a task's candidate
// models in order, verifying after each and recording every attempt whether
// it passed or failed. The first candidate to pass wins: only that
// attempt's detail is ever returned as the deliverable, never an earlier
// failed candidate's output. Exhausting every candidate without a pass is
// not retried indefinitely - it is reported as StatusNeedsRevision so the
// execution engine's round logic leaves it alone (see execute.go).
type FallbackRunner struct {
	// Inner actually executes a task against one candidate model.
	Inner TaskRunner
	// Verify decides pass or fail for each candidate's output. Required:
	// a nil Verify is a caller error and Run will panic if it is reached,
	// the same way a nil Inner would.
	Verify Verifier
	// Now supplies the current time, injectable so attempt durations are
	// deterministic in tests. Defaults to time.Now.
	Now func() time.Time
	// Candidates resolves a task's ordered candidate model list. Defaults
	// to t.CandidateModels, falling back to a single-element list of
	// t.Model when that is empty. Callers that want candidates derived
	// from the model picker (see internal/modelcat.OrderedCandidates)
	// supply their own function here; FallbackRunner itself takes no
	// dependency on modelcat.
	Candidates func(t Task) []string

	// OnAttempt, when set, is called with each attempt as it completes, in the
	// order tried. It replaces a shared LastAttempts field that could only be
	// read safely by a strictly sequential caller: once waves run tasks
	// concurrently, two Run calls on one FallbackRunner would race on it, and
	// the only thing preventing that was a copy performed at a single call
	// site. A callback has no shared mutable state to race on.
	//
	// It also reports each attempt AS IT HAPPENS rather than after Run
	// returns, which is what a live attempt log needs.
	//
	// It is called on the goroutine running the task, so an implementation
	// that touches shared state must do its own synchronizing.
	OnAttempt func(Attempt)
}

// Run implements TaskRunner.
func (f *FallbackRunner) Run(ctx context.Context, t Task) (status string, detail string, err error) {
	// Attempts are reported through OnAttempt as they happen. Nothing is kept
	// on the receiver, so concurrent Run calls on one FallbackRunner cannot
	// race and no caller has to copy the runner to stay safe.
	recordAttempt := func(a Attempt) {
		if f.OnAttempt != nil {
			f.OnAttempt(a)
		}
	}

	now := f.Now
	if now == nil {
		now = time.Now
	}
	resolve := f.Candidates
	if resolve == nil {
		resolve = defaultCandidates
	}

	candidates := resolve(t)
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("phaseflow: task %q has no candidate models", t.ID)
	}

	var reasons []string
	for _, model := range candidates {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}

		attemptTask := t
		attemptTask.Model = model

		started := now()
		_, innerDetail, runErr := f.Inner.Run(ctx, attemptTask)
		duration := now().Sub(started)

		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		if isCancel(runErr) {
			return "", "", runErr
		}

		if runErr != nil {
			reason := runErr.Error()
			recordAttempt(Attempt{
				Model:     model,
				StartedAt: started,
				Duration:  duration.String(),
				Verdict:   "fail",
				Reason:    reason,
			})
			reasons = append(reasons, reason)
			continue
		}

		ok, reason := f.Verify.Verify(ctx, attemptTask, innerDetail)
		verdict := "fail"
		if ok {
			verdict = "pass"
		}
		recordAttempt(Attempt{
			Model:     model,
			StartedAt: started,
			Duration:  duration.String(),
			Verdict:   verdict,
			Reason:    reason,
		})
		if ok {
			return StatusDone, innerDetail, nil
		}
		reasons = append(reasons, reason)
	}

	return StatusNeedsRevision, summarizeFailures(reasons), nil
}

// defaultCandidates is FallbackRunner's Candidates function when none is
// supplied: t.CandidateModels when non-empty, else a single-element list of
// t.Model, else nil (which Run turns into an error rather than a silent
// no-op).
func defaultCandidates(t Task) []string {
	if len(t.CandidateModels) > 0 {
		return t.CandidateModels
	}
	if t.Model != "" {
		return []string{t.Model}
	}
	return nil
}

// summarizeFailures renders the distinct failure reasons from an exhausted
// candidate list as the task's StatusNeedsRevision detail.
func summarizeFailures(reasons []string) string {
	seen := make(map[string]bool, len(reasons))
	var uniq []string
	for _, r := range reasons {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		uniq = append(uniq, r)
	}
	return "all candidate models failed: " + strings.Join(uniq, "; ")
}
