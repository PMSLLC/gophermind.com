package executor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
)

// maxLeafInterrupts is how many times one run may find a leaf interrupted
// (context ended, or no provider could answer) before the scheduler refuses to
// claim it again. One: an interrupt stops the scheduler at once, and a caller
// that calls it again for the same leaf in the same run gets the fixed stop
// below instead of another claim. The leaf stays claimable for the next run.
const maxLeafInterrupts = 1

// staleHeartbeats is how many heartbeat intervals a claim may go without a
// heartbeat before the sweep at the start of a wave releases it.
const staleHeartbeats = 3

// waveResult is what scheduleLeaves did with one wave. Every leaf it was given
// is named in exactly one list, or left as it was because Stop ended the wave
// before its turn (it is then still pending or ready and never counted done).
type waveResult struct {
	Verified, Failed, Escalated []string
	Blocked                     map[string]string // leaf id -> id of the dependency that blocks it
	Stop                        *stopError
}

// schedState is the scheduler's memory for one run.
type schedState struct {
	mu         sync.Mutex
	interrupts map[string]int
	body       sync.Mutex // held for the whole of one leaf body: two never run at once
}

func (s *schedState) interrupted(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interrupts[id]
}

func (s *schedState) markInterrupted(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.interrupts == nil {
		s.interrupts = map[string]int{}
	}
	s.interrupts[id]++
}

// interruptStop is the stop for an interrupted leaf. The reason is a class:
// empty becomes "cancelled".
func interruptStop(reason string) *stopError {
	if reason == "" {
		reason = "cancelled"
	}
	return &stopError{Status: "interrupted", Reason: reason,
		Message: "executor: the run was interrupted (" + reason + "); the leaf is left claimable for the next run"}
}

// job is one leaf of a level and what became of it.
type job struct {
	l       *Leaf
	status  blackboard.Status // the row when the level began
	blocker string            // the dependency that blocks the leaf
	ran     bool
	out     leafOutcome
	err     error
}

// workerCount is the number of leaves run at once: always 1. The leaf loops
// share one working tree (the stray-file check and the stub swap look at it
// whole) and the spec allows one model call at a time, so executor.workers is
// refused above 1 by settings validation and ignored here. The job structure
// stays for the day leaves get isolated trees.
func (rc *runCtx) workerCount(leaves []*Leaf) int { return 1 }

// levels orders the leaves: by dependency among themselves first, then by id.
// Leaves of one wave have no dependency on each other, so a wave is one level.
func levels(leaves []*Leaf) [][]*Leaf {
	in := map[string]*Leaf{}
	for _, l := range leaves {
		in[l.ID] = l
	}
	depth := map[string]int{}
	var walk func(id string, guard int) int
	walk = func(id string, guard int) int {
		if d, ok := depth[id]; ok {
			return d
		}
		d := 0
		if guard < len(leaves) {
			for _, dep := range in[id].DependsOn {
				if _, ok := in[dep]; ok {
					if x := walk(dep, guard+1) + 1; x > d {
						d = x
					}
				}
			}
		}
		depth[id] = d
		return d
	}
	var out [][]*Leaf
	for _, l := range leaves {
		d := walk(l.ID, 0)
		for len(out) <= d {
			out = append(out, nil)
		}
		out[d] = append(out[d], l)
	}
	for _, lv := range out {
		sort.Slice(lv, func(i, j int) bool { return lv[i].ID < lv[j].ID })
	}
	return out
}

// scheduleLeaves runs the leaves of one wave to a terminal status, in
// dependency order and then id order, and says what became of each:
//
//   - a leaf with a dependency that is not verified is not claimed, stays
//     pending, is named in Blocked and in one blocked event;
//   - a leaf the blackboard already holds verified or failed is counted as such
//     (a resumed run), an escalated one asks the gate again (spec S8);
//   - every other leaf is made ready and run (runLeaf), at most
//     executor.workers at a time, claims taken in id order, results merged in
//     id order;
//   - the context is checked before every claim;
//   - a stop error, an interrupted leaf or a claim still held by another worker
//     stops claiming, lets in-flight leaves finish and is returned in Stop. An
//     interrupted leaf is never a failed one and is never claimed again in this
//     run (maxLeafInterrupts).
//
// Before the first claim the claims whose heartbeat has gone stale (a crashed
// worker) are released. The error is for a fault of the harness; a *stopError
// is in Stop.
func (rc *runCtx) scheduleLeaves(ctx context.Context, leaves []*Leaf) (waveResult, error) {
	wr := waveResult{Blocked: map[string]string{}}
	if len(leaves) == 0 {
		return wr, nil
	}
	if ctx.Err() != nil {
		wr.Stop = interruptStop("")
		return wr, nil
	}
	runID := rc.plan.RunID
	every := rc.heartbeatEvery()
	if every <= 0 {
		every = 30 * time.Second
	}
	released, err := rc.o.Board.ReleaseStale(ctx, runID, staleHeartbeats*every)
	if err != nil {
		if ctx.Err() != nil {
			wr.Stop = interruptStop("")
			return wr, nil
		}
		return wr, errors.New("executor: releasing stale claims failed")
	}
	for _, id := range released {
		rc.emit("warning", id, "a claim with no heartbeat was released")
	}

	workers := rc.workerCount(leaves)
	var firstErr error
	for _, lv := range levels(leaves) {
		if wr.Stop != nil || firstErr != nil {
			break
		}
		jobs, err := rc.classify(ctx, lv, &wr)
		if err != nil {
			return wr, err
		}
		var stop bool
		rc.runJobs(ctx, jobs, workers, &wr, &stop)
		for _, j := range jobs {
			if err := rc.merge(ctx, j, &wr); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return wr, firstErr
}

// classify reads every leaf of a level and decides what it is: blocked,
// already terminal, held by another worker, or to run.
func (rc *runCtx) classify(ctx context.Context, level []*Leaf, wr *waveResult) ([]*job, error) {
	var jobs []*job
	for _, l := range level {
		row, err := rc.o.Board.Get(ctx, rc.plan.RunID, l.ID)
		if err != nil {
			return nil, fmt.Errorf("executor: reading the row of %s failed", l.ID)
		}
		j := &job{l: l, status: row.Status}
		deps := append([]string(nil), l.DependsOn...)
		sort.Strings(deps)
		for _, d := range deps {
			if rc.plan.Leaf(d) == nil {
				continue
			}
			dr, err := rc.o.Board.Get(ctx, rc.plan.RunID, d)
			if err != nil {
				return nil, fmt.Errorf("executor: reading the row of %s failed", d)
			}
			if dr.Status != blackboard.StatusVerified {
				j.blocker = d
				break
			}
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

// runJobs runs the jobs that are to run. With one worker it runs them one
// after the other in order; with more it starts them in order, at most
// `workers` at a time.
func (rc *runCtx) runJobs(ctx context.Context, jobs []*job, workers int, wr *waveResult, stop *bool) {
	var mu sync.Mutex
	stopped := func() bool { mu.Lock(); defer mu.Unlock(); return *stop }
	halt := func(st *stopError) {
		mu.Lock()
		defer mu.Unlock()
		*stop = true
		if wr.Stop == nil && st != nil {
			wr.Stop = st
		}
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, j := range jobs {
		if j.blocker != "" || j.status == blackboard.StatusVerified || j.status == blackboard.StatusFailed {
			continue
		}
		sem <- struct{}{}
		if stopped() {
			<-sem
			break
		}
		if ctx.Err() != nil {
			<-sem
			halt(interruptStop(""))
			break
		}
		if rc.sched.interrupted(j.l.ID) >= maxLeafInterrupts {
			<-sem
			rc.emit("interrupted", j.l.ID, "interrupted_repeatedly")
			halt(&stopError{Status: "interrupted", Reason: "interrupted_repeatedly",
				Message: fmt.Sprintf("executor: leaf %s was interrupted earlier in this run and is not claimed again", j.l.ID)})
			break
		}
		if j.status == blackboard.StatusClaimed || j.status == blackboard.StatusInProgress {
			<-sem
			rc.emit("interrupted", j.l.ID, "claim_held")
			halt(&stopError{Status: "interrupted", Reason: "claim_held",
				Message: fmt.Sprintf("executor: leaf %s is claimed by another worker whose heartbeat is current", j.l.ID)})
			break
		}
		if j.status == blackboard.StatusPending {
			if err := rc.o.Board.SetStatus(ctx, rc.plan.RunID, j.l.ID, blackboard.StatusPending, blackboard.StatusReady); err != nil {
				<-sem
				j.err = fmt.Errorf("executor: making %s ready failed", j.l.ID)
				halt(nil)
				break
			}
		}
		j.ran = true
		run := func() {
			defer func() { <-sem }()
			rc.sched.body.Lock()
			defer rc.sched.body.Unlock()
			if j.status == blackboard.StatusEscalated || j.status == blackboard.StatusNeedsRevision {
				j.out, j.err = rc.resumeEscalated(ctx, j.l, j.status)
			} else {
				j.out, j.err = rc.runLeaf(ctx, j.l, leafIn{})
			}
			var se *stopError
			switch {
			case errors.As(j.err, &se):
				halt(se)
			case j.err != nil:
				halt(nil)
			case j.out.Interrupted:
				rc.sched.markInterrupted(j.l.ID)
				rc.emit("interrupted", j.l.ID, j.out.Reason)
				halt(interruptStop(j.out.Reason))
			}
		}
		if workers == 1 {
			run()
		} else {
			wg.Add(1)
			go func() { defer wg.Done(); run() }()
		}
	}
	wg.Wait()
}

// merge puts one job's result in the wave result. Called in id order.
func (rc *runCtx) merge(ctx context.Context, j *job, wr *waveResult) error {
	id := j.l.ID
	switch {
	case j.blocker != "":
		wr.Blocked[id] = j.blocker
		repMu.Lock()
		rc.rep.blocked[id] = j.blocker
		repMu.Unlock()
		rc.emit("blocked", id, "blocked by "+j.blocker)
		return nil
	case !j.ran:
		switch j.status {
		case blackboard.StatusVerified:
			wr.Verified = append(wr.Verified, id)
		case blackboard.StatusFailed:
			wr.Failed = append(wr.Failed, id)
		}
		return nil
	}
	var se *stopError
	if j.err != nil && !errors.As(j.err, &se) {
		return j.err
	}
	status := j.out.Status
	if se != nil && status == "" {
		// A stop that left no outcome: the row says where the leaf is.
		if row, err := rc.o.Board.Get(ctx, rc.plan.RunID, id); err == nil {
			status = row.Status
		}
	}
	switch status {
	case blackboard.StatusVerified:
		wr.Verified = append(wr.Verified, id)
	case blackboard.StatusFailed:
		wr.Failed = append(wr.Failed, id)
		rc.keepReason(id, j.out.Reason)
	case blackboard.StatusEscalated:
		wr.Escalated = append(wr.Escalated, id)
		rc.keepReason(id, j.out.Reason)
	}
	return nil
}

// keepReason makes sure the report has the reason of a leaf that ended failed
// or escalated.
func (rc *runCtx) keepReason(id, reason string) {
	if reason == "" {
		return
	}
	repMu.Lock()
	defer repMu.Unlock()
	rc.rep.reasons[id] = reason
}

// resumeEscalated asks the gate again about a leaf an earlier run left
// escalated (ruling S8): the same question, with the attempts on the
// blackboard as its history. retry runs the leaf, skip fails it, stop and a
// gate with no answer end the run.
func (rc *runCtx) resumeEscalated(ctx context.Context, l *Leaf, from blackboard.Status) (leafOutcome, error) {
	row, err := rc.o.Board.Get(ctx, rc.plan.RunID, l.ID)
	if err != nil {
		return leafOutcome{}, fmt.Errorf("executor: reading the row of %s failed", l.ID)
	}
	var history []string
	for i, a := range row.Attempts {
		history = append(history, historyLine(i+1, a))
	}
	// The class the leaf escalated with comes from the state file, which
	// survives a restart; the report's memory does not.
	results, err := LoadLeafResults(rc.o.RunDir)
	if err != nil {
		return leafOutcome{}, err
	}
	reason := results[l.ID].Reason
	if reason == "" {
		reason = "escalated" // the result was never written: the crash came before it
	}
	res, err := rc.escalate(ctx, l, from, reason, history)
	if err != nil {
		var se *stopError
		if errors.As(err, &se) {
			return leafOutcome{}, err
		}
		if ctx.Err() != nil {
			return leafOutcome{Interrupted: true}, nil
		}
		return leafOutcome{}, err
	}
	switch res.Action {
	case human.ActionSkip:
		rc.emit("leaf_failed", l.ID, reasonSkipped)
		return leafOutcome{Status: blackboard.StatusFailed, Reason: reasonSkipped}, nil
	case human.ActionRetry:
		return rc.runLeaf(ctx, l, leafIn{})
	}
	return leafOutcome{}, humanStop(l.ID)
}
