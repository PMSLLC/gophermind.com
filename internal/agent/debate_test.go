package agent

import (
	"context"
	"errors"
	"testing"
)

func TestDebatePicksWhenDivergent(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	calls := 0
	run := func(context.Context, string) (string, error) {
		calls++
		if calls == 1 {
			return "answer A", nil
		}
		return "answer B", nil
	}
	pick := func(_ context.Context, _, candA, candB string) (string, error) {
		return "synthesis of (" + candA + ") and (" + candB + ")", nil
	}
	out, err := a.Debate(context.Background(), "task", run, pick)
	if err != nil {
		t.Fatal(err)
	}
	if out != "synthesis of (answer A) and (answer B)" {
		t.Errorf("debate result = %q", out)
	}
	if calls != 2 {
		t.Errorf("expected 2 candidate runs, got %d", calls)
	}
}

func TestDebateConsensusSkipsJudge(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	run := func(context.Context, string) (string, error) { return "same", nil }
	judged := false
	pick := func(context.Context, string, string, string) (string, error) {
		judged = true
		return "should not be called", nil
	}
	out, err := a.Debate(context.Background(), "task", run, pick)
	if err != nil {
		t.Fatal(err)
	}
	if out != "same" {
		t.Errorf("consensus answer = %q, want same", out)
	}
	if judged {
		t.Error("judge should be skipped when candidates agree")
	}
}

func TestDebatePropagatesRunError(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	run := func(context.Context, string) (string, error) { return "", errors.New("boom") }
	pick := func(context.Context, string, string, string) (string, error) { return "", nil }
	if _, err := a.Debate(context.Background(), "task", run, pick); err == nil {
		t.Error("a candidate run error should propagate")
	}
}
