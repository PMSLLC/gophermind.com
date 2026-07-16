package orchestrate

import (
	"context"
	"errors"
	"testing"

	"gophermind/internal/phaseflow"
)

func TestRunWithVerifySatisfiedFirstTry(t *testing.T) {
	turn := func(ctx context.Context, input string) (string, error) {
		return "answer", nil
	}
	verify := func(ctx context.Context, task, answer string) (bool, string, error) {
		return true, "", nil
	}

	status, detail := runWithVerify(context.Background(), turn, verify, "do the task", []string{"criterion 1"})

	if status != phaseflow.StatusDone {
		t.Errorf("status = %q, want %q", status, phaseflow.StatusDone)
	}
	if detail != "" {
		t.Errorf("detail = %q, want empty", detail)
	}
}

func TestRunWithVerifySatisfiedAfterCorrection(t *testing.T) {
	calls := 0
	turn := func(ctx context.Context, input string) (string, error) {
		calls++
		return "answer", nil
	}
	verifyCalls := 0
	verify := func(ctx context.Context, task, answer string) (bool, string, error) {
		verifyCalls++
		if verifyCalls == 1 {
			return false, "missing edge case", nil
		}
		return true, "", nil
	}

	status, detail := runWithVerify(context.Background(), turn, verify, "do the task", []string{"criterion 1"})

	if status != phaseflow.StatusCorrected {
		t.Errorf("status = %q, want %q", status, phaseflow.StatusCorrected)
	}
	if detail != "" {
		t.Errorf("detail = %q, want empty", detail)
	}
	if calls != 2 {
		t.Errorf("turn called %d times, want 2 (original + correction)", calls)
	}
}

func TestRunWithVerifyFailedAfterCorrection(t *testing.T) {
	turn := func(ctx context.Context, input string) (string, error) {
		return "answer", nil
	}
	verify := func(ctx context.Context, task, answer string) (bool, string, error) {
		return false, "still broken", nil
	}

	status, detail := runWithVerify(context.Background(), turn, verify, "do the task", []string{"criterion 1"})

	if status != phaseflow.StatusFailed {
		t.Errorf("status = %q, want %q", status, phaseflow.StatusFailed)
	}
	if detail != "still broken" {
		t.Errorf("detail = %q, want %q", detail, "still broken")
	}
}

func TestRunWithVerifyTurnError(t *testing.T) {
	turnErr := errors.New("llm unreachable")
	turn := func(ctx context.Context, input string) (string, error) {
		return "", turnErr
	}
	verify := func(ctx context.Context, task, answer string) (bool, string, error) {
		t.Fatal("verify should not be called when the turn errors")
		return false, "", nil
	}

	status, detail := runWithVerify(context.Background(), turn, verify, "do the task", []string{"criterion 1"})

	if status != phaseflow.StatusFailed {
		t.Errorf("status = %q, want %q", status, phaseflow.StatusFailed)
	}
	if detail != turnErr.Error() {
		t.Errorf("detail = %q, want %q", detail, turnErr.Error())
	}
}

func TestRunWithVerifyCorrectionTurnError(t *testing.T) {
	calls := 0
	turnErr := errors.New("llm unreachable on correction")
	turn := func(ctx context.Context, input string) (string, error) {
		calls++
		if calls == 1 {
			return "answer", nil
		}
		return "", turnErr
	}
	verify := func(ctx context.Context, task, answer string) (bool, string, error) {
		return false, "needs work", nil
	}

	status, detail := runWithVerify(context.Background(), turn, verify, "do the task", []string{"criterion 1"})

	if status != phaseflow.StatusFailed {
		t.Errorf("status = %q, want %q", status, phaseflow.StatusFailed)
	}
	if detail != turnErr.Error() {
		t.Errorf("detail = %q, want %q", detail, turnErr.Error())
	}
}
