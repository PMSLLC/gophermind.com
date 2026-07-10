package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestQueueRunsAllSequentially(t *testing.T) {
	q := New()
	q.Add("first")
	q.Add("second")
	q.Add("third")
	if q.Pending() != 3 {
		t.Fatalf("pending = %d, want 3", q.Pending())
	}

	var order []string
	run := func(_ context.Context, task string) (string, error) {
		order = append(order, task)
		return "ok:" + task, nil
	}
	q.Run(context.Background(), run, nil)

	if len(order) != 3 || order[0] != "first" || order[2] != "third" {
		t.Errorf("run order = %v", order)
	}
	for _, j := range q.Jobs() {
		if j.Status != Done {
			t.Errorf("job %d status = %s, want done", j.ID, j.Status)
		}
		if j.Result != "ok:"+j.Task {
			t.Errorf("job %d result = %q", j.ID, j.Result)
		}
	}
	if q.Pending() != 0 {
		t.Errorf("pending after run = %d, want 0", q.Pending())
	}
}

func TestQueueRecordsFailures(t *testing.T) {
	q := New()
	q.Add("good")
	q.Add("bad")
	run := func(_ context.Context, task string) (string, error) {
		if task == "bad" {
			return "", errors.New("boom")
		}
		return "fine", nil
	}
	q.Run(context.Background(), run, nil)

	js := q.Jobs()
	if js[0].Status != Done {
		t.Errorf("good job status = %s", js[0].Status)
	}
	if js[1].Status != Failed || js[1].Err != "boom" {
		t.Errorf("bad job = %+v, want failed/boom", js[1])
	}
	d, f, p := q.Counts()
	if d != 1 || f != 1 || p != 0 {
		t.Errorf("counts done/failed/pending = %d/%d/%d, want 1/1/0", d, f, p)
	}
}

func TestQueueStopsOnCancel(t *testing.T) {
	q := New()
	q.Add("a")
	q.Add("b")
	q.Add("c")
	ctx, cancel := context.WithCancel(context.Background())
	run := func(_ context.Context, task string) (string, error) {
		if task == "a" {
			cancel() // cancel after the first job
		}
		return "done", nil
	}
	q.Run(ctx, run, nil)

	// a ran; b and c should remain pending (not executed).
	js := q.Jobs()
	if js[0].Status != Done {
		t.Errorf("a status = %s, want done", js[0].Status)
	}
	if js[1].Status != Pending || js[2].Status != Pending {
		t.Errorf("remaining jobs should stay pending: %s,%s", js[1].Status, js[2].Status)
	}
}

func TestQueueOnUpdateCallback(t *testing.T) {
	q := New()
	q.Add("x")
	var states []Status
	q.Run(context.Background(),
		func(_ context.Context, _ string) (string, error) { return "ok", nil },
		func(j *Job) { states = append(states, j.Status) })
	// Expect at least running then done.
	if len(states) < 2 || states[len(states)-1] != Done {
		t.Errorf("update states = %v, want to end in done", states)
	}
}
