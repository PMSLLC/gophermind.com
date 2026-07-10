package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GitInfo returns a read-only, structured git tool. It exposes a fixed set of
// non-mutating operations (log, status, diff) with parsed output where useful —
// log and status return JSON arrays, diff returns the raw unified diff. Because
// the operation is chosen from a fixed allowlist and the only free-form input is
// an optional path passed after "--", it cannot run arbitrary git subcommands.
func GitInfo(root string) Tool {
	return Tool{
		Name:        "git_info",
		Description: "Read-only structured git access: op=log (JSON commits), op=status (JSON changed files), or op=diff (unified diff). Optional path narrows the scope. Never mutates the repo.",
		Schema: object(map[string]any{
			"op":    str("One of: log, status, diff."),
			"path":  str("Optional path to scope the operation to."),
			"limit": map[string]any{"type": "integer", "description": "log only: max commits (default 20)."},
		}, "op"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Op    string `json:"op"`
				Path  string `json:"path"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			switch a.Op {
			case "log":
				return gitLog(ctx, root, a.Path, a.Limit)
			case "status":
				return gitStatus(ctx, root, a.Path)
			case "diff":
				return gitDiff(ctx, root, a.Path)
			default:
				return "", fmt.Errorf("unknown op %q: use log, status, or diff", a.Op)
			}
		},
	}
}

// gitFieldSep / gitRecordSep are unlikely-to-collide separators for --pretty.
const (
	gitFieldSep  = "\x1f" // unit separator
	gitRecordSep = "\x1e" // record separator
)

func gitLog(ctx context.Context, root, path string, limit int) (string, error) {
	if limit <= 0 {
		limit = 20
	}
	format := strings.Join([]string{"%H", "%an", "%aI", "%s"}, gitFieldSep) + gitRecordSep
	args := []string{"log", fmt.Sprintf("-n%d", limit), "--pretty=format:" + format}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := runGit(ctx, root, args)
	if err != nil {
		return "", err
	}
	type entry struct {
		Hash    string `json:"hash"`
		Author  string `json:"author"`
		Date    string `json:"date"`
		Subject string `json:"subject"`
	}
	var entries []entry
	for _, rec := range strings.Split(out, gitRecordSep) {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, gitFieldSep)
		if len(f) < 4 {
			continue
		}
		entries = append(entries, entry{Hash: f[0], Author: f[1], Date: f[2], Subject: f[3]})
	}
	return marshalJSON(entries)
}

func gitStatus(ctx context.Context, root, path string) (string, error) {
	args := []string{"status", "--porcelain"}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := runGit(ctx, root, args)
	if err != nil {
		return "", err
	}
	type entry struct {
		Status string `json:"status"`
		Path   string `json:"path"`
	}
	var entries []entry
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		entries = append(entries, entry{
			Status: strings.TrimSpace(line[:2]),
			Path:   strings.TrimSpace(line[3:]),
		})
	}
	return marshalJSON(entries)
}

func gitDiff(ctx context.Context, root, path string) (string, error) {
	args := []string{"diff"}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := runGit(ctx, root, args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "(no changes)", nil
	}
	return truncate(out), nil
}

// runGit executes a git subcommand in root and returns its stdout. Errors carry
// git's stderr so the model can see what went wrong.
func runGit(ctx context.Context, root string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

func marshalJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return truncate(string(b)), nil
}
