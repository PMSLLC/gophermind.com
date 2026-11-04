package planner

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"strings"
)

// errNoTestFile is the refusal for a Test-writer reply that decoded but holds
// no Go file, neither in test_file nor in a fenced block after the object.
const errNoTestFile = "reply has no test file: after the JSON object, put the complete Go test file in one fenced go block"

// splitSourceReply splits a reply into the JSON head and the Go file that
// follows it. The file is the body (ending in a newline) of the last fenced
// block whose info string is empty or starts with go (any case) and whose
// body parses up to its package clause (so comments and //go:build lines may
// come first). A trailing block with no closing fence counts when its body
// parses as a whole Go file (a reply cut off at the token limit). The head is
// the text before that block's opening fence, trimmed. With no such block the
// head is the text with its go blocks removed (so a JSON error is about the
// head, not the file) and the file is "".
//
// A fence closes only on a bare run of backticks at least as long as the
// opener's (the CommonMark rule), so a fence line with an info string inside
// the file does not end it. A bare fence inside the file (only a block comment can hold one, since a Go raw string cannot hold a backtick) does close a
// block by that rule; when the body so cut is not Go but a later closer makes
// it parse, the later closer ends the file.
func splitSourceReply(text string) (head, source string) {
	lines := strings.SplitAfter(text, "\n")
	offsets := make([]int, len(lines))
	trimmed := make([]string, len(lines))
	off := 0
	for i, raw := range lines {
		offsets[i], trimmed[i] = off, strings.TrimSpace(raw)
		off += len(raw)
	}
	foundAt, foundBody := -1, ""
	for i := 0; i < len(lines); i++ {
		t := trimmed[i]
		if !strings.HasPrefix(t, "```") {
			continue
		}
		run := len(t) - len(strings.TrimLeft(t, "`"))
		info := strings.TrimSpace(t[run:])
		var closers []int
		for j := i + 1; j < len(lines); j++ {
			if c := trimmed[j]; c != "" && strings.Trim(c, "`") == "" && len(c) >= run {
				closers = append(closers, j)
			}
		}
		if len(closers) == 0 {
			// An unclosed fence is a block only when it is Go to the end.
			if body := fenceBody(lines, i, len(lines)); goInfo(info) && isGoFile(body) {
				foundAt, foundBody = offsets[i], body
				break
			}
			continue
		}
		end := closers[0]
		qualifies := false
		for _, j := range closers {
			if hasPackageClause(fenceBody(lines, i, j)) {
				qualifies = true
				break
			}
		}
		if body := fenceBody(lines, i, end); goInfo(info) && qualifies {
			for _, j := range closers {
				if isGoFile(fenceBody(lines, i, j)) {
					end, body = j, fenceBody(lines, i, j)
					break
				}
			}
			foundAt, foundBody = offsets[i], body
		}
		i = end
	}
	if foundAt < 0 {
		return dropGoBlocks(text), ""
	}
	return strings.TrimSpace(text[:foundAt]), foundBody
}

// fenceBody is the lines strictly between open and close, \r trimmed, each
// ended by a newline.
func fenceBody(lines []string, open, close int) string {
	var b strings.Builder
	for _, l := range lines[open+1 : close] {
		if l == "" { // the empty tail SplitAfter leaves after a final newline
			continue
		}
		b.WriteString(strings.TrimRight(l, "\r\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

func isGoFile(src string) bool {
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	return err == nil
}

func goInfo(info string) bool {
	info = strings.ToLower(info)
	return info == "" || strings.HasPrefix(info, "go")
}

// hasPackageClause reports whether src parses up to its package clause.
func hasPackageClause(src string) bool {
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.PackageClauseOnly)
	return err == nil
}

// dropGoBlocks removes the fenced blocks whose info string starts with go
// (any case), an unclosed one to the end of the text. Text that is already
// valid JSON is returned as it is.
func dropGoBlocks(text string) string {
	if json.Valid([]byte(strings.TrimSpace(text))) {
		return text
	}
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "```") {
			run := len(t) - len(strings.TrimLeft(t, "`"))
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t[run:])), "go") {
				end := len(lines)
				for j := i + 1; j < len(lines); j++ {
					if c := strings.TrimSpace(lines[j]); c != "" && strings.Trim(c, "`") == "" && len(c) >= run {
						end = j
						break
					}
				}
				i = end
				continue
			}
		}
		out.WriteString(lines[i])
	}
	return out.String()
}
