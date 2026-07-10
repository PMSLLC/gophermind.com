// Package abtest runs prompt A/B experiments: each prompt variant is scored
// against a set of fixtures (prompt + expected substring), so prompt changes can
// be tuned on data rather than vibes.
package abtest

import (
	"context"
	"fmt"
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

// Scorer decides whether an answer passes for a fixture. The default is a
// substring match; a JudgeScorer uses an LLM rubric for open-ended answers.
type Scorer func(ctx context.Context, prompt, answer, expect string) bool

// SubstringScorer is the default scorer: case-insensitive substring match.
func SubstringScorer(_ context.Context, _, answer, expect string) bool {
	return matches(answer, expect)
}

// JudgeFn asks an LLM whether an answer satisfies a rubric for a prompt.
type JudgeFn func(ctx context.Context, rubric, prompt, answer string) (bool, error)

// JudgeScorer builds a Scorer that grades open-ended answers with an LLM judge,
// using the fixture's Expect field as the rubric. A judge error counts as a fail.
func JudgeScorer(judge JudgeFn) Scorer {
	return func(ctx context.Context, prompt, answer, expect string) bool {
		ok, err := judge(ctx, expect, prompt, answer)
		return err == nil && ok
	}
}

// RunMatrixScored is RunMatrix with a pluggable scorer (RunMatrix uses
// SubstringScorer).
func RunMatrixScored(ctx context.Context, variants []Variant, fixtures []Fixture, run Runner, score Scorer) []Result {
	if score == nil {
		score = SubstringScorer
	}
	results := make([]Result, 0, len(variants))
	for _, v := range variants {
		r := Result{Variant: v.Name, Total: len(fixtures)}
		for _, f := range fixtures {
			if ctx.Err() != nil {
				break
			}
			out, err := run(ctx, v.System, f.Prompt)
			if err == nil && score(ctx, f.Prompt, out, f.Expect) {
				r.Passed++
			}
		}
		results = append(results, r)
	}
	return results
}

// BenchResult is the outcome of a benchmark run: pass rate and wall-clock time.
type BenchResult struct {
	Passed     int
	Total      int
	DurationMs int64
}

// Benchmark runs each fixture through run (scored by SubstringScorer), timing
// the whole set — a standardized way to compare a model/version's quality and
// latency over time. now is injectable for testing.
func Benchmark(ctx context.Context, fixtures []Fixture, run Runner, now func() int64) BenchResult {
	start := now()
	res := BenchResult{Total: len(fixtures)}
	for _, f := range fixtures {
		if ctx.Err() != nil {
			break
		}
		out, err := run(ctx, "", f.Prompt)
		if err == nil && matches(out, f.Expect) {
			res.Passed++
		}
	}
	res.DurationMs = now() - start
	return res
}

// Leaderboard renders results ranked by pass rate (best first) as a table, so
// the best variant×model pair is obvious at a glance.
func Leaderboard(results []Result) string {
	ranked := make([]Result, len(results))
	copy(ranked, results)
	sortByScore(ranked)
	var b strings.Builder
	b.WriteString("rank  variant                        score\n")
	for i, r := range ranked {
		score := 0.0
		if r.Total > 0 {
			score = float64(r.Passed) / float64(r.Total) * 100
		}
		fmt.Fprintf(&b, "%-4d  %-30s %d/%d (%.0f%%)\n", i+1, r.Variant, r.Passed, r.Total, score)
	}
	return b.String()
}

func sortByScore(rs []Result) {
	rate := func(r Result) float64 {
		if r.Total == 0 {
			return 0
		}
		return float64(r.Passed) / float64(r.Total)
	}
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rate(rs[j]) > rate(rs[j-1]); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}
