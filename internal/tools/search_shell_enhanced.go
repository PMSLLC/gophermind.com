package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"gophermind/internal/safety"
)

// SearchEnhanced returns an enhanced search tool with context lines, scoped
// search, case/whole-word/literal flags, and result pagination.
func SearchEnhanced(root string) Tool {
	return Tool{
		Name:        "search",
		Description: "Search the repository for a pattern. Supports context lines (-A/-B/-C), scoped search (path/glob/type filters), case/whole-word/literal modes, and pagination.",
		Schema: object(map[string]any{
			"pattern":          str("Regular expression to search for."),
			"context":          map[string]any{"type": "integer", "description": "Number of context lines to show before and after matches (default: 0)."},
			"path":             str("Optional path/glob filter (e.g. '*.go', 'src/')."),
			"type":             str("Optional file type filter (e.g. 'go', 'js', 'md')."),
			"case_insensitive": map[string]any{"type": "boolean", "description": "Case-insensitive search."},
			"whole_word":       map[string]any{"type": "boolean", "description": "Match whole words only."},
			"fixed_string":     map[string]any{"type": "boolean", "description": "Treat pattern as a fixed string, not regex."},
			"page":             map[string]any{"type": "integer", "description": "Page number for pagination (1-indexed)."},
			"page_size":        map[string]any{"type": "integer", "description": "Results per page (default: 50)."},
		}, "pattern"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Pattern         string `json:"pattern"`
				Context         *int   `json:"context"`
				Path            string `json:"path"`
				Type            string `json:"type"`
				CaseInsensitive *bool  `json:"case_insensitive"`
				WholeWord       *bool  `json:"whole_word"`
				FixedString     *bool  `json:"fixed_string"`
				Page            *int   `json:"page"`
				PageSize        *int   `json:"page_size"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(a.Pattern) == "" {
				return "", fmt.Errorf("empty pattern")
			}

			var args []string
			args = append(args, "-n", "--hidden", "--glob", "!.git")

			// Context lines.
			if a.Context != nil && *a.Context > 0 {
				args = append(args, "-C", fmt.Sprintf("%d", *a.Context))
			}

			// Path filter.
			if a.Path != "" {
				args = append(args, "-g", a.Path)
			}

			// Type filter.
			if a.Type != "" {
				args = append(args, "--type", a.Type)
			}

			// Case insensitive.
			if a.CaseInsensitive != nil && *a.CaseInsensitive {
				args = append(args, "-i")
			}

			// Whole word.
			if a.WholeWord != nil && *a.WholeWord {
				args = append(args, "-w")
			}

			// Fixed string.
			if a.FixedString != nil && *a.FixedString {
				args = append(args, "-F")
			}

			// The "--" terminator forces the pattern to be positional.
			args = append(args, "--", a.Pattern)

			var cmd *exec.Cmd
			if _, err := exec.LookPath("rg"); err == nil {
				cmd = exec.CommandContext(ctx, "rg", args...)
			} else {
				// Fallback to grep with basic options.
				grepArgs := []string{"-rn", "--exclude-dir=.git"}
				if a.CaseInsensitive != nil && *a.CaseInsensitive {
					grepArgs = append(grepArgs, "-i")
				}
				if a.WholeWord != nil && *a.WholeWord {
					grepArgs = append(grepArgs, "-w")
				}
				grepArgs = append(grepArgs, "-e", a.Pattern, "--", ".")
				cmd = exec.CommandContext(ctx, "grep", grepArgs...)
			}
			cmd.Dir = root

			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()
			text := strings.TrimSpace(out.String())
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
					return "(no matches)", nil
				}
				return "", fmt.Errorf("search failed: %w; %s", err, text)
			}
			if text == "" {
				return "(no matches)", nil
			}

			// Pagination.
			if a.Page != nil && *a.Page > 1 {
				page := *a.Page
				pageSize := 50
				if a.PageSize != nil && *a.PageSize > 0 {
					pageSize = *a.PageSize
				}
				lines := strings.Split(text, "\n")
				start := (page - 1) * pageSize
				end := start + pageSize
				if start >= len(lines) {
					return fmt.Sprintf("[page %d: no results (total %d lines)]", page, len(lines)), nil
				}
				if end > len(lines) {
					end = len(lines)
				}
				text = strings.Join(lines[start:end], "\n")
			}

			return truncate(text), nil
		},
	}
}

// RunShellEnhanced returns an enhanced shell tool with per-command timeout,
// working directory, environment allow-list, and exit-code-aware results.
func RunShellEnhanced(root string, timeout time.Duration) Tool {
	return Tool{
		Name:        "run_shell",
		Description: "Run a shell command via bash with timeout, safety deny-list, and structured exit-code result. Supports working directory and environment allow-list.",
		Schema: object(map[string]any{
			"command":        str("Shell command to run."),
			"timeout":        map[string]any{"type": "integer", "description": "Per-command timeout in seconds (overrides global, within hard ceiling)."},
			"working_dir":    str("Optional subdirectory to run in (contained via SafeJoin)."),
			"env_allow_list": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Environment variables to pass through (default: all)."},
			"login_shell":    map[string]any{"type": "boolean", "description": "Use 'bash -c' instead of 'bash -lc' for faster execution."},
		}, "command"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Command      string   `json:"command"`
				Timeout      *int     `json:"timeout"`
				WorkingDir   string   `json:"working_dir"`
				EnvAllowList []string `json:"env_allow_list"`
				LoginShell   *bool    `json:"login_shell"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if err := safety.CheckCommand(a.Command); err != nil {
				return "", err
			}

			cmdTimeout := timeout
			if a.Timeout != nil && *a.Timeout > 0 {
				cmdTimeout = time.Duration(*a.Timeout) * time.Second
			}

			runCtx, cancel := context.WithTimeout(ctx, cmdTimeout)
			defer cancel()

			// Working directory.
			cmdDir := root
			if a.WorkingDir != "" {
				full, err := safety.SafeJoin(root, a.WorkingDir)
				if err != nil {
					return "", err
				}
				cmdDir = full
			}

			// Environment allow-list.
			var env []string
			if len(a.EnvAllowList) > 0 {
				for _, key := range a.EnvAllowList {
					if val := os.Getenv(key); val != "" {
						env = append(env, key+"="+val)
					}
				}
			} else {
				env = os.Environ()
			}

			loginShell := "bash"
			if a.LoginShell == nil || *a.LoginShell {
				loginShell = "bash"
				// Use -lc for login shell (default), -c for non-login.
			}
			args := []string{"-lc", a.Command}
			if a.LoginShell != nil && !*a.LoginShell {
				args = []string{"-c", a.Command}
			}

			cmd := exec.CommandContext(runCtx, loginShell, args...)
			cmd.Dir = cmdDir
			cmd.Env = env
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()

			body := truncate(strings.TrimSpace(out.String()))
			result := fmt.Sprintf("$ %s\n%s", a.Command, body)

			if runCtx.Err() == context.DeadlineExceeded {
				return fmt.Sprintf("%s\n[timed out after %s]", result, cmdTimeout), nil
			}

			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					return "", fmt.Errorf("run %q: %w", a.Command, err)
				}
			}
			return fmt.Sprintf("%s\n[exit %d]", result, exitCode), nil
		},
	}
}
