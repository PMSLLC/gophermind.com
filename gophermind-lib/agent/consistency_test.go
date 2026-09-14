package agent

import (
	"context"
	"errors"
	"testing"
)

func TestMajorityVote(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"a", "b", "a"}, "a"},
		{[]string{"x", "x", "x"}, "x"},
		{[]string{" yes ", "yes", "no"}, "yes"}, // normalized (trim) before counting
		{[]string{"only"}, "only"},
	}
	for _, c := range cases {
		if got := majorityVote(c.in); got != c.want {
			t.Errorf("majorityVote(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSelfConsistentPicksMajority(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())

	calls := 0
	// Returns "B" twice and "A" once across three samples; B should win.
	run := func(ctx context.Context, _ string) (string, error) {
		calls++
		switch calls {
		case 1:
			return "A", nil
		default:
			return "B", nil
		}
	}
	out, err := a.SelfConsistent(context.Background(), "task", run, 3)
	if err != nil {
		t.Fatal(err)
	}
	if out != "B" {
		t.Errorf("majority answer = %q, want B", out)
	}
	if calls != 3 {
		t.Errorf("expected 3 samples, got %d", calls)
	}
}

func TestSelfConsistentSingleSample(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	calls := 0
	run := func(ctx context.Context, _ string) (string, error) {
		calls++
		return "once", nil
	}
	out, err := a.SelfConsistent(context.Background(), "task", run, 1)
	if err != nil || out != "once" || calls != 1 {
		t.Errorf("n<=1 should run once: out=%q calls=%d err=%v", out, calls, err)
	}
}

func TestSelfConsistentPropagatesError(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	run := func(ctx context.Context, _ string) (string, error) {
		return "", errors.New("boom")
	}
	if _, err := a.SelfConsistent(context.Background(), "task", run, 3); err == nil {
		t.Error("a sample error should propagate")
	}
}
