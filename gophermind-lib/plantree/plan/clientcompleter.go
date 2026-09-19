package plan

import (
	"context"
	"errors"

	"gophermind/gophermind-lib/llm"
)

// plannerSystemPrompt replaces the coding agent's system prompt. A planning
// pass only turns a prompt into JSON, so it must not be nudged to explore,
// edit files or run commands.
const plannerSystemPrompt = "You are a planning assistant. You break project briefs into plans and reply only with what the user asks for. You never call tools, write files or run commands."

// ClientCompleter runs every prompt as a brand-new two-message conversation
// (a planner system prompt and the prompt itself) with no tools, so no pass
// can inherit, or overflow on, an earlier pass's context.
type ClientCompleter struct {
	Client *llm.Client
}

// Complete implements Completer.
func (c ClientCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	msgs := []llm.Message{
		{Role: "system", Content: plannerSystemPrompt},
		{Role: "user", Content: prompt},
	}
	reply, _, err := c.Client.Stream(ctx, msgs, nil, nil)
	if err != nil {
		return "", err
	}
	if reply.Content == "" {
		return "", errors.New("the model returned an empty reply")
	}
	return reply.Content, nil
}
