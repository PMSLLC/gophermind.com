package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// pluginTimeout bounds how long an out-of-process plugin may run.
const pluginTimeout = 60 * time.Second

// PluginManifest describes a third-party out-of-process tool loaded from a
// <name>.plugin.json file: it names the executable and its JSON schema.
type PluginManifest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Command     string         `json:"command"` // executable path
	Schema      map[string]any `json:"schema"`
}

// PluginTool returns a Tool backed by an out-of-process plugin: the raw args
// JSON is written to the plugin's stdin, and the plugin must print a JSON object
// {"result":"..."} (success) or {"error":"..."} (failure) to stdout. This is the
// stable protocol third parties implement to ship tools without recompiling.
func PluginTool(name, description string, schema map[string]any, command string) Tool {
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	return Tool{
		Name:        name,
		Description: description,
		Schema:      schema,
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, pluginTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, command)
			cmd.Stdin = bytes.NewReader(raw)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("plugin %q failed: %w: %s", name, err, strings.TrimSpace(stderr.String()))
			}
			var resp struct {
				Result string `json:"result"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
				// Not JSON — return the raw stdout as the result.
				return strings.TrimSpace(stdout.String()), nil
			}
			if resp.Error != "" {
				return "", fmt.Errorf("plugin %q: %s", name, resp.Error)
			}
			return resp.Result, nil
		},
	}
}

// LoadPlugins reads *.plugin.json manifests from dir and returns their tools. A
// missing directory returns (nil, nil).
func LoadPlugins(dir string) ([]Tool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Tool
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".plugin.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var m PluginManifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse plugin %s: %w", e.Name(), err)
		}
		if m.Name == "" || m.Command == "" {
			return nil, fmt.Errorf("plugin %s: name and command are required", e.Name())
		}
		out = append(out, PluginTool(m.Name, m.Description, m.Schema, m.Command))
	}
	return out, nil
}
