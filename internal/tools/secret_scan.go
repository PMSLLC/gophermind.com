package tools

import "gophermind/internal/safety"

var secretScanner = safety.NewSecretScanner()

// secretWarning returns a warning suffix when content appears to contain a
// credential, else "". It is appended to write-tool results so a secret about
// to be written to disk is surfaced rather than silently committed.
func secretWarning(content string) string {
	if secretScanner.Scan(content) {
		return "\n⚠ warning: the written content appears to contain a secret/credential — review before committing."
	}
	return ""
}
