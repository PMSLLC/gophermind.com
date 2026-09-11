package phaseflow

import (
	"sort"
	"time"
)

// This file implements the end-of-run model performance summary (pipeline
// piece 5, see docs/superpowers/specs/2026-09-11-harness-pipeline-design.md,
// from source spec section 5). BuildRunReport is a pure aggregation over
// Task.Attempts and Task.RevisionRounds, which pieces 1 through 4 already
// record - the summary needs no new per-task state, just a rollup step keyed
// by model across all tasks in a run.

// ModelStat is one model's record across a whole run.
type ModelStat struct {
	Model string
	// Attempts is how many times this model was tried, across every task.
	Attempts int
	// Tasks lists the ids of every task this model was tried on, in first-
	// encountered order.
	Tasks []string
	Passes int
	Fails  int
	// PassRate is Passes / Attempts, or 0 when Attempts is 0.
	PassRate float64
	// PassPositions records, for each of this model's passes, which
	// position in that task's attempt order the pass happened at (1 for
	// first model tried, 2 for second, and so on). It answers "did it win
	// first, or only after others failed."
	PassPositions []int
	// AvgDuration is the mean duration across this model's attempts.
	AvgDuration time.Duration
	// FailReasons lists the distinct reasons this model's failed attempts
	// gave, so a model that fails the same way every time is visibly
	// different from one with a one-off flaky failure.
	FailReasons []string
}

// RevisedTask rolls up one task whose definition the planner rewrote at
// least once: how many rounds it took, and the note from each round.
type RevisedTask struct {
	ID     string
	Rounds int
	Notes  []string
}

// RunReport rolls up every model that had at least one attempt in a run,
// plus the tasks that needed a revision - a property of the run, not of any
// single model.
type RunReport struct {
	Models       []ModelStat
	RevisedTasks []RevisedTask
	GeneratedAt  time.Time
}

// BuildRunReport aggregates a run's tasks into a report. It is a pure
// function over the tasks' existing Attempts and RevisionRounds; it needs
// no new per-task state and does no I/O.
//
// The trap this guards against: a model absent from a task's Attempts
// contributes nothing to that task - neither a pass nor a fail. A naive
// implementation that loops every known model against every task and
// counts a missing entry as a failure would attribute a failure to a model
// that was simply never reached. BuildRunReport only ever walks a task's
// own Attempts slice, so "never tried on this task" can never be conflated
// with "tried and failed on this task."
func BuildRunReport(tasks []Task, now time.Time) RunReport {
	type acc struct {
		attempts      int
		tasks         []string
		taskSeen      map[string]bool
		passes        int
		fails         int
		passPositions []int
		totalDuration time.Duration
		durationCount int
		failReasons   []string
		reasonSeen    map[string]bool
	}

	var order []string
	accs := map[string]*acc{}
	get := func(model string) *acc {
		a, ok := accs[model]
		if !ok {
			a = &acc{taskSeen: map[string]bool{}, reasonSeen: map[string]bool{}}
			accs[model] = a
			order = append(order, model)
		}
		return a
	}

	for _, t := range tasks {
		for i, at := range t.Attempts {
			a := get(at.Model)
			a.attempts++
			if !a.taskSeen[t.ID] {
				a.taskSeen[t.ID] = true
				a.tasks = append(a.tasks, t.ID)
			}
			if d, err := time.ParseDuration(at.Duration); err == nil {
				a.totalDuration += d
				a.durationCount++
			}
			if at.Verdict == "pass" {
				a.passes++
				a.passPositions = append(a.passPositions, i+1)
				continue
			}
			a.fails++
			if at.Reason != "" && !a.reasonSeen[at.Reason] {
				a.reasonSeen[at.Reason] = true
				a.failReasons = append(a.failReasons, at.Reason)
			}
		}
	}

	sort.Strings(order)
	models := make([]ModelStat, 0, len(order))
	for _, name := range order {
		a := accs[name]
		var passRate float64
		if a.attempts > 0 {
			passRate = float64(a.passes) / float64(a.attempts)
		}
		var avg time.Duration
		if a.durationCount > 0 {
			avg = a.totalDuration / time.Duration(a.durationCount)
		}
		models = append(models, ModelStat{
			Model:         name,
			Attempts:      a.attempts,
			Tasks:         a.tasks,
			Passes:        a.passes,
			Fails:         a.fails,
			PassRate:      passRate,
			PassPositions: a.passPositions,
			AvgDuration:   avg,
			FailReasons:   a.failReasons,
		})
	}

	var revised []RevisedTask
	for _, t := range tasks {
		if len(t.RevisionRounds) == 0 {
			continue
		}
		notes := make([]string, 0, len(t.RevisionRounds))
		for _, r := range t.RevisionRounds {
			notes = append(notes, r.Note)
		}
		revised = append(revised, RevisedTask{ID: t.ID, Rounds: len(t.RevisionRounds), Notes: notes})
	}

	return RunReport{Models: models, RevisedTasks: revised, GeneratedAt: now}
}
