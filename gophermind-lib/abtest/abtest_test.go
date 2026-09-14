package abtest

import (
	"context"
	"strings"
	"testing"
)

func TestRunMatrixScores(t *testing.T) {
	variants := []Variant{{Name: "a", System: "sysA"}, {Name: "b", System: "sysB"}}
	fixtures := []Fixture{
		{Prompt: "2+2", Expect: "4"},
		{Prompt: "cap of france", Expect: "paris"},
	}
	// Fake runner: variant "a" answers correctly; "b" always says "idk".
	run := func(_ context.Context, system, prompt string) (string, error) {
		if system == "sysA" {
			if strings.Contains(prompt, "2+2") {
				return "the answer is 4", nil
			}
			return "it is Paris", nil
		}
		return "idk", nil
	}
	results := RunMatrix(context.Background(), variants, fixtures, run)
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	byName := map[string]Result{}
	for _, r := range results {
		byName[r.Variant] = r
	}
	if byName["a"].Passed != 2 || byName["a"].Total != 2 {
		t.Errorf("variant a = %d/%d, want 2/2", byName["a"].Passed, byName["a"].Total)
	}
	if byName["b"].Passed != 0 {
		t.Errorf("variant b passed = %d, want 0", byName["b"].Passed)
	}
}

func TestRunMatrixCaseInsensitiveMatch(t *testing.T) {
	results := RunMatrix(context.Background(),
		[]Variant{{Name: "v"}},
		[]Fixture{{Prompt: "x", Expect: "HELLO"}},
		func(context.Context, string, string) (string, error) { return "well hello there", nil })
	if results[0].Passed != 1 {
		t.Errorf("case-insensitive match failed: %+v", results[0])
	}
}

func TestRunMatrixCountsErrorsAsFail(t *testing.T) {
	results := RunMatrix(context.Background(),
		[]Variant{{Name: "v"}},
		[]Fixture{{Prompt: "x", Expect: "y"}},
		func(context.Context, string, string) (string, error) { return "", context.Canceled })
	if results[0].Passed != 0 || results[0].Total != 1 {
		t.Errorf("error should count as fail: %+v", results[0])
	}
}
