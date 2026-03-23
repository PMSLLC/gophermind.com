package tools

import (
	"context"
)

func Diff(parent context.Context, root string, timeoutSeconds int) (string, error) {
	result, err := RunCommand(parent, root, "git diff -- . ':(exclude).forge'", timeoutSeconds)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}
