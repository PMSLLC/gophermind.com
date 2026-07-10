package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// Checkpoint represents a saved point in the conversation that can be restored.
type Checkpoint struct {
	Name  string
	Index int // index into a.msgs
}

// checkpoints holds named conversation checkpoints.
type checkpoints struct {
	m map[string]int // name -> index in msgs
}

func newCheckpoints() *checkpoints {
	return &checkpoints{m: make(map[string]int)}
}

// Add records a checkpoint at the current conversation state.
func (cs *checkpoints) Add(name string, index int) {
	cs.m[name] = index
}

// Get returns the index for a named checkpoint.
func (cs *checkpoints) Get(name string) (int, bool) {
	idx, ok := cs.m[name]
	return idx, ok
}

// Remove deletes a checkpoint.
func (cs *checkpoints) Remove(name string) {
	delete(cs.m, name)
}

// checkpointTool is the tool definition for conversation checkpoints.
var checkpointTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        "_gophermind_checkpoint",
		Description: "Manage conversation checkpoints: snapshot, list, restore, or delete conversation states.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "Action: snapshot, list, restore, or delete.",
					"enum":        []string{"snapshot", "list", "restore", "delete"},
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Checkpoint name (required for snapshot, restore, delete).",
				},
			},
			"required": []string{"action"},
		},
	},
}

// Checkpoints wraps the agent with conversation checkpoint support.
type Checkpoints struct {
	cs *checkpoints
}

// NewCheckpoints creates a new checkpoint manager.
func NewCheckpoints() *Checkpoints {
	return &Checkpoints{cs: newCheckpoints()}
}

// Snapshot saves the current conversation state under the given name.
func (a *Agent) Snapshot(name string) {
	a.checkpoints.Add(name, len(a.msgs))
}

// Restore restores the conversation to a previously saved checkpoint.
func (a *Agent) Restore(name string) ([]llm.Message, bool) {
	idx, ok := a.checkpoints.Get(name)
	if !ok {
		return nil, false
	}
	return a.msgs[:idx], true
}

// List returns all checkpoint names.
func (a *Agent) ListCheckpoints() []string {
	names := make([]string, 0, len(a.checkpoints.m))
	for n := range a.checkpoints.m {
		names = append(names, n)
	}
	return names
}

// Delete removes a checkpoint.
func (a *Agent) DeleteCheckpoint(name string) {
	a.checkpoints.Remove(name)
}

// CheckpointTool returns a tool that wraps checkpoint operations.
func CheckpointTool(agent *Agent, cs *Checkpoints) tools.Tool {
	return tools.Tool{
		Name:        "_gophermind_checkpoint",
		Description: checkpointTool.Function.Description,
		Schema:      checkpointTool.Function.Parameters,
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Action string `json:"action"`
				Name   string `json:"name"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			switch args.Action {
			case "snapshot":
				if args.Name == "" {
					return "", fmt.Errorf("name is required for snapshot")
				}
				agent.Snapshot(args.Name)
				return fmt.Sprintf("Checkpoint '%s' saved at turn %d.", args.Name, len(agent.msgs)), nil
			case "list":
				names := agent.ListCheckpoints()
				if len(names) == 0 {
					return "No checkpoints.", nil
				}
				return "Checkpoints: " + strings.Join(names, ", "), nil
			case "restore":
				if args.Name == "" {
					return "", fmt.Errorf("name is required for restore")
				}
				msgs, ok := agent.Restore(args.Name)
				if !ok {
					return fmt.Sprintf("Checkpoint '%s' not found.", args.Name), nil
				}
				agent.msgs = msgs
				return fmt.Sprintf("Restored to checkpoint '%s'.", args.Name), nil
			case "delete":
				if args.Name == "" {
					return "", fmt.Errorf("name is required for delete")
				}
				agent.DeleteCheckpoint(args.Name)
				return fmt.Sprintf("Checkpoint '%s' deleted.", args.Name), nil
			default:
				return "", fmt.Errorf("unknown action: %s", args.Action)
			}
		},
	}
}
