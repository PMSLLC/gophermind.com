package serve

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/safety"
	"gophermind/gophermind-lib/tools"
)

// maxToolOutput is the most any chat tool returns in one call.
const maxToolOutput = 8 << 10

type chatToolsKey struct{}

// withChatTools returns a context carrying the extra tools a chat turn may use.
func withChatTools(ctx context.Context, ts []tools.Tool) context.Context {
	return context.WithValue(ctx, chatToolsKey{}, ts)
}

// ChatToolsFrom returns the chat tools carried by ctx, or nil when the server
// registered none (for example when briefs are not enabled).
func ChatToolsFrom(ctx context.Context) []tools.Tool {
	ts, _ := ctx.Value(chatToolsKey{}).([]tools.Tool)
	return ts
}

// chatTools is the set of brief tools offered to the web chat agent.
type chatTools struct {
	bm *briefManager
	h  *harness // may be nil
}

// all returns the tools.
func (c *chatTools) all() []tools.Tool {
	return []tools.Tool{c.briefsList(), c.briefState(), c.questionsPending(), c.harnessState()}
}

// capOutput keeps the tail of s within max bytes. When it cuts, the result ends
// with a note naming the file to read for the whole text.
func capOutput(s string, max int, pointer string) string {
	if len(s) <= max {
		return s
	}
	note := fmt.Sprintf("\n[tail of %d bytes; full text at %s]", len(s), pointer)
	keep := max - len(note)
	if keep < 0 {
		keep = 0
	}
	tail := s[len(s)-keep:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return tail + note
}

// bearerRE catches short bearer tokens the scanner's 40-character rule misses.
var bearerRE = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`)

// clean redacts secrets from s, then caps it.
func (c *chatTools) clean(s string) string {
	s = bearerRE.ReplaceAllString(safety.NewSecretScanner().Redact(s), "Bearer [REDACTED]")
	return capOutput(s, maxToolOutput, "the run's run.log")
}

// validBriefID reports whether id is safe to use as a run id: non-empty, in
// the question-id character set, and free of path separators.
func validBriefID(id string) bool {
	return questionIDRE.MatchString(id) && !strings.ContainsAny(id, "/\\") && id != "." && id != ".."
}
