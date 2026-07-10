package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// diffEntry holds a proposed file edit before it is applied.
type diffEntry struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// diffPreviewTool is the tool definition for accumulating and previewing diffs.
var diffPreviewTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        "_gophermind_diff_preview",
		Description: "Accumulate proposed file edits and show a combined diff before applying. Call with action=preview to see all accumulated changes.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "Action: accumulate or preview.",
					"enum":        []string{"accumulate", "preview"},
				},
				"path": map[string]any{"type": "string", "description": "File path being edited."},
				"old":  map[string]any{"type": "string", "description": "Text to replace."},
				"new":  map[string]any{"type": "string", "description": "Replacement text."},
			},
			"required": []string{"action"},
		},
	},
}

// diffAccumulator collects proposed edits across a turn and presents a
// combined diff before the model proceeds.
type diffAccumulator struct {
	entries []diffEntry
}

func newDiffAccumulator() *diffAccumulator {
	return &diffAccumulator{}
}

// Add records a proposed edit.
func (da *diffAccumulator) Add(path, old, new string) {
	da.entries = append(da.entries, diffEntry{Path: path, Old: old, New: new})
}

// Preview generates a combined diff string of all accumulated entries.
func (da *diffAccumulator) Preview() string {
	var b strings.Builder
	for i, e := range da.entries {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		fmt.Fprintf(&b, "📝 %s:\n", e.Path)
		if e.Old != "" {
			fmt.Fprintf(&b, "  - %s\n", strings.ReplaceAll(e.Old, "\n", "\n  - "))
		}
		if e.New != "" {
			fmt.Fprintf(&b, "  + %s\n", strings.ReplaceAll(e.New, "\n", "\n  + "))
		}
	}
	if len(da.entries) == 0 {
		return "(no edits accumulated)"
	}
	return b.String()
}

// DiffPreviewTool returns a tool that accumulates and previews diffs.
func DiffPreviewTool() tools.Tool {
	da := newDiffAccumulator()
	return tools.Tool{
		Name:        "_gophermind_diff_preview",
		Description: diffPreviewTool.Function.Description,
		Schema:      diffPreviewTool.Function.Parameters,
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Action string `json:"action"`
				Path   string `json:"path"`
				Old    string `json:"old"`
				New    string `json:"new"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			switch args.Action {
			case "accumulate":
				da.Add(args.Path, args.Old, args.New)
				return fmt.Sprintf("Accumulated edit for %s. Total: %d edits.", args.Path, len(da.entries)), nil
			case "preview":
				return da.Preview(), nil
			default:
				return "", fmt.Errorf("unknown action: %s", args.Action)
			}
		},
	}
}

// diffBuffer captures write_file and edit_file results to build a combined
// diff preview at the end of a turn.
type diffBuffer struct {
	entries []diffEntry
}

func newDiffBuffer() *diffBuffer {
	return &diffBuffer{}
}

// CaptureWrite records a write_file result for diff preview.
func (db *diffBuffer) CaptureWrite(path, content string) {
	db.entries = append(db.entries, diffEntry{
		Path: path,
		New:  content,
	})
}

// CaptureEdit records an edit_file result for diff preview.
func (db *diffBuffer) CaptureEdit(path, old, new string) {
	db.entries = append(db.entries, diffEntry{
		Path: path,
		Old:  old,
		New:  new,
	})
}

// Preview generates a combined diff of all captured edits.
func (db *diffBuffer) Preview() string {
	var b bytes.Buffer
	for i, e := range db.entries {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "=== %s ===\n", e.Path)
		if e.Old != "" {
			b.WriteString("- " + strings.ReplaceAll(e.Old, "\n", "\n- ") + "\n")
		}
		if e.New != "" {
			b.WriteString("+ " + strings.ReplaceAll(e.New, "\n", "\n+ ") + "\n")
		}
	}
	return b.String()
}
