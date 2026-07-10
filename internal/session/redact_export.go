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
	"gophermind/internal/safety"
)

// ExportRedacted writes a saved session to dst with credential/PII spans in each
// message scrubbed, so a debugging session can be shared safely.
func ExportRedacted(id, dst string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return exportRedactedIn(dir, id, dst)
}

func exportRedactedIn(dir, id, dst string) error {
	if err := validID(id); err != nil {
		return err
	}
	src := filepath.Join(dir, id+".jsonl")
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("session %q not found", id)
	}
	if isEncrypted(data) {
		return fmt.Errorf("session %q is encrypted; decrypt it before redacted export", id)
	}

	scanner := safety.NewSecretScanner()
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m llm.Message
		if json.Unmarshal([]byte(line), &m) != nil {
			// Not a message object — redact the raw line defensively.
			out.WriteString(scanner.Redact(line) + "\n")
			continue
		}
		m.Content = scanner.Redact(m.Content)
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		out.Write(b)
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return os.WriteFile(dst, out.Bytes(), 0o600)
}
