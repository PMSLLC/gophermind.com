package agent

import (
	"context"
	"strings"
	"testing"
)

func TestReflexionRetriesWithLesson(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	var prompts []string
	run := func(_ context.Context, task string) (string, error) {
		prompts = append(prompts, task)
		if len(prompts) == 1 {
			return "first attempt", nil
		}
		return "second attempt", nil
	}
	// Verifier fails the first answer with feedback, passes on retry.
	verify := func(_ context.Context, _, answer string) (bool, string) {
		return answer == "second attempt", "missing error handling"
	}

	out, err := a.Reflexion(context.Background(), "do it", run, verify)
	if err != nil {
		t.Fatal(err)
	}
	if out != "second attempt" {
		t.Errorf("final answer = %q", out)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected a retry, got %d runs", len(prompts))
	}
	// The retry prompt must carry a structured lesson derived from the feedback.
	if !strings.Contains(strings.ToLower(prompts[1]), "lesson") || !strings.Contains(prompts[1], "missing error handling") {
		t.Errorf("retry prompt should include the structured lesson:\n%s", prompts[1])
	}
}

func TestReflexionAcceptsFirst(t *testing.T) {
	a := newTestAgent(t, &scriptedProvider{}, t.TempDir())
	calls := 0
	run := func(context.Context, string) (string, error) { calls++; return "good", nil }
	verify := func(context.Context, string, string) (bool, string) { return true, "" }
	out, _ := a.Reflexion(context.Background(), "t", run, verify)
	if out != "good" || calls != 1 {
		t.Errorf("passing answer should not retry: out=%q calls=%d", out, calls)
	}
}

func TestFormatLesson(t *testing.T) {
	l := formatLesson("missing tests")
	if !strings.Contains(strings.ToLower(l), "lesson") || !strings.Contains(l, "missing tests") {
		t.Errorf("lesson format wrong: %q", l)
	}
}
