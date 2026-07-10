package safety

import "regexp"

// redactPlaceholder replaces any matched secret/PII span in redacted output.
const redactPlaceholder = "[REDACTED]"

// emailPattern matches email addresses (PII) for redaction. Kept alongside the
// SecretScanner's credential patterns.
var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// Redact replaces credential-like spans (the scanner's patterns) and email
// addresses with a placeholder, so transcripts and sessions can be scrubbed
// before being written to disk. Clean text is returned unchanged.
func (ss *SecretScanner) Redact(content string) string {
	out := emailPattern.ReplaceAllString(content, redactPlaceholder)
	for _, pattern := range ss.patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		out = re.ReplaceAllString(out, redactPlaceholder)
	}
	return out
}
