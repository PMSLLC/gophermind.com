package tools

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// emailRe matches email addresses, the PII category the egress classifier flags
// in addition to the credential patterns the secret scanner detects.
var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// egressMode returns the configured data-egress guard mode from
// GOPHERMIND_EGRESS_GUARD: "warn" appends a warning to a network tool's result
// when the outbound payload matches secret/PII patterns; "deny" blocks the
// request. Any other value (including unset) disables the guard.
func egressMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOPHERMIND_EGRESS_GUARD"))) {
	case "warn":
		return "warn"
	case "deny", "block":
		return "deny"
	default:
		return ""
	}
}

// injectionGuardEnabled reports whether prompt-injection neutralization of
// fetched/untrusted tool output is enabled (GOPHERMIND_INJECTION_GUARD).
func injectionGuardEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOPHERMIND_INJECTION_GUARD"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// classifyEgress returns the categories of sensitive data found in an outbound
// payload ("secret" for credentials, "email" for PII email addresses).
func classifyEgress(payload string) []string {
	var cats []string
	if secretScanner.Scan(payload) {
		cats = append(cats, "secret")
	}
	if emailRe.MatchString(payload) {
		cats = append(cats, "email")
	}
	return cats
}

// guardEgress applies the egress policy to a payload about to be sent by a
// network tool. In "warn" mode it returns a non-empty warning when the payload
// contains sensitive data; in "deny" mode it returns an error instead. A clean
// payload or a disabled guard returns ("", nil).
func guardEgress(mode, payload string) (string, error) {
	if mode == "" {
		return "", nil
	}
	cats := classifyEgress(payload)
	if len(cats) == 0 {
		return "", nil
	}
	detail := strings.Join(cats, ", ")
	if mode == "deny" {
		return "", fmt.Errorf("egress blocked: outbound payload contains %s (GOPHERMIND_EGRESS_GUARD=deny)", detail)
	}
	return fmt.Sprintf("\n⚠ egress warning: outbound payload appears to contain %s.", detail), nil
}
