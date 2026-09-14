package agent

import (
	"context"
	"fmt"
)

// VerifyFn judges an answer for a task, returning (satisfied, feedback).
type VerifyFn func(ctx context.Context, task, answer string) (bool, string)

// Reflexion runs a task, verifies the answer, and on failure generates a
// structured lesson from the verifier's feedback and retries ONCE with that
// lesson appended — a targeted recovery rather than a blind re-run.
func (a *Agent) Reflexion(ctx context.Context, task string, run TurnFunc, verify VerifyFn) (string, error) {
	answer, err := run(ctx, task)
	if err != nil {
		return answer, err
	}
	ok, feedback := verify(ctx, task, answer)
	if ok {
		return answer, nil
	}
	lesson := formatLesson(feedback)
	a.onEvent(Event{Type: "assistant", Text: "🪞 reflexion: " + lesson})
	return run(ctx, task+"\n\n"+lesson)
}

// formatLesson turns verifier feedback into a structured lesson to steer a retry.
func formatLesson(feedback string) string {
	if feedback == "" {
		feedback = "the previous answer did not fully satisfy the task"
	}
	return fmt.Sprintf("Lesson from the previous attempt (apply it now): %s", feedback)
}

// SendWithReflexion is Reflexion over the default Send strategy, using the
// built-in verifier agent to judge the answer.
func (a *Agent) SendWithReflexion(ctx context.Context, task string) (string, error) {
	return a.Reflexion(ctx, task, a.Send, func(ctx context.Context, task, answer string) (bool, string) {
		return a.runVerifier(ctx, task, answer)
	})
}
