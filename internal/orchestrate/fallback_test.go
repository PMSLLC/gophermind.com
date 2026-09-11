package orchestrate

import (
	"context"
	"testing"

	"gophermind/internal/phaseflow"
)

// stubTaskRunner is a phaseflow.TaskRunner double: it returns a scripted
// result per call, in order, so StatusVerifyingRunner's behavior can be
// checked without a real agent.
type stubTaskRunner struct {
	results []struct {
		status, detail string
		err            error
	}
	call int
}

func (s *stubTaskRunner) Run(context.Context, phaseflow.Task) (string, string, error) {
	r := s.results[s.call]
	s.call++
	return r.status, r.detail, r.err
}

func TestStatusVerifyingRunnerPassesOnDoneOrCorrected(t *testing.T) {
	for _, status := range []string{phaseflow.StatusDone, phaseflow.StatusCorrected} {
		inner := &stubTaskRunner{results: []struct {
			status, detail string
			err            error
		}{{status: status, detail: "the output"}}}
		sv := NewStatusVerifyingRunner(inner)

		gotStatus, gotDetail, err := sv.Run(context.Background(), phaseflow.Task{ID: "01-01"})
		if err != nil || gotStatus != status || gotDetail != "the output" {
			t.Fatalf("Run() = (%q, %q, %v)", gotStatus, gotDetail, err)
		}
		ok, reason := sv.Verify(context.Background(), phaseflow.Task{ID: "01-01"}, gotDetail)
		if !ok {
			t.Errorf("status %q: Verify() ok = false, want true", status)
		}
		if reason != "" {
			t.Errorf("status %q: Verify() reason = %q, want empty on pass", status, reason)
		}
	}
}

func TestStatusVerifyingRunnerFailsOnFailedWithReason(t *testing.T) {
	inner := &stubTaskRunner{results: []struct {
		status, detail string
		err            error
	}{{status: phaseflow.StatusFailed, detail: "acceptance criterion 2 not met"}}}
	sv := NewStatusVerifyingRunner(inner)

	_, detail, _ := sv.Run(context.Background(), phaseflow.Task{ID: "01-01"})
	ok, reason := sv.Verify(context.Background(), phaseflow.Task{ID: "01-01"}, detail)
	if ok {
		t.Fatal("Verify() ok = true, want false for a failed status")
	}
	if reason != "acceptance criterion 2 not met" {
		t.Errorf("reason = %q, want the runner's own detail", reason)
	}
}

func TestStatusVerifyingRunnerFailsWithFallbackReasonWhenDetailEmpty(t *testing.T) {
	inner := &stubTaskRunner{results: []struct {
		status, detail string
		err            error
	}{{status: phaseflow.StatusFailed, detail: ""}}}
	sv := NewStatusVerifyingRunner(inner)

	_, detail, _ := sv.Run(context.Background(), phaseflow.Task{ID: "01-01"})
	ok, reason := sv.Verify(context.Background(), phaseflow.Task{ID: "01-01"}, detail)
	if ok {
		t.Fatal("Verify() ok = true, want false")
	}
	if reason == "" {
		t.Error("reason is empty even with no detail from the runner; the circuit breaker needs something specific")
	}
}
