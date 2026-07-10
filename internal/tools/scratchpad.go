package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/safety"
)

// scratchpadPath is where durable task notes live (repo-scoped, under the
// agent's own dot-directory).
const scratchpadPath = ".gophermind/scratchpad.md"

// Scratchpad returns a tool giving the agent a durable notes file that survives
// across turns (and resumes), so it can record task state, decisions, and TODOs.
// Writes are confined to .gophermind/scratchpad.md, so it is not a general write
// primitive and is left non-gated.
func Scratchpad(root string) Tool {
	return Tool{
		Name:        "scratchpad",
		Description: "Durable task notes that persist across turns. action=read returns the notes; action=append adds text; action=clear empties them.",
		Schema: object(map[string]any{
			"action": str("One of: read, append, clear."),
			"text":   str("Text to append (required for action=append)."),
		}, "action"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Action string `json:"action"`
				Text   string `json:"text"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, scratchpadPath)
			if err != nil {
				return "", err
			}
			switch a.Action {
			case "read":
				data, err := os.ReadFile(full)
				if err != nil || len(strings.TrimSpace(string(data))) == 0 {
					return "(scratchpad is empty)", nil
				}
				return string(data), nil
			case "append":
				if strings.TrimSpace(a.Text) == "" {
					return "", fmt.Errorf("append requires non-empty text")
				}
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					return "", err
				}
				f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err != nil {
					return "", err
				}
				defer f.Close()
				if _, err := f.WriteString(strings.TrimRight(a.Text, "\n") + "\n"); err != nil {
					return "", err
				}
				return "appended to scratchpad", nil
			case "clear":
				if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
					return "", err
				}
				return "scratchpad cleared", nil
			default:
				return "", fmt.Errorf("unknown action %q: use read, append, or clear", a.Action)
			}
		},
	}
}
