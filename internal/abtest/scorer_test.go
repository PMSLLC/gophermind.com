package abtest

import (
	"context"
	"errors"
	"testing"
)

func TestRunMatrixScoredUsesScorer(t *testing.T) {
	variants := []Variant{{Name: "v1", System: "s"}}
	fixtures := []Fixture{{Prompt: "p1", Expect: "x"}, {Prompt: "p2", Expect: "y"}}
	run := func(_ context.Context, _, prompt string) (string, error) { return "answer for " + prompt, nil }

	// A judge that passes only p1.
	judge := func(_ context.Context, rubric, prompt, answer string) (bool, error) {
		return prompt == "p1", nil
	}
	res := RunMatrixScored(context.Background(), variants, fixtures, run, JudgeScorer(judge))
	if res[0].Passed != 1 || res[0].Total != 2 {
		t.Errorf("judge scorer: passed=%d total=%d, want 1/2", res[0].Passed, res[0].Total)
	}
}

func TestJudgeScorerErrorFails(t *testing.T) {
	sc := JudgeScorer(func(context.Context, string, string, string) (bool, error) {
		return true, errors.New("judge down")
	})
	if sc(context.Background(), "p", "a", "e") {
		t.Error("a judge error must count as a fail")
	}
}

func TestSubstringScorerDefault(t *testing.T) {
	if !SubstringScorer(context.Background(), "p", "the ANSWER", "answer") {
		t.Error("substring scorer should match case-insensitively")
	}
}
