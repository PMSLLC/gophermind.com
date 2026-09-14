package abtest

import (
	"strings"
	"testing"
)

func TestLeaderboardRanksByScore(t *testing.T) {
	results := []Result{
		{Variant: "b@m1", Passed: 1, Total: 4},
		{Variant: "a@m2", Passed: 3, Total: 4},
		{Variant: "c@m1", Passed: 2, Total: 4},
	}
	out := Leaderboard(results)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// The best (a@m2, 75%) should be rank 1 (first data line).
	if !strings.Contains(lines[1], "a@m2") {
		t.Errorf("best variant should rank first:\n%s", out)
	}
	if !strings.Contains(lines[3], "b@m1") {
		t.Errorf("worst variant should rank last:\n%s", out)
	}
}
