package main

import (
	"encoding/json"
	"io"

	"gophermind/internal/agent"
)

// renderJSONResult writes a single machine-readable JSON object describing a
// run/ask outcome (--output-format json), so the command is scriptable beyond
// print mode. On error, is_error is true and the message is in "error".
func renderJSONResult(w io.Writer, answer string, u agent.UsageSnapshot, model string, runErr error) error {
	out := map[string]any{
		"type":           "result",
		"model":          model,
		"result":         answer,
		"is_error":       runErr != nil,
		"input_tokens":   u.PromptTokens,
		"output_tokens":  u.CompletionTokens,
		"total_tokens":   u.TotalTokens,
		"total_cost_usd": u.CostUSD,
	}
	if runErr != nil {
		out["error"] = runErr.Error()
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
