package orchestrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gophermind/internal/llm"
	"gophermind/internal/phaseflow"
)

// reviseToolName is the structured revision LLMReviser asks the model to
// emit, mirroring the verifierToolName / planTool convention in
// internal/agent: a strict tool-call response is far more reliable to parse
// than free text.
const reviseToolName = "_gophermind_revise"

// reviseSystemPrompt frames the reviser's job: every candidate model already
// failed this task, so the point is not to retry it, it is to work out what
// the failures had in common and fix the definition itself.
const reviseSystemPrompt = `You are revising a task definition that every candidate model failed to satisfy. You will be given the task's current deliverable and acceptance criteria, plus the full history of attempts against it - each with which model tried it and specifically why it failed. Before proposing anything, work out what the failures have in common: a test that is too strict, a deliverable that is under-specified or ambiguous, or a genuine edge case every model missed the same way. Then produce a revised deliverable and/or test that addresses that pattern specifically, not a generic "try again". Respond ONLY by calling the ` + reviseToolName + ` function with your revision.`

// reviseTool is the structured revision the model emits.
var reviseTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        reviseToolName,
		Description: "Propose a revised task definition after every candidate model failed it.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"note": map[string]any{
					"type":        "string",
					"description": "What the failures had in common, what changed, and why. Required even if only the test changed.",
				},
				"deliverable": map[string]any{
					"type":        "string",
					"description": "The revised deliverable/description. Leave empty if the deliverable itself is unchanged.",
				},
				"test": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "The revised acceptance criteria, as a full replacement list. Leave empty if the test itself is unchanged.",
				},
			},
			"required": []string{"note"},
		},
	},
}

// LLMReviser implements phaseflow.Reviser by asking an LLM to rewrite a task
// definition after every candidate model failed it. It uses Model - the
// strongest configured model, the same choice the contract step makes -
// rather than the task's own tier: this is a reasoning task about why work
// failed, not a coding task, and the task's own (possibly weak) tier has no
// bearing on which model should judge that.
//
// Each Revise call runs on a clone of Client configured for Model, so it
// never mutates the shared client. Revise
// must therefore only ever be called strictly sequentially against a given
// Client, never concurrently with another call that also mutates its Model -
// which is exactly how phaseflow.ExecuteWithReviser calls a Reviser: only
// between rounds, after every wave has already finished, never while another
// call is using the same client.
type LLMReviser struct {
	Client *llm.Client
	Model  string
}

// NewLLMReviser builds an LLMReviser. client is shared with other callers
// (see the Client field's doc comment for the concurrency contract that
// sharing requires); model is the strongest configured model to revise with.
func NewLLMReviser(client *llm.Client, model string) *LLMReviser {
	return &LLMReviser{Client: client, Model: model}
}

// Revise implements phaseflow.Reviser. It sends the task and its full
// attempt history - every attempt's model and reason, not just the latest
// one - and asks the model what the failures have in common before it
// proposes a fix. A malformed or missing response is an error, never a
// silently empty Revision: an empty Revision would otherwise be recorded by
// phaseflow.ApplyRevision as if the model had genuinely found nothing to
// change, which is not the same thing as the model failing to answer.
func (r *LLMReviser) Revise(ctx context.Context, t phaseflow.Task, attempts []phaseflow.Attempt) (phaseflow.Revision, error) {
	if r.Client == nil {
		return phaseflow.Revision{}, errors.New("orchestrate: LLMReviser has no client")
	}

	msgs := []llm.Message{
		{Role: "system", Content: reviseSystemPrompt},
		{Role: "user", Content: buildRevisePrompt(t, attempts)},
	}

	// Use a clone rather than setting and restoring Model on the shared
	// client. The restore made this safe only while revision never overlapped
	// another user of that client, which is true today because revision runs
	// between waves, but is an assumption about scheduling rather than a
	// property of the code. A clone needs no such assumption, and cannot leave
	// the model changed if this path ever returns early.
	client := r.Client
	if r.Model != "" {
		client = r.Client.CloneForModel(r.Model)
	}
	reply, _, err := client.Complete(ctx, msgs, []llm.Tool{reviseTool})
	if err != nil {
		return phaseflow.Revision{}, fmt.Errorf("orchestrate: revise %q: %w", t.ID, err)
	}

	for _, tc := range reply.ToolCalls {
		if tc.Function.Name != reviseToolName {
			continue
		}
		var v struct {
			Note        string   `json:"note"`
			Deliverable string   `json:"deliverable"`
			Test        []string `json:"test"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &v); err != nil {
			return phaseflow.Revision{}, fmt.Errorf("orchestrate: parse revision for %q: %w", t.ID, err)
		}
		return phaseflow.Revision{Note: v.Note, Deliverable: v.Deliverable, Test: v.Test}, nil
	}

	return phaseflow.Revision{}, fmt.Errorf("orchestrate: model produced no revision for %q", t.ID)
}

// buildRevisePrompt renders the task and its full attempt history for the
// reviser: every attempt's model and reason, in order, followed by an
// explicit ask for what they have in common. That ask is the difference
// between a revision that addresses the actual pattern and a generic "try
// again" - see the source spec's acceptance test in
// docs/superpowers/specs/2026-09-11-harness-pipeline-source-spec.md section 3.
func buildRevisePrompt(t phaseflow.Task, attempts []phaseflow.Attempt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s: %s\n\n%s\n", t.ID, t.Title, t.Description)
	b.WriteString("\nCurrent acceptance criteria:\n")
	for _, c := range t.AcceptanceCriteria {
		fmt.Fprintf(&b, "- %s\n", c)
	}

	fmt.Fprintf(&b, "\nEvery candidate model failed this task. Full attempt history (%d attempts):\n", len(attempts))
	for i, a := range attempts {
		fmt.Fprintf(&b, "%d. model=%s verdict=%s reason=%s\n", i+1, a.Model, a.Verdict, a.Reason)
	}

	b.WriteString("\nWhat do these failures have in common? Propose a revised deliverable and/or test that addresses that pattern specifically, not a generic \"try again\". Call ")
	b.WriteString(reviseToolName)
	b.WriteString(" with your answer.\n")
	return b.String()
}
