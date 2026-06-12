package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"gophermind/internal/safety"
)

// RunShell returns the run_shell tool, which executes a command via bash with
// a timeout and the safety deny-list applied. Output is truncated to protect
// the model's context window.
func RunShell(root string, timeout time.Duration) Tool {
	return Tool{
		Name:        "run_shell",
		Description: "Run a shell command via bash in the repository root and return its combined stdout/stderr and exit code. Use this for builds, tests, git status/diff, etc. Destructive commands are blocked.",
		Schema:      object(map[string]any{"command": str("Shell command to run, e.g. 'go test ./...'.")}, "command"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if err := safety.CheckCommand(a.Command); err != nil {
				return "", err
			}

			runCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			cmd := exec.CommandContext(runCtx, "bash", "-lc", a.Command)
			cmd.Dir = root
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()

			body := truncate(strings.TrimSpace(out.String()))
			if runCtx.Err() == context.DeadlineExceeded {
				return fmt.Sprintf("$ %s\n%s\n[timed out after %s]", a.Command, body, timeout), nil
			}
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					return "", fmt.Errorf("run %q: %w", a.Command, err)
				}
			}
			return fmt.Sprintf("$ %s\n%s\n[exit %d]", a.Command, body, exitCode), nil
		},
	}
}
