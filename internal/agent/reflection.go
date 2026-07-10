package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// reflectionTool is the tool definition for the reflection step on failure.
var reflectionTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        "_gophermind_reflect",
		Description: "Reflect on a tool failure and propose next steps. Call after a tool error to analyze what went wrong.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"error":     map[string]any{"type": "string", "description": "The error that occurred."},
				"tool":      map[string]any{"type": "string", "description": "The tool that failed."},
				"analysis":  map[string]any{"type": "string", "description": "What went wrong."},
				"next_step": map[string]any{"type": "string", "description": "Suggested next step."},
			},
			"required": []string{"error", "tool", "analysis", "next_step"},
		},
	},
}

// Reflection wraps the agent with reflection-on-failure support. When a tool
// call fails, the agent injects a structured reflection before retrying.
type Reflection struct {
	enabled bool
}

// NewReflection creates a new reflection handler.
func NewReflection(enabled bool) *Reflection {
	return &Reflection{enabled: enabled}
}

// ReflectOnFailure analyzes a tool error and returns a reflection message.
// If reflection is disabled, it returns an empty string.
func (r *Reflection) ReflectOnFailure(toolName, args, errMsg string) string {
	if !r.enabled {
		return ""
	}
	// The model itself will generate the reflection; this is a hint.
	return fmt.Sprintf("Tool '%s' failed: %s. Please reflect on what went wrong and propose a next step.", toolName, errMsg)
}

// ReflectionTool returns a tool that wraps the reflection step.
func ReflectionTool(ref *Reflection) tools.Tool {
	return tools.Tool{
		Name:        "_gophermind_reflect",
		Description: reflectionTool.Function.Description,
		Schema:      reflectionTool.Function.Parameters,
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Error    string `json:"error"`
				Tool     string `json:"tool"`
				Analysis string `json:"analysis"`
				NextStep string `json:"next_step"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			return fmt.Sprintf("Reflection:\nAnalysis: %s\nNext step: %s", args.Analysis, args.NextStep), nil
		},
	}
}
