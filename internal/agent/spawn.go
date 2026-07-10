package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// SpawnAgentToolName is the tool name for spawning a sub-agent.
const SpawnAgentToolName = "spawn_agent"

// spawnAgentTool is the tool definition for spawning a focused child agent.
var spawnAgentTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        SpawnAgentToolName,
		Description: "Spawn a focused child agent with its own context for an isolated subtask. Returns only the result.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task":        map[string]any{"type": "string", "description": "The focused subtask to execute."},
				"max_iter":    map[string]any{"type": "integer", "description": "Maximum iterations for the child agent (default: 10)."},
				"system_hint": map[string]any{"type": "string", "description": "Optional hint to guide the child agent's behavior."},
			},
			"required": []string{"task"},
		},
	},
}

// SpawnAgent runs a focused child agent with its own conversation context for
// an isolated subtask. The child agent uses the same LLM client and tool
// registry but starts with a clean conversation, so it doesn't pollute the
// parent's context. The result is returned as a string.
func (a *Agent) SpawnAgent(ctx context.Context, task string, maxIter int, systemHint string) (string, error) {
	if maxIter <= 0 {
		maxIter = 10
	}

	// Build a child agent with its own conversation.
	child := &Agent{
		llm:     a.llm,
		reg:     a.reg,
		maxIter: maxIter,
		approve: a.approve,
		onEvent: func(e Event) {
			// Forward child events with a "child:" prefix so the parent can see them.
			e.Text = "[child] " + e.Text
			a.onEvent(e)
		},
		caps: a.caps,
	}

	// Seed with system hint if provided.
	if systemHint != "" {
		child.msgs = []llm.Message{{Role: "system", Content: systemHint}}
	}

	// Run the child's task.
	return child.Send(ctx, task)
}

// SpawnAgentTool returns a tool that wraps SpawnAgent, suitable for registering
// in the tool registry.
func SpawnAgentTool(agent *Agent) tools.Tool {
	return tools.Tool{
		Name:        SpawnAgentToolName,
		Description: spawnAgentTool.Function.Description,
		Schema:      spawnAgentTool.Function.Parameters,
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Task       string `json:"task"`
				MaxIter    int    `json:"max_iter"`
				SystemHint string `json:"system_hint"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if args.Task == "" {
				return "", fmt.Errorf("task is required")
			}
			result, err := agent.SpawnAgent(ctx, args.Task, args.MaxIter, args.SystemHint)
			if err != nil {
				return "", err
			}
			return result, nil
		},
	}
}
