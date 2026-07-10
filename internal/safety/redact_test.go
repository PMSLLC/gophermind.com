package safety

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	ss := NewSecretScanner()
	in := "here is a token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 and key sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ done"
	out := ss.Redact(in)
	if strings.Contains(out, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
		t.Errorf("github token not redacted: %q", out)
	}
	if strings.Contains(out, "sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("sk key not redacted: %q", out)
	}
	if !strings.Contains(out, redactPlaceholder) {
		t.Errorf("no placeholder present: %q", out)
	}
	// surrounding prose survives
	if !strings.Contains(out, "here is a token") || !strings.Contains(out, "done") {
		t.Errorf("prose damaged: %q", out)
	}
}

func TestRedactEmail(t *testing.T) {
	ss := NewSecretScanner()
	out := ss.Redact("contact john@example.com for access")
	if strings.Contains(out, "john@example.com") {
		t.Errorf("email not redacted: %q", out)
	}
}

func TestRedactLeavesCleanTextUnchanged(t *testing.T) {
	ss := NewSecretScanner()
	in := "the quick brown fox"
	if out := ss.Redact(in); out != in {
		t.Errorf("clean text changed: %q", out)
	}
}
