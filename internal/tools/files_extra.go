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

// FileStat returns a stat tool that returns file metadata.
func FileStat(root string) Tool {
	return Tool{
		Name:        "file_stat",
		Description: "Return file metadata: size, mtime, mode, line count, and whether it's a symlink.",
		Schema: object(map[string]any{
			"path": str("File path relative to the repository root."),
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			info, err := os.Lstat(full)
			if err != nil {
				return "", fmt.Errorf("stat %s: %w", a.Path, err)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "path: %s\n", a.Path)
			fmt.Fprintf(&b, "size: %d bytes\n", info.Size())
			fmt.Fprintf(&b, "mode: %s\n", info.Mode())
			fmt.Fprintf(&b, "mtime: %s\n", info.ModTime().Format("2006-01-02 15:04:05"))
			fmt.Fprintf(&b, "is_dir: %t\n", info.IsDir())
			fmt.Fprintf(&b, "is_symlink: %t\n", info.Mode()&os.ModeSymlink != 0)
			if info.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(full)
				fmt.Fprintf(&b, "symlink_target: %s\n", target)
			}
			if !info.IsDir() {
				data, err := os.ReadFile(full)
				if err == nil {
					lines := strings.Count(string(data), "\n") + 1
					fmt.Fprintf(&b, "lines: %d\n", lines)
				}
			}
			return b.String(), nil
		},
	}
}

// MoveFile returns a gated move/rename tool.
func MoveFile(root string) Tool {
	return Tool{
		Name:        "move_file",
		Description: "Move or rename a file within the repository. Path is relative to the repository root.",
		Schema: object(map[string]any{
			"source": str("Current file path."),
			"dest":   str("Destination file path."),
		}, "source", "dest"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Source string `json:"source"`
				Dest   string `json:"dest"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			srcFull, err := safety.SafeJoin(root, a.Source)
			if err != nil {
				return "", err
			}
			dstFull, err := safety.SafeJoin(root, a.Dest)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Dir(dstFull), 0o755); err != nil {
				return "", fmt.Errorf("mkdir for %s: %w", a.Dest, err)
			}
			if err := os.Rename(srcFull, dstFull); err != nil {
				return "", fmt.Errorf("move %s -> %s: %w", a.Source, a.Dest, err)
			}
			return fmt.Sprintf("moved %s -> %s", a.Source, a.Dest), nil
		},
	}
}

// DeleteFile returns a gated delete tool.
func DeleteFile(root string) Tool {
	return Tool{
		Name:        "delete_file",
		Description: "Delete a file from the repository. Path is relative to the repository root.",
		Schema: object(map[string]any{
			"path": str("File path relative to the repository root."),
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			if err := os.Remove(full); err != nil {
				return "", fmt.Errorf("delete %s: %w", a.Path, err)
			}
			return fmt.Sprintf("deleted %s", a.Path), nil
		},
	}
}

// Mkdir returns a directory creation tool.
func Mkdir(root string) Tool {
	return Tool{
		Name:        "mkdir",
		Description: "Create a directory (and parent directories as needed). Path is relative to the repository root.",
		Schema: object(map[string]any{
			"path": str("Directory path relative to the repository root."),
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(full, 0o755); err != nil {
				return "", fmt.Errorf("mkdir %s: %w", a.Path, err)
			}
			return fmt.Sprintf("created directory %s", a.Path), nil
		},
	}
}
