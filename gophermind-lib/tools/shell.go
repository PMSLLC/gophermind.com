package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"gophermind/gophermind-lib/gitenv"
	"gophermind/gophermind-lib/safety"
)

var (
	loginPathOnce sync.Once
	loginPath     string
)

// loginShellPath returns PATH as a login shell computes it, resolved once per
// process. Commands then run under `bash -c`, which skips ~/.bash_profile: a
// profile that inits conda or similar forks a Python interpreter per call,
// costing seconds and several processes every time a tool runs. Reading the
// PATH once keeps the user's toolchain reachable without paying that cost on
// every invocation.
func loginShellPath() string {
	loginPathOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "bash", "-lc", `printf %s "$PATH"`).Output()
		if err != nil {
			return
		}
		loginPath = strings.TrimSpace(string(out))
	})
	return loginPath
}

// RunShell returns the run_shell tool, which executes a command via bash with
// a timeout and the safety deny-list applied. Output is truncated to protect
// the model's context window.
func RunShell(root string, timeout time.Duration) Tool {
	return Tool{
		Name:        "run_shell",
		Description: "Run a shell command via bash in the repository root and return its combined stdout/stderr and exit code. Use this for builds, tests, git status/diff, etc. Destructive commands are blocked. For code searches prefer the `search` tool: a regex full of backslashes often fails to serialize into these JSON arguments.",
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

			cmd := exec.CommandContext(runCtx, "bash", "-c", a.Command)
			cmd.Dir = root
			// Env is set unconditionally, never left nil: a nil Env makes the
			// child inherit the parent's environment wholesale, GIT_* included,
			// which is the case this strips. An inherited GIT_DIR redirects a
			// git command away from cmd.Dir and onto whatever repository that
			// variable names, and run_shell is the path an agent runs git
			// through. See internal/gitenv.
			env := gitenv.SanitizedEnv()
			if p := loginShellPath(); p != "" {
				env = append(env, "PATH="+p)
			}
			cmd.Env = env
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
