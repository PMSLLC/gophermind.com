package tools

import (
	"context"
	"fmt"
	"strings"
)

func Search(parent context.Context, root, pattern string, timeoutSeconds int) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("empty search pattern")
	}
	result, err := RunCommand(parent, root, fmt.Sprintf("rg -n --hidden --glob '!.git' %q .", pattern), timeoutSeconds)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 && result.Stdout == "" && result.Stderr == "" {
		return "", nil
	}
	combined := strings.TrimSpace(result.Stdout)
	if combined == "" {
		combined = strings.TrimSpace(result.Stderr)
	}
	return combined, nil
}
