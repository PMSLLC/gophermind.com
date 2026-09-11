package serve

import (
	"context"
	"time"

	"gophermind/internal/phaseflow"
	"gophermind/internal/watch"
)

// pipelineWatchInterval is how often the watcher restats the assignments
// file. Task attempts take seconds at minimum (each is an LLM call), so a
// sub-second poll would buy nothing but wakeups, and a stat on an unchanged
// file is cheap enough that this is not worth making configurable.
const pipelineWatchInterval = 750 * time.Millisecond

// StartPipelineWatcher publishes live pipeline events by watching the
// project's assignments file, and returns immediately. It stops when ctx is
// cancelled.
//
// Watching a file rather than being called directly is deliberate, and is
// the only thing that can work here. A run executes in a different process
// from the one serving this dashboard: `gophermind phase execute` and the
// TUI's /project-execute both mutate .planning/assignments.json from their
// own process, so an in-process callback on the hub could never see them.
// Without this, /pipeline/events opened a stream that stayed silent forever
// and the dashboard showed whatever state happened to exist when the page
// loaded.
//
// The file is also exactly what GET /pipeline/state serves, so the live
// stream and a fresh page load cannot disagree about what happened.
//
// A nil hub or an empty root disables the watcher.
func StartPipelineWatcher(ctx context.Context, root string, hub *PipelineHub) {
	if hub == nil || root == "" {
		return
	}
	go watchPipeline(ctx, root, hub, pipelineWatchInterval)
}

// watchPipeline is StartPipelineWatcher's loop, with the poll interval
// injectable so tests do not have to wait on the production cadence.
func watchPipeline(ctx context.Context, root string, hub *PipelineHub, interval time.Duration) {
	path := phaseflow.AssignmentsPath(root)

	// prev is the last state successfully observed. It starts empty, so the
	// first read after startup publishes nothing: the dashboard fetches
	// /pipeline/state on load and would otherwise be told, as "news", about
	// every task that finished before it connected.
	var (
		prev     map[string]phaseflow.Task
		prevSeen bool
		lastMod  time.Time
		lastWave = -1
		reported bool
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		changed, mod, err := watch.Changed(path, lastMod)
		if err != nil || !changed {
			// A missing file is the normal "no plan yet" case and a
			// transient read error is not worth a frame; either way the
			// next tick tries again.
			continue
		}
		lastMod = mod

		a, found, err := phaseflow.LoadAssignments(root)
		if err != nil || !found {
			continue
		}

		cur := make(map[string]phaseflow.Task, len(a.Tasks))
		for _, t := range a.Tasks {
			cur[t.ID] = t
		}

		if prevSeen {
			publishDiff(hub, prev, a.Tasks)

			// The active wave is the lowest wave that still has unfinished
			// work; announcing it only when it moves keeps wave-changed a
			// signal rather than a heartbeat.
			if w := activeWave(a.Tasks); w != lastWave {
				lastWave = w
				if w >= 0 {
					hub.WaveChanged(w, "running")
				}
			}

			// A run is over when nothing is left to do. The report goes out
			// once per run; a later change that reopens work arms it again.
			if done := runFinished(a.Tasks); done && !reported {
				reported = true
				hub.RunReport(phaseflow.BuildRunReport(a.Tasks, time.Now()))
			} else if !done {
				reported = false
			}
		} else {
			lastWave = activeWave(a.Tasks)
			reported = runFinished(a.Tasks)
		}

		prev, prevSeen = cur, true
	}
}

// publishDiff emits one frame per actual change between prev and cur: a
// status that moved, and every attempt that was appended since the last
// observation. A task absent from prev is new, and its status is announced
// but its pre-existing attempts are not replayed one by one.
func publishDiff(hub *PipelineHub, prev map[string]phaseflow.Task, cur []phaseflow.Task) {
	for _, t := range cur {
		before, existed := prev[t.ID]
		if !existed {
			hub.TaskStatus(t.ID, t.Status, t.Wave)
			continue
		}
		for i := len(before.Attempts); i < len(t.Attempts); i++ {
			hub.TaskAttempt(t.ID, t.Attempts[i])
		}
		if before.Status != t.Status {
			hub.TaskStatus(t.ID, t.Status, t.Wave)
		}
	}
}

// activeWave returns the lowest wave with a task still to finish, or -1 when
// every task is in a terminal state.
func activeWave(tasks []phaseflow.Task) int {
	active := -1
	for _, t := range tasks {
		if terminalStatus(t.Status) {
			continue
		}
		if active == -1 || t.Wave < active {
			active = t.Wave
		}
	}
	return active
}

// runFinished reports whether every task has reached a terminal state.
func runFinished(tasks []phaseflow.Task) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if !terminalStatus(t.Status) {
			return false
		}
	}
	return true
}

// terminalStatus reports whether a status means the engine will not pick the
// task up again on its own. needs_revision and contract_flagged count as
// terminal here because both wait on something outside the run loop - a
// reviser or a human - rather than on another automatic attempt.
func terminalStatus(s string) bool {
	switch s {
	case phaseflow.StatusDone, phaseflow.StatusCorrected, phaseflow.StatusFailed,
		phaseflow.StatusNeedsRevision, phaseflow.StatusEscalated, phaseflow.StatusContractFlagged:
		return true
	}
	return false
}
