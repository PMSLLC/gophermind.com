package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// normalizeToolArgs prepares a model-emitted tool-call arguments string for
// decoding by a tool.
//
// Models routinely mis-serialize shell commands and regexes: a backslash in the
// value (`grep '\[0-9\]'`) has to reach JSON as `\\`, and a model that emits a
// single `\[` produces a string no JSON parser will accept. Raw newlines inside
// a string are the same class of mistake. Both are recoverable without guessing
// at intent — the repair only re-escapes what was already there — so they are
// repaired here, once, at the single boundary every tool call passes through,
// rather than in each of the tools' own json.Unmarshal calls.
//
// Truncated arguments are NOT repaired. Closing an unterminated string would
// synthesize a command the model never finished emitting, and run_shell would
// then execute it; a clear error the model can act on is the safe answer.
func normalizeToolArgs(tool, raw string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(raw)
	// A tool with no required arguments is commonly called with nothing at all.
	if trimmed == "" {
		return json.RawMessage("{}"), nil
	}

	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed), nil
	}

	if repaired := reescapeJSONStrings(trimmed); json.Valid([]byte(repaired)) {
		return json.RawMessage(repaired), nil
	}

	// Unrecoverable. Report it in terms the model can act on: the generic
	// "unexpected end of JSON input" tells it nothing about what to do next,
	// and it will usually re-emit the same broken call.
	var syntax error
	if err := json.Unmarshal([]byte(trimmed), &json.RawMessage{}); err != nil {
		syntax = err
	}
	return nil, fmt.Errorf(
		"could not parse the arguments for %s as JSON (%v). "+
			"Re-issue the call with a simpler value: for code searches prefer the `search` tool, "+
			"which takes the pattern as a plain field and needs no shell quoting or backslash escaping",
		tool, syntax)
}

// isJSONEscape reports whether c may follow a backslash in a JSON string.
func isJSONEscape(c byte) bool {
	switch c {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	}
	return false
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// reescapeJSONStrings rewrites the two ways a model commonly breaks a JSON
// string it is quoting a shell command into:
//
//   - an invalid escape (`\[`, `\$`, `\d`) becomes a literal backslash (`\\[`),
//     which is what the model meant — the character it wrote is preserved
//   - a raw control character (a literal newline or tab inside the quotes)
//     becomes its escape sequence
//
// Everything outside a string, and every already-valid escape, is copied
// through untouched. The result is only used when it parses (see
// normalizeToolArgs), so a rewrite that does not actually fix the document is
// discarded rather than passed to a tool.
func reescapeJSONStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)

	inString := false
	for i := 0; i < len(s); {
		c := s[i]

		if !inString {
			if c == '"' {
				inString = true
			}
			b.WriteByte(c)
			i++
			continue
		}

		switch {
		case c == '"':
			inString = false
			b.WriteByte(c)
			i++

		case c == '\\':
			next := byte(0)
			if i+1 < len(s) {
				next = s[i+1]
			}
			valid := isJSONEscape(next)
			// \u must be followed by exactly four hex digits to be an escape;
			// anything else is a literal backslash before a 'u'.
			if next == 'u' {
				valid = i+5 < len(s) && isHex(s[i+2]) && isHex(s[i+3]) && isHex(s[i+4]) && isHex(s[i+5])
			}
			if valid {
				b.WriteByte(c)
				b.WriteByte(next)
				i += 2
				continue
			}
			// A trailing backslash, or one before a character JSON does not
			// recognize: the model meant the backslash itself.
			b.WriteString(`\\`)
			i++

		case c < 0x20:
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				fmt.Fprintf(&b, `\u%04x`, c)
			}
			i++

		default:
			// Copy whole runes so multi-byte characters are never split.
			_, size := utf8.DecodeRuneInString(s[i:])
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}
