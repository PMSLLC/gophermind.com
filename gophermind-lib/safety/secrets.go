package safety

import (
	"bufio"
	"os"
	"strings"
)

// ResolveSecret resolves a credential reference at call time without persisting
// it. A value of the form "@NAME" is looked up in the key=value secrets file at
// secretsFile (so tokens live in one gitignored file, never in config or
// transcripts); any other value is returned unchanged. An unresolvable ref
// yields "".
func ResolveSecret(value, secretsFile string) string {
	name, ok := strings.CutPrefix(value, "@")
	if !ok {
		return value
	}
	if secretsFile == "" {
		return ""
	}
	f, err := os.Open(secretsFile)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == name {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
