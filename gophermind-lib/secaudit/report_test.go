package secaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleReport() *Report {
	return &Report{
		Target: "/repo",
		Findings: []Finding{
			{RuleID: "private-key (CWE-321)", Severity: Critical, Category: "secrets",
				File: "id_rsa", Line: 1, Snippet: "-----BEGIN RSA PRIVATE KEY-----",
				Message: "Private key committed.", Remediation: "Rotate and remove.", Verified: true, VerifyNote: "confirmed real key"},
			{RuleID: "weak-crypto (CWE-327)", Severity: Medium, Category: "crypto",
				File: "h.py", Line: 3, Snippet: "hashlib.md5(x)", Message: "Weak hash.", Remediation: "Use SHA-256."},
		},
	}
}

// TestRenderHasDisclaimer is non-negotiable: the report must never read as a
// certificate of security.
func TestRenderHasDisclaimer(t *testing.T) {
	out := Render(sampleReport())
	low := strings.ToLower(out)
	if !strings.Contains(low, "not") || !(strings.Contains(low, "prove") || strings.Contains(low, "guarantee")) {
		t.Errorf("report lacks a 'does not prove security' disclaimer:\n%s", out)
	}
}

// TestRenderShowsCountsAndFindings covers the substance.
func TestRenderShowsCountsAndFindings(t *testing.T) {
	out := Render(sampleReport())
	for _, want := range []string{"critical", "medium", "private-key", "id_rsa:1", "Rotate and remove", "CWE-321"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

// TestRenderOrdersWorstFirst: a reader should see critical before medium.
func TestRenderOrdersWorstFirst(t *testing.T) {
	out := Render(sampleReport())
	if strings.Index(out, "private-key") > strings.Index(out, "weak-crypto") {
		t.Error("medium finding rendered above the critical one")
	}
}

// TestRenderCleanReport: a run with no findings still says so and still carries
// the disclaimer (clean is not "secure").
func TestRenderCleanReport(t *testing.T) {
	out := Render(&Report{Target: "/repo"})
	low := strings.ToLower(out)
	if !strings.Contains(low, "no findings") && !strings.Contains(low, "nothing") {
		t.Errorf("clean report does not say it found nothing:\n%s", out)
	}
	if !strings.Contains(low, "not") {
		t.Errorf("clean report dropped the disclaimer:\n%s", out)
	}
}

// TestRenderMarksVerification distinguishes a confirmed finding from an
// unreviewed one, so the reader knows the confidence.
func TestRenderMarksVerification(t *testing.T) {
	out := Render(sampleReport())
	if !strings.Contains(strings.ToLower(out), "verified") {
		t.Errorf("verified findings not marked:\n%s", out)
	}
}

// TestWriteReportCreatesFile round-trips to disk.
func TestWriteReportCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SECURITY-AUDIT.md")
	if err := WriteReport(sampleReport(), path); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "private-key") {
		t.Error("written report missing content")
	}
}
