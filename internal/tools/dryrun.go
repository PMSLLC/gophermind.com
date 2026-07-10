package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/safety"
)

// WrapDryRun returns a copy of ts in which every gated (mutating) tool is
// replaced by a no-op that reports the call it *would* have made instead of
// executing it. Read-only tools pass through unchanged, so the agent can still
// inspect the repo while previewing its mutations.
func WrapDryRun(ts []Tool) []Tool {
	out := make([]Tool, len(ts))
	for i, t := range ts {
		if safety.Gated(t.Name) {
			out[i] = dryRunTool(t)
		} else {
			out[i] = t
		}
	}
	return out
}

// dryRunTool replaces a tool's Run with an intent reporter.
func dryRunTool(t Tool) Tool {
	name := t.Name
	t.Run = func(_ context.Context, raw json.RawMessage) (string, error) {
		args := string(raw)
		if args == "" {
			args = "{}"
		}
		return fmt.Sprintf("[dry-run] would call %s with %s (not executed)", name, args), nil
	}
	return t
}
