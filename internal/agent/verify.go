package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/llm"
)

const verifierToolName = "_gophermind_verify"

// verifierSystemPrompt frames the second (verifier) agent's job: judge whether a
// proposed answer actually satisfies the task, and if not, say concretely why.
const verifierSystemPrompt = `You are a strict verification agent. Given a task and a proposed answer, decide whether the answer FULLY and CORRECTLY satisfies the task. Be skeptical: check for missing steps, unhandled edge cases, unverified claims, and incomplete work. Respond ONLY by calling the ` + verifierToolName + ` function with your verdict.`

// verifierTool is the structured verdict the verifier emits.
var verifierTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        verifierToolName,
		Description: "Report whether the proposed answer satisfies the task.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"satisfied": map[string]any{"type": "boolean", "description": "True if the answer fully satisfies the task."},
				"feedback":  map[string]any{"type": "string", "description": "If not satisfied, concrete issues to fix."},
			},
			"required": []string{"satisfied"},
		},
	},
}

// TurnFunc is a turn strategy sharing Send's signature (Send, PlanThenExecute,
// DispatchParallel, SendWithBudget all satisfy it).
type TurnFunc func(ctx context.Context, userInput string) (string, error)

// VerifyResult runs a turn via the given strategy, then has a second (verifier)
// agent check the result. If the verifier judges the answer incomplete, it feeds
// the concrete issues back for one correction round (using the same strategy)
// and returns the revised answer. A verifier error never blocks: the original
// answer is returned.
func (a *Agent) VerifyResult(ctx context.Context, userInput string, run TurnFunc) (string, error) {
	answer, err := run(ctx, userInput)
	if err != nil {
		return answer, err
	}

	ok, feedback := a.runVerifier(ctx, userInput, answer)
	if ok {
		a.onEvent(Event{Type: "assistant", Text: "✅ Verifier: answer accepted"})
		return answer, nil
	}

	a.onEvent(Event{Type: "assistant", Text: "🔍 Verifier flagged issues: " + feedback})
	return run(ctx, "A verifier reviewed your previous answer and judged it incomplete:\n"+
		feedback+"\nRevise your work and produce a corrected final answer.")
}

// SendWithVerification is VerifyResult over the default Send strategy.
func (a *Agent) SendWithVerification(ctx context.Context, userInput string) (string, error) {
	return a.VerifyResult(ctx, userInput, a.Send)
}

// runVerifier asks a fresh verifier agent (its own minimal context, not the main
// conversation) to judge the answer. Returns (satisfied, feedback).
func (a *Agent) runVerifier(ctx context.Context, task, answer string) (bool, string) {
	msgs := []llm.Message{
		{Role: "system", Content: verifierSystemPrompt},
		{Role: "user", Content: fmt.Sprintf("Task:\n%s\n\nProposed answer:\n%s\n\nCall %s with your verdict.", task, answer, verifierToolName)},
	}
	reply, usage, err := a.llm.Stream(ctx, msgs, []llm.Tool{verifierTool}, func(string) {})
	if err != nil {
		return true, "" // never block on verifier failure
	}
	a.usage.Add(usage)
	a.onEvent(Event{Type: "usage", Usage: a.usage.Snapshot()})
	return parseVerdict(reply)
}

// parseVerdict extracts the verifier's decision from its reply. Absent a
// well-formed verdict tool call, it leniently accepts (an uncooperative verifier
// should not block a plausibly-correct answer).
func parseVerdict(reply llm.Message) (satisfied bool, feedback string) {
	for _, tc := range reply.ToolCalls {
		if tc.Function.Name != verifierToolName {
			continue
		}
		var v struct {
			Satisfied bool   `json:"satisfied"`
			Feedback  string `json:"feedback"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &v); err != nil {
			return true, ""
		}
		return v.Satisfied, v.Feedback
	}
	return true, ""
}
