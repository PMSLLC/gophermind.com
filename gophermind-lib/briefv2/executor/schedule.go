package executor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	body       sync.Mutex // held for the whole of one leaf body in a serial level, and by the turn holder of a parallel level
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
	main    bool              // a parallel level runs it in the main tree in its turn (needsMain)
	ran     bool
	out     leafOutcome
	err     error
}

// runnable says the job is to run: not blocked, not already finished.
func runnable(j *job) bool {
	return j.blocker == "" && j.status != blackboard.StatusVerified && j.status != blackboard.StatusFailed
}

// workerCount is how many leaves a level runs at once: executor.workers capped
// by the jobs that may run in a private tree. Below 2 the level runs as before,
// one leaf after the other in the main tree, with no copy.
func (rc *runCtx) workerCount(jobs []*job) int {
	n := 0
	for _, j := range jobs {
		if runnable(j) && !j.main {
			n++
		}
	}
	return min(rc.cfg.Executor.Workers, n)
}

// splitOverlaps puts leaves of a level whose File, StubFile or TestFile is the
// same path into different sub-levels, the higher id later (spec section 4):
// they cannot be isolated from each other. First fit, id order kept inside
// each sub-level. Used only when executor.workers > 1 (ruling 12).
func splitOverlaps(level []*Leaf) [][]*Leaf {
	var subs [][]*Leaf
	var used []map[string]bool
	for _, l := range level {
		paths := []string{l.File, l.StubFile, l.TestFile}
		placed := false
		for i := range subs {
			clash := false
			for _, p := range paths {
				clash = clash || used[i][p]
			}
			if !clash {
				subs[i] = append(subs[i], l)
				for _, p := range paths {
					used[i][p] = true
				}
				placed = true
				break
			}
		}
		if !placed {
			m := map[string]bool{}
			for _, p := range paths {
				m[p] = true
			}
			subs = append(subs, []*Leaf{l})
			used = append(used, m)
		}
	}
	return subs
}

// needsMain says a leaf of a parallel level must run in the main tree in its
// turn (ruling 5): the gate is to be asked again, a real file is on disk to
// adopt, a repair left a reopen marker, or a candidate waits to land
// (resumeCandidate runs only in the main tree). The candidate is read as
// resumeCandidate reads it: a file readCandidate would drop does not count.
func (rc *runCtx) needsMain(l *Leaf, status blackboard.Status) (bool, error) {
	if status == blackboard.StatusEscalated || status == blackboard.StatusNeedsRevision {
		return true, nil
	}
	if _, found, err := readCandidate(rc.o.RunDir, l.ID); err != nil || found {
		return true, err
	}
	if _, marked, err := readReopenMarker(rc.o.RunDir, l.ID); err != nil || marked {
		return true, err
	}
	st, err := NewSwap(rc.o.Repo, l, rc.git, nil).State()
	if err != nil {
		return false, err
	}
	return st != SwapStubOnly, nil
}

// levelWorkers decides how a level runs. With executor.workers above 1 it marks
// the jobs that need the main tree; when at least two may run in private trees
// it copies the level's base and emits level_parallel, which counts every
// runnable job of the level, main jobs included (ruling C16). A base that
// cannot be made (over 256 MiB, a link out of the tree, a full disk) makes the
// level serial with a warning: nothing is lost (ruling 4, the base copy only).
func (rc *runCtx) levelWorkers(jobs []*job, wave, level int) (int, error) {
	if rc.cfg.Executor.Workers <= 1 {
		return 1, nil
	}
	for _, j := range jobs {
		if !runnable(j) {
			continue
		}
		m, err := rc.needsMain(j.l, j.status)
		if err != nil {
			return 0, err
		}
		j.main = m
	}
	serial := func() (int, error) {
		for _, j := range jobs {
			j.main = false
		}
		return 1, nil
	}
	n := rc.workerCount(jobs)
	if n < 2 {
		return serial()
	}
	if err := rc.snapshotLevel(); err != nil {
		rc.emit("warning", "", "this level runs one leaf at a time: "+strings.TrimPrefix(err.Error(), "executor: "))
		rc.endLevel()
		return serial()
	}
	leaves := 0
	for _, j := range jobs {
		if runnable(j) {
			leaves++
		}
	}
	rc.emit("level_parallel", "", fmt.Sprintf("wave %d level %d: %d leaves, %d workers", wave, level, leaves, n))
	rc.noteParallel()
	return n, nil
}

// noteParallel counts one parallel level for the report.
func (rc *runCtx) noteParallel() {
	repMu.Lock()
	rc.rep.parallelLevels++
	repMu.Unlock()
}

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

	var firstErr error
	level := 0
	for _, lv := range levels(leaves) {
		subs := [][]*Leaf{lv}
		if rc.cfg.Executor.Workers > 1 {
			subs = splitOverlaps(lv)
		}
		for _, sub := range subs {
			if wr.Stop != nil || firstErr != nil {
				break
			}
			level++
			jobs, err := rc.classify(ctx, sub, &wr)
			if err != nil {
				return wr, err
			}
			workers, err := rc.levelWorkers(jobs, sub[0].Wave, level)
			if err != nil {
				return wr, err
			}
			var stop bool
			rc.runJobs(ctx, jobs, workers, &wr, &stop)
			if workers > 1 {
				rc.endLevel()
			}
			for _, j := range jobs {
				if err := rc.merge(ctx, j, &wr); err != nil && firstErr == nil {
					firstErr = err
				}
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
// after the other in the main tree. With more it starts them in id order, at
// most `workers` at a time, each with a turn that follows the previous job's:
// private jobs work in their own tree and land in their turn; main jobs wait
// for their turn first. A landing commit that fails sets the level's fault
// flag, and every job behind it ends interrupted with its candidate kept
// (ruling C5, spec section 7.3); any other harness fault only stops claiming.
func (rc *runCtx) runJobs(ctx context.Context, jobs []*job, workers int, wr *waveResult, stop *bool) {
	var mu sync.Mutex
	var fault atomic.Bool // a landing commit of this level failed
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
	var prev <-chan struct{} // done of the previous job that ran (parallel levels)
	for _, j := range jobs {
		if !runnable(j) {
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
		var tn *turn
		if workers > 1 {
			tn = newTurn(prev, &rc.sched.body)
			tn.fault = &fault
			prev = tn.done
		}
		run := func() {
			defer func() { <-sem }()
			if tn == nil {
				rc.sched.body.Lock()
				defer rc.sched.body.Unlock()
				j.out, j.err = rc.runJob(ctx, j)
			} else {
				defer tn.release()
				j.out, j.err = rc.runParallelJob(ctx, j, tn)
			}
			var se *stopError
			var ce *commitError
			switch {
			case errors.As(j.err, &se):
				halt(se)
			case j.err != nil:
				if errors.As(j.err, &ce) {
					fault.Store(true) // before the deferred release lets the next job go
				}
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

// runJob is one leaf in the main tree: the gate again for a leaf an earlier
// run left escalated, else the leaf.
func (rc *runCtx) runJob(ctx context.Context, j *job) (leafOutcome, error) {
	if j.status == blackboard.StatusEscalated || j.status == blackboard.StatusNeedsRevision {
		return rc.resumeEscalated(ctx, j.l, j.status)
	}
	return rc.runLeaf(ctx, j.l, leafIn{})
}

// runParallelJob is one leaf of a parallel level. A main job waits for its
// turn and holds it for the whole leaf, as runJob runs it (ruling 5: toMain
// does nothing in the main tree, so the turn is taken here). Any other gets a
// private tree, removed when the leaf ends, and lands in its turn. A wait that
// ends with the context, or behind a failed landing (errLandingFault), ends the
// job interrupted and unclaimed, its candidate kept. A makeTree failure or a
// panic outside the leaf body (tree setup, landing glue) is a harness fault of
// this job only (spec section 10): the tree is removed, the caller releases
// the turn, and the process lives.
func (rc *runCtx) runParallelJob(ctx context.Context, j *job, tn *turn) (out leafOutcome, err error) {
	defer func() {
		if r := recover(); r != nil {
			rc.emit("warning", j.l.ID, "a panic ended the job outside the leaf")
			out, err = leafOutcome{}, fmt.Errorf("executor: the job of leaf %s panicked", j.l.ID)
		}
	}()
	if j.main {
		if werr := tn.wait(ctx); werr != nil {
			return leafOutcome{Interrupted: true}, nil
		}
		return rc.runJob(ctx, j)
	}
	wt, terr := rc.makeTree(j.l.ID)
	if terr != nil {
		return leafOutcome{}, terr // never claimed: the leaf stays ready
	}
	defer rc.dropTree(wt)
	rc.emit("tree_ready", j.l.ID, "private tree ready")
	if rc.afterTree != nil {
		rc.afterTree(j.l.ID)
	}
	return rc.runLeaf(ctx, j.l, leafIn{Tree: wt, Turn: tn})
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
