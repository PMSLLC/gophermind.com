package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/secaudit"
)

func TestParseSecAuditArgsDefaults(t *testing.T) {
	o, err := parseSecAuditArgs([]string{"myrepo"})
	if err != nil {
		t.Fatal(err)
	}
	if o.path != "myrepo" {
		t.Errorf("path = %q", o.path)
	}
	if o.staticOnly {
		t.Error("staticOnly should default false")
	}
	if o.failOn != secaudit.High {
		t.Errorf("failOn = %v, want High", o.failOn)
	}
	if o.out != filepath.Join("myrepo", "SECURITY-AUDIT.md") {
		t.Errorf("out = %q", o.out)
	}
}

func TestParseSecAuditArgsFlags(t *testing.T) {
	o, err := parseSecAuditArgs([]string{"r", "--static-only", "--out", "/tmp/a.md", "--fail-on", "critical"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.staticOnly || o.out != "/tmp/a.md" || o.failOn != secaudit.Critical {
		t.Errorf("flags not parsed: %+v", o)
	}
}

func TestParseSecAuditArgsBadSeverity(t *testing.T) {
	if _, err := parseSecAuditArgs([]string{"r", "--fail-on", "nope"}); err == nil {
		t.Error("expected an error for a bad severity")
	}
}

func TestParseSecAuditArgsUnknownFlag(t *testing.T) {
	if _, err := parseSecAuditArgs([]string{"r", "--frobnicate"}); err == nil {
		t.Error("expected an error for an unknown flag")
	}
}

// TestRunSecAuditWritesReportAndFailsOnFinding: a repo with a critical issue
// gets a report written and returns a non-zero (error) result at the default
// threshold.
func TestRunSecAuditWritesReportAndFailsOnFinding(t *testing.T) {
	dir := t.TempDir()
	// A committed private key: a Critical finding.
	if err := os.WriteFile(filepath.Join(dir, "id_rsa"),
		[]byte("-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runSecAudit(context.Background(), nil, []string{dir, "--static-only"})
	if err == nil {
		t.Error("expected a non-nil error (findings at/above threshold)")
	}

	b, rerr := os.ReadFile(filepath.Join(dir, "SECURITY-AUDIT.md"))
	if rerr != nil {
		t.Fatalf("report not written: %v", rerr)
	}
	if !strings.Contains(string(b), "private-key") {
		t.Error("report missing the finding")
	}
	if !strings.Contains(strings.ToLower(string(b)), "not") {
		t.Error("report missing the disclaimer")
	}
}

// TestRunSecAuditCleanRepoSucceeds: a repo with nothing to find returns nil
// (exit 0) and still writes a report.
func TestRunSecAuditCleanRepoSucceeds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.go"),
		[]byte("package x\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runSecAudit(context.Background(), nil, []string{dir, "--static-only"}); err != nil {
		t.Errorf("clean repo returned an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SECURITY-AUDIT.md")); err != nil {
		t.Errorf("report not written for a clean repo: %v", err)
	}
}
