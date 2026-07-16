package orchestrate

import (
	"context"

	"gophermind/internal/phaseflow"
)

// turnFunc runs one agent turn (mirrors agent.TurnFunc / agent.Agent.Send).
type turnFunc func(ctx context.Context, userInput string) (string, error)

// verifyFunc judges an answer against the task, returning (satisfied,
// feedback, err). err is a verifier-infrastructure failure (e.g. the verifier
// LLM call itself failed), distinct from an unsatisfied verdict.
type verifyFunc func(ctx context.Context, task, answer string) (satisfied bool, feedback string, err error)

// runWithVerify runs userPrompt via turn, then verifies the result against
// criteria. Satisfied on the first try -> "done". If not, it feeds the
// verifier's feedback back as one correction turn and re-verifies: satisfied
// -> "corrected", still unsatisfied -> "failed" (detail carries the final
// feedback). Any turn error -> "failed" (detail carries the error text). A
// verify-infrastructure error is treated leniently (never blocks), mirroring
// agent.runVerifier's own never-block-on-verifier-failure behavior.
func runWithVerify(ctx context.Context, turn turnFunc, verify verifyFunc, userPrompt string, criteria []string) (status, detail string) {
	answer, err := turn(ctx, userPrompt)
	if err != nil {
		return phaseflow.StatusFailed, err.Error()
	}

	ok, feedback, verr := verify(ctx, userPrompt, answer)
	if verr != nil || ok {
		return phaseflow.StatusDone, ""
	}

	corrected, err := turn(ctx, correctionPrompt(feedback))
	if err != nil {
		return phaseflow.StatusFailed, err.Error()
	}

	ok2, feedback2, verr2 := verify(ctx, userPrompt, corrected)
	if verr2 != nil || ok2 {
		return phaseflow.StatusCorrected, ""
	}
	return phaseflow.StatusFailed, feedback2
}

// correctionPrompt turns verifier feedback into the follow-up turn asking the
// agent to revise its previous answer.
func correctionPrompt(feedback string) string {
	return "A verifier reviewed your previous answer and judged it incomplete:\n" +
		feedback + "\nRevise your work and produce a corrected final answer."
}
