package phaseflow

import (
	"reflect"
	"testing"
	"time"
)

// modelStatFor is a test helper that finds model's ModelStat in a report,
// failing the test if it is absent.
func modelStatFor(t *testing.T, r RunReport, model string) ModelStat {
	t.Helper()
	for _, m := range r.Models {
		if m.Model == model {
			return m
		}
	}
	t.Fatalf("no ModelStat for %q in report: %+v", model, r.Models)
	return ModelStat{}
}

// TestBuildRunReportAttributesWinsAndLosses covers the source spec's first
// summary test: a task where model A failed and model B passed must record
// one fail for A and one pass for B, not just a win for whoever ultimately
// succeeded.
func TestBuildRunReportAttributesWinsAndLosses(t *testing.T) {
	tasks := []Task{
		{
			ID: "t1",
			Attempts: []Attempt{
				{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "timeout"},
				{Model: "model-b", Duration: "2s", Verdict: "pass", Reason: "6/6 passing"},
			},
		},
	}
	report := BuildRunReport(tasks, time.Now())

	a := modelStatFor(t, report, "model-a")
	if a.Passes != 0 || a.Fails != 1 || a.Attempts != 1 {
		t.Errorf("model-a: got passes=%d fails=%d attempts=%d, want 0/1/1", a.Passes, a.Fails, a.Attempts)
	}

	b := modelStatFor(t, report, "model-b")
	if b.Passes != 1 || b.Fails != 0 || b.Attempts != 1 {
		t.Errorf("model-b: got passes=%d fails=%d attempts=%d, want 1/0/1", b.Passes, b.Fails, b.Attempts)
	}
}

// TestBuildRunReportPassRateMatchesHandCount builds a small run whose pass
// rate per model can be hand-counted, and checks BuildRunReport's arithmetic
// matches exactly.
func TestBuildRunReportPassRateMatchesHandCount(t *testing.T) {
	tasks := []Task{
		{
			ID: "t1",
			Attempts: []Attempt{
				{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "r1"},
				{Model: "model-b", Duration: "1s", Verdict: "pass", Reason: "ok"},
			},
		},
		{
			ID: "t2",
			Attempts: []Attempt{
				{Model: "model-a", Duration: "1s", Verdict: "pass", Reason: "ok"},
			},
		},
		{
			ID: "t3",
			Attempts: []Attempt{
				{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "r2"},
				{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "r3"},
			},
		},
	}
	report := BuildRunReport(tasks, time.Now())

	// Hand count for model-a: 4 attempts total (t1 fail, t2 pass, t3 fail,
	// t3 fail), 1 pass -> 1/4 = 0.25.
	a := modelStatFor(t, report, "model-a")
	if a.Attempts != 4 {
		t.Fatalf("model-a attempts = %d, want 4", a.Attempts)
	}
	if a.Passes != 1 || a.Fails != 3 {
		t.Errorf("model-a passes/fails = %d/%d, want 1/3", a.Passes, a.Fails)
	}
	if a.PassRate != 0.25 {
		t.Errorf("model-a pass rate = %v, want 0.25", a.PassRate)
	}

	// Hand count for model-b: 1 attempt, 1 pass -> 1.0.
	b := modelStatFor(t, report, "model-b")
	if b.Attempts != 1 || b.Passes != 1 || b.PassRate != 1.0 {
		t.Errorf("model-b = %+v, want attempts=1 passes=1 rate=1.0", b)
	}
}

// TestBuildRunReportNeverTriedIsNotAFailure is the trap test: a model that
// was never tried on a task must contribute neither a pass nor a fail for
// that task. A naive models-by-tasks loop that treats a missing attempt as
// a failure would inflate model-b's fail count here.
func TestBuildRunReportNeverTriedIsNotAFailure(t *testing.T) {
	tasks := []Task{
		{
			ID: "t1",
			Attempts: []Attempt{
				{Model: "model-a", Duration: "1s", Verdict: "pass", Reason: "ok"},
			},
		},
		{
			ID: "t2",
			Attempts: []Attempt{
				{Model: "model-b", Duration: "1s", Verdict: "pass", Reason: "ok"},
			},
		},
	}
	report := BuildRunReport(tasks, time.Now())

	a := modelStatFor(t, report, "model-a")
	if a.Attempts != 1 || a.Fails != 0 {
		t.Errorf("model-a (never tried on t2) got attempts=%d fails=%d, want 1/0 - t2 must not count against it", a.Attempts, a.Fails)
	}
	if !reflect.DeepEqual(a.Tasks, []string{"t1"}) {
		t.Errorf("model-a tasks = %v, want [t1] only", a.Tasks)
	}

	b := modelStatFor(t, report, "model-b")
	if b.Attempts != 1 || b.Fails != 0 {
		t.Errorf("model-b (never tried on t1) got attempts=%d fails=%d, want 1/0 - t1 must not count against it", b.Attempts, b.Fails)
	}
	if !reflect.DeepEqual(b.Tasks, []string{"t2"}) {
		t.Errorf("model-b tasks = %v, want [t2] only", b.Tasks)
	}
}

// TestBuildRunReportPassPositionRecordsSecondTry checks that a model which
// only wins after an earlier candidate failed gets PassPositions = [2], not
// [1] - the "did it win first, or only after others failed" question.
func TestBuildRunReportPassPositionRecordsSecondTry(t *testing.T) {
	tasks := []Task{
		{
			ID: "t1",
			Attempts: []Attempt{
				{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "r1"},
				{Model: "model-b", Duration: "1s", Verdict: "pass", Reason: "ok"},
			},
		},
	}
	report := BuildRunReport(tasks, time.Now())

	b := modelStatFor(t, report, "model-b")
	if !reflect.DeepEqual(b.PassPositions, []int{2}) {
		t.Errorf("model-b PassPositions = %v, want [2] (won as the second model tried)", b.PassPositions)
	}

	a := modelStatFor(t, report, "model-a")
	if len(a.PassPositions) != 0 {
		t.Errorf("model-a PassPositions = %v, want none - it never passed", a.PassPositions)
	}
}

// TestBuildRunReportFailReasonsDistinct checks that repeated identical
// failure reasons collapse to one entry, so "fails consistently on X" is
// visible rather than drowned in duplicates.
func TestBuildRunReportFailReasonsDistinct(t *testing.T) {
	tasks := []Task{
		{ID: "t1", Attempts: []Attempt{{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "timeout"}}},
		{ID: "t2", Attempts: []Attempt{{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "timeout"}}},
		{ID: "t3", Attempts: []Attempt{{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "bad schema"}}},
	}
	report := BuildRunReport(tasks, time.Now())

	a := modelStatFor(t, report, "model-a")
	if !reflect.DeepEqual(a.FailReasons, []string{"timeout", "bad schema"}) {
		t.Errorf("model-a FailReasons = %v, want [timeout, bad schema] with the duplicate collapsed", a.FailReasons)
	}
}

// TestBuildRunReportRevisedTasksRollUp checks that revised tasks are rolled
// up by round count and note, as a property of the run rather than of any
// model.
func TestBuildRunReportRevisedTasksRollUp(t *testing.T) {
	tasks := []Task{
		{
			ID: "t1",
			RevisionRounds: []Revision{
				{Round: 1, Note: "clarified the deliverable"},
				{Round: 2, Note: "narrowed the test to drop an impossible edge case"},
			},
		},
		{ID: "t2"}, // never revised
	}
	report := BuildRunReport(tasks, time.Now())

	if len(report.RevisedTasks) != 1 {
		t.Fatalf("RevisedTasks = %+v, want exactly one entry (t1 only)", report.RevisedTasks)
	}
	rt := report.RevisedTasks[0]
	if rt.ID != "t1" || rt.Rounds != 2 {
		t.Errorf("t1 revision rollup = %+v, want ID=t1 Rounds=2", rt)
	}
	want := []string{"clarified the deliverable", "narrowed the test to drop an impossible edge case"}
	if !reflect.DeepEqual(rt.Notes, want) {
		t.Errorf("t1 revision notes = %v, want %v", rt.Notes, want)
	}
}

// TestBuildRunReportEmptyRun checks that an empty run produces an empty
// report rather than a nil-map panic.
func TestBuildRunReportEmptyRun(t *testing.T) {
	report := BuildRunReport(nil, time.Now())
	if len(report.Models) != 0 {
		t.Errorf("Models = %v, want empty", report.Models)
	}
	if len(report.RevisedTasks) != 0 {
		t.Errorf("RevisedTasks = %v, want empty", report.RevisedTasks)
	}
}

// A task that was revised must still report the models tried BEFORE the
// revision. ApplyRevision clears the live attempt list so the next pass starts
// a fresh count, and without carrying those attempts into the revision record
// the report would show zero attempts for a model that in fact failed several
// times, which is the opposite of attributing wins and losses to every model
// actually tried.
func TestBuildRunReportCountsAttemptsFromBeforeARevision(t *testing.T) {
	task := Task{ID: "t1", Status: StatusDone}

	// Round 1: two models failed, which prompted a revision.
	task.RecordAttempt(Attempt{Model: "model-a", Verdict: "fail", Reason: "missing idempotency key"})
	task.RecordAttempt(Attempt{Model: "model-b", Verdict: "fail", Reason: "missing idempotency key"})
	if err := ApplyRevision(&task, Revision{Round: 1, Note: "test was under-specified", Deliverable: "clearer"}); err != nil {
		t.Fatalf("ApplyRevision: %v", err)
	}
	// Round 2: a third model passed.
	task.RecordAttempt(Attempt{Model: "model-c", Verdict: "pass"})

	rep := BuildRunReport([]Task{task}, time.Now())

	byModel := map[string]ModelStat{}
	for _, m := range rep.Models {
		byModel[m.Model] = m
	}
	for _, name := range []string{"model-a", "model-b"} {
		m, ok := byModel[name]
		if !ok {
			t.Fatalf("%s is absent from the report: its pre-revision attempts were lost", name)
		}
		if m.Attempts != 1 || m.Fails != 1 {
			t.Errorf("%s = %d attempts / %d fails, want 1/1", name, m.Attempts, m.Fails)
		}
	}
	if m := byModel["model-c"]; m.Passes != 1 {
		t.Errorf("model-c passes = %d, want 1", m.Passes)
	}
}
