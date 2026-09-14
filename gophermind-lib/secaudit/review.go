package secaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reviewer is the minimal model interface the verification pass needs: given a
// prompt, return the model's reply. *llm.Client satisfies this via ClientReviewer.
type Reviewer interface {
	Review(ctx context.Context, prompt string) (string, error)
}

// Verify asks the reviewer to confirm or refute each static finding, annotating
// Verified and VerifyNote. It is deliberately conservative: a refuted finding is
// KEPT (its confidence lowered), never dropped — a weak local model must not be
// able to erase a real static finding, only downgrade it. A nil reviewer, a
// reviewer error, or an unparseable reply leaves the finding unverified. The
// finding count is never reduced.
func Verify(ctx context.Context, root string, findings []Finding, r Reviewer) []Finding {
	if r == nil {
		return findings
	}
	out := make([]Finding, len(findings))
	copy(out, findings)

	for i := range out {
		if ctx.Err() != nil {
			break
		}
		prompt := verifyPrompt(root, out[i])
		reply, err := r.Review(ctx, prompt)
		if err != nil {
			continue // offline / failed: leave this finding unverified
		}
		verdict, ok := parseVerdict(reply)
		if !ok {
			continue // couldn't parse: no verdict rather than a guessed one
		}
		out[i].Verified = verdict.Exploitable
		note := strings.TrimSpace(verdict.Note)
		if verdict.Exploitable {
			out[i].VerifyNote = note
		} else {
			// Refuted: keep the finding, record why the model was unconvinced.
			if note == "" {
				note = "model could not confirm this is exploitable"
			}
			out[i].VerifyNote = "unconfirmed: " + note
		}
	}
	return out
}

type verdict struct {
	Exploitable bool   `json:"exploitable"`
	Note        string `json:"note"`
}

// verifyPrompt asks for a single JSON verdict, including a slice of the file
// around the finding so the model has context without the whole file (which may
// not fit a small local window).
func verifyPrompt(root string, f Finding) string {
	ctxLines := readAround(filepath.Join(root, f.File), f.Line, 12)
	var b strings.Builder
	b.WriteString("You are triaging a static security finding to reduce false positives.\n\n")
	fmt.Fprintf(&b, "Finding: %s\nSeverity: %s\nFile: %s:%d\nIssue: %s\n\n", f.RuleID, f.Severity, f.File, f.Line, f.Message)
	if ctxLines != "" {
		b.WriteString("Surrounding code:\n\n")
		b.WriteString(ctxLines)
		b.WriteString("\n\n")
	}
	b.WriteString("Is this actually exploitable in this code? Reply with ONE JSON object and nothing else:\n")
	b.WriteString(`{"exploitable": true|false, "note": "<one sentence why>"}`)
	return b.String()
}

// readAround returns up to `radius` lines on each side of line (1-based) from
// path, or "" if the file cannot be read.
func readAround(path string, line, radius int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	lo := line - radius - 1
	if lo < 0 {
		lo = 0
	}
	hi := line + radius
	if hi > len(lines) {
		hi = len(lines)
	}
	var b strings.Builder
	for i := lo; i < hi; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i+1, lines[i])
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseVerdict extracts the JSON verdict from a reply, tolerating prose or
// fenced blocks around it.
func parseVerdict(reply string) (verdict, bool) {
	raw, ok := firstJSONObject(reply)
	if !ok {
		return verdict{}, false
	}
	var v verdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return verdict{}, false
	}
	return v, true
}

// firstJSONObject returns the first brace-balanced JSON object in s, ignoring
// braces inside string literals.
func firstJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	var depth int
	var inString, escaped bool
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}
