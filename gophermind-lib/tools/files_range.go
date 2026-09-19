package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gophermind/gophermind-lib/safety"
)

// maxReadBytes caps what one read_file call returns, roughly 8k tokens. A
// model that reads whole documents one after another otherwise fills its
// context window with tool output; the cap forces it to read in ranges.
const maxReadBytes = 32_000

// ReadFileRange returns the read_file tool with optional line-range support.
// When rangeStart and rangeEnd are both 0, the full file is read (default).
// Otherwise, only lines rangeStart through rangeEnd (1-indexed, inclusive) are
// returned, with line numbers prepended.
func ReadFileRange(root string) Tool {
	return Tool{
		Name:        "read_file",
		Description: "Read a UTF-8 text file. Path is relative to the repository root. A single call returns at most about 32 KB, cut at a line boundary with a notice saying where it stopped, so read large files in pieces with range_start and range_end (1-indexed, inclusive). Grep or list headings first to find the piece you need.",
		Schema: object(map[string]any{
			"path":              str("File path relative to the repository root."),
			"range_start":       map[string]any{"type": "integer", "description": "Starting line number (1-indexed). Omit for full file."},
			"range_end":         map[string]any{"type": "integer", "description": "Ending line number (1-indexed, inclusive). Omit for full file."},
			"with_line_numbers": map[string]any{"type": "boolean", "description": "When true, prepend line numbers to each line."},
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path            string `json:"path"`
				RangeStart      *int   `json:"range_start"`
				RangeEnd        *int   `json:"range_end"`
				WithLineNumbers *bool  `json:"with_line_numbers"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			b, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", a.Path, err)
			}

			// Binary file guard: check for null bytes.
			if len(b) > 0 && containsNullBytes(b) {
				return "", fmt.Errorf("binary file: %s (use a different tool for binary data)", a.Path)
			}

			content := string(b)
			lines := strings.Split(content, "\n")
			// Remove trailing empty line from split if file ends with newline.
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}

			var result string
			firstLine := 1
			if a.RangeStart != nil && a.RangeEnd != nil {
				start := *a.RangeStart
				end := *a.RangeEnd
				if start < 1 {
					start = 1
				}
				if end > len(lines) {
					end = len(lines)
				}
				if start > end {
					return "", fmt.Errorf("range_start (%d) > range_end (%d)", start, end)
				}
				if start > len(lines) {
					return "", fmt.Errorf("range_start (%d) beyond file length (%d)", start, len(lines))
				}
				firstLine = start
				selected := lines[start-1 : end]
				if a.WithLineNumbers != nil && *a.WithLineNumbers {
					for i, line := range selected {
						if i > 0 {
							result += "\n"
						}
						result += fmt.Sprintf("%d: %s", start+i, line)
					}
				} else {
					result = strings.Join(selected, "\n")
				}
				result += fmt.Sprintf("\n\n[lines %d-%d of %d]", start, end, len(lines))
			} else {
				if a.WithLineNumbers != nil && *a.WithLineNumbers {
					for i, line := range lines {
						if i > 0 {
							result += "\n"
						}
						result += fmt.Sprintf("%d: %s", i+1, line)
					}
				} else {
					result = content
				}
			}

			return capRead(result, firstLine, len(lines)), nil
		},
	}
}

func containsNullBytes(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// capRead cuts result to maxReadBytes at a line boundary and appends a notice
// naming how far it got, so the caller knows to ask for the rest by range.
func capRead(result string, firstLine, totalLines int) string {
	if len(result) <= maxReadBytes {
		return result
	}
	cut := strings.LastIndex(result[:maxReadBytes], "\n")
	if cut <= 0 {
		cut = maxReadBytes
	}
	last := firstLine + strings.Count(result[:cut], "\n")
	return fmt.Sprintf("%s\n\n[truncated: showed through line %d of %d, a read is capped at %d bytes. Use range_start=%d to continue.]",
		result[:cut], last, totalLines, maxReadBytes, last+1)
}
