package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gophermind/internal/safety"
)

// PatchApply returns a tool that applies a unified diff across files.
func PatchApply(root string) Tool {
	return Tool{
		Name:        "apply_patch",
		Description: "Apply a unified diff patch to one or more files atomically.",
		Schema: object(map[string]any{
			"patch": str("Unified diff patch text to apply."),
		}, "patch"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Patch string `json:"patch"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(a.Patch) == "" {
				return "", fmt.Errorf("patch is empty")
			}
			// Parse the unified diff and apply each hunk.
			files, hunks, err := parseUnifiedDiff(a.Patch)
			if err != nil {
				return "", fmt.Errorf("parse patch: %w", err)
			}
			// Phase 1: read every target and compute its updated content WITHOUT
			// writing. If any file is missing or a hunk fails to apply, abort here
			// so nothing is modified (true atomicity).
			type pendingWrite struct {
				full, orig, updated string
			}
			var pending []pendingWrite
			for path, hunkList := range hunks {
				full, err := safety.SafeJoin(root, path)
				if err != nil {
					return "", err
				}
				data, err := os.ReadFile(full)
				if err != nil {
					return "", fmt.Errorf("read %s: %w", path, err)
				}
				updated, err := applyHunks(string(data), hunkList)
				if err != nil {
					return "", fmt.Errorf("apply to %s: %w", path, err)
				}
				pending = append(pending, pendingWrite{full: full, orig: string(data), updated: updated})
			}

			// Phase 2: write all files. If a write fails, roll back the ones already
			// written to their original content so the tree is left consistent.
			var written []pendingWrite
			for _, p := range pending {
				if err := atomicWrite(p.full, []byte(p.updated)); err != nil {
					for _, w := range written {
						_ = atomicWrite(w.full, []byte(w.orig))
					}
					return "", fmt.Errorf("write %s (rolled back %d file(s)): %w", p.full, len(written), err)
				}
				written = append(written, p)
			}
			return fmt.Sprintf("Applied patch: %d files, %d hunks.", len(files), len(hunks)), nil
		},
	}
}

// parseUnifiedDiff parses a unified diff and returns file paths and their hunks.
func parseUnifiedDiff(patch string) ([]string, map[string][]hunk, error) {
	lines := strings.Split(patch, "\n")
	var files []string
	hunks := make(map[string][]hunk)
	var currentFile string

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git") {
			// Extract file path from "diff --git a/path b/path"
			parts := strings.SplitN(line, "b/", 2)
			if len(parts) == 2 {
				currentFile = strings.TrimPrefix(parts[1], "a/")
				files = append(files, currentFile)
				hunks[currentFile] = nil
			}
		} else if strings.HasPrefix(line, "@@ ") && currentFile != "" {
			// Parse hunk header: @@ -oldStart,oldCount +newStart,newCount @@
			var oldStart, oldCount, newStart, newCount int
			_, err := fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &oldStart, &oldCount, &newStart, &newCount)
			if err != nil {
				// Try without counts
				_, err = fmt.Sscanf(line, "@@ -%d +%d @@", &oldStart, &newStart)
				if err != nil {
					continue
				}
				oldCount, newCount = 1, 1
			}
			hunks[currentFile] = append(hunks[currentFile], hunk{
				OldStart: oldStart, OldCount: oldCount,
				NewStart: newStart, NewCount: newCount,
			})
		} else if currentFile != "" && len(hunks[currentFile]) > 0 {
			h := &hunks[currentFile][len(hunks[currentFile])-1]
			switch {
			case strings.HasPrefix(line, "+"):
				h.NewLines = append(h.NewLines, strings.TrimPrefix(line, "+"))
				h.NewCount++
			case strings.HasPrefix(line, "-"):
				h.OldLines = append(h.OldLines, strings.TrimPrefix(line, "-"))
				h.OldCount++
			case strings.HasPrefix(line, " "):
				h.OldLines = append(h.OldLines, strings.TrimPrefix(line, " "))
				h.NewLines = append(h.NewLines, strings.TrimPrefix(line, " "))
				h.OldCount++
				h.NewCount++
			}
		}
	}
	return files, hunks, nil
}

type hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	OldLines, NewLines []string
}

func applyHunks(content string, hunks []hunk) (string, error) {
	lines := strings.Split(content, "\n")
	// Remove trailing empty string from split if file ends with newline.
	endsWithNewline := strings.HasSuffix(content, "\n")
	if endsWithNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	result := make([]string, 0, len(lines))
	lineIdx := 0

	for _, h := range hunks {
		// Add lines before this hunk.
		for lineIdx < h.NewStart-1 && lineIdx < len(lines) {
			result = append(result, lines[lineIdx])
			lineIdx++
		}
		// Add new lines from the hunk.
		result = append(result, h.NewLines...)
		lineIdx += h.OldCount
	}
	// Add remaining lines.
	for lineIdx < len(lines) {
		result = append(result, lines[lineIdx])
		lineIdx++
	}

	if endsWithNewline {
		return strings.Join(result, "\n") + "\n", nil
	}
	return strings.Join(result, "\n"), nil
}
