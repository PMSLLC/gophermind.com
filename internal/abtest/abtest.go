// Package abtest runs prompt A/B experiments: each prompt variant is scored
// against a set of fixtures (prompt + expected substring), so prompt changes can
// be tuned on data rather than vibes.
package abtest

import (
	"context"
	"strings"
)

// Variant is a named prompt variant: a system prompt to test.
type Variant struct {
	Name   string
	System string
}

// Fixture is one test case: a prompt and an expected substring in the answer.
type Fixture struct {
	Prompt string `json:"prompt"`
	Expect string `json:"expect"`
}

// Result is a variant's aggregate score across the fixtures.
type Result struct {
	Variant string
	Passed  int
	Total   int
}

// Runner produces an answer for a (system, prompt) pair.
type Runner func(ctx context.Context, system, prompt string) (string, error)

// RunMatrix runs every variant against every fixture and scores each answer by a
// case-insensitive substring match against the fixture's Expect. A runner error
// counts as a fail. Results preserve variant order.
func RunMatrix(ctx context.Context, variants []Variant, fixtures []Fixture, run Runner) []Result {
	results := make([]Result, 0, len(variants))
	for _, v := range variants {
		r := Result{Variant: v.Name, Total: len(fixtures)}
		for _, f := range fixtures {
			if ctx.Err() != nil {
				break
			}
			out, err := run(ctx, v.System, f.Prompt)
			if err == nil && matches(out, f.Expect) {
				r.Passed++
			}
		}
		results = append(results, r)
	}
	return results
}

// matches reports whether answer contains expect, case-insensitively. An empty
// expect matches anything (no assertion).
func matches(answer, expect string) bool {
	if expect == "" {
		return true
	}
	return strings.Contains(strings.ToLower(answer), strings.ToLower(expect))
}
