package ui

import (
	"testing"
	"time"

	"gophermind/gophermind-lib/phaseflow"
)

func TestPipelineState_SetTasksStoresSnapshot(t *testing.T) {
	p := NewPipelineState()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tasks := []phaseflow.Task{{ID: "01-01", Status: phaseflow.StatusDone}}

	p.SetTasks(tasks, now)

	got := p.Tasks()
	if len(got) != 1 || got[0].ID != "01-01" {
		t.Errorf("Tasks() = %+v", got)
	}
	if !p.GeneratedAt().Equal(now) {
		t.Errorf("GeneratedAt() = %v, want %v", p.GeneratedAt(), now)
	}
}

func TestPipelineState_ApplyTaskStatusUpdatesExistingTask(t *testing.T) {
	p := NewPipelineState()
	p.SetTasks([]phaseflow.Task{{ID: "01-01", Status: phaseflow.StatusPending, Wave: 1}}, time.Time{})

	p.ApplyTaskStatus("01-01", phaseflow.StatusRunning, 1)

	got := p.Tasks()
	if got[0].Status != phaseflow.StatusRunning {
		t.Errorf("Status = %q, want %q", got[0].Status, phaseflow.StatusRunning)
	}
}

// TestPipelineState_ApplyTaskStatusUnknownIDIsIgnored covers an event for a
// task this state hasn't seen yet (e.g. SSE connected after /pipeline/state
// was fetched but before the first status snapshot) -- ignoring it rather
// than fabricating a new task avoids inventing rows with missing fields.
func TestPipelineState_ApplyTaskStatusUnknownIDIsIgnored(t *testing.T) {
	p := NewPipelineState()
	p.ApplyTaskStatus("never-seen", phaseflow.StatusRunning, 1)
	if len(p.Tasks()) != 0 {
		t.Errorf("Tasks() = %+v, want empty", p.Tasks())
	}
}

func TestPipelineState_ApplyTaskAttemptAppendsToTask(t *testing.T) {
	p := NewPipelineState()
	p.SetTasks([]phaseflow.Task{{ID: "01-01"}}, time.Time{})

	p.ApplyTaskAttempt("01-01", phaseflow.Attempt{Model: "strong", Duration: "3m", Verdict: "pass"})

	got := p.Tasks()
	if len(got[0].Attempts) != 1 || got[0].Attempts[0].Model != "strong" {
		t.Errorf("Attempts = %+v", got[0].Attempts)
	}
}

func TestPipelineState_ApplyWaveChangedTracksCurrentWave(t *testing.T) {
	p := NewPipelineState()
	p.ApplyWaveChanged(2, "started")

	wave, state := p.CurrentWave()
	if wave != 2 || state != "started" {
		t.Errorf("CurrentWave() = (%d, %q), want (2, \"started\")", wave, state)
	}
}

func TestPipelineState_SetReportStoresReport(t *testing.T) {
	p := NewPipelineState()
	if p.Report() != nil {
		t.Fatal("Report() should start nil")
	}

	r := phaseflow.RunReport{Models: []phaseflow.ModelStat{{Model: "strong"}}}
	p.SetReport(r)

	got := p.Report()
	if got == nil || len(got.Models) != 1 || got.Models[0].Model != "strong" {
		t.Errorf("Report() = %+v", got)
	}
}

func TestPipelineState_OnChangeFiresOnEveryMutation(t *testing.T) {
	p := NewPipelineState()
	calls := 0
	p.OnChange(func() { calls++ })

	p.SetTasks([]phaseflow.Task{{ID: "01-01"}}, time.Time{})
	p.ApplyTaskStatus("01-01", phaseflow.StatusRunning, 1)
	p.ApplyTaskAttempt("01-01", phaseflow.Attempt{Model: "strong"})
	p.ApplyWaveChanged(1, "started")
	p.SetReport(phaseflow.RunReport{})

	if calls != 5 {
		t.Errorf("OnChange called %d times, want 5", calls)
	}
}

// TestPipelineState_ApplyTaskStatusOnUnknownIDDoesNotFireOnChange covers the
// no-op path: an ignored event is not a state change, so it shouldn't
// trigger a redraw either.
func TestPipelineState_ApplyTaskStatusOnUnknownIDDoesNotFireOnChange(t *testing.T) {
	p := NewPipelineState()
	calls := 0
	p.OnChange(func() { calls++ })

	p.ApplyTaskStatus("never-seen", phaseflow.StatusRunning, 1)

	if calls != 0 {
		t.Errorf("OnChange called %d times, want 0 for an unknown task id", calls)
	}
}
