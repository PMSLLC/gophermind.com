package phaseflow

import (
	"context"
	"errors"
	"strings"
	"time"
)

// This file implements the revision circuit breaker (pipeline piece 4, see
// docs/superpowers/specs/2026-09-11-harness-pipeline-design.md): when a task
// exhausts every candidate model, a bad task spec is more likely than every
// model being incapable, so the harness rewrites the task definition instead
// of retrying it as-is. It is a seam only - ApplyRevision mutates a Task in
// memory and takes no dependency on execute.go. Wiring a Reviser into the
// run loop is execute.go's job (see reviseNeedsRevisionTasks).

// Reviser rewrites a task definition after every candidate model failed it.
// It receives the full attempt history so it can see what the failures had
// in common, which is the whole reason revision beats retrying: three
// failures sharing a root cause should produce a revision that addresses
// that pattern, not a generic "try again".
type Reviser interface {
	Revise(ctx context.Context, t Task, attempts []Attempt) (Revision, error)
}

// MaxRevisionRounds bounds how many times a task's definition is rewritten
// before it escalates to a human. Two is the source spec's recommendation:
// enough for a genuinely under-specified task, few enough that a task
// nothing can satisfy stops burning quota.
const MaxRevisionRounds = 2

// NeedsEscalation reports whether t has already used every revision round
// MaxRevisionRounds allows, and so must escalate to a human rather than
// receive another revision. It is a pure function over t.RevisionRounds, so
// the run loop can decide to escalate a task without ever calling a
// Reviser - a task at the cap gets no further model attempt automatically,
// which is the source spec's own test.
func NeedsEscalation(t Task) bool {
	return len(t.RevisionRounds) >= MaxRevisionRounds
}

// ApplyRevision records revision r on task t and resets it for another
// pass: status goes back to StatusPending, Attempts is cleared so the next
// pass starts a fresh count, and r is appended to RevisionRounds with an
// incrementing Round number - RevisionRounds is the durable record of what
// was tried, and losing it would hide the pattern the next revision needs
// to see.
//
// A revision that changes nothing (empty Deliverable and empty Test) is
// rejected as an error and t is left untouched: recording a no-op revision
// would consume one of only MaxRevisionRounds rounds and teach nothing.
func ApplyRevision(t *Task, r Revision) error {
	if strings.TrimSpace(r.Deliverable) == "" && len(r.Test) == 0 {
		return errors.New("phaseflow: revision changes nothing (empty deliverable and test)")
	}
	r.Round = len(t.RevisionRounds) + 1
	if r.At.IsZero() {
		r.At = time.Now()
	}
	// Carry the attempts that prompted this revision into its record before
	// clearing the live list. The next pass needs a fresh count, but the run
	// report needs every attempt ever made, and dropping them here would make
	// it silently under-report any model tried before a revision.
	r.Attempts = append([]Attempt(nil), t.Attempts...)
	t.RevisionRounds = append(t.RevisionRounds, r)
	t.Attempts = nil
	t.Status = StatusPending
	return nil
}
