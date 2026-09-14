package phaseflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

// callRunner is a TaskRunner double for fallback tests: it records the
// model each call used, in order, and returns a scripted result keyed by
// model (a permissive StatusDone default when a model has no script entry,
// so tests only need to script the models they care about).
type callRunner struct {
	byModel map[string]scriptedResult
	models  []string
}

func (c *callRunner) Run(_ context.Context, t Task) (string, string, error) {
	c.models = append(c.models, t.Model)
	if r, ok := c.byModel[t.Model]; ok {
		return r.status, r.detail, r.err
	}
	return StatusDone, "output:" + t.Model, nil
}

// verifyFunc adapts a plain function to the Verifier interface, so tests can
// script pass/fail per call without a dedicated named type per test.
type verifyFunc func(ctx context.Context, t Task, detail string) (bool, string)

func (f verifyFunc) Verify(ctx context.Context, t Task, detail string) (bool, string) {
	return f(ctx, t, detail)
}

// stepClock returns a clock that advances by step on every call, starting at
// start, so a FallbackRunner test gets deterministic, non-zero durations
// without a real one.
func stepClock(start time.Time, step time.Duration) func() time.Time {
	cur := start.Add(-step)
	return func() time.Time {
		cur = cur.Add(step)
		return cur
	}
}

func fallbackTask(id string, models ...string) Task {
	return Task{ID: id, Phase: "1", Title: "T " + id, Agent: "coder", CandidateModels: models, Status: StatusPending}
}

func TestFallbackFirstCandidatePasses(t *testing.T) {
	runner := &callRunner{byModel: map[string]scriptedResult{}}
	var got []Attempt
	f := &FallbackRunner{
		OnAttempt: func(a Attempt) { got = append(got, a) },
		Inner:     runner,
		Verify:    verifyFunc(func(context.Context, Task, string) (bool, string) { return true, "" }),
		Now:       stepClock(time.Unix(0, 0), time.Second),
	}

	status, detail, err := f.Run(context.Background(), fallbackTask("01-01", "model-a", "model-b"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != StatusDone {
		t.Errorf("status = %q, want %q", status, StatusDone)
	}
	if detail != "output:model-a" {
		t.Errorf("detail = %q, want %q", detail, "output:model-a")
	}
	if len(runner.models) != 1 || runner.models[0] != "model-a" {
		t.Errorf("inner calls = %v, want [model-a] (second candidate must not run)", runner.models)
	}
	if len(got) != 1 {
		t.Fatalf("attempts = %+v, want exactly 1 attempt", got)
	}
	if got[0].Model != "model-a" || got[0].Verdict != "pass" {
		t.Errorf("attempt = %+v, want model-a/pass", got[0])
	}
}

func TestFallbackFirstFailsSecondPasses(t *testing.T) {
	runner := &callRunner{byModel: map[string]scriptedResult{}}
	var got []Attempt
	f := &FallbackRunner{
		OnAttempt: func(a Attempt) { got = append(got, a) },
		Inner:     runner,
		Verify: verifyFunc(func(_ context.Context, _ Task, detail string) (bool, string) {
			if detail == "output:model-a" {
				return false, "missed the edge case"
			}
			return true, ""
		}),
		Now: stepClock(time.Unix(0, 0), time.Second),
	}

	status, detail, err := f.Run(context.Background(), fallbackTask("01-01", "model-a", "model-b"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != StatusDone {
		t.Errorf("status = %q, want %q", status, StatusDone)
	}
	if detail != "output:model-b" {
		t.Errorf("detail = %q, want the SECOND model's output only, got %q", detail, detail)
	}
	if len(runner.models) != 2 || runner.models[0] != "model-a" || runner.models[1] != "model-b" {
		t.Errorf("inner calls = %v, want [model-a model-b] in order", runner.models)
	}
	if len(got) != 2 {
		t.Fatalf("attempts = %+v, want 2 attempts", got)
	}
	if got[0].Model != "model-a" || got[0].Verdict != "fail" {
		t.Errorf("attempt[0] = %+v, want model-a/fail", got[0])
	}
	if got[0].Reason != "missed the edge case" {
		t.Errorf("attempt[0].Reason = %q, want the specific reason", got[0].Reason)
	}
	if got[1].Model != "model-b" || got[1].Verdict != "pass" {
		t.Errorf("attempt[1] = %+v, want model-b/pass", got[1])
	}
}

func TestFallbackAllCandidatesFail(t *testing.T) {
	runner := &callRunner{byModel: map[string]scriptedResult{}}
	var got []Attempt
	f := &FallbackRunner{
		OnAttempt: func(a Attempt) { got = append(got, a) },
		Inner:     runner,
		Verify: verifyFunc(func(_ context.Context, _ Task, detail string) (bool, string) {
			return false, "wrong output for " + detail
		}),
		Now: stepClock(time.Unix(0, 0), time.Second),
	}

	status, _, err := f.Run(context.Background(), fallbackTask("01-01", "model-a", "model-b", "model-c"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != StatusNeedsRevision {
		t.Errorf("status = %q, want %q", status, StatusNeedsRevision)
	}
	if len(runner.models) != 3 {
		t.Fatalf("inner calls = %v, want exactly one per candidate (no infinite loop)", runner.models)
	}
	if len(got) != 3 {
		t.Fatalf("attempts = %+v, want 3 attempts", got)
	}
	for i, want := range []string{"model-a", "model-b", "model-c"} {
		if got[i].Model != want {
			t.Errorf("attempt[%d].Model = %q, want %q", i, got[i].Model, want)
		}
		if got[i].Verdict != "fail" {
			t.Errorf("attempt[%d].Verdict = %q, want fail", i, got[i].Verdict)
		}
		if got[i].Reason == "" {
			t.Errorf("attempt[%d].Reason is empty, want a specific reason", i)
		}
	}
}

func TestFallbackEmptyCandidateListIsError(t *testing.T) {
	runner := &callRunner{byModel: map[string]scriptedResult{}}
	var got []Attempt
	f := &FallbackRunner{
		OnAttempt: func(a Attempt) { got = append(got, a) },
		Inner:     runner,
		Verify:    verifyFunc(func(context.Context, Task, string) (bool, string) { return true, "" }),
	}

	_, _, err := f.Run(context.Background(), Task{ID: "01-01", Status: StatusPending})
	if err == nil {
		t.Fatal("expected an error for an empty candidate list, got nil")
	}
	if len(runner.models) != 0 {
		t.Errorf("inner calls = %v, want none", runner.models)
	}
}

func TestFallbackUsesModelWhenCandidateModelsEmpty(t *testing.T) {
	runner := &callRunner{byModel: map[string]scriptedResult{}}
	var got []Attempt
	f := &FallbackRunner{
		OnAttempt: func(a Attempt) { got = append(got, a) },
		Inner:     runner,
		Verify:    verifyFunc(func(context.Context, Task, string) (bool, string) { return true, "" }),
	}

	task := Task{ID: "01-01", Model: "strong", Status: StatusPending}
	status, _, err := f.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != StatusDone {
		t.Errorf("status = %q, want %q", status, StatusDone)
	}
	if len(runner.models) != 1 || runner.models[0] != "strong" {
		t.Errorf("inner calls = %v, want [strong] (t.Model used as the sole candidate)", runner.models)
	}
}

func TestFallbackContextCancelledMidListStopsPromptly(t *testing.T) {
	runner := &callRunner{byModel: map[string]scriptedResult{
		"model-b": {err: context.Canceled},
	}}
	var got []Attempt
	f := &FallbackRunner{
		OnAttempt: func(a Attempt) { got = append(got, a) },
		Inner:     runner,
		Verify:    verifyFunc(func(context.Context, Task, string) (bool, string) { return false, "no good" }),
		Now:       stepClock(time.Unix(0, 0), time.Second),
	}

	_, _, err := f.Run(context.Background(), fallbackTask("01-01", "model-a", "model-b", "model-c"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(runner.models) != 2 || runner.models[1] != "model-b" {
		t.Fatalf("inner calls = %v, want [model-a model-b] (model-c must never run)", runner.models)
	}
	if len(got) != 1 {
		t.Errorf("attempts = %+v, want only model-a's failed attempt recorded", got)
	}
}

func TestFallbackDurationsComeFromInjectedClock(t *testing.T) {
	runner := &callRunner{byModel: map[string]scriptedResult{}}
	var got []Attempt
	f := &FallbackRunner{
		OnAttempt: func(a Attempt) { got = append(got, a) },
		Inner:     runner,
		Verify:    verifyFunc(func(context.Context, Task, string) (bool, string) { return true, "" }),
		Now:       stepClock(time.Unix(1000, 0), 5*time.Second),
	}

	if _, _, err := f.Run(context.Background(), fallbackTask("01-01", "model-a")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("attempts = %+v, want 1 attempt", got)
	}
	a := got[0]
	if !a.StartedAt.Equal(time.Unix(1000, 0)) {
		t.Errorf("StartedAt = %v, want %v", a.StartedAt, time.Unix(1000, 0))
	}
	if a.Duration != (5 * time.Second).String() {
		t.Errorf("Duration = %q, want %q", a.Duration, (5 * time.Second).String())
	}
}
