package orchestrate

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gophermind/internal/phaseflow"
)

// stubRunner returns a status derived from the task id, after a pause long
// enough for a sibling to interleave.
type stubRunner struct{ pause time.Duration }

func (s stubRunner) Run(_ context.Context, t phaseflow.Task) (string, string, error) {
	time.Sleep(s.pause)
	if t.ID == "fail-task" {
		return phaseflow.StatusFailed, "criterion not met", nil
	}
	return phaseflow.StatusDone, "ok", nil
}

// One StatusVerifyingRunner is wired as both FallbackRunner.Inner and
// FallbackRunner.Verify, and a wave runs its tasks concurrently. The verdict
// a task's Run produced therefore has to survive until that same task's
// Verify reads it.
//
// A single shared lastStatus field cannot do that: between one task's Run and
// its Verify, a sibling's Run overwrites the field, so the failing task is
// verified against the passing task's status and reported done. The attempt
// history then records a pass for work that failed, which is worse than the
// failure itself - nothing downstream has any way to notice.
func TestStatusVerifyingRunnerKeepsVerdictsPerTask(t *testing.T) {
	sv := NewStatusVerifyingRunner(stubRunner{pause: 20 * time.Millisecond})

	type result struct {
		id string
		ok bool
	}
	var (
		mu      sync.Mutex
		results []result
		wg      sync.WaitGroup
	)

	// "fail-task" must come back not-ok; the passing siblings run alongside
	// it and are what corrupt a shared field.
	ids := []string{"fail-task", "pass-1", "pass-2", "pass-3"}
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			task := phaseflow.Task{ID: id, Model: "m"}
			_, detail, err := sv.Run(context.Background(), task)
			if err != nil {
				t.Errorf("%s: %v", id, err)
				return
			}
			ok, _ := sv.Verify(context.Background(), task, detail)
			mu.Lock()
			results = append(results, result{id, ok})
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	for _, r := range results {
		want := r.id != "fail-task"
		if r.ok != want {
			t.Errorf("%s verified ok=%v, want %v: a sibling's verdict was read "+
				"in place of this task's own", r.id, r.ok, want)
		}
	}
}

// Sequential use is the ordinary case and must keep working: FallbackRunner
// tries a task's candidates one after another, Run then Verify each time, and
// each candidate's verdict is the one its own Verify must see.
func TestStatusVerifyingRunnerSequentialCandidatesEachSeeTheirOwnVerdict(t *testing.T) {
	sv := NewStatusVerifyingRunner(stubRunner{})
	for i := 0; i < 3; i++ {
		task := phaseflow.Task{ID: fmt.Sprintf("t%d", i), Model: "m"}
		_, detail, err := sv.Run(context.Background(), task)
		if err != nil {
			t.Fatal(err)
		}
		if ok, reason := sv.Verify(context.Background(), task, detail); !ok {
			t.Fatalf("t%d: got not-ok (%s), want ok", i, reason)
		}
	}

	task := phaseflow.Task{ID: "fail-task", Model: "m"}
	_, detail, _ := sv.Run(context.Background(), task)
	ok, reason := sv.Verify(context.Background(), task, detail)
	if ok {
		t.Error("a failing task verified as ok")
	}
	if reason != "criterion not met" {
		t.Errorf("reason = %q, want the runner's own explanation", reason)
	}
}
