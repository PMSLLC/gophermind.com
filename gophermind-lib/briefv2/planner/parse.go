package planner

import (
	"encoding/json"
	"strings"
)

// StripReply removes what models wrap around the JSON they were asked for: a
// markdown fence, a sentence before it, a sentence after it. A reply that is
// already valid JSON is returned as it is, so a fence inside a JSON string
// (Go source in a test file) is never mistaken for a wrapper.
func StripReply(text string) string {
	t := strings.TrimSpace(text)
	if json.Valid([]byte(t)) {
		return t
	}
	if blocks := fencedBlocks(t); len(blocks) > 0 {
		for _, b := range blocks {
			if json.Valid([]byte(b)) {
				return b
			}
		}
		return blocks[0]
	}
	start := strings.IndexAny(t, "{[")
	if start < 0 {
		return t
	}
	closer := "}"
	if t[start] == '[' {
		closer = "]"
	}
	end := strings.LastIndex(t, closer)
	if end < start {
		return t[start:]
	}
	return t[start : end+1]
}

// fencedBlocks returns the trimmed content of every ``` block, in order. An
// unclosed fence runs to the end of the text.
func fencedBlocks(t string) []string {
	var out []string
	var cur []string
	in := false
	for _, line := range strings.Split(t, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if in {
				out = append(out, strings.TrimSpace(strings.Join(cur, "\n")))
				cur = nil
			}
			in = !in
			continue
		}
		if in {
			cur = append(cur, line)
		}
	}
	if in {
		out = append(out, strings.TrimSpace(strings.Join(cur, "\n")))
	}
	return out
}

// Question finds the pause-and-ask marker: a line that is exactly QUESTION:
// with the question on the lines after it.
func Question(text string) (q string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "QUESTION:" {
			q = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			return q, q != ""
		}
	}
	return "", false
}
