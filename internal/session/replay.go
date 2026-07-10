package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/llm"
)

// Replay returns a readable, turn-by-turn rendering of a saved session, so an
// agent's reasoning can be reviewed step by step.
func Replay(id string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return replayIn(dir, id)
}

func replayIn(dir, id string) (string, error) {
	if err := validID(id); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return "", fmt.Errorf("session %q not found", id)
	}
	if isEncrypted(data) {
		return "", fmt.Errorf("session %q is encrypted; decrypt it before replay", id)
	}

	var b strings.Builder
	turn := 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m llm.Message
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		turn++
		label := m.Role
		if m.Name != "" {
			label += " (" + m.Name + ")"
		}
		fmt.Fprintf(&b, "── turn %d · %s ──\n", turn, label)
		if content := strings.TrimSpace(m.Content); content != "" {
			b.WriteString(content + "\n")
		}
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&b, "  → calls %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
		}
		b.WriteString("\n")
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if turn == 0 {
		return "(empty session)\n", nil
	}
	return b.String(), nil
}
