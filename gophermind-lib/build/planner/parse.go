package planner

import (
	"encoding/json"
	"strings"
)

// StripReply removes what models wrap around the JSON they were asked for: a
// markdown fence, a sentence before it, a sentence after it. A reply that is
// already valid JSON is returned as it is, so a fence inside a JSON string
// (Go source in a test file) is never mistaken for a wrapper.
//
// A reply that is not valid JSON only because a string holds a backslash that
// is not a JSON escape (a regex such as go1\.2, a Windows path) is repaired
// by doubling that backslash. If the repair does not make it valid, the
// unrepaired text is returned so the decoder's error stays truthful.
func StripReply(text string) string {
	t := stripReply(text)
	if json.Valid([]byte(t)) {
		return t
	}
	if fixed := repairJSONEscapes(t); fixed != t && json.Valid([]byte(fixed)) {
		return fixed
	}
	return t
}

func stripReply(text string) string {
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

// repairJSONEscapes doubles every backslash inside a JSON string literal that
// does not start a valid escape: one not followed by one of " \\ / b f n r t,
// or by u and four hex digits. Text outside strings is left alone.
func repairJSONEscapes(t string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case !inStr:
			if c == '"' {
				inStr = true
			}
		case c == '"':
			inStr = false
		case c == '\\' && i+1 < len(t):
			n := t[i+1]
			if strings.IndexByte(`"\\/bfnrt`, n) >= 0 || (n == 'u' && validUnicodeEscape(t[i+2:])) {
				b.WriteByte(c)
				b.WriteByte(n)
				i++
				continue
			}
			b.WriteString(`\\`)
			continue
		case c == '\\':
			b.WriteString(`\\`)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func validUnicodeEscape(rest string) bool {
	if len(rest) < 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := rest[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
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
