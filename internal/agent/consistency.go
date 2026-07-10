package agent

import (
	"context"
	"fmt"
	"strings"
)

// SelfConsistent runs the given turn strategy n times and returns the most
// common (majority-voted) answer, improving robustness on reasoning tasks where
// a single sample may be wrong. n <= 1 runs the strategy once. A sample error
// aborts and is returned.
func (a *Agent) SelfConsistent(ctx context.Context, userInput string, run TurnFunc, n int) (string, error) {
	if n <= 1 {
		return run(ctx, userInput)
	}
	answers := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ans, err := run(ctx, userInput)
		if err != nil {
			return ans, err
		}
		answers = append(answers, ans)
	}
	winner := majorityVote(answers)
	a.onEvent(Event{Type: "assistant", Text: fmt.Sprintf("🗳 self-consistency: chose the majority of %d samples", n)})
	return winner, nil
}

// SendSelfConsistent is SelfConsistent over the default Send strategy.
func (a *Agent) SendSelfConsistent(ctx context.Context, userInput string, n int) (string, error) {
	return a.SelfConsistent(ctx, userInput, a.Send, n)
}

// majorityVote returns the most frequent answer (compared after trimming
// surrounding whitespace). Ties are broken by first appearance (stable). The
// returned value is the trimmed form of the winning answer.
func majorityVote(answers []string) string {
	counts := map[string]int{}
	firstIdx := map[string]int{}
	for i, ans := range answers {
		key := strings.TrimSpace(ans)
		counts[key]++
		if _, seen := firstIdx[key]; !seen {
			firstIdx[key] = i
		}
	}
	bestKey := ""
	bestCount := -1
	for key, c := range counts {
		if c > bestCount || (c == bestCount && firstIdx[key] < firstIdx[bestKey]) {
			bestKey, bestCount = key, c
		}
	}
	return bestKey
}
