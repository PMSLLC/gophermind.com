package planner

import "strings"

// errNoTestFile is the refusal for a Test-writer reply that decoded but holds
// no Go file, neither in test_file nor in a fenced block after the object.
const errNoTestFile = "reply has no test file: after the JSON object, put the complete Go test file in one fenced go block"

// splitSourceReply splits a reply into the JSON head and the Go file that
// follows it. The file is the body (ending in a newline) of the last fenced block whose info string
// is empty or starts with go (any case) and whose trimmed body starts with
// "package ". The head is the text before that block's opening fence,
// trimmed. With no such block the head is the whole text and the file is "".
func splitSourceReply(text string) (head, source string) {
	var (
		offset    int
		open      bool
		info      string
		openAt    int
		body      []string
		foundAt   = -1
		foundBody string
	)
	for _, raw := range strings.SplitAfter(text, "\n") {
		line := strings.TrimRight(raw, "\r\n")
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "```") {
			if !open {
				open, info, openAt, body = true, strings.TrimSpace(strings.TrimLeft(t, "`")), offset, nil
			} else {
				open = false
				src := strings.Join(body, "\n") + "\n"
				if goInfo(info) && strings.HasPrefix(strings.TrimSpace(src), "package ") {
					foundAt, foundBody = openAt, src
				}
			}
		} else if open {
			body = append(body, line)
		}
		offset += len(raw)
	}
	if foundAt < 0 {
		return text, ""
	}
	return strings.TrimSpace(text[:foundAt]), foundBody
}

func goInfo(info string) bool {
	info = strings.ToLower(info)
	return info == "" || strings.HasPrefix(info, "go")
}
