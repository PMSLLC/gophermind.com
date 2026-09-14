package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSecretRef(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "secrets.env")
	os.WriteFile(f, []byte("# comment\nGITHUB_TOKEN=ghp_secret\nJIRA_TOKEN = jira-secret \n"), 0o600)

	// A plain value passes through unchanged.
	if got := ResolveSecret("plain-value", f); got != "plain-value" {
		t.Errorf("plain value should pass through, got %q", got)
	}
	// An @ref resolves from the file (whitespace trimmed).
	if got := ResolveSecret("@GITHUB_TOKEN", f); got != "ghp_secret" {
		t.Errorf("resolve @GITHUB_TOKEN = %q, want ghp_secret", got)
	}
	if got := ResolveSecret("@JIRA_TOKEN", f); got != "jira-secret" {
		t.Errorf("resolve @JIRA_TOKEN = %q, want jira-secret", got)
	}
	// An unknown ref resolves to empty (not the literal @ref).
	if got := ResolveSecret("@NOPE", f); got != "" {
		t.Errorf("unknown ref should be empty, got %q", got)
	}
	// With no secrets file, an @ref cannot resolve -> empty.
	if got := ResolveSecret("@GITHUB_TOKEN", ""); got != "" {
		t.Errorf("no file should yield empty, got %q", got)
	}
}
