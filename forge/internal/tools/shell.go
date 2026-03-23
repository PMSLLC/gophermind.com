package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type CommandResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

var blockedPatterns = []string{
	"rm -rf",
	"sudo ",
	"git reset --hard",
	"git clean -fd",
	"> /",
	">~",
	":(){",
}

func RunCommand(parent context.Context, root, command string, timeoutSeconds int) (CommandResult, error) {
	if err := validateCommand(command); err != nil {
		return CommandResult{}, err
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = root

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := CommandResult{
		Command: command,
		Stdout:  truncateOutput(stdout.String()),
		Stderr:  truncateOutput(stderr.String()),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("run command %q: %w", command, err)
	}

	result.ExitCode = 0
	return result, nil
}

func validateCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return fmt.Errorf("empty command")
	}
	for _, blocked := range blockedPatterns {
		if strings.Contains(trimmed, blocked) {
			return fmt.Errorf("blocked command pattern detected: %s", blocked)
		}
	}
	return nil
}

func truncateOutput(s string) string {
	const max = 12000
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... [truncated]"
}
