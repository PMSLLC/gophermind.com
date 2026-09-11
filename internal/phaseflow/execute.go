package phaseflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"gophermind/internal/codeindex"
)

// This file implements the autonomous per-task executor for `/project-execute`
// (Spec 2): pure orchestration over the plan produced by `/project` and
// persisted in assignments.json. It never calls an LLM itself — a TaskRunner
// (e.g. an agent dispatcher) does the actual work; Execute just sequences
// pending tasks, persists status transitions as it goes, and tallies results.

// TaskOutcome records the terminal result of one finished task.
type TaskOutcome struct {
	ID     string
	Status string
	Detail string
}

// RunSummary tallies the outcome of an Execute run.
type RunSummary struct {
	Done      int
	Corrected int
	Failed    int
	// NeedsRevision and Escalated are counted separately rather than folded
	// into Failed. A task that exhausted its candidate models has not failed
	// in the ordinary sense, it is waiting on a revised definition, and a
	// summary that omitted them would not add up to the number of tasks run.
	NeedsRevision int
	Escalated     int
	// ContractFlagged counts tasks that raised StatusContractFlagged: the
	// contract they were building against is wrong or incomplete. See
	// executeOnce for what a flag does to the wave it occurred in.
	ContractFlagged int
	Outcomes        []TaskOutcome
}

// TaskRunner executes a single task and reports its terminal status. Status
// must be one of StatusDone/StatusCorrected/StatusFailed; any other value is
// treated as StatusFailed by Execute. A non-nil err that wraps
// context.Canceled or context.DeadlineExceeded signals the caller wants the
// run stopped rather than the task marked failed.
type TaskRunner interface {
	Run(ctx context.Context, t Task) (status string, detail string, err error)
}

// Execute runs all pending tasks in assignments.json in ascending ID order,
// persisting each task's status transition to disk as it happens so a killed
// or interrupted run leaves an accurate on-disk record. Non-pending tasks
// (already done/failed/corrected from a prior run) are left untouched.
//
// A runner error marks that task failed and execution continues with the
// next task. If ctx is canceled (either observed via ctx.Err() before a task
// runs, or reported by the runner wrapping context.Canceled/
// DeadlineExceeded), the in-flight task is reverted to pending and the loop
// stops immediately, leaving later tasks pending for a future run.
func Execute(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome)) (RunSummary, error) {
	return ExecuteWithRounds(ctx, root, runner, emit, DefaultMaxRounds)
}

// DefaultMaxRounds bounds how many times ExecuteWithRounds re-attempts the
// tasks that failed, so an unattended run finishes rather than spinning.
const DefaultMaxRounds = 3

// DefaultWaveConcurrency bounds how many tasks in one wave run at once when
// a caller does not pick a limit explicitly (Execute, ExecuteWithRounds).
// Unbounded fan-out would turn a 50-task wave into 50 simultaneous calls
// against free-tier model providers, tripping every rate limit at once -
// exactly the failure mode routing across several free providers exists to
// avoid. 4 is small enough to stay well under a single free-tier provider's
// per-minute request limit even when every task in flight happens to land
// on the same provider, while still giving a meaningfully wide wave real
// concurrency instead of degrading to one-at-a-time.
const DefaultWaveConcurrency = 4

// ExecuteWithRounds runs passes over the plan until every task is accounted
// for, retrying failures with their failure detail fed back to the runner. It
// stops at the first of: no failures left, a round that fixed nothing (further
// retries would only repeat themselves), or maxRounds.
//
// The failure note is handed to the runner on the retry attempt but never
// persisted, so assignments.json keeps the plan the user approved. Tasks
// within a wave run concurrently, bounded by DefaultWaveConcurrency; see
// ExecuteWithConcurrency to choose a different limit.
func ExecuteWithRounds(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), maxRounds int) (RunSummary, error) {
	return ExecuteWithConcurrency(ctx, root, runner, emit, maxRounds, DefaultWaveConcurrency)
}

// ExecuteWithConcurrency is ExecuteWithRounds with an explicit per-wave
// concurrency limit instead of DefaultWaveConcurrency. waveConcurrency below
// 1 is treated as 1 (fully sequential).
func ExecuteWithConcurrency(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), maxRounds, waveConcurrency int) (RunSummary, error) {
	if maxRounds < 1 {
		maxRounds = 1
	}
	if waveConcurrency < 1 {
		waveConcurrency = 1
	}
	var summary RunSummary
	lastDetail := map[string]string{}
	// Only tasks this run actually attempted are tallied; work left done by an
	// earlier run must not inflate this run's summary.
	touched := map[string]bool{}

	for round := 0; round < maxRounds; round++ {
		passSummary, ran, err := executePass(ctx, root, runner, emit, lastDetail, waveConcurrency)
		summary.Outcomes = append(summary.Outcomes, passSummary.Outcomes...)
		for _, o := range passSummary.Outcomes {
			touched[o.ID] = true
		}
		if err != nil {
			return summary, err
		}
		if !ran || ctx.Err() != nil {
			break
		}

		progressed := passSummary.Done + passSummary.Corrected
		if passSummary.Failed == 0 || progressed == 0 || round == maxRounds-1 {
			break
		}
		// Another round is coming: return the failures to pending so the next
		// pass picks them up.
		if err := resetFailedToPending(root); err != nil {
			return summary, err
		}
	}

	// Counts reflect the plan's final state, not the sum of every attempt, so a
	// task that failed once and then succeeded counts once, as done.
	final, _, err := LoadAssignments(root)
	if err != nil {
		return summary, err
	}
	for _, t := range final.Tasks {
		if !touched[t.ID] {
			continue
		}
		switch t.Status {
		case StatusDone:
			summary.Done++
		case StatusCorrected:
			summary.Corrected++
		case StatusFailed:
			summary.Failed++
		case StatusNeedsRevision:
			summary.NeedsRevision++
		case StatusEscalated:
			summary.Escalated++
		case StatusContractFlagged:
			summary.ContractFlagged++
		}
	}
	return summary, nil
}

// resetFailedToPending requeues failed tasks for the next retry round. It
// matches StatusFailed exactly and so, deliberately, leaves
// StatusNeedsRevision and StatusEscalated tasks alone: those are not
// ordinary failures, and requeuing one would re-run it against the same
// exhausted candidate model list for an identical result. See
// normalizeStatus, which is what keeps such a task from ever becoming
// StatusFailed in the first place.
func resetFailedToPending(root string) error {
	a, _, err := LoadAssignments(root)
	if err != nil {
		return err
	}
	for i, t := range a.Tasks {
		if t.Status == StatusFailed {
			a.Tasks[i].Status = StatusPending
		}
	}
	return a.Save(root)
}

// executePass runs every currently-pending task once, wave by wave. It
// reports whether any task ran, so the caller can stop when the plan is
// exhausted.
func executePass(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), lastDetail map[string]string, waveConcurrency int) (RunSummary, bool, error) {
	summary, err := executeOnce(ctx, root, runner, emit, lastDetail, waveConcurrency)
	return summary, len(summary.Outcomes) > 0, err
}

// executeOnce groups pending tasks by wave and runs each wave's tasks
// concurrently (bounded by waveConcurrency), waiting for the whole wave to
// finish before starting the next one in ascending wave order.
//
// Wave 0 - the zero value of Task.Wave - is never batched: it is both "no
// wave assigned" (a plan written before AssignWaves existed, or a task a
// planner never ran through it) and the contract step's own wave, and both
// meanings want the same behaviour, one task at a time, so a wave-0 group
// always runs at an effective concurrency of 1 regardless of
// waveConcurrency. This is what keeps every pre-existing
// execute_*_test.go fixture (whose tasks never set Wave, so all default to
// 0) running exactly as it did before this file gained concurrency: they
// degenerate to a sequence of size-one waves, identical in order and
// timing to the old plain per-task loop.
//
// Every status mutation for a task goes through Update, never a whole-file
// Save built from a stale in-memory read - once a wave has more than one
// task in flight, two of them can finish at nearly the same instant, and
// only Update's locked read-modify-write makes that safe.
func executeOnce(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), lastDetail map[string]string, waveConcurrency int) (RunSummary, error) {
	if waveConcurrency < 1 {
		waveConcurrency = 1
	}

	a, found, err := LoadAssignments(root)
	if err != nil {
		return RunSummary{}, err
	}
	if !found {
		return RunSummary{}, fmt.Errorf("phaseflow: no assignments found at %s", AssignmentsPath(root))
	}

	recovered := false
	for i, t := range a.Tasks {
		if t.Status == StatusRunning {
			a.Tasks[i].Status = StatusPending
			recovered = true
		}
	}
	if recovered {
		if err := Update(root, func(as *Assignments) error {
			for i := range as.Tasks {
				if as.Tasks[i].Status == StatusRunning {
					as.Tasks[i].Status = StatusPending
				}
			}
			return nil
		}); err != nil {
			return RunSummary{}, err
		}
	}

	pending := make([]int, 0, len(a.Tasks))
	for i, t := range a.Tasks {
		if t.Status == StatusPending {
			pending = append(pending, i)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return a.Tasks[pending[i]].ID < a.Tasks[pending[j]].ID
	})

	// Group by wave, preserving each group's ID order from the sort above.
	waveTasks := map[int][]Task{}
	var waveNums []int
	for _, idx := range pending {
		w := a.Tasks[idx].Wave
		if _, seen := waveTasks[w]; !seen {
			waveNums = append(waveNums, w)
		}
		waveTasks[w] = append(waveTasks[w], a.Tasks[idx])
	}
	sort.Ints(waveNums)

	var summary RunSummary
	for _, w := range waveNums {
		if ctx.Err() != nil {
			return summary, nil
		}

		limit := waveConcurrency
		if w == 0 {
			limit = 1
		}

		cancelled, flagged, err := runWave(ctx, root, runner, emit, lastDetail, waveTasks[w], limit, &summary)
		if err != nil {
			return summary, err
		}
		if cancelled {
			return summary, nil
		}
		if flagged {
			// A task in this wave determined the contract is wrong or
			// incomplete. In-flight siblings were allowed to finish (see
			// runWave); no task not yet started in this wave was
			// dispatched; and no later wave may start on top of a contract
			// already known to be broken - so the pass ends here.
			return summary, nil
		}
	}

	return summary, nil
}

// waveTaskResult is one task's outcome from runWave's worker goroutines,
// fanned in and processed by a single goroutine so summary/lastDetail/emit
// and the best-effort index and context-doc refreshes never see concurrent
// access.
type waveTaskResult struct {
	outcome   TaskOutcome
	cancelled bool
	err       error
}

// runWave runs tasks concurrently, at most limit at a time, and reports
// whether the run was cancelled (in which case every in-flight task has
// already been reverted to pending and the caller must not start the next
// wave) or whether some task raised StatusContractFlagged (in which case no
// task not yet started in this wave was dispatched, every in-flight task was
// allowed to finish normally, and the caller must not start the next wave
// either - see executeOnce). A non-nil error is fatal, matching the old
// loop's behaviour when a disk write failed: the caller stops immediately
// rather than continuing with an assignments.json that may no longer
// reflect reality.
func runWave(ctx context.Context, root string, runner TaskRunner, emit func(TaskOutcome), lastDetail map[string]string, tasks []Task, limit int, summary *RunSummary) (cancelled, flagged bool, err error) {
	if len(tasks) == 0 {
		return false, false, nil
	}

	sem := make(chan struct{}, limit)
	results := make(chan waveTaskResult)
	var stop atomic.Bool

	// lastDetail is also written by the result-processing loop below, on
	// the calling goroutine, as each task in this wave finishes. Reading it
	// from the dispatch goroutine below at the same time would race, and a
	// task's retry note only ever comes from an earlier pass in any case
	// (never from a sibling dispatched in this same wave) - so the dispatch
	// goroutine gets its own frozen copy instead of touching the live map.
	notes := make(map[string]string, len(lastDetail))
	for k, v := range lastDetail {
		notes[k] = v
	}

	go func() {
		var wg sync.WaitGroup
		for _, task := range tasks {
			if stop.Load() || ctx.Err() != nil {
				break
			}

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				close(results)
				return
			}
			if stop.Load() {
				<-sem
				break
			}

			// On a retry round, hand the runner the same task plus what went
			// wrong last time. The copy is deliberate: the note must not
			// reach disk.
			attempt := task
			if note := notes[attempt.ID]; note != "" {
				attempt.AgentAddendum = strings.TrimSpace(attempt.AgentAddendum +
					"\n\nA previous attempt at this task failed with:\n" + note +
					"\nFix that before proceeding.")
			}

			wg.Add(1)
			go func(attempt Task) {
				defer wg.Done()
				results <- runSingleTask(ctx, root, runner, attempt)
			}(attempt)
		}
		wg.Wait()
		close(results)
	}()

	// sem's slot for a finished task is released here, by the result loop,
	// rather than by the worker goroutine itself right after it sends - and
	// always after any stop.Store(true) below. Releasing from the worker
	// would let the dispatch goroutine above win a race: it could acquire
	// the newly-freed slot and dispatch the next task before this loop had
	// a chance to record that the wave must stop, so a cancellation or
	// fatal error on task N could let task N+1 start anyway. The dispatch
	// loop's own re-check of stop right after acquiring a slot only closes
	// that gap if the slot was not released until stop was already visible.
	for res := range results {
		if res.err != nil {
			stop.Store(true)
			err = res.err
			<-sem
			continue
		}
		if res.cancelled {
			stop.Store(true)
			cancelled = true
			<-sem
			continue
		}
		if res.outcome.Status == StatusContractFlagged {
			// Set stop before releasing the slot, for the same reason as
			// the err/cancelled branches above: a task dispatched after
			// this point would start against a contract already known to
			// be wrong. Unlike those branches, the flagged outcome itself
			// still falls through below to be recorded and emitted - the
			// flag is a distinguishable event, not a failure to swallow.
			flagged = true
			stop.Store(true)
		}
		<-sem
		if err != nil || cancelled {
			// A fatal error or cancellation was already seen; drain the
			// remaining in-flight results without acting on them further.
			continue
		}

		if res.outcome.Status == StatusFailed {
			lastDetail[res.outcome.ID] = res.outcome.Detail
		} else {
			delete(lastDetail, res.outcome.ID)
		}

		// Refresh the symbol index and context doc after each task, same as
		// the old sequential loop did - just fanned in here to a single
		// goroutine so concurrent siblings never race on the same files.
		// Best-effort by design: neither is a gate on the task's own result.
		_, _ = codeindex.BuildAndWrite(root)
		if reloaded, ok, loadErr := LoadAssignments(root); loadErr == nil && ok {
			_ = UpsertContextDoc(root, RenderContextDocBody(projectNameFor(root), &reloaded, res.outcome))
		}

		summary.Outcomes = append(summary.Outcomes, res.outcome)
		switch res.outcome.Status {
		case StatusDone:
			summary.Done++
		case StatusCorrected:
			summary.Corrected++
		case StatusFailed:
			summary.Failed++
		case StatusNeedsRevision:
			summary.NeedsRevision++
		case StatusEscalated:
			summary.Escalated++
		case StatusContractFlagged:
			summary.ContractFlagged++
		}
		if emit != nil {
			emit(res.outcome)
		}
	}

	return cancelled, flagged, err
}

// runSingleTask runs one task to a terminal state (or reports cancellation),
// persisting every status transition through Update. It is safe to call
// from multiple goroutines at once: each call only ever touches the task
// with attempt.ID under Update's lock, never a shared in-memory Assignments.
func runSingleTask(ctx context.Context, root string, runner TaskRunner, attempt Task) waveTaskResult {
	id := attempt.ID

	if err := updateTaskStatus(root, id, StatusRunning); err != nil {
		return waveTaskResult{err: err}
	}

	// FallbackRunner reports each attempt through OnAttempt as it happens.
	// Collecting them into a local slice keeps this goroutine's attempts its
	// own, so two tasks in a wave calling Run on the same runner cannot
	// interfere. The runner holds no per-call state, so it needs no copy.
	var attempts []Attempt
	callRunner := runner
	if orig, ok := runner.(*FallbackRunner); ok {
		cp := *orig
		cp.OnAttempt = func(a Attempt) { attempts = append(attempts, a) }
		callRunner = &cp
	}

	status, detail, runErr := callRunner.Run(ctx, attempt)

	if len(attempts) > 0 {
		if err := updateTaskAttempts(root, id, attempts); err != nil {
			return waveTaskResult{err: err}
		}
	}

	if ctx.Err() != nil || isCancel(runErr) {
		if err := updateTaskStatus(root, id, StatusPending); err != nil {
			return waveTaskResult{err: err}
		}
		return waveTaskResult{cancelled: true}
	}

	final := normalizeStatus(status)
	if runErr != nil {
		final = StatusFailed
		detail = runErr.Error()
	}

	if err := updateTaskStatus(root, id, final); err != nil {
		return waveTaskResult{err: err}
	}

	return waveTaskResult{outcome: TaskOutcome{ID: id, Status: final, Detail: detail}}
}

// updateTaskStatus sets task id's status under Update's lock.
func updateTaskStatus(root, id, status string) error {
	return Update(root, func(a *Assignments) error {
		for i := range a.Tasks {
			if a.Tasks[i].ID == id {
				a.Tasks[i].Status = status
				return nil
			}
		}
		return fmt.Errorf("phaseflow: task %q not found while setting status %q", id, status)
	})
}

// updateTaskAttempts appends newAttempts to task id's history under
// Update's lock.
func updateTaskAttempts(root, id string, newAttempts []Attempt) error {
	return Update(root, func(a *Assignments) error {
		for i := range a.Tasks {
			if a.Tasks[i].ID == id {
				for _, at := range newAttempts {
					a.Tasks[i].RecordAttempt(at)
				}
				return nil
			}
		}
		return fmt.Errorf("phaseflow: task %q not found while recording attempts", id)
	})
}

// isCancel reports whether err represents a run-stopping cancellation rather
// than an ordinary task failure.
func isCancel(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

// normalizeStatus treats any status other than done/corrected/needs_revision/
// escalated/contract_flagged as failed. StatusNeedsRevision and
// StatusEscalated must pass through unchanged: a task in either state
// already ran its candidate models to exhaustion (see FallbackRunner), and
// folding it into StatusFailed here would make resetFailedToPending requeue
// it for an identical, quota-burning re-run. StatusContractFlagged must
// pass through unchanged for the same reason: it is a distinguishable
// event, not an ordinary failure, and requeuing it would silently work
// around the very flag the task raised instead of pausing on it.
func normalizeStatus(status string) string {
	switch status {
	case StatusDone, StatusCorrected, StatusNeedsRevision, StatusEscalated, StatusContractFlagged:
		return status
	default:
		return StatusFailed
	}
}

// projectNameFor labels the context doc with the project directory's name, so
// the doc is never anonymous.
func projectNameFor(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		return filepath.Base(abs)
	}
	return filepath.Base(root)
}
