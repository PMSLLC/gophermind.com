package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/internal/llm"
)

// mutatingTools are the tools whose calls represent changes the agent made.
var mutatingTools = map[string]bool{
	"write_file": true, "edit_file": true, "apply_patch": true,
	"move_file": true, "delete_file": true, "mkdir": true,
}

// Diff summarizes what happened across a saved session: message counts by role,
// the file-mutating tool calls (with paths), and the shell commands run — so an
// agent's work can be reviewed at a glance.
func Diff(id string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return diffIn(dir, id)
}

func diffIn(dir, id string) (string, error) {
	if err := validID(id); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return "", fmt.Errorf("session %q not found", id)
	}

	roleCounts := map[string]int{}
	var fileChanges []string
	var shellCmds []string

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m llm.Message
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		roleCounts[m.Role]++
		for _, tc := range m.ToolCalls {
			name := tc.Function.Name
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			switch {
			case mutatingTools[name]:
				path, _ := args["path"].(string)
				if path == "" {
					path, _ = args["source"].(string)
				}
				fileChanges = append(fileChanges, fmt.Sprintf("%s %s", name, path))
			case name == "run_shell":
				if cmd, ok := args["command"].(string); ok {
					shellCmds = append(shellCmds, cmd)
				}
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "session %s\n", id)
	b.WriteString("messages: ")
	roles := make([]string, 0, len(roleCounts))
	for r := range roleCounts {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		parts = append(parts, fmt.Sprintf("%s=%d", r, roleCounts[r]))
	}
	b.WriteString(strings.Join(parts, " ") + "\n")

	if len(fileChanges) == 0 {
		b.WriteString("file changes: (no file changes)\n")
	} else {
		b.WriteString("file changes:\n")
		for _, c := range fileChanges {
			b.WriteString("  " + c + "\n")
		}
	}
	if len(shellCmds) > 0 {
		b.WriteString("shell commands:\n")
		for _, c := range shellCmds {
			b.WriteString("  $ " + c + "\n")
		}
	}
	return b.String(), nil
}
