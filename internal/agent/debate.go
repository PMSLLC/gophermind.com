package agent

import (
	"context"

	"gophermind/internal/llm"
)

// PickFn chooses or synthesizes a final answer from two candidate answers to a
// task (e.g. via an LLM judge).
type PickFn func(ctx context.Context, task, candA, candB string) (string, error)

// Debate runs a task twice to produce two independent candidate answers, then —
// when they disagree — has pick choose or synthesize the better one. When the
// candidates already agree (consensus), that answer is returned without invoking
// the judge. Improves answer quality on ambiguous tasks.
func (a *Agent) Debate(ctx context.Context, task string, run TurnFunc, pick PickFn) (string, error) {
	candA, err := run(ctx, task)
	if err != nil {
		return candA, err
	}
	candB, err := run(ctx, task)
	if err != nil {
		return candB, err
	}
	if candA == candB {
		a.onEvent(Event{Type: "assistant", Text: "🤝 debate: candidates agree (consensus)"})
		return candA, nil
	}
	a.onEvent(Event{Type: "assistant", Text: "⚖ debate: candidates differ — judging"})
	return pick(ctx, task, candA, candB)
}

// SendDebate is Debate over the default Send strategy, synthesizing divergent
// candidates with the model.
func (a *Agent) SendDebate(ctx context.Context, task string) (string, error) {
	return a.Debate(ctx, task, a.Send, a.synthesize)
}

// synthesize asks the model to pick or merge the better of two candidate answers.
func (a *Agent) synthesize(ctx context.Context, task, candA, candB string) (string, error) {
	msgs := []llm.Message{
		{Role: "system", Content: "You are a judge. Given a task and two candidate answers, produce the single best final answer, merging their strengths and correcting errors. Output only the final answer."},
		{Role: "user", Content: "Task:\n" + task + "\n\nCandidate A:\n" + candA + "\n\nCandidate B:\n" + candB + "\n\nBest final answer:"},
	}
	reply, usage, err := a.llm.Stream(ctx, msgs, nil, func(string) {})
	if err != nil {
		// Fall back to the first candidate if the judge is unavailable.
		return candA, nil
	}
	a.usage.Add(usage)
	return reply.Content, nil
}
