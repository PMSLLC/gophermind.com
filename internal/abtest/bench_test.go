package abtest

import (
	"context"
	"testing"
)

func TestBenchmark(t *testing.T) {
	fixtures := []Fixture{
		{Prompt: "p1", Expect: "yes"},
		{Prompt: "p2", Expect: "no"},
	}
	run := func(_ context.Context, _, prompt string) (string, error) {
		if prompt == "p1" {
			return "yes indeed", nil
		}
		return "wrong", nil
	}
	clock := int64(0)
	now := func() int64 { clock += 100; return clock } // each call advances 100ms

	res := Benchmark(context.Background(), fixtures, run, now)
	if res.Passed != 1 || res.Total != 2 {
		t.Errorf("passed=%d total=%d, want 1/2", res.Passed, res.Total)
	}
	if res.DurationMs != 100 {
		t.Errorf("duration = %d, want 100 (two now() calls)", res.DurationMs)
	}
}
